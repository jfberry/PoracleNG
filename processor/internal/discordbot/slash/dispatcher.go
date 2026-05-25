package slash

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/autocomplete"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/autocomplete/listers"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/mappers"
	"github.com/pokemon/poracleng/processor/internal/discordroles"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

type Config struct {
	Enabled bool
	Global  bool
	Guilds  []string
	// Enable lists short command names this installation registers (e.g. "track").
	// Empty = all supported commands enabled. Maps 1:1 from config's
	// [discord.slash_commands] enable.
	Enable []string
	// Optional override paths for testing
	CachePath string
	ForceSync bool
}

type Dispatcher struct {
	cfg         Config
	session     *discordgo.Session // set by Attach()
	appID       string             // set after session.Open()
	deps        *bot.BotDeps
	registry    *bot.Registry
	bundle      *i18n.Bundle
	cfgRoot     *config.Config
	commandsAPI commandsAPI // test seam; nil = use session

	// nameToKey maps the *registered* slash command name (English by default,
	// or operator-renamed via slash.cmd.<short>) back to the canonical
	// identifier key (e.g. "cmd.version"). Populated by buildNameMap() in
	// Attach() so HandleCommand can route interactions whose Name was
	// localized at registration time.
	nameToKey map[string]string

	// autocompleteRegistry hosts the UserStateLister implementations used by
	// HandleAutocomplete. Populated in NewDispatcher; HandleAutocomplete
	// looks up listers by name when routing user-state options
	// (tracking/areas/profiles). Built-in providers (Pokemon, IV, RaidBoss,
	// Template) are called directly without going through the registry —
	// the registry exists only for state-bound listers that need deps +
	// userID + a hint.
	autocompleteRegistry *autocomplete.Registry
}

// commandsSkippingRegistration matches the text bot's special-case logic in
// internal/discordbot/bot.go: commands that work even when the user has not
// registered with !poracle. /version is currently the only entry; /poracle
// itself is not surfaced as a slash command.
var commandsSkippingRegistration = map[string]bool{
	"cmd.version": true,
}

func NewDispatcher(cfg Config) *Dispatcher {
	d := &Dispatcher{
		cfg:                  cfg,
		autocompleteRegistry: autocomplete.NewRegistry(),
	}
	d.autocompleteRegistry.Register("tracking", listers.ListTracking)
	d.autocompleteRegistry.Register("areas", listers.ListAreas)
	d.autocompleteRegistry.Register("profiles", listers.ListProfiles)
	d.autocompleteRegistry.Register("locations", listers.ListUserLocations)
	return d
}

// SetAppID stores the application ID used by SyncCommands. Called by the
// discordbot wrapper after session.Open() resolves the bot's own user ID,
// which doubles as the Discord application ID for slash registration.
func (d *Dispatcher) SetAppID(id string) {
	if d == nil {
		return
	}
	d.appID = id
}

func (d *Dispatcher) Attach(s *discordgo.Session, deps *bot.BotDeps, registry *bot.Registry, bundle *i18n.Bundle, cfg *config.Config) {
	d.session = s
	d.deps = deps
	d.registry = registry
	d.bundle = bundle
	d.cfgRoot = cfg
	d.nameToKey = d.buildNameMap()
}

// buildNameMap walks every command key this build supports and computes the
// registered slash name (operator-renameable via slash.cmd.<short>). The
// result is what Discord sends back as ApplicationCommandData().Name.
//
// Built once at Attach() time so HandleCommand's hot path is a single map
// lookup. The map handles three cases per key:
//
//   - the canonical short name ("version")
//   - the i18n-renamed name from slash.cmd.<short> ("poracle-version")
//
// Both keys point at the same identifier ("cmd.version") so either form
// routes correctly.
func (d *Dispatcher) buildNameMap() map[string]string {
	out := make(map[string]string)
	for _, key := range allCommandKeys() {
		canon := canonShortName(key)
		out[canon] = key
		renamed := resolveSlashName(d.bundle, key, canon)
		if renamed != "" && renamed != canon {
			out[renamed] = key
		}
	}
	return out
}

