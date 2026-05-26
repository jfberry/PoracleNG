package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

type LocationCommand struct{}

func (c *LocationCommand) Name() string      { return "cmd.location" }
func (c *LocationCommand) Aliases() []string { return nil }

var locationParams = []bot.ParamDef{
	{Type: bot.ParamLatLon},
}

func (c *LocationCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Bare `!location` with no args: show the user's current location (if
	// set) before the usage help so they can see what's stored without
	// having to run a different command.
	if len(args) == 0 {
		var replies []bot.Reply
		if ctx.HasLocation {
			if human, err := ctx.Humans.Get(ctx.TargetID); err == nil && human != nil &&
				(human.Latitude != 0 || human.Longitude != 0) {
				mapLink := fmt.Sprintf("<https://maps.google.com/maps?q=%f,%f>", human.Latitude, human.Longitude)
				replies = append(replies, bot.Reply{
					Text: tr.Tf("msg.location.current", human.Latitude, human.Longitude) + "\n" + mapLink,
				})
			}
		}
		replies = append(replies, bot.Reply{Text: inlineUsage(ctx, "msg.location.usage")})
		return replies
	}

	if help := helpArgReply(ctx, args, "msg.location.usage"); help != nil {
		return []bot.Reply{*help}
	}

	// Subcommand dispatch — checked before the bare lat/lon path so that
	// "add" and "list" are intercepted even when no ArgMatcher is wired.
	enTr := ctx.Translations.For("en")
	matchSub := func(key string) bool {
		sub := strings.ToLower(args[0])
		return sub == strings.ToLower(tr.T(key)) || sub == strings.ToLower(enTr.T(key))
	}

	switch {
	case matchSub("arg.add"):
		return c.addLocation(ctx, args[1:])
	case matchSub("arg.list"):
		return c.listLocations(ctx)
	case matchSub("arg.show"):
		return c.showLocation(ctx, args[1:])
	case matchSub("arg.remove"):
		return c.removeLocation(ctx, args[1:])
	}

	parsed := ctx.ArgMatcher.Match(args, locationParams, ctx.Language)

	var lat, lon float64

	if parsed.Coords != nil {
		lat = parsed.Coords.Lat
		lon = parsed.Coords.Lon
	} else if len(parsed.Unrecognized) > 0 && ctx.Geocoder != nil {
		// Try forward geocoding with unrecognized args as an address query
		query := strings.Join(parsed.Unrecognized, " ")
		results, err := ctx.Geocoder.Forward(query)
		if err != nil || len(results) == 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.location.not_found")}}
		}
		lat = results[0].Latitude
		lon = results[0].Longitude
	} else {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.location.specify")}}
	}

	// Set location
	if err := ctx.Humans.SetLocation(ctx.TargetID, ctx.ProfileNo, lat, lon); err != nil {
		log.Errorf("location: update human: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()

	mapLink := fmt.Sprintf("<https://maps.google.com/maps?q=%f,%f>", lat, lon)
	reply := bot.Reply{React: "✅", Text: tr.Tf("msg.location.set", lat, lon) + "\n" + mapLink}

	// Generate location pin tile if static map is available
	if ctx.StaticMap != nil {
		data := map[string]any{
			"latitude":  lat,
			"longitude": lon,
		}
		reply.ImageURL = ctx.StaticMap.GetPregeneratedTileURL("location", data, "staticMap")
	}

	return []bot.Reply{reply}
}

// addLocation handles `!location add <name> <coords-or-place>`.
func (c *LocationCommand) addLocation(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) < 2 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.add_usage", bot.CommandPrefix(ctx))}}
	}
	name := args[0]
	placeOrCoords := strings.Join(args[1:], " ")

	lat, lon, err := resolveLatLon(ctx, placeOrCoords)
	if err != nil {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.geocode_failed", placeOrCoords)}}
	}

	if _, err := ctx.Humans.AddLocation(store.UserLocation{
		ID: ctx.TargetID, Label: name, Latitude: lat, Longitude: lon,
	}); err != nil {
		if errors.Is(err, store.ErrDuplicateLocation) {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.duplicate", name, bot.CommandPrefix(ctx))}}
		}
		log.Errorf("location add: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	reply := bot.Reply{React: "✅", Text: tr.Tf("msg.location.added", name, lat, lon)}
	if ctx.StaticMap != nil {
		data := map[string]any{
			"latitude":  lat,
			"longitude": lon,
		}
		reply.ImageURL = ctx.StaticMap.GetPregeneratedTileURL("location", data, "staticMap")
	}
	return []bot.Reply{reply}
}

// listLocations handles `!location list`.
func (c *LocationCommand) listLocations(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	locs, _ := ctx.Humans.ListLocations(ctx.TargetID)
	human, _ := ctx.Humans.Get(ctx.TargetID)

	hasDefault := human != nil && (human.Latitude != 0 || human.Longitude != 0)
	if len(locs) == 0 && !hasDefault {
		return []bot.Reply{{Text: tr.Tf("msg.location.list_empty", bot.CommandPrefix(ctx))}}
	}

	var sb strings.Builder
	sb.WriteString(tr.T("msg.location.list_header") + "\n")
	if hasDefault {
		sb.WriteString(formatLocationRow(ctx, tr, "default", human.Latitude, human.Longitude) + "\n")
	}
	for _, l := range locs {
		sb.WriteString(formatLocationRow(ctx, tr, l.Label, l.Latitude, l.Longitude) + "\n")
	}
	return []bot.Reply{{Text: sb.String()}}
}

// formatLocationRow renders a single list row, with reverse-geocoded address
// appended when the geocoder is configured and lookup succeeds.
func formatLocationRow(ctx *bot.CommandContext, tr *i18n.Translator, label string, lat, lon float64) string {
	if ctx.Geocoder != nil {
		if a := ctx.Geocoder.GetAddress(lat, lon); a != nil && a.FormattedAddress != "" {
			return tr.Tf("msg.location.list_row_with_addr", label, lat, lon, a.FormattedAddress)
		}
	}
	return tr.Tf("msg.location.list_row", label, lat, lon)
}

// showLocation handles `!location show <name>` and `!location show default`.
func (c *LocationCommand) showLocation(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.show_usage", bot.CommandPrefix(ctx))}}
	}

	target := args[0]
	enTr := ctx.Translations.For("en")
	if isDefaultKeyword(target, tr, enTr) {
		human, _ := ctx.Humans.Get(ctx.TargetID)
		if human == nil || (human.Latitude == 0 && human.Longitude == 0) {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.location.no_default")}}
		}
		return formatShowReply(ctx, tr, "default", human.Latitude, human.Longitude)
	}

	loc, _ := ctx.Humans.GetLocation(ctx.TargetID, target)
	if loc == nil {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.show_not_found", target)}}
	}
	return formatShowReply(ctx, tr, loc.Label, loc.Latitude, loc.Longitude)
}

