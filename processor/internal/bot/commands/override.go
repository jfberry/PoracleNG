package commands

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// Override holds the resolved per-rule override fields ready to drop onto a
// tracking insert struct. Empty fields mean "no override".
type Override struct {
	// LocationLabel is the stored-case label of the saved location to use as
	// this rule's origin. Non-empty only when the user specified location:Name.
	LocationLabel string
	// Areas is the list of area names to scope this rule to instead of the
	// user's profile areas. Non-empty only when the user specified area:X.
	Areas []string
}

// parseOverride validates the per-rule location:/area:/d: combination from
// parsed args and returns a populated Override or a blocking Reply.
//
// The four mutually-exclusive combinations from the spec are:
//
//  1. location: AND area: → REJECT (mutually exclusive)
//  2. area: AND distance > 0 → REJECT (mutually exclusive)
//  3. location: AND distance == 0 → REJECT (location requires d:)
//  4. Otherwise valid — validate label exists and each area is permitted.
//
// strs should be parsed.Strings (e.g. {"location": "Home"}).
// areas should be parsed.StringLists["area"] (may be nil).
// distance is the already-parsed d: value (0 = not set).
func parseOverride(ctx *bot.CommandContext, strs map[string]string, areas []string, distance int) (Override, *bot.Reply) {
	tr := ctx.Tr()

	var locLabel string
	if strs != nil {
		locLabel = strs["location"]
	}

	hasLocation := locLabel != ""
	hasAreas := len(areas) > 0

	// Rule 1: location: AND area: are mutually exclusive.
	if hasLocation && hasAreas {
		return Override{}, &bot.Reply{
			React: "🙅",
			Text:  tr.T("msg.override.area_and_location"),
		}
	}

	// Rule 2: area: AND distance > 0 are mutually exclusive.
	if hasAreas && distance > 0 {
		return Override{}, &bot.Reply{
			React: "🙅",
			Text:  tr.T("msg.override.area_and_distance"),
		}
	}

	// Rule 3: location: requires a distance.
	if hasLocation && distance == 0 {
		return Override{}, &bot.Reply{
			React: "🙅",
			Text:  tr.T("msg.override.requires_distance"),
		}
	}

	out := Override{}

	if hasLocation {
		loc, _ := ctx.Humans.GetLocation(ctx.TargetID, locLabel)
		if loc == nil {
			return Override{}, &bot.Reply{
				React: "🙅",
				Text:  tr.Tf("msg.override.unknown_location", locLabel, bot.CommandPrefix(ctx)),
			}
		}
		out.LocationLabel = loc.Label // normalize to stored case
	}

	if hasAreas {
		h := getUserHuman(ctx)
		communities := humanCommunities(ctx, h)
		available := ctx.AreaLogic.GetAvailableAreas(communities, ctx.IsAdmin)

		// Build a lowercase set for O(1) membership test.
		permitted := make(map[string]bool, len(available))
		for _, a := range available {
			permitted[strings.ToLower(a.Name)] = true
		}

		for _, a := range areas {
			if !permitted[strings.ToLower(a)] {
				return Override{}, &bot.Reply{
					React: "🙅",
					Text:  tr.Tf("msg.override.area_not_permitted", a, bot.CommandPrefix(ctx)),
				}
			}
		}
		for _, a := range areas {
			out.Areas = append(out.Areas, strings.ToLower(strings.ReplaceAll(a, "_", " ")))
		}
	}

	return out, nil
}
