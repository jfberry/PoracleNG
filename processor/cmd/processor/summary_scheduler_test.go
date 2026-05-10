package main

import (
	"sync"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// dispatchRecorder is a thread-safe SummaryDispatch that records every
// call so tests can assert what fired.
type dispatchRecorder struct {
	mu    sync.Mutex
	calls []dispatchCall
}

type dispatchCall struct {
	HumanID   string
	AlertType string
}

func (r *dispatchRecorder) fn() SummaryDispatch {
	return func(humanID, alertType string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.calls = append(r.calls, dispatchCall{HumanID: humanID, AlertType: alertType})
	}
}

func (r *dispatchRecorder) snapshot() []dispatchCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]dispatchCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// minimalHumanStore is a tiny in-memory HumanStore for the scheduler tests.
// We don't need most of the interface; only Get is exercised by tick().
type minimalHumanStore struct {
	store.HumanStore
	humans map[string]*store.Human
	getErr error
}

func (m *minimalHumanStore) Get(id string) (*store.Human, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	h, ok := m.humans[id]
	if !ok {
		return nil, nil
	}
	return h, nil
}

func newScheduler(
	t *testing.T,
	mgr *state.Manager,
	humans store.HumanStore,
	dispatch SummaryDispatch,
	now time.Time,
) *SummaryScheduler {
	t.Helper()
	s := NewSummaryScheduler(
		schedulerConfig{Locale: "en", QuestSummaryBufferTTLHours: 24},
		mgr,
		humans,
		store.NewMockSummaryScheduleStore(),
		tracker.NewSummaryBuffer(""),
		dispatch,
		0,
	)
	s.nowFunc = func() time.Time { return now }
	return s
}

// TestSummaryScheduler_Tick_FiresWhenScheduleMatches asserts that tick
// invokes dispatch with the right (humanID, alertType) when an entry
// matches the current local time.
func TestSummaryScheduler_Tick_FiresWhenScheduleMatches(t *testing.T) {
	// "now" = Tue 2026-03-24 14:05 UTC, schedule entry = Tue 14:00.
	// matchesTimeWindow: same day, same hour, nowMin >= entry.Mins,
	// (nowMin - entry.Mins) < 10 → true.
	now := time.Date(2026, 3, 24, 14, 5, 0, 0, time.UTC)

	mgr := state.NewManager()
	mgr.Set(&state.State{
		SummarySchedules: map[string]map[string][]db.ActiveHourEntry{
			"discord:user:42": {
				"quest": {{Day: 2, Hours: 14, Mins: 0}},
			},
		},
	})
	humans := &minimalHumanStore{
		humans: map[string]*store.Human{
			"discord:user:42": {ID: "discord:user:42", Type: "discord:user", Latitude: 0, Longitude: 0},
		},
	}
	rec := &dispatchRecorder{}

	s := newScheduler(t, mgr, humans, rec.fn(), now)
	s.tick()

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 dispatch, got %d: %+v", len(calls), calls)
	}
	if calls[0].HumanID != "discord:user:42" || calls[0].AlertType != "quest" {
		t.Errorf("unexpected dispatch: %+v", calls[0])
	}
}

// TestSummaryScheduler_Tick_SkipsWhenScheduleEmpty asserts that empty
// entry lists are skipped and never dispatched.
func TestSummaryScheduler_Tick_SkipsWhenScheduleEmpty(t *testing.T) {
	now := time.Date(2026, 3, 24, 14, 5, 0, 0, time.UTC)
	mgr := state.NewManager()
	mgr.Set(&state.State{
		SummarySchedules: map[string]map[string][]db.ActiveHourEntry{
			"discord:user:42": {
				"quest": nil,
			},
		},
	})
	humans := &minimalHumanStore{
		humans: map[string]*store.Human{
			"discord:user:42": {ID: "discord:user:42", Type: "discord:user"},
		},
	}
	rec := &dispatchRecorder{}

	s := newScheduler(t, mgr, humans, rec.fn(), now)
	s.tick()

	if calls := rec.snapshot(); len(calls) != 0 {
		t.Errorf("expected no dispatch for empty entries, got %+v", calls)
	}
}

// TestSummaryScheduler_Tick_SkipsWhenHumanMissing asserts that a schedule
// for an absent human is silently skipped (the buffer scheduler should
// never panic on stale state).
func TestSummaryScheduler_Tick_SkipsWhenHumanMissing(t *testing.T) {
	now := time.Date(2026, 3, 24, 14, 5, 0, 0, time.UTC)
	mgr := state.NewManager()
	mgr.Set(&state.State{
		SummarySchedules: map[string]map[string][]db.ActiveHourEntry{
			"discord:user:gone": {
				"quest": {{Day: 2, Hours: 14, Mins: 0}},
			},
		},
	})
	humans := &minimalHumanStore{humans: map[string]*store.Human{}}
	rec := &dispatchRecorder{}

	s := newScheduler(t, mgr, humans, rec.fn(), now)
	s.tick()

	if calls := rec.snapshot(); len(calls) != 0 {
		t.Errorf("expected no dispatch when human missing, got %+v", calls)
	}
}

// TestSummaryScheduler_Tick_RespectsScheduleWindow asserts the 10-minute
// matchesTimeWindow constraint: a schedule at 14:00 fires when "now" is
// in [14:00, 14:10) but not at 14:11.
func TestSummaryScheduler_Tick_RespectsScheduleWindow(t *testing.T) {
	humanID := "discord:user:42"
	scheduleEntry := db.ActiveHourEntry{Day: 2, Hours: 14, Mins: 0}

	mgr := state.NewManager()
	mgr.Set(&state.State{
		SummarySchedules: map[string]map[string][]db.ActiveHourEntry{
			humanID: {"quest": {scheduleEntry}},
		},
	})
	humans := &minimalHumanStore{
		humans: map[string]*store.Human{
			humanID: {ID: humanID, Type: "discord:user"},
		},
	}

	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{
			name: "inside window — Tue 14:05",
			now:  time.Date(2026, 3, 24, 14, 5, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "outside window — Tue 14:11",
			now:  time.Date(2026, 3, 24, 14, 11, 0, 0, time.UTC),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &dispatchRecorder{}
			s := newScheduler(t, mgr, humans, rec.fn(), tc.now)
			s.tick()
			got := len(rec.snapshot()) > 0
			if got != tc.want {
				t.Errorf("dispatch fired=%v, want %v (now=%s)", got, tc.want, tc.now.Format("Mon 15:04"))
			}
		})
	}
}

// TestSummaryScheduler_Tick_NilStateNoOp asserts that a nil state snapshot
// does not panic.
func TestSummaryScheduler_Tick_NilStateNoOp(t *testing.T) {
	mgr := state.NewManager() // no Set; Get returns nil
	humans := &minimalHumanStore{humans: map[string]*store.Human{}}
	rec := &dispatchRecorder{}
	s := newScheduler(t, mgr, humans, rec.fn(), time.Now())
	s.tick()
	if calls := rec.snapshot(); len(calls) != 0 {
		t.Errorf("expected no dispatch on nil state, got %+v", calls)
	}
}
