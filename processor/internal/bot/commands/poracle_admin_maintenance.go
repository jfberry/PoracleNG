package commands

import (
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// paMaintenance implements !poracle-admin maintenance — live control over
// the dispatcher pause primitive.
//
// Subcommands:
//
//	(no arg) → status (most common, idempotent)
//	help     → group help
//	pause [reason words...] → call Dispatcher.Pause(reason)
//	resume   → call Dispatcher.Resume()
//	status   → render current paused/running state
//
// Note: the `help` hook on paSubgroup is called when the user types just
// `!poracle-admin maintenance` (no further args). For this subgroup we
// redirect that to status since it's the most useful idempotent op.
// The actual help text is reached via `!poracle-admin maintenance help`.
var paMaintenance = &paSubgroup{
	run:  paMaintenanceRun,
	help: paMaintenanceHelpAsStatus,
}

// paMaintenanceHelpAsStatus is the paSubgroup.help hook — called when the user
// types `!poracle-admin maintenance` with no further args. Delegates to status
// rather than showing command syntax, matching the task spec: "(no arg) → status".
func paMaintenanceHelpAsStatus(ctx *bot.CommandContext) []bot.Reply {
	if ctx.Dispatcher == nil {
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.maintenance.no_dispatcher")}}
	}
	return paMaintenanceStatus(ctx)
}

// paMaintenanceShowHelp renders the actual subgroup help text.
func paMaintenanceShowHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.maintenance.help.intro"))
	sb.WriteString("\n\n")
	sb.WriteString(tr.T("cmd.poracle_admin.maintenance.pause.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.maintenance.resume.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.maintenance.status.desc"))
	return []bot.Reply{{Text: sb.String()}}
}

func paMaintenanceRun(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// No dispatcher wired — graceful fallback for all subcommands.
	if ctx.Dispatcher == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.maintenance.no_dispatcher")}}
	}

	// No arg → status (most common, idempotent).
	if len(args) == 0 {
		return paMaintenanceStatus(ctx)
	}

	switch strings.ToLower(args[0]) {
	case "help":
		return paMaintenanceShowHelp(ctx)
	case "pause":
		return paMaintenancePause(ctx, args[1:])
	case "resume":
		return paMaintenanceResume(ctx)
	case "status":
		return paMaintenanceStatus(ctx)
	default:
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.unknown_sub", "maintenance")}}
	}
}

// paMaintenancePause handles !poracle-admin maintenance pause [reason...]
func paMaintenancePause(ctx *bot.CommandContext, reasonArgs []string) []bot.Reply {
	tr := ctx.Tr()

	// Check before calling Pause — reading PauseState first avoids any
	// subtle ordering issue with the idempotency semantics.
	paused, existingReason, existingSince := ctx.Dispatcher.PauseState()
	if paused {
		d := time.Since(existingSince).Round(time.Second)
		ts := existingSince.UTC().Format("2006-01-02 15:04:05 UTC")
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.maintenance.pause.already",
			ts, formatDuration(d), existingReason)}}
	}

	reason := strings.Join(reasonArgs, " ")
	if reason == "" {
		reason = tr.T("cmd.poracle_admin.maintenance.no_reason")
	}

	ctx.Dispatcher.Pause(reason)

	var sb strings.Builder
	sb.WriteString(tr.Tf("cmd.poracle_admin.maintenance.pause.new", reason))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.maintenance.pause.detail"))
	return []bot.Reply{{Text: sb.String()}}
}

// paMaintenanceResume handles !poracle-admin maintenance resume
func paMaintenanceResume(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	paused, reason, since := ctx.Dispatcher.PauseState()
	if !paused {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.maintenance.resume.not_paused")}}
	}

	d := time.Since(since).Round(time.Second)
	ctx.Dispatcher.Resume()

	var sb strings.Builder
	sb.WriteString(tr.Tf("cmd.poracle_admin.maintenance.resume.success", formatDuration(d), reason))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.maintenance.resume.detail"))
	return []bot.Reply{{Text: sb.String()}}
}

// paMaintenanceStatus handles !poracle-admin maintenance status (and no-arg)
func paMaintenanceStatus(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	paused, reason, since := ctx.Dispatcher.PauseState()
	if !paused {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.maintenance.status.running")}}
	}

	d := time.Since(since).Round(time.Second)
	var sb strings.Builder
	sb.WriteString(tr.Tf("cmd.poracle_admin.maintenance.status.paused", reason, formatDuration(d)))

	// Queue depth — per-platform breakdown via dispatcher accessors.
	discord := ctx.Dispatcher.DiscordDepth()
	telegram := ctx.Dispatcher.TelegramDepth()
	total := ctx.Dispatcher.QueueDepth()
	sb.WriteString("\n")
	sb.WriteString(tr.Tf("cmd.poracle_admin.maintenance.status.queued",
		total, discord, telegram))

	return []bot.Reply{{Text: sb.String()}}
}
