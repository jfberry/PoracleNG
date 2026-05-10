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

	isNew, change := et.Track("enc1", state)
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

	et.Track("enc1", state)
	isNew, change := et.Track("enc1", state)

	if isNew {
		t.Error("Expected isNew=false for duplicate")
	}
	if change != nil {
		t.Error("Expected no change for identical re-sight")
	}
}

func TestEncounterTrackerStatChangeFiresEncountered(t *testing.T) {
	et := NewEncounterTracker()

	// First sight: unencountered (CP=0)
	state1 := EncounterState{
		PokemonID: 25, Form: 0, Weather: 1, CP: 0,
		DisappearTime: time.Now().Unix() + 600,
	}
	et.Track("enc1", state1)

	// Re-sight with encounter data — should fire ChangeEncountered (non-IV → IV transition)
	state2 := EncounterState{
		PokemonID: 25, Form: 0, Weather: 3, CP: 600,
		ATK: 15, DEF: 10, STA: 12, DisappearTime: time.Now().Unix() + 600,
	}
	isNew, change := et.Track("enc1", state2)

	if isNew {
		t.Error("Expected isNew=false for re-sight")
	}
	if change == nil {
		t.Fatal("Expected ChangeEncountered for non-IV → IV transition")
	}
	if change.Type != ChangeEncountered {
		t.Errorf("change.Type = %v, want ChangeEncountered", change.Type)
	}
}

func TestEncounterTrackerFormChange(t *testing.T) {
	et := NewEncounterTracker()

	state1 := EncounterState{
		PokemonID: 25, Form: 0, Weather: 0, CP: 500,
		DisappearTime: time.Now().Unix() + 600,
	}
	et.Track("enc1", state1)

	// Form change should trigger
	state2 := EncounterState{
		PokemonID: 25, Form: 1, Weather: 0, CP: 500,
		DisappearTime: time.Now().Unix() + 600,
	}
	_, change := et.Track("enc1", state2)

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
	et.Track("enc1", state1)

	state2 := EncounterState{
		PokemonID: 25, Form: 0, Weather: 0, CP: 500,
		ATK: 15, DEF: 10, STA: 12, DisappearTime: time.Now().Unix() + 600,
	}
	_, change := et.Track("enc1", state2)

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
	if isNew, change := et.Track("enc-1", first); !isNew || change != nil {
		t.Fatalf("first sighting: isNew=%v change=%v", isNew, change)
	}

	encountered := EncounterState{PokemonID: 1, Form: 0, CP: 1500, ATK: 15, DEF: 14, STA: 13}
	isNew, change := et.Track("enc-1", encountered)
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
	et.Track("enc-2", EncounterState{PokemonID: 25, Gender: 1})
	_, change := et.Track("enc-2", EncounterState{PokemonID: 25, Gender: 2})
	if change == nil || change.Type != ChangeGender {
		t.Fatalf("expected ChangeGender, got %v", change)
	}
}

func TestTrackIVNoiseIgnored(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-3", EncounterState{PokemonID: 25, CP: 1000, ATK: 10})
	_, change := et.Track("enc-3", EncounterState{PokemonID: 25, CP: 1000, ATK: 11})
	if change != nil {
		t.Fatalf("post-encounter IV change should not fire, got %+v", change)
	}
}

func TestTrackDetectsWeatherBoost(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-4", EncounterState{PokemonID: 25, CP: 1000, Weather: 1, ATK: 15})
	_, change := et.Track("enc-4", EncounterState{PokemonID: 25, CP: 1250, Weather: 3, ATK: 15})
	if change == nil || change.Type != ChangeWeatherBoost {
		t.Fatalf("expected ChangeWeatherBoost, got %+v", change)
	}
	if change.Old.CP != 1000 || change.New.CP != 1250 {
		t.Errorf("CP delta not propagated")
	}
}

func TestTrackWeatherChangeWithoutCPChangeIgnored(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-5", EncounterState{PokemonID: 25, CP: 1000, Weather: 1})
	_, change := et.Track("enc-5", EncounterState{PokemonID: 25, CP: 1000, Weather: 3})
	if change != nil {
		t.Fatalf("weather-only change with no stat impact should be silent, got %+v", change)
	}
}
