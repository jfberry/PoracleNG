package commands

import (
	"strconv"
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// paReload implements !poracle-admin reload with three subcommands:
//
//	!poracle-admin reload dts      — reload DTS templates and partials
//	!poracle-admin reload geofence — reload geofence files + full state
//	!poracle-admin reload state    — reload tracking rules + humans from MySQL only
var paReload = &paSubgroup{
	run:  paReloadRun,
	help: paReloadHelp,
}

func paReloadHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.reload.help.intro"))
	sb.WriteString("\n\n")
	sb.WriteString(tr.T("cmd.poracle_admin.reload.dts.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.reload.geofence.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.reload.state.desc"))

	return []bot.Reply{{Text: sb.String()}}
}

func paReloadRun(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 {
		return paReloadHelp(ctx)
	}

	sub := strings.ToLower(args[0])
	switch sub {
	case "help":
		return paReloadHelp(ctx)

	case "dts":
		if ctx.Admin == nil || ctx.Admin.ReloadDTS == nil {
			return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reload.error", "DTS reload not configured")}}
		}
		start := time.Now()
		count, err := ctx.Admin.ReloadDTS()
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reload.error", err.Error())}}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reload.dts.success",
			strconv.FormatInt(elapsed, 10),
			strconv.Itoa(count),
		)}}

	case "geofence":
		if ctx.Admin == nil || ctx.Admin.ReloadGeofence == nil {
			return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reload.error", "geofence reload not configured")}}
		}
		start := time.Now()
		err := ctx.Admin.ReloadGeofence()
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reload.error", err.Error())}}
		}
		// Read counts from state snapshot taken after the reload.
		fenceCount := 0
		trackingCount := 0
		if ctx.StateMgr != nil {
			if s := ctx.StateMgr.Get(); s != nil {
				fenceCount = len(s.Fences)
				trackingCount = countTrackingRules(s)
			}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reload.geofence.success",
			strconv.FormatInt(elapsed, 10),
			strconv.Itoa(fenceCount),
			strconv.Itoa(trackingCount),
		)}}

	case "state":
		if ctx.Admin == nil || ctx.Admin.ReloadState == nil {
			return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reload.error", "state reload not configured")}}
		}
		start := time.Now()
		err := ctx.Admin.ReloadState()
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reload.error", err.Error())}}
		}
		// Read counts from state snapshot taken after the reload.
		trackingCount := 0
		humanCount := 0
		if ctx.StateMgr != nil {
			if s := ctx.StateMgr.Get(); s != nil {
				trackingCount = countTrackingRules(s)
				humanCount = len(s.Humans)
			}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reload.state.success",
			strconv.FormatInt(elapsed, 10),
			strconv.Itoa(trackingCount),
			strconv.Itoa(humanCount),
		)}}

	default:
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.unknown_sub", "reload")}}
	}
}

// countTrackingRules is defined in poracle_admin_util.go and shared here.
