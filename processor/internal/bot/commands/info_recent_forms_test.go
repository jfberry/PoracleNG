package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// infoFormCtx mirrors infoCostumeCtx (info_costume_test.go) but wires a named
// form (680 → "Winter 2023") so !info pikachu can exercise the recent-forms
// section.
func infoFormCtx(t *testing.T) *bot.CommandContext {
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
	}

	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25":  "Pikachu",
		"form_680": "Winter 2023",
	}))

	ctx.Resolver = bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.GameData = gd
	ctx.RecentActivity = tracker.NewRecentActivity()

	return ctx
}

func TestInfo_Pokemon_RecentlySeenForms(t *testing.T) {
	ctx := infoFormCtx(t)
	ctx.RecentActivity.RecordForm(25, 680)

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"pikachu"})
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !strings.Contains(text, "form:winter_2023") {
		t.Errorf("expected copy-pasteable 'form:<name>' recent form line, got: %q", text)
	}
	if !strings.Contains(text, "Recently-seen forms") {
		t.Errorf("expected a recently-seen forms header, got: %q", text)
	}
}

// TestInfo_Pokemon_SectionOrder locks the approved layout: the recency
// sections (recent forms → recent costumes) render BEFORE the full
// "Available forms" list, so what's spawning now is surfaced first.
func TestInfo_Pokemon_SectionOrder(t *testing.T) {
	ctx := infoFormCtx(t)
	ctx.RecentActivity.RecordForm(25, 680)

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"pikachu"})
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	recentIdx := strings.Index(text, "Recently-seen forms")
	availIdx := strings.Index(text, "Available forms")
	if recentIdx == -1 || availIdx == -1 {
		t.Fatalf("expected both recent-forms and available-forms sections present, got: %q", text)
	}
	if recentIdx > availIdx {
		t.Errorf("recent forms must render before available forms; recentIdx=%d availIdx=%d in: %q", recentIdx, availIdx, text)
	}
}

func TestInfo_Pokemon_NoRecentForms_SectionOmitted(t *testing.T) {
	ctx := infoFormCtx(t)

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"pikachu"})
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if strings.Contains(replies[0].Text, "Recently-seen forms") {
		t.Errorf("expected no recent-forms section when none recorded, got: %q", replies[0].Text)
	}
}
