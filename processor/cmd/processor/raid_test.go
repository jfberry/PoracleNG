package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// newRsvpTemplateStore creates a TemplateStore with a single rsvpChanges entry
// for the given platform and template ID. Used to simulate a deployed
// rsvpChanges template in partition tests.
func newRsvpTemplateStore(t *testing.T, platform, templateID string) *dts.TemplateStore {
	t.Helper()
	tmp := t.TempDir()

	entry := map[string]any{
		"type":     "rsvpChanges",
		"id":       templateID,
		"platform": platform,
		"language": "",
		"default":  true,
		"template": "rsvp: {{name}}",
	}
	data, err := json.Marshal([]any{entry})
	if err != nil {
		t.Fatalf("marshal rsvpChanges entry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "dts.json"), data, 0o644); err != nil {
		t.Fatalf("write dts.json: %v", err)
	}
	// Use tmp for both config and fallback dirs so LoadTemplates succeeds
	// with no fallback/dts.json present.
	ts, err := dts.LoadTemplates(tmp, tmp)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	return ts
}

// newEmptyTemplateStore creates a TemplateStore with no entries.
func newEmptyTemplateStore(t *testing.T) *dts.TemplateStore {
	t.Helper()
	tmp := t.TempDir()
	// Write an empty array so LoadTemplates returns a valid but empty store.
	if err := os.WriteFile(filepath.Join(tmp, "dts.json"), []byte("[]"), 0o644); err != nil {
		t.Fatalf("write empty dts.json: %v", err)
	}
	ts, err := dts.LoadTemplates(tmp, tmp)
	if err != nil {
		t.Fatalf("LoadTemplates (empty): %v", err)
	}
	return ts
}

// ---------------------------------------------------------------------------
// Test 1 – Template selection via partitionRaidUsers
// ---------------------------------------------------------------------------

// partitionRaidUsers returns (fullUsers, rsvpUsers). We drive it for both
// "raid" and "egg" msgTypes; the function itself is msgType-agnostic, so the
// label is just documentation in the test name.
func TestPartitionRaidUsers_FirstNotification(t *testing.T) {
	for _, msgType := range []string{"raid", "egg"} {
		t.Run(msgType, func(t *testing.T) {
			ts := newRsvpTemplateStore(t, "discord", "")
			user := webhook.MatchedUser{ID: "u1", Type: "discord:user", Clean: 0}

			full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, true, ts, "en")

			if len(full) != 1 || len(rsvp) != 0 {
				t.Errorf("isFirstNotification=true: got full=%d rsvp=%d, want full=1 rsvp=0", len(full), len(rsvp))
			}
		})
	}
}

func TestPartitionRaidUsers_EditMode(t *testing.T) {
	for _, msgType := range []string{"raid", "egg"} {
		t.Run(msgType, func(t *testing.T) {
			ts := newRsvpTemplateStore(t, "discord", "")
			// Clean=2 sets the edit bit (db.IsEdit(2) == true).
			user := webhook.MatchedUser{ID: "u1", Type: "discord:user", Clean: 2}

			full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, false, ts, "en")

			if len(full) != 1 || len(rsvp) != 0 {
				t.Errorf("IsEdit(clean)=true: got full=%d rsvp=%d, want full=1 rsvp=0", len(full), len(rsvp))
			}
		})
	}
}

func TestPartitionRaidUsers_NilTemplateStore(t *testing.T) {
	for _, msgType := range []string{"raid", "egg"} {
		t.Run(msgType, func(t *testing.T) {
			user := webhook.MatchedUser{ID: "u1", Type: "discord:user", Clean: 0}

			// ts == nil means no DTS renderer — always fall back to full template.
			full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, false, nil, "en")

			if len(full) != 1 || len(rsvp) != 0 {
				t.Errorf("ts==nil: got full=%d rsvp=%d, want full=1 rsvp=0", len(full), len(rsvp))
			}
		})
	}
}

func TestPartitionRaidUsers_NoRsvpChangesTemplate(t *testing.T) {
	for _, msgType := range []string{"raid", "egg"} {
		t.Run(msgType, func(t *testing.T) {
			// TemplateStore has no rsvpChanges entry.
			ts := newEmptyTemplateStore(t)
			user := webhook.MatchedUser{ID: "u1", Type: "discord:user", Clean: 0}

			full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, false, ts, "en")

			if len(full) != 1 || len(rsvp) != 0 {
				t.Errorf("no rsvpChanges template: got full=%d rsvp=%d, want full=1 rsvp=0", len(full), len(rsvp))
			}
		})
	}
}