// resolveCommandKey returns the identifier key ("cmd.version") for a slash
// name as received from Discord, or "" if unknown.
func (d *Dispatcher) resolveCommandKey(slashName string) string {
	if d.nameToKey == nil {
		return ""
	}
	return d.nameToKey[slashName]
}

// HandleCommand routes an ApplicationCommand interaction end-to-end:
//
//  1. Defer ephemerally (Discord's 3-second deadline)
//  2. Resolve slash name → identifier key
//  3. Check disabled_commands
//  4. Build CommandContext
//  5. Registration check (skipped for commandsSkippingRegistration)
//  6. command_security check
//  7. Map options to tokens
//  8. Run command, send replies
func (d *Dispatcher) HandleCommand(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	if d == nil || ic == nil {
		return
	}

	// 1. Defer ephemerally. ALL slash responses are ephemeral; Reply.IsDM=true
	//    later triggers an additional persistent DM via Send().
	if s != nil {
		if err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
		}); err != nil {
			log.WithError(err).Warn("slash: failed to defer interaction")
			return
		}
	}

	// Log every slash invocation (DM or channel) to the configured
	// dm_log_channel_id. Slash responses are ephemeral, so channel
	// invocations are just as private as DMs from other members' point
	// of view — the audit log is the only place they show up. Logged
	// unconditionally (before any auth/registration check) so even
	// rejected attempts show up; that's the audit signal admins want.
	d.logInvocation(s, ic)

	// 2. Resolve invoked name. May be the operator-renamed/i18n variant.
	invoked := ic.ApplicationCommandData().Name
	cmdKey := d.resolveCommandKey(invoked)
	if cmdKey == "" {
		d.respondError(s, ic, "🛑 Unknown command.")
		return
	}
	if d.registry == nil || d.registry.Lookup(cmdKey) == nil {
		d.respondError(s, ic, "🛑 Command not implemented.")
		return
	}
	canon := canonShortName(cmdKey)

	// 3. Disabled-command check (shared text+slash mechanism).
	if d.cfgRoot != nil && bot.IsCommandDisabled(d.cfgRoot.General.DisabledCommands, cmdKey) {
		d.respondError(s, ic, "🛑 This command is disabled by the operator.")
		return
	}

	// 4. Build context.
	ctx, err := d.buildContext(ic, cmdKey)
	if err != nil {
		d.respondError(s, ic, fmt.Sprintf("🛑 %s", err.Error()))
		return
	}

	// 5. Registration check. Skipped for /version (and the other
	//    commandsSkippingRegistration entries) so a brand-new user can still
	//    poke at the bot to confirm it's alive.
	//
	// buildContext already loaded the human and set ctx.IsRegistered, so we
	// avoid a second store lookup here.
	if !commandsSkippingRegistration[cmdKey] {
		if d.deps != nil && d.deps.Humans != nil && !ctx.IsRegistered {
			d.respondError(s, ic, registrationErrorText(d.cfgRoot, d.bundle, ctx.Language, ic.GuildID))
			return
		}
	}

	// 6. command_security check (text+slash shared config).
	if !d.commandAllowed(ic, cmdKey, ctx.IsAdmin) {
		d.respondError(s, ic, fmt.Sprintf("🛑 You don't have permission to run /%s.", invoked))
		return
	}

	// 7. Map slash options to text-command tokens.
	var tokens []string
	mapperFn := mappers.Lookup(canon)
	if mapperFn == nil {
		d.respondError(s, ic, "🛑 Command not implemented.")
		return
	}
	tokens, err = mapperFn(ic.ApplicationCommandData().Options)
	if err != nil {
		d.respondError(s, ic, formatMapperError(err, ctx.Language, d.bundle))
		return
	}

	// 8. Dispatch & send. /untrack reroutes to the per-type command for
	// non-pokemon sub-commands (cmd.untrack only knows how to delete monster
	// rules; eggs, raids, etc. live on their own commands as "<type> remove
	// id:N"). The mapper emits the right token grammar for each branch.
	runKey := cmdKey
	if canon == "untrack" {
		if sub := findUntrackSubtype(ic); sub != "" && sub != "pokemon" {
			runKey = "cmd." + sub
		}
	}
	cmd := d.registry.Lookup(runKey)
	if cmd == nil {
		d.respondError(s, ic, "🛑 Command not implemented.")
		return
	}
	replies := cmd.Run(ctx, tokens)
	// Apply maintenance suffix so slash users see the paused-delivery
	// warning, matching the gateway (discordbot/bot.go) and Telegram surfaces.
	if d.bundle != nil && ctx != nil && d.deps != nil {
		tr := d.bundle.For(ctx.Language)
		replies = bot.ApplyMaintenanceSuffix(replies, d.deps.Dispatcher, tr.T("cmd.maintenance.active_suffix"))
	}
	if err := Send(s, ic, replies); err != nil {
		log.WithError(err).Warnf("slash: failed to send replies for /%s", invoked)
	}
}

