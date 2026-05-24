package slash

import (
	"errors"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/mappers"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

func TestNewDispatcherStoresConfig(t *testing.T) {
	cfg := Config{Enabled: true, Global: true}
	d := NewDispatcher(cfg)
	if !d.cfg.Enabled {
		t.Error("cfg.Enabled lost")
	}
}

func TestHandleCommandSkipsWhenNoCommand(t *testing.T) {
	d := NewDispatcher(Config{})
	// No registration; HandleCommand should return without panic
	d.HandleCommand(nil, nil)
}

func TestAttachStoresSessionAndDeps(t *testing.T) {
	d := NewDispatcher(Config{Enabled: true})
	s := &discordgo.Session{}
	deps := &bot.BotDeps{}
	reg := &bot.Registry{}
	bundle := &i18n.Bundle{}
	cfg := &config.Config{}

	d.Attach(s, deps, reg, bundle, cfg)

	if d.session != s {
		t.Error("session not stored")
	}
	if d.deps != deps {
		t.Error("deps not stored")
	}
	if d.registry != reg {
		t.Error("registry not stored")
	}
	if d.bundle != bundle {
		t.Error("bundle not stored")
	}
	if d.cfgRoot != cfg {
		t.Error("cfgRoot not stored")
	}
	// Attach should also have populated nameToKey so HandleCommand can route.
	if d.nameToKey == nil {
		t.Error("nameToKey not built by Attach")
	}
}

func TestBuildNameMapCanonicalLookup(t *testing.T) {
	// Bundle without any slash.cmd.* overrides — every command maps to its
	// canonical short name.
	bundle := testBundle(t)
	d := NewDispatcher(Config{})
	d.bundle = bundle
	d.nameToKey = d.buildNameMap()

	got := d.resolveCommandKey("version")
	if got != "cmd.version" {
		t.Errorf("resolveCommandKey(version)=%q, want cmd.version", got)
	}

	if d.resolveCommandKey("not-a-command") != "" {
		t.Error("unknown name should resolve to empty")
	}
}

func TestBuildNameMapHonoursOperatorRename(t *testing.T) {
	// Operator override renamed /version → /poracle-version. Both the
	// canonical and the renamed name should route to "cmd.version" so an
	// admin can hand-edit and live-reload without losing the dispatch.
	bundle := testBundle(t, withOverride("en", "slash.cmd.version", "poracle-version"))
	d := NewDispatcher(Config{})
	d.bundle = bundle
	d.nameToKey = d.buildNameMap()

	if got := d.resolveCommandKey("poracle-version"); got != "cmd.version" {
		t.Errorf("renamed lookup = %q, want cmd.version", got)
	}
	// Canonical name still routes too — useful while migrating.
	if got := d.resolveCommandKey("version"); got != "cmd.version" {
		t.Errorf("canonical lookup = %q, want cmd.version", got)
	}
}

func TestBuildContextSetsCoreFields(t *testing.T) {
	bundle := testBundle(t)
	cfg := &config.Config{}
	cfg.General.Locale = "de"

	d := NewDispatcher(Config{})
	d.bundle = bundle
	d.cfgRoot = cfg

	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Member: &discordgo.Member{
			User: &discordgo.User{ID: "u-1", Username: "alice"},
		},
	}}

	ctx, err := d.buildContext(ic, "cmd.version")
	if err != nil {
		t.Fatalf("buildContext err: %v", err)
	}
	if ctx.UserID != "u-1" {
		t.Errorf("UserID=%q", ctx.UserID)
	}
	if ctx.UserName != "alice" {
		t.Errorf("UserName=%q", ctx.UserName)
	}
	if ctx.Platform != "discord" {
		t.Errorf("Platform=%q", ctx.Platform)
	}
	if ctx.ChannelID != "chan-1" {
		t.Errorf("ChannelID=%q", ctx.ChannelID)
	}
	if ctx.GuildID != "guild-1" {
		t.Errorf("GuildID=%q", ctx.GuildID)
	}
	if ctx.IsDM {
		t.Error("IsDM should be false in guild context")
	}
	if ctx.Language != "de" {
		t.Errorf("Language=%q, want de", ctx.Language)
	}
	if ctx.TargetID != "u-1" {
		t.Errorf("TargetID=%q, want u-1", ctx.TargetID)
	}
	if ctx.TargetType != bot.TypeDiscordUser {
		t.Errorf("TargetType=%q, want %q", ctx.TargetType, bot.TypeDiscordUser)
	}
	if ctx.Config != cfg {
		t.Error("Config not wired through")
	}
	if ctx.Translations != bundle {
		t.Error("Translations not wired through")
	}
}

