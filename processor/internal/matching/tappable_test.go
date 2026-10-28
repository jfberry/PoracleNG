package matching

import (
	"encoding/json"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func makeTappable(id string, rewardType, reward, amount int) *db.TappableTracking {
	return &db.TappableTracking{
		UID:        1,
		ID:         id,
		ProfileNo:  1,
		RewardType: rewardType,
		Reward:     reward,
		Amount:     amount,
		Template:   "1",
	}
}

// Two fences: TestArea (the human's default area, holding the event at 51,0)
// and OtherArea (holding 55,0).
func makeTappableState(tappables []*db.TappableTracking, humans map[string]*db.Human) *state.State {
	fences := []geofence.Fence{
		{
			Name:             "TestArea",
			DisplayInMatches: true,
			Path:             [][2]float64{{50.0, -1.0}, {52.0, -1.0}, {52.0, 1.0}, {50.0, 1.0}},
		},
		{
			Name:             "OtherArea",
			DisplayInMatches: true,
			Path:             [][2]float64{{54.0, -1.0}, {56.0, -1.0}, {56.0, 1.0}, {54.0, 1.0}},
		},
	}
	return &state.State{
		Humans:    humans,
		Tappables: tappables,
		Geofence:  geofence.NewSpatialIndex(fences),
		Fences:    fences,
	}
}

func tappableAt(lat float64, rewards ...TappableRewardData) *TappableData {
	return &TappableData{EncounterID: "16082073574809335024", Latitude: lat, Longitude: 0.0, Rewards: rewards}
}

func matchTappable(t *testing.T, rule *db.TappableTracking, data *TappableData) int {
	t.Helper()
	st := makeTappableState([]*db.TappableTracking{rule}, map[string]*db.Human{"user1": makeHuman("user1")})
	matched, _ := (&TappableMatcher{}).Match(data, st)
	return len(matched)
}

func TestTappableMatchRewardTypes(t *testing.T) {
	cases := []struct {
		name   string
		rule   *db.TappableTracking
		reward TappableRewardData
		want   int
	}{
		{"item exact", makeTappable("user1", 2, 1301, 0), TappableRewardData{Type: 2, ItemID: 1301, Amount: 1}, 1},
		{"item other id", makeTappable("user1", 2, 1301, 0), TappableRewardData{Type: 2, ItemID: 701, Amount: 1}, 0},
		{"item any", makeTappable("user1", 2, 0, 0), TappableRewardData{Type: 2, ItemID: 701, Amount: 1}, 1},
		{"stardust above min", makeTappable("user1", 3, 500, 0), TappableRewardData{Type: 3, Amount: 1000}, 1},
		{"stardust equal min", makeTappable("user1", 3, 500, 0), TappableRewardData{Type: 3, Amount: 500}, 1},
		{"stardust below min", makeTappable("user1", 3, 500, 0), TappableRewardData{Type: 3, Amount: 200}, 0},
		{"stardust any", makeTappable("user1", 3, 0, 0), TappableRewardData{Type: 3, Amount: 1}, 1},
		{"candy exact", makeTappable("user1", 4, 41, 0), TappableRewardData{Type: 4, PokemonID: 41, Amount: 6}, 1},
		{"candy other pokemon", makeTappable("user1", 4, 41, 0), TappableRewardData{Type: 4, PokemonID: 25, Amount: 6}, 0},
		{"candy any", makeTappable("user1", 4, 0, 0), TappableRewardData{Type: 4, PokemonID: 854, Amount: 6}, 1},
		{"pokecoin above min", makeTappable("user1", 8, 10, 0), TappableRewardData{Type: 8, Amount: 50}, 1},
		{"pokecoin below min", makeTappable("user1", 8, 10, 0), TappableRewardData{Type: 8, Amount: 5}, 0},
		{"xl candy exact", makeTappable("user1", 9, 25, 0), TappableRewardData{Type: 9, PokemonID: 25, Amount: 1}, 1},
		{"xl candy other pokemon", makeTappable("user1", 9, 25, 0), TappableRewardData{Type: 9, PokemonID: 41, Amount: 1}, 0},
		{"xl candy any", makeTappable("user1", 9, 0, 0), TappableRewardData{Type: 9, PokemonID: 41, Amount: 1}, 1},
		{"mega energy exact", makeTappable("user1", 12, 6, 0), TappableRewardData{Type: 12, PokemonID: 6, Amount: 20}, 1},
		{"mega energy any", makeTappable("user1", 12, 0, 0), TappableRewardData{Type: 12, PokemonID: 3, Amount: 20}, 1},
		{"mega energy other pokemon", makeTappable("user1", 12, 6, 0), TappableRewardData{Type: 12, PokemonID: 3, Amount: 20}, 0},
		{"candy rule vs xl candy reward", makeTappable("user1", 4, 25, 0), TappableRewardData{Type: 9, PokemonID: 25, Amount: 1}, 0},
		{"untrackable reward type", makeTappable("user1", 0, 0, 0), TappableRewardData{Type: 0, Amount: 1}, 0},
		{"experience reward type", makeTappable("user1", 1, 0, 0), TappableRewardData{Type: 1, Amount: 100}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchTappable(t, tc.rule, tappableAt(51.0, tc.reward)); got != tc.want {
				t.Errorf("got %d matches, want %d", got, tc.want)
			}
		})
	}
}

