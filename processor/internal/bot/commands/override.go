package commands

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/geofence"
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
// locationLabel should be parsed.Strings["location"] (empty = not set).
// areas should be parsed.StringLists["area"] (may be nil).
// distance is the already-parsed d: value (0 = not set).
func parseOverride(ctx *bot.CommandContext, locationLabel string, areas []string, distance int) (Override, *bot.Reply) {
	tr := ctx.Tr()

	hasLocation := locationLabel != ""
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
		loc, _ := ctx.Humans.GetLocation(ctx.TargetID, locationLabel)
		if loc == nil {
			return Override{}, &bot.Reply{
				React: "🙅",
				Text:  tr.Tf("msg.override.unknown_location", locationLabel, bot.CommandPrefix(ctx)),
			}
		}
		out.LocationLabel = loc.Label // normalize to stored case
	}

	if hasAreas {
		h := getUserHuman(ctx)
		communities := humanCommunities(ctx, h)
		available := ctx.AreaLogic.GetAvailableAreas(communities, ctx.IsAdmin)

		// Key the permitted fences by NormalizeAreaKey so either spelling of
		// a fence name resolves (the parser has already turned unquoted
		// underscores into spaces), and remember each fence's own spelling,
		// which is what gets stored.
		canonical := make(map[string]string, len(available))
		for _, a := range available {
			canonical[geofence.NormalizeAreaKey(a.Name)] = strings.ToLower(a.Name)
		}

		for _, a := range areas {
			if _, ok := canonical[geofence.NormalizeAreaKey(a)]; !ok {
				return Override{}, &bot.Reply{
					React: "🙅",
					Text:  tr.Tf("msg.override.area_not_permitted", a, bot.CommandPrefix(ctx)),
				}
			}
		}
		// Repeated `area:` params and comma lists both accumulate, so the
		// same area can arrive twice ("area:london area:paris,london").
		// Store each one once, in the order the user first named it.
		seen := make(map[string]bool, len(areas))
		for _, a := range areas {
			key := geofence.NormalizeAreaKey(a)
			if seen[key] {
				continue
			}
			seen[key] = true
			out.Areas = append(out.Areas, canonical[key])
		}
	}

	return out, nil
}

// limitExempt reports whether [tracking] max_distance / max_areas are lifted
// for this command: the sender is an admin, or the target is a channel,
// webhook, group or thread (destinations only admins configure).
func limitExempt(ctx *bot.CommandContext) bool {
	return bot.LimitExempt(ctx.IsAdmin, ctx.TargetType)
}

// overrideLimitReply refuses a tracking write whose area: list breaks
// [tracking] max_areas, judged on the diff the write is about to apply (see
// bot.OverrideAreaLimitBreached). Call it just before store.ApplyDiff with
// the same rows. Returns nil when the write may go ahead.
func overrideLimitReply[T any](ctx *bot.CommandContext, tracked, insert []T,
	getUID func(*T) int64, setUID func(*T, int64)) []bot.Reply {
	if !bot.OverrideAreaLimitBreached(ctx.Config, limitExempt(ctx), tracked, insert, getUID, setUID) {
		return nil
	}
	return []bot.Reply{{React: "🙅", Text: ctx.Tr().Tf("msg.override.too_many_areas", ctx.Config.Tracking.MaxAreas)}}
}
