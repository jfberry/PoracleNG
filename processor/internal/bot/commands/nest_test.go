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

func nestCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}

	nests := store.NewMockTrackingStore[db.NestTrackingAPI](
		store.NestGetUID, store.NestSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Nests: nests,
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

func runNest(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &NestCommand{}
	args := strings.Fields(input)
	return cmd.Run(ctx, args)
}

func TestNest_BasicPokemon(t *testing.T) {
	ctx := nestCtx(t)
	replies := runNest(t, ctx, "25")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Nests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 25, rows[0].PokemonID)
}

func TestNest_Everything(t *testing.T) {
	ctx := nestCtx(t)
	replies := runNest(t, ctx, "everything")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Nests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 0, rows[0].PokemonID, "everything = pokemon ID 0")
}

func TestNest_Duplicate(t *testing.T) {
	ctx := nestCtx(t)
	replies1 := runNest(t, ctx, "25")
	require.NotEmpty(t, replies1)
	assert.Equal(t, "✅", replies1[0].React)

	replies2 := runNest(t, ctx, "25")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "👌", replies2[0].React, "duplicate should be 👌, reply: %s", replies2[0].Text)
}

func TestNest_Remove(t *testing.T) {
	ctx := nestCtx(t)
	runNest(t, ctx, "25")
	rows, _ := ctx.Tracking.Nests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)

	replies := runNest(t, ctx, "remove 25")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Nests.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0)
}

func TestNest_NoPokemon(t *testing.T) {
	ctx := nestCtx(t)
	replies := runNest(t, ctx, "clean")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
}

func TestNest_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := nestCtx(t)
	ctx.IsAdmin = false

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runNest(t, ctx, "25 template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "99")
}

func TestNest_WithDistance(t *testing.T) {
	ctx := nestCtx(t)
	ctx.HasLocation = true
	replies := runNest(t, ctx, "25 d500")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Nests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 500, rows[0].Distance)
}
