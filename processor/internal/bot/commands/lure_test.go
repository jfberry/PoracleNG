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

func lureCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}

	lures := store.NewMockTrackingStore[db.LureTrackingAPI](
		store.LureGetUID, store.LureSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Lures: lures,
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

func runLure(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &LureCommand{}
	args := strings.Fields(input)
	return cmd.Run(ctx, args)
}

func TestLure_Everything(t *testing.T) {
	ctx := lureCtx(t)
	replies := runLure(t, ctx, "everything")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 0, rows[0].LureID, "everything = lure ID 0")
}

func TestLure_Glacial(t *testing.T) {
	ctx := lureCtx(t)
	replies := runLure(t, ctx, "glacial")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.NotEqual(t, 0, rows[0].LureID, "glacial should have a specific lure ID")
}

// "normal" is a real lure type (Lure Module, 501) — it must create a
// specific-type rule, not collide with the 0 "any lure" sentinel and
// error out with "No lure type specified".
func TestLure_Normal(t *testing.T) {
	ctx := lureCtx(t)
	replies := runLure(t, ctx, "normal")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 501, rows[0].LureID)
}

// Multiple lure types in one command create one rule per type. Before
// LureTypes collected all matches, only the first name was consumed and
// the rest were rejected as "Unrecognized".
func TestLure_MultipleTypes(t *testing.T) {
	ctx := lureCtx(t)
	replies := runLure(t, ctx, "normal glacial mossy magnetic sparkly clean")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 5)
	gotIDs := make(map[int]bool)
	for _, r := range rows {
		gotIDs[r.LureID] = true
		assert.Equal(t, 1, r.Clean&1, "clean bit should be set on lure %d", r.LureID)
	}
	for _, want := range []int{501, 502, 503, 504, 506} {
		assert.True(t, gotIDs[want], "missing rule for lure %d (got %v)", want, gotIDs)
	}
}

// Repeated lure names collapse to a single rule rather than duplicate
// inserts for the same type.
func TestLure_DuplicateTypesDeduped(t *testing.T) {
	ctx := lureCtx(t)
	replies := runLure(t, ctx, "glacial glacial")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 502, rows[0].LureID)
}

// Removing one named type only deletes that type's rule.
func TestLure_RemoveOneOfMany(t *testing.T) {
	ctx := lureCtx(t)
	runLure(t, ctx, "normal glacial")
	rows, _ := ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 2)

	replies := runLure(t, ctx, "remove normal")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 502, rows[0].LureID, "glacial rule should survive")
}

// `!lure <type> edit` must set Clean bit 2 (db.IsEdit). Without
// arg.edit in lureParams the matcher would warn "unrecognized" and
// the edit flag would never propagate, defeating the EditKey wiring
// in cmd/processor/lure.go.
func TestLure_EditFlag(t *testing.T) {
	ctx := lureCtx(t)
	replies := runLure(t, ctx, "glacial edit")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	if rows[0].Clean&2 == 0 {
		t.Errorf("Clean bit 2 (edit) not set: got %d", rows[0].Clean)
	}
}

func TestLure_Duplicate(t *testing.T) {
	ctx := lureCtx(t)
	replies1 := runLure(t, ctx, "everything")
	require.NotEmpty(t, replies1)
	assert.Equal(t, "✅", replies1[0].React)

	replies2 := runLure(t, ctx, "everything")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "👌", replies2[0].React, "duplicate should be 👌, reply: %s", replies2[0].Text)
}

func TestLure_Remove(t *testing.T) {
	ctx := lureCtx(t)
	runLure(t, ctx, "everything")
	rows, _ := ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)

	replies := runLure(t, ctx, "remove everything")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0)
}

func TestLure_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := lureCtx(t)
	ctx.IsAdmin = false

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runLure(t, ctx, "everything template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "99")
}

func TestLure_NoType(t *testing.T) {
	ctx := lureCtx(t)
	replies := runLure(t, ctx, "clean")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
}

func TestLure_AcceptsAreaOverride(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)

	lures := store.NewMockTrackingStore[db.LureTrackingAPI](
		store.LureGetUID, store.LureSetUID,
	)
	ctx.Tracking = &store.TrackingStores{Lures: lures}

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

	cmd := &LureCommand{}
	replies := cmd.Run(ctx, strings.Fields("everything area:london"))
	require.NotEmpty(t, replies)
	assert.NotEqual(t, "🙅", replies[0].React, "rejected: %+v", replies)

	rules, _ := ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
	require.Len(t, rules, 1)
	assert.Len(t, rules[0].OverrideAreas, 1, "override not stored: %+v", rules[0])
}
