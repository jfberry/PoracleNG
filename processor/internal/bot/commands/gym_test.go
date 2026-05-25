package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gymCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}

	gyms := store.NewMockTrackingStore[db.GymTrackingAPI](
		store.GymGetUID, store.GymSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Gyms: gyms,
	}

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Moves:    map[int]*gamedata.Move{},
		Types:    map[int]*gamedata.TypeInfo{},
	}

	resolver := bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.Resolver = resolver
	ctx.ArgMatcher = bot.NewArgMatcher(ctx.Translations, gd, resolver, []string{"en"})
	ctx.GameData = gd
	ctx.RowText = &rowtext.Generator{
		GD:                  gd,
		Translations:        ctx.Translations,
		DefaultTemplateName: "1",
	}
	ctx.HasArea = true

	return ctx
}

func runGym(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &GymCommand{}
	// Mimic the bot parser's underscore→space replacement per token
	tokens := strings.Fields(input)
	for i, tok := range tokens {
		tokens[i] = strings.ReplaceAll(tok, "_", " ")
	}
	return cmd.Run(ctx, tokens)
}

// TestGym_SpecificGym_RoleDenied — when command_security.specificgym
// is set and the user lacks the matching role, gym:<id> must be
// rejected before the gym row is created. PoracleJS never accepted
// gym:<id> from the command at all; PoracleNG keeps the parsing but
// gates it behind the same permission key the delivery-time
// blocked_alerts check uses.
func TestGym_SpecificGym_RoleDenied(t *testing.T) {
	ctx := gymCtx(t)
	ctx.Platform = "discord"
	ctx.UserID = "u1"
	ctx.UserRoles = []string{"some-other-role"}
	ctx.Config.Discord.CommandSecurity = map[string][]string{
		"specificgym": {"role-a"},
	}

	replies := runGym(t, ctx, "mystic gym:abc123")
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}
	if replies[0].React != "🙅" {
		t.Errorf("expected 🙅 for specificgym-denied user, got %q (text=%q)", replies[0].React, replies[0].Text)
	}
	// And no gym row should have been created.
	rows, _ := ctx.Tracking.Gyms.SelectByIDProfile("user1", 1)
	if len(rows) != 0 {
		t.Errorf("specificgym-denied user should not have a gym row stored; got %d", len(rows))
	}
}

// TestGym_SpecificGym_RoleAllowed: same config, user has the role →
// command proceeds (resolveGymRef runs; with a literal id it succeeds
// and a row is stored).
func TestGym_SpecificGym_RoleAllowed(t *testing.T) {
	ctx := gymCtx(t)
	ctx.Platform = "discord"
	ctx.UserID = "u1"
	ctx.UserRoles = []string{"role-a"}
	ctx.Config.Discord.CommandSecurity = map[string][]string{
		"specificgym": {"role-a"},
	}

	replies := runGym(t, ctx, "mystic gym:abc123.16")
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}
	// Past the permission gate — downstream errors (e.g. "no scanner
	// configured" from resolveGymRef) are OK; what matters is the
	// reply text isn't the "no permission" message.
	if replies[0].Text == "You do not have permission to execute this command" {
		t.Errorf("specificgym-allowed user blocked by permission gate; got %q", replies[0].Text)
	}
}

// TestGym_SpecificGym_NoConfigUnrestricted: when command_security.specificgym
// is not configured at all, every user can use gym:<id> (existing
// PoracleNG behaviour preserved — the gate only activates when
// operators opt in).
func TestGym_SpecificGym_NoConfigUnrestricted(t *testing.T) {
	ctx := gymCtx(t)
	ctx.Platform = "discord"
	ctx.UserID = "u1"
	ctx.UserRoles = nil // no roles at all

	replies := runGym(t, ctx, "mystic gym:abc123.16")
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}
	// Same pin: downstream failures (scanner not configured) are
	// acceptable; "no permission" is not.
	if replies[0].Text == "You do not have permission to execute this command" {
		t.Errorf("user denied with no specificgym security configured; got %q", replies[0].Text)
	}
}

func TestGym_DefaultTeam(t *testing.T) {
	ctx := gymCtx(t)
	// No team specified — defaults to team 4 (any)
	replies := runGym(t, ctx, "clean")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Gyms.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 4, rows[0].Team, "default team should be 4 (any)")
}

func TestGym_Everything(t *testing.T) {
	ctx := gymCtx(t)
	replies := runGym(t, ctx, "everything")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Gyms.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 4, "everything should create rows for teams 0,1,2,3")
}

func TestGym_SlotChanges(t *testing.T) {
	ctx := gymCtx(t)
	replies := runGym(t, ctx, "slot_changes")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Gyms.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.True(t, bool(rows[0].SlotChanges), "slot_changes should be true")
}

func TestGym_BattleChanges_Disabled(t *testing.T) {
	ctx := gymCtx(t)
	ctx.Config.Tracking.EnableGymBattle = false

	replies := runGym(t, ctx, "battle_changes")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React, "battle_changes should be blocked when disabled")
}

func TestGym_BattleChanges_Enabled(t *testing.T) {
	ctx := gymCtx(t)
	ctx.Config.Tracking.EnableGymBattle = true

	replies := runGym(t, ctx, "battle_changes")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Gyms.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.True(t, bool(rows[0].BattleChanges), "battle_changes should be true")
}

func TestGym_Duplicate(t *testing.T) {
	ctx := gymCtx(t)
	replies1 := runGym(t, ctx, "clean")
	require.NotEmpty(t, replies1)
	assert.Equal(t, "✅", replies1[0].React)

	replies2 := runGym(t, ctx, "clean")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "👌", replies2[0].React, "duplicate should be 👌, reply: %s", replies2[0].Text)
}

func TestGym_Remove(t *testing.T) {
	ctx := gymCtx(t)
	runGym(t, ctx, "everything")
	rows, _ := ctx.Tracking.Gyms.SelectByIDProfile("user1", 1)
	require.True(t, len(rows) > 0)

	replies := runGym(t, ctx, "remove everything")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Gyms.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0)
}

func TestGym_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := gymCtx(t)
	ctx.IsAdmin = false

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runGym(t, ctx, "template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "99")
}

func TestGym_AcceptsAreaOverride(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)

	gyms := store.NewMockTrackingStore[db.GymTrackingAPI](
		store.GymGetUID, store.GymSetUID,
	)
	ctx.Tracking = &store.TrackingStores{Gyms: gyms}

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Moves:    map[int]*gamedata.Move{},
		Types:    map[int]*gamedata.TypeInfo{},
	}
	resolver := bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.Resolver = resolver
	ctx.ArgMatcher = bot.NewArgMatcher(ctx.Translations, gd, resolver, []string{"en"})
	ctx.GameData = gd
	ctx.RowText = &rowtext.Generator{GD: gd, Translations: ctx.Translations, DefaultTemplateName: "1"}
	ctx.HasArea = true

	cmd := &GymCommand{}
	replies := cmd.Run(ctx, strings.Fields("area:london"))
	require.NotEmpty(t, replies)
	assert.NotEqual(t, "🙅", replies[0].React, "rejected: %+v", replies)

	rules, _ := ctx.Tracking.Gyms.SelectByIDProfile("user1", 1)
	require.Len(t, rules, 1)
	assert.Len(t, rules[0].OverrideAreas, 1, "override not stored: %+v", rules[0])
}
