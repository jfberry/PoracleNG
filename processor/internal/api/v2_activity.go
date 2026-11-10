package api

import (
	"context"
	"sort"

	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// v2PokemonIDs is one pokemon's recently-seen ids in a per-pokemon category
// (costumes, forms, raid costumes, raid forms).
//
// An array of these rather than a JSON object keyed by pokemon id: v2 schemas
// are strictly typed, and a map would document only as
// additionalProperties. The cost is one client-side index build; the benefit is
// that the shape appears in /openapi.json like every other v2 body.
type v2PokemonIDs struct {
	PokemonID int   `json:"pokemon_id" doc:"Pokédex id"`
	IDs       []int `json:"ids" doc:"Ids seen on this pokemon within the window, ascending"`
}

// v2ActivityBody is the whole recent-activity snapshot.
//
// Everything the processor has seen in the last window_secs, in one response.
// One call rather than twelve because the payload is a few KB and a client
// populating several pickers on a page would otherwise fan out — the N+1 the
// PoracleWeb.NET author objected to on #214.
//
// IMPORTANT: an empty list means "nothing of this kind has been seen recently",
// NOT "this server does not support it". Feature support is reported by the
// capability flags on /health (and by the field's presence in /openapi.json);
// this endpoint reports live data only. A quiet instance, or one restarted
// within the window, legitimately returns empty lists. See #212.
type v2ActivityBody struct {
	WindowSecs int `json:"window_secs" doc:"Recency window in seconds: an id appears here if it was seen within this many seconds"`

	RaidBosses      []int `json:"raid_bosses" doc:"Pokédex ids seen as raid bosses"`
	MaxBattleBosses []int `json:"max_battle_bosses" doc:"Pokédex ids seen as max battle bosses"`
	QuestPokemon    []int `json:"quest_pokemon" doc:"Pokédex ids seen as quest encounter rewards"`
	QuestItems      []int `json:"quest_items" doc:"Item ids seen as quest rewards"`
	QuestCandy      []int `json:"quest_candy" doc:"Pokédex ids seen as quest candy rewards"`
	QuestMega       []int `json:"quest_mega" doc:"Pokédex ids seen as quest mega energy rewards"`
	QuestXL         []int `json:"quest_xl" doc:"Pokédex ids seen as quest XL candy rewards"`
	InvasionGrunts  []int `json:"invasion_grunts" doc:"Grunt ids (game-master) seen as invasions"`

	Costumes     []v2PokemonIDs `json:"costumes" doc:"Costume ids seen on wild pokemon, grouped by pokemon, ascending by pokemon_id"`
	Forms        []v2PokemonIDs `json:"forms" doc:"Form ids seen on wild pokemon, grouped by pokemon"`
	RaidCostumes []v2PokemonIDs `json:"raid_costumes" doc:"Costume ids seen on raid bosses, grouped by pokemon"`
	RaidForms    []v2PokemonIDs `json:"raid_forms" doc:"Form ids seen on raid bosses, grouped by pokemon"`
}

type v2ActivityOutput struct {
	Body v2ActivityBody
}

// RegisterV2Activity registers GET /api/v2/activity.
//
// The tracker is in-memory with no persistence, so a restart empties it until
// traffic refills it — another reason empty must not be read as unsupported.
// A nil tracker (a processor built without one) serves the same empty payload
// rather than failing.
func RegisterV2Activity(api huma.API, ra *tracker.RecentActivity) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-get-activity", Method: "GET", Path: "/v2/activity",
		Summary: "Recently-seen bosses, rewards, costumes and forms",
		Description: "Everything the processor has seen within `window_secs`, in one response: raid and max battle bosses, " +
			"quest reward ids, invasion grunts, and the costumes/forms seen on wild pokemon and raid bosses. " +
			"Intended for populating pickers with what is actually live rather than the whole masterfile. " +
			"Resolve ids to names via /api/masterdata/monsters, /api/masterdata/costumes and /api/masterdata/grunts. " +
			"An empty list means nothing of that kind was seen recently — NOT that the server lacks support; " +
			"use the /health capability flags for that. Data is in-memory and does not survive a restart.",
		Tags: []string{"v2-activity"}, Security: []map[string][]string{{"poracleSecret": {}}},
		RejectUnknownQueryParameters: true,
	}, func(_ context.Context, _ *struct{}) (*v2ActivityOutput, error) {
		body := v2ActivityBody{
			WindowSecs:      int(tracker.RecentActivityTTL.Seconds()),
			RaidBosses:      []int{},
			MaxBattleBosses: []int{},
			QuestPokemon:    []int{},
			QuestItems:      []int{},
			QuestCandy:      []int{},
			QuestMega:       []int{},
			QuestXL:         []int{},
			InvasionGrunts:  []int{},
			Costumes:        []v2PokemonIDs{},
			Forms:           []v2PokemonIDs{},
			RaidCostumes:    []v2PokemonIDs{},
			RaidForms:       []v2PokemonIDs{},
		}
		if ra == nil {
			return &v2ActivityOutput{Body: body}, nil
		}

		body.RaidBosses = sortedInts(ra.ActiveRaidBosses())
		body.MaxBattleBosses = sortedInts(ra.ActiveMaxBattleBosses())
		body.QuestPokemon = sortedInts(ra.ActiveQuestPokemon())
		body.QuestItems = sortedInts(ra.ActiveQuestItems())
		body.QuestCandy = sortedInts(ra.ActiveQuestCandy())
		body.QuestMega = sortedInts(ra.ActiveQuestMega())
		body.QuestXL = sortedInts(ra.ActiveQuestXL())
		body.InvasionGrunts = sortedInts(ra.ActiveInvasionGrunts())

		body.Costumes = groupByPokemon(ra.AllCostumes())
		body.Forms = groupByPokemon(ra.AllForms())
		body.RaidCostumes = groupByPokemon(ra.AllRaidCostumes())
		body.RaidForms = groupByPokemon(ra.AllRaidForms())

		return &v2ActivityOutput{Body: body}, nil
	})
}

// sortedInts returns ids ascending, never nil — the tracker returns them in Go
// map order, which is randomised per iteration, and a JSON API that reshuffles
// an unchanged payload defeats client-side caching and makes diffs unreadable.
func sortedInts(ids []int) []int {
	if ids == nil {
		return []int{}
	}
	sort.Ints(ids)
	return ids
}

// groupByPokemon flattens a pokemon → ids map into the wire shape, ascending by
// pokemon_id with each id list ascending too.
func groupByPokemon(m map[int][]int) []v2PokemonIDs {
	out := make([]v2PokemonIDs, 0, len(m))
	for pokemonID, ids := range m {
		out = append(out, v2PokemonIDs{PokemonID: pokemonID, IDs: sortedInts(ids)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PokemonID < out[j].PokemonID })
	return out
}