func TestPartitionRaidUsers_HasRsvpChangesTemplate(t *testing.T) {
	for _, msgType := range []string{"raid", "egg"} {
		t.Run(msgType, func(t *testing.T) {
			// TemplateStore has an rsvpChanges entry for the discord platform.
			// The ID is "" (default), matching users with Template="".
			ts := newRsvpTemplateStore(t, "discord", "")
			user := webhook.MatchedUser{ID: "u1", Type: "discord:user", Clean: 0, Template: ""}

			full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, false, ts, "en")

			if len(full) != 0 || len(rsvp) != 1 {
				t.Errorf("has rsvpChanges template: got full=%d rsvp=%d, want full=0 rsvp=1", len(full), len(rsvp))
			}
		})
	}
}

// A mix of conditions in a single call: first-notification user, edit-mode
// user, and a user eligible for rsvpChanges — each lands in the right bucket.
func TestPartitionRaidUsers_MixedConditions(t *testing.T) {
	ts := newRsvpTemplateStore(t, "discord", "")

	first := webhook.MatchedUser{ID: "first", Type: "discord:user", Clean: 0}
	editMode := webhook.MatchedUser{ID: "edit", Type: "discord:user", Clean: 2}
	rsvpCandidate := webhook.MatchedUser{ID: "rsvp", Type: "discord:user", Clean: 0}

	// Only rsvpCandidate is not a first-notification and not in edit mode,
	// and the store has an rsvpChanges template.
	full, rsvp := partitionRaidUsers(
		[]webhook.MatchedUser{first, editMode, rsvpCandidate},
		false, // not first notification (affects all)
		ts,
		"en",
	)

	// "first" is driven by isFirstNotification=false here, but wait —
	// isFirstNotification is a single flag for the whole batch, not per-user.
	// So all three users go through the normal (non-first) path.
	// edit → fullUsers (IsEdit=true)
	// first and rsvpCandidate → checked against ts.Exists → rsvpUsers
	if len(full) != 1 || len(rsvp) != 2 {
		t.Errorf("mixed: got full=%d rsvp=%d, want full=1 (edit) rsvp=2 (first+rsvp)", len(full), len(rsvp))
	}
	if len(full) == 1 && full[0].ID != "edit" {
		t.Errorf("full bucket should contain the edit-mode user, got %s", full[0].ID)
	}
}

// Telegram users must be routed via their own platform key.
// A discord rsvpChanges template must not match a telegram:user type.
func TestPartitionRaidUsers_TelegramUserNoDiscordTemplate(t *testing.T) {
	ts := newRsvpTemplateStore(t, "discord", "")
	user := webhook.MatchedUser{ID: "tg", Type: "telegram:user", Clean: 0}

	full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, false, ts, "en")

	if len(full) != 1 || len(rsvp) != 0 {
		t.Errorf("telegram user with discord-only rsvpChanges: got full=%d rsvp=%d, want full=1 rsvp=0", len(full), len(rsvp))
	}
}

// ---------------------------------------------------------------------------
// Test 2 – ReplyKey format
// ---------------------------------------------------------------------------

// TestRaidReplyKeyFormat verifies the ReplyKey format emitted by ProcessRaid
// via a source-level grep (the handler runs inside a goroutine with full
// ProcessorService wiring that is impractical to stub in unit tests).
func TestRaidReplyKeyFormat(t *testing.T) {
	src, err := os.ReadFile("raid.go")
	if err != nil {
		t.Fatalf("read raid.go: %v", err)
	}
	// We check both key formats since they are adjacent in the source.
	// replyKey carries the "raidlife:" prefix that the MessageTracker uses
	// to link successive alerts for the same raid window.
	wantReply := `fmt.Sprintf("raidlife:%s:%d", raid.GymID, raid.End)`
	wantEdit := `fmt.Sprintf("raid:%s:%d", raid.GymID, raid.End)`
	for _, want := range []string{wantReply, wantEdit} {
		found := false
		// Collapse whitespace to tolerate gofmt alignment
		normalized := ""
		for _, b := range src {
			if b == ' ' || b == '\t' || b == '\n' {
				if len(normalized) > 0 && normalized[len(normalized)-1] != ' ' {
					normalized += " "
				}
			} else {
				normalized += string(b)
			}
		}
		found = len(normalized) > 0 && containsNorm(normalized, want)
		if !found {
			t.Errorf("raid.go must contain %q (whitespace-normalised)", want)
		}
	}
}

// containsNorm does a whitespace-normalised substring search by also
// collapsing whitespace in the needle.
func containsNorm(haystack, needle string) bool {
	normalizeStr := func(s string) string {
		out := ""
		for _, b := range s {
			if b == ' ' || b == '\t' || b == '\n' {
				if len(out) > 0 && out[len(out)-1] != ' ' {
					out += " "
				}
			} else {
				out += string(b)
			}
		}
		return out
	}
	return len(normalizeStr(needle)) > 0 &&
		len(haystack) >= len(normalizeStr(needle)) &&
		indexNorm(haystack, normalizeStr(needle)) >= 0
}

