package commands

import (
	"strconv"
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// paEmoji implements !poracle-admin emoji — manages and inspects the
// processor's emoji configuration AND the Discord guild's emoji assets.
//
// Subcommands:
//
//	list                       — list configured keys with per-platform resolutions
//	reload                     — reload config/emoji.json from disk
//	test <key>                 — resolve one emoji key for the current platform
//	upload [overwrite]         — upload uicons-based emojis to the guild + write emoji.json
//	discord-config             — dump emoji.json from current guild state (no uploads)
//
// upload and discord-config require a Discord bot — they call into the gateway
// session to talk to the guild API.
var paEmoji = &paSubgroup{
	run:  paEmojiRun,
	help: paEmojiHelp,
}

func paEmojiHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.emoji.help.intro"))
	sb.WriteString("\n\n")
	sb.WriteString(tr.T("cmd.poracle_admin.emoji.list.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.emoji.reload.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.emoji.test.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.emoji.upload.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.emoji.discord_config.desc"))

	return []bot.Reply{{Text: sb.String()}}
}

func paEmojiRun(ctx *bot.CommandContext, args []string) []bot.Reply {
	if len(args) == 0 || args[0] == "help" {
		return paEmojiHelp(ctx)
	}

	switch strings.ToLower(args[0]) {
	case "list":
		return paEmojiList(ctx)
	case "reload":
		return paEmojiReload(ctx)
	case "test":
		return paEmojiTest(ctx, args[1:])
	case "upload":
		overwrite := false
		for _, a := range args[1:] {
			if strings.EqualFold(a, "overwrite") {
				overwrite = true
			}
		}
		return paEmojiOperation(ctx, true, overwrite)
	case "discord-config", "discord_config":
		return paEmojiOperation(ctx, false, false)
	default:
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.unknown_sub", "emoji")}}
	}
}

func paEmojiList(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	if ctx.Emoji == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.emoji.not_loaded")}}
	}

	keys := ctx.Emoji.AllKeys()
	overrides := ctx.Emoji.PlatformOverrides()
	defaults := ctx.Emoji.Defaults()

	var sb strings.Builder
	sb.WriteString(tr.Tf("cmd.poracle_admin.emoji.list.header", strconv.Itoa(len(keys))))

	for _, key := range keys {
		sb.WriteString("\n")
		sb.WriteString(tr.Tf("cmd.poracle_admin.emoji.list.row.key", key))

		// Collect per-platform resolutions: first the platforms listed in
		// overrides, then "default" if the key exists in defaults.
		// Deduplicate by showing the effective resolved value per platform.
		platformsDone := make(map[string]bool)
		for platform := range overrides {
			val := ctx.Emoji.Lookup(key, platform)
			if val != "" {
				sb.WriteString("\n")
				sb.WriteString(tr.Tf("cmd.poracle_admin.emoji.list.row.platform", platform, val))
				platformsDone[platform] = true
			}
		}

		// If key is in defaults and not already shown for every platform,
		// show the fallback default value once.
		if defVal, ok := defaults[key]; ok {
			// Only show the default if there are no overrides (all platforms
			// fall through to default), or to indicate the base value.
			if len(platformsDone) == 0 {
				sb.WriteString("\n")
				sb.WriteString(tr.Tf("cmd.poracle_admin.emoji.list.row.platform", "default", defVal))
			}
		}
	}

	return bot.SplitTextReply(sb.String())
}

func paEmojiReload(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	if ctx.EmojiReload == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.emoji.not_loaded")}}
	}

	start := time.Now()
	count, err := ctx.EmojiReload()
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.emoji.reload.error", err.Error())}}
	}

	return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.emoji.reload.success",
		strconv.Itoa(count),
		strconv.FormatInt(elapsed, 10),
	)}}
}

// paEmojiOperation routes to the shared EmojiOperation closure on BotDeps
// (which calls into the discord bot's gateway session). Used by both
// `upload` (with optional overwrite) and `discord-config` (no upload).
func paEmojiOperation(ctx *bot.CommandContext, upload, overwrite bool) []bot.Reply {
	tr := ctx.Tr()

	if ctx.Platform != "discord" {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.emoji.discord_only")}}
	}
	if ctx.EmojiOperation == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.emoji.discord_only")}}
	}
	return ctx.EmojiOperation(ctx.ChannelID, ctx.GuildID, upload, overwrite)
}

func paEmojiTest(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.emoji.test.usage")}}
	}

	if ctx.Emoji == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.emoji.not_loaded")}}
	}

	key := args[0]
	platform := ctx.Platform
	if platform == "" {
		platform = "discord"
	}

	resolved := ctx.Emoji.Lookup(key, platform)
	if resolved == "" {
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.emoji.test.not_found", key)}}
	}

	return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.emoji.test.success", key, platform, resolved)}}
}
