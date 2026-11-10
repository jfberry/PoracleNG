package db

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"

	"github.com/pokemon/poracleng/processor/internal/db/migrations"
)

// otherCollation is set by migrationTestDB to a collation other than the
// utf8mb4_unicode_ci the migrations normalise to.
var otherCollation string

var profileRepairTables = []string{"monsters", "raid", "egg", "quest", "invasion", "lures", "nests", "gym", "forts", "weather", "maxbattle"}

// migrationTestDB creates a unique disposable database (never touching tables
// in the supplied DSN's database) and returns a DSN for it. The account must be
// allowed to CREATE/DROP DATABASE.
func migrationTestDB(t *testing.T) *mysql.Config {
	t.Helper()
	dsn := os.Getenv("PORACLENG_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set PORACLENG_MIGRATION_TEST_DSN to run the MySQL migration regression")
	}
	cfg, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	cfg.DBName = ""
	cfg.MultiStatements = true
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	// MySQL 8's default collation; MariaDB has none of that name.
	otherCollation = "utf8mb4_general_ci"
	var mysql8 int
	require.NoError(t, admin.QueryRow("SELECT COUNT(*) FROM information_schema.COLLATIONS WHERE COLLATION_NAME = 'utf8mb4_0900_ai_ci'").Scan(&mysql8))
	if mysql8 > 0 {
		otherCollation = "utf8mb4_0900_ai_ci"
	}
	name := fmt.Sprintf("poracle_collation_%d", time.Now().UnixNano())
	_, err = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE " + otherCollation)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE `" + name + "`") })
	cfg.DBName = name
	return cfg
}

func openMigrationTestDB(t *testing.T, cfg *mysql.Config) *sql.DB {
	t.Helper()
	conn, err := sql.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)
	conn.SetMaxOpenConns(1) // migration temporary tables belong to this connection
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func migrationExec(t *testing.T, conn *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := conn.Exec(query, args...)
	require.NoError(t, err)
}

// seedProfileZero builds a legacy-shaped schema whose tables alternate between
// collations, with one human per repair case:
//   - live: on profile 0 (its live area must reach profile 1)
//   - switched: moved to profile 2 after rebuilding profile 1 (abandoned rules dropped)
//   - empty: moved to profile 2 with an empty profile 1 (rules restored to 1)
//   - real: has a genuine profile 0 (left alone)
func seedProfileZero(t *testing.T, conn *sql.DB) {
	t.Helper()
	migrationExec(t, conn, `CREATE TABLE humans (id VARCHAR(255) PRIMARY KEY, current_profile_no INT,
		name VARCHAR(255), area TEXT, latitude DOUBLE, longitude DOUBLE) COLLATE `+otherCollation)
	migrationExec(t, conn, `CREATE TABLE profiles (id VARCHAR(255), profile_no INT, name VARCHAR(255),
		area TEXT, latitude DOUBLE, longitude DOUBLE, UNIQUE KEY (id, profile_no)) COLLATE utf8mb4_unicode_ci`)
	for i, table := range profileRepairTables {
		collation := "utf8mb4_unicode_ci"
		if i%2 == 0 {
			collation = otherCollation
		}
		migrationExec(t, conn, "CREATE TABLE `"+table+"` (uid INT PRIMARY KEY AUTO_INCREMENT, id VARCHAR(255), profile_no INT) ENGINE=InnoDB COLLATE "+collation)
		for _, id := range []string{"live", "switched", "empty", "real"} {
			migrationExec(t, conn, "INSERT INTO `"+table+"` (id, profile_no) VALUES (?, 0)", id)
		}
	}
	migrationExec(t, conn, `INSERT INTO humans VALUES
		('live', 0, 'Live', '["live area"]', 51, -1),
		('switched', 2, 'Switched', '[]', 0, 0),
		('empty', 2, 'Empty', '[]', 0, 0),
		('real', 0, 'Real', '[]', 0, 0)`)
	migrationExec(t, conn, `INSERT INTO profiles VALUES ('real', 0, 'Real zero', '[]', 0, 0),
		('switched', 1, 'Default', '["keep"]', 20, 30)`)
	// A rule only in maxbattle must count when deciding whether to discard
	// abandoned profile-0 rules in every other table.
	migrationExec(t, conn, `INSERT INTO maxbattle (id, profile_no) VALUES ('switched', 1), ('live', 1)`)
}

func runMigrationFile(conn *sql.DB, name string) error {
	migration, err := migrations.FS.ReadFile(name)
	if err != nil {
		return err
	}
	_, err = conn.Exec(string(migration))
	return err
}

