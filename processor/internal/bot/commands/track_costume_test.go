package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// costumeCtx builds a CommandContext wired for !track/!untrack costume tests.
// Unlike trackCtx (track_test.go), GameData.Costumes and the costume_1
// translation must exist BEFORE bot.NewArgMatcher runs, since the multi-word
// costume vocabulary is built once at construction time (see
// ArgMatcher.buildMultiWordVocabularies). Mirrors nestCtx's pattern of
// seeding translations before constructing the resolver/matcher.
func costumeCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{
		PVP: config.PVPConfig{
			LevelCaps: []int{50},
		},
	}

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
		Moves:    map[int]*gamedata.Move{},
		Types:    map[int]*gamedata.TypeInfo{},
		Costumes: map[int]gamedata.CostumeInfo{1: {ID: 1, Name: "Holiday 2016"}},
	}

	// costume_1 must land in the bundle before NewArgMatcher indexes the
	// multi-word costume vocabulary.
	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25":   "Pikachu",
		"costume_1": "Holiday 2016",
	}))

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

// TestTrack_Costume_Named verifies `!track pikachu costume:holiday 2016`
// resolves the multi-word costume name to its ID and stores it. Real users
// type "costume:holiday_2016"; bot/parser.go's tokenize step converts
// underscores to spaces before ArgMatcher ever sees the token (verified at
// the ArgMatcher layer in TestCostumeArgCaptured / TestCostumeArgEagerJoinsMultiWord
// in internal/bot/argmatch_costume_test.go). This test exercises the same
// multi-word join + resolution end-to-end through the command.
func TestTrack_Costume_Named(t *testing.T) {
	ctx := costumeCtx(t)

	replies := runTrack(t, ctx, "pikachu costume:holiday 2016")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Costume)
}

// TestTrack_Costume_Zero verifies costume:0 ("no costume") is stored as 0,
// not defaulted to the 9000 wildcard.
func TestTrack_Costume_Zero(t *testing.T) {
	ctx := costumeCtx(t)

	replies := runTrack(t, ctx, "pikachu costume:0")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 0, rows[0].Costume)
}

// TestTrack_Costume_DefaultsToAny verifies a bare !track pikachu (no
// costume: arg) still defaults to the 9000 "any costume" wildcard — this
// pins the behavior that used to be a stopgap literal in track.go.
func TestTrack_Costume_DefaultsToAny(t *testing.T) {
	ctx := costumeCtx(t)

	replies := runTrack(t, ctx, "pikachu")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 9000, rows[0].Costume)
}

// TestTrack_Costume_UnresolvedName verifies an unrecognized costume name
// produces a 🙅 reply rather than silently falling back to some ID or
// crashing.
func TestTrack_Costume_UnresolvedName(t *testing.T) {
	ctx := costumeCtx(t)

	replies := runTrack(t, ctx, "pikachu costume:bogus")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "bogus")

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0, "no rule should be created when costume: is unresolved")
}

// TestUntrack_Costume_RemovesOnlyMatchingRule verifies !untrack pikachu
// costume:1 removes only the costume-1 rule, leaving the wildcard
// (costume 9000) rule for the same species untouched.
func TestUntrack_Costume_RemovesOnlyMatchingRule(t *testing.T) {
	ctx := costumeCtx(t)

	// Seed two pikachu rules: the default wildcard and a costume-1 rule.
	_, err := ctx.Tracking.Monsters.Insert(&db.MonsterTrackingAPI{PokemonID: 25, Costume: 9000})
	require.NoError(t, err)
	_, err = ctx.Tracking.Monsters.Insert(&db.MonsterTrackingAPI{PokemonID: 25, Costume: 1})
	require.NoError(t, err)

	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 2, "pre-condition: two pikachu rules should exist")

	// Numeric costume value — avoids any dependency on multi-word
	// resolution for this assertion.
	cmd := &UntrackCommand{}
	replies := cmd.Run(ctx, strings.Fields("25 costume:1"))
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Monsters.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1, "only the costume-1 rule should have been removed")
	assert.Equal(t, 9000, rows[0].Costume, "the remaining rule should be the wildcard rule")
}

// TestMonsterRowText_Costume verifies the rowtext for a costume-1 rule
// includes the translated costume name, so !tracked surfaces it.
func TestMonsterRowText_Costume(t *testing.T) {
	ctx := costumeCtx(t)
	tr := ctx.Tr()

	rule := &db.MonsterTracking{
		PokemonID: 25,
		Costume:   1,
		MinIV:     -1,
		MaxIV:     100,
		MaxCP:     9000,
		MaxLevel:  55,
		MaxATK:    15,
		MaxDEF:    15,
		MaxSTA:    15,
		Rarity:    -1,
		MaxRarity: 6,
		Size:      -1,
		MaxSize:   5,
	}
	text := ctx.RowText.MonsterRowText(tr, rule)
	assert.Contains(t, text, "Holiday 2016")
}

// TestMonsterRowText_CostumeWildcardOmitted verifies the default 9000
// wildcard costume produces no costume text in the rowtext.
func TestMonsterRowText_CostumeWildcardOmitted(t *testing.T) {
	ctx := costumeCtx(t)
	tr := ctx.Tr()

	rule := &db.MonsterTracking{
		PokemonID: 25,
		Costume:   9000,
		MinIV:     -1,
		MaxIV:     100,
		MaxCP:     9000,
		MaxLevel:  55,
		MaxATK:    15,
		MaxDEF:    15,
		MaxSTA:    15,
		Rarity:    -1,
		MaxRarity: 6,
		Size:      -1,
		MaxSize:   5,
	}
	text := ctx.RowText.MonsterRowText(tr, rule)
	assert.NotContains(t, text, "Holiday 2016")
	assert.NotContains(t, text, tr.T("msg.no_costume"))
}

// TestMonsterRowText_CostumeZero verifies costume 0 ("no costume") shows
// the msg.no_costume label rather than a translation-key fallback.
func TestMonsterRowText_CostumeZero(t *testing.T) {
	ctx := costumeCtx(t)
	tr := ctx.Tr()

	rule := &db.MonsterTracking{
		PokemonID: 25,
		Costume:   0,
		MinIV:     -1,
		MaxIV:     100,
		MaxCP:     9000,
		MaxLevel:  55,
		MaxATK:    15,
		MaxDEF:    15,
		MaxSTA:    15,
		Rarity:    -1,
		MaxRarity: 6,
		Size:      -1,
		MaxSize:   5,
	}
	text := ctx.RowText.MonsterRowText(tr, rule)
	assert.Contains(t, text, tr.T("msg.no_costume"))
}
