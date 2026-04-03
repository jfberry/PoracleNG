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

func eggCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}

	eggs := store.NewMockTrackingStore[db.EggTrackingAPI](
		store.EggGetUID, store.EggSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Eggs: eggs,
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

func runEgg(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &EggCommand{}
	args := strings.Fields(input)
	return cmd.Run(ctx, args)
}

func TestEgg_BasicLevel(t *testing.T) {
	ctx := eggCtx(t)
	replies := runEgg(t, ctx, "level5")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Eggs.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 5, rows[0].Level)
}

func TestEgg_LevelRange(t *testing.T) {
	ctx := eggCtx(t)
	replies := runEgg(t, ctx, "level3-5")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Eggs.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 3, "should create rows for levels 3, 4, 5")
}

func TestEgg_Duplicate(t *testing.T) {
	ctx := eggCtx(t)
	replies1 := runEgg(t, ctx, "level5")
	require.NotEmpty(t, replies1)
	assert.Equal(t, "✅", replies1[0].React)

	replies2 := runEgg(t, ctx, "level5")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "👌", replies2[0].React, "duplicate should be 👌, reply: %s", replies2[0].Text)
}

func TestEgg_Remove(t *testing.T) {
	ctx := eggCtx(t)
	runEgg(t, ctx, "level5")
	rows, _ := ctx.Tracking.Eggs.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)

	replies := runEgg(t, ctx, "remove level5")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Eggs.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0)
}

func TestEgg_NoLevel(t *testing.T) {
	ctx := eggCtx(t)
	replies := runEgg(t, ctx, "clean")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
}

func TestEgg_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := eggCtx(t)
	ctx.IsAdmin = false

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runEgg(t, ctx, "level5 template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "99")
}

func TestEgg_InvalidTemplate_Admin(t *testing.T) {
	ctx := eggCtx(t)
	ctx.IsAdmin = true

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runEgg(t, ctx, "level5 template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "admin should not be blocked, reply: %s", replies[0].Text)
	assert.Contains(t, replies[0].Text, "99")
}
