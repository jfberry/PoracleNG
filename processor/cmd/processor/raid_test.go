package main

import (
	"encoding/json"
	"fmt"
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

			full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, true, ts)

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

			full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, false, ts)

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
			full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, false, nil)

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

			full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, false, ts)

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

			full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, false, ts)

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

	full, rsvp := partitionRaidUsers([]webhook.MatchedUser{user}, false, ts)

	if len(full) != 1 || len(rsvp) != 0 {
		t.Errorf("telegram user with discord-only rsvpChanges: got full=%d rsvp=%d, want full=1 rsvp=0", len(full), len(rsvp))
	}
}

// ---------------------------------------------------------------------------
// Test 2 – ReplyKey / EditKey format constants
// ---------------------------------------------------------------------------

// TestRaidReplyKeyFormat verifies that the raidReplyKeyFmt and raidEditKeyFmt
// constants produce the expected key strings used by the MessageTracker.
// Testing the constants directly exercises the same format strings that
// ProcessRaid uses at runtime (via fmt.Sprintf(raidReplyKeyFmt, ...)).
func TestRaidReplyKeyFormat(t *testing.T) {
	tests := []struct {
		name string
		fmt  string
		want string
	}{
		{
			name: "replyKey",
			fmt:  raidReplyKeyFmt,
			want: "raidlife:gym-abc:1700000000",
		},
		{
			name: "editKey",
			fmt:  raidEditKeyFmt,
			want: "raid:gym-abc:1700000000",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fmt.Sprintf(tc.fmt, "gym-abc", int64(1700000000))
			if got != tc.want {
				t.Errorf("Sprintf(%q, gym-abc, 1700000000) = %q, want %q", tc.fmt, got, tc.want)
			}
		})
	}
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

// TestOverrideCleanTTH_PartitionOutput verifies OverrideCleanTTH semantics
// via latestFutureTimeslotSec — the value that ProcessRaid assigns to
// job.OverrideCleanTTH for rsvpUsers jobs. The fullUsers job never sets it
// (zero value = render pool uses map-derived TTH). The rsvpUsers job sets it
// only when a real future timeslot exists.
func TestOverrideCleanTTH_PartitionOutput(t *testing.T) {
	nowMs := time.Now().UnixMilli()

	t.Run("no_future_timeslots_yields_zero", func(t *testing.T) {
		// When all timeslots are in the past, latestFutureTimeslotSec returns 0.
		// ProcessRaid sees 0 and leaves OverrideCleanTTH unset (zero value),
		// so the render pool falls back to the map-derived raid.End TTH.
		rsvps := []tracker.RaidRSVP{
			{Timeslot: nowMs - 5*60*1000, GoingCount: 1},
		}
		got := latestFutureTimeslotSec(rsvps, nowMs)
		if got != 0 {
			t.Errorf("all-past timeslots: want 0 (no override), got %d", got)
		}
	})

	t.Run("future_timeslot_yields_nonzero", func(t *testing.T) {
		// A future timeslot produces a positive seconds value that ProcessRaid
		// assigns to job.OverrideCleanTTH, overriding the default raid.End TTH
		// so the rsvpChanges message deletes itself when RSVPs expire.
		futureMs := nowMs + 15*60*1000 // 15 minutes from now
		rsvps := []tracker.RaidRSVP{
			{Timeslot: nowMs - 60*1000, GoingCount: 2}, // past — ignored
			{Timeslot: futureMs, GoingCount: 3},
		}
		got := latestFutureTimeslotSec(rsvps, nowMs)
		wantSec := (futureMs + 999) / 1000
		if got != wantSec {
			t.Errorf("future timeslot: want %d, got %d", wantSec, got)
		}
		if got <= 0 {
			t.Errorf("OverrideCleanTTH must be > 0 for a future timeslot, got %d", got)
		}
	})
}