// formatShowReply builds a show-location reply with optional geocoded address
// and optional static map tile.
func formatShowReply(ctx *bot.CommandContext, tr *i18n.Translator, label string, lat, lon float64) []bot.Reply {
	var text string
	if ctx.Geocoder != nil {
		if a := ctx.Geocoder.GetAddress(lat, lon); a != nil && a.FormattedAddress != "" {
			text = tr.Tf("msg.location.show_with_addr", label, lat, lon, a.FormattedAddress)
		}
	}
	if text == "" {
		text = tr.Tf("msg.location.show", label, lat, lon)
	}
	reply := bot.Reply{Text: text}
	if ctx.StaticMap != nil {
		data := map[string]any{
			"latitude":  lat,
			"longitude": lon,
		}
		reply.ImageURL = ctx.StaticMap.GetPregeneratedTileURL("location", data, "staticMap")
	}
	return []bot.Reply{reply}
}

// removeLocation handles `!location remove <name>` and `!location remove default`.
func (c *LocationCommand) removeLocation(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.remove_usage", bot.CommandPrefix(ctx))}}
	}

	target := args[0]
	enTr := ctx.Translations.For("en")
	if isDefaultKeyword(target, tr, enTr) {
		if err := ctx.Humans.SetLocation(ctx.TargetID, ctx.ProfileNo, 0, 0); err != nil {
			return []bot.Reply{{React: "🙅"}}
		}
		ctx.TriggerReload()
		return []bot.Reply{{React: "✅", Text: tr.T("msg.location.default_removed")}}
	}

	refs, err := ctx.Humans.CountLocationReferences(ctx.TargetID, target)
	if err != nil {
		log.Errorf("location remove count refs: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	if len(refs) > 0 {
		var parts []string
		for _, r := range refs {
			parts = append(parts, fmt.Sprintf("%s id:%d", r.Type, r.UID))
		}
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.remove_referenced", target, len(refs), strings.Join(parts, ", "))}}
	}

	if err := ctx.Humans.DeleteLocation(ctx.TargetID, target); err != nil {
		log.Errorf("location remove delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.location.removed", target)}}
}

// isDefaultKeyword returns true if s matches the i18n translation for "default"
// (in the user's language), the English translation, or the literal string "default".
func isDefaultKeyword(s string, tr, enTr *i18n.Translator) bool {
	low := strings.ToLower(s)
	return low == strings.ToLower(tr.T("arg.default")) ||
		low == strings.ToLower(enTr.T("arg.default")) ||
		low == "default"
}

// resolveLatLon parses a "lat,lon" string or falls back to forward geocoding.
// Returns an error when neither succeeds.
func resolveLatLon(ctx *bot.CommandContext, s string) (float64, float64, error) {
	// Try lat,lon parsing first (same regex shape as ArgMatcher.tryLatLon).
	if idx := strings.Index(s, ","); idx != -1 {
		latStr := strings.TrimSpace(s[:idx])
		lonStr := strings.TrimSpace(s[idx+1:])
		lat, err1 := strconv.ParseFloat(latStr, 64)
		lon, err2 := strconv.ParseFloat(lonStr, 64)
		if err1 == nil && err2 == nil {
			return lat, lon, nil
		}
	}

	// Fall back to forward geocoding.
	if ctx.Geocoder != nil {
		results, err := ctx.Geocoder.Forward(s)
		if err == nil && len(results) > 0 {
			return results[0].Latitude, results[0].Longitude, nil
		}
	}

	return 0, 0, fmt.Errorf("could not resolve %q to coordinates", s)
}
