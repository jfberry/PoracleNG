package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

func infoRaidCostumeCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0}},
		Moves:    map[int]*gamedata.Move{}, Types: map[int]*gamedata.TypeInfo{}, Util: &gamedata.UtilData{},
		Costumes: map[int]gamedata.CostumeInfo{12: {ID: 12, Name: "Party Hat"}},
	}
	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{"poke_25": "Pikachu", "costume_12": "Party Hat"}))
	ctx.Resolver = bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.GameData = gd
	ctx.RecentActivity = tracker.NewRecentActivity()
	return ctx
}

func TestInfo_Pokemon_RecentRaidCostumes(t *testing.T) {
	ctx := infoRaidCostumeCtx(t)
	ctx.RecentActivity.RecordRaidCostume(25, 12)
	replies := (&InfoCommand{}).Run(ctx, []string{"pikachu"})
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}
	text := replies[0].Text
	if !strings.Contains(text, "costume:party_hat") || !strings.Contains(text, "Recently-seen raid costumes") {
		t.Errorf("expected copy-pasteable recent raid costume section, got: %q", text)
	}
}

func TestInfo_Pokemon_NoRaidCostumes_SectionOmitted(t *testing.T) {
	ctx := infoRaidCostumeCtx(t)
	replies := (&InfoCommand{}).Run(ctx, []string{"pikachu"})
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}
	if strings.Contains(replies[0].Text, "Recently-seen raid costumes") {
		t.Errorf("no raid-costume section when none recorded, got: %q", replies[0].Text)
	}
}
