package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
)

func makeQuestTestState(quests []*db.QuestTracking, humans map[string]*db.Human) *state.State {
	fences := []geofence.Fence{
		{
			Name:             "TestArea",
			DisplayInMatches: true,
			Path: [][2]float64{
				{50.0, -1.0},
				{52.0, -1.0},
				{52.0, 2.0},
				{50.0, 2.0},
			},
		},
	}
	si := geofence.NewSpatialIndex(fences)

	return &state.State{
		Humans:   humans,
		Quests:   quests,
		Geofence: si,
		Fences:   fences,
	}
}

// --- singleRewardMatches unit tests ---

func TestSingleRewardMatchesPokemon(t *testing.T) {
	tests := []struct {
		name     string
		tracking *db.QuestTracking
		reward   *QuestRewardData
		expected bool
	}{
		{
			"exact pokemon match",
			&db.QuestTracking{RewardType: 7, Reward: 25},
			&QuestRewardData{Type: 7, PokemonID: 25},
			true,
		},
		{
			"wrong pokemon",
			&db.QuestTracking{RewardType: 7, Reward: 25},
			&QuestRewardData{Type: 7, PokemonID: 26},
			false,
		},
		{
			"wrong reward type",
			&db.QuestTracking{RewardType: 7, Reward: 25},
			&QuestRewardData{Type: 3, Amount: 500},
			false,
		},
		{
			"form filter matches",
			&db.QuestTracking{RewardType: 7, Reward: 25, Form: 598},
			&QuestRewardData{Type: 7, PokemonID: 25, FormID: 598},
			true,
		},
		{
			"form filter wrong form",
			&db.QuestTracking{RewardType: 7, Reward: 25, Form: 598},
			&QuestRewardData{Type: 7, PokemonID: 25, FormID: 181},
			false,
		},
		{
			"form=0 matches any form",
			&db.QuestTracking{RewardType: 7, Reward: 25, Form: 0},
			&QuestRewardData{Type: 7, PokemonID: 25, FormID: 598},
			true,
		},
		{
			"shiny required but not shiny",
			&db.QuestTracking{RewardType: 7, Reward: 25, Shiny: true},
			&QuestRewardData{Type: 7, PokemonID: 25, Shiny: false},
			false,
		},
		{
			"shiny required and shiny",
			&db.QuestTracking{RewardType: 7, Reward: 25, Shiny: true},
			&QuestRewardData{Type: 7, PokemonID: 25, Shiny: true},
			true,
		},
		{
			"not tracking shiny, reward is shiny - should match",
			&db.QuestTracking{RewardType: 7, Reward: 25, Shiny: false},
			&QuestRewardData{Type: 7, PokemonID: 25, Shiny: true},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := singleRewardMatches(tt.tracking, tt.reward)
			if got != tt.expected {
				t.Errorf("singleRewardMatches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSingleRewardMatchesItem(t *testing.T) {
	tests := []struct {
		name     string
		tracking *db.QuestTracking
		reward   *QuestRewardData
		expected bool
	}{
		{
			"exact item match",
			&db.QuestTracking{RewardType: 2, Reward: 2, Amount: 5},
			&QuestRewardData{Type: 2, ItemID: 2, Amount: 5},
			true,
		},
		{
			"item amount exceeds minimum",
			&db.QuestTracking{RewardType: 2, Reward: 2, Amount: 5},
			&QuestRewardData{Type: 2, ItemID: 2, Amount: 10},
			true,
		},
		{
			"item amount below minimum",
			&db.QuestTracking{RewardType: 2, Reward: 2, Amount: 5},
			&QuestRewardData{Type: 2, ItemID: 2, Amount: 3},
			false,
		},
		{
			"wrong item_id",
			&db.QuestTracking{RewardType: 2, Reward: 2},
			&QuestRewardData{Type: 2, ItemID: 701},
			false,
		},
		{
			"amount=0 tracking matches any amount",
			&db.QuestTracking{RewardType: 2, Reward: 701, Amount: 0},
			&QuestRewardData{Type: 2, ItemID: 701, Amount: 3},
			true,
		},
		// Real webhook: item_id=2 (Potion), amount=5
		{
			"real webhook item potion",
			&db.QuestTracking{RewardType: 2, Reward: 2, Amount: 3},
			&QuestRewardData{Type: 2, ItemID: 2, Amount: 5},
			true,
		},
		// Real webhook: item_id=701 (Golden Razz?), amount=3
		{
			"real webhook item 701",
			&db.QuestTracking{RewardType: 2, Reward: 701},
			&QuestRewardData{Type: 2, ItemID: 701, Amount: 3},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := singleRewardMatches(tt.tracking, tt.reward)
			if got != tt.expected {
				t.Errorf("singleRewardMatches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSingleRewardMatchesStardust(t *testing.T) {
	tests := []struct {
		name     string
		tracking *db.QuestTracking
		reward   *QuestRewardData
		expected bool
	}{
		{
			"stardust exact match",
			&db.QuestTracking{RewardType: 3, Reward: 200},
			&QuestRewardData{Type: 3, Amount: 200},
			true,
		},
		{
			"stardust exceeds minimum",
			&db.QuestTracking{RewardType: 3, Reward: 100},
			&QuestRewardData{Type: 3, Amount: 200},
			true,
		},
		{
			"stardust below minimum",
			&db.QuestTracking{RewardType: 3, Reward: 500},
			&QuestRewardData{Type: 3, Amount: 200},
			false,
		},
		{
			"stardust reward=0 matches any amount",
			&db.QuestTracking{RewardType: 3, Reward: 0},
			&QuestRewardData{Type: 3, Amount: 200},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := singleRewardMatches(tt.tracking, tt.reward)
			if got != tt.expected {
				t.Errorf("singleRewardMatches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSingleRewardMatchesMegaEnergy(t *testing.T) {
	tests := []struct {
		name     string
		tracking *db.QuestTracking
		reward   *QuestRewardData
		expected bool
	}{
		// Real webhook: pokemon_id=306 (Aggron), amount=10
		{
			"real webhook mega energy aggron",
			&db.QuestTracking{RewardType: 12, Reward: 306, Amount: 10},
			&QuestRewardData{Type: 12, PokemonID: 306, Amount: 10},
			true,
		},
		{
			"mega energy specific pokemon",
			&db.QuestTracking{RewardType: 12, Reward: 6, Amount: 50},
			&QuestRewardData{Type: 12, PokemonID: 6, Amount: 100},
			true,
		},
		{
			"mega energy wrong pokemon",
			&db.QuestTracking{RewardType: 12, Reward: 6, Amount: 50},
			&QuestRewardData{Type: 12, PokemonID: 18, Amount: 100},
			false,
		},
		{
			"mega energy insufficient amount",
			&db.QuestTracking{RewardType: 12, Reward: 6, Amount: 50},
			&QuestRewardData{Type: 12, PokemonID: 6, Amount: 10},
			false,
		},
		{
			"mega energy any pokemon (reward=0)",
			&db.QuestTracking{RewardType: 12, Reward: 0},
			&QuestRewardData{Type: 12, PokemonID: 254, Amount: 10},
			true,
		},
		{
			"mega energy any amount (amount=0)",
			&db.QuestTracking{RewardType: 12, Reward: 306, Amount: 0},
			&QuestRewardData{Type: 12, PokemonID: 306, Amount: 10},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := singleRewardMatches(tt.tracking, tt.reward)
			if got != tt.expected {
				t.Errorf("singleRewardMatches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSingleRewardMatchesCandy(t *testing.T) {
	tests := []struct {
		name     string
		tracking *db.QuestTracking
		reward   *QuestRewardData
		expected bool
	}{
		{
			"candy specific pokemon enough",
			&db.QuestTracking{RewardType: 4, Reward: 25, Amount: 3},
			&QuestRewardData{Type: 4, PokemonID: 25, Amount: 5},
			true,
		},
		{
			"candy specific pokemon insufficient",
			&db.QuestTracking{RewardType: 4, Reward: 25, Amount: 3},
			&QuestRewardData{Type: 4, PokemonID: 25, Amount: 1},
			false,
		},
		{
			"candy wrong pokemon",
			&db.QuestTracking{RewardType: 4, Reward: 25, Amount: 3},
			&QuestRewardData{Type: 4, PokemonID: 26, Amount: 5},
			false,
		},
		{
			"candy any pokemon (reward=0)",
			&db.QuestTracking{RewardType: 4, Reward: 0},
			&QuestRewardData{Type: 4, PokemonID: 150, Amount: 3},
			true,
		},
		{
			"candy any amount (amount=0)",
			&db.QuestTracking{RewardType: 4, Reward: 25, Amount: 0},
			&QuestRewardData{Type: 4, PokemonID: 25, Amount: 1},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := singleRewardMatches(tt.tracking, tt.reward)
			if got != tt.expected {
				t.Errorf("singleRewardMatches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSingleRewardMatchesUnknownType(t *testing.T) {
	// Unknown reward type should not match
	tracking := &db.QuestTracking{RewardType: 99, Reward: 1}
	reward := &QuestRewardData{Type: 99, PokemonID: 1}
	if singleRewardMatches(tracking, reward) {
		t.Error("Unknown reward type should not match")
	}
}

// --- questRewardMatches tests ---

func TestQuestRewardMatchesMultipleRewards(t *testing.T) {
	tracking := &db.QuestTracking{RewardType: 7, Reward: 7}

	rewards := []QuestRewardData{
		{Type: 3, Amount: 200},          // stardust - no match
		{Type: 2, ItemID: 2, Amount: 5}, // item - no match
		{Type: 7, PokemonID: 7},         // pokemon - match!
	}

	if !questRewardMatches(tracking, rewards) {
		t.Error("Expected match when one of multiple rewards matches")
	}
}

func TestQuestRewardMatchesNoRewards(t *testing.T) {
	tracking := &db.QuestTracking{RewardType: 7, Reward: 25}

	if questRewardMatches(tracking, nil) {
		t.Error("Expected no match with nil rewards")
	}
	if questRewardMatches(tracking, []QuestRewardData{}) {
		t.Error("Expected no match with empty rewards")
	}
}

func TestQuestRewardMatchesNoneMatch(t *testing.T) {
	tracking := &db.QuestTracking{RewardType: 7, Reward: 25}

	rewards := []QuestRewardData{
		{Type: 3, Amount: 200},
		{Type: 2, ItemID: 2, Amount: 5},
	}

	if questRewardMatches(tracking, rewards) {
		t.Error("Expected no match when no rewards match")
	}
}

// --- Full matcher integration tests with real webhook data ---

// Real: Squirtle encounter quest (pokemon_id=7, form_id=181)
func TestQuestRealSquirtleEncounter(t *testing.T) {
	human := makeHuman("user1")
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 7, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "9c151c82f0a3416e8b970d4a8a2e6291.16",
		Latitude:   51.282747, Longitude: 1.063537,
		Rewards: []QuestRewardData{
			{Type: 7, PokemonID: 7, FormID: 181},
		},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for Squirtle quest, got %d", len(matched))
	}
}

// Real: Squirtle with specific form tracking
func TestQuestRealSquirtleFormFilter(t *testing.T) {
	human := makeHuman("user1")

	// Tracking form 181 specifically
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 7, Form: 181, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "stop1",
		Latitude:   51.282747, Longitude: 1.063537,
		Rewards: []QuestRewardData{{Type: 7, PokemonID: 7, FormID: 181}},
	}
	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct form 181, got %d", len(matched))
	}

	// Different form from webhook (172 = Charmander form)
	data.Rewards[0].FormID = 172
	matched = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for form 172 when tracking form 181, got %d", len(matched))
	}
}

// Real: Potion (item_id=2, amount=5) from catch water quest
func TestQuestRealItemPotion(t *testing.T) {
	human := makeHuman("user1")
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 2, Reward: 2, Amount: 3, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "1ca60ca952e94fda9445decbd8a02441.11",
		Latitude:   51.297267, Longitude: 1.069734,
		Rewards: []QuestRewardData{{Type: 2, ItemID: 2, Amount: 5}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for potion quest, got %d", len(matched))
	}
}

// Real: Mystery item (item_id=701, amount=3)
func TestQuestRealItem701(t *testing.T) {
	human := makeHuman("user1")
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 2, Reward: 701, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "43ac0e4d4c5a422eb31ff5ce120fbabc.16",
		Latitude:   51.242691, Longitude: 1.123545,
		Rewards: []QuestRewardData{{Type: 2, ItemID: 701, Amount: 3}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for item 701 quest, got %d", len(matched))
	}
}

// Real: 200 stardust from catch quest
func TestQuestRealStardust(t *testing.T) {
	human := makeHuman("user1")
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 3, Reward: 100, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "e7d3f864bd434660bc10a836b92b1204.16",
		Latitude:   51.311853, Longitude: 1.193484,
		Rewards: []QuestRewardData{{Type: 3, Amount: 200}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for stardust quest, got %d", len(matched))
	}
}

// Real: stardust tracking threshold higher than reward
func TestQuestRealStardustBelowThreshold(t *testing.T) {
	human := makeHuman("user1")
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 3, Reward: 500, Template: "1"}, // wants 500+
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "e7d3f864bd434660bc10a836b92b1204.16",
		Latitude:   51.311853, Longitude: 1.193484,
		Rewards: []QuestRewardData{{Type: 3, Amount: 200}}, // only 200
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for stardust below threshold, got %d", len(matched))
	}
}

// Real: Archen encounter (pokemon_id=566) from win raid quest
func TestQuestRealArchenEncounter(t *testing.T) {
	human := makeHuman("user1")
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 566, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "69399e05c98145b4b59802f7d7fe51ad.16",
		Latitude:   51.302032, Longitude: 1.054028,
		Rewards: []QuestRewardData{{Type: 7, PokemonID: 566}}, // no form_id in this webhook
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for Archen quest, got %d", len(matched))
	}
}

// Real: Pikachu encounter (pokemon_id=25, form_id=598) from explore quest
func TestQuestRealPikachuForm(t *testing.T) {
	human := makeHuman("user1")
	// Tracking any Pikachu (form=0)
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 25, Form: 0, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "3a79c552fd983df88f9aaee01f06aef2.16",
		Latitude:   51.287301, Longitude: 1.079881,
		Rewards: []QuestRewardData{{Type: 7, PokemonID: 25, FormID: 598}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for Pikachu (any form), got %d", len(matched))
	}
}

// Real: Mega energy (type=12, pokemon_id=306, amount=10)
func TestQuestRealMegaEnergyAggron(t *testing.T) {
	human := makeHuman("user1")
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 12, Reward: 306, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "stop1",
		Latitude:   51.0, Longitude: 0.5,
		Rewards: []QuestRewardData{{Type: 12, PokemonID: 306, Amount: 10}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for Aggron mega energy, got %d", len(matched))
	}
}

// Real: Lapras with background field (pokemon_id=131, form_id=322, background=242)
func TestQuestRealLaprasWithBackground(t *testing.T) {
	human := makeHuman("user1")
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 131, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	// The background field is present in the webhook but not used in matching
	data := &QuestData{
		PokestopID: "44c3e9ca7e104bdeb94e78479078ef87.16",
		Latitude:   51.286826, Longitude: 1.079319,
		Rewards: []QuestRewardData{{Type: 7, PokemonID: 131, FormID: 322}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for Lapras quest, got %d", len(matched))
	}
}

// --- Multiple users / multiple trackings ---

func TestQuestMultipleUsersMultipleTrackings(t *testing.T) {
	human1 := makeHuman("user1")
	human2 := makeHuman("user2")
	human2.Area = []string{"testarea"}

	quests := []*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 25, Template: "1"},
		{ID: "user2", ProfileNo: 1, RewardType: 3, Reward: 100, Template: "1"},
		{ID: "user2", ProfileNo: 1, RewardType: 7, Reward: 7, Template: "1"},
	}

	st := makeQuestTestState(quests, map[string]*db.Human{
		"user1": human1, "user2": human2,
	})
	matcher := &QuestMatcher{}

	// Pokemon encounter for Squirtle (7) - only user2 should match
	data := &QuestData{
		PokestopID: "stop1",
		Latitude:   51.0, Longitude: 0.5,
		Rewards: []QuestRewardData{{Type: 7, PokemonID: 7, FormID: 181}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match (user2 for Squirtle), got %d", len(matched))
	}
	if len(matched) > 0 && matched[0].ID != "user2" {
		t.Errorf("Expected user2, got %s", matched[0].ID)
	}
}

func TestQuestSameUserMultipleTrackingsDedup(t *testing.T) {
	human := makeHuman("user1")

	// User tracks both stardust and pokemon - quest has stardust reward
	quests := []*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 3, Reward: 100, Template: "1"},
		{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 25, Template: "2"},
	}

	st := makeQuestTestState(quests, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "stop1",
		Latitude:   51.0, Longitude: 0.5,
		Rewards: []QuestRewardData{{Type: 3, Amount: 200}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match (deduped), got %d", len(matched))
	}
}

// --- Edge cases ---

func TestQuestBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.BlockedAlerts = `["quest"]`
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 3, Reward: 100, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "stop1",
		Latitude:   51.0, Longitude: 0.5,
		Rewards: []QuestRewardData{{Type: 3, Amount: 500}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked alerts, got %d", len(matched))
	}
}

func TestQuestOutsideArea(t *testing.T) {
	human := makeHuman("user1")
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 3, Reward: 100, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "stop1",
		Latitude:   40.0, Longitude: 0.0, // way outside fence
		Rewards: []QuestRewardData{{Type: 3, Amount: 500}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches outside area, got %d", len(matched))
	}
}

func TestQuestWrongProfile(t *testing.T) {
	human := makeHuman("user1")
	human.CurrentProfileNo = 2
	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 25, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "stop1",
		Latitude:   51.0, Longitude: 0.5,
		Rewards: []QuestRewardData{{Type: 7, PokemonID: 25}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong profile, got %d", len(matched))
	}
}

func TestQuestDistanceFilter(t *testing.T) {
	human := makeHuman("user1")
	human.Latitude = 51.5
	human.Longitude = 0.0

	st := makeQuestTestState([]*db.QuestTracking{
		{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 25, Distance: 500, Template: "1"},
	}, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	// Within distance
	data := &QuestData{
		PokestopID: "stop1",
		Latitude:   51.5001, Longitude: 0.0,
		Rewards: []QuestRewardData{{Type: 7, PokemonID: 25}},
	}
	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match within distance, got %d", len(matched))
	}

	// Too far
	data.Latitude = 51.6
	matched = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches out of distance, got %d", len(matched))
	}
}

func TestQuestNoTrackings(t *testing.T) {
	human := makeHuman("user1")
	st := makeQuestTestState(nil, map[string]*db.Human{"user1": human})
	matcher := &QuestMatcher{}

	data := &QuestData{
		PokestopID: "stop1",
		Latitude:   51.0, Longitude: 0.5,
		Rewards: []QuestRewardData{{Type: 7, PokemonID: 25}},
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches with no trackings, got %d", len(matched))
	}
}
