package store

import (
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// newTestSummaryScheduleStore opens a connection to a test MySQL instance via
// PORACLENG_TEST_DSN and returns a fresh store with the summary_schedules
// table re-created. If the env var is unset or the connection fails, the test
// is skipped — DB integration tests are nice-to-have for this store, build /
// vet must pass regardless.
func newTestSummaryScheduleStore(t *testing.T) *SQLSummaryScheduleStore {
	t.Helper()

	dsn := os.Getenv("PORACLENG_TEST_DSN")
	if dsn == "" {
		t.Skip("requires test DB (set PORACLENG_TEST_DSN)")
	}

	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Skipf("requires test DB: connect failed: %v", err)
	}

	// Reset state: humans is the FK target; ensure a parent row exists for
	// the IDs used by the tests, then clear schedules.
	exec := func(q string, args ...any) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("setup query failed (%s): %v", q, err)
		}
	}

	// summary_schedules has FK to humans(id); ensure parents exist.
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
	exec(`CREATE TABLE IF NOT EXISTS summary_schedules (
		id varchar(64) NOT NULL,
		alert_type varchar(32) NOT NULL,
		active_hours varchar(4096) NOT NULL DEFAULT '[]',
		PRIMARY KEY (id, alert_type)
	) ENGINE=InnoDB`)
	exec(`DELETE FROM summary_schedules`)
	exec(`DELETE FROM humans WHERE id LIKE 'test-%'`)
	exec(`INSERT INTO humans (id, type, name) VALUES ('test-user-1', 'discord:user', 'tester1')`)
	exec(`INSERT INTO humans (id, type, name) VALUES ('test-user-2', 'discord:user', 'tester2')`)

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM summary_schedules`)
		_, _ = db.Exec(`DELETE FROM humans WHERE id LIKE 'test-%'`)
		_ = db.Close()
	})

	return NewSQLSummaryScheduleStore(db)
}

func TestSummaryScheduleStore_Roundtrip(t *testing.T) {
	s := newTestSummaryScheduleStore(t)
	hours := `[{"day":1,"hours":7,"mins":30}]`

	if err := s.Set("test-user-1", "quest", hours); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get("test-user-1", "quest")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil after Set")
	}
	if got.ID != "test-user-1" || got.AlertType != "quest" {
		t.Errorf("identity wrong: %+v", got)
	}
	if got.ActiveHours != hours {
		t.Errorf("ActiveHours = %q, want %q", got.ActiveHours, hours)
	}
	if len(got.ParsedActiveHours) != 1 {
		t.Fatalf("ParsedActiveHours len = %d, want 1", len(got.ParsedActiveHours))
	}
	if got.ParsedActiveHours[0].Hours != 7 || got.ParsedActiveHours[0].Mins != 30 {
		t.Errorf("ParsedActiveHours[0] = %+v", got.ParsedActiveHours[0])
	}
}

func TestSummaryScheduleStore_Update(t *testing.T) {
	s := newTestSummaryScheduleStore(t)
	if err := s.Set("test-user-1", "quest", `[{"day":1,"hours":7,"mins":0}]`); err != nil {
		t.Fatal(err)
	}
	updated := `[{"day":2,"hours":18,"mins":15}]`
	if err := s.Set("test-user-1", "quest", updated); err != nil {
		t.Fatalf("Set (update): %v", err)
	}
	got, err := s.Get("test-user-1", "quest")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ActiveHours != updated {
		t.Errorf("after update ActiveHours = %v, want %q", got, updated)
	}
}

func TestSummaryScheduleStore_Delete(t *testing.T) {
	s := newTestSummaryScheduleStore(t)
	if err := s.Set("test-user-1", "quest", `[]`); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("test-user-1", "quest"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := s.Get("test-user-1", "quest")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("Get after Delete returned %+v, want nil", got)
	}

	// Deleting a missing row is not an error.
	if err := s.Delete("test-user-1", "quest"); err != nil {
		t.Errorf("Delete missing row: %v", err)
	}
}

func TestSummaryScheduleStore_GetMissing(t *testing.T) {
	s := newTestSummaryScheduleStore(t)
	got, err := s.Get("test-user-1", "quest")
	if err != nil {
		t.Fatalf("Get missing: %v", err)
	}
	if got != nil {
		t.Errorf("Get missing returned %+v, want nil", got)
	}
}

func TestSummaryScheduleStore_ListByType(t *testing.T) {
	s := newTestSummaryScheduleStore(t)
	if err := s.Set("test-user-1", "quest", `[{"day":1,"hours":7,"mins":0}]`); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("test-user-2", "quest", `[{"day":2,"hours":8,"mins":0}]`); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("test-user-1", "raid", `[{"day":3,"hours":9,"mins":0}]`); err != nil {
		t.Fatal(err)
	}

	rows, err := s.ListByType("quest")
	if err != nil {
		t.Fatalf("ListByType: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("ListByType(quest) returned %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if r.AlertType != "quest" {
			t.Errorf("ListByType returned wrong alert type: %s", r.AlertType)
		}
		if len(r.ParsedActiveHours) != 1 {
			t.Errorf("ParsedActiveHours not populated for %s", r.ID)
		}
	}
}
