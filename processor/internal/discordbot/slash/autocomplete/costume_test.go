package autocomplete

import (
	"context"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// costumeTestDeps wires a minimal BotDeps with three costumes: 0 ("Unset",
// a real selectable value unlike form's placeholder 0), 1 ("Holiday
// 2016"), and 2 ("Anniversary").
func costumeTestDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"costume_0": "Unset",
		"costume_1": "Holiday 2016",
		"costume_2": "Anniversary",
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Costumes: map[int]gamedata.CostumeInfo{
			0: {ID: 0, Name: "Unset"},
			1: {ID: 1, Name: "Holiday 2016"},
			2: {ID: 2, Name: "Anniversary"},
		},
	}
	return &bot.BotDeps{Translations: bundle, GameData: gd, Cfg: &config.Config{}}
}

func TestCostume_NoFocusedListsAll(t *testing.T) {
	deps := costumeTestDeps(t)
	out := Costume(context.Background(), deps, "", "en")
	if len(out) != 3 {
		t.Fatalf("expected 3 choices, got %d (%+v)", len(out), out)
	}
}

// Costume ID 0 ("Unset") is a real, meaningful filter value — unlike
// Form's ID 0 placeholder, it must NOT be filtered out of the picker.
func TestCostume_IncludesIDZero(t *testing.T) {
	deps := costumeTestDeps(t)
	out := Costume(context.Background(), deps, "", "en")
	found := false
	for _, c := range out {
		if c.Value == "0" {
			found = true
			if c.Name != "Unset" {
				t.Errorf("expected 'Unset' label for id 0, got %q", c.Name)
			}
		}
	}
	if !found {
		t.Error("expected costume id 0 to be present in choices")
	}
}

func TestCostume_ValueIsNumericID(t *testing.T) {
	deps := costumeTestDeps(t)
	out := Costume(context.Background(), deps, "", "en")
	for _, c := range out {
		if c.Name == "Holiday 2016" {
			if c.Value != "1" {
				t.Errorf("Holiday 2016 value=%v, want \"1\"", c.Value)
			}
			return
		}
	}
	t.Error("Holiday 2016 entry not found")
}

func TestCostume_FiltersBySubstring(t *testing.T) {
	deps := costumeTestDeps(t)
	out := Costume(context.Background(), deps, "holi", "en")
	if len(out) != 1 || out[0].Name != "Holiday 2016" {
		t.Errorf("expected only 'Holiday 2016' for 'holi' filter, got %+v", out)
	}
}

func TestCostume_NilDepsReturnsNil(t *testing.T) {
	if out := Costume(context.Background(), nil, "", "en"); out != nil {
		t.Errorf("expected nil for nil deps, got %+v", out)
	}
}

func TestCostume_SortedByLabel(t *testing.T) {
	deps := costumeTestDeps(t)
	out := Costume(context.Background(), deps, "", "en")
	for i := 1; i < len(out); i++ {
		if out[i-1].Name > out[i].Name {
			t.Errorf("choices not sorted: %q before %q", out[i-1].Name, out[i].Name)
		}
	}
}
