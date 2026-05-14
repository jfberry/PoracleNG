package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/store"
)

func TestSummaryCommand_NoArgs(t *testing.T) {
	c := &SummaryCommand{}
	cc, _ := testCtx(t)
	cc.SummarySchedules = store.NewMockSummaryScheduleStore()

	r := c.Run(cc, nil)
	if len(r) != 1 || r[0].React != "🙅" {
		t.Fatalf("expected 🙅 reply for empty args, got %+v", r)
	}
	if !strings.Contains(r[0].Text, "summary quest") {
		t.Errorf("expected usage text mentioning `summary quest`, got %q", r[0].Text)
	}
}

func TestSummaryCommand_FeatureDisabled(t *testing.T) {
	c := &SummaryCommand{}
	cc, _ := testCtx(t)
	// SummarySchedules left nil to simulate quest_summary_enabled=false.

	r := c.Run(cc, []string{"quest"})
	if len(r) != 1 || r[0].React != "🙅" {
		t.Fatalf("expected 🙅 reply when feature disabled, got %+v", r)
	}
}

func TestSummaryCommand_UnsupportedAlertType(t *testing.T) {
	c := &SummaryCommand{}
	cc, _ := testCtx(t)
	cc.SummarySchedules = store.NewMockSummaryScheduleStore()

	r := c.Run(cc, []string{"raid"})
	if len(r) != 1 || r[0].React != "🙅" {
		t.Fatalf("expected 🙅 reply for unsupported alertType, got %+v", r)
	}
}

func TestSummaryCommand_ShowStatus_NoSchedule(t *testing.T) {
	c := &SummaryCommand{}
	cc, _ := testCtx(t)
	cc.SummarySchedules = store.NewMockSummaryScheduleStore()

	r := c.Run(cc, []string{"quest"})
	if len(r) != 1 {
		t.Fatalf("expected single reply, got %d", len(r))
	}
	if !strings.Contains(r[0].Text, "No schedule") {
		t.Errorf("expected 'No schedule' text, got %q", r[0].Text)
	}
	if !strings.Contains(r[0].Text, "No buffered") {
		t.Errorf("expected 'No buffered' text, got %q", r[0].Text)
	}
}

func TestSummaryCommand_ShowStatus_WithScheduleAndBuffer(t *testing.T) {
	c := &SummaryCommand{}
	cc, _ := testCtx(t)
	mockStore := store.NewMockSummaryScheduleStore()
	mockStore.Seed(store.SummarySchedule{
		ID:          "user1",
		AlertType:   "quest",
		ActiveHours: `[{"day":1,"hours":7,"mins":30}]`,
	})
	cc.SummarySchedules = mockStore
	cc.SummaryBufferCount = func(id, alertType string) int { return 5 }

	r := c.Run(cc, []string{"quest"})
	if len(r) != 1 {
		t.Fatalf("expected single reply, got %d", len(r))
	}
	if !strings.Contains(r[0].Text, "Schedule for quest") {
		t.Errorf("expected 'Schedule for quest' text, got %q", r[0].Text)
	}
	if !strings.Contains(r[0].Text, "07:30") {
		t.Errorf("expected '07:30' rendering, got %q", r[0].Text)
	}
	if !strings.Contains(r[0].Text, "5 buffered") {
		t.Errorf("expected buffered count text, got %q", r[0].Text)
	}
}

func TestSummaryCommand_SetTime_StoresJSON(t *testing.T) {
	c := &SummaryCommand{}
	cc, _ := testCtx(t)
	mockStore := store.NewMockSummaryScheduleStore()
	cc.SummarySchedules = mockStore

	r := c.Run(cc, []string{"quest", "settime", "mon07:30", "fri22:00"})
	if len(r) != 1 || r[0].React != "✅" {
		t.Fatalf("expected ✅ reply, got %+v", r)
	}

	got, err := mockStore.Get("user1", "quest")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected schedule to be stored")
	}

	var entries []map[string]any
	if err := json.Unmarshal([]byte(got.ActiveHours), &entries); err != nil {
		t.Fatalf("stored active_hours not valid JSON: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d: %s", len(entries), got.ActiveHours)
	}
	// Verify mon → day 1 and fri → day 5. Hours/Mins are now written
	// as JSON numbers (matching the on-disk ActiveHourEntry int
	// fields); ParseActiveHours's flexToInt still accepts the legacy
	// string-encoded form for back-compat on read.
	if entries[0]["day"] != float64(1) {
		t.Errorf("first entry day=%v, want 1", entries[0]["day"])
	}
	if entries[0]["hours"] != float64(7) {
		t.Errorf("first entry hours=%v, want 7", entries[0]["hours"])
	}
	if entries[1]["day"] != float64(5) {
		t.Errorf("second entry day=%v, want 5", entries[1]["day"])
	}
}

