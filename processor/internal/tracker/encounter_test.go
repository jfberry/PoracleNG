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

func TestEncounterTrackerChangeDetection(t *testing.T) {
	et := NewEncounterTracker()

	state1 := EncounterState{
		PokemonID: 25, Form: 0, Weather: 1, CP: 500,
		ATK: 15, DEF: 10, STA: 12, DisappearTime: time.Now().Unix() + 600,
	}
	et.Track("enc1", state1)

	state2 := EncounterState{
		PokemonID: 25, Form: 0, Weather: 3, CP: 600, // weather and CP changed
		ATK: 15, DEF: 10, STA: 12, DisappearTime: time.Now().Unix() + 600,
	}
	isNew, change := et.Track("enc1", state2)

	if isNew {
		t.Error("Expected isNew=false for re-sight")
	}
	if change == nil {
		t.Fatal("Expected change to be detected")
	}
	if change.Old.Weather != 1 || change.New.Weather != 3 {
		t.Errorf("Expected weather change 1->3, got %d->%d", change.Old.Weather, change.New.Weather)
	}
	if change.Old.CP != 500 || change.New.CP != 600 {
		t.Errorf("Expected CP change 500->600, got %d->%d", change.Old.CP, change.New.CP)
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
