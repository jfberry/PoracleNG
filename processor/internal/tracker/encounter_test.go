package tracker

import (
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func TestEncounterTrackerFirstSight(t *testing.T) {
	et := NewEncounterTracker()

	state := EncounterState{
		PokemonID: 25, Form: 0, Weather: 1, CP: 500,
		ATK: 15, DEF: 10, STA: 12, DisappearTime: time.Now().Unix() + 600,
	}

	isNew, change := et.Track("enc1", state, nil)
	if !isNew {
		t.Error("Expected isNew=true for first sighting")
	}
	if change != nil {
		t.Error("Expected no change for first sighting")
	}
}

func TestEncounterTrackerDuplicate(t *testing.T) {
	et := NewEncounterTracker()

	state := EncounterState{
		PokemonID: 25, Form: 0, Weather: 1, CP: 500,
		ATK: 15, DEF: 10, STA: 12, DisappearTime: time.Now().Unix() + 600,
	}

	et.Track("enc1", state, nil)
	isNew, change := et.Track("enc1", state, nil)

	if isNew {
		t.Error("Expected isNew=false for duplicate")
	}
	if change != nil {
		t.Error("Expected no change for identical re-sight")
	}
}

func TestEncounterTrackerFormChange(t *testing.T) {
	et := NewEncounterTracker()

	state1 := EncounterState{
		PokemonID: 25, Form: 0, Weather: 0, CP: 500,
		DisappearTime: time.Now().Unix() + 600,
	}
	et.Track("enc1", state1, nil)

	// Form change should trigger
	state2 := EncounterState{
		PokemonID: 25, Form: 1, Weather: 0, CP: 500,
		DisappearTime: time.Now().Unix() + 600,
	}
	_, change := et.Track("enc1", state2, nil)

	if change == nil {
		t.Fatal("Expected form change to be detected")
	}
	if change.Old.Form != 0 || change.New.Form != 1 {
		t.Errorf("Expected form change 0->1, got %d->%d", change.Old.Form, change.New.Form)
	}
}

func TestEncounterTrackerPokemonIDChange(t *testing.T) {
	et := NewEncounterTracker()

	state1 := EncounterState{
		PokemonID: 132, Form: 0, Weather: 0, CP: 300,
		DisappearTime: time.Now().Unix() + 600,
	}
	et.Track("enc1", state1, nil)

	state2 := EncounterState{
		PokemonID: 25, Form: 0, Weather: 0, CP: 500,
		ATK: 15, DEF: 10, STA: 12, DisappearTime: time.Now().Unix() + 600,
	}
	_, change := et.Track("enc1", state2, nil)

	if change == nil {
		t.Fatal("Expected pokemon ID change to be detected")
	}
	if change.Old.PokemonID != 132 || change.New.PokemonID != 25 {
		t.Errorf("Expected pokemon change 132->25, got %d->%d", change.Old.PokemonID, change.New.PokemonID)
	}
}

func TestTrackDetectsEncountered(t *testing.T) {
	et := NewEncounterTracker()

	first := EncounterState{PokemonID: 1, Form: 0, CP: 0}
	if isNew, change := et.Track("enc-1", first, nil); !isNew || change != nil {
		t.Fatalf("first sighting: isNew=%v change=%v", isNew, change)
	}

	encountered := EncounterState{PokemonID: 1, Form: 0, CP: 1500, ATK: 15, DEF: 14, STA: 13}
	isNew, change := et.Track("enc-1", encountered, nil)
	if isNew {
		t.Fatal("expected isNew=false on update")
	}
	if change == nil {
		t.Fatal("expected change for non-IV → IV transition")
	}
	if change.Type != ChangeEncountered {
		t.Errorf("change.Type = %v, want ChangeEncountered", change.Type)
	}
	if change.Old.CP != 0 || change.New.CP != 1500 {
		t.Errorf("CP not propagated: old=%d new=%d", change.Old.CP, change.New.CP)
	}
}

func TestTrackDetectsGenderChange(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-2", EncounterState{PokemonID: 25, Gender: 1}, nil)
	_, change := et.Track("enc-2", EncounterState{PokemonID: 25, Gender: 2}, nil)
	if change == nil || change.Type != ChangeGender {
		t.Fatalf("expected ChangeGender, got %v", change)
	}
}

// Raw IV drift between two encountered webhooks fires ChangeStats.
// Golbat re-reports the same encounter with different IVs under the
// A/B scanner anomaly — the user's filter may reject the new reading,
// so prior recipients need a monsterChanged follow-up.
func TestTrackDetectsStatsDrift(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-3", EncounterState{PokemonID: 25, CP: 1000, ATK: 15, DEF: 15, STA: 15}, nil)
	_, change := et.Track("enc-3", EncounterState{PokemonID: 25, CP: 1000, ATK: 10, DEF: 10, STA: 10}, nil)
	if change == nil || change.Type != ChangeStats {
		t.Fatalf("expected ChangeStats for raw IV drift, got %+v", change)
	}
	if change.Old.ATK != 15 || change.New.ATK != 10 {
		t.Errorf("ATK delta not propagated: old=%d new=%d", change.Old.ATK, change.New.ATK)
	}
}

// Stats drift on a pre-encounter sighting (CP=0 → CP>0) is the
// "encountered" transition, not stats drift. ChangeEncountered wins.
func TestTrackStatsDriftIgnoredOnEncounterTransition(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-3b", EncounterState{PokemonID: 25, CP: 0, ATK: 0, DEF: 0, STA: 0}, nil)
	_, change := et.Track("enc-3b", EncounterState{PokemonID: 25, CP: 1000, ATK: 15, DEF: 15, STA: 15}, nil)
	if change == nil || change.Type != ChangeEncountered {
		t.Fatalf("expected ChangeEncountered (not ChangeStats), got %+v", change)
	}
}

// No diff in monitored fields → no change.
func TestTrackNoChangeOnIdenticalState(t *testing.T) {
	et := NewEncounterTracker()
	state := EncounterState{PokemonID: 25, CP: 1000, ATK: 15, DEF: 15, STA: 15, Weather: 1}
	et.Track("enc-3c", state, nil)
	_, change := et.Track("enc-3c", state, nil)
	if change != nil {
		t.Fatalf("identical-state re-Track should not fire, got %+v", change)
	}
}

// Wild re-scan after a weather shift must not downgrade the
// encountered state. Real webhook sequence from production:
//
//   W1 wild      CP=0    weather=0  → first sighting
//   W2 encounter CP=153  weather=0  IVs=15/15/15  → ChangeEncountered
//   W3 wild      CP=0    weather=4  → must NOT overwrite state
//   W4 encounter CP=280  weather=4  IVs=13/13/12  → must fire
//                                                   ChangeWeatherBoost
//                                                   against W2's stats
//
// Without the wild-rescan guard, W3 zeroed prev.state.CP and W4 then
// triggered a second ChangeEncountered, which the dispatcher skips
// for prior-only users — silently dropping the follow-up alert.
func TestTrackWildRescanDoesNotDowngradeEncounteredState(t *testing.T) {
	et := NewEncounterTracker()

	w1 := EncounterState{PokemonID: 23, Form: 697, Gender: 2, CP: 0, Weather: 0}
	w2 := EncounterState{PokemonID: 23, Form: 697, Gender: 2, CP: 153, Weather: 0, ATK: 15, DEF: 15, STA: 15}
	w3 := EncounterState{PokemonID: 23, Form: 697, Gender: 2, CP: 0, Weather: 4}
	w4 := EncounterState{PokemonID: 23, Form: 697, Gender: 2, CP: 280, Weather: 4, ATK: 13, DEF: 13, STA: 12}

	et.Track("enc-prod", w1, nil)

	_, change2 := et.Track("enc-prod", w2, nil)
	if change2 == nil || change2.Type != ChangeEncountered {
		t.Fatalf("W2 should fire ChangeEncountered, got %+v", change2)
	}

	_, change3 := et.Track("enc-prod", w3, nil)
	if change3 != nil {
		t.Fatalf("W3 (wild re-scan) should not fire a change, got %+v", change3)
	}

	_, change4 := et.Track("enc-prod", w4, nil)
	if change4 == nil {
		t.Fatalf("W4 must fire a change against W2's preserved state, got nil")
	}
	if change4.Type != ChangeWeatherBoost {
		t.Errorf("W4 change type: got %v, want ChangeWeatherBoost", change4.Type)
	}
	// And the diff must reflect W2's stats, not W3's zeroed state.
	if change4.Old.CP != 153 || change4.Old.ATK != 15 {
		t.Errorf("W4 change.Old must reflect W2's encountered state, got CP=%d ATK=%d (W3 would have CP=0 ATK=0)", change4.Old.CP, change4.Old.ATK)
	}
}

func TestTrackDetectsWeatherBoost(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-4", EncounterState{PokemonID: 25, CP: 1000, Weather: 1}, nil)
	_, change := et.Track("enc-4", EncounterState{PokemonID: 25, CP: 1250, Weather: 3}, nil)
	if change == nil || change.Type != ChangeWeatherBoost {
		t.Fatalf("expected ChangeWeatherBoost, got %+v", change)
	}
	if change.Old.CP != 1000 || change.New.CP != 1250 {
		t.Errorf("CP delta not propagated")
	}
}

func TestTrackWeatherChangeWithoutCPChangeIgnored(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-5", EncounterState{PokemonID: 25, CP: 1000, Weather: 1}, nil)
	_, change := et.Track("enc-5", EncounterState{PokemonID: 25, CP: 1000, Weather: 3}, nil)
	if change != nil {
		t.Fatalf("weather-only change with no stat impact should be silent, got %+v", change)
	}
}

// TestTrackPersistsPriorWebhook covers the struct-storage path: a Track
// call with a parsed PokemonWebhook followed by a change-firing Track call
// must surface that struct on the returned EncounterChange so the change
// handler can rebuild a full {{original.X}} view without re-parsing JSON.
func TestTrackPersistsPriorWebhook(t *testing.T) {
	et := NewEncounterTracker()
	prior := &webhook.PokemonWebhook{PokemonID: 25, CP: 1000, Weather: 1}

	et.Track("enc-prior", EncounterState{PokemonID: 25, CP: 1000, Weather: 1}, prior)
	_, change := et.Track("enc-prior", EncounterState{PokemonID: 25, CP: 1250, Weather: 3}, &webhook.PokemonWebhook{PokemonID: 25, CP: 1250, Weather: 3})
	if change == nil {
		t.Fatal("expected ChangeWeatherBoost, got nil")
	}
	if change.OldWebhook != prior {
		t.Errorf("OldWebhook should point to the originally-stored struct")
	}
	if change.OldWebhook.CP != 1000 || change.OldWebhook.Weather != 1 {
		t.Errorf("OldWebhook fields wrong: got CP=%d Weather=%d", change.OldWebhook.CP, change.OldWebhook.Weather)
	}
}

// TestTrackHandlesNilWebhookGracefully ensures legacy callers (and tests) that
// don't supply a webhook struct still get a working tracker with a nil
// OldWebhook on the change struct.
func TestTrackHandlesNilWebhookGracefully(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-nil", EncounterState{PokemonID: 25, Form: 0, CP: 1000}, nil)
	_, change := et.Track("enc-nil", EncounterState{PokemonID: 25, Form: 65, CP: 1000}, nil)
	if change == nil || change.Type != ChangeForm {
		t.Fatalf("expected ChangeForm, got %v", change)
	}
	if change.OldWebhook != nil {
		t.Errorf("OldWebhook should be nil when nil was stored, got %v", change.OldWebhook)
	}
}
