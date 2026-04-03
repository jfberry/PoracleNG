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

func invasionCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}

	invasions := store.NewMockTrackingStore[db.InvasionTrackingAPI](
		store.InvasionGetUID, store.InvasionSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Invasions: invasions,
	}

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Moves:    map[int]*gamedata.Move{},
		Types:    map[int]*gamedata.TypeInfo{},
		Grunts:   map[int]*gamedata.Grunt{},
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

func runInvasion(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &InvasionCommand{}
	args := strings.Fields(input)
	return cmd.Run(ctx, args)
}

func TestInvasion_Everything(t *testing.T) {
	ctx := invasionCtx(t)
	replies := runInvasion(t, ctx, "everything")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Invasions.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, "everything", rows[0].GruntType)
}

func TestInvasion_DefaultToEverything(t *testing.T) {
	// When no type specified and no unrecognized args, defaults to everything
	ctx := invasionCtx(t)
	replies := runInvasion(t, ctx, "clean")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Invasions.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, "everything", rows[0].GruntType)
}

func TestInvasion_Duplicate(t *testing.T) {
	ctx := invasionCtx(t)
	replies1 := runInvasion(t, ctx, "everything")
	require.NotEmpty(t, replies1)
	assert.Equal(t, "✅", replies1[0].React)

	replies2 := runInvasion(t, ctx, "everything")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "👌", replies2[0].React, "duplicate should be 👌, reply: %s", replies2[0].Text)
}

func TestInvasion_Remove(t *testing.T) {
	ctx := invasionCtx(t)
	runInvasion(t, ctx, "everything")
	rows, _ := ctx.Tracking.Invasions.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)

	replies := runInvasion(t, ctx, "remove everything")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Invasions.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0)
}

func TestInvasion_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := invasionCtx(t)
	ctx.IsAdmin = false

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runInvasion(t, ctx, "everything template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "99")
}

func TestInvasion_WithDistance(t *testing.T) {
	ctx := invasionCtx(t)
	ctx.HasLocation = true
	replies := runInvasion(t, ctx, "everything d500")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Invasions.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 500, rows[0].Distance)
}
