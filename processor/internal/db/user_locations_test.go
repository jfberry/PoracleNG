package db

import (
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// openTestDB opens a sqlx connection via PORACLENG_TEST_DSN and skips the
// test if the env var is unset or the connection fails.
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
	return db
}

// mustExec runs a SQL statement and fatals on error.
func mustExec(t *testing.T, db *sqlx.DB, query string, args ...interface{}) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("mustExec(%q): %v", query, err)
	}
}

func TestLoadUserLocations_GroupsByID(t *testing.T) {
	dbx := openTestDB(t)
	defer dbx.Close()

	// Ensure the table exists (idempotent for test environments that haven't
	// run the full migration suite).
	mustExec(t, dbx, `CREATE TABLE IF NOT EXISTS user_locations (
		uid        INT PRIMARY KEY AUTO_INCREMENT,
		id         VARCHAR(50) NOT NULL,
		label      VARCHAR(64) NOT NULL,
		latitude   DOUBLE NOT NULL DEFAULT 0,
		longitude  DOUBLE NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY uniq_id_label (id, label)
	) ENGINE=InnoDB`)

	// Clean up any leftover rows from a previous run.
	mustExec(t, dbx, `DELETE FROM user_locations WHERE id IN ('u1','u2')`)

	t.Cleanup(func() {
		_, _ = dbx.Exec(`DELETE FROM user_locations WHERE id IN ('u1','u2')`)
	})

	mustExec(t, dbx, `INSERT INTO user_locations (id, label, latitude, longitude) VALUES
		('u1', 'Home', 51.5, -0.1),
		('u1', 'Work', 51.6, -0.2),
		('u2', 'Home', 40.7, -74.0)`)

	got, err := LoadUserLocations(dbx)
	if err != nil {
		t.Fatalf("LoadUserLocations: %v", err)
	}
	if len(got["u1"]) != 2 {
		t.Fatalf("expected 2 entries for u1, got %d: %+v", len(got["u1"]), got["u1"])
	}
	if len(got["u2"]) != 1 {
		t.Fatalf("expected 1 entry for u2, got %d: %+v", len(got["u2"]), got["u2"])
	}
	// Keys must be lowercased.
	if got["u1"]["home"] == nil {
		t.Fatalf("expected lowercased key 'home' for u1, got keys: %v", keysOf(got["u1"]))
	}
	if got["u1"]["home"].Latitude != 51.5 {
		t.Fatalf("u1/home latitude: got %v, want 51.5", got["u1"]["home"].Latitude)
	}
	if got["u1"]["work"] == nil {
		t.Fatalf("expected lowercased key 'work' for u1")
	}
	if got["u2"]["home"] == nil {
		t.Fatalf("expected lowercased key 'home' for u2")
	}
}

// keysOf returns the keys of a map for diagnostic messages.
func keysOf(m map[string]*UserLocation) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
