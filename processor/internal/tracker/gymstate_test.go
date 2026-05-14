package tracker

import (
	"testing"
)

// TestGymStateLastOwnerEvolution mirrors PoracleJS app.js behaviour for the
// gym last_owner field: cache stores `team || cached.last_owner_id`, the
// alerter sees the cached value from BEFORE the update.
//
// Sequence walks a gym through team change → slot change → team change to
// Uncontested → slot change while Uncontested → team change back, asserting
// the value the alerter sees and the value left in the cache after each
// event.
func TestGymStateLastOwnerEvolution(t *testing.T) {
	gst := NewGymStateTracker("")

	step := func(name string, teamID, slots int, inBattle bool, wantOldLastOwner, wantCachedLastOwner int) {
		t.Helper()
		old := gst.Update("gym1", teamID, slots, inBattle)
		gotOldLastOwner := -1
		if old != nil {
			gotOldLastOwner = old.LastOwnerID
		}
		if gotOldLastOwner != wantOldLastOwner {
			t.Errorf("%s: alerter saw lastOwnerID=%d, want %d", name, gotOldLastOwner, wantOldLastOwner)
		}
		cur := gst.gyms["gym1"]
		if cur == nil {
			t.Fatalf("%s: cache entry missing after update", name)
		}
		if cur.LastOwnerID != wantCachedLastOwner {
			t.Errorf("%s: cache lastOwnerID=%d, want %d", name, cur.LastOwnerID, wantCachedLastOwner)
		}
	}

	// First sight: gym held by Mystic. Alerter has no prior info (-1).
	// Cache stores 1 (Mystic).
	step("first sight Mystic", 1, 4, false, -1, 1)

	// Slot change while still Mystic. Alerter sees previous = 1 (Mystic).
	// Cache stays 1.
	step("Mystic slot change", 1, 5, false, 1, 1)

	// Team change to Valor. Alerter sees previous = 1 (Mystic). Cache → 2.
	step("Mystic → Valor", 2, 6, false, 1, 2)

	// Gym becomes Uncontested. Alerter sees previous = 2 (Valor).
	// Cache preserves the last actual controller (Valor=2), NOT 0.
	step("Valor → Uncontested", 0, 6, false, 2, 2)

	// Slot change while Uncontested. Alerter still sees Valor (2). Cache stays 2.
	step("Uncontested slot change", 0, 5, false, 2, 2)

	// Team takeover by Instinct. Alerter sees previous = 2 (Valor). Cache → 3.
	step("Uncontested → Instinct", 3, 6, false, 2, 3)
}

// TestGymStateLastOwnerFirstSightUncontested checks first-sight handling
// when the gym is initially seen as Uncontested. PoracleJS stores -1 in this
// case (`team || hook.last_owner_id` where both are 0/missing) so the
// alerter has nothing meaningful to render.
func TestGymStateLastOwnerFirstSightUncontested(t *testing.T) {
	gst := NewGymStateTracker("")

	// First sight: Uncontested. Alerter sees no prior. Cache should store -1
	// (no real controller has ever been observed).
	old := gst.Update("gym2", 0, 6, false)
	if old != nil {
		t.Errorf("first sight should return nil oldState, got %+v", old)
	}
	cur := gst.gyms["gym2"]
	if cur == nil {
		t.Fatal("cache entry missing")
	}
	if cur.LastOwnerID != -1 {
		t.Errorf("first-sight Uncontested cache lastOwnerID=%d, want -1", cur.LastOwnerID)
	}

	// Mystic takes over. Alerter sees previous = -1 (still no real controller
	// known when this event arrived). Cache → 1.
	old = gst.Update("gym2", 1, 5, false)
	if old == nil || old.LastOwnerID != -1 {
		t.Errorf("after Uncontested → Mystic, alerter saw lastOwnerID=%v, want -1", old)
	}
	cur = gst.gyms["gym2"]
	if cur.LastOwnerID != 1 {
		t.Errorf("Uncontested → Mystic cache lastOwnerID=%d, want 1", cur.LastOwnerID)
	}
}
