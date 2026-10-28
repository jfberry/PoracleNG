package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func tappableCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.Config = &config.Config{}
	ctx.Tracking = &store.TrackingStores{
		Tappables: store.NewMockTrackingStore[db.TappableTrackingAPI](store.TappableGetUID, store.TappableSetUID),
	}

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 6, Form: 0}:    {PokemonID: 6, FormID: 0},
			{ID: 25, Form: 0}:   {PokemonID: 25, FormID: 0},
			{ID: 25, Form: 598}: {PokemonID: 25, FormID: 598},
			{ID: 41, Form: 0}:   {PokemonID: 41, FormID: 0},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
		Items: map[int]*gamedata.Item{1301: {ItemID: 1301}},
	}
	// The resolver and the multi-word vocabulary are built from the bundle,
	// so names must land first.
	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_6":    "Charizard",
		"poke_25":   "Pikachu",
		"poke_41":   "Zubat",
		"item_1301": "Rare Candy",
	}))

	resolver := bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.Resolver = resolver
	ctx.ArgMatcher = bot.NewArgMatcher(ctx.Translations, gd, resolver, []string{"en"})
	ctx.GameData = gd
	ctx.RowText = &rowtext.Generator{GD: gd, Translations: ctx.Translations, DefaultTemplateName: "1"}
	ctx.HasArea = true
	return ctx
}

// runTappable mirrors the parser: whitespace split, underscores to spaces.
func runTappable(t *testing.T, ctx *bot.CommandContext, input string) []bot.Reply {
	t.Helper()
	var args []string
	for _, a := range strings.Fields(input) {
		args = append(args, strings.ReplaceAll(a, "_", " "))
	}
	return (&TappableCommand{}).Run(ctx, args)
}

func tappableRows(t *testing.T, ctx *bot.CommandContext) []db.TappableTrackingAPI {
	t.Helper()
	rows, err := ctx.Tracking.Tappables.SelectByIDProfile("user1", 1)
	require.NoError(t, err)
	return rows
}

type tappableKey struct{ rewardType, reward, amount int }

func tappableKeys(rows []db.TappableTrackingAPI) []tappableKey {
	out := make([]tappableKey, 0, len(rows))
	for _, r := range rows {
		out = append(out, tappableKey{r.RewardType, r.Reward, r.Amount})
	}
	return out
}

func TestTappable_BareAdd(t *testing.T) {
	ctx := tappableCtx(t)
	r := runTappable(t, ctx, "zubat")
	require.Equal(t, "✅", r[0].React, r[0].Text)
	rows := tappableRows(t, ctx)
	require.Len(t, rows, 1)
	assert.Equal(t, tappableKey{4, 41, 0}, tappableKey{rows[0].RewardType, rows[0].Reward, rows[0].Amount}, "bare pokemon = candy")
	assert.Contains(t, r[0].Text, "candy Zubat")
}

// One candy rule per species, even though the resolver returns each form.
func TestTappable_BarePokemonIgnoresForms(t *testing.T) {
	ctx := tappableCtx(t)
	runTappable(t, ctx, "pikachu")
	assert.ElementsMatch(t, []tappableKey{{4, 25, 0}}, tappableKeys(tappableRows(t, ctx)))
}

func TestTappable_RewardKeywords(t *testing.T) {
	cases := []struct {
		in   string
		want []tappableKey
	}{
		{"candy", []tappableKey{{4, 0, 0}}},
		{"candy:pikachu", []tappableKey{{4, 25, 0}}},
		{"candy pikachu amount:6", []tappableKey{{4, 25, 6}}},
		{"xlcandy", []tappableKey{{9, 0, 0}}},
		{"xlcandy:pikachu amount:2", []tappableKey{{9, 25, 2}}},
		{"xlcandy zubat", []tappableKey{{9, 41, 0}}},
		{"energy", []tappableKey{{12, 0, 0}}},
		{"energy:charizard", []tappableKey{{12, 6, 0}}},
		{"stardust", []tappableKey{{3, 0, 0}}},
		{"stardust:500", []tappableKey{{3, 500, 0}}},
		{"stardust amount:500", []tappableKey{{3, 500, 0}}},
		{"pokecoins", []tappableKey{{8, 0, 0}}},
		{"pokecoins:10", []tappableKey{{8, 10, 0}}},
		{"rare_candy", []tappableKey{{2, 1301, 0}}},
		{"rare_candy amount:2", []tappableKey{{2, 1301, 2}}},
		{"all_items", []tappableKey{{2, 0, 0}}},
		{"zubat stardust", []tappableKey{{4, 41, 0}, {3, 0, 0}}},
		{"everything", []tappableKey{{4, 0, 0}, {9, 0, 0}, {12, 0, 0}, {2, 0, 0}, {3, 0, 0}, {8, 0, 0}}},
		{"everything amount:5", []tappableKey{{4, 0, 5}, {9, 0, 5}, {12, 0, 5}, {2, 0, 5}, {3, 5, 0}, {8, 5, 0}}},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			ctx := tappableCtx(t)
			r := runTappable(t, ctx, c.in)
			require.Equal(t, "✅", r[0].React, r[0].Text)
			assert.ElementsMatch(t, c.want, tappableKeys(tappableRows(t, ctx)))
		})
	}
}

