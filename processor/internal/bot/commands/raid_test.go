package commands

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func raidCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}

	raids := store.NewMockTrackingStore[db.RaidTrackingAPI](
		store.RaidGetUID, store.RaidSetUID,
	)
	ctx.Tracking = &store.TrackingStores{
		Raids: raids,
	}

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}:    {PokemonID: 25, FormID: 0},
			{ID: 649, Form: 0}:   {PokemonID: 649, FormID: 0},
			{ID: 649, Form: 917}: {PokemonID: 649, FormID: 917},
		},
		Moves:    map[int]*gamedata.Move{},
		Types:    map[int]*gamedata.TypeInfo{},
		Costumes: map[int]gamedata.CostumeInfo{1: {ID: 1, Name: "Holiday 2016"}},
	}

	// The resolver indexes poke_{id} names at construction, and the
	// ArgMatcher indexes costume_{id} names at construction too (see
	// buildMultiWordVocabularies), so name/form/costume translations must
	// land in the bundle before NewPokemonResolver/NewArgMatcher run below.
	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_649":  "Genesect",
		"form_917":  "Burn",
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

func runRaid(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	cmd := &RaidCommand{}
	args := strings.Fields(input)
	return cmd.Run(ctx, args)
}

func TestRaid_BasicPokemon(t *testing.T) {
	ctx := raidCtx(t)
	replies := runRaid(t, ctx, "25")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 25, rows[0].PokemonID)
	assert.Equal(t, bot.WildcardID, rows[0].Level, "pokemon tracking should use wildcard level")
}

func TestRaid_ByLevel(t *testing.T) {
	ctx := raidCtx(t)
	replies := runRaid(t, ctx, "level5")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 5, rows[0].Level)
	assert.Equal(t, bot.WildcardID, rows[0].PokemonID, "level tracking should use wildcard pokemon")
}

func TestRaid_FormFilter(t *testing.T) {
	ctx := raidCtx(t)
	replies := runRaid(t, ctx, "genesect form:burn")

	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	assert.Equal(t, 649, rows[0].PokemonID)
	assert.Equal(t, 917, rows[0].Form, "form:burn must narrow the rule to the Burn form, not form 0")
}

func TestRaid_FormFilter_UnknownForm(t *testing.T) {
	ctx := raidCtx(t)
	replies := runRaid(t, ctx, "genesect form:bogus")

	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React, "unknown form must be rejected, reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	assert.Empty(t, rows, "unknown form must not silently create a form-0 rule")
}

func TestRaid_Remove_FormRejected(t *testing.T) {
	ctx := raidCtx(t)
	runRaid(t, ctx, "genesect form:burn")
	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)

	// Pokemon remove (!untrack) rejects form: as unrecognized; raid remove
	// must do the same rather than silently removing every form.
	replies := runRaid(t, ctx, "remove genesect form:burn")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React, "reply: %s", replies[0].Text)
	assert.Contains(t, replies[0].Text, "form:burn")

	rows, _ = ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 1, "rejected remove must not delete anything")
}

func TestRaid_Duplicate(t *testing.T) {
	ctx := raidCtx(t)
	replies1 := runRaid(t, ctx, "25")
	require.NotEmpty(t, replies1)
	assert.Equal(t, "✅", replies1[0].React)

	replies2 := runRaid(t, ctx, "25")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "👌", replies2[0].React, "duplicate should be 👌, reply: %s", replies2[0].Text)
}

func TestRaid_Remove(t *testing.T) {
	ctx := raidCtx(t)
	// Add first
	runRaid(t, ctx, "25")
	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)

	// Remove
	replies := runRaid(t, ctx, "remove 25")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0)
}

