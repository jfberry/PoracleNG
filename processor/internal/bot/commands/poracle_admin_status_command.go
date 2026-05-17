package commands

import (
	"github.com/pokemon/poracleng/processor/internal/bot"
)

// paStatus implements !poracle-admin status.
//
// Usage:
//
//	!poracle-admin status           — current health snapshot (concise)
//	!poracle-admin status -v        — verbose: per-route Discord detail + full type breakdown
//	!poracle-admin status --verbose — same as -v
//	!poracle-admin status verbose   — same as -v
//	!poracle-admin status help      — show this help text
//
// The help hook renders the snapshot (not help text) so that typing just
// "!poracle-admin status" immediately returns the useful output. The
// explicit "help" sub-subcommand still reaches the introductory text.
// This mirrors the operator-pragmatic pattern used by paMaintenance.
var paStatus = &paSubgroup{
	run:  paStatusRun,
	help: paStatusHelpAsReport,
}

// paStatusHelpAsReport is the paSubgroup.help hook — called when the user
// types `!poracle-admin status` with no further args. Renders the full
// snapshot rather than the help text so the most useful output is always
// one word away. Matches the pattern established by paMaintenance.
func paStatusHelpAsReport(ctx *bot.CommandContext) []bot.Reply {
	return statusReport(ctx, false)
}

func paStatusRun(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	verbose := false
	if len(args) > 0 {
		switch args[0] {
		case "help":
			return paStatusHelp(ctx)
		case "-v", "--verbose", "verbose":
			verbose = true
		default:
			return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.unknown_sub", "status")}}
		}
	}
	return statusReport(ctx, verbose)
}

func paStatusHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	return []bot.Reply{{Text: tr.T("cmd.poracle_admin.status.help.intro")}}
}
