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
		replies = append(replies, bot.Reply{Text: tr.Tf("msg.location.usage", bot.CommandPrefix(ctx))})
		return replies
	}

	if help := helpArgReply(ctx, args, "msg.location.usage"); help != nil {
		return []bot.Reply{*help}
	}

	parsed := ctx.ArgMatcher.Match(args, locationParams, ctx.Language)

	// Remove location
	if parsed.HasKeyword("arg.remove") {
		if err := ctx.Humans.SetLocation(ctx.TargetID, ctx.ProfileNo, 0, 0); err != nil {
			return []bot.Reply{{React: "🙅"}}
		}
		ctx.TriggerReload()
		return []bot.Reply{{React: "✅", Text: tr.T("msg.location.removed")}}
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
