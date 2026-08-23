package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// infoFormCostumeCtx combines infoFormCtx (info_recent_forms_test.go) and
// infoCostumeCtx (info_costume_test.go): a pikachu monster with a named form
// (680 → "Winter 2023") and two named costumes (1 → "Holiday 2016", 8 →
// "Party Hat"), so a single !info pikachu run can exercise recent forms,
// recent raid forms, and both costume sections together. The second costume
// lets tests prove spawn-costume and raid-costume buckets combine (id 1 vs
// id 8 resolve to distinct names).
func infoFormCostumeCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}:   {PokemonID: 25, FormID: 0},
			{ID: 25, Form: 680}: {PokemonID: 25, FormID: 680},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
		Util:  &gamedata.UtilData{},
		Costumes: map[int]gamedata.CostumeInfo{
			1: {ID: 1, Name: "Holiday 2016"},
			8: {ID: 8, Name: "Party Hat"},
		},
	}

	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25":   "Pikachu",
		"form_680":  "Winter 2023",
		"costume_1": "Holiday 2016",
		"costume_8": "Party Hat",
	}))

	ctx.Resolver = bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.GameData = gd
	ctx.RecentActivity = tracker.NewRecentActivity()

	return ctx
}

func TestInfo_Pokemon_RecentRaidForms(t *testing.T) {
	ctx := infoFormCostumeCtx(t) // helper priming forms + costumes + RecentActivity
	ctx.RecentActivity.RecordRaidForm(25, 680)
	text := (&InfoCommand{}).Run(ctx, []string{"pikachu"})[0].Text
	if !strings.Contains(text, "Recently-seen raid forms") || !strings.Contains(text, "form:winter_2023") {
		t.Errorf("expected copy-pasteable recent raid forms section, got: %q", text)
	}
}

func TestInfo_Pokemon_CostumesCopyPasteable(t *testing.T) {
	ctx := infoFormCostumeCtx(t)
	ctx.RecentActivity.RecordCostume(25, 1)     // spawn costume
	ctx.RecentActivity.RecordRaidCostume(25, 1) // raid costume
	text := (&InfoCommand{}).Run(ctx, []string{"pikachu"})[0].Text
	if !strings.Contains(text, "costume:holiday_2016") {
		t.Errorf("costume sections must be copy-pasteable 'costume:<name>', got: %q", text)
	}
	if strings.Contains(text, "1 — Holiday 2016") {
		t.Errorf("costume sections must NOT use the old 'id — name' format, got: %q", text)
	}
}
