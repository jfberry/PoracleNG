package slash

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/mappers"
	"github.com/pokemon/poracleng/processor/internal/i18n"
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
}

// commandsSkippingRegistration matches the text bot's special-case logic in
// internal/discordbot/bot.go: commands that work even when the user has not
// registered with !poracle. /version is the only Phase 1 entry; /poracle
// itself is not surfaced as a slash command.
var commandsSkippingRegistration = map[string]bool{
	"cmd.version": true,
}

func NewDispatcher(cfg Config) *Dispatcher {
	return &Dispatcher{cfg: cfg}
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
	if !commandsSkippingRegistration[cmdKey] {
		// buildContext already resolved ctx.Language via the proper chain
		// (human → Discord locale → cfgRoot.General.Locale). The registration
		// check here only consults the same store for the registered bit.
		if d.deps != nil && d.deps.Humans != nil {
			_, _, _, _, registered := bot.LookupUserStateFromStore(
				d.deps.Humans, ctx.UserID, ctx.Language)
			if !registered {
				d.respondError(s, ic, registrationErrorText(d.cfgRoot, d.bundle, ctx.Language, ic.GuildID))
				return
			}
		}
	}

	// 6. command_security check (text+slash shared config).
	//    TODO(Task 15): pull user roles for Discord and pass them in. /version
	//    has no security mapping (commandSecurityName returns "") so this
	//    check trivially passes for Phase 1.
	if !d.commandAllowed(ic, cmdKey, ctx.IsAdmin) {
		d.respondError(s, ic, fmt.Sprintf("🛑 You don't have permission to run /%s.", invoked))
		return
	}

	// 7. Map slash options to text-command tokens.
	mapperFn := mappers.Lookup(canon)
	if mapperFn == nil {
		d.respondError(s, ic, "🛑 Command not implemented.")
		return
	}
	tokens, err := mapperFn(ic.ApplicationCommandData().Options)
	if err != nil {
		d.respondError(s, ic, formatMapperError(err, ctx.Language, d.bundle))
		return
	}

	// 8. Dispatch & send.
	cmd := d.registry.Lookup(cmdKey)
	replies := cmd.Run(ctx, tokens)
	if err := Send(s, ic, replies); err != nil {
		log.WithError(err).Warnf("slash: failed to send replies for /%s", invoked)
	}
}

// HandleAutocomplete routes an ApplicationCommandAutocomplete interaction.
// No-op skeleton for Phase 0.
func (d *Dispatcher) HandleAutocomplete(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	if d == nil || ic == nil {
		return
	}
	// TODO: Task 28 — implement autocomplete routing
}

// commandAllowed checks command_security for the invoking user.
//
// TODO(Task 15): pull the user's Discord role list (from gateway state cache
// with REST fallback, matching internal/discordbot/bot.go's lazy fetchRoles)
// and call bot.CommandAllowed properly. For Phase 1 this stub returns true so
// /version (which has no security mapping anyway) dispatches cleanly.
func (d *Dispatcher) commandAllowed(ic *discordgo.InteractionCreate, cmdKey string, isAdmin bool) bool {
	_ = ic
	_ = cmdKey
	_ = isAdmin
	return true
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
// TODO(Task 44): replace this hardcoded English with the operator's
// UnregisteredUserMessage / "msg.not_registered" i18n key, matching the text
// bot's branch in internal/discordbot/bot.go.
func registrationErrorText(cfg *config.Config, bundle *i18n.Bundle, lang, guildID string) string {
	_ = cfg
	_ = bundle
	_ = lang
	_ = guildID
	return "🛑 You are not registered with Poracle. DM the bot with `!poracle` to register first."
}

// formatMapperError translates a *mappers.MapperError to a user-facing string.
// Falls back to the raw key when no translation is available.
//
// TODO(Task 44): when slash-specific error keys land in the i18n bundle this
// becomes a plain Tf call; for now the raw key is acceptable since /version
// has no mapper errors.
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
