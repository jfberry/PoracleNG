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

func TestEncounterTrackerStatChangeNoEvent(t *testing.T) {
	et := NewEncounterTracker()

	// First sight: unencountered (CP=0)
	state1 := EncounterState{
		PokemonID: 25, Form: 0, Weather: 1, CP: 0,
		DisappearTime: time.Now().Unix() + 600,
	}
	et.Track("enc1", state1)

	// Re-sight with encounter data — should NOT trigger a change event
	state2 := EncounterState{
		PokemonID: 25, Form: 0, Weather: 3, CP: 600,
		ATK: 15, DEF: 10, STA: 12, DisappearTime: time.Now().Unix() + 600,
	}
	isNew, change := et.Track("enc1", state2)

	if isNew {
		t.Error("Expected isNew=false for re-sight")
	}
	if change != nil {
		t.Error("Expected no change event for stat/weather-only changes")
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