func TestBuildContextDMHasEmptyGuild(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}

	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ChannelID: "dm-chan",
		User:      &discordgo.User{ID: "u-2", Username: "bob"},
	}}

	ctx, err := d.buildContext(ic, "cmd.version")
	if err != nil {
		t.Fatalf("buildContext err: %v", err)
	}
	if !ctx.IsDM {
		t.Error("IsDM should be true when GuildID empty")
	}
	if ctx.UserID != "u-2" {
		t.Errorf("UserID=%q, want u-2 (fallback from User, not Member)", ctx.UserID)
	}
}

func TestBuildContextLanguageDefaultsToEnglish(t *testing.T) {
	// Operator config without an explicit locale should not surface as an
	// empty Language string downstream — fall back to "en".
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}

	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		User: &discordgo.User{ID: "u-3"},
	}}

	ctx, _ := d.buildContext(ic, "cmd.version")
	if ctx.Language != "en" {
		t.Errorf("Language=%q, want en", ctx.Language)
	}
}

func TestFormatMapperErrorMapperKey(t *testing.T) {
	// Bundle with a known mapper-error key. formatMapperError should look it up.
	bundle := testBundle(t, withOverride("en", "mapper.err.bad_input", "Bad input: {0}"))
	merr := &mappers.MapperError{Key: "mapper.err.bad_input", Args: []any{"pikachu"}}

	got := formatMapperError(merr, "en", bundle)
	if !strings.Contains(got, "Bad input: pikachu") {
		t.Errorf("formatMapperError=%q, want translated text with arg", got)
	}
	if !strings.HasPrefix(got, "🛑") {
		t.Errorf("formatMapperError=%q, want error-emoji prefix", got)
	}
}

func TestFormatMapperErrorFallsBackToKey(t *testing.T) {
	bundle := testBundle(t) // no mapper-error keys
	merr := &mappers.MapperError{Key: "mapper.err.unknown"}

	got := formatMapperError(merr, "en", bundle)
	// Translator.T returns the key itself when missing; that's the fallback
	// when the i18n bundle has no entry for the mapper error.
	if !strings.Contains(got, "mapper.err.unknown") {
		t.Errorf("formatMapperError=%q, want fallback to key", got)
	}
}

func TestFormatMapperErrorNonMapperError(t *testing.T) {
	// Plain error wraps to the same "🛑 …" shape, using Error() text.
	got := formatMapperError(errors.New("boom"), "en", testBundle(t))
	if got != "🛑 boom" {
		t.Errorf("formatMapperError=%q, want \"🛑 boom\"", got)
	}
}

func TestRegistrationErrorTextHasGuidance(t *testing.T) {
	// Bundle-less call path (defensive: should not panic, must mention
	// "poracle" so unregistered users see what to do).
	got := registrationErrorText(nil, nil, "en", "")
	if !strings.Contains(strings.ToLower(got), "poracle") {
		t.Errorf("registrationErrorText=%q, expected mention of !poracle", got)
	}
}

func TestRegistrationErrorTextDMOnlyWhenNoChannelConfigured(t *testing.T) {
	bundle := testBundle(t,
		withOverride("en", "error.slash.unregistered_dm_only", "🛑 Register first. DM !poracle."),
		withOverride("en", "error.slash.unregistered_with_channel", "🛑 Register first. DM !poracle or use {0}."),
	)
	cfg := &config.Config{}
	// No channels configured → DM-only form even when guildID is set.
	got := registrationErrorText(cfg, bundle, "en", "guild-1")
	if !strings.Contains(got, "DM !poracle") {
		t.Errorf("expected DM-only form, got %q", got)
	}
	if strings.Contains(got, "<#") {
		t.Errorf("did not expect channel mention, got %q", got)
	}
}

func TestRegistrationErrorTextChannelHintWhenSingleChannel(t *testing.T) {
	bundle := testBundle(t,
		withOverride("en", "error.slash.unregistered_dm_only", "🛑 Register first. DM !poracle."),
		withOverride("en", "error.slash.unregistered_with_channel", "🛑 Register first. DM !poracle or use {0}."),
	)
	cfg := &config.Config{}
	cfg.Discord.Channels = []string{"chan-42"}

	got := registrationErrorText(cfg, bundle, "en", "guild-1")
	if !strings.Contains(got, "<#chan-42>") {
		t.Errorf("expected channel mention <#chan-42>, got %q", got)
	}
}

