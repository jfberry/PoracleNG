package mappers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// coordsRe matches "lat,lon" pairs with optional surrounding whitespace.
// Reuses the same shape as bot/argmatch.go's latLonRe but tolerates a single
// space after the comma — slash users frequently paste coordinates from
// Google Maps which include one ("51.28, 1.08").
var coordsRe = regexp.MustCompile(`^\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*$`)

// Location maps /location to text-command tokens. Accepts either a coordinate
// pair (passed through unchanged in the `lat,lon` form the text bot expects)
// or a free-form place name that we forward-geocode via deps.Geocoder.
//
// Unlike every other mapper, Location takes BotDeps because resolving a place
// name requires a live geocoder call. The dispatcher special-cases this in
// HandleCommand and routes here directly; Location does NOT register in the
// shared mapper registry because the registry's func type is options-only.
//
// When the geocoder is not configured, a coordinate input still works (the
// fast path doesn't touch deps); a place-name input surfaces a clean error
// telling the operator they need to enable geocoding. The text bot has the
// same constraint — see internal/bot/commands/location.go — so behaviour is
// consistent across surfaces.
func Location(opts []*discordgo.ApplicationCommandInteractionDataOption, deps *bot.BotDeps) ([]string, error) {
	o := flattenOptions(opts)
	raw := strings.TrimSpace(getString(o["place"]))
	if raw == "" {
		return nil, &MapperError{Key: "error.slash.location.empty"}
	}

	if m := coordsRe.FindStringSubmatch(raw); m != nil {
		// Re-pack in the text-bot canonical form "lat,lon" with no spaces
		// so latLonRe in argmatch.go matches it on the first try.
		return []string{m[1] + "," + m[2]}, nil
	}

	if deps == nil || deps.Geocoder == nil {
		return nil, &MapperError{Key: "error.slash.location.no_geocoder"}
	}
	results, err := deps.Geocoder.Forward(raw)
	if err != nil || len(results) == 0 {
		return nil, &MapperError{Key: "error.slash.location.geocode_failed", Args: []any{raw}}
	}
	return []string{fmt.Sprintf("%g,%g", results[0].Latitude, results[0].Longitude)}, nil
}
