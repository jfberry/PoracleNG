package state

import (
	"errors"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/store"
)

func TestLoadSummarySchedules_NilStore(t *testing.T) {
	got := loadSummarySchedules(nil)
	if got == nil {
		t.Fatalf("expected non-nil empty map")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(got))
	}
}

func TestLoadSummarySchedules_EmptyStore(t *testing.T) {
	mock := store.NewMockSummaryScheduleStore()
	got := loadSummarySchedules(mock)
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(got))
	}
}

func TestLoadSummarySchedules_PopulatesMap(t *testing.T) {
	mock := store.NewMockSummaryScheduleStore()

	// Two users with parsed quest schedules.
	if err := mock.Set("user-a", "quest", `[{"day":1,"hours":9,"mins":0}]`); err != nil {
		t.Fatalf("seed Set: %v", err)
	}
	if err := mock.Set("user-b", "quest", `[{"day":2,"hours":10,"mins":30}]`); err != nil {
		t.Fatalf("seed Set: %v", err)
	}
	// Non-quest schedule (alert type not currently in summaryScheduleAlertTypes
	// — should be skipped silently).
	if err := mock.Set("user-c", "raid", `[{"day":3,"hours":11,"mins":0}]`); err != nil {
		t.Fatalf("seed Set: %v", err)
	}

	got := loadSummarySchedules(mock)
	if len(got) != 2 {
		t.Fatalf("expected 2 users with quest schedules, got %d (entries: %v)", len(got), got)
	}
	if _, ok := got["user-a"]["quest"]; !ok {
		t.Errorf("expected quest schedule for user-a")
	}
	if _, ok := got["user-b"]["quest"]; !ok {
		t.Errorf("expected quest schedule for user-b")
	}
	if _, ok := got["user-c"]; ok {
		t.Errorf("user-c is raid-only and should not appear in summary schedules: %v", got["user-c"])
	}

	entries := got["user-a"]["quest"]
	if len(entries) != 1 || entries[0].Day != 1 || entries[0].Hours != 9 {
		t.Errorf("user-a entries unexpected: %+v", entries)
	}
}

func TestLoadSummarySchedules_EmptyParsedHoursStillStored(t *testing.T) {
	mock := store.NewMockSummaryScheduleStore()

	// Empty active_hours JSON — parser returns nil; we still want the
	// (id, alertType) mapping present so the future scheduler can no-op.
	if err := mock.Set("user-empty", "quest", `[]`); err != nil {
		t.Fatalf("seed Set: %v", err)
	}

	got := loadSummarySchedules(mock)
	if _, ok := got["user-empty"]; !ok {
		t.Fatalf("expected user-empty to be present even with empty active_hours")
	}
	if _, ok := got["user-empty"]["quest"]; !ok {
		t.Fatalf("expected quest key to be present (entries may be nil)")
	}
}

// TestLoadSummarySchedules_StoreErrorIsTolerated pins the resilience
// contract: a transient DB error on ListByType must not abort the
// state reload (which would also kill the alert-side tracking refresh).
// The loader logs and continues with an empty map for the affected
// alert type, the same convention as the humans / profiles loaders.
func TestLoadSummarySchedules_StoreErrorIsTolerated(t *testing.T) {
	mock := store.NewMockSummaryScheduleStore()
	mock.InjectListError(errors.New("simulated db hiccup"))

	got := loadSummarySchedules(mock)
	if got == nil {
		t.Fatal("expected non-nil empty map even on store error")
	}
	if len(got) != 0 {
		t.Errorf("expected empty map on store error, got %d entries", len(got))
	}
}

// TestCountSummaryScheduleEntries pins the user-vs-entry-count
// distinction in the state log line: one user with two alert types
// should report as 2 entries.
func TestCountSummaryScheduleEntries(t *testing.T) {
	mock := store.NewMockSummaryScheduleStore()
	_ = mock.Set("user-a", "quest", `[{"day":1,"hours":9,"mins":0}]`)
	_ = mock.Set("user-b", "quest", `[{"day":2,"hours":10,"mins":0}]`)
	schedules := loadSummarySchedules(mock)
	if got := countSummaryScheduleEntries(schedules); got != 2 {
		t.Errorf("countSummaryScheduleEntries = %d, want 2", got)
	}
}
