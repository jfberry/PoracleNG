package commands

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

type LocationCommand struct{}

func (c *LocationCommand) Name() string      { return "cmd.location" }
func (c *LocationCommand) Aliases() []string { return nil }

var locationParams = []bot.ParamDef{
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamLatLon},
}

func (c *LocationCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	parsed := ctx.ArgMatcher.Match(args, locationParams, ctx.Language)

	// Remove location
	if parsed.HasKeyword("arg.remove") {
		_, err := ctx.DB.Exec("UPDATE humans SET latitude = 0, longitude = 0 WHERE id = ?", ctx.TargetID)
		if err != nil {
			return []bot.Reply{{React: "🙅"}}
		}
		ctx.DB.Exec("UPDATE profiles SET latitude = 0, longitude = 0 WHERE id = ? AND profile_no = ?",
			ctx.TargetID, ctx.ProfileNo)
		return []bot.Reply{{React: "✅", Text: tr.T("cmd.location.removed")}}
	}

	var lat, lon float64

	if parsed.Coords != nil {
		lat = parsed.Coords.Lat
		lon = parsed.Coords.Lon
	} else if len(parsed.Unrecognized) > 0 && ctx.Geocoder != nil {
		// Try forward geocoding with unrecognized args as an address query
		query := strings.Join(parsed.Unrecognized, " ")
		results, err := ctx.Geocoder.Forward(query)
		if err != nil || len(results) == 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("cmd.location.not_found")}}
		}
		lat = results[0].Latitude
		lon = results[0].Longitude
	} else {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.location.specify")}}
	}

	// Set location
	_, err := ctx.DB.Exec("UPDATE humans SET latitude = ?, longitude = ? WHERE id = ?", lat, lon, ctx.TargetID)
	if err != nil {
		log.Errorf("location: update human: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.DB.Exec("UPDATE profiles SET latitude = ?, longitude = ? WHERE id = ? AND profile_no = ?",
		lat, lon, ctx.TargetID, ctx.ProfileNo)

	mapLink := fmt.Sprintf("https://maps.google.com/maps?q=%f,%f", lat, lon)
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.location.set", lat, lon) + "\n" + mapLink}}
}
