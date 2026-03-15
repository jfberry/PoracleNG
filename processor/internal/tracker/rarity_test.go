package tracker

import (
	"testing"
)

func testStatsConfig() StatsConfig {
	return StatsConfig{
		MinSampleSize:       0, // no minimum for tests
		WindowHours:         24,
		RefreshIntervalMins: 60,
		Uncommon:            1.0,
		Rare:                0.5,
		VeryRare:            0.03,
		UltraRare:           0.01,
	}
}

func TestStatsTrackerRarity(t *testing.T) {
	st := NewStatsTracker(testStatsConfig())

	// Initially unknown
	group := st.GetRarityGroup(25)
	if group != RarityUnknown {
		t.Errorf("Expected unknown rarity, got %d", group)
	}

	// Record lots of sightings
	for i := 0; i < 1000; i++ {
		st.RecordSighting(25, false, false) // Very common
	}
	for i := 0; i < 100; i++ {
		st.RecordSighting(150, false, false) // Uncommon
	}
	for i := 0; i < 10; i++ {
		st.RecordSighting(151, false, false) // Rare
	}
	st.RecordSighting(132, false, false) // Ultra rare

	// Force recalculation
	st.recalculate()

	// Pidgey should be common
	group = st.GetRarityGroup(25)
	if group != RarityCommon {
		t.Errorf("Expected common (1) for pokemon 25, got %d", group)
	}

	// Mewtwo should be rarer
	group = st.GetRarityGroup(132)
	if group < RarityRare {
		t.Errorf("Expected rare or rarer for pokemon 132, got %d", group)
	}
}

func TestStatsTrackerShiny(t *testing.T) {
	st := NewStatsTracker(testStatsConfig())

	// Record IV-scanned encounters with some shiny
	for i := 0; i < 200; i++ {
		st.RecordSighting(25, true, i < 2) // 2 shiny out of 200
	}

	st.recalculate()

	rate := st.GetShinyRate(25)
	if rate == 0 {
		t.Error("Expected non-zero shiny rate")
	}
	// Expected ratio: 200/2 = 100
	if rate < 99 || rate > 101 {
		t.Errorf("Expected shiny rate ~100, got %f", rate)
	}

	stats := st.ExportShinyStats()
	if _, ok := stats[25]; !ok {
		t.Error("Expected pokemon 25 in shiny stats export")
	}
}

func TestStatsTrackerShinyMinEncounters(t *testing.T) {
	st := NewStatsTracker(testStatsConfig())

	// Record fewer than minIVSeenForShiny encounters
	for i := 0; i < 50; i++ {
		st.RecordSighting(25, true, i == 0) // 1 shiny out of 50
	}

	st.recalculate()

	// Should not report shiny stats with too few encounters
	rate := st.GetShinyRate(25)
	if rate != 0 {
		t.Errorf("Expected zero shiny rate with too few encounters, got %f", rate)
	}
}

func TestStatsTrackerReset(t *testing.T) {
	st := NewStatsTracker(testStatsConfig())

	st.RecordSighting(25, false, false)
	st.recalculate()

	group := st.GetRarityGroup(25)
	if group == RarityUnknown {
		t.Error("Expected known rarity after recording")
	}

	st.Reset()
	group = st.GetRarityGroup(25)
	if group != RarityUnknown {
		t.Error("Expected unknown rarity after reset")
	}
}
