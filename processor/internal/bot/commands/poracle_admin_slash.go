package commands

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// paSlash implements !poracle-admin slash — live slash-command registration
// lifecycle management.
//
// Subcommands:
//
//	sync          — push the current command set (fingerprint-cached)
//	force-resync  — clear fingerprint cache, then push unconditionally
//	clear-global  — remove all globally-registered Discord commands
//	clear-guild <guild_id>  — remove guild-scoped commands from one guild
//	status        — show last sync timestamp + short fingerprint per scope
//	list          — list registered commands as Discord command mentions

// paSlash wires the real implementation into the dispatch table in
// poracle_admin.go. The stub that used to live there has been removed.
var paSlash = &paSubgroup{
	run:  paSlashRun,
	help: paSlashHelp,
}

func paSlashHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.slash.help.intro"))
	sb.WriteString("\n")
	for _, sub := range []string{"sync", "force-resync", "clear-global", "clear-guild", "status", "list"} {
		key := "cmd.poracle_admin.slash." + strings.ReplaceAll(sub, "-", "_") + ".desc"
		sb.WriteString("\n  **")
		sb.WriteString(sub)
		sb.WriteString("** — ")
		sb.WriteString(tr.T(key))
	}

	return []bot.Reply{{Text: sb.String()}}
}

func paSlashRun(ctx *bot.CommandContext, args []string) []bot.Reply {
	if len(args) == 0 || args[0] == "help" {
		return paSlashHelp(ctx)
	}

	// Telegram cannot manage Discord slash commands.
	if ctx.Platform == "telegram" {
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.discord_only")}}
	}

	tr := ctx.Tr()
	switch args[0] {
	case "sync":
		return runSlashSync(ctx, tr)
	case "force-resync":
		return runSlashForceResync(ctx, tr)
	case "clear-global":
		return runSlashClearGlobal(ctx, tr)
	case "clear-guild":
		return runSlashClearGuild(ctx, tr, args[1:])
	case "status":
		return runSlashStatus(ctx, tr)
	case "list":
		return runSlashList(ctx, tr)
	default:
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.unknown_sub", "slash")}}
	}
}

func runSlashSync(ctx *bot.CommandContext, tr *i18n.Translator) []bot.Reply {
	if ctx.Admin == nil || ctx.Admin.SlashSync == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
	}
	start := time.Now()
	err := ctx.Admin.SlashSync()
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		if errors.Is(err, slash.ErrSlashNotConfigured) {
			return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.slash.sync.error", err.Error())}}
	}
	return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.slash.sync.success",
		fmt.Sprintf("%d", elapsed),
	)}}
}

func runSlashForceResync(ctx *bot.CommandContext, tr *i18n.Translator) []bot.Reply {
	if ctx.Admin == nil || ctx.Admin.SlashForceResync == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
	}
	start := time.Now()
	err := ctx.Admin.SlashForceResync()
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		if errors.Is(err, slash.ErrSlashNotConfigured) {
			return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.slash.force_resync.error", err.Error())}}
	}
	return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.slash.force_resync.success",
		fmt.Sprintf("%d", elapsed),
	)}}
}

func runSlashClearGlobal(ctx *bot.CommandContext, tr *i18n.Translator) []bot.Reply {
	if ctx.Admin == nil || ctx.Admin.SlashClearGlobal == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
	}
	err := ctx.Admin.SlashClearGlobal()
	if err != nil {
		if errors.Is(err, slash.ErrSlashNotConfigured) {
			return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.slash.clear_global.error", err.Error())}}
	}
	return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.clear_global.success")}}
}

func runSlashClearGuild(ctx *bot.CommandContext, tr *i18n.Translator, args []string) []bot.Reply {
	if len(args) == 0 || args[0] == "" {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.clear_guild.needs_arg")}}
	}
	guildID := args[0]
	if ctx.Admin == nil || ctx.Admin.SlashClearGuild == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
	}
	err := ctx.Admin.SlashClearGuild(guildID)
	if err != nil {
		if errors.Is(err, slash.ErrSlashNotConfigured) {
			return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.slash.clear_guild.error", err.Error())}}
	}
	return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.slash.clear_guild.success", guildID)}}
}

// runSlashList renders every registered command as a Discord command mention
// alongside its raw syntax. Both forms are deliberate: the rendered mention
// proves the id actually resolves, while the fenced copy is the only way to
// get the syntax back out of Discord for reuse in an announcement or a DTS
// template (a rendered mention cannot be copied as text).
func runSlashList(ctx *bot.CommandContext, tr *i18n.Translator) []bot.Reply {
	if ctx.Admin == nil || ctx.Admin.SlashList == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
	}
	scopes, err := ctx.Admin.SlashList(ctx.GuildID)
	if err != nil {
		if errors.Is(err, slash.ErrSlashNotConfigured) {
			return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.slash.list.error", err.Error())}}
	}

	var sb strings.Builder
	total := 0
	for _, scope := range scopes {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(tr.Tf("cmd.poracle_admin.slash.list.scope_header",
			scope.Name, fmt.Sprintf("%d", len(scope.Commands))))
		sb.WriteString("\n")
		if len(scope.Commands) == 0 {
			sb.WriteString(tr.T("cmd.poracle_admin.slash.list.scope_empty"))
			sb.WriteString("\n")
			continue
		}
		for _, c := range scope.Commands {
			total++
			mention := c.Mention()
			// Rendered mention, then the same string fenced so it survives
			// as copyable text.
			fmt.Fprintf(&sb, "%s  `%s`\n", mention, mention)
		}
	}

	if total == 0 && len(scopes) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.list.none")}}
	}

	// A full command set runs to ~37 mentions rendered twice over, which
	// comfortably exceeds Discord's per-message limit — fall back to a file
	// the same way !tracked does.
	return bot.SplitOrAttachReply(
		strings.TrimRight(sb.String(), "\n"),
		"slash-commands.txt",
		tr.Tf("cmd.poracle_admin.slash.list.attached", fmt.Sprintf("%d", total)),
	)
}

func runSlashStatus(ctx *bot.CommandContext, tr *i18n.Translator) []bot.Reply {
	if ctx.Admin == nil || ctx.Admin.SlashStatus == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
	}
	global, guilds, err := ctx.Admin.SlashStatus()
	if err != nil {
		if errors.Is(err, slash.ErrSlashNotConfigured) {
			return []bot.Reply{{Text: tr.T("cmd.poracle_admin.slash.not_configured")}}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.slash.status.error", err.Error())}}
	}

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.slash.status.header"))

	// Helper to render one scope row.
	renderScope := func(scope bot.SlashScope) {
		fp := scope.Fingerprint
		if fp == "" {
			fp = tr.T("cmd.poracle_admin.slash.status.never_synced")
		} else if len(fp) > 8 {
			fp = fp[:8]
		}

		syncedStr := tr.T("cmd.poracle_admin.slash.status.never_synced")
		if !scope.LastSyncedAt.IsZero() {
			ago := time.Since(scope.LastSyncedAt)
			syncedStr = tr.Tf("cmd.poracle_admin.slash.status.ago", formatDuration(ago))
		}

		sb.WriteString("\n")
		sb.WriteString(tr.Tf("cmd.poracle_admin.slash.status.scope_row",
			scope.Name,
			syncedStr,
			fp,
		))
	}

	renderScope(global)
	for _, g := range guilds {
		renderScope(g)
	}

	return []bot.Reply{{Text: sb.String()}}
}
