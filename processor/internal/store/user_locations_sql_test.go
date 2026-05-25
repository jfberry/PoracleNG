package store

import (
	"os"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// openTestDB connects to the test MySQL instance via PORACLENG_TEST_DSN and
// creates the tables needed for user-location tests. Skips the test if the
// env var is unset or the connection fails.
func openTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("PORACLENG_TEST_DSN")
	if dsn == "" {
		t.Skip("requires test DB (set PORACLENG_TEST_DSN)")
	}
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Skipf("requires test DB: connect failed: %v", err)
	}

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("setup query failed (%s): %v", q, err)
		}
	}

	// Ensure parent humans table exists.
	exec(`CREATE TABLE IF NOT EXISTS humans (
		id varchar(64) NOT NULL,
		type varchar(32) NOT NULL DEFAULT '',
		name varchar(128) NOT NULL DEFAULT '',
		enabled tinyint(1) NOT NULL DEFAULT 1,
		area text,
		latitude double NOT NULL DEFAULT 0,
		longitude double NOT NULL DEFAULT 0,
		fails int NOT NULL DEFAULT 0,
		last_checked datetime,
		language varchar(8),
		admin_disable tinyint(1) NOT NULL DEFAULT 0,
		disabled_date datetime,
		current_profile_no int NOT NULL DEFAULT 1,
		community_membership text,
		area_restriction text,
		notes text,
		blocked_alerts text,
		PRIMARY KEY (id)
	) ENGINE=InnoDB`)

	exec(`CREATE TABLE IF NOT EXISTS user_locations (
		uid        INT PRIMARY KEY AUTO_INCREMENT,
		id         VARCHAR(50) NOT NULL,
		label      VARCHAR(64) NOT NULL,
		latitude   float(14,10) NOT NULL,
		longitude  float(14,10) NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY uniq_id_label (id, label)
	) ENGINE=InnoDB`)

	// Minimal tracking tables needed by CountLocationReferences (all 10 types).
	for _, tbl := range []string{"monsters", "raid", "egg", "quest", "invasion", "lures", "nests", "gym", "forts", "maxbattle"} {
		exec(`CREATE TABLE IF NOT EXISTS ` + tbl + ` (
			uid                     INT PRIMARY KEY AUTO_INCREMENT,
			id                      VARCHAR(50) NOT NULL,
			profile_no              INT NOT NULL DEFAULT 0,
			pokemon_id              INT NOT NULL DEFAULT 0,
			level                   INT NOT NULL DEFAULT 0,
			override_location_label VARCHAR(64) NULL
		) ENGINE=InnoDB`)
	}

	// Seed the parent rows required by any FK constraints.
	exec(`INSERT IGNORE INTO humans (id, type, name) VALUES ('u1', 'discord:user', 'tester')`)

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM user_locations WHERE id = 'u1'`)
		for _, tbl := range []string{"monsters", "raid", "egg", "quest", "invasion", "lures", "nests", "gym", "forts", "maxbattle"} {
			_, _ = db.Exec(`DELETE FROM ` + tbl + ` WHERE id = 'u1'`)
		}
		_, _ = db.Exec(`DELETE FROM humans WHERE id = 'u1'`)
		_ = db.Close()
	})

	return db
}

// mustExec runs a SQL statement and fails the test on error.
func mustExec(t *testing.T, db *sqlx.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("mustExec(%q): %v", query, err)
	}
}

func TestUserLocationsSQL_RoundTrip(t *testing.T) {
	dbx := openTestDB(t)
	defer dbx.Close()
	s := &SQLHumanStore{db: dbx}

	if _, err := s.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1}); err != nil {
		t.Fatalf("AddLocation: %v", err)
	}
	if _, err := s.AddLocation(UserLocation{ID: "u1", Label: "Work", Latitude: 51.6, Longitude: -0.2}); err != nil {
		t.Fatalf("AddLocation: %v", err)
	}

	list, err := s.ListLocations("u1")
	if err != nil || len(list) != 2 {
		t.Fatalf("ListLocations: got %d rows err=%v", len(list), err)
	}

	got, err := s.GetLocation("u1", "home") // lowercase lookup
	if err != nil || got == nil || got.Label != "Home" {
		t.Fatalf("GetLocation case-insensitive: got=%+v err=%v", got, err)
	}

	if _, err := s.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 0, Longitude: 0}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}

	if err := s.DeleteLocation("u1", "WORK"); err != nil {
		t.Fatalf("DeleteLocation: %v", err)
	}
	list, _ = s.ListLocations("u1")
	if len(list) != 1 || list[0].Label != "Home" {
		t.Fatalf("after delete, got %+v", list)
	}
}

func TestCountLocationReferences(t *testing.T) {
	dbx := openTestDB(t)
	defer dbx.Close()
	s := &SQLHumanStore{db: dbx}

	mustExec(t, dbx, `INSERT INTO monsters (id, profile_no, pokemon_id, override_location_label) VALUES ('u1', 0, 25, 'Home'), ('u1', 0, 26, 'Home')`)
	mustExec(t, dbx, `INSERT INTO raid     (id, profile_no, pokemon_id, level, override_location_label) VALUES ('u1', 0, 0, 5, 'home')`)

	refs, err := s.CountLocationReferences("u1", "HOME")
	if err != nil {
		t.Fatalf("CountLocationReferences: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs (2 pokemon + 1 raid), got %d: %+v", len(refs), refs)
	}
}
