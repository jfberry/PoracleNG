package matching

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func ptr[T any](v T) *T { return &v }

func TestParseTappableRewards(t *testing.T) {
	cases := []struct {
		name string
		tw   webhook.TappableWebhook
		want []TappableRewardData
	}{
		{
			name: "candy",
			tw:   webhook.TappableWebhook{Rewards: json.RawMessage(`[{"pokemon_candy":41,"count":6}]`)},
			want: []TappableRewardData{{Type: TappableRewardCandy, PokemonID: 41, Amount: 6, Key: "pokemon_candy"}},
		},
		{
			name: "item",
			tw:   webhook.TappableWebhook{Rewards: json.RawMessage(`[{"item":1301,"count":1}]`)},
			want: []TappableRewardData{{Type: TappableRewardItem, ItemID: 1301, Amount: 1, Key: "item"}},
		},
		{
			name: "stardust, pokecoin, xl candy, mega energy and an unknown key",
			tw: webhook.TappableWebhook{Rewards: json.RawMessage(`[{"stardust":true,"count":250},{"pokecoin":true,"count":5},
				{"xl_candy":25,"count":1},{"mega_energy_pokemon_id":6,"count":20},{"experience":true,"count":100,"future_field":1}]`)},
			want: []TappableRewardData{
				{Type: TappableRewardStardust, Amount: 250, Key: "stardust"},
				{Type: TappableRewardPokecoin, Amount: 5, Key: "pokecoin"},
				{Type: TappableRewardXLCandy, PokemonID: 25, Amount: 1, Key: "xl_candy"},
				{Type: TappableRewardMegaEnergy, PokemonID: 6, Amount: 20, Key: "mega_energy_pokemon_id"},
				{Amount: 100, Key: "experience"},
			},
		},
		{
			name: "temp evo branch maps to mega energy",
			tw:   webhook.TappableWebhook{Rewards: json.RawMessage(`[{"temp_evo_pokemon_branch":{"pokedex_id":6,"temp_evo_id":2},"count":10}]`)},
			want: []TappableRewardData{{Type: TappableRewardMegaEnergy, PokemonID: 6, Amount: 10, Key: "temp_evo_pokemon_branch"}},
		},
		{
			name: "null rewards fall back to the top-level fields",
			tw: webhook.TappableWebhook{Rewards: json.RawMessage(`null`), RewardType: ptr("item"),
				ItemID: ptr(1301), Count: ptr(2)},
			want: []TappableRewardData{{Type: TappableRewardItem, ItemID: 1301, Amount: 2, Key: "item"}},
		},
		{
			name: "absent rewards fall back to top-level candy",
			tw:   webhook.TappableWebhook{RewardType: ptr("pokemon_candy"), RewardPokemonID: ptr(25), Count: ptr(6)},
			want: []TappableRewardData{{Type: TappableRewardCandy, PokemonID: 25, Amount: 6, Key: "pokemon_candy"}},
		},
		{
			name: "no rewards at all",
			tw:   webhook.TappableWebhook{Rewards: json.RawMessage(`null`)},
			want: nil,
		},
		{
			// A reward whose identifying value is missing or invalid is kept
			// for display but untrackable (type 0): a zero id would match the
			// "any item" / "any candy" rules.
			name: "item, candy, xl candy and mega energy without a valid id are untrackable",
			tw: webhook.TappableWebhook{Rewards: json.RawMessage(`[{"item":null,"count":1},{"item":"abc","count":1},
				{"pokemon_candy":0,"count":3},{"xl_candy":-4,"count":1},{"mega_energy_pokemon_id":null,"count":20},
				{"temp_evo_pokemon_branch":{"temp_evo_id":2},"count":10}]`)},
			want: []TappableRewardData{
				{Amount: 1, Key: "item"},
				{Amount: 1, Key: "item"},
				{Amount: 3, Key: "pokemon_candy"},
				{Amount: 1, Key: "xl_candy"},
				{Amount: 20, Key: "mega_energy_pokemon_id"},
				{Amount: 10, Key: "temp_evo_pokemon_branch"},
			},
		},
		{
			name: "stardust and pokecoin without a count are amount 0, still trackable",
			tw:   webhook.TappableWebhook{Rewards: json.RawMessage(`[{"stardust":true},{"pokecoin":true,"count":null}]`)},
			want: []TappableRewardData{
				{Type: TappableRewardStardust, Key: "stardust"},
				{Type: TappableRewardPokecoin, Key: "pokecoin"},
			},
		},
		{
			name: "top-level item without an item id is untrackable",
			tw:   webhook.TappableWebhook{RewardType: ptr("item"), Count: ptr(1)},
			want: []TappableRewardData{{Amount: 1, Key: "item"}},
		},
		{
			name: "top-level candy, xl candy and mega energy without a pokemon are untrackable",
			tw:   webhook.TappableWebhook{RewardType: ptr("pokemon_candy"), Count: ptr(6)},
			want: []TappableRewardData{{Amount: 6, Key: "pokemon_candy"}},
		},
		{
			name: "top-level xl candy with pokemon 0",
			tw:   webhook.TappableWebhook{RewardType: ptr("xl_candy"), RewardPokemonID: ptr(0), Count: ptr(1)},
			want: []TappableRewardData{{Amount: 1, Key: "xl_candy"}},
		},
		{
			name: "top-level mega energy without a pokemon",
			tw:   webhook.TappableWebhook{RewardType: ptr("mega_energy_pokemon_id"), Count: ptr(20)},
			want: []TappableRewardData{{Amount: 20, Key: "mega_energy_pokemon_id"}},
		},
		{
			name: "top-level stardust without a count is amount 0",
			tw:   webhook.TappableWebhook{RewardType: ptr("stardust")},
			want: []TappableRewardData{{Type: TappableRewardStardust, Key: "stardust"}},
		},
		{
			name: "entry with only count is skipped",
			tw:   webhook.TappableWebhook{Rewards: json.RawMessage(`[{"count":3}]`)},
			want: []TappableRewardData{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseTappableRewards(&tc.tw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTappableRewardSignature(t *testing.T) {
	a := TappableRewardSignature([]TappableRewardData{{Key: "pokemon_candy", PokemonID: 41, Amount: 6}})
	b := TappableRewardSignature([]TappableRewardData{{Key: "pokemon_candy", PokemonID: 25, Amount: 6}})
	if a == b {
		t.Errorf("different rewards share signature %q", a)
	}
	if TappableRewardSignature(nil) != "" {
		t.Error("empty rewards must have an empty signature")
	}
}
