package commands

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// removeCase drives the shared remove rules (remove.go) through one
// tracking command. seed stores one rule and returns its uid; selector is
// an argument that selects that rule; extra is an argument the command
// accepts on add but its remove doesn't use.
type removeCase struct {
	name     string
	ctx      func(t *testing.T) *bot.CommandContext
	cmd      bot.Command
	seed     func(t *testing.T, ctx *bot.CommandContext) int64
	count    func(ctx *bot.CommandContext) int
	selector string
	extra    string
	// noRemoveKeyword: the command is itself a remove (!untrack).
	noRemoveKeyword bool
}

func removeCases() []removeCase {
	return []removeCase{
		{
			name: "raid", ctx: raidCtx, cmd: &RaidCommand{}, selector: "25", extra: "mystic",
			seed: func(t *testing.T, ctx *bot.CommandContext) int64 {
				uid, err := ctx.Tracking.Raids.Insert(&db.RaidTrackingAPI{ID: "user1", ProfileNo: 1, PokemonID: 25, Level: 9000})
				require.NoError(t, err)
				return uid
			},
			count: func(ctx *bot.CommandContext) int {
				rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
				return len(rows)
			},
		},
		{
			name: "egg", ctx: eggCtx, cmd: &EggCommand{}, selector: "level5", extra: "ex",
			seed: func(t *testing.T, ctx *bot.CommandContext) int64 {
				uid, err := ctx.Tracking.Eggs.Insert(&db.EggTrackingAPI{ID: "user1", ProfileNo: 1, Level: 5})
				require.NoError(t, err)
				return uid
			},
			count: func(ctx *bot.CommandContext) int {
				rows, _ := ctx.Tracking.Eggs.SelectByIDProfile("user1", 1)
				return len(rows)
			},
		},
		{
			name: "gym", ctx: gymCtx, cmd: &GymCommand{}, selector: "mystic", extra: "clean",
			seed: func(t *testing.T, ctx *bot.CommandContext) int64 {
				uid, err := ctx.Tracking.Gyms.Insert(&db.GymTrackingAPI{ID: "user1", ProfileNo: 1, Team: 1})
				require.NoError(t, err)
				return uid
			},
			count: func(ctx *bot.CommandContext) int {
				rows, _ := ctx.Tracking.Gyms.SelectByIDProfile("user1", 1)
				return len(rows)
			},
		},
		{
			name: "fort", ctx: fortCtx, cmd: &FortCommand{}, selector: "pokestop", extra: "new",
			seed: func(t *testing.T, ctx *bot.CommandContext) int64 {
				uid, err := ctx.Tracking.Forts.Insert(&db.FortTrackingAPI{ID: "user1", ProfileNo: 1, FortType: "pokestop", ChangeTypes: "[]"})
				require.NoError(t, err)
				return uid
			},
			count: func(ctx *bot.CommandContext) int {
				rows, _ := ctx.Tracking.Forts.SelectByIDProfile("user1", 1)
				return len(rows)
			},
		},
		{
			name: "lure", ctx: lureCtx, cmd: &LureCommand{}, selector: "glacial", extra: "clean",
			seed: func(t *testing.T, ctx *bot.CommandContext) int64 {
				uid, err := ctx.Tracking.Lures.Insert(&db.LureTrackingAPI{ID: "user1", ProfileNo: 1, LureID: 502})
				require.NoError(t, err)
				return uid
			},
			count: func(ctx *bot.CommandContext) int {
				rows, _ := ctx.Tracking.Lures.SelectByIDProfile("user1", 1)
				return len(rows)
			},
		},
		{
			name: "nest", ctx: nestCtx, cmd: &NestCommand{}, selector: "25", extra: "clean",
			seed: func(t *testing.T, ctx *bot.CommandContext) int64 {
				uid, err := ctx.Tracking.Nests.Insert(&db.NestTrackingAPI{ID: "user1", ProfileNo: 1, PokemonID: 25})
				require.NoError(t, err)
				return uid
			},
			count: func(ctx *bot.CommandContext) int {
				rows, _ := ctx.Tracking.Nests.SelectByIDProfile("user1", 1)
				return len(rows)
			},
		},
		{
			name: "maxbattle", ctx: maxbattleCtx, cmd: &MaxbattleCommand{}, selector: "25", extra: "gmax",
			seed: func(t *testing.T, ctx *bot.CommandContext) int64 {
				uid, err := ctx.Tracking.Maxbattles.Insert(&db.MaxbattleTrackingAPI{ID: "user1", ProfileNo: 1, PokemonID: 25})
				require.NoError(t, err)
				return uid
			},
			count: func(ctx *bot.CommandContext) int {
				rows, _ := ctx.Tracking.Maxbattles.SelectByIDProfile("user1", 1)
				return len(rows)
			},
		},
		{
			name: "quest", ctx: questCtx, cmd: &QuestCommand{}, selector: "stardust", extra: "shiny",
			seed: func(t *testing.T, ctx *bot.CommandContext) int64 {
				uid, err := ctx.Tracking.Quests.Insert(&db.QuestTrackingAPI{ID: "user1", ProfileNo: 1, RewardType: 3})
				require.NoError(t, err)
				return uid
			},
			count: func(ctx *bot.CommandContext) int {
				rows, _ := ctx.Tracking.Quests.SelectByIDProfile("user1", 1)
				return len(rows)
			},
		},
		{
			name: "invasion", cmd: &InvasionCommand{}, selector: "giovanni", extra: "clean",
			ctx: func(t *testing.T) *bot.CommandContext { ctx, _ := invasionTestCtx(t); return ctx },
			seed: func(t *testing.T, ctx *bot.CommandContext) int64 {
				uid, err := ctx.Tracking.Invasions.Insert(&db.InvasionTrackingAPI{ID: "user1", ProfileNo: 1, GruntType: "giovanni"})
				require.NoError(t, err)
				return uid
			},
			count: func(ctx *bot.CommandContext) int {
				rows, _ := ctx.Tracking.Invasions.SelectByIDProfile("user1", 1)
				return len(rows)
			},
		},
		{
			name: "untrack", cmd: &UntrackCommand{}, selector: "25", noRemoveKeyword: true,
			ctx: func(t *testing.T) *bot.CommandContext { ctx, _ := untrackCtx(t); return ctx },
			seed: func(t *testing.T, ctx *bot.CommandContext) int64 {
				uid, err := ctx.Tracking.Monsters.Insert(&db.MonsterTrackingAPI{ID: "user1", ProfileNo: 1, PokemonID: 25})
				require.NoError(t, err)
				return uid
			},
			count: func(ctx *bot.CommandContext) int {
				rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
				return len(rows)
			},
		},
	}
}