func TestTappable_CommonFields(t *testing.T) {
	ctx := tappableCtx(t)
	r := runTappable(t, ctx, "candy:pikachu d:500 clean")
	require.Equal(t, "✅", r[0].React, r[0].Text)
	rows := tappableRows(t, ctx)
	require.Len(t, rows, 1)
	assert.Equal(t, 500, rows[0].Distance)
	assert.Equal(t, 1, rows[0].Clean)
	assert.Equal(t, "", rows[0].Template)
}

func TestTappable_Rejections(t *testing.T) {
	for _, in := range []string{
		"zubat edit",
		"zubat summary",
		"zubat shiny",
		"zubat form:alola",
		"bogus",
		"amount:3",
		"stardust:500 amount:3",
		"pokecoins:5 amount:3",
		"clean",
	} {
		t.Run(in, func(t *testing.T) {
			ctx := tappableCtx(t)
			r := runTappable(t, ctx, in)
			require.NotEmpty(t, r)
			assert.Equal(t, "🙅", r[0].React, r[0].Text)
			assert.Empty(t, tappableRows(t, ctx))
		})
	}
}

func TestTappable_UnsupportedKeywordsSurfaceAsUnrecognized(t *testing.T) {
	for _, tok := range []string{"edit", "summary", "shiny", "form:alola"} {
		ctx := tappableCtx(t)
		r := runTappable(t, ctx, "zubat "+tok)
		assert.Contains(t, r[0].Text, tok)
	}
}

func TestTappable_AmountNotApplicableMessage(t *testing.T) {
	ctx := tappableCtx(t)
	r := runTappable(t, ctx, "stardust:500 amount:3")
	assert.Contains(t, r[0].Text, "amount:N")
}

func TestTappable_Duplicate(t *testing.T) {
	ctx := tappableCtx(t)
	require.Equal(t, "✅", runTappable(t, ctx, "zubat")[0].React)
	r := runTappable(t, ctx, "zubat")
	assert.Equal(t, "👌", r[0].React, r[0].Text)
	assert.Len(t, tappableRows(t, ctx), 1)
}

func TestTappable_UpdateSquashKeepsID(t *testing.T) {
	ctx := tappableCtx(t)
	runTappable(t, ctx, "zubat")
	before := tappableRows(t, ctx)
	require.Len(t, before, 1)

	r := runTappable(t, ctx, "zubat d:750")
	require.Equal(t, "✅", r[0].React, r[0].Text)
	after := tappableRows(t, ctx)
	require.Len(t, after, 1)
	assert.Equal(t, before[0].UID, after[0].UID, "an edited rule keeps its id")
	assert.Equal(t, 750, after[0].Distance)
}

func TestTappable_Remove(t *testing.T) {
	ctx := tappableCtx(t)
	runTappable(t, ctx, "zubat pikachu stardust")
	require.Len(t, tappableRows(t, ctx), 3)

	r := runTappable(t, ctx, "remove zubat")
	require.Equal(t, "✅", r[0].React, r[0].Text)
	assert.ElementsMatch(t, []tappableKey{{4, 25, 0}, {3, 0, 0}}, tappableKeys(tappableRows(t, ctx)))

	r = runTappable(t, ctx, "remove stardust")
	require.Equal(t, "✅", r[0].React, r[0].Text)
	assert.ElementsMatch(t, []tappableKey{{4, 25, 0}}, tappableKeys(tappableRows(t, ctx)))

	assert.Equal(t, "👌", runTappable(t, ctx, "remove xlcandy")[0].React)
}

func TestTappable_RemoveWithoutRewardRemovesAll(t *testing.T) {
	for _, in := range []string{"remove", "remove everything"} {
		ctx := tappableCtx(t)
		runTappable(t, ctx, "zubat xlcandy:pikachu stardust:500 rare_candy")
		require.Len(t, tappableRows(t, ctx), 4)
		r := runTappable(t, ctx, in)
		require.Equal(t, "✅", r[0].React, r[0].Text)
		assert.Empty(t, tappableRows(t, ctx), in)
	}
}

