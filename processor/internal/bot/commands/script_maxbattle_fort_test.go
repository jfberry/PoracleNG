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
		Tappables:  store.NewMockTrackingStore[db.TappableTrackingAPI](store.TappableGetUID, store.TappableSetUID),
	}
	return ctx
}

func runScriptLines(t *testing.T, ctx *bot.CommandContext) []string {
	t.Helper()
	replies := (&ScriptCommand{}).Run(ctx, nil)
	require.NotEmpty(t, replies)
	return strings.Split(replies[0].Text, "\n")
}

// replay runs each emitted script line through the matching command.
func replay(t *testing.T, ctx *bot.CommandContext, lines []string) {
	t.Helper()
	prefix := bot.CommandPrefix(ctx)
	for _, line := range lines {
		fields := strings.Fields(strings.TrimPrefix(line, prefix))
		require.NotEmpty(t, fields)
		var cmd bot.Command
		switch fields[0] {
		case "maxbattle":
			cmd = &MaxbattleCommand{}
		case "fort":
			cmd = &FortCommand{}
		default:
			t.Fatalf("unexpected script line %q", line)
		}
		// Mirror the parser: underscores become spaces in arguments.
		args := make([]string, 0, len(fields)-1)
		for _, a := range fields[1:] {
			args = append(args, strings.ReplaceAll(a, "_", " "))
		}
		r := cmd.Run(ctx, args)
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
