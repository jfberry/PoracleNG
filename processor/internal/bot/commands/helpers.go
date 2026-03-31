package commands

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
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
			ctx.Config.Discord.Prefix, tr.T("tracking.tracked"))
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