// HandleAutocomplete routes an ApplicationCommandAutocomplete interaction
// to the appropriate provider based on the (command, option) tuple.
//
// The flow mirrors HandleCommand's early stages but never defers — Discord
// expects an autocomplete response within 3 seconds in the same response
// frame, not a deferred follow-up.
//
//  1. Find the focused option (walks sub-commands).
//  2. Resolve user language for localized labels.
//  3. Unregistered-user gate: commands not in commandsSkippingRegistration
//     return empty choices so the suggestion list doesn't entice a user
//     into typing input they can't submit.
//  4. Dispatch on (cmd, opt) to a built-in provider or to a registered
//     UserStateLister.
//  5. Respond with the choice list (always emit a response — empty
//     choices is still a valid autocomplete reply).
func (d *Dispatcher) HandleAutocomplete(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	if d == nil || ic == nil {
		return
	}

	data := ic.ApplicationCommandData()
	focused := focusedOption(data.Options)
	if focused == nil {
		return
	}

	cmdName := data.Name
	optName := focused.Name
	focusedValue := focusedStringValue(focused)

	// Resolve user language for localized labels. Autocomplete only needs
	// language + the "registered?" bit, so the lightweight GetLite is enough
	// here — buildContext (the dispatch path) takes the heavier Get because
	// commands like /tracked also need HasArea / HasLocation.
	userID := interactionUserID(ic)
	var human *store.HumanLite
	if d.deps != nil && d.deps.Humans != nil && userID != "" {
		human, _ = d.deps.Humans.GetLite(userID)
	}
	humanLang := ""
	if human != nil {
		humanLang = human.Language
	}
	userLang := d.resolveLanguage(ic, humanLang)

	// Resolve cmdKey from invoked name to check skip-registration list.
	cmdKey := d.resolveCommandKey(cmdName)

	// Unregistered user → return empty choices (don't mislead with active
	// suggestions they can't actually submit successfully).
	if human == nil && !commandsSkippingRegistration[cmdKey] {
		respondAutocomplete(s, ic, nil)
		return
	}

	choices := d.routeAutocomplete(cmdName, optName, focusedValue, userLang, ic)
	respondAutocomplete(s, ic, choices)
}

