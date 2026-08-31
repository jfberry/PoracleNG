package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func invasionTestCtx(t *testing.T) (*bot.CommandContext, *store.MockTrackingStore[db.InvasionTrackingAPI]) {
	t.Helper()
	ctx, _ := testCtx(t)

	invasions := store.NewMockTrackingStore[db.InvasionTrackingAPI](
		store.InvasionGetUID, store.InvasionSetUID,
	).WithIDScope(func(i *db.InvasionTrackingAPI) string { return i.ID })
	ctx.Tracking = &store.TrackingStores{Invasions: invasions}

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Moves:    map[int]*gamedata.Move{},
		Types:    map[int]*gamedata.TypeInfo{},
		Grunts: map[int]*gamedata.Grunt{
			500: {ID: 500, Template: "CHARACTER_EVENT_NPC_0"},
			40:  {ID: 40, Template: "CHARACTER_PLAYER_TEAM_LEADER"},
			44:  {ID: 44, Template: "CHARACTER_GIOVANNI"},
		},
		Util: &gamedata.UtilData{PokestopEvent: map[int]gamedata.EventInfo{}},
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
	return ctx, invasions
}

// The parser replaces underscores with spaces in every unquoted token
// (parser.go), so `!invasion npc_0` reaches the command as the arg "npc 0"
// while the canonical set built from TypeNameFromTemplate holds "npc_0". Every
// underscore-named grunt — the 24 npc_* and player_team_leader — was therefore
// unreachable from the bot unless the user thought to quote it.
func TestInvasionAcceptsUnderscoreNamesAfterParserNormalisation(t *testing.T) {
	for _, arg := range []string{"npc 0", "player team leader"} {
		t.Run(arg, func(t *testing.T) {
			ctx, invasions := invasionTestCtx(t)
			c := &InvasionCommand{}
			replies := c.Run(ctx, []string{arg})
			if len(replies) == 0 {
				t.Fatal("expected a reply")
			}
			if replies[0].React == "🙅" {
				t.Errorf("%q was rejected. The parser turns the typed npc_0 into %q, "+
					"so this is exactly how an underscore-named grunt arrives: %+v",
					arg, arg, replies[0])
			}
			// And it must store the CANONICAL underscore form, or the matcher
			// (which compares against ResolveGruntTypeName) will never fire.
			rows := invasions.AllRows()
			want := strings.ReplaceAll(arg, " ", "_")
			if len(rows) != 1 || rows[0].GruntType != want {
				t.Errorf("stored = %+v, want grunt_type %q", rows, want)
			}
		})
	}
}

// A name that matches nothing must still be rejected — normalising a spelling
// is not the same as accepting anything.
func TestInvasionStillRejectsUnknownName(t *testing.T) {
	ctx, _ := invasionTestCtx(t)
	c := &InvasionCommand{}
	replies := c.Run(ctx, []string{"no such grunt"})
	if len(replies) == 0 || replies[0].React != "🙅" {
		t.Errorf("expected rejection of an unknown name, got %+v", replies)
	}
}

// A name with no underscore is unaffected.
func TestInvasionAcceptsPlainName(t *testing.T) {
	ctx, _ := invasionTestCtx(t)
	c := &InvasionCommand{}
	replies := c.Run(ctx, []string{"giovanni"})
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}
	if replies[0].React == "🙅" {
		t.Errorf("giovanni was rejected: %+v", replies[0])
	}
}
