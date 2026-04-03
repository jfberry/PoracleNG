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

// trackCtx builds a CommandContext suitable for testing !track.
// It provides mock stores, a config with PVP defaults, and translations.
func trackCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{
		PVP: config.PVPConfig{
			PVPFilterMaxRank:     100,
			PVPFilterGreatMinCP:  1400,
			PVPFilterUltraMinCP:  2350,
			PVPFilterLittleMinCP: 450,
			LevelCaps:            []int{50},
			DisplayMaxRank:       10,
			DisplayGreatMinCP:    1400,
			DisplayUltraMinCP:    2350,
			DisplayLittleMinCP:   450,
		},
	}

	monsters := store.NewMockTrackingStore[db.MonsterTrackingAPI](
		store.MonsterGetUID, store.MonsterSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Monsters: monsters,
	}

	// Minimal GameData with a couple of pokemon for row text generation
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 1, Form: 0}:  {PokemonID: 1, FormID: 0},
			{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
	}

	// Provide a resolver and arg matcher with English translations
	resolver := bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.Resolver = resolver
	ctx.ArgMatcher = bot.NewArgMatcher(ctx.Translations, gd, resolver, []string{"en"})
	ctx.GameData = gd
	ctx.RowText = &rowtext.Generator{
		GD:                  gd,
		Translations:        ctx.Translations,
		DefaultTemplateName: "1",
	}
	ctx.HasArea = true // avoid area warnings

	return ctx
}

func runTrack(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &TrackCommand{}
	args := strings.Fields(input)
	return cmd.Run(ctx, args)
}

// --- PVP min CP floor ---

func TestTrack_PVPMinCPFloor_Great(t *testing.T) {
	ctx := trackCtx(t)
	replies := runTrack(t, ctx, "25 great5")

	require.NotEmpty(t, replies)
	// Should succeed (✅)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	// Check the stored tracking has the config floor applied
	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 1500, rows[0].PVPRankingLeague)
	assert.Equal(t, 5, rows[0].PVPRankingWorst)
	assert.Equal(t, 1400, rows[0].PVPRankingMinCP, "great league min CP should default to config value 1400")
}

func TestTrack_PVPMinCPFloor_Ultra(t *testing.T) {
	ctx := trackCtx(t)
	replies := runTrack(t, ctx, "25 ultra10")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 2500, rows[0].PVPRankingLeague)
	assert.Equal(t, 2350, rows[0].PVPRankingMinCP, "ultra league min CP should default to config value 2350")
}

func TestTrack_PVPMinCPFloor_Little(t *testing.T) {
	ctx := trackCtx(t)
	replies := runTrack(t, ctx, "25 little5")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 500, rows[0].PVPRankingLeague)
	assert.Equal(t, 450, rows[0].PVPRankingMinCP, "little league min CP should default to config value 450")
}

func TestTrack_PVPMinCPFloor_UserHigherWins(t *testing.T) {
	ctx := trackCtx(t)
	// User specifies greatcp1450 which is > config floor 1400
	replies := runTrack(t, ctx, "25 great5 greatcp1450")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 1450, rows[0].PVPRankingMinCP, "user's explicit CP should win when higher than floor")
}

func TestTrack_PVPMinCPFloor_UserLowerClamped(t *testing.T) {
	ctx := trackCtx(t)
	// User specifies greatcp500 which is < config floor 1400
	replies := runTrack(t, ctx, "25 great5 greatcp500")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 1400, rows[0].PVPRankingMinCP, "user's low CP should be clamped to config floor")
}

// --- PVP max rank clamping ---

func TestTrack_PVPMaxRankClamped(t *testing.T) {
	ctx := trackCtx(t)
	ctx.Config.PVP.PVPFilterMaxRank = 50

	replies := runTrack(t, ctx, "25 great100")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 50, rows[0].PVPRankingWorst, "worst rank should be clamped to pvp_filter_max_rank")
}

// --- Multiple PVP leagues ---

func TestTrack_MultipleLeagues(t *testing.T) {
	ctx := trackCtx(t)
	replies := runTrack(t, ctx, "25 great5 ultra10")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 2, "should create one tracking row per league")

	// Verify both leagues are present
	leagues := map[int]db.MonsterTrackingAPI{}
	for _, r := range rows {
		leagues[r.PVPRankingLeague] = r
	}

	great, ok := leagues[1500]
	require.True(t, ok, "should have great league entry")
	assert.Equal(t, 5, great.PVPRankingWorst)
	assert.Equal(t, 1400, great.PVPRankingMinCP)

	ultra, ok := leagues[2500]
	require.True(t, ok, "should have ultra league entry")
	assert.Equal(t, 10, ultra.PVPRankingWorst)
	assert.Equal(t, 2350, ultra.PVPRankingMinCP)
}

