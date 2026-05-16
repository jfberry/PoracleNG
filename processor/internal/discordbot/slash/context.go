package slash

import (
	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// buildContext assembles a minimal CommandContext for a slash dispatch.
//
// Phase 1 scope: enough fields to run /version (which only needs identity +
// translations). Task 14 expands this to load the human row, populate
// permissions, hand-shake with BuildTarget, etc.
//
// Returns (ctx, nil) on success. The error return path is reserved for Task 14
// so that future failures (e.g. ProvideUserContext-style guild/channel
// validation) can surface as user-facing errors via respondError.
func (d *Dispatcher) buildContext(ic *discordgo.InteractionCreate, cmdKey string) (*bot.CommandContext, error) {
	_ = cmdKey // reserved — Task 14 will use this for per-command overrides

	userID := interactionUserID(ic)
	userName := interactionUserName(ic)

	lang := ""
	if d.cfgRoot != nil {
		lang = d.cfgRoot.General.Locale
	}
	if lang == "" {
		lang = "en"
	}

	ctx := &bot.CommandContext{
		UserID:       userID,
		UserName:     userName,
		Platform:     "discord",
		ChannelID:    ic.ChannelID,
		GuildID:      ic.GuildID,
		IsDM:         ic.GuildID == "",
		Language:     lang,
		TargetID:     userID,
		TargetName:   userName,
		TargetType:   bot.TypeDiscordUser,
		Config:       d.cfgRoot,
		Translations: d.bundle,
	}

	// Wire injected deps from BotDeps so the underlying Command has everything
	// it needs. /version doesn't touch most of these, but commands added in
	// later phases (Tasks 16+) will, and keeping the wiring here means the
	// dispatch path is consistent regardless of which command is invoked.
	if d.deps != nil {
		ctx.DB = d.deps.DB
		ctx.Humans = d.deps.Humans
		ctx.Tracking = d.deps.Tracking
		ctx.StateMgr = d.deps.StateMgr
		ctx.GameData = d.deps.GameData
		ctx.Dispatcher = d.deps.Dispatcher
		ctx.RowText = d.deps.RowText
		ctx.Resolver = d.deps.Resolver
		ctx.ArgMatcher = d.deps.ArgMatcher
		ctx.Geocoder = d.deps.Geocoder
		ctx.StaticMap = d.deps.StaticMap
		ctx.Weather = d.deps.Weather
		ctx.Stats = d.deps.Stats
		ctx.DTS = d.deps.DTS
		ctx.Emoji = d.deps.Emoji
		ctx.NLP = d.deps.NLPParser
		ctx.TestProcessor = d.deps.TestProcessor
		ctx.Registry = d.deps.Registry
		ctx.Scanner = d.deps.Scanner
		ctx.ReloadFunc = d.deps.ReloadFunc
	}

	return ctx, nil
}

// interactionUserID extracts the invoking user's typed ID. Guild interactions
// populate Member.User; DM interactions populate User directly.
func interactionUserID(ic *discordgo.InteractionCreate) string {
	if ic == nil || ic.Interaction == nil {
		return ""
	}
	if ic.Member != nil && ic.Member.User != nil {
		return ic.Member.User.ID
	}
	if ic.User != nil {
		return ic.User.ID
	}
	return ""
}

// interactionUserName extracts the invoking user's username.
func interactionUserName(ic *discordgo.InteractionCreate) string {
	if ic == nil || ic.Interaction == nil {
		return ""
	}
	if ic.Member != nil && ic.Member.User != nil {
		return ic.Member.User.Username
	}
	if ic.User != nil {
		return ic.User.Username
	}
	return ""
}
