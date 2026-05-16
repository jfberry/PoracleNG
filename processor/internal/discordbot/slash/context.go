package slash

import (
	"slices"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// discordLocaleToPoracle maps a Discord client locale to the Poracle language
// code used by the i18n bundle. Currently ships the common-European subset;
// expand as additional translations land. Unmapped locales fall through to
// [general] locale.
//
// The map intentionally uses the typed discordgo.Locale constants (not the
// raw string values like "de" or "fr") so a compile error catches any rename
// in the upstream library.
var discordLocaleToPoracle = map[discordgo.Locale]string{
	discordgo.German:    "de",
	discordgo.French:    "fr",
	discordgo.SpanishES: "es",
	discordgo.Italian:   "it",
}

// buildContext assembles a CommandContext for a slash dispatch.
//
// Wires the full BotDeps graph through, resolves the user's language using
// the chain human.Language → Discord client locale → [general] locale → "en",
// loads identity and area/location state from the Human store, and populates
// the admin flag.
//
// Returns (ctx, nil) on success. The error return path is reserved for future
// per-command guild/channel validation (BuildTarget-style overrides).
func (d *Dispatcher) buildContext(ic *discordgo.InteractionCreate, cmdKey string) (*bot.CommandContext, error) {
	_ = cmdKey // reserved — later phases may use this for per-command overrides

	userID := interactionUserID(ic)
	userName := interactionUserName(ic)

	// Load the full human record if available. nil result means unregistered,
	// which is allowed at this stage — the dispatcher's registration check
	// runs after buildContext and decides whether to gate the command. The
	// full Get (vs GetLite) is needed because /tracked and other read-only
	// commands inspect ctx.HasArea / ctx.HasLocation, which require the lat,
	// lon, and area JSON columns that GetLite skips.
	var human *store.Human
	if d.deps != nil && d.deps.Humans != nil && userID != "" {
		human, _ = d.deps.Humans.Get(userID)
	}

	humanLang := ""
	if human != nil {
		humanLang = human.Language
	}
	lang := d.resolveLanguage(ic, humanLang)

	ctx := &bot.CommandContext{
		UserID:       userID,
		UserName:     userName,
		Platform:     "discord",
		ChannelID:    ic.ChannelID,
		GuildID:      ic.GuildID,
		IsDM:         ic.GuildID == "",
		IsSlash:      true,
		Language:     lang,
		IsAdmin:      d.isAdmin(userID),
		TargetID:     userID,
		TargetName:   userName,
		TargetType:   bot.TypeDiscordUser,
		Config:       d.cfgRoot,
		Translations: d.bundle,
	}

	if human != nil {
		// Admin-disabled users are treated as unregistered (role removed,
		// banned). enabled=0 (!stop) just pauses alerts — user is still
		// registered and can run commands like !start, !tracked, !area, etc.
		// Mirrors the gating logic in bot.LookupUserStateFromStore.
		ctx.IsRegistered = !human.AdminDisable
		ctx.ProfileNo = human.CurrentProfileNo
		ctx.HasLocation = human.Latitude != 0 || human.Longitude != 0
		ctx.HasArea = len(human.Area) > 0
	}

	// Wire injected deps from BotDeps so the underlying Command has everything
	// it needs. /version doesn't touch most of these, but most tracking and
	// area/profile commands do; keeping the wiring here means the dispatch
	// path is consistent regardless of which command is invoked.
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
		// /summary needs the schedule store + buffer-count/dispatch
		// callbacks. The text bot wires the same trio for !summary; nil
		// values disable the feature gracefully (the command reacts with
		// the usage hint instead of erroring).
		ctx.SummarySchedules = d.deps.SummarySchedules
		ctx.SummaryBufferCount = d.deps.SummaryBufferCount
		ctx.SummaryDispatch = d.deps.SummaryDispatch
	}

	// Geofence + AreaLogic come from the current state snapshot. The text
	// bot does the same lookup in discordbot/bot.go before invoking Run —
	// /area, /track (area pinning), and several other commands consult
	// ctx.AreaLogic / ctx.Fences directly.
	if d.deps != nil && d.deps.StateMgr != nil {
		if st := d.deps.StateMgr.Get(); st != nil {
			ctx.Geofence = st.Geofence
			ctx.Fences = st.Fences
			ctx.AreaLogic = bot.NewAreaLogic(st.Fences, d.cfgRoot)
		}
	}

	return ctx, nil
}

// resolveLanguage walks the language chain: explicit human language → mapped
// Discord client locale → [general] locale → "en". Pass the human's Language
// field directly (empty string when the user has no preference) so the helper
// stays decoupled from the store's Human / HumanLite types.
func (d *Dispatcher) resolveLanguage(ic *discordgo.InteractionCreate, humanLang string) string {
	if humanLang != "" {
		return humanLang
	}
	if ic != nil && ic.Interaction != nil {
		if mapped, ok := discordLocaleToPoracle[ic.Locale]; ok {
			return mapped
		}
	}
	if d.cfgRoot != nil && d.cfgRoot.General.Locale != "" {
		return d.cfgRoot.General.Locale
	}
	return "en"
}

// isAdmin returns true if userID is in the configured Discord admin list.
// Safe to call with an empty userID (returns false) and does not panic on a
// nil cfgRoot.
func (d *Dispatcher) isAdmin(userID string) bool {
	if userID == "" || d.cfgRoot == nil {
		return false
	}
	return slices.Contains(d.cfgRoot.Discord.Admins, userID)
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