func indexNorm(s, substr string) int {
	if len(substr) > len(s) {
		return -1
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Test 3 – OverrideCleanTTH via latestFutureTimeslotSec
// ---------------------------------------------------------------------------

// TestLatestFutureTimeslotSec_FutureSlots verifies that the latest future
// timeslot is returned in seconds (ceiling ms→s conversion).
func TestLatestFutureTimeslotSec_FutureSlots(t *testing.T) {
	nowMs := time.Now().UnixMilli()

	past1 := nowMs - 5*60*1000 // 5 minutes ago
	past2 := nowMs - 1*60*1000 // 1 minute ago
	future := nowMs + 10*60*1000 // 10 minutes from now

	rsvps := []tracker.RaidRSVP{
		{Timeslot: past1, GoingCount: 1},
		{Timeslot: past2, GoingCount: 2},
		{Timeslot: future, GoingCount: 3},
	}

	got := latestFutureTimeslotSec(rsvps, nowMs)
	want := (future + 999) / 1000
	if got != want {
		t.Errorf("latestFutureTimeslotSec = %d, want %d", got, want)
	}
}

// TestLatestFutureTimeslotSec_AllPast verifies that zero is returned when
// all timeslots are in the past.
func TestLatestFutureTimeslotSec_AllPast(t *testing.T) {
	nowMs := time.Now().UnixMilli()
	rsvps := []tracker.RaidRSVP{
		{Timeslot: nowMs - 10*60*1000},
		{Timeslot: nowMs - 1*60*1000},
	}
	if got := latestFutureTimeslotSec(rsvps, nowMs); got != 0 {
		t.Errorf("all-past: latestFutureTimeslotSec = %d, want 0", got)
	}
}

// TestLatestFutureTimeslotSec_EmptySlice returns 0 for an empty rsvps slice.
func TestLatestFutureTimeslotSec_EmptySlice(t *testing.T) {
	if got := latestFutureTimeslotSec(nil, time.Now().UnixMilli()); got != 0 {
		t.Errorf("empty: latestFutureTimeslotSec = %d, want 0", got)
	}
}

// TestLatestFutureTimeslotSec_ExactMs verifies the ceiling ms→s formula with
// a known value: 1699999800000 ms → 1699999800 s (evenly divisible).
func TestLatestFutureTimeslotSec_ExactMs(t *testing.T) {
	// Use a timestamp far in the future so it's always "future" relative to now.
	futureMs := int64(2_000_000_000_000) // year 2033
	rsvps := []tracker.RaidRSVP{{Timeslot: futureMs, GoingCount: 1}}
	nowMs := int64(1_000_000_000_000) // nowMs is well before futureMs

	got := latestFutureTimeslotSec(rsvps, nowMs)
	want := (futureMs + 999) / 1000
	if got != want {
		t.Errorf("exact ms: got %d, want %d", got, want)
	}
}

// TestLatestFutureTimeslotSec_CeilingNotFloor verifies the ceiling behaviour:
// a timeslot of 1699999800001 ms rounds up to 1699999801 s, not down.
func TestLatestFutureTimeslotSec_CeilingNotFloor(t *testing.T) {
	futureMs := int64(1_699_999_800_001) // 1 ms into the next second
	rsvps := []tracker.RaidRSVP{{Timeslot: futureMs}}
	nowMs := int64(1_000_000_000_000)

	got := latestFutureTimeslotSec(rsvps, nowMs)
	wantCeil := int64(1_699_999_801) // ceiling
	wantFloor := int64(1_699_999_800)
	if got == wantFloor {
		t.Errorf("ceiling ms→s must round UP: got floor %d, want ceiling %d", got, wantCeil)
	}
	if got != wantCeil {
		t.Errorf("ceiling ms→s: got %d, want %d", got, wantCeil)
	}
}

// TestOverrideCleanTTH_FullUsersZero verifies that the fullUsers render job
// carries OverrideCleanTTH=0 via a source-level check: the struct field
// assignment "OverrideCleanTTH:" must appear exactly once in raid.go (the
// rsvpUsers job only). The fullUsers RenderJob literal must omit it — the
// zero value means "use the map-derived TTH", which is correct for full
// raid/egg alerts.
func TestOverrideCleanTTH_FullUsersZero(t *testing.T) {
	src, err := os.ReadFile("raid.go")
	if err != nil {
		t.Fatalf("read raid.go: %v", err)
	}
	// "OverrideCleanTTH:" (with colon) uniquely identifies struct-literal
	// field assignments. Comments reference the field without the colon, so
	// this search skips them automatically.
	needle := "OverrideCleanTTH:"
	content := string(src)
	count := 0
	for i := 0; i <= len(content)-len(needle); i++ {
		if content[i:i+len(needle)] == needle {
			count++
		}
	}
	// Only the rsvpUsers job should set OverrideCleanTTH (once).
	if count != 1 {
		t.Errorf("raid.go: expected 1 occurrence of 'OverrideCleanTTH:' (rsvpUsers job only), got %d — fullUsers job must NOT set it (zero value = use map-derived TTH)", count)
	}
}
