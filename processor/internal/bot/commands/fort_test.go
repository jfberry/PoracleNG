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

func fortCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}

	forts := store.NewMockTrackingStore[db.FortTrackingAPI](
		store.FortGetUID, store.FortSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Forts: forts,
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

func runFort(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &FortCommand{}
	args := strings.Fields(input)
	return cmd.Run(ctx, args)
}

func TestFort_Everything(t *testing.T) {
	ctx := fortCtx(t)
	replies := runFort(t, ctx, "everything")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Forts.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, "everything", rows[0].FortType)
}

func TestFort_Pokestop(t *testing.T) {
	ctx := fortCtx(t)
	replies := runFort(t, ctx, "pokestop")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Forts.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, "pokestop", rows[0].FortType)
}

func TestFort_Gym(t *testing.T) {
	ctx := fortCtx(t)
	replies := runFort(t, ctx, "gym")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Forts.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, "gym", rows[0].FortType)
}

func TestFort_DefaultToEverything(t *testing.T) {
	// No fort type keyword — defaults to everything
	ctx := fortCtx(t)
	replies := runFort(t, ctx, "d0")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Forts.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, "everything", rows[0].FortType)
}

func TestFort_Duplicate(t *testing.T) {
	ctx := fortCtx(t)
	replies1 := runFort(t, ctx, "everything")
	require.NotEmpty(t, replies1)
	assert.Equal(t, "✅", replies1[0].React)

	replies2 := runFort(t, ctx, "everything")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "👌", replies2[0].React, "duplicate should be 👌, reply: %s", replies2[0].Text)
}

func TestFort_Remove(t *testing.T) {
	ctx := fortCtx(t)
	runFort(t, ctx, "everything")
	rows, _ := ctx.Tracking.Forts.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)

	replies := runFort(t, ctx, "remove everything")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Forts.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0)
}

func TestFort_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := fortCtx(t)
	ctx.IsAdmin = false

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runFort(t, ctx, "everything template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "99")
}

func TestFort_ChangeTypes(t *testing.T) {
	ctx := fortCtx(t)
	replies := runFort(t, ctx, "everything location new")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Forts.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Contains(t, rows[0].ChangeTypes, "location")
	assert.Contains(t, rows[0].ChangeTypes, "new")
}
