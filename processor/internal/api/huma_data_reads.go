package api

import (
	"context"
	"sort"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/geocoding"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// anyBodyOutput is a huma output whose body re-marshals an arbitrary value to
// the same JSON the legacy gin handlers produced via c.JSON.
type anyBodyOutput struct {
	Body any
}

// weatherCellInput carries the required cell query param for the weather read.
type weatherCellInput struct {
	Cell string `query:"cell" required:"true"`
}

// weatherCellOutput is the typed body for the per-cell weather read: a JSON
// object mapping S2 cell id (int64, marshalled as a JSON string key) to the
// weather condition id (int). Matches the legacy weather.ExportCellWeather
// return shape exactly. The map element type carries the value description and
// the op carries the key/value documentation (huma does not surface a
// description on a bare map body).
type weatherCellOutput struct {
	Body map[int64]int `doc:"S2 cell id (string-encoded int64) → weather condition id (int)"`
}

// RegisterWeather registers GET /api/weather, serving the per-cell weather map.
// Replaces the legacy gin HandleWeather. A missing cell param now yields a
// problem+json 422 (huma's required-validation) instead of the legacy 400.
func RegisterWeather(api huma.API, weather WeatherExporter) {
	huma.Register(api, huma.Operation{
		OperationID: "get-weather", Method: "GET", Path: "/weather",
		Summary: "Get weather data for an S2 cell",
		Description: "Returns the current weather classification around the given S2 cell. The response body is a JSON object mapping each S2 cell id " +
			"(an int64, serialised as a string key) to its weather condition id (an int — the Pokemon GO GameplayWeather condition).",
		Tags:     []string{"weather"},
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *weatherCellInput) (*weatherCellOutput, error) {
		return &weatherCellOutput{Body: weather.ExportCellWeather(in.Cell)}, nil
	})
}

// --- stats: rarity ----------------------------------------------------------

// rarityGroup is one rarity bucket: a rarity group constant and the sorted set
// of pokemon ids that currently fall into it within the rolling window.
type rarityGroup struct {
	Group      int   `json:"group" doc:"Rarity group constant (1=common … 5=ultra-rare)"`
	PokemonIDs []int `json:"pokemon_ids" doc:"Sorted pokemon ids in this rarity group"`
}

// rarityStatsOutput is the typed body for GET /api/stats/rarity: an array of
// rarity groups (ascending by group), each listing its pokemon ids. This is a
// re-shape of the legacy int-keyed map[int][]int into an explicitly described
// array — the stats endpoints are not consumed internally, so the cleaner
// schema is preferred over the bare map.
type rarityStatsOutput struct {
	Body struct {
		Groups []rarityGroup `json:"groups" doc:"Rarity groups in ascending order"`
	}
}

// RegisterStatsRarity registers GET /api/stats/rarity. Replaces the legacy gin
// HandleStats for the rarity export. The body documents each rarity group and
// its pokemon ids.
func RegisterStatsRarity(api huma.API, export func() map[int][]int) {
	huma.Register(api, huma.Operation{
		OperationID: "get-stats-rarity", Method: "GET", Path: "/stats/rarity",
		Summary:     "Rarity group statistics",
		Description: "Returns the rolling-window rarity classification: each rarity group constant (1=common … 5=ultra-rare) with the sorted list of pokemon ids currently in that group.",
		Tags:        []string{"stats"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*rarityStatsOutput, error) {
		m := export()
		groups := make([]rarityGroup, 0, len(m))
		for g, ids := range m {
			groups = append(groups, rarityGroup{Group: g, PokemonIDs: ids})
		}
		sort.Slice(groups, func(i, j int) bool { return groups[i].Group < groups[j].Group })
		out := &rarityStatsOutput{}
		out.Body.Groups = groups
		return out, nil
	})
}

// --- stats: shiny -----------------------------------------------------------

// shinyStatsEntry is one pokemon's shiny rate within the rolling window: the
// pokemon id plus the ShinyStats fields (total seen, shiny seen, ratio).
type shinyStatsEntry struct {
	PokemonID int     `json:"pokemon_id" doc:"Pokemon id"`
	Total     int64   `json:"total" doc:"Total sightings in the rolling window"`
	Seen      int64   `json:"seen" doc:"Confirmed-shiny sightings in the rolling window"`
	Ratio     float64 `json:"ratio" doc:"total / seen — e.g. 512 means an observed 1:512 shiny rate"`
}

// shinyStatsOutput is the typed body for GET /api/stats/shiny: an array of
// per-pokemon shiny rates (ascending by pokemon id). Re-shapes the legacy
// int-keyed map[int]ShinyStats into a described array.
type shinyStatsOutput struct {
	Body struct {
		Pokemon []shinyStatsEntry `json:"pokemon" doc:"Per-pokemon shiny rates, ascending by pokemon id"`
	}
}

// RegisterStatsShiny registers GET /api/stats/shiny. Replaces the legacy gin
// HandleStats for the shiny export. The body documents each pokemon's shiny
// rate fields.
func RegisterStatsShiny(api huma.API, export func() map[int]tracker.ShinyStats) {
	huma.Register(api, huma.Operation{
		OperationID: "get-stats-shiny", Method: "GET", Path: "/stats/shiny",
		Summary:     "Shiny rate statistics",
		Description: "Returns per-pokemon shiny rates observed in the rolling window: total sightings, confirmed-shiny sightings, and the implied ratio.",
		Tags:        []string{"stats"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*shinyStatsOutput, error) {
		m := export()
		entries := make([]shinyStatsEntry, 0, len(m))
		for id, s := range m {
			entries = append(entries, shinyStatsEntry{PokemonID: id, Total: s.Total, Seen: s.Seen, Ratio: s.Ratio})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].PokemonID < entries[j].PokemonID })
		out := &shinyStatsOutput{}
		out.Body.Pokemon = entries
		return out, nil
	})
}

// --- stats: shiny-possible --------------------------------------------------

// shinyPossibleOutput is the typed body for GET /api/stats/shiny-possible: the
// set of pokemon ids that have been seen shiny in the rolling window, nested
// under `map` for backward compatibility with the alerter's ShinyPossible
// loader. Keys are pokemon ids (string-encoded), values are always true.
type shinyPossibleOutput struct {
	Body struct {
		Map map[string]bool `json:"map" doc:"pokemon id → true for every pokemon seen shiny in the rolling window"`
	}
}

// RegisterStatsShinyPossible registers GET /api/stats/shiny-possible. Replaces
// the legacy gin HandleStats for the shiny-possible export. The wire shape
// ({map: {id: true}}) is preserved; the schema now documents it.
func RegisterStatsShinyPossible(api huma.API, export func() map[string]any) {
	huma.Register(api, huma.Operation{
		OperationID: "get-stats-shiny-possible", Method: "GET", Path: "/stats/shiny-possible",
		Summary:     "Shiny-possible spawn data",
		Description: "Returns the set of pokemon ids seen shiny in the rolling window, nested under `map` (pokemon id → true) for the alerter's ShinyPossible loader.",
		Tags:        []string{"stats"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*shinyPossibleOutput, error) {
		raw := export()
		out := &shinyPossibleOutput{}
		out.Body.Map = map[string]bool{}
		if inner, ok := raw["map"].(map[int]bool); ok {
			for id, v := range inner {
				out.Body.Map[strconv.Itoa(id)] = v
			}
		}
		return out, nil
	})
}

// ForwardGeocoder performs a forward geocode lookup. *geocoding.Geocoder
// satisfies this; a minimal interface keeps the Register signature testable.
type ForwardGeocoder interface {
	Forward(query string) ([]geocoding.ForwardResult, error)
}

// geocodeQueryInput carries the required q query param for the forward geocode.
type geocodeQueryInput struct {
	Q string `query:"q" required:"true"`
}

// geocodeForwardOutput is the typed body for the forward geocode read: the list
// of forward-geocode results the geocoder returned.
type geocodeForwardOutput struct {
	Body []geocoding.ForwardResult
}

// RegisterGeocode registers GET /api/geocode/forward, performing a forward
// geocode lookup. Replaces the legacy gin HandleGeocode. A missing/empty q now
// yields a problem+json 422 (huma's required-validation) instead of the legacy
// 400; a nil geocoder yields 503; lookup errors yield 500.
func RegisterGeocode(api huma.API, geocoder ForwardGeocoder) {
	huma.Register(api, huma.Operation{
		OperationID: "get-geocode-forward", Method: "GET", Path: "/geocode/forward",
		Summary: "Forward geocode lookup", Tags: []string{"geocode"},
		Description: "Resolves a free-text place query (`q`) to coordinates via the configured geocoding provider. Returns an array of candidate results (lat/lon plus address detail); an empty array when nothing matched. 503 when no geocoder is configured.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *geocodeQueryInput) (*geocodeForwardOutput, error) {
		// Guard against both an interface-nil and a typed-nil
		// *geocoding.Geocoder (proc.enricher.Geocoder is nil when geocoding
		// is disabled), matching the legacy concrete nil check → 503.
		if geocoder == nil {
			return nil, huma.Error503ServiceUnavailable("geocoder not configured")
		}
		if g, ok := geocoder.(*geocoding.Geocoder); ok && g == nil {
			return nil, huma.Error503ServiceUnavailable("geocoder not configured")
		}
		results, err := geocoder.Forward(in.Q)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return &geocodeForwardOutput{Body: results}, nil
	})
}