func TestTappableMatchAmountMin(t *testing.T) {
	for _, rt := range []int{2, 4, 9, 12} {
		rule := makeTappable("user1", rt, 0, 5)
		for amount, want := range map[int]int{3: 0, 5: 1, 6: 1} {
			r := TappableRewardData{Type: rt, ItemID: 1301, PokemonID: 41, Amount: amount}
			if got := matchTappable(t, rule, tappableAt(51.0, r)); got != want {
				t.Errorf("type %d amount %d: got %d, want %d", rt, amount, got, want)
			}
		}
	}
}

func TestTappableMatchAnyReward(t *testing.T) {
	rule := makeTappable("user1", 2, 1301, 0)
	data := tappableAt(51.0,
		TappableRewardData{Type: 4, PokemonID: 41, Amount: 6},
		TappableRewardData{Type: 1, Amount: 100},
		TappableRewardData{Type: 2, ItemID: 1301, Amount: 1},
	)
	if got := matchTappable(t, rule, data); got != 1 {
		t.Errorf("rule matching the third reward: got %d, want 1", got)
	}
	data.Rewards = data.Rewards[:2]
	if got := matchTappable(t, rule, data); got != 0 {
		t.Errorf("no reward matching: got %d, want 0", got)
	}
	data.Rewards = nil
	if got := matchTappable(t, rule, data); got != 0 {
		t.Errorf("no rewards: got %d, want 0", got)
	}
}

// One user with two rules that both match gets one alert.
func TestTappableMatchDedupsUser(t *testing.T) {
	a := makeTappable("user1", 4, 41, 0)
	b := makeTappable("user1", 4, 0, 0)
	b.UID = 2
	st := makeTappableState([]*db.TappableTracking{a, b}, map[string]*db.Human{"user1": makeHuman("user1")})
	matched, areas := (&TappableMatcher{}).Match(tappableAt(51.0, TappableRewardData{Type: 4, PokemonID: 41, Amount: 6}), st)
	if len(matched) != 1 {
		t.Fatalf("got %d matches, want 1", len(matched))
	}
	if matched[0].RuleTable != "tappable" {
		t.Errorf("RuleTable = %q, want tappable", matched[0].RuleTable)
	}
	if len(areas) != 1 || areas[0].Name != "TestArea" {
		t.Errorf("areas = %+v, want [TestArea]", areas)
	}
}