func TestTrack_MultipleLeagues_MultipleMonsters(t *testing.T) {
	ctx := trackCtx(t)
	replies := runTrack(t, ctx, "25 1 great5 ultra10")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 4, "2 pokemon × 2 leagues = 4 tracking rows")
}

// --- Level cap validation ---

func TestTrack_InvalidCap(t *testing.T) {
	ctx := trackCtx(t)
	ctx.Config.PVP.LevelCaps = []int{50}

	replies := runTrack(t, ctx, "25 great5 cap40")

	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "not supported")
}

func TestTrack_ValidCap(t *testing.T) {
	ctx := trackCtx(t)
	ctx.Config.PVP.LevelCaps = []int{40, 50, 51}

	replies := runTrack(t, ctx, "25 great5 cap40")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 40, rows[0].PVPRankingCap)
}

func TestTrack_ZeroCap(t *testing.T) {
	ctx := trackCtx(t)
	// cap0 means "any cap" — should always be allowed
	replies := runTrack(t, ctx, "25 great5")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 0, rows[0].PVPRankingCap)
}

// --- Template validation ---

func TestTrack_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := trackCtx(t)
	ctx.IsAdmin = false

	// Create a TemplateStore with a temp dir containing an empty dts.json
	dir := t.TempDir()
	require.NoError(t, writeFile(dir, "dts.json", "[]"))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runTrack(t, ctx, "25 template:99")

	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "99")
}

func TestTrack_InvalidTemplate_Admin(t *testing.T) {
	ctx := trackCtx(t)
	ctx.IsAdmin = true

	dir := t.TempDir()
	require.NoError(t, writeFile(dir, "dts.json", "[]"))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runTrack(t, ctx, "25 template:99")

	require.NotEmpty(t, replies)
	// Admin: should succeed but with warning
	assert.Equal(t, "✅", replies[0].React, "admin should not be blocked, reply: %s", replies[0].Text)
	assert.Contains(t, replies[0].Text, "99")
}

func writeFile(dir, name, content string) error {
	return os.WriteFile(dir+"/"+name, []byte(content), 0644)
}

func TestTrack_NoTemplate_NoDTS(t *testing.T) {
	ctx := trackCtx(t)
	ctx.DTS = nil // DTS not loaded — skip validation

	replies := runTrack(t, ctx, "25 great5")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)
}

// --- Basic tracking ---

func TestTrack_BasicPokemon(t *testing.T) {
	ctx := trackCtx(t)
	replies := runTrack(t, ctx, "25")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 25, rows[0].PokemonID) // Pikachu
	assert.Equal(t, 0, rows[0].PVPRankingLeague, "no PVP when not specified")
}

func TestTrack_WithIVFilter(t *testing.T) {
	ctx := trackCtx(t)
	replies := runTrack(t, ctx, "25 iv90")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 90, rows[0].MinIV)
	assert.Equal(t, 100, rows[0].MaxIV)
}

func TestTrack_WithDistance(t *testing.T) {
	ctx := trackCtx(t)
	ctx.HasLocation = true
	replies := runTrack(t, ctx, "25 d500")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 500, rows[0].Distance)
}

func TestTrack_NoPokemon(t *testing.T) {
	ctx := trackCtx(t)
	replies := runTrack(t, ctx, "iv100")

	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
}

func TestTrack_Duplicate(t *testing.T) {
	ctx := trackCtx(t)
	// Track 25 twice — second should be "already present"
	replies1 := runTrack(t, ctx, "25")
	require.NotEmpty(t, replies1)
	assert.Equal(t, "✅", replies1[0].React, "first track: %s", replies1[0].Text)

	replies2 := runTrack(t, ctx, "25")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "👌", replies2[0].React, "duplicate should be 👌, reply: %s", replies2[0].Text)
}

// --- PVP range syntax ---

func TestTrack_PVPRange(t *testing.T) {
	ctx := trackCtx(t)
	replies := runTrack(t, ctx, "25 great1-10")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].PVPRankingBest)
	assert.Equal(t, 10, rows[0].PVPRankingWorst)
}

// --- Warnings ---

func TestTrack_WarningNoArea(t *testing.T) {
	ctx := trackCtx(t)
	ctx.HasArea = false
	ctx.HasLocation = false

	replies := runTrack(t, ctx, "25")

	require.NotEmpty(t, replies)
	assert.Contains(t, replies[0].Text, "area")
}

func TestTrack_WarningNoLocation(t *testing.T) {
	ctx := trackCtx(t)
	ctx.HasLocation = false

	replies := runTrack(t, ctx, "25 d500")

	require.NotEmpty(t, replies)
	assert.Contains(t, replies[0].Text, "location")
}