func TestRegistrationErrorTextSkipsHintWhenMultipleChannels(t *testing.T) {
	// Multiple channels — we can't tell which guild they belong to, so
	// degrade to the DM-only message rather than mis-link the user.
	bundle := testBundle(t,
		withOverride("en", "error.slash.unregistered_dm_only", "🛑 Register first. DM !poracle."),
		withOverride("en", "error.slash.unregistered_with_channel", "🛑 Register first. DM !poracle or use {0}."),
	)
	cfg := &config.Config{}
	cfg.Discord.Channels = []string{"chan-1", "chan-2"}

	got := registrationErrorText(cfg, bundle, "en", "guild-1")
	if strings.Contains(got, "<#") {
		t.Errorf("expected no channel mention with multiple channels, got %q", got)
	}
}

func TestRegistrationErrorTextLanguageFallback(t *testing.T) {
	// Bundle has only English keys; a user with lang="de" still gets a
	// usable message via the English fallback.
	bundle := testBundle(t,
		withOverride("en", "error.slash.unregistered_dm_only", "Register first via !poracle (English)."),
	)
	got := registrationErrorText(&config.Config{}, bundle, "de", "")
	if !strings.Contains(got, "English") {
		t.Errorf("expected English fallback for missing German entry, got %q", got)
	}
}

// dispatcherWithSecurity constructs a Dispatcher whose cfgRoot has the given
// command_security mapping (security key → allow list of user/role IDs).
func dispatcherWithSecurity(t *testing.T, security map[string][]string) *Dispatcher {
	t.Helper()
	cfg := &config.Config{}
	cfg.Discord.CommandSecurity = security
	d := NewDispatcher(Config{})
	d.cfgRoot = cfg
	d.bundle = testBundle(t)
	return d
}

// buildInteractionWithRoles is a guild-style invocation with Member populated
// (so userRoles returns the supplied list). Mirrors buildInteraction but
// promotes the User into Member.User and adds Roles.
func buildInteractionWithRoles(userID, guildID string, roles []string) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type:    discordgo.InteractionApplicationCommand,
		GuildID: guildID,
		Member: &discordgo.Member{
			User:  &discordgo.User{ID: userID, Username: userID},
			Roles: roles,
		},
	}}
}

func TestCommandAllowedAdminBypass(t *testing.T) {
	// Admin should pass even when command_security would otherwise deny.
	d := dispatcherWithSecurity(t, map[string][]string{
		"monster": {"role-tracker"},
	})
	ic := buildInteractionWithRoles("42", "guild-1", nil)
	if !d.commandAllowed(ic, "cmd.track", true) {
		t.Error("admin should bypass command_security")
	}
}

func TestCommandAllowedRoleGated(t *testing.T) {
	// User has role-tracker, which is the only role allowed for monster.
	// Should pass for cmd.track (mapped to "monster") and fail for cmd.raid
	// (mapped to "raid", which has no allow list configured — so it passes
	// trivially). To make a meaningful negative case, restrict "raid" too.
	d := dispatcherWithSecurity(t, map[string][]string{
		"monster": {"role-tracker"},
		"raid":    {"role-raid-admin"},
	})
	ic := buildInteractionWithRoles("42", "guild-1", []string{"role-tracker"})

	if !d.commandAllowed(ic, "cmd.track", false) {
		t.Error("cmd.track: user with role-tracker should be allowed")
	}
	if d.commandAllowed(ic, "cmd.raid", false) {
		t.Error("cmd.raid: user without role-raid-admin should be denied")
	}
}

func TestCommandAllowedNoRoles(t *testing.T) {
	// User with no roles, but command is restricted — should be denied.
	d := dispatcherWithSecurity(t, map[string][]string{
		"monster": {"role-tracker"},
	})
	ic := buildInteractionWithRoles("42", "guild-1", nil)
	if d.commandAllowed(ic, "cmd.track", false) {
		t.Error("user with no roles should be denied for restricted command")
	}
}

