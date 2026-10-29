package commands

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// Regression tests for #347: remove paths and argument handling.

// questCtxWithItems is questCtx plus two named items (razz berry 701,
// pinap berry 705).
func questCtxWithItems(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx := questCtx(t)
	ctx.GameData.Items[701] = &gamedata.Item{ItemID: 701}
	ctx.GameData.Items[705] = &gamedata.Item{ItemID: 705}
	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"item_701": "Razz Berry",
		"item_705": "Pinap Berry",
	}))
	return ctx
}

// 1. A typo on remove is reported and removes nothing.
func TestQuest_RemoveTypo_RemovesNothing(t *testing.T) {
	ctx := questCtx(t)
	runQuest(t, ctx, "stardust 25")
	require.Len(t, questKinds(t, ctx), 2)

	replies := runQuest(t, ctx, "remove pikchu")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "pikchu")
	assert.Len(t, questKinds(t, ctx), 2, "a typo must not remove any rule")
}

// A typo next to a real reward is still refused rather than half-applied.
func TestQuest_RemoveRewardPlusTypo_RemovesNothing(t *testing.T) {
	ctx := questCtx(t)
	runQuest(t, ctx, "stardust 25")

	replies := runQuest(t, ctx, "remove stardust pikchu")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Len(t, questKinds(t, ctx), 2)
}

// 5. Every item named becomes a rule. The parser has already turned
// `razz_berry` into the single token "razz berry", so pass that form.
func TestQuest_SeveralItems(t *testing.T) {
	ctx := questCtxWithItems(t)
	replies := (&QuestCommand{}).Run(ctx, []string{"razz berry", "pinap berry"})
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)
	assert.ElementsMatch(t, []string{"2:701", "2:705"}, questKinds(t, ctx))
}

// Multi-word item names still match when typed as separate tokens.
func TestQuest_ItemAcrossTokens(t *testing.T) {
	ctx := questCtxWithItems(t)
	replies := runQuest(t, ctx, "pinap berry")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)
	assert.ElementsMatch(t, []string{"2:705"}, questKinds(t, ctx))
}

// 5. A typo beside a valid item is reported, not silently dropped.
func TestQuest_ItemPlusTypo_Rejected(t *testing.T) {
	ctx := questCtxWithItems(t)
	replies := (&QuestCommand{}).Run(ctx, []string{"razz berry", "xyz"})
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "xyz")
	assert.Empty(t, questKinds(t, ctx))
}

func TestQuest_RemoveSeveralItems(t *testing.T) {
	ctx := questCtxWithItems(t)
	(&QuestCommand{}).Run(ctx, []string{"razz berry", "pinap berry"})
	runQuest(t, ctx, "stardust")

	replies := (&QuestCommand{}).Run(ctx, []string{"remove", "razz berry", "pinap berry"})
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)
	assert.ElementsMatch(t, []string{"3:0"}, questKinds(t, ctx))
}

// 2. remove everything also removes "any team" (team 4) rules.
func TestGym_RemoveEverything_IncludesAnyTeam(t *testing.T) {
	ctx := gymCtx(t)
	runGym(t, ctx, "clean")  // team 4 (any)
	runGym(t, ctx, "mystic") // team 1
	rows, _ := ctx.Tracking.Gyms.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 2)

	replies := runGym(t, ctx, "remove everything")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)
	rows, _ = ctx.Tracking.Gyms.SelectByIDProfile("user1", 1)
	assert.Empty(t, rows)
}

// 3. remove everything removes rules at levels game data doesn't list.
func TestEgg_RemoveEverything_UnlistedLevel(t *testing.T) {
	ctx := eggCtx(t)
	_, err := ctx.Tracking.Eggs.Insert(&db.EggTrackingAPI{ID: "user1", ProfileNo: 1, Level: 42})
	require.NoError(t, err)
	runEgg(t, ctx, "level5")

	replies := runEgg(t, ctx, "remove everything")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)
	rows, _ := ctx.Tracking.Eggs.SelectByIDProfile("user1", 1)
	assert.Empty(t, rows)
}

// 4. !track remove id:N hands off to !untrack instead of being refused.
func TestTrack_RemoveByUID(t *testing.T) {
	ctx := trackCtx(t)
	runTrack(t, ctx, "25")
	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)

	replies := runTrack(t, ctx, "remove id:"+strconv.FormatInt(rows[0].UID, 10))
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)
	rows, _ = ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	assert.Empty(t, rows)
}

// 5. Each fort type named becomes its own rule.
func TestFort_SeveralTypes(t *testing.T) {
	ctx := fortCtx(t)
	replies := runFort(t, ctx, "pokestop gym")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)
	rows, _ := ctx.Tracking.Forts.SelectByIDProfile("user1", 1)
	var types []string
	for _, r := range rows {
		types = append(types, r.FortType)
	}
	assert.ElementsMatch(t, []string{"pokestop", "gym"}, types)

	runFort(t, ctx, "station")
	runFort(t, ctx, "remove pokestop gym")
	rows, _ = ctx.Tracking.Forts.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, "station", rows[0].FortType)
}

// 6. !info <pokemon> form:<x> shows the named form.
func TestInfo_FormFilter_ShowsForm(t *testing.T) {
	ctx := infoFormCostumeCtx(t)
	replies := (&InfoCommand{}).Run(ctx, []string{"pikachu", "form:winter 2023"})
	require.NotEmpty(t, replies)
	assert.NotEqual(t, "🙅", replies[0].React, "reply: %s", replies[0].Text)
	assert.Contains(t, replies[0].Text, "Winter 2023")
}

func TestInfo_FormFilter_UnknownForm(t *testing.T) {
	ctx := infoFormCostumeCtx(t)
	replies := (&InfoCommand{}).Run(ctx, []string{"pikachu", "form:nope"})
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.True(t, strings.Contains(replies[0].Text, "nope"), "reply: %s", replies[0].Text)
}
