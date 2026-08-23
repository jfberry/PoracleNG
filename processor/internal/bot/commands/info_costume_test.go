package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// infoCostumeCtx builds a CommandContext wired with GameData.Costumes, a
// pikachu monster entry, a pokemon resolver, and a RecentActivity tracker —
// everything !info costumes and !info <pokemon> need to exercise the
// costume-related sections. Mirrors costumeCtx (track_costume_test.go).
func infoCostumeCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0},
		},
		Moves:    map[int]*gamedata.Move{},
		Types:    map[int]*gamedata.TypeInfo{},
		Util:     &gamedata.UtilData{},
		Costumes: map[int]gamedata.CostumeInfo{1: {ID: 1, Name: "Holiday 2016"}},
	}

	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25":   "Pikachu",
		"costume_1": "Holiday 2016",
	}))

	ctx.Resolver = bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.GameData = gd
	ctx.RecentActivity = tracker.NewRecentActivity()

	return ctx
}

// TestInfo_Costumes_ListsAll verifies `!info costumes` lists every known
// costume from GameData.Costumes, using the translated costume_{id} name.
func TestInfo_Costumes_ListsAll(t *testing.T) {
	ctx := infoCostumeCtx(t)

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"costumes"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if !strings.Contains(replies[0].Text, "Holiday 2016") {
		t.Errorf("expected costume list to contain %q, got: %q", "Holiday 2016", replies[0].Text)
	}
	if !strings.Contains(replies[0].Text, "1 — Holiday 2016") {
		t.Errorf("expected costume entry formatted as 'id — name', got: %q", replies[0].Text)
	}
}

// TestInfo_Costumes_SkipsZero verifies costume id 0 (the "no costume"
// wildcard state) is never listed in the global costume list.
func TestInfo_Costumes_SkipsZero(t *testing.T) {
	ctx := infoCostumeCtx(t)
	ctx.GameData.Costumes[0] = gamedata.CostumeInfo{ID: 0, Name: "Unset"}

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"costumes"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if strings.Contains(replies[0].Text, "0 — ") {
		t.Errorf("expected costume id 0 to be omitted from the list, got: %q", replies[0].Text)
	}
}

// TestInfo_Pokemon_RecentlySeenCostumes verifies `!info pikachu` surfaces a
// recently-seen costume section after RecordCostume(25, 1).
func TestInfo_Pokemon_RecentlySeenCostumes(t *testing.T) {
	ctx := infoCostumeCtx(t)
	ctx.RecentActivity.RecordCostume(25, 1)

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"pikachu"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !strings.Contains(text, "costume:holiday_2016") {
		t.Errorf("expected recently-seen costume section to contain copy-pasteable %q, got: %q", "costume:holiday_2016", text)
	}
	if !strings.Contains(text, "Recently-seen costumes") {
		t.Errorf("expected a recently-seen costumes header, got: %q", text)
	}
}

// TestInfo_Pokemon_NoRecentCostumes_SectionOmitted verifies the
// recently-seen costumes section is absent entirely when nothing has been
// recorded for the species.
func TestInfo_Pokemon_NoRecentCostumes_SectionOmitted(t *testing.T) {
	ctx := infoCostumeCtx(t)

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"pikachu"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if strings.Contains(replies[0].Text, "Recently-seen costumes") {
		t.Errorf("expected no recently-seen costumes section when none recorded, got: %q", replies[0].Text)
	}
}

// TestInfo_Pokemon_NilRecentActivity_NoPanic verifies !info <pokemon> still
// works (no recently-seen costumes section, no panic) when RecentActivity
// isn't wired up on the CommandContext.
func TestInfo_Pokemon_NilRecentActivity_NoPanic(t *testing.T) {
	ctx := infoCostumeCtx(t)
	ctx.RecentActivity = nil

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"pikachu"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if strings.Contains(replies[0].Text, "Recently-seen costumes") {
		t.Errorf("expected no recently-seen costumes section with nil RecentActivity, got: %q", replies[0].Text)
	}
}