func TestCommandAllowedUnconfiguredCommand(t *testing.T) {
	// No command_security at all — every command should pass (except admin
	// bypass which is moot). cmd.version has no security mapping anyway, so
	// it passes regardless; cmd.track also passes because nothing is
	// configured.
	d := dispatcherWithSecurity(t, nil)
	ic := buildInteractionWithRoles("42", "guild-1", nil)
	if !d.commandAllowed(ic, "cmd.track", false) {
		t.Error("command without security config should be allowed")
	}
	if !d.commandAllowed(ic, "cmd.version", false) {
		t.Error("cmd.version (no security mapping) should always be allowed")
	}
}

func TestCommandAllowedDMHasNoRoles(t *testing.T) {
	// DM-style interaction: Member is nil. userRoles returns nil. Restricted
	// commands fail closed; unrestricted commands still pass.
	d := dispatcherWithSecurity(t, map[string][]string{
		"monster": {"role-tracker"},
	})
	ic := buildInteraction("42", "") // DM: User populated, Member nil
	if ic.Member != nil {
		t.Fatal("test setup: expected DM-style interaction with nil Member")
	}
	if d.commandAllowed(ic, "cmd.track", false) {
		t.Error("DM with no roles should be denied for restricted command")
	}
	// Unrestricted command still passes via the empty securityName path.
	if !d.commandAllowed(ic, "cmd.version", false) {
		t.Error("DM should still allow commands with no security mapping")
	}
}

func TestCommandAllowedUserIDInAllowList(t *testing.T) {
	// command_security allow lists can name user IDs directly (not just role
	// IDs). User 42 is explicitly named for "monster", so they pass even
	// with no roles attached.
	d := dispatcherWithSecurity(t, map[string][]string{
		"monster": {"42"},
	})
	ic := buildInteractionWithRoles("42", "guild-1", nil)
	if !d.commandAllowed(ic, "cmd.track", false) {
		t.Error("user named directly in allow list should be allowed")
	}
}

func TestLookupRolesDMNoGuildsConfigured(t *testing.T) {
	d := NewDispatcher(Config{})
	ic := buildInteraction("42", "") // DM: no Member
	// No cfgRoot wired and no session — DM branch can't fetch
	// anything, returns nil. Standalone Dispatcher (no Attach call)
	// pins the safe fall-through.
	if got := d.lookupRoles(ic, "42"); got != nil {
		t.Errorf("lookupRoles(DM, unwired)=%v, want nil", got)
	}
}

func TestLookupRolesGuildUsesMemberRoles(t *testing.T) {
	d := NewDispatcher(Config{})
	ic := buildInteractionWithRoles("42", "guild-1", []string{"r1", "r2"})
	got := d.lookupRoles(ic, "42")
	if len(got) != 2 || got[0] != "r1" || got[1] != "r2" {
		t.Errorf("lookupRoles=%v, want [r1 r2]", got)
	}
}

// --- autocomplete dispatcher helpers ---

func TestNewDispatcherRegistersAutocompleteListers(t *testing.T) {
	// The constructor must wire the three built-in listers so HandleAutocomplete
	// can route tracking/area/profile options without further setup.
	d := NewDispatcher(Config{})
	if d.autocompleteRegistry == nil {
		t.Fatal("autocompleteRegistry not constructed")
	}
	for _, name := range []string{"tracking", "areas", "profiles"} {
		if d.autocompleteRegistry.Lookup(name) == nil {
			t.Errorf("lister %q not registered", name)
		}
	}
}

func TestFocusedOptionTopLevel(t *testing.T) {
	opts := []*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "iv", Focused: true, Type: discordgo.ApplicationCommandOptionString, Value: "0-100"},
	}
	got := focusedOption(opts)
	if got == nil || got.Name != "iv" {
		t.Errorf("got %v, want option named iv", got)
	}
}

func TestFocusedOptionNestedInSubCommand(t *testing.T) {
	// /untrack raid <tracking> — sub-command "raid" wraps the focused option.
	opts := []*discordgo.ApplicationCommandInteractionDataOption{
		{
			Name: "raid",
			Type: discordgo.ApplicationCommandOptionSubCommand,
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{Name: "tracking", Focused: true, Type: discordgo.ApplicationCommandOptionString, Value: ""},
			},
		},
	}
	got := focusedOption(opts)
	if got == nil || got.Name != "tracking" {
		t.Errorf("got %v, want nested option named tracking", got)
	}
}

func TestFocusedOptionNoneFocused(t *testing.T) {
	opts := []*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "iv"},
		{Name: "pokemon"},
	}
	if got := focusedOption(opts); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestFocusedOptionEmptySlice(t *testing.T) {
	if got := focusedOption(nil); got != nil {
		t.Errorf("nil opts: got %v, want nil", got)
	}
}

