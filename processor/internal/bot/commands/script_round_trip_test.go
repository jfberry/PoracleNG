package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// roundTripCtx is scriptCtx plus everything the rules below need: PVP
// config for !track, permitted fences (one with a space, one with an
// underscore), saved locations (one label with a space), and names for a
// formed pokemon and an item.
func roundTripCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx := scriptCtx(t)
	ctx.Config = &config.Config{
		PVP: config.PVPConfig{
			PVPFilterMaxRank:     100,
			PVPFilterGreatMinCP:  1400,
			PVPFilterUltraMinCP:  2350,
			PVPFilterLittleMinCP: 450,
			LevelCaps:            []int{50},
		},
	}
	ctx.HasLocation = true

	ctx.GameData.Monsters[gamedata.MonsterKey{ID: 649, Form: 0}] = &gamedata.Monster{PokemonID: 649}
	ctx.GameData.Monsters[gamedata.MonsterKey{ID: 649, Form: 917}] = &gamedata.Monster{PokemonID: 649, FormID: 917}
	// Multi-word pokemon and form names, which must stay one token.
	ctx.GameData.Monsters[gamedata.MonsterKey{ID: 785, Form: 0}] = &gamedata.Monster{PokemonID: 785}
	ctx.GameData.Monsters[gamedata.MonsterKey{ID: 128, Form: 0}] = &gamedata.Monster{PokemonID: 128}
	ctx.GameData.Monsters[gamedata.MonsterKey{ID: 128, Form: 2975}] = &gamedata.Monster{PokemonID: 128, FormID: 2975}
	ctx.GameData.Items = map[int]*gamedata.Item{701: {ItemID: 701}}
	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25":   "Pikachu",
		"poke_649":  "Genesect",
		"form_917":  "Burn",
		"poke_785":  "Tapu Koko",
		"poke_128":  "Tauros",
		"form_2975": "Paldea Combat",
		"item_701":  "Razz Berry",
	}))
	resolver := bot.NewPokemonResolver(ctx.GameData, ctx.Translations, []string{"en"}, nil)
	ctx.Resolver = resolver
	ctx.ArgMatcher = bot.NewArgMatcher(ctx.Translations, ctx.GameData, resolver, []string{"en"})

	ctx.AreaLogic = bot.NewAreaLogic([]geofence.Fence{
		{Name: "London", UserSelectable: true},
		{Name: "North London", UserSelectable: true},
		{Name: "gent_centrum", UserSelectable: true},
	}, ctx.Config)

	humans := ctx.Humans.(*store.MockHumanStore)
	for _, label := range []string{"Home", "My Spot"} {
		_, err := humans.AddLocation(store.UserLocation{ID: "user1", Label: label, Latitude: 51.5, Longitude: -0.1})
		require.NoError(t, err)
	}
	return ctx
}

// allRows is every tracking row for user1/profile 1, with UIDs zeroed so
// two profiles built independently compare equal.
type allRows struct {
	Monsters, Raids, Eggs, Invasions, Lures, Nests, Gyms, Quests, Maxbattles, Forts any
}

func snapshotRows(t *testing.T, ctx *bot.CommandContext) allRows {
	t.Helper()
	tr := ctx.Tracking
	mon, _ := tr.Monsters.SelectByIDProfile("user1", 1)
	raid, _ := tr.Raids.SelectByIDProfile("user1", 1)
	egg, _ := tr.Eggs.SelectByIDProfile("user1", 1)
	inv, _ := tr.Invasions.SelectByIDProfile("user1", 1)
	lure, _ := tr.Lures.SelectByIDProfile("user1", 1)
	nest, _ := tr.Nests.SelectByIDProfile("user1", 1)
	gym, _ := tr.Gyms.SelectByIDProfile("user1", 1)
	quest, _ := tr.Quests.SelectByIDProfile("user1", 1)
	mb, _ := tr.Maxbattles.SelectByIDProfile("user1", 1)
	fort, _ := tr.Forts.SelectByIDProfile("user1", 1)
	for i := range mon {
		mon[i].UID = 0
	}
	for i := range raid {
		raid[i].UID = 0
	}
	for i := range egg {
		egg[i].UID = 0
	}
	for i := range inv {
		inv[i].UID = 0
	}
	for i := range lure {
		lure[i].UID = 0
	}
	for i := range nest {
		nest[i].UID = 0
	}
	for i := range gym {
		gym[i].UID = 0
	}
	for i := range quest {
		quest[i].UID = 0
	}
	return allRows{mon, raid, egg, inv, lure, nest, gym, quest, stripMaxbattle(mb), stripFort(fort)}
}

