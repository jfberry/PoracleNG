package commands

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// commandPrefix returns the appropriate command prefix for the platform.
func commandPrefix(ctx *bot.CommandContext) string {
	if ctx.Platform == "telegram" {
		return "/"
	}
	prefix := ctx.Config.Discord.Prefix
	if prefix == "" {
		return "!"
	}
	return prefix
}

// usageReply returns a usage help reply if args are empty, or nil if args are present.
func usageReply(ctx *bot.CommandContext, args []string, usageKey string) *bot.Reply {
	if len(args) > 0 {
		return nil
	}
	tr := ctx.Tr()
	return &bot.Reply{Text: tr.Tf(usageKey, commandPrefix(ctx))}
}

// helpArgReply returns a usage reply if the first argument is "help", or nil otherwise.
func helpArgReply(ctx *bot.CommandContext, args []string, usageKey string) *bot.Reply {
	if len(args) > 0 && args[0] == "help" {
		tr := ctx.Tr()
		return &bot.Reply{Text: tr.Tf(usageKey, commandPrefix(ctx))}
	}
	return nil
}

// extractPings removes Discord @mention tokens (<@123456789> or <@!123456789>)
// from args and returns the combined ping string and remaining args.
func extractPings(args []string) (pings string, remaining []string) {
	var pingParts []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "<@") && strings.HasSuffix(arg, ">") {
			pingParts = append(pingParts, arg)
		} else {
			remaining = append(remaining, arg)
		}
	}
	return strings.Join(pingParts, " "), remaining
}

// enforceDistance applies default and max distance limits from config.
func enforceDistance(ctx *bot.CommandContext, distance int) int {
	if distance == 0 && ctx.Config.Tracking.DefaultDistance > 0 {
		distance = ctx.Config.Tracking.DefaultDistance
	}
	if ctx.Config.Tracking.MaxDistance > 0 && distance > ctx.Config.Tracking.MaxDistance {
		distance = ctx.Config.Tracking.MaxDistance
	}
	return distance
}

// trackingWarnings generates warning messages for common tracking issues.
// Returns a string to append to the reply, or empty if no warnings.
// Should be called by all tracking commands after successful tracking changes.
func trackingWarnings(ctx *bot.CommandContext, distance int) string {
	tr := ctx.Tr()
	var warnings []string

	// Check if user has stopped alerts
	if ctx.Humans != nil {
		if h, err := ctx.Humans.Get(ctx.TargetID); err == nil && h != nil && !h.Enabled {
			prefix := commandPrefix(ctx)
			warnings = append(warnings, tr.Tf("tracking.warn_stopped", prefix))
		}
	}

	// Check distance with no location
	if distance > 0 && !ctx.HasLocation {
		warnings = append(warnings, tr.T("tracking.warn_no_location"))
	}

	// Check no distance and no areas
	if distance == 0 && !ctx.HasArea {
		warnings = append(warnings, tr.T("tracking.warn_no_area"))
	}

	// Check max distance was applied
	if ctx.Config.Tracking.MaxDistance > 0 && distance > 0 && distance >= ctx.Config.Tracking.MaxDistance {
		warnings = append(warnings, tr.Tf("tracking.warn_max_distance", ctx.Config.Tracking.MaxDistance))
	}

	if len(warnings) == 0 {
		return ""
	}
	return "\n⚠️ " + strings.Join(warnings, "\n⚠️ ")
}

// resolveMoveByName looks up a move ID by its translated name.
// Returns bot.WildcardID if no match is found.
func resolveMoveByName(ctx *bot.CommandContext, moveName string) int {
	if ctx.GameData == nil {
		return bot.WildcardID
	}
	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")
	for id := range ctx.GameData.Moves {
		key := gamedata.MoveTranslationKey(id)
		if strings.EqualFold(tr.T(key), moveName) || strings.EqualFold(enTr.T(key), moveName) {
			return id
		}
	}
	return bot.WildcardID
}

// buildTrackingMessage generates the confirmation message for tracking mutations.
// The rowFunc callbacks produce row text for each entry by index.
func buildTrackingMessage(
	tr *i18n.Translator,
	ctx *bot.CommandContext,
	unchangedCount, updateCount, insertCount int,
	unchangedRow func(i int) string,
	updateRow func(i int) string,
	insertRow func(i int) string,
) string {
	total := unchangedCount + updateCount + insertCount
	if total > 20 {
		return tr.Tf("tracking.bulk_changes",
			commandPrefix(ctx), tr.T("tracking.tracked"))
	}

	var sb strings.Builder
	for i := 0; i < unchangedCount; i++ {
		sb.WriteString(tr.T("tracking.unchanged"))
		sb.WriteString(unchangedRow(i))
		sb.WriteByte('\n')
	}
	for i := 0; i < updateCount; i++ {
		sb.WriteString(tr.T("tracking.updated"))
		sb.WriteString(updateRow(i))
		sb.WriteByte('\n')
	}
	for i := 0; i < insertCount; i++ {
		sb.WriteString(tr.T("tracking.new"))
		sb.WriteString(insertRow(i))
		sb.WriteByte('\n')
	}
	return sb.String()
}
