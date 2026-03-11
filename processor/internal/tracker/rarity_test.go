package tracker

import (
	"testing"
	"time"
)

func TestRarityTrackerBasic(t *testing.T) {
	rt := NewRarityTracker(24 * time.Hour)

	// Initially unknown
	group := rt.GetRarityGroup(25)
	if group != RarityUnknown {
		t.Errorf("Expected unknown rarity, got %d", group)
	}

	// Record lots of sightings
	for i := 0; i < 1000; i++ {
		rt.RecordSighting(25) // Very common
	}
	for i := 0; i < 100; i++ {
		rt.RecordSighting(150) // Uncommon
	}
	for i := 0; i < 10; i++ {
		rt.RecordSighting(151) // Rare
	}
	rt.RecordSighting(132) // Ultra rare

	// Force recalculation
	rt.recalculate()

	// Pidgey should be common
	group = rt.GetRarityGroup(25)
	if group != RarityCommon {
		t.Errorf("Expected common (1) for pokemon 25, got %d", group)
	}

	// Mewtwo should be rarer
	group = rt.GetRarityGroup(132)
	if group < RarityRare {
		t.Errorf("Expected rare or rarer for pokemon 132, got %d", group)
	}
}

func TestRarityTrackerReset(t *testing.T) {
	rt := NewRarityTracker(24 * time.Hour)

	rt.RecordSighting(25)
	rt.recalculate()

	group := rt.GetRarityGroup(25)
	if group == RarityUnknown {
		t.Error("Expected known rarity after recording")
	}

	rt.Reset()
	group = rt.GetRarityGroup(25)
	if group != RarityUnknown {
		t.Error("Expected unknown rarity after reset")
	}
}
