package tracker

import (
	"testing"
	"time"
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

func TestTrackIVNoiseIgnored(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-3", EncounterState{PokemonID: 25, CP: 1000, ATK: 10}, nil)
	_, change := et.Track("enc-3", EncounterState{PokemonID: 25, CP: 1000, ATK: 11}, nil)
	if change != nil {
		t.Fatalf("post-encounter IV change should not fire, got %+v", change)
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

// TestTrackPersistsPriorWebhook covers the new bytes-storage path: a Track
// call with raw webhook bytes followed by a change-firing Track call must
// surface those bytes on the returned EncounterChange so the change handler
// can rebuild a full {{original.X}} view.
func TestTrackPersistsPriorWebhook(t *testing.T) {
	et := NewEncounterTracker()
	priorBytes := []byte(`{"pokemon_id":25,"cp":1000,"weather":1}`)

	et.Track("enc-prior", EncounterState{PokemonID: 25, CP: 1000, Weather: 1}, priorBytes)
	_, change := et.Track("enc-prior", EncounterState{PokemonID: 25, CP: 1250, Weather: 3}, []byte(`{"pokemon_id":25,"cp":1250,"weather":3}`))
	if change == nil {
		t.Fatal("expected ChangeWeatherBoost, got nil")
	}
	if string(change.OldWebhook) != string(priorBytes) {
		t.Errorf("OldWebhook: got %s, want %s", change.OldWebhook, priorBytes)
	}
}

// TestTrackHandlesNilRawGracefully ensures legacy callers (and tests) that
// don't supply webhook bytes still get a working tracker with an empty
// OldWebhook on the change struct.
func TestTrackHandlesNilRawGracefully(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-nil", EncounterState{PokemonID: 25, Form: 0, CP: 1000}, nil)
	_, change := et.Track("enc-nil", EncounterState{PokemonID: 25, Form: 65, CP: 1000}, nil)
	if change == nil || change.Type != ChangeForm {
		t.Fatalf("expected ChangeForm, got %v", change)
	}
	if change.OldWebhook != nil {
		t.Errorf("OldWebhook should be nil when nil bytes were stored, got %v", change.OldWebhook)
	}
}
