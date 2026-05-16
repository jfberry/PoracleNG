package slash

import (
	"errors"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/mappers"
	"github.com/pokemon/poracleng/processor/internal/i18n"
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
	// Translator.T returns the key itself when missing; that's acceptable for
	// Phase 1 — Task 44 will replace this with proper i18n.
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
	// Phase 1 hardcoded message — sanity check that the text mentions
	// "poracle" so users know what to do.
	got := registrationErrorText(nil, nil, "en", "")
	if !strings.Contains(strings.ToLower(got), "poracle") {
		t.Errorf("registrationErrorText=%q, expected mention of !poracle", got)
	}
}