// routeAutocomplete dispatches by (command, option) tuple. The plan's
// tracking option lives under an /untrack sub-command whose name IS the
// tracking subtype, so we walk the option tree to find it.
func (d *Dispatcher) routeAutocomplete(cmd, opt, focused, userLang string, ic *discordgo.InteractionCreate) []*discordgo.ApplicationCommandOptionChoice {
	switch {
	// /maxbattle pokemon — empty focused boosts currently-active max
	// battle bosses (recorded by the maxbattle webhook handler).
	// Routed before the generic `pokemon` case so the boost wins.
	case opt == "pokemon" && cmd == "maxbattle":
		base := autocomplete.Pokemon(context.Background(), d.deps, focused, userLang)
		if focused == "" && d.deps != nil && d.deps.RecentActivity != nil {
			base = autocomplete.PrependActivePokemon(base, d.deps, d.deps.RecentActivity.ActiveMaxBattleBosses(), userLang)
		}
		return base
	// /quest pokemon — empty focused boosts currently-active quest
	// pokemon rewards. Routed before the generic `pokemon` case.
	case opt == "pokemon" && cmd == "quest":
		base := autocomplete.Pokemon(context.Background(), d.deps, focused, userLang)
		if focused == "" && d.deps != nil && d.deps.RecentActivity != nil {
			base = autocomplete.PrependActivePokemon(base, d.deps, d.deps.RecentActivity.ActiveQuestPokemon(), userLang)
		}
		return base
	case opt == "pokemon":
		return autocomplete.Pokemon(context.Background(), d.deps, focused, userLang)
	case opt == "iv":
		return autocomplete.IVRange(focused)
	case opt == "boss" && cmd == "raid":
		return autocomplete.RaidBoss(context.Background(), d.deps, focused, userLang)
	case opt == "template":
		return autocomplete.Template(context.Background(), d.deps, focused, dtsTypeFor(cmd), "discord", userLang)
	case opt == "tracking" && cmd == "untrack":
		subtype := findUntrackSubtype(ic)
		return d.userstateAutocomplete(ic, "tracking", subtype, focused)
	case opt == "area":
		return d.userstateAutocomplete(ic, "areas", "", focused)
	case opt == "name" && cmd == "profile":
		return d.userstateAutocomplete(ic, "profiles", "", focused)
	// /profile copyto target — pick another profile of the same user.
	// Reuses the same profiles lister as `name`; the sub-command name
	// (copyto) selects the option name (profile), not a different list.
	case opt == "profile" && cmd == "profile":
		return d.userstateAutocomplete(ic, "profiles", "", focused)
	// /location show name and /location remove name — autocomplete from
	// the user's saved named locations. Both sub-commands use the same
	// option name ("name"), so a single (opt, cmd) pair covers both.
	case opt == "name" && cmd == "location":
		return d.userstateAutocomplete(ic, "locations", "", focused)
	// /quest reward-type options. Item is its own translated lookup;
	// candy and mega_energy are pokemon-keyed (the reward IS for a
	// specific species), so they reuse the pokemon autocomplete.
	// All three boost from RecentActivity on empty focused so live
	// quest rewards surface first.
	case opt == "item" && cmd == "quest":
		base := autocomplete.Item(context.Background(), d.deps, focused, userLang)
		if focused == "" && d.deps != nil && d.deps.RecentActivity != nil {
			base = autocomplete.PrependActiveItems(base, d.deps, d.deps.RecentActivity.ActiveQuestItems(), userLang)
		}
		return base
	case opt == "candy" && cmd == "quest":
		base := autocomplete.Pokemon(context.Background(), d.deps, focused, userLang)
		if focused == "" && d.deps != nil && d.deps.RecentActivity != nil {
			base = autocomplete.PrependActivePokemon(base, d.deps, d.deps.RecentActivity.ActiveQuestCandy(), userLang)
		}
		return base
	case opt == "mega_energy" && cmd == "quest":
		base := autocomplete.Pokemon(context.Background(), d.deps, focused, userLang)
		if focused == "" && d.deps != nil && d.deps.RecentActivity != nil {
			base = autocomplete.PrependActivePokemon(base, d.deps, d.deps.RecentActivity.ActiveQuestMega(), userLang)
		}
		return base
	// /invasion grunt_type: typed grunts (Fire, Water…) and bosses
	// (Giovanni, Arlo, Cliff, Sierra). Incidents live on /incident.
	// Empty focused boosts currently-active grunt types (TypeID-keyed
	// from invasion webhooks).
	case opt == "grunt_type" && cmd == "invasion":
		base := autocomplete.Grunt(context.Background(), d.deps, focused, userLang)
		if focused == "" && d.deps != nil && d.deps.RecentActivity != nil {
			base = autocomplete.PrependActiveGrunts(base, d.deps, d.deps.RecentActivity.ActiveInvasionGrunts(), userLang)
		}
		return base
	// /incident type: pokestop events (Kecleon, Gold Pokestop, Showcase,
	// Pokestop Spawn…).
	case opt == "type" && cmd == "incident":
		return autocomplete.IncidentType(context.Background(), d.deps, focused, userLang)
	// /track form cascades from the user's currently-selected pokemon
	// option in the same interaction.
	case opt == "form" && cmd == "track":
		pokemonValue := siblingOptionString(ic, "pokemon")
		return autocomplete.Form(context.Background(), d.deps, pokemonValue, focused, userLang)
	}
	return nil
}

