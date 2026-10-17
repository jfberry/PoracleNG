package store

import (
	"os"
	"slices"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// openTestDB connects to the test MySQL instance via PORACLENG_TEST_DSN and
// creates the tables needed for user-location tests. Skips the test if the
// env var is unset or the connection fails.
//
// Callers must NOT `defer db.Close()`: deferred closes run before
// t.Cleanup callbacks, so the row-deleting cleanup registered here would
// silently fail against a closed connection and leak rows into the next
// test. The cleanup closes the connection itself.
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

	// Mirrors 000001_initial_schema.up.sql.
	exec(`CREATE TABLE IF NOT EXISTS profiles (
		uid          int unsigned NOT NULL AUTO_INCREMENT,
		id           varchar(255) NOT NULL,
		profile_no   int NOT NULL DEFAULT 1,
		name         varchar(255) NOT NULL,
		area         text NOT NULL,
		latitude     float(14,10) NOT NULL DEFAULT 0,
		longitude    float(14,10) NOT NULL DEFAULT 0,
		active_hours varchar(4096) NOT NULL DEFAULT '[]',
		PRIMARY KEY (uid),
		UNIQUE KEY profile_unique (id, profile_no)
	) ENGINE=InnoDB`)

	// Minimal tracking tables needed by CountLocationReferences and StripOverrideAreas (all 10 types).
	for _, tbl := range []string{"monsters", "raid", "egg", "quest", "invasion", "lures", "nests", "gym", "forts", "maxbattle"} {
		exec(`CREATE TABLE IF NOT EXISTS ` + tbl + ` (
			uid                     INT PRIMARY KEY AUTO_INCREMENT,
			id                      VARCHAR(50) NOT NULL,
			profile_no              INT NOT NULL DEFAULT 0,
			pokemon_id              INT NOT NULL DEFAULT 0,
			level                   INT NOT NULL DEFAULT 0,
			override_location_label VARCHAR(64) NULL,
			override_areas          TEXT NULL
		) ENGINE=InnoDB`)
	}

	// Seed the parent rows required by any FK constraints.
	exec(`INSERT IGNORE INTO humans (id, type, name) VALUES ('u1', 'discord:user', 'tester')`)

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM user_locations WHERE id = 'u1'`)
		for _, tbl := range []string{"monsters", "raid", "egg", "quest", "invasion", "lures", "nests", "gym", "forts", "maxbattle"} {
			_, _ = db.Exec(`DELETE FROM ` + tbl + ` WHERE id = 'u1'`)
		}
		_, _ = db.Exec(`DELETE FROM profiles WHERE id = 'u1'`)
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

// --- dropDeniedAreas (pure in-memory helper) ---

func TestDropDeniedAreas_StripsDenied(t *testing.T) {
	result := dropDeniedAreas([]string{"munich", "berlin", "hamburg"}, map[string]bool{"hamburg": true})
	if !slices.Equal(result, []string{"munich", "berlin"}) {
		t.Fatalf("got %v", result)
	}
}

func TestDropDeniedAreas_AllDenied(t *testing.T) {
	result := dropDeniedAreas([]string{"berlin", "hamburg"}, map[string]bool{"berlin": true, "hamburg": true})
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %v", result)
	}
}

// A name absent from denied survives even if it is not a known fence.
func TestDropDeniedAreas_UnknownNameKept(t *testing.T) {
	result := dropDeniedAreas([]string{"ghost", "berlin"}, map[string]bool{"berlin": true})
	if !slices.Equal(result, []string{"ghost"}) {
		t.Fatalf("got %v, want [ghost]", result)
	}
}

func TestDropDeniedAreas_EmptyInput(t *testing.T) {
	if result := dropDeniedAreas(nil, map[string]bool{"munich": true}); len(result) != 0 {
		t.Fatalf("expected empty result for nil input, got %v", result)
	}
}

func TestDropDeniedAreas_CaseInsensitive(t *testing.T) {
	result := dropDeniedAreas([]string{"Munich", "BERLIN"}, map[string]bool{"berlin": true})
	if len(result) != 1 || result[0] != "Munich" {
		t.Fatalf("expected [Munich], got %v", result)
	}
}

// --- StripOverrideAreas (integration test, requires PORACLENG_TEST_DSN) ---

func TestStripOverrideAreas_StripsDenied(t *testing.T) {
	dbx := openTestDB(t)
	s := &SQLHumanStore{db: dbx}

	mustExec(t, dbx, `INSERT INTO monsters (id, profile_no, pokemon_id, override_areas) VALUES
		('u1', 0, 25, '["berlin","munich"]'),
		('u1', 0, 26, '["berlin"]')`)

	denied := map[string]bool{"berlin": true}
	if err := s.StripOverrideAreas("u1", denied); err != nil {
		t.Fatalf("StripOverrideAreas: %v", err)
	}

	var rows []struct {
		PokemonID     int    `db:"pokemon_id"`
		OverrideAreas string `db:"override_areas"`
	}
	if err := dbx.Select(&rows, `SELECT pokemon_id, COALESCE(override_areas, '') AS override_areas FROM monsters WHERE id='u1' ORDER BY pokemon_id`); err != nil {
		t.Fatalf("select after prune: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].OverrideAreas != `["munich"]` {
		t.Fatalf("pokemon 25: expected pruned to munich only, got %q", rows[0].OverrideAreas)
	}
	if rows[1].OverrideAreas != "" {
		t.Fatalf("pokemon 26: expected NULL after empty pruning, got %q", rows[1].OverrideAreas)
	}
}

// A PUT that re-submits a location's current coordinates must succeed.
// MySQL reports rows *changed* (not matched) for UPDATE without
// CLIENT_FOUND_ROWS, so a no-op update affects zero rows even though the
// row exists — which UpdateLocation used to misread as not-found (404).
func TestUserLocationsSQL_UpdateUnchangedCoordinates(t *testing.T) {
	dbx := openTestDB(t)
	s := NewSQLHumanStore(dbx)

	if _, err := s.AddLocation(UserLocation{ID: "u1", Label: "PutNoop", Latitude: 51.5, Longitude: -0.12}); err != nil {
		t.Fatalf("AddLocation: %v", err)
	}

	// Same coordinates — zero rows changed, but the row exists.
	if err := s.UpdateLocation("u1", "putnoop", 51.5, -0.12); err != nil {
		t.Fatalf("UpdateLocation with unchanged coordinates should succeed, got: %v", err)
	}

	// A genuinely missing label must still report not-found.
	if err := s.UpdateLocation("u1", "nowhere", 1, 2); err == nil {
		t.Fatal("UpdateLocation on missing label should fail")
	}
}

func TestStripUserAreas_SQL(t *testing.T) {
	dbx := openTestDB(t)
	s := &SQLHumanStore{db: dbx}
	mustExec(t, dbx, `UPDATE humans SET area = '["alpha","beta","gent_centrum"]', current_profile_no = 1 WHERE id = 'u1'`)
	mustExec(t, dbx, `INSERT INTO profiles (id, profile_no, name, area) VALUES
		('u1', 1, 'one', '["alpha","beta","gent_centrum"]'),
		('u1', 2, 'two', '["beta","delta"]')`)

	removed, err := s.StripUserAreas("u1", map[string]bool{"beta": true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(removed, []string{"beta"}) {
		t.Errorf("removed = %v, want [beta]", removed)
	}
	var humanArea string
	if err := dbx.Get(&humanArea, `SELECT area FROM humans WHERE id='u1'`); err != nil {
		t.Fatal(err)
	}
	if humanArea != `["alpha","gent_centrum"]` {
		t.Errorf("humans.area = %s", humanArea)
	}
	// A name absent from denied survives even when it is not a fence.
	mustExec(t, dbx, `UPDATE humans SET area = '["ghost","beta"]' WHERE id = 'u1'`)
	if _, err := s.StripUserAreas("u1", map[string]bool{"beta": true}); err != nil {
		t.Fatal(err)
	}
	if err := dbx.Get(&humanArea, `SELECT area FROM humans WHERE id='u1'`); err != nil || humanArea != `["ghost"]` {
		t.Errorf("humans.area = %q err=%v, want [\"ghost\"] kept", humanArea, err)
	}
	var rows []struct {
		ProfileNo int    `db:"profile_no"`
		Area      string `db:"area"`
	}
	if err := dbx.Select(&rows, `SELECT profile_no, area FROM profiles WHERE id='u1' ORDER BY profile_no`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Area != `["alpha","gent_centrum"]` || rows[1].Area != `["delta"]` {
		t.Errorf("profiles = %+v", rows)
	}
	// Everything pruned writes [] (not NULL), matching marshalStringSlice.
	if _, err := s.StripUserAreas("u1", map[string]bool{"alpha": true, "beta": true, "ghost": true, "gent_centrum": true, "delta": true}); err != nil {
		t.Fatal(err)
	}
	if err := dbx.Get(&humanArea, `SELECT area FROM humans WHERE id='u1'`); err != nil {
		t.Fatal(err)
	}
	if humanArea != `[]` {
		t.Errorf("fully pruned humans.area = %q, want []", humanArea)
	}
}

func TestStripUserAreas_SQL_NoHuman(t *testing.T) {
	dbx := openTestDB(t)
	s := &SQLHumanStore{db: dbx}
	mustExec(t, dbx, `INSERT INTO profiles (id, profile_no, name, area) VALUES ('ghost', 1, 'one', '["beta","alpha"]')`)
	t.Cleanup(func() { _, _ = dbx.Exec(`DELETE FROM profiles WHERE id = 'ghost'`) })

	removed, err := s.StripUserAreas("ghost", map[string]bool{"beta": true})
	if err != nil {
		t.Fatal(err)
	}
	if removed != nil {
		t.Errorf("removed = %v, want nil", removed)
	}
	var area string
	if err := dbx.Get(&area, `SELECT area FROM profiles WHERE id='ghost'`); err != nil {
		t.Fatal(err)
	}
	if area != `["beta","alpha"]` {
		t.Errorf("profile area = %s, want unchanged", area)
	}
}