func requireProfileZeroRepaired(t *testing.T, conn *sql.DB) {
	t.Helper()
	for _, table := range profileRepairTables {
		for _, id := range []string{"live", "switched", "empty", "real"} {
			var zero, one int
			err := conn.QueryRow("SELECT COUNT(CASE WHEN profile_no = 0 THEN 1 END), COUNT(CASE WHEN profile_no = 1 THEN 1 END) FROM `"+table+"` WHERE id = ?", id).Scan(&zero, &one)
			require.NoError(t, err)
			wantZero, wantOne := 0, 1
			switch id {
			case "real":
				wantZero, wantOne = 1, 0
			case "switched":
				wantOne = 0
				if table == "maxbattle" {
					wantOne = 1
				}
			case "live":
				if table == "maxbattle" {
					wantOne = 2
				}
			}
			require.Equal(t, wantZero, zero, "%s/%s profile 0", table, id)
			require.Equal(t, wantOne, one, "%s/%s profile 1", table, id)
		}
	}
	var profile int
	for id, want := range map[string]int{"live": 1, "switched": 2, "empty": 2, "real": 0} {
		require.NoError(t, conn.QueryRow("SELECT current_profile_no FROM humans WHERE id = ?", id).Scan(&profile))
		require.Equal(t, want, profile, id)
	}
	var area string
	var lat, lon float64
	require.NoError(t, conn.QueryRow("SELECT area, latitude, longitude FROM profiles WHERE id = 'live' AND profile_no = 1").Scan(&area, &lat, &lon))
	require.Equal(t, `["live area"]`, area)
	require.Equal(t, float64(51), lat)
	require.Equal(t, float64(-1), lon)
	require.NoError(t, conn.QueryRow("SELECT area FROM profiles WHERE id = 'switched' AND profile_no = 1").Scan(&area))
	require.Equal(t, `["keep"]`, area)
}

func TestProfileZeroMigrationMixedCollations(t *testing.T) {
	conn := openMigrationTestDB(t, migrationTestDB(t))
	seedProfileZero(t, conn)

	require.NoError(t, runMigrationFile(conn, "000009_profile_zero_to_one.up.sql"))
	requireProfileZeroRepaired(t, conn)

	// Re-running (a dirty retry, or a manual reset to version 8) changes nothing.
	require.NoError(t, runMigrationFile(conn, "000009_profile_zero_to_one.up.sql"))
	requireProfileZeroRepaired(t, conn)
}

func TestProfileZeroMigrationFailureChangesNothing(t *testing.T) {
	cfg := migrationTestDB(t)
	conn := openMigrationTestDB(t, cfg)
	seedProfileZero(t, conn)
	// Fail on the last table, after every other table's rules have moved.
	migrationExec(t, conn, `CREATE TRIGGER fail_maxbattle BEFORE UPDATE ON maxbattle FOR EACH ROW
		SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'injected failure'`)
	require.ErrorContains(t, runMigrationFile(conn, "000009_profile_zero_to_one.up.sql"), "injected failure")
	// The migrator exits on failure; closing the connection rolls back.
	require.NoError(t, conn.Close())

	conn = openMigrationTestDB(t, cfg)
	var moved, live int
	require.NoError(t, conn.QueryRow("SELECT COUNT(*) FROM monsters WHERE profile_no = 1").Scan(&moved))
	require.Equal(t, 0, moved)
	require.NoError(t, conn.QueryRow("SELECT current_profile_no FROM humans WHERE id = 'live'").Scan(&live))
	require.Equal(t, 0, live)

	migrationExec(t, conn, "DROP TRIGGER fail_maxbattle")
	require.NoError(t, runMigrationFile(conn, "000009_profile_zero_to_one.up.sql"))
	requireProfileZeroRepaired(t, conn)
}

func TestRunMigrationsDirtyState(t *testing.T) {
	conn := openMigrationTestDB(t, migrationTestDB(t))
	seedProfileZero(t, conn)
	// Each migrator keeps a dedicated connection (RunMigrations cannot Close it:
	// that would close the shared pool too).
	conn.SetMaxOpenConns(4)
	migrationExec(t, conn, "CREATE TABLE schema_migrations (version BIGINT PRIMARY KEY, dirty BOOLEAN NOT NULL)")

	// A version that is not known to be safe to re-run must not be marked
	// clean and skipped.
	migrationExec(t, conn, "INSERT INTO schema_migrations VALUES (5, true)")
	require.ErrorContains(t, RunMigrations(conn), "migration 5 is dirty")
	var version int
	var dirty bool
	require.NoError(t, conn.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	require.Equal(t, 5, version)
	require.True(t, dirty)

	// A failed 000009 is retried, and the repair it was meant to make happens.
	migrationExec(t, conn, "UPDATE schema_migrations SET version = 9")
	require.NoError(t, RunMigrations(conn))
	require.NoError(t, conn.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	require.GreaterOrEqual(t, version, 9)
	require.False(t, dirty)
	requireProfileZeroRepaired(t, conn)
}
