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

func raidCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}

	raids := store.NewMockTrackingStore[db.RaidTrackingAPI](
		store.RaidGetUID, store.RaidSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Raids: raids,
	}

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
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

func runRaid(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &RaidCommand{}
	args := strings.Fields(input)
	return cmd.Run(ctx, args)
}

func TestRaid_BasicPokemon(t *testing.T) {
	ctx := raidCtx(t)
	replies := runRaid(t, ctx, "25")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 25, rows[0].PokemonID)
	assert.Equal(t, bot.WildcardID, rows[0].Level, "pokemon tracking should use wildcard level")
}

func TestRaid_ByLevel(t *testing.T) {
	ctx := raidCtx(t)
	replies := runRaid(t, ctx, "level5")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 5, rows[0].Level)
	assert.Equal(t, bot.WildcardID, rows[0].PokemonID, "level tracking should use wildcard pokemon")
}

func TestRaid_Duplicate(t *testing.T) {
	ctx := raidCtx(t)
	replies1 := runRaid(t, ctx, "25")
	require.NotEmpty(t, replies1)
	assert.Equal(t, "✅", replies1[0].React)

	replies2 := runRaid(t, ctx, "25")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "👌", replies2[0].React, "duplicate should be 👌, reply: %s", replies2[0].Text)
}

func TestRaid_Remove(t *testing.T) {
	ctx := raidCtx(t)
	// Add first
	runRaid(t, ctx, "25")
	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)

	// Remove
	replies := runRaid(t, ctx, "remove 25")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0)
}

func TestRaid_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := raidCtx(t)
	ctx.IsAdmin = false

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runRaid(t, ctx, "25 template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "99")
}

func TestRaid_InvalidTemplate_Admin(t *testing.T) {
	ctx := raidCtx(t)
	ctx.IsAdmin = true

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runRaid(t, ctx, "25 template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "admin should not be blocked, reply: %s", replies[0].Text)
	assert.Contains(t, replies[0].Text, "99")
}

func TestRaid_NoTarget(t *testing.T) {
	ctx := raidCtx(t)
	// No pokemon, no level — should fail
	replies := runRaid(t, ctx, "clean")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
}
