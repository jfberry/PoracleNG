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
var paStatus = &paSubgroup{
	run:  paStatusRun,
	help: paStatusHelp,
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
