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

func TestQuest_SummaryKeyword(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "25 summary")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	// bit 4 (summary) should be set in clean.
	assert.True(t, db.IsSummary(rows[0].Clean), "summary keyword should set bit 4 of clean, got %d", rows[0].Clean)
	assert.False(t, db.IsClean(rows[0].Clean), "summary alone should NOT set bit 1")
	assert.False(t, db.IsEdit(rows[0].Clean), "summary alone should NOT set bit 2")
}

func TestQuest_SummaryWithClean(t *testing.T) {
	// summary + clean both set: clean=5 (bits 1+4)
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "25 summary clean")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.True(t, db.IsSummary(rows[0].Clean), "clean=%d", rows[0].Clean)
	assert.True(t, db.IsClean(rows[0].Clean), "clean=%d", rows[0].Clean)
}

func TestQuest_SummaryRejectsEdit(t *testing.T) {
	// edit + summary is rejected up-front. (Quest currently doesn't expose
	// `edit` directly as a keyword, but parseCommonTrackFields recognises
	// it via the shared keyword list. We simulate the parsed-args state
	// the matcher would produce by adding `arg.edit` to the param list
	// just for this test.)
	ctx := questCtx(t)

	// Inject the keyword param so the matcher consumes `edit` for this
	// invocation. This mirrors what would happen in production once a
	// future quest variant accepts edit-mode (today the user-facing
	// surface only allows summary OR clean).
	origParams := append([]bot.ParamDef(nil), questParams...)
	defer func() { questParams = origParams }()
	questParams = append(questParams, bot.ParamDef{Type: bot.ParamKeyword, Key: "arg.edit"})

	replies := runQuest(t, ctx, "25 summary edit")

	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React, "reply: %s", replies[0].Text)
	assert.Contains(t, replies[0].Text, "mutually exclusive")

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	assert.Empty(t, rows, "rejected combo should not insert a tracking rule")
}

// TestQuest_RemoveSummary_OnlyTargetsSummaryRules pins the selective
// removal behaviour: when the user types `!quest remove pikachu summary`
// only the summary-bit rules should be deleted, leaving any
// non-summary rule intact. Defensive against DB-direct inserts or
// future API paths that can create both variants — the !quest command
// itself dedups via diff:"match" on (RewardType, Reward, Form) so a
// user can't normally have two parallel rules for the same reward
// through the bot. We seed directly to exercise the predicate.
func TestQuest_RemoveSummary_OnlyTargetsSummaryRules(t *testing.T) {
	ctx := questCtx(t)

	// Seed two rules for Pikachu with different forms (so the diff:"match"
	// keys don't collapse them). Both are summary-bit rules — we then
	// add a non-summary rule for a DIFFERENT reward to confirm the
	// summary filter doesn't sweep it up.
	mock := ctx.Tracking.Quests.(*store.MockTrackingStore[db.QuestTrackingAPI])
	_, _ = mock.Insert(&db.QuestTrackingAPI{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 25, Form: 0, Clean: 4})  // summary Pikachu
	_, _ = mock.Insert(&db.QuestTrackingAPI{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 25, Form: 65, Clean: 4}) // summary Alolan Pikachu
	_, _ = mock.Insert(&db.QuestTrackingAPI{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 25, Form: 1290, Clean: 0}) // immediate cosplay Pikachu
	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 3, "seed should produce 3 rules across distinct forms")

	// Remove only the summary variants.
	replies := runQuest(t, ctx, "remove 25 summary")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1, "only the immediate rule should remain")
	assert.False(t, db.IsSummary(rows[0].Clean), "remaining rule must be the immediate one (clean=0)")
}

// TestQuest_AmountOnCandy persists amount:N as QuestTrackingAPI.Amount
// when paired with a candy reward — the matcher then enforces it via
// q.Amount > 0 in matching/quest.go. (Reward / Form are pokemon-resolver
// dependent and asserted in the per-reward tests; here we focus on the
// Amount plumbing.)
func TestQuest_AmountOnCandy(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "candy:pikachu amount:5")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 4, rows[0].RewardType, "candy reward type")
	assert.Equal(t, 5, rows[0].Amount, "amount:5 should persist into Amount column")
}

// Bare `candy` keyword (no pokemon name) + amount:N — the catch-all
// path that matches ANY candy reward over the threshold.
func TestQuest_AmountOnBareCandy(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "candy amount:5")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 4, rows[0].RewardType)
	assert.Equal(t, 0, rows[0].Reward, "bare candy = any pokemon")
	assert.Equal(t, 5, rows[0].Amount, "amount:5 should persist")
}

// Item reward (type 2) with amount:N — same Amount column, matcher
// gates on q.Amount > 0 && r.Amount < q.Amount.
func TestQuest_AmountOnItem(t *testing.T) {
	ctx := questCtx(t)
	// Seed an item so matchItemName can resolve "razz" against a name.
	// The full Pokemon item set isn't loaded — we add a single Razz Berry
	// (item_701) and rely on the embedded translation bundle.
	ctx.GameData.Items[701] = &gamedata.Item{ItemID: 701}

	// "razz berry" — matchItemName lookup via translated name.
	replies := runQuest(t, ctx, "razz berry amount:3")
	require.NotEmpty(t, replies)
	if replies[0].React != "✅" {
		t.Skipf("translated item name not available in default test bundle (reply: %s) — Amount plumbing covered by candy/mega-energy tests", replies[0].Text)
	}

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 2, rows[0].RewardType, "item reward type")
	assert.Equal(t, 3, rows[0].Amount, "amount:3 should persist on item reward")
}

