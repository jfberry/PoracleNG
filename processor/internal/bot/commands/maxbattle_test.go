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

func maxbattleCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}

	maxbattles := store.NewMockTrackingStore[db.MaxbattleTrackingAPI](
		store.MaxbattleGetUID, store.MaxbattleSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Maxbattles: maxbattles,
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

func runMaxbattle(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &MaxbattleCommand{}
	args := strings.Fields(input)
	return cmd.Run(ctx, args)
}

func TestMaxbattle_BasicPokemon(t *testing.T) {
	ctx := maxbattleCtx(t)
	replies := runMaxbattle(t, ctx, "25")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Maxbattles.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 25, rows[0].PokemonID)
	assert.Equal(t, 90, rows[0].Level, "pokemon tracking should use level 90 (all levels)")
}

func TestMaxbattle_ByLevel(t *testing.T) {
	ctx := maxbattleCtx(t)
	replies := runMaxbattle(t, ctx, "level3")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Maxbattles.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 3, rows[0].Level)
	assert.Equal(t, bot.WildcardID, rows[0].PokemonID, "level tracking should use wildcard pokemon")
}

func TestMaxbattle_Everything(t *testing.T) {
	ctx := maxbattleCtx(t)
	replies := runMaxbattle(t, ctx, "everything")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Maxbattles.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 90, rows[0].Level, "everything = level 90")
}

func TestMaxbattle_Duplicate(t *testing.T) {
	ctx := maxbattleCtx(t)
	replies1 := runMaxbattle(t, ctx, "25")
	require.NotEmpty(t, replies1)
	assert.Equal(t, "✅", replies1[0].React)

	replies2 := runMaxbattle(t, ctx, "25")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "👌", replies2[0].React, "duplicate should be 👌, reply: %s", replies2[0].Text)
}

func TestMaxbattle_Remove(t *testing.T) {
	ctx := maxbattleCtx(t)
	runMaxbattle(t, ctx, "25")
	rows, _ := ctx.Tracking.Maxbattles.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)

	replies := runMaxbattle(t, ctx, "remove 25")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Maxbattles.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0)
}

func TestMaxbattle_NoTarget(t *testing.T) {
	ctx := maxbattleCtx(t)
	replies := runMaxbattle(t, ctx, "clean")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
}

func TestMaxbattle_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := maxbattleCtx(t)
	ctx.IsAdmin = false

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runMaxbattle(t, ctx, "25 template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "99")
}

func TestMaxbattle_Gmax(t *testing.T) {
	ctx := maxbattleCtx(t)
	replies := runMaxbattle(t, ctx, "25 gmax")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Maxbattles.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Gmax, "gmax should be 1")
}
