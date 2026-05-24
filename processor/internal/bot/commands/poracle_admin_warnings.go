package commands

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/logbuffer"
)

// paWarnings implements !poracle-admin warnings — in-memory WARN/ERROR log buffer
// inspection.
//
// The framework calls help() when the user types just "!poracle-admin warnings"
// (no additional arguments). help() renders the full buffer output — startup and
// recent sections — which is the most useful default for this subgroup.
//
// Subcommands (passed to run):
//
//	help    — same as the framework help view (full buffer render)
//	startup — show only the startup buffer
//	recent  — show only the rolling buffer
//	clear   — empty the rolling buffer (startup is immutable post-MarkStartupComplete)
var paWarnings = &paSubgroup{
	run:  paWarningsRun,
	help: paWarningsHelp,
}

// paWarningsHelp is invoked by the framework when the user types
// "!poracle-admin warnings" with no further arguments. It renders both the
// startup and recent sections — the full live view.
func paWarningsHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	if ctx.Admin == nil || ctx.Admin.LogBuffer == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.warnings.not_configured")}}
	}

	return paWarningsBoth(ctx)
}

func paWarningsRun(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) > 0 && args[0] == "help" {
		return paWarningsHelpText(ctx)
	}

	if ctx.Admin == nil || ctx.Admin.LogBuffer == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.warnings.not_configured")}}
	}

	if len(args) == 0 {
		return paWarningsBoth(ctx)
	}

	switch strings.ToLower(args[0]) {
	case "startup":
		return paWarningsStartup(ctx)
	case "recent":
		return paWarningsRecent(ctx)
	case "clear":
		return paWarningsClear(ctx)
	default:
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.unknown_sub", "warnings")}}
	}
}

// paWarningsHelpText renders the static help text listing available subcommands.
func paWarningsHelpText(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.warnings.help.intro"))
	sb.WriteString("\n\n")
	sb.WriteString(tr.T("cmd.poracle_admin.warnings.startup.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.warnings.recent.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.warnings.clear.desc"))
	return []bot.Reply{{Text: sb.String()}}
}

// paWarningsBoth renders both the startup and recent sections.
func paWarningsBoth(ctx *bot.CommandContext) []bot.Reply {
	var sb strings.Builder
	sb.WriteString(renderWarningsSection(ctx, ctx.Admin.LogBuffer.Startup(), "startup"))
	sb.WriteString("\n\n")
	sb.WriteString(renderWarningsSection(ctx, ctx.Admin.LogBuffer.Recent(), "recent"))
	return bot.SplitTextReply(sb.String())
}

// paWarningsStartup renders only the startup buffer.
func paWarningsStartup(ctx *bot.CommandContext) []bot.Reply {
	text := renderWarningsSection(ctx, ctx.Admin.LogBuffer.Startup(), "startup")
	return bot.SplitTextReply(text)
}

// paWarningsRecent renders only the rolling buffer.
func paWarningsRecent(ctx *bot.CommandContext) []bot.Reply {
	text := renderWarningsSection(ctx, ctx.Admin.LogBuffer.Recent(), "recent")
	return bot.SplitTextReply(text)
}

// paWarningsClear empties the rolling buffer and reports the count cleared.
func paWarningsClear(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	// Capture the count before clearing — ClearRecent returns void.
	count := len(ctx.Admin.LogBuffer.Recent())
	ctx.Admin.LogBuffer.ClearRecent()
	return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.warnings.cleared", count)}}
}

// renderWarningsSection builds the display text for one buffer section ("startup"
// or "recent"). Returns the header + rows, or the appropriate empty message.
func renderWarningsSection(ctx *bot.CommandContext, entries []logbuffer.Entry, section string) string {
	tr := ctx.Tr()

	if len(entries) == 0 {
		emptyKey := "cmd.poracle_admin.warnings.empty." + section
		return tr.T(emptyKey)
	}

	var sb strings.Builder
	headerKey := "cmd.poracle_admin.warnings.section." + section
	sb.WriteString(tr.Tf(headerKey, len(entries)))
	for _, e := range entries {
		sb.WriteByte('\n')
		sb.WriteString(formatWarningEntry(tr, e))
	}
	return sb.String()
}

// warningsTimeLayout is the timestamp format used for warning entries.
// RFC3339 without timezone suffix: "2006-01-02 15:04:05".
const warningsTimeLayout = "2006-01-02 15:04:05"

// formatWarningEntry renders one log buffer entry as an indented row.
// If the entry has a Source, appends "[source]" after the message.
func formatWarningEntry(tr interface{ Tf(string, ...any) string }, e logbuffer.Entry) string {
	ts := e.Time.UTC().Format(warningsTimeLayout)
	source := ""
	if e.Source != "" {
		source = "[" + e.Source + "]"
	}
	return tr.Tf("cmd.poracle_admin.warnings.entry_row", ts, e.Level, e.Message, source)
}

// formatDuration is defined in poracle_admin_util.go and shared here.