// siblingOptionString returns the StringValue of the given top-level
// option name on the interaction's command data, or "" if the option is
// absent or not string-typed. Cascading autocompletes (e.g. /track form
// reading the chosen pokemon) use this to read peers without rebuilding
// the parser.
func siblingOptionString(ic *discordgo.InteractionCreate, name string) string {
	if ic == nil {
		return ""
	}
	for _, o := range ic.ApplicationCommandData().Options {
		if o == nil || o.Name != name {
			continue
		}
		if o.Type != discordgo.ApplicationCommandOptionString {
			return ""
		}
		return o.StringValue()
	}
	return ""
}

// userstateAutocomplete looks up a lister by name and runs it with the
// invoking user's ID. Returns nil when the registry has no such lister or
// the lister errors — autocomplete shouldn't surface infrastructure errors
// to the end user, so we degrade silently to "no suggestions".
func (d *Dispatcher) userstateAutocomplete(ic *discordgo.InteractionCreate, listerName, subtype, focused string) []*discordgo.ApplicationCommandOptionChoice {
	if d.autocompleteRegistry == nil {
		return nil
	}
	lister := d.autocompleteRegistry.Lookup(listerName)
	if lister == nil {
		return nil
	}
	userID := interactionUserID(ic)
	out, err := lister(context.Background(), d.deps, userID, autocomplete.UserStateHint{Subtype: subtype, Focused: focused})
	if err != nil {
		return nil
	}
	return autocomplete.FilterAndCap(out, focused)
}

// focusedOption returns the option flagged Focused=true. Walks into
// sub-command options because Discord nests an autocomplete option inside
// the chosen sub-command (e.g. /untrack raid <tracking> → top-level
// option "raid" has Focused=false; its Options child "tracking" has
// Focused=true).
func focusedOption(opts []*discordgo.ApplicationCommandInteractionDataOption) *discordgo.ApplicationCommandInteractionDataOption {
	for _, o := range opts {
		if o == nil {
			continue
		}
		if o.Focused {
			return o
		}
		if len(o.Options) > 0 {
			if sub := focusedOption(o.Options); sub != nil {
				return sub
			}
		}
	}
	return nil
}

// focusedStringValue extracts the focused option's typed value as a string.
// discordgo's StringValue panics when the option type isn't String, so we
// guard against that path — autocomplete focused options are nearly always
// strings, but a defensive read avoids tripping over a misregistered option.
func focusedStringValue(opt *discordgo.ApplicationCommandInteractionDataOption) string {
	if opt == nil {
		return ""
	}
	if opt.Type != discordgo.ApplicationCommandOptionString {
		if s, ok := opt.Value.(string); ok {
			return s
		}
		return ""
	}
	if s, ok := opt.Value.(string); ok {
		return s
	}
	return ""
}

// findUntrackSubtype walks the top-level interaction options for an
// /untrack invocation and returns the chosen sub-command's name (which IS
// the tracking subtype: "raid", "egg", ...). Returns "" when no
// sub-command option is present — caller treats that as "no subtype hint".
func findUntrackSubtype(ic *discordgo.InteractionCreate) string {
	if ic == nil || ic.Interaction == nil {
		return ""
	}
	for _, o := range ic.ApplicationCommandData().Options {
		if o == nil {
			continue
		}
		if o.Type == discordgo.ApplicationCommandOptionSubCommand {
			return o.Name
		}
	}
	return ""
}

// dtsTypeFor maps a slash command name to the DTS template type used by
// the template store. Most are identical to the slash command name; the
// exception is /track → "monster", which preserves the historic webhook
// naming.
//
// The DTS types come from fallbacks/dts.json: monster, raid, egg, quest,
// invasion, lure, nest, gym, fort-update, maxbattle.
func dtsTypeFor(cmd string) string {
	switch cmd {
	case "track":
		return "monster"
	case "fort":
		return "fort-update"
	}
	return cmd
}

// respondAutocomplete sends an autocomplete result with the given choices.
// Nil choices is a valid Discord autocomplete reply ("no suggestions"); we
// pass it through unchanged so a legitimately empty result doesn't trigger
// retries.
func respondAutocomplete(s *discordgo.Session, ic *discordgo.InteractionCreate, choices []*discordgo.ApplicationCommandOptionChoice) {
	if s == nil || ic == nil || ic.Interaction == nil {
		return
	}
	_ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	})
}

