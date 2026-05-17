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
// Each subgroup is declared as a package-level var in its own file
// (poracle_admin_reload.go, etc.) and referenced here.
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
	"config":      paConfig,
	"warnings":    paWarnings,
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
	"config",
	"warnings",
}

func (c *PoracleAdminCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Admin gate — text refusal (not 🙅 react, which is reserved for
	// command_security role denials).
	if !ctx.IsAdmin {
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
//
// Output structure:
//  1. cmd.poracle_admin.help.admin_only  (admin-only banner)
//  2. blank line
//  3. cmd.poracle_admin.help_intro       (existing emoji + title line)
//  4. blank line
//  5. cmd.poracle_admin.help.groups      (subgroups header)
//  6. one line per subgroup with its description
//
// Note for real subgroup implementations: when a subgroup receives subcommand
// args that it does not recognise, it should emit cmd.poracle_admin.unknown_sub
// with the subgroup name as {0}, e.g.:
//
//	tr.Tf("cmd.poracle_admin.unknown_sub", subgroupName)
func (c *PoracleAdminCommand) topLevelHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.help.admin_only"))
	sb.WriteString("\n\n")
	sb.WriteString(tr.T("cmd.poracle_admin.help_intro"))
	sb.WriteString("\n\n")
	sb.WriteString(tr.T("cmd.poracle_admin.help.groups"))
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

// paEmoji is declared in poracle_admin_emoji.go.
// paReconcile is declared in poracle_admin_reconcile.go.
// paRatelimit is declared in poracle_admin_ratelimit.go.
// paSummary is declared in poracle_admin_summary.go.

var paMaintenance = &paSubgroup{
	run:  paStubRun,
	help: paStubHelp("maintenance"),
}