func TestTappableMatchAreaMiss(t *testing.T) {
	rule := makeTappable("user1", 4, 41, 0)
	if got := matchTappable(t, rule, tappableAt(55.0, TappableRewardData{Type: 4, PokemonID: 41, Amount: 6})); got != 0 {
		t.Errorf("event outside the human's area: got %d, want 0", got)
	}
}

func TestTappableMatchDistance(t *testing.T) {
	rule := makeTappable("user1", 4, 41, 0)
	rule.Distance = 1000
	reward := TappableRewardData{Type: 4, PokemonID: 41, Amount: 6}
	// Human is at (51, 0); 51.005 is ~556m away, 51.02 ~2.2km.
	if got := matchTappable(t, rule, tappableAt(51.005, reward)); got != 1 {
		t.Errorf("inside distance: got %d, want 1", got)
	}
	if got := matchTappable(t, rule, tappableAt(51.02, reward)); got != 0 {
		t.Errorf("outside distance: got %d, want 0", got)
	}
}

func TestTappableBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["tappable"]`)
	st := makeTappableState([]*db.TappableTracking{makeTappable("user1", 4, 41, 0)}, map[string]*db.Human{"user1": human})
	matched, _ := (&TappableMatcher{}).Match(tappableAt(51.0, TappableRewardData{Type: 4, PokemonID: 41, Amount: 6}), st)
	if len(matched) != 0 {
		t.Errorf("blocked tappable: got %d matches, want 0", len(matched))
	}
}

func TestTappableOverrideAreas(t *testing.T) {
	rule := makeTappable("user1", 4, 41, 0)
	rule.OverrideAreas = []string{"otherarea"}
	reward := TappableRewardData{Type: 4, PokemonID: 41, Amount: 6}
	if got := matchTappable(t, rule, tappableAt(55.0, reward)); got != 1 {
		t.Errorf("event in the override area: got %d, want 1", got)
	}
	if got := matchTappable(t, rule, tappableAt(51.0, reward)); got != 0 {
		t.Errorf("event in the human's (replaced) area: got %d, want 0", got)
	}
}

func TestTappableOverrideLocation(t *testing.T) {
	human := makeHuman("user1")
	human.Locations = map[string]*db.UserLocation{"work": {Label: "Work", Latitude: 55.0, Longitude: 0.0}}
	rule := makeTappable("user1", 4, 41, 0)
	rule.Distance = 1000
	rule.OverrideLocationLabel = "Work"
	st := makeTappableState([]*db.TappableTracking{rule}, map[string]*db.Human{"user1": human})
	m := &TappableMatcher{}
	reward := TappableRewardData{Type: 4, PokemonID: 41, Amount: 6}

	if matched, _ := m.Match(tappableAt(55.005, reward), st); len(matched) != 1 {
		t.Errorf("near the override location: got %d, want 1", len(matched))
	}
	if matched, _ := m.Match(tappableAt(51.0, reward), st); len(matched) != 0 {
		t.Errorf("near the human's own location: got %d, want 0", len(matched))
	}
}

// A malformed reward (item id null, candy without a pokemon) parses as an
// untrackable reward, so it never fires an "all items" / "any candy" rule.
func TestTappableMalformedRewardDoesNotMatchWildcard(t *testing.T) {
	for _, tc := range []struct {
		rewards string
		rule    *db.TappableTracking
	}{
		{`[{"item":null,"count":1}]`, makeTappable("user1", TappableRewardItem, 0, 0)},
		{`[{"pokemon_candy":null,"count":3}]`, makeTappable("user1", TappableRewardCandy, 0, 0)},
	} {
		tw := webhook.TappableWebhook{Rewards: json.RawMessage(tc.rewards)}
		if n := matchTappable(t, tc.rule, tappableAt(51.0, ParseTappableRewards(&tw)...)); n != 0 {
			t.Errorf("%s: %d matches, want 0", tc.rewards, n)
		}
	}
}
