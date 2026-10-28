package matching

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// tappableRewardTypeByKey maps Golbat's LootItemProto oneof names to the
// quest reward type ids tappable rules use. Keys not listed are kept with
// Type 0 (described, never matched).
var tappableRewardTypeByKey = map[string]int{
	"item":                    TappableRewardItem,
	"stardust":                TappableRewardStardust,
	"pokemon_candy":           TappableRewardCandy,
	"pokecoin":                TappableRewardPokecoin,
	"xl_candy":                TappableRewardXLCandy,
	"mega_energy_pokemon_id":  TappableRewardMegaEnergy,
	"temp_evo_pokemon_branch": TappableRewardMegaEnergy,
}

// TappableRewardTypeForKey returns the reward type id for a LootItemProto
// oneof name, or 0 when Poracle can't track that reward.
func TappableRewardTypeForKey(key string) int {
	return tappableRewardTypeByKey[key]
}

// ParseTappableRewards decodes a tappable's rewards once per event. Each
// entry of `rewards` is a protojson LootItemProto: one oneof key plus
// `count`. When `rewards` is null, missing or unreadable, one entry is
// synthesised from the top-level reward_type / item_id / reward_pokemon_id /
// count (which describe rewards[0]). Unknown keys inside an entry are
// ignored; an entry without a recognisable oneof key is skipped.
func ParseTappableRewards(tw *webhook.TappableWebhook) []TappableRewardData {
	if tw == nil {
		return nil
	}
	if rewards, ok := parseTappableRewardArray(tw.Rewards); ok {
		return rewards
	}
	if tw.RewardType == nil || *tw.RewardType == "" {
		return nil
	}
	r := TappableRewardData{Key: *tw.RewardType, Type: TappableRewardTypeForKey(*tw.RewardType)}
	if tw.Count != nil {
		r.Amount = *tw.Count
	}
	switch r.Type {
	case TappableRewardItem:
		if tw.ItemID != nil {
			r.ItemID = *tw.ItemID
		}
	case TappableRewardCandy, TappableRewardXLCandy, TappableRewardMegaEnergy:
		if tw.RewardPokemonID != nil {
			r.PokemonID = *tw.RewardPokemonID
		}
	}
	return []TappableRewardData{untrackIfUnidentified(r)}
}

// untrackIfUnidentified clears the type of a reward whose identifying value
// is missing or invalid (an item without an item id, candy / XL candy / mega
// energy without a pokemon), so it is described but never matched — a
// zero id would otherwise match "any item" / "any candy" rules. Stardust and
// pokecoins are identified by their count; a missing count is amount 0.
func untrackIfUnidentified(r TappableRewardData) TappableRewardData {
	switch r.Type {
	case TappableRewardItem:
		if r.ItemID <= 0 {
			r.Type, r.ItemID = 0, 0
		}
	case TappableRewardCandy, TappableRewardXLCandy, TappableRewardMegaEnergy:
		if r.PokemonID <= 0 {
			r.Type, r.PokemonID = 0, 0
		}
	}
	return r
}

// parseTappableRewardArray decodes the rewards array. ok is false when the
// array is absent, null or not an array, so the caller falls back to the
// top-level fields.
func parseTappableRewardArray(raw json.RawMessage) ([]TappableRewardData, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, false
	}
	out := make([]TappableRewardData, 0, len(entries))
	for _, entry := range entries {
		if r, ok := parseTappableRewardEntry(entry); ok {
			out = append(out, r)
		}
	}
	return out, true
}

func parseTappableRewardEntry(entry map[string]json.RawMessage) (TappableRewardData, bool) {
	var r TappableRewardData
	if c, ok := entry["count"]; ok {
		r.Amount = jsonInt(c)
	}
	// Known keys first, so an entry can't be misread through an unknown key.
	for key, typ := range tappableRewardTypeByKey {
		v, ok := entry[key]
		if !ok {
			continue
		}
		r.Key, r.Type = key, typ
		switch key {
		case "item":
			r.ItemID = jsonInt(v)
		case "pokemon_candy", "xl_candy", "mega_energy_pokemon_id":
			r.PokemonID = jsonInt(v)
		case "temp_evo_pokemon_branch":
			var branch struct {
				PokedexID json.RawMessage `json:"pokedex_id"`
			}
			if json.Unmarshal(v, &branch) == nil {
				r.PokemonID = jsonInt(branch.PokedexID)
			}
		}
		return untrackIfUnidentified(r), true
	}
	// An untrackable reward: keep the oneof name so it can be described.
	for _, key := range tappableUntrackedKeys {
		if _, ok := entry[key]; ok {
			r.Key = key
			return r, true
		}
	}
	// A oneof added after this list: take the first other key, sorted, so
	// the result doesn't depend on map order.
	others := make([]string, 0, len(entry))
	for key := range entry {
		if key != "count" {
			others = append(others, key)
		}
	}
	if len(others) == 0 {
		return r, false
	}
	sort.Strings(others)
	r.Key = others[0]
	return r, true
}

// tappableUntrackedKeys are the other LootItemProto oneof names Golbat pins
// (decoder/tappable_decode_test.go): described in alerts, never matched.
var tappableUntrackedKeys = []string{
	"experience", "pokemon_egg", "avatar_template_id", "sticker_id",
	"follower_pokemon", "neutral_avatar_template_id",
	"neutral_avatar_item_template", "neutral_avatar_item_display",
}

// jsonInt reads a protojson number (int32 renders as a number, int64 as a
// quoted string). Anything else reads as 0.
func jsonInt(raw json.RawMessage) int {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if v, err := strconv.Atoi(s); err == nil {
			return v
		}
	}
	return 0
}

// TappableRewardSignature identifies the first reward for duplicate
// detection: type, item, pokemon and count of rewards[0].
func TappableRewardSignature(rewards []TappableRewardData) string {
	if len(rewards) == 0 {
		return ""
	}
	r := rewards[0]
	return r.Key + "/" + strconv.Itoa(r.ItemID) + "/" + strconv.Itoa(r.PokemonID) + "/" + strconv.Itoa(r.Amount)
}
