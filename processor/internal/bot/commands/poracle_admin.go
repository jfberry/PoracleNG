package commands

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// PoracleAdminCommand implements !poracle-admin (alias !pa) — a top-level
// admin-only dispatcher for live-operations subgroups.
//
// Usage:
//
//	!poracle-admin              — list available subgroups
//	!poracle-admin <group>      — show that group's help
//	!poracle-admin <group> ...  — run a command within that group
type PoracleAdminCommand struct{}

func (c *PoracleAdminCommand) Name() string      { return "cmd.poracle_admin" }
func (c *PoracleAdminCommand) Aliases() []string { return []string{"cmd.pa"} }

// paSubgroup is the interface for a named admin subgroup.
type paSubgroup struct {
	run  func(ctx *bot.CommandContext, args []string) []bot.Reply
	help func(ctx *bot.CommandContext) []bot.Reply
}

// paSubgroups is the canonical dispatch table for !poracle-admin subgroups.
// Keys are the short English names users type on the command line.
var paSubgroups = map[string]*paSubgroup{
	"slash":       paSlash,
	"reload":      paReload,
	"emoji":       paEmoji,
	"reconcile":   paReconcile,
	"cache":       paCache,
	"ratelimit":   paRatelimit,
	"summary":     paSummary,
	"status":      paStatus,
	"maintenance": paMaintenance,
}

// paSubgroupOrder controls the display order in the top-level help listing.
var paSubgroupOrder = []string{
	"slash",
	"reload",
	"emoji",
	"reconcile",
	"cache",
	"ratelimit",
	"summary",
	"status",
	"maintenance",
}

func (c *PoracleAdminCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Admin gate — text refusal (not 🙅 react, which is reserved for
	// command_security role denials).
	if !bot.IsAdmin(ctx.Config, ctx.Platform, ctx.UserID) {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.not_admin")}}
	}

	// No subgroup specified — return the top-level help listing.
	if len(args) == 0 {
		return c.topLevelHelp(ctx)
	}

	group := strings.ToLower(args[0])
	sg, ok := paSubgroups[group]
	if !ok {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.unknown_group")}}
	}

	// Subgroup name only — show that group's help.
	if len(args) == 1 {
		return sg.help(ctx)
	}

	// Subgroup name + arguments — run the subgroup.
	return sg.run(ctx, args[1:])
}

// topLevelHelp renders the listing of all nine subgroups with their one-line
// descriptions.
func (c *PoracleAdminCommand) topLevelHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.help_intro"))
	sb.WriteString("\n")
	for _, name := range paSubgroupOrder {
		descKey := "cmd.poracle_admin.group." + name + ".desc"
		sb.WriteString("\n  **")
		sb.WriteString(name)
		sb.WriteString("** — ")
		sb.WriteString(tr.T(descKey))
	}

	return []bot.Reply{{Text: sb.String()}}
}

// ---------------------------------------------------------------------------
// Stub subgroup definitions — each will be replaced in a later task.
// ---------------------------------------------------------------------------

func paStubRun(ctx *bot.CommandContext, _ []string) []bot.Reply {
	tr := ctx.Tr()
	return []bot.Reply{{Text: tr.T("cmd.poracle_admin.stub")}}
}

func paStubHelp(name string) func(ctx *bot.CommandContext) []bot.Reply {
	return func(ctx *bot.CommandContext) []bot.Reply {
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin." + name + ".help_stub")}}
	}
}

var paSlash = &paSubgroup{
	run:  paStubRun,
	help: paStubHelp("slash"),
}

var paReload = &paSubgroup{
	run:  paStubRun,
	help: paStubHelp("reload"),
}

var paEmoji = &paSubgroup{
	run:  paStubRun,
	help: paStubHelp("emoji"),
}

var paReconcile = &paSubgroup{
	run:  paStubRun,
	help: paStubHelp("reconcile"),
}

var paCache = &paSubgroup{
	run:  paStubRun,
	help: paStubHelp("cache"),
}

var paRatelimit = &paSubgroup{
	run:  paStubRun,
	help: paStubHelp("ratelimit"),
}

var paSummary = &paSubgroup{
	run:  paStubRun,
	help: paStubHelp("summary"),
}

var paStatus = &paSubgroup{
	run:  paStubRun,
	help: paStubHelp("status"),
}

var paMaintenance = &paSubgroup{
	run:  paStubRun,
	help: paStubHelp("maintenance"),
}