func TestFocusedStringValueStringType(t *testing.T) {
	opt := &discordgo.ApplicationCommandInteractionDataOption{
		Type:  discordgo.ApplicationCommandOptionString,
		Value: "pika",
	}
	if got := focusedStringValue(opt); got != "pika" {
		t.Errorf("got %q, want pika", got)
	}
}

func TestFocusedStringValueNonString(t *testing.T) {
	// Defensive read: an integer-typed option should not panic, but return ""
	// (or the underlying string value if Discord sent one regardless).
	opt := &discordgo.ApplicationCommandInteractionDataOption{
		Type:  discordgo.ApplicationCommandOptionInteger,
		Value: float64(42),
	}
	if got := focusedStringValue(opt); got != "" {
		t.Errorf("non-string opt: got %q, want empty", got)
	}
}

func TestFocusedStringValueNil(t *testing.T) {
	if got := focusedStringValue(nil); got != "" {
		t.Errorf("nil opt: got %q, want empty", got)
	}
}

func TestDtsTypeForKnownMappings(t *testing.T) {
	cases := map[string]string{
		"track":     "monster",
		"fort":      "fort-update",
		"raid":      "raid",
		"egg":       "egg",
		"quest":     "quest",
		"invasion":  "invasion",
		"lure":      "lure",
		"nest":      "nest",
		"gym":       "gym",
		"maxbattle": "maxbattle",
	}
	for cmd, want := range cases {
		if got := dtsTypeFor(cmd); got != want {
			t.Errorf("dtsTypeFor(%q)=%q, want %q", cmd, got, want)
		}
	}
}

func TestFindUntrackSubtypeReturnsSubCommandName(t *testing.T) {
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type: discordgo.InteractionApplicationCommandAutocomplete,
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "untrack",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{
					Name: "raid",
					Type: discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandInteractionDataOption{
						{Name: "tracking", Focused: true, Type: discordgo.ApplicationCommandOptionString, Value: ""},
					},
				},
			},
		},
	}}
	if got := findUntrackSubtype(ic); got != "raid" {
		t.Errorf("findUntrackSubtype=%q, want raid", got)
	}
}

func TestFindUntrackSubtypeNoSubCommand(t *testing.T) {
	// Flat options (no sub-command) — caller treats empty as "no subtype hint".
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type: discordgo.InteractionApplicationCommandAutocomplete,
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "untrack",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{Name: "tracking", Focused: true, Type: discordgo.ApplicationCommandOptionString},
			},
		},
	}}
	if got := findUntrackSubtype(ic); got != "" {
		t.Errorf("findUntrackSubtype=%q, want empty (no sub-command)", got)
	}
}

func TestFindUntrackSubtypeNilInteraction(t *testing.T) {
	if got := findUntrackSubtype(nil); got != "" {
		t.Errorf("findUntrackSubtype(nil)=%q, want empty", got)
	}
}

func TestRouteAutocompleteUnknownTupleReturnsNil(t *testing.T) {
	// An option name we don't recognise should return nil — the caller still
	// emits an empty autocomplete response, but we don't fabricate suggestions.
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	got := d.routeAutocomplete("track", "unknownopt", "abc", "en", nil)
	if got != nil {
		t.Errorf("unknown opt: got %v, want nil", got)
	}
}

func TestUserstateAutocompleteUnknownListerReturnsNil(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	got := d.userstateAutocomplete(nil, "no-such-lister", "", "")
	if got != nil {
		t.Errorf("unknown lister: got %v, want nil", got)
	}
}

// boostRouteDeps builds a minimal BotDeps with translated pokemon/items/grunts
// and a primed RecentActivity tracker — used for testing dispatcher routing
// of the recent-activity boost across /maxbattle, /quest, and /invasion.
func boostRouteDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25":      "Pikachu",
		"poke_6":       "Charizard",
		"poke_150":     "Mewtwo",
		"item_706":     "Golden Razz Berry",
		"poke_type_10": "Fire",
		"poke_type_11": "Water",
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}:  {PokemonID: 25},
			{ID: 6, Form: 0}:   {PokemonID: 6},
			{ID: 150, Form: 0}: {PokemonID: 150},
		},
		Items: map[int]*gamedata.Item{
			706: {ItemID: 706},
		},
		Grunts: map[int]*gamedata.Grunt{
			1: {Template: "CHARACTER_FIRE_GRUNT_MALE", TypeID: 10},
			2: {Template: "CHARACTER_WATER_GRUNT_FEMALE", TypeID: 11},
		},
	}
	ra := tracker.NewRecentActivity()
	ra.RecordMaxBattleBoss(150)
	ra.RecordQuestPokemon(6)
	ra.RecordQuestCandy(25)
	ra.RecordQuestMega(6)
	ra.RecordQuestItem(706)
	ra.RecordInvasionGrunt(11) // TypeID = 11 (Water)
	return &bot.BotDeps{
		Translations:   bundle,
		GameData:       gd,
		Cfg:            &config.Config{},
		RecentActivity: ra,
	}
}

