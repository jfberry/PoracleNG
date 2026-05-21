package main

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// recentActivityFixture builds a ProcessorService stripped down to just the
// pieces ProcessRaid / ProcessMaxbattle / ProcessInvasion / ProcessQuest
// touch up to and including the Record* call. The handlers run inside a
// goroutine, so callers must `ps.wg.Wait()` before reading the tracker.
//
// Nil state.Manager.Get() returns nil, the matcher early-returns empty,
// and the else branch in each handler logs and exits — no enricher,
// dispatcher, or renderCh required.
func recentActivityFixture(t *testing.T) *ProcessorService {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ra := tracker.NewRecentActivity()
	ps := &ProcessorService{
		cfg:              &config.Config{General: config.GeneralConfig{Locale: "en"}},
		stateMgr:         state.NewManager(), // Get() returns nil → matcher returns empty
		ctx:              ctx,
		workerPool:       make(chan struct{}, 4),
		duplicates:       tracker.NewDuplicateCache(),
		recentActivity:   ra,
		enricher:         &enrichment.Enricher{},
		raidMatcher:      &matching.RaidMatcher{},
		invasionMatcher:  &matching.InvasionMatcher{},
		questMatcher:     &matching.QuestMatcher{},
		maxbattleMatcher: &matching.MaxbattleMatcher{},
	}
	t.Cleanup(ps.duplicates.Close)
	return ps
}

func TestProcessRaidRecordsRecentActivity(t *testing.T) {
	ps := recentActivityFixture(t)

	raw, err := json.Marshal(map[string]any{
		"gym_id":     "raid-gym-1",
		"pokemon_id": 150, // Mewtwo
		"level":      5,
		"end":        9999999999,
	})
	if err != nil {
		t.Fatalf("marshal raid: %v", err)
	}

	if err := ps.ProcessRaid(raw); err != nil {
		t.Fatalf("ProcessRaid: %v", err)
	}
	ps.wg.Wait()

	active := ps.recentActivity.ActiveRaidBosses()
	if !slices.Contains(active, 150) {
		t.Fatalf("ActiveRaidBosses: want [150], got %v", active)
	}
}

func TestProcessRaidEggDoesNotRecord(t *testing.T) {
	// PokemonID == 0 (egg) — Record* silently no-ops on id<=0.
	ps := recentActivityFixture(t)

	raw, err := json.Marshal(map[string]any{
		"gym_id":     "raid-gym-egg",
		"pokemon_id": 0,
		"level":      5,
		"end":        9999999998,
	})
	if err != nil {
		t.Fatalf("marshal egg: %v", err)
	}

	if err := ps.ProcessRaid(raw); err != nil {
		t.Fatalf("ProcessRaid: %v", err)
	}
	ps.wg.Wait()

	if got := ps.recentActivity.ActiveRaidBosses(); len(got) != 0 {
		t.Fatalf("egg should record nothing, got %v", got)
	}
}

func TestProcessRaidDuplicateDoesNotDoubleRecord(t *testing.T) {
	ps := recentActivityFixture(t)

	raw, err := json.Marshal(map[string]any{
		"gym_id":     "raid-gym-dup",
		"pokemon_id": 250, // Ho-Oh
		"level":      5,
		"end":        9999999997,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for i := range 3 {
		if err := ps.ProcessRaid(raw); err != nil {
			t.Fatalf("ProcessRaid iter %d: %v", i, err)
		}
	}
	ps.wg.Wait()

	active := ps.recentActivity.ActiveRaidBosses()
	count := 0
	for _, id := range active {
		if id == 250 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("Ho-Oh should appear exactly once, got %d in %v", count, active)
	}
}

func TestProcessMaxbattleRecordsRecentActivity(t *testing.T) {
	ps := recentActivityFixture(t)

	raw, err := json.Marshal(map[string]any{
		"id":                "station-1",
		"battle_pokemon_id": 382, // Kyogre
		"battle_level":      6,
		"battle_end":        9999999996,
	})
	if err != nil {
		t.Fatalf("marshal maxbattle: %v", err)
	}

	if err := ps.ProcessMaxbattle(raw); err != nil {
		t.Fatalf("ProcessMaxbattle: %v", err)
	}
	ps.wg.Wait()

	if got := ps.recentActivity.ActiveMaxBattleBosses(); !slices.Contains(got, 382) {
		t.Fatalf("ActiveMaxBattleBosses: want [382], got %v", got)
	}
}

func TestProcessInvasionRecordsRecentActivity(t *testing.T) {
	ps := recentActivityFixture(t)

	raw, err := json.Marshal(map[string]any{
		"pokestop_id":           "stop-1",
		"incident_expiration":   9999999995,
		"incident_grunt_type":   41, // Some grunt type
		"incident_display_type": 8,
	})
	if err != nil {
		t.Fatalf("marshal invasion: %v", err)
	}

	if err := ps.ProcessInvasion(raw); err != nil {
		t.Fatalf("ProcessInvasion: %v", err)
	}
	ps.wg.Wait()

	if got := ps.recentActivity.ActiveInvasionGrunts(); !slices.Contains(got, 41) {
		t.Fatalf("ActiveInvasionGrunts: want [41], got %v", got)
	}
}

func TestProcessQuestRecordsAllRewardTypes(t *testing.T) {
	ps := recentActivityFixture(t)

	raw, err := json.Marshal(map[string]any{
		"pokestop_id": "stop-quest-1",
		"with_ar":     false,
		"rewards": []map[string]any{
			{"type": 7, "info": map[string]any{"pokemon_id": 25}},               // Pikachu encounter
			{"type": 2, "info": map[string]any{"item_id": 701, "amount": 6}},    // Razz Berry
			{"type": 4, "info": map[string]any{"pokemon_id": 132, "amount": 3}}, // Ditto candy
			{"type": 12, "info": map[string]any{"pokemon_id": 6, "amount": 50}}, // Charizard mega energy
			{"type": 3, "info": map[string]any{"amount": 500}},                  // stardust — no ID to record
		},
	})
	if err != nil {
		t.Fatalf("marshal quest: %v", err)
	}

	if err := ps.ProcessQuest(raw); err != nil {
		t.Fatalf("ProcessQuest: %v", err)
	}
	ps.wg.Wait()

	if got := ps.recentActivity.ActiveQuestPokemon(); !slices.Contains(got, 25) {
		t.Errorf("ActiveQuestPokemon: want [25], got %v", got)
	}
	if got := ps.recentActivity.ActiveQuestItems(); !slices.Contains(got, 701) {
		t.Errorf("ActiveQuestItems: want [701], got %v", got)
	}
	if got := ps.recentActivity.ActiveQuestCandy(); !slices.Contains(got, 132) {
		t.Errorf("ActiveQuestCandy: want [132], got %v", got)
	}
	if got := ps.recentActivity.ActiveQuestMega(); !slices.Contains(got, 6) {
		t.Errorf("ActiveQuestMega: want [6], got %v", got)
	}
}