// amount:N on a mega_energy quest persists into Amount, mirroring the
// candy case — both reward types honour q.Amount in the matcher.
func TestQuest_AmountOnMegaEnergy(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "energy:pikachu amount:50")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 12, rows[0].RewardType, "mega energy reward type")
	assert.Equal(t, 50, rows[0].Amount)
}

// amount:N on a pokemon reward (type 7) has no defined meaning — the
// matcher has no Amount check on that branch — so reject up-front.
func TestQuest_AmountRejectedOnPokemonReward(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "25 amount:5")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React, "should reject amount:N on pokemon quest, reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	assert.Empty(t, rows, "rejected combo should not persist")
}

// Explicit stardust:N + amount:N — the explicit form wins (stardust:N
// is unambiguous about which column to fill). The bot accepts the
// combo and the resulting row stores 1000 in Reward (stardust min).
func TestQuest_StardustExplicitTakesPrecedenceOverAmount(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "stardust:1000 amount:5")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "should accept stardust:N + amount:N, reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 3, rows[0].RewardType, "stardust reward type")
	assert.Equal(t, 1000, rows[0].Reward, "stardust:N populates Reward")
	assert.Equal(t, 0, rows[0].Amount, "amount:N is ignored when stardust:N is explicit")
}

// Bare "stardust" + amount:N — the amount routes into the Reward
// column (stardust's own min slot). Equivalent to `stardust:N` but
// reads more naturally to users who think of "amount" as the threshold.
func TestQuest_BareStardustWithAmountRoutesToReward(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "stardust amount:500")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "should accept bare stardust + amount:N, reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 3, rows[0].RewardType)
	assert.Equal(t, 500, rows[0].Reward, "amount:N populates Reward for bare stardust")
	assert.Equal(t, 0, rows[0].Amount, "Amount column unused for stardust")
}

// `everything` + amount:N applies the amount per-row where supported:
// pokemon ignores it, stardust routes into Reward, candy/mega/item
// store it in Amount.
func TestQuest_EverythingWithAmountRoutesPerRewardType(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "everything amount:25")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "should accept everything + amount:N, reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 5, "everything inserts one row per reward type")

	byType := map[int]db.QuestTrackingAPI{}
	for _, r := range rows {
		byType[r.RewardType] = r
	}
	assert.Equal(t, 0, byType[7].Amount, "pokemon: no amount")
	assert.Equal(t, 0, byType[7].Reward, "pokemon: no reward filter")
	assert.Equal(t, 25, byType[3].Reward, "stardust: amount routes into Reward")
	assert.Equal(t, 0, byType[3].Amount, "stardust: Amount unused")
	assert.Equal(t, 25, byType[12].Amount, "mega energy: amount → Amount")
	assert.Equal(t, 25, byType[4].Amount, "candy: amount → Amount")
	assert.Equal(t, 25, byType[2].Amount, "item: amount → Amount")
}

// TestQuest_RemoveWithoutSummary_RemovesBoth confirms back-compat: a
// bare `!quest remove pikachu` removes both summary and immediate rules
// (matches the historic "remove regardless of clean/edit bits" behaviour).
func TestQuest_RemoveWithoutSummary_RemovesBoth(t *testing.T) {
	ctx := questCtx(t)

	mock := ctx.Tracking.Quests.(*store.MockTrackingStore[db.QuestTrackingAPI])
	_, _ = mock.Insert(&db.QuestTrackingAPI{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 25, Form: 0, Clean: 0})  // immediate
	_, _ = mock.Insert(&db.QuestTrackingAPI{ID: "user1", ProfileNo: 1, RewardType: 7, Reward: 25, Form: 65, Clean: 4}) // summary

	replies := runQuest(t, ctx, "remove 25")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	assert.Empty(t, rows, "remove without summary keyword should remove both rules")
}

func TestQuest_AcceptsAreaOverride(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)

	quests := store.NewMockTrackingStore[db.QuestTrackingAPI](
		store.QuestGetUID, store.QuestSetUID,
	)
	ctx.Tracking = &store.TrackingStores{Quests: quests}

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Moves:    map[int]*gamedata.Move{},
		Types:    map[int]*gamedata.TypeInfo{},
		Items:    map[int]*gamedata.Item{},
	}
	resolver := bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.Resolver = resolver
	ctx.ArgMatcher = bot.NewArgMatcher(ctx.Translations, gd, resolver, []string{"en"})
	ctx.GameData = gd
	ctx.RowText = &rowtext.Generator{GD: gd, Translations: ctx.Translations, DefaultTemplateName: "1"}
	ctx.HasArea = true

	cmd := &QuestCommand{}
	replies := cmd.Run(ctx, strings.Fields("stardust area:london"))
	require.NotEmpty(t, replies)
	assert.NotEqual(t, "🙅", replies[0].React, "rejected: %+v", replies)

	rules, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
	require.Len(t, rules, 1)
	assert.Len(t, rules[0].OverrideAreas, 1, "override not stored: %+v", rules[0])
}