// commandAllowed checks command_security for the invoking user.
//
// Admins always bypass. Non-admins are evaluated by bot.CommandAllowed against
// the operator's [discord] command_security mapping: a command with no entry
// (commandSecurityName returns "") trivially passes; a command with an entry
// requires the user's ID or one of their guild roles to be in the allow list.
//
// In DM-style interactions Discord doesn't populate ic.Member, so the
// dispatcher falls back to unioning the user's roles across every
// configured guild — same approach the text bot uses for !commands sent
// from a DM. Without that, role-gated command_security silently blocks
// DM slash invocations even when the user holds the right role.
func (d *Dispatcher) commandAllowed(ic *discordgo.InteractionCreate, cmdKey string, isAdmin bool) bool {
	if isAdmin {
		return true
	}
	userID := interactionUserID(ic)
	return bot.CommandAllowed(d.cfgRoot, "discord", cmdKey, userID, d.lookupRoles(ic, userID))
}

// lookupRoles returns the role IDs to evaluate the user against. Two
// code paths:
//
//   - Guild interaction (ic.Member non-nil): use ic.Member.Roles —
//     Discord already populated them in the interaction payload.
//   - DM interaction: union the user's roles across every guild in
//     cfg.Discord.Guilds via discordroles.GetUserRoleIDs. Mirrors the
//     text bot's fetchRoles DM branch and PoracleJS's getUserRoles.
func (d *Dispatcher) lookupRoles(ic *discordgo.InteractionCreate, userID string) []string {
	if ic != nil && ic.Member != nil {
		return ic.Member.Roles
	}
	if d.session == nil || d.cfgRoot == nil || len(d.cfgRoot.Discord.Guilds) == 0 || userID == "" {
		return nil
	}
	roleIDs, err := discordroles.GetUserRoleIDs(d.session, d.cfgRoot.Discord.Guilds, userID)
	if err != nil {
		return nil
	}
	return roleIDs
}

// respondError edits the deferred ephemeral reply with an error message. Falls
// back to FollowupMessageCreate if the edit fails (e.g. the interaction has
// already been responded to).
func (d *Dispatcher) respondError(s *discordgo.Session, ic *discordgo.InteractionCreate, msg string) {
	if s == nil || ic == nil || ic.Interaction == nil {
		return
	}
	if _, err := s.InteractionResponseEdit(ic.Interaction, &discordgo.WebhookEdit{
		Content: &msg,
	}); err != nil {
		log.WithError(err).Debug("slash: respondError edit failed; trying followup")
		if _, ferr := s.FollowupMessageCreate(ic.Interaction, true, &discordgo.WebhookParams{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		}); ferr != nil {
			log.WithError(ferr).Warn("slash: respondError followup also failed")
		}
	}
}

// registrationErrorText returns the user-facing message shown to an
// unregistered user who runs a slash command that requires registration.
//
// The message is rendered in the user's resolved language (falling back to
// English when the bundle has no entry). When the operator has configured
// a single registration channel for Discord we mention it inline so the
// user has a one-click destination; otherwise we render the DM-only form.
// We deliberately do NOT mention a channel when multiple registration
// channels are configured because the flat [discord] channels list isn't
// keyed by guild — naming an arbitrary channel could send the user into
// the wrong server.
func registrationErrorText(cfg *config.Config, bundle *i18n.Bundle, lang, guildID string) string {
	var tr *i18n.Translator
	if bundle != nil {
		tr = bundle.For(lang)
		if tr == nil {
			tr = bundle.For("en")
		}
	}
	channel := registrationChannelHint(cfg, guildID)
	if tr != nil {
		if channel != "" {
			return tr.Tf("error.slash.unregistered_with_channel", channel)
		}
		return tr.T("error.slash.unregistered_dm_only")
	}
	// Bundle-less fallback (test seam): keep wording that mentions !poracle
	// so the existing TestRegistrationErrorTextHasGuidance assertion holds.
	return "🛑 You are not registered with Poracle. DM the bot with `!poracle` to register first."
}

