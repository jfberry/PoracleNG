package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// max_areas on a tracking command's area: list, through !egg (every tracking
// command runs the same overrideLimitReply check before writing).

func eggAreaCtx(t *testing.T) (*bot.CommandContext, *store.MockTrackingStore[db.EggTrackingAPI]) {
	t.Helper()
	ctx, _ := newTestLocationCtx(t) // fences london, paris
	eggs := store.NewMockTrackingStore[db.EggTrackingAPI](store.EggGetUID, store.EggSetUID)
	ctx.Tracking = &store.TrackingStores{Eggs: eggs}
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
	return ctx, eggs
}

func runEggArgs(ctx *bot.CommandContext, args string) []bot.Reply {
	return (&EggCommand{}).Run(ctx, strings.Fields(args))
}

func TestMaxAreas_NewRuleOverLimitRefused(t *testing.T) {
	ctx, eggs := eggAreaCtx(t)
	ctx.Config.Tracking.MaxAreas = 1

	replies := runEggArgs(ctx, "level5 area:london,paris")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "at most 1 areas")
	assert.Empty(t, eggs.AllRows())
}

// A rule stored before the limit was lowered can be resubmitted, but its
// list cannot be copied into a new rule.
func TestMaxAreas_GrandfatheredRuleOnly(t *testing.T) {
	ctx, eggs := eggAreaCtx(t)
	require.NotEqual(t, "🙅", runEggArgs(ctx, "level5 area:london,paris")[0].React)
	require.Len(t, eggs.AllRows(), 1)

	ctx.Config.Tracking.MaxAreas = 1
	again := runEggArgs(ctx, "level5 area:london,paris")
	assert.NotEqual(t, "🙅", again[0].React, "resubmitting the stored rule: %+v", again)

	copied := runEggArgs(ctx, "level3 area:london,paris")
	assert.Equal(t, "🙅", copied[0].React, "a new rule must not reuse the over-limit list: %+v", copied)
	assert.Len(t, eggs.AllRows(), 1)
}

func TestMaxAreas_ChannelAndAdminExempt(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*bot.CommandContext)
	}{
		{"channel target", func(ctx *bot.CommandContext) { ctx.TargetType = bot.TypeDiscordChannel }},
		{"admin sender", func(ctx *bot.CommandContext) { ctx.IsAdmin = true }},
	} {
		ctx, eggs := eggAreaCtx(t)
		ctx.Config.Tracking.MaxAreas = 1
		c.setup(ctx)
		replies := runEggArgs(ctx, "level5 area:london,paris")
		assert.NotEqual(t, "🙅", replies[0].React, "%s: %+v", c.name, replies)
		assert.Len(t, eggs.AllRows(), 1, c.name)
	}
}