func TestTappable_RemoveByID(t *testing.T) {
	ctx := tappableCtx(t)
	runTappable(t, ctx, "zubat pikachu")
	rows := tappableRows(t, ctx)
	require.Len(t, rows, 2)

	r := runTappable(t, ctx, "remove id:"+itoa64(rows[0].UID))
	require.Equal(t, "✅", r[0].React, r[0].Text)
	left := tappableRows(t, ctx)
	require.Len(t, left, 1)
	assert.Equal(t, rows[1].UID, left[0].UID)
}

func TestTappable_InvalidTemplate_NonAdmin(t *testing.T) {
	ctx := tappableCtx(t)
	ctx.IsAdmin = false
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/dts.json", []byte("[]"), 0o644))
	ts, err := dts.LoadTemplates(dir, dir)
	require.NoError(t, err)
	ctx.DTS = ts

	r := runTappable(t, ctx, "zubat template:99")
	assert.Equal(t, "🙅", r[0].React)
	assert.Contains(t, r[0].Text, "99")
	assert.Empty(t, tappableRows(t, ctx))
}

func TestTappable_EverythingGated(t *testing.T) {
	for _, in := range []string{"everything", "all_items"} {
		ctx := tappableCtx(t)
		ctx.Config.Tracking.EverythingFlagPermissions = "deny"
		ctx.IsAdmin = false
		r := runTappable(t, ctx, in)
		assert.Equal(t, "🙅", r[0].React, in)
		assert.Empty(t, tappableRows(t, ctx))

		ctx.IsAdmin = true
		r = runTappable(t, ctx, in)
		assert.Equal(t, "✅", r[0].React, "%s as admin: %s", in, r[0].Text)
	}
}

func TestTappable_Disabled(t *testing.T) {
	ctx := tappableCtx(t)
	ctx.Config.General.DisableTappable = true
	r := runTappable(t, ctx, "zubat")
	assert.Equal(t, "🙅", r[0].React)
	assert.Empty(t, tappableRows(t, ctx))
}

func TestTappable_Usage(t *testing.T) {
	ctx := tappableCtx(t)
	r := (&TappableCommand{}).Run(ctx, nil)
	require.NotEmpty(t, r)
	assert.Contains(t, r[0].Text, "tappable zubat")
}

func TestTappable_MuteAlias(t *testing.T) {
	ctx := tappableCtx(t)
	ms := mute.NewStore()
	ctx.MuteStore = ms
	reg := bot.NewRegistry()
	reg.Register(&MuteCommand{})
	reg.Register(&UnmuteCommand{})
	ctx.Registry = reg

	runTappable(t, ctx, "zubat")
	uid := tappableRows(t, ctx)[0].UID

	r := runTappable(t, ctx, "mute id:"+itoa64(uid))
	require.NotEmpty(t, r)
	require.Equal(t, "🔇", r[0].React, r[0].Text)
	list := ms.List("user1")
	require.Len(t, list, 1)
	assert.Equal(t, mute.ScopeTracking, list[0].ScopeType)
	assert.Equal(t, "tappable", list[0].RuleTable)
	assert.Equal(t, itoa64(uid), list[0].ScopeValue)
}

func TestTappable_UntrackReroute(t *testing.T) {
	ctx := tappableCtx(t)
	reg := bot.NewRegistry()
	reg.Register(&TappableCommand{})
	ctx.Registry = reg
	runTappable(t, ctx, "zubat")
	r := (&UntrackCommand{}).Run(ctx, []string{"tappable", "zubat"})
	require.Equal(t, "✅", r[0].React, r[0].Text)
	assert.Empty(t, tappableRows(t, ctx))
}

func TestTappable_ScriptRoundTrip(t *testing.T) {
	ctx := scriptCtx(t)
	ctx.GameData.Items = map[int]*gamedata.Item{1301: {ItemID: 1301}}
	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25": "Pikachu", "item_1301": "Rare Candy",
	}))
	ctx.Resolver = bot.NewPokemonResolver(ctx.GameData, ctx.Translations, []string{"en"}, nil)
	ctx.ArgMatcher = bot.NewArgMatcher(ctx.Translations, ctx.GameData, ctx.Resolver, []string{"en"})

	for _, in := range []string{
		"candy:pikachu amount:3 d:500 clean",
		"xlcandy template:2",
		"energy:pikachu",
		"stardust:500",
		"pokecoins",
		"rare_candy amount:2",
		"all_items",
	} {
		r := runTappable(t, ctx, in)
		require.Equal(t, "✅", r[0].React, "%s: %s", in, r[0].Text)
	}
	orig := tappableRows(t, ctx)
	require.Len(t, orig, 7)

	lines := runScriptLines(t, ctx)
	require.Len(t, lines, 7, "script: %v", lines)
	assert.Contains(t, lines, "!tappable candy:pikachu amount:3 d:500 clean")
	assert.Contains(t, lines, "!tappable rare_candy amount:2")

	fresh := scriptCtx(t)
	fresh.GameData = ctx.GameData
	fresh.Translations = ctx.Translations
	fresh.Resolver = ctx.Resolver
	fresh.ArgMatcher = ctx.ArgMatcher
	for _, line := range lines {
		r := runTappable(t, fresh, strings.TrimPrefix(line, "!tappable "))
		require.Equal(t, "✅", r[0].React, "line %q: %s", line, r[0].Text)
	}
	strip := func(rows []db.TappableTrackingAPI) []db.TappableTrackingAPI {
		for i := range rows {
			rows[i].UID = 0
		}
		return rows
	}
	assert.ElementsMatch(t, strip(orig), strip(tappableRows(t, fresh)))
}