func TestSummaryCommand_SetTime_Weekday(t *testing.T) {
	c := &SummaryCommand{}
	cc, _ := testCtx(t)
	mockStore := store.NewMockSummaryScheduleStore()
	cc.SummarySchedules = mockStore

	r := c.Run(cc, []string{"quest", "settime", "weekday07:30"})
	if r[0].React != "✅" {
		t.Fatalf("expected ✅ reply, got %+v", r)
	}
	got, _ := mockStore.Get("user1", "quest")
	var entries []map[string]any
	_ = json.Unmarshal([]byte(got.ActiveHours), &entries)
	if len(entries) != 5 {
		t.Errorf("weekday should expand to 5 entries, got %d", len(entries))
	}
}

func TestSummaryCommand_SetTime_NoMatches(t *testing.T) {
	c := &SummaryCommand{}
	cc, _ := testCtx(t)
	mockStore := store.NewMockSummaryScheduleStore()
	cc.SummarySchedules = mockStore

	r := c.Run(cc, []string{"quest", "settime", "garbage"})
	if r[0].React != "🙅" {
		t.Fatalf("expected 🙅 reply for unparseable settime, got %+v", r)
	}
}

func TestSummaryCommand_ClearTime(t *testing.T) {
	c := &SummaryCommand{}
	cc, _ := testCtx(t)
	mockStore := store.NewMockSummaryScheduleStore()
	mockStore.Seed(store.SummarySchedule{
		ID:          "user1",
		AlertType:   "quest",
		ActiveHours: `[{"day":1,"hours":7,"mins":30}]`,
	})
	cc.SummarySchedules = mockStore

	r := c.Run(cc, []string{"quest", "cleartime"})
	if r[0].React != "✅" {
		t.Fatalf("expected ✅ reply, got %+v", r)
	}
	got, _ := mockStore.Get("user1", "quest")
	if got != nil {
		t.Errorf("expected schedule deleted, still present: %+v", got)
	}
	// Verify Delete was called
	found := false
	for _, m := range mockStore.Calls {
		if m == "Delete" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Delete call, got: %v", mockStore.Calls)
	}
}

func TestSummaryCommand_Now_EmptyBuffer(t *testing.T) {
	c := &SummaryCommand{}
	cc, _ := testCtx(t)
	cc.SummarySchedules = store.NewMockSummaryScheduleStore()
	cc.SummaryBufferCount = func(id, alertType string) int { return 0 }

	dispatched := 0
	cc.SummaryDispatch = func(id, alertType string) { dispatched++ }

	r := c.Run(cc, []string{"quest", "now"})
	if r[0].React != "👌" {
		t.Fatalf("expected 👌 reply for empty buffer, got %+v", r)
	}
	if dispatched != 0 {
		t.Errorf("expected dispatch NOT to fire on empty buffer, fired %d times", dispatched)
	}
}

func TestSummaryCommand_Now_FiresDispatch(t *testing.T) {
	c := &SummaryCommand{}
	cc, _ := testCtx(t)
	cc.SummarySchedules = store.NewMockSummaryScheduleStore()
	cc.SummaryBufferCount = func(id, alertType string) int { return 3 }

	var lastID, lastType string
	dispatched := 0
	cc.SummaryDispatch = func(id, alertType string) {
		dispatched++
		lastID = id
		lastType = alertType
	}

	r := c.Run(cc, []string{"quest", "now"})
	if r[0].React != "✅" {
		t.Fatalf("expected ✅ reply, got %+v", r)
	}
	if dispatched != 1 {
		t.Errorf("expected dispatch to fire once, fired %d times", dispatched)
	}
	if lastID != "user1" || lastType != "quest" {
		t.Errorf("dispatch called with id=%s type=%s, want user1/quest", lastID, lastType)
	}
}