func TestRouteAutocomplete_MaxBattlePokemon_BoostsRecentActivity(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = boostRouteDeps(t)
	out := d.routeAutocomplete("maxbattle", "pokemon", "", "en", nil)
	if len(out) == 0 || out[0].Name != "Mewtwo" {
		t.Errorf("/maxbattle pokemon empty focused: first=%+v, want Mewtwo (RecentActivity-active)", firstName(out))
	}
}

func TestRouteAutocomplete_QuestPokemon_BoostsRecentActivity(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = boostRouteDeps(t)
	out := d.routeAutocomplete("quest", "pokemon", "", "en", nil)
	if len(out) == 0 || out[0].Name != "Charizard" {
		t.Errorf("/quest pokemon empty focused: first=%+v, want Charizard", firstName(out))
	}
}

func TestRouteAutocomplete_QuestCandy_BoostsRecentActivity(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = boostRouteDeps(t)
	out := d.routeAutocomplete("quest", "candy", "", "en", nil)
	if len(out) == 0 || out[0].Name != "Pikachu" {
		t.Errorf("/quest candy empty focused: first=%+v, want Pikachu", firstName(out))
	}
}

func TestRouteAutocomplete_QuestMegaEnergy_BoostsRecentActivity(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = boostRouteDeps(t)
	out := d.routeAutocomplete("quest", "mega_energy", "", "en", nil)
	if len(out) == 0 || out[0].Name != "Charizard" {
		t.Errorf("/quest mega_energy empty focused: first=%+v, want Charizard", firstName(out))
	}
}

func TestRouteAutocomplete_QuestItem_BoostsRecentActivity(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = boostRouteDeps(t)
	out := d.routeAutocomplete("quest", "item", "", "en", nil)
	if len(out) == 0 || out[0].Name != "Golden Razz Berry" {
		t.Errorf("/quest item empty focused: first=%+v, want Golden Razz Berry", firstName(out))
	}
}

func TestRouteAutocomplete_InvasionGruntType_BoostsRecentActivity(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = boostRouteDeps(t)
	out := d.routeAutocomplete("invasion", "grunt_type", "", "en", nil)
	if len(out) == 0 || out[0].Name != "Water Grunt" {
		t.Errorf("/invasion grunt_type empty focused: first=%+v, want Water Grunt (TypeID 11)", firstName(out))
	}
}

// On non-empty focused the boost stays out of the way — the user is
// typing a specific search, not browsing for active entities.
func TestRouteAutocomplete_QuestPokemon_NonEmptyFocusedNoBoost(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = boostRouteDeps(t)
	// Search for "pika" — RecentActivity has Charizard (6), so a non-typed
	// boost would surface it first. With typed search active, the underlying
	// Pokemon autocomplete should put Pikachu first.
	out := d.routeAutocomplete("quest", "pokemon", "pika", "en", nil)
	if len(out) == 0 || out[0].Name != "Pikachu" {
		t.Errorf("non-empty focused 'pika': first=%+v, want Pikachu (no boost)", firstName(out))
	}
}

// Nil RecentActivity must not crash; route falls back to the underlying
// autocomplete provider unchanged.
func TestRouteAutocomplete_NilRecentActivity_Survives(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	deps := boostRouteDeps(t)
	deps.RecentActivity = nil
	d.deps = deps
	out := d.routeAutocomplete("maxbattle", "pokemon", "", "en", nil)
	if len(out) == 0 {
		t.Errorf("/maxbattle pokemon with nil RecentActivity: got empty, want fallthrough to Pokemon autocomplete")
	}
}

func firstName(c []*discordgo.ApplicationCommandOptionChoice) string {
	if len(c) == 0 {
		return "<empty>"
	}
	return c[0].Name
}