func (c removeCase) run(ctx *bot.CommandContext, args ...string) []bot.Reply {
	if !c.noRemoveKeyword {
		args = append([]string{"remove"}, args...)
	}
	return c.cmd.Run(ctx, args)
}

// Every tracking command's remove follows the same rules.
func TestRemoveRules_AllCommands(t *testing.T) {
	for _, c := range removeCases() {
		t.Run(c.name, func(t *testing.T) {
			refused := func(t *testing.T, ctx *bot.CommandContext, replies []bot.Reply) {
				t.Helper()
				require.NotEmpty(t, replies)
				assert.Equal(t, "🙅", replies[0].React, "reply: %s", replies[0].Text)
				assert.Equal(t, 1, c.count(ctx), "a refused remove must not remove anything")
			}

			t.Run("bare remove is refused", func(t *testing.T) {
				ctx := c.ctx(t)
				c.seed(t, ctx)
				refused(t, ctx, c.run(ctx))
			})

			t.Run("unrecognised word is refused", func(t *testing.T) {
				ctx := c.ctx(t)
				c.seed(t, ctx)
				refused(t, ctx, c.run(ctx, c.selector, "zzqx"))
			})

			if c.extra != "" {
				t.Run("argument remove doesn't use is refused", func(t *testing.T) {
					ctx := c.ctx(t)
					c.seed(t, ctx)
					replies := c.run(ctx, c.selector, c.extra)
					refused(t, ctx, replies)
					assert.Contains(t, replies[0].Text, c.extra)
				})
			}

			t.Run("id:N stands alone", func(t *testing.T) {
				ctx := c.ctx(t)
				uid := c.seed(t, ctx)
				refused(t, ctx, c.run(ctx, "id:"+strconv.FormatInt(uid, 10), c.selector))

				replies := c.run(ctx, "id:"+strconv.FormatInt(uid, 10))
				require.NotEmpty(t, replies)
				assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)
				assert.Equal(t, 0, c.count(ctx))
			})

			t.Run("selector removes the rule", func(t *testing.T) {
				ctx := c.ctx(t)
				c.seed(t, ctx)
				replies := c.run(ctx, c.selector)
				require.NotEmpty(t, replies)
				assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)
				assert.Equal(t, 0, c.count(ctx))
			})

			t.Run("everything removes every rule", func(t *testing.T) {
				ctx := c.ctx(t)
				c.seed(t, ctx)
				replies := c.run(ctx, "everything")
				require.NotEmpty(t, replies)
				assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)
				assert.Equal(t, 0, c.count(ctx))
			})
		})
	}
}

// everything removes rules whose values no keyword names, e.g. a quest
// reward type the command can't express (set through the API).
func TestQuest_RemoveEverything_UnlistedRewardType(t *testing.T) {
	ctx := questCtx(t)
	_, err := ctx.Tracking.Quests.Insert(&db.QuestTrackingAPI{ID: "user1", ProfileNo: 1, RewardType: 99})
	require.NoError(t, err)
	runQuest(t, ctx, "remove everything")
	assert.Empty(t, questKinds(t, ctx))
}

// A filter alone names nothing: `!nest remove gen1` is refused, while
// `everything gen1` removes only the gen-1 rules.
func TestNest_RemoveFilterNeedsSelector(t *testing.T) {
	ctx := nestCtx(t)
	ctx.GameData.Util = &gamedata.UtilData{GenData: map[int]gamedata.GenInfo{
		1: {Min: 1, Max: 151},
		2: {Min: 152, Max: 251},
		3: {Min: 252, Max: 386},
	}}
	_, _ = ctx.Tracking.Nests.Insert(&db.NestTrackingAPI{ID: "user1", ProfileNo: 1, PokemonID: 25})
	_, _ = ctx.Tracking.Nests.Insert(&db.NestTrackingAPI{ID: "user1", ProfileNo: 1, PokemonID: 252})

	replies := (&NestCommand{}).Run(ctx, []string{"remove", "gen1"})
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)

	(&NestCommand{}).Run(ctx, []string{"remove", "everything", "gen1"})
	rows, _ := ctx.Tracking.Nests.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 252, rows[0].PokemonID)
}

// An unknown pokemon in energy:/candy: is refused rather than widened to
// "any pokemon" — on add and on remove.
func TestQuest_UnknownEnergyPokemonRefused(t *testing.T) {
	ctx := questCtx(t)
	replies := runQuest(t, ctx, "energy:zzqx")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Empty(t, questKinds(t, ctx))

	runQuest(t, ctx, "energy")
	replies = runQuest(t, ctx, "remove energy:zzqx")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Len(t, questKinds(t, ctx), 1)
}