// TestRaid_RemoveByUID_IgnoresBadCostume verifies that `!raid remove id:N`
// deletes the rule even when an unresolvable costume: arg is also present.
// Remove-by-UID targets a specific rule directly and never consults the
// costume filter, so a costume arg — valid or not — must not block it.
func TestRaid_RemoveByUID_IgnoresBadCostume(t *testing.T) {
	ctx := raidCtx(t)
	runRaid(t, ctx, "25")
	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	require.Len(t, rows, 1)
	uid := rows[0].UID

	replies := runRaid(t, ctx, fmt.Sprintf("remove id:%d costume:bogus", uid))
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "id: removal must not be blocked by an unresolvable costume arg, reply: %s", replies[0].Text)

	rows, _ = ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	assert.Len(t, rows, 0, "rule should have been removed by UID despite the bad costume arg")
}

func TestRaid_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := raidCtx(t)
	ctx.IsAdmin = false

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runRaid(t, ctx, "25 template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
	assert.Contains(t, replies[0].Text, "99")
}

func TestRaid_InvalidTemplate_Admin(t *testing.T) {
	ctx := raidCtx(t)
	ctx.IsAdmin = true

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	replies := runRaid(t, ctx, "25 template:99")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "admin should not be blocked, reply: %s", replies[0].Text)
	assert.Contains(t, replies[0].Text, "99")
}

func TestRaid_NoTarget(t *testing.T) {
	ctx := raidCtx(t)
	// No pokemon, no level — should fail
	replies := runRaid(t, ctx, "clean")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React)
}

func TestRaid_AcceptsAreaOverride(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)

	raids := store.NewMockTrackingStore[db.RaidTrackingAPI](
		store.RaidGetUID, store.RaidSetUID,
	)
	ctx.Tracking = &store.TrackingStores{Raids: raids}

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

	cmd := &RaidCommand{}
	replies := cmd.Run(ctx, strings.Fields("level:5 area:london"))
	require.NotEmpty(t, replies)
	assert.NotEqual(t, "🙅", replies[0].React, "rejected: %+v", replies)

	rules, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	require.Len(t, rules, 1)
	assert.Len(t, rules[0].OverrideAreas, 1, "override not stored: %+v", rules[0])
}

// TestRaid_Costume verifies costume:<name> resolves to a costume ID and is
// stored on the rule, and that a bare add defaults to the 9000 "any costume"
// wildcard. raidCtx's resolver does not register a "pikachu" name (id 25 is
// only reachable by numeric ID in these tests — see TestRaid_BasicPokemon),
// so this uses "genesect" (id 649), the species raidCtx does register a
// poke_649 translation for, paired with the costume_1 "Holiday 2016"
// translation added to raidCtx for this test.
//
// The input is "costume:holiday 2016" (space, not underscore): real users
// type "costume:holiday_2016" and bot/parser.go's tokenizer converts the
// underscore to a space before ArgMatcher ever sees the token (see
// TestTrack_Costume_Named in track_costume_test.go, and the ArgMatcher-level
// coverage in internal/bot/argmatch_costume_test.go). runRaid calls
// RaidCommand.Run directly, bypassing that parser step, so this test
// supplies the already-converted form the command actually receives.
func TestRaid_Costume(t *testing.T) {
	ctx := raidCtx(t)
	replies := runRaid(t, ctx, "genesect costume:holiday 2016")
	require.NotEmpty(t, replies)
	assert.Equal(t, "✅", replies[0].React, "reply: %s", replies[0].Text)

	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	if len(rows) != 1 || rows[0].Costume != 1 {
		t.Fatalf("expected 1 raid rule with Costume=1, got %+v", rows)
	}

	// Bare add defaults to 9000 (any).
	ctx2 := raidCtx(t)
	replies2 := runRaid(t, ctx2, "genesect")
	require.NotEmpty(t, replies2)
	assert.Equal(t, "✅", replies2[0].React, "reply: %s", replies2[0].Text)

	rows2, _ := ctx2.Tracking.Raids.SelectByIDProfile("user1", 1)
	if len(rows2) != 1 || rows2[0].Costume != 9000 {
		t.Fatalf("bare !raid genesect should store Costume=9000, got %+v", rows2)
	}
}