func TestTappable_ScriptLines(t *testing.T) {
	ctx := scriptCtx(t)
	c := &ScriptCommand{}
	assert.Equal(t, "!tappable xlcandy amount:2 d:100 clean location:home_base",
		c.tappableToScript(ctx, "!", &db.TappableTrackingAPI{
			RewardType: 9, Amount: 2, Distance: 100, Clean: 1, OverrideLocationLabel: "home base",
		}))
	assert.Equal(t, "!tappable stardust:500 area:city_centre,park",
		c.tappableToScript(ctx, "!", &db.TappableTrackingAPI{
			RewardType: 3, Reward: 500, OverrideAreas: []string{"city centre", "park"},
		}))
	assert.Equal(t, "!tappable all_items", c.tappableToScript(ctx, "!", &db.TappableTrackingAPI{RewardType: 2}))
}

func TestTappable_Tracked(t *testing.T) {
	ctx := scriptCtx(t)
	require.Equal(t, "✅", runTappable(t, ctx, "xlcandy")[0].React)
	r := (&TrackedCommand{}).Run(ctx, nil)
	require.NotEmpty(t, r)
	text := r[0].Text
	assert.Contains(t, text, "**Tappables:**")
	assert.Contains(t, text, "XL candy")

	ctx.Config.General.DisableTappable = true
	text = (&TrackedCommand{}).Run(ctx, nil)[0].Text
	assert.NotContains(t, text, "Tappables")
	assert.NotContains(t, text, "tappables")
}

// The same rule created through the bot command and through v2 POST must be
// stored identically (same sentinels, clean bits, template, amount).
func TestTappable_BotAndV2StoreIdenticalRows(t *testing.T) {
	ctx := tappableCtx(t)
	r := runTappable(t, ctx, "candy:pikachu amount:3 d:500 clean")
	require.Equal(t, "✅", r[0].React, r[0].Text)
	botRows := tappableRows(t, ctx)
	require.Len(t, botRows, 1)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	humaAPI := api.NewHumaAPI(engine, engine.Group("/api"), "test")
	v2Store := store.NewMockTrackingStore[db.TappableTrackingAPI](store.TappableGetUID, store.TappableSetUID)
	api.RegisterV2TrackingTappable(humaAPI, &api.TrackingDeps{
		Humans:       ctx.Humans,
		Tracking:     &store.TrackingStores{Tappables: v2Store},
		Config:       &config.Config{},
		RowText:      ctx.RowText,
		Translations: ctx.Translations,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/humans/user1/tracking/tappable?silent=true",
		strings.NewReader(`[{"reward_type":4,"reward":25,"amount":3,"distance":500,"clean":true}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	v2Rows, err := v2Store.SelectByIDProfile("user1", 1)
	require.NoError(t, err)
	require.Len(t, v2Rows, 1)

	b, v := botRows[0], v2Rows[0]
	b.UID, v.UID = 0, 0
	if len(b.OverrideAreas) == 0 {
		b.OverrideAreas = nil
	}
	if len(v.OverrideAreas) == 0 {
		v.OverrideAreas = nil
	}
	assert.Equal(t, b, v)
}

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

// A kind:<name> that resolves to no pokemon must not widen to "any pokemon":
// on add that would track every candy, on remove it would delete every candy rule.
func TestTappable_UnresolvedPokemonIsUnrecognized(t *testing.T) {
	ctx := tappableCtx(t)
	for _, in := range []string{"candy:pikchu", "xlcandy:pikchu", "energy:pikchu", "candy:pikchu rare_candy"} {
		r := runTappable(t, ctx, in)
		require.Equal(t, "🙅", r[0].React, in)
		assert.Contains(t, r[0].Text, "pikchu", in)
	}
	assert.Empty(t, tappableRows(t, ctx))

	runTappable(t, ctx, "candy zubat pikachu")
	require.Len(t, tappableRows(t, ctx), 2)
	r := runTappable(t, ctx, "remove candy:zubbat")
	require.Equal(t, "🙅", r[0].React, r[0].Text)
	assert.Len(t, tappableRows(t, ctx), 2, "nothing removed")
}
