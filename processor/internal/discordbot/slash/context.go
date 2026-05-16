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

	// Wire injected deps from BotDeps via the shared constructor so the
	// dispatcher and the text bot stay in lockstep — a new BotDeps field
	// flows into both surfaces from one site. Per-interaction fields are
	// layered on top below; nil deps yields a bare CommandContext that
	// still gets the identity / language / admin fields populated.
	//
	// NB: ctx.Translations falls back to d.bundle when deps is nil so the
	// downstream Tr() lookup doesn't panic on a misconfigured dispatcher.
	ctx := bot.NewCommandContext(d.deps)
	ctx.UserID = userID
	ctx.UserName = userName
	ctx.Platform = "discord"
	ctx.ChannelID = ic.ChannelID
	ctx.GuildID = ic.GuildID
	ctx.IsDM = ic.GuildID == ""
	ctx.IsSlash = true
	ctx.Language = lang
	ctx.IsAdmin = d.isAdmin(userID)
	ctx.TargetID = userID
	ctx.TargetName = userName
	ctx.TargetType = bot.TypeDiscordUser
	if ctx.Config == nil {
		ctx.Config = d.cfgRoot
	}
	if ctx.Translations == nil {
		ctx.Translations = d.bundle
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
