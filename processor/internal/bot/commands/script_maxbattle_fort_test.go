package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// scriptCtx is a command context with every tracking store mocked (so
// !script can query them all) and game data for pokemon 25.
func scriptCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx := maxbattleCtx(t)
	ctx.Tracking = &store.TrackingStores{
		Monsters:   store.NewMockTrackingStore[db.MonsterTrackingAPI](store.MonsterGetUID, store.MonsterSetUID),
		Raids:      store.NewMockTrackingStore[db.RaidTrackingAPI](store.RaidGetUID, store.RaidSetUID),
		Eggs:       store.NewMockTrackingStore[db.EggTrackingAPI](store.EggGetUID, store.EggSetUID),
		Quests:     store.NewMockTrackingStore[db.QuestTrackingAPI](store.QuestGetUID, store.QuestSetUID),
		Invasions:  store.NewMockTrackingStore[db.InvasionTrackingAPI](store.InvasionGetUID, store.InvasionSetUID),
		Lures:      store.NewMockTrackingStore[db.LureTrackingAPI](store.LureGetUID, store.LureSetUID),
		Nests:      store.NewMockTrackingStore[db.NestTrackingAPI](store.NestGetUID, store.NestSetUID),
		Gyms:       store.NewMockTrackingStore[db.GymTrackingAPI](store.GymGetUID, store.GymSetUID),
		Forts:      store.NewMockTrackingStore[db.FortTrackingAPI](store.FortGetUID, store.FortSetUID),
		Maxbattles: store.NewMockTrackingStore[db.MaxbattleTrackingAPI](store.MaxbattleGetUID, store.MaxbattleSetUID),
	}
	return ctx
}

func runScriptLines(t *testing.T, ctx *bot.CommandContext) []string {
	t.Helper()
	replies := (&ScriptCommand{}).Run(ctx, nil)
	require.NotEmpty(t, replies)
	return strings.Split(replies[0].Text, "\n")
}

// scriptCommands maps the command keys a script line can parse to.
var scriptCommands = map[string]bot.Command{
	"cmd.track":     &TrackCommand{},
	"cmd.raid":      &RaidCommand{},
	"cmd.egg":       &EggCommand{},
	"cmd.invasion":  &InvasionCommand{},
	"cmd.lure":      &LureCommand{},
	"cmd.nest":      &NestCommand{},
	"cmd.gym":       &GymCommand{},
	"cmd.quest":     &QuestCommand{},
	"cmd.maxbattle": &MaxbattleCommand{},
	"cmd.fort":      &FortCommand{},
}

// replay runs each line through the real command parser (so quoting and
// the underscore→space rewrite apply exactly as for a user) and then the
// matching command, requiring every line to succeed.
func replay(t *testing.T, ctx *bot.CommandContext, lines []string) {
	t.Helper()
	parser := bot.NewParser(bot.CommandPrefix(ctx), ctx.Translations, []string{"en"}, nil)
	for _, line := range lines {
		cmds := parser.Parse(line)
		require.Len(t, cmds, 1, "line %q", line)
		cmd, ok := scriptCommands[cmds[0].CommandKey]
		require.True(t, ok, "unexpected script line %q", line)
		r := cmd.Run(ctx, cmds[0].Args)
		require.NotEmpty(t, r)
		require.Contains(t, []string{"✅", "👌"}, r[0].React, "line %q: %s", line, r[0].Text)
	}
}

func stripMaxbattle(rows []db.MaxbattleTrackingAPI) []db.MaxbattleTrackingAPI {
	out := make([]db.MaxbattleTrackingAPI, len(rows))
	for i, r := range rows {
		r.UID = 0
		out[i] = r
	}
	return out
}

func stripFort(rows []db.FortTrackingAPI) []db.FortTrackingAPI {
	out := make([]db.FortTrackingAPI, len(rows))
	for i, r := range rows {
		r.UID = 0
		out[i] = r
	}
	return out
}

func TestScript_MaxbattleAndFortRoundTrip(t *testing.T) {
	ctx := scriptCtx(t)
	for _, in := range []string{"25 gmax d:500 clean", "level3", "everything template:2"} {
		r := runMaxbattle(t, ctx, in)
		require.Equal(t, "✅", r[0].React, "%s: %s", in, r[0].Text)
	}
	for _, in := range []string{"pokestop photo name include_empty", "gym new removal d:200", "everything location description"} {
		var args []string
		for _, a := range strings.Fields(in) {
			args = append(args, strings.ReplaceAll(a, "_", " "))
		}
		r := (&FortCommand{}).Run(ctx, args)
		require.Equal(t, "✅", r[0].React, "%s: %s", in, r[0].Text)
	}

	origMB, _ := ctx.Tracking.Maxbattles.SelectByIDProfile("user1", 1)
	origFort, _ := ctx.Tracking.Forts.SelectByIDProfile("user1", 1)
	require.Len(t, origMB, 3)
	require.Len(t, origFort, 3)

	lines := runScriptLines(t, ctx)
	require.Len(t, lines, 6, "script: %v", lines)

	// Wipe and replay.
	fresh := scriptCtx(t)
	replay(t, fresh, lines)

	gotMB, _ := fresh.Tracking.Maxbattles.SelectByIDProfile("user1", 1)
	gotFort, _ := fresh.Tracking.Forts.SelectByIDProfile("user1", 1)
	assert.ElementsMatch(t, stripMaxbattle(origMB), stripMaxbattle(gotMB))
	assert.ElementsMatch(t, stripFort(origFort), stripFort(gotFort))
}

func TestScript_MaxbattleLines(t *testing.T) {
	ctx := scriptCtx(t)
	c := &ScriptCommand{}
	assert.Equal(t, "!maxbattle 25 gmax d:100 clean", c.maxbattleToScript(ctx, "!", &db.MaxbattleTrackingAPI{
		PokemonID: 25, Level: 90, Move: bot.WildcardID, Gmax: 1, Distance: 100, Clean: 3,
	}))
	assert.Equal(t, "!maxbattle level:5", c.maxbattleToScript(ctx, "!", &db.MaxbattleTrackingAPI{
		PokemonID: bot.WildcardID, Level: 5, Move: bot.WildcardID,
	}))
	assert.Equal(t, "!maxbattle everything template:x", c.maxbattleToScript(ctx, "!", &db.MaxbattleTrackingAPI{
		PokemonID: bot.WildcardID, Level: 90, Move: bot.WildcardID, Template: "x",
	}))
}

func TestScript_FortLines(t *testing.T) {
	ctx := scriptCtx(t)
	c := &ScriptCommand{}
	assert.Equal(t, "!fort station photo removal include_empty", c.fortToScript(ctx, "!", &db.FortTrackingAPI{
		FortType: "station", ChangeTypes: `["image_url","removal"]`, IncludeEmpty: true,
	}))
	assert.Equal(t, "!fort everything", c.fortToScript(ctx, "!", &db.FortTrackingAPI{
		FortType: "everything", ChangeTypes: "[]",
	}))
}