// registrationChannelHint returns a Discord channel mention for the
// operator-configured registration channel, or "" when no unambiguous
// channel exists. We require a non-empty guildID and exactly one entry in
// [discord] channels — anything else (zero, or multiple channels across
// multiple guilds) could mislead the user and we silently skip the hint.
func registrationChannelHint(cfg *config.Config, guildID string) string {
	if cfg == nil || guildID == "" {
		return ""
	}
	if len(cfg.Discord.Channels) != 1 {
		return ""
	}
	ch := cfg.Discord.Channels[0]
	if ch == "" {
		return ""
	}
	return "<#" + ch + ">"
}

// formatMapperError translates a *mappers.MapperError to a user-facing string.
// Falls back to the raw key when no translation is available.
func formatMapperError(err error, lang string, bundle *i18n.Bundle) string {
	me, ok := err.(*mappers.MapperError)
	if !ok || me == nil {
		return "🛑 " + err.Error()
	}
	if bundle == nil {
		return "🛑 " + me.Key
	}
	tr := bundle.For(lang)
	if tr == nil {
		return "🛑 " + me.Key
	}
	if len(me.Args) > 0 {
		return "🛑 " + tr.Tf(me.Key, me.Args...)
	}
	return "🛑 " + tr.T(me.Key)
}

// logInvocation posts a one-line audit entry to the configured
// dm_log_channel_id for every slash invocation, including those sent
// from guild channels. No-op when the channel isn't configured.
//
// Slash responses are ephemeral by default — other channel members
// don't see the invocation or the reply, so the "channel commands
// are already public" carve-out from the text path doesn't apply here.
// Channel-invoked entries include a <#channel-id> mention so the
// auditor can jump straight to where the user was. DM entries omit
// the location since there's nowhere to link to.
//
// Send failures are logged at WARN and don't block the dispatch.
func (d *Dispatcher) logInvocation(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	if s == nil || ic == nil {
		return
	}
	if d.cfgRoot == nil || d.cfgRoot.Discord.DmLogChannelID == "" {
		return
	}
	user := ic.User
	if user == nil && ic.Member != nil {
		user = ic.Member.User
	}
	if user == nil {
		return
	}
	source := "DM"
	if ic.GuildID != "" {
		source = fmt.Sprintf("<#%s>", ic.ChannelID)
	}
	logMsg := fmt.Sprintf("Slash from %s (%s) in %s: %s", user.Username, user.ID, source, formatSlashInvocation(ic))
	sent, err := s.ChannelMessageSend(d.cfgRoot.Discord.DmLogChannelID, logMsg)
	if err != nil {
		log.WithError(err).Warn("slash: failed to post invocation log entry")
		return
	}
	if sent == nil || d.cfgRoot.Discord.DmLogChannelDeletionTime <= 0 {
		return
	}
	delay := time.Duration(d.cfgRoot.Discord.DmLogChannelDeletionTime) * time.Minute
	go func(channelID, messageID string) {
		time.Sleep(delay)
		s.ChannelMessageDelete(channelID, messageID) //nolint:errcheck
	}(d.cfgRoot.Discord.DmLogChannelID, sent.ID)
}

// formatSlashInvocation reconstructs the slash command the user typed,
// e.g. "/info pokemon name:pikachu" or "/track pokemon:pikachu iv:90".
// Walks Options recursively so sub-commands (Type 1) and sub-command
// groups (Type 2) render as positional tokens, while value options
// render as key:value.
func formatSlashInvocation(ic *discordgo.InteractionCreate) string {
	data := ic.ApplicationCommandData()
	var sb strings.Builder
	sb.WriteByte('/')
	sb.WriteString(data.Name)
	appendSlashOptions(&sb, data.Options)
	return sb.String()
}

func appendSlashOptions(sb *strings.Builder, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		sb.WriteByte(' ')
		switch opt.Type {
		case discordgo.ApplicationCommandOptionSubCommand, discordgo.ApplicationCommandOptionSubCommandGroup:
			sb.WriteString(opt.Name)
			appendSlashOptions(sb, opt.Options)
		default:
			sb.WriteString(opt.Name)
			sb.WriteByte(':')
			sb.WriteString(fmt.Sprintf("%v", opt.Value))
		}
	}
}