// TestScript_RoundTripAllTypes creates rules for every tracking type with
// the per-rule settings !script has to carry (pings, area:/location:
// overrides, template, clean/edit/summary, and the quest reward details),
// replays the emitted script into an empty profile, and requires the
// stored rows to come back identical.
func TestScript_RoundTripAllTypes(t *testing.T) {
	ctx := roundTripCtx(t)
	replay(t, ctx, []string{
		"!track pikachu iv:90 area:london clean edit template:2 <@123>",
		`!track pikachu d:500 "location:my spot" <@&456>`,
		`!track genesect form:burn "area:north london" area:gent_centrum`,
		"!raid level:5 area:london clean edit <@123>",
		"!raid pikachu d:300 location:home template:2",
		`!egg level:3 template:2 clean edit "area:north london"`,
		"!invasion everything clean template:2 area:london <@123>",
		"!lure glacial clean edit d:100 location:home <@123>",
		"!nest pikachu clean template:2 area:london",
		"!gym mystic slot_changes clean area:london <@123>",
		"!quest stardust:500 summary area:london <@123>",
		"!quest pikachu shiny clean d:200 location:home",
		"!quest genesect form:burn template:2",
		"!quest candy:pikachu amount:3",
		"!quest energy",
		"!quest pokecoins:10",
		"!quest all_pokemon",
		"!quest all_items amount:2",
		"!quest razz_berry amount:3 clean",
		"!maxbattle pikachu clean area:london <@123>",
		"!track tapu_koko iv:80",
		`!track tauros "form:paldea combat"`,
		"!raid tapu_koko",
		"!nest tapu_koko",
		"!maxbattle tapu_koko",
		`!quest "candy:tapu koko"`,
		`!quest tauros "form:paldea combat"`,
		"!fort pokestop photo area:london <@123>",
		"!fort gym new d:100 location:home",
	})
	want := snapshotRows(t, ctx)

	lines := runScriptLines(t, ctx)

	fresh := roundTripCtx(t)
	replay(t, fresh, lines)
	got := snapshotRows(t, fresh)

	assert.ElementsMatch(t, want.Monsters, got.Monsters, "monsters; script:\n%v", lines)
	assert.ElementsMatch(t, want.Raids, got.Raids, "raids")
	assert.ElementsMatch(t, want.Eggs, got.Eggs, "eggs")
	assert.ElementsMatch(t, want.Invasions, got.Invasions, "invasions")
	assert.ElementsMatch(t, want.Lures, got.Lures, "lures")
	assert.ElementsMatch(t, want.Nests, got.Nests, "nests")
	assert.ElementsMatch(t, want.Gyms, got.Gyms, "gyms")
	assert.ElementsMatch(t, want.Quests, got.Quests, "quests")
	assert.ElementsMatch(t, want.Maxbattles, got.Maxbattles, "maxbattles")
	assert.ElementsMatch(t, want.Forts, got.Forts, "forts")
}

// TestScript_OverrideNeedsMatchingDistance: the commands only take
// location: with a distance and area: without one. A row that breaks that
// (possible through the API) must still script to a line the command
// accepts, so the override the matcher ignores is left out.
func TestScript_OverrideNeedsMatchingDistance(t *testing.T) {
	ctx := roundTripCtx(t)
	c := &ScriptCommand{}
	assert.Equal(t, "!lure glacial", c.lureToScript(ctx, "!", &db.LureTrackingAPI{
		LureID: 502, OverrideLocationLabel: "Home",
	}), "location: without a distance is dropped")
	assert.Equal(t, "!lure glacial d:500", c.lureToScript(ctx, "!", &db.LureTrackingAPI{
		LureID: 502, Distance: 500, OverrideAreas: []string{"london"},
	}), "area: with a distance is dropped")
}

// TestScriptToken_StripsEmbeddedQuotes: the tokenizer has no escapes, so a
// quote inside a quoted token would split it.
func TestScriptToken_StripsEmbeddedQuotes(t *testing.T) {
	assert.Equal(t, `"location:my spot"`, scriptToken(`location:my "spot"`))
	assert.Equal(t, `location:my"spot`, scriptToken(`location:my"spot`), "unquoted tokens keep their quotes")
}
