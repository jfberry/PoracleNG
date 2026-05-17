package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spyCommand records the args it was called with so tests can assert routing.
type spyCommand struct {
	name    string
	gotArgs []string
	replies []bot.Reply
}

func (s *spyCommand) Name() string      { return s.name }
func (s *spyCommand) Aliases() []string { return nil }
func (s *spyCommand) Run(_ *bot.CommandContext, args []string) []bot.Reply {
	s.gotArgs = args
	if s.replies != nil {
		return s.replies
	}
	return []bot.Reply{{React: "✅"}}
}

// untrackCtx builds a CommandContext wired for untrack tests. It creates a
// minimal pokemon tracking store (for the !untrack pikachu fall-through path)
// plus a Registry populated with spy commands for raid, egg, and invasion.
func untrackCtx(t *testing.T) (*bot.CommandContext, map[string]*spyCommand) {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}

	monsters := store.NewMockTrackingStore[db.MonsterTrackingAPI](
		store.MonsterGetUID, store.MonsterSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Monsters: monsters,
	}

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
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

	// Build a registry with spy commands for the types we want to test.
	registry := bot.NewRegistry()
	spies := map[string]*spyCommand{}
	for _, typeName := range []string{"raid", "egg", "invasion", "quest", "incident", "lure", "nest", "gym", "fort", "maxbattle"} {
		spy := &spyCommand{name: "cmd." + typeName}
		spies[typeName] = spy
		registry.Register(spy)
	}
	ctx.Registry = registry

	return ctx, spies
}

func runUntrack(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &UntrackCommand{}
	args := strings.Fields(input)
	return cmd.Run(ctx, args)
}

// TestUntrack_Pokemon_FallThrough verifies that existing !untrack <pokemon-id>
// behavior is unchanged — the pokemon path must not be intercepted by the
// type reroute.
func TestUntrack_Pokemon_FallThrough(t *testing.T) {
	ctx, _ := untrackCtx(t)

	// Seed a pikachu tracking rule so there is something to remove.
	pikachu := db.MonsterTrackingAPI{PokemonID: 25}
	_, err := ctx.Tracking.Monsters.Insert(&pikachu)
	require.NoError(t, err)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1, "pre-condition: pikachu tracking rule should exist")

	// Use numeric ID to avoid dependency on translation bundle in tests.
	replies := runUntrack(t, ctx, "25")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0, "pokemon 25 tracking rule should have been removed")
}

// TestUntrack_Raid_Reroute verifies that !untrack raid id:12 is rerouted to
// cmd.raid with ["remove", "id:12"].
func TestUntrack_Raid_Reroute(t *testing.T) {
	ctx, spies := untrackCtx(t)

	replies := runUntrack(t, ctx, "raid id:12")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React)
	assert.Equal(t, []string{"remove", "id:12"}, spies["raid"].gotArgs,
		"cmd.raid should have been called with [remove id:12]")
}

// TestUntrack_Egg_Reroute verifies that !untrack egg level:5 is rerouted to
// cmd.egg with ["remove", "level:5"].
func TestUntrack_Egg_Reroute(t *testing.T) {
	ctx, spies := untrackCtx(t)

	replies := runUntrack(t, ctx, "egg level:5")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React)
	assert.Equal(t, []string{"remove", "level:5"}, spies["egg"].gotArgs,
		"cmd.egg should have been called with [remove level:5]")
}

// TestUntrack_Invasion_Reroute verifies that !untrack invasion grunt:bug is
// rerouted to cmd.invasion with ["remove", "grunt:bug"].
func TestUntrack_Invasion_Reroute(t *testing.T) {
	ctx, spies := untrackCtx(t)

	replies := runUntrack(t, ctx, "invasion grunt:bug")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React)
	assert.Equal(t, []string{"remove", "grunt:bug"}, spies["invasion"].gotArgs,
		"cmd.invasion should have been called with [remove grunt:bug]")
}

// TestUntrack_RerouteWithNoExtraArgs verifies that !untrack raid (no further
// args) calls cmd.raid with just ["remove"].
func TestUntrack_RerouteWithNoExtraArgs(t *testing.T) {
	ctx, spies := untrackCtx(t)

	replies := runUntrack(t, ctx, "raid")
	require.NotEmpty(t, replies)
	assert.Equal(t, []string{"remove"}, spies["raid"].gotArgs)
}

// TestUntrack_NilRegistry_ReturnsError verifies that a nil registry produces
// an error reply rather than a panic.
func TestUntrack_NilRegistry_ReturnsError(t *testing.T) {
	ctx, _ := untrackCtx(t)
	ctx.Registry = nil

	replies := runUntrack(t, ctx, "raid id:12")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
}

// TestUntrack_ValidUntrackType_HelperCoverage exercises validUntrackType.
func TestUntrack_ValidUntrackType_HelperCoverage(t *testing.T) {
	for _, typ := range []string{"raid", "egg", "quest", "invasion", "incident", "lure", "nest", "gym", "fort", "maxbattle"} {
		assert.True(t, validUntrackType(typ), "%s should be a valid untrack type", typ)
	}
	for _, notType := range []string{"pokemon", "pikachu", "track", "untrack", ""} {
		assert.False(t, validUntrackType(notType), "%q should NOT be a valid untrack type", notType)
	}
}
