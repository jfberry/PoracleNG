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

func questCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}

	quests := store.NewMockTrackingStore[db.QuestTrackingAPI](
		store.QuestGetUID, store.QuestSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Quests: quests,
	}

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
		Items: map[int]*gamedata.Item{},
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

func runQuest(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &QuestCommand{}
	args := strings.Fields(input)
	return cmd.Run(ctx, args)
}

func TestQuest_Pokemon(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "25")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 7, rows[0].RewardType, "pokemon quest reward type")
	assert.Equal(t, 25, rows[0].Reward, "pokemon ID as reward")
}

func TestQuest_Stardust(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "stardust:1000")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 3, rows[0].RewardType, "stardust reward type")
	assert.Equal(t, 1000, rows[0].Reward, "stardust amount")
}

func TestQuest_BareStardust(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "stardust")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 3, rows[0].RewardType, "stardust reward type")
	assert.Equal(t, 0, rows[0].Reward, "bare stardust = any amount")
}

func TestQuest_Duplicate(t *testing.T) {
	ctx := questCtx(t)
	replies1 := runQuest(t, ctx, "25")
	require.NotEmpty(t, replies1)
	assert.Equal(t, "✅", replies1[0].React)

	replies2 := runQuest(t, ctx, "25")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "👌", replies2[0].React, "duplicate should be 👌, reply: %s", replies2[0].Text)
}

func TestQuest_Remove(t *testing.T) {
	ctx := questCtx(t)
	runQuest(t, ctx, "25")
	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)

	replies := runQuest(t, ctx, "remove 25")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0)
}

func TestQuest_NoTarget(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "clean")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
}

func TestQuest_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := questCtx(t)
	ctx.IsAdmin = false

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runQuest(t, ctx, "25 template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "99")
}

func TestQuest_Everything(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "everything")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	// everything creates: pokemon(7), stardust(3), energy(12), candy(4), item(2)
	assert.Len(t, rows, 5, "everything should create 5 reward types")
}
