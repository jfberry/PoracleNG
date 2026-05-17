package commands

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
	reconcilesentinel "github.com/pokemon/poracleng/processor/internal/discordbot/reconcile"
)

// paReconcile implements !poracle-admin reconcile — immediate Discord role
// reconciliation without waiting for the periodic timer.
//
// Subcommands:
//
//	run         — reconcile all guild members now (blocks until complete)
//	user <id>   — reconcile a single Discord user by snowflake ID
var paReconcile = &paSubgroup{
	run:  paReconcileRun,
	help: paReconcileHelp,
}

func paReconcileHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.reconcile.help.intro"))
	sb.WriteString("\n\n")
	sb.WriteString(tr.T("cmd.poracle_admin.reconcile.run.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.reconcile.user.desc"))

	return []bot.Reply{{Text: sb.String()}}
}

func paReconcileRun(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 || args[0] == "help" {
		return paReconcileHelp(ctx)
	}

	switch strings.ToLower(args[0]) {
	case "run":
		return paReconcileRunAll(ctx)
	case "user":
		return paReconcileUser(ctx, args[1:])
	default:
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.unknown_sub", "reconcile")}}
	}
}

// paReconcileRunAll triggers a full Discord role reconciliation immediately.
func paReconcileRunAll(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	start := time.Now()
	err := ctx.RunReconcile()
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		if errors.Is(err, reconcilesentinel.ErrDisabled) {
			return []bot.Reply{{Text: tr.T("cmd.poracle_admin.reconcile.not_configured")}}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reconcile.run.error", err.Error())}}
	}

	return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reconcile.run.success",
		fmt.Sprintf("%d", elapsed),
	)}}
}

// paReconcileUser reconciles a single Discord user by snowflake ID.
func paReconcileUser(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 || args[0] == "" {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.reconcile.user.usage")}}
	}

	userID := args[0]

	start := time.Now()
	err := ctx.Reconciler(userID)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		if errors.Is(err, reconcilesentinel.ErrDisabled) {
			return []bot.Reply{{Text: tr.T("cmd.poracle_admin.reconcile.not_configured")}}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reconcile.user.error",
			userID, err.Error(),
		)}}
	}

	return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.reconcile.user.success",
		userID,
		fmt.Sprintf("%d", elapsed),
	)}}
}
