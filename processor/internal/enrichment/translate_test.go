package enrichment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// newTestBundle creates an empty Bundle (for tests that only need non-nil Translations).
func newTestBundle() *i18n.Bundle {
	return i18n.NewBundle()
}

// newTestBundleWithTranslations creates a Bundle with English and German translations.
func newTestBundleWithTranslations(t *testing.T) *i18n.Bundle {
	t.Helper()

	dir := t.TempDir()

	enJSON := []byte(`{
		"poke_25": "Pikachu",
		"poke_6": "Charizard",
		"form_0": "Normal",
		"form_65": "Alolan",
		"move_14": "Hyper Beam",
		"weather_1": "Clear",
		"weather_3": "Partly Cloudy",
		"poke_type_4": "Poison",
		"poke_type_12": "Grass",
		"item_1": "Poke Ball"
	}`)

	deJSON := []byte(`{
		"poke_25": "Pikachu",
		"poke_6": "Glurak",
		"form_0": "Normal",
		"form_65": "Alola",
		"move_14": "Hyperstrahl",
		"weather_1": "Sonnig",
		"weather_3": "Teilweise Bewölkt",
		"poke_type_4": "Gift",
		"poke_type_12": "Pflanze",
		"item_1": "Pokeball"
	}`)

	if err := os.WriteFile(filepath.Join(dir, "en.json"), enJSON, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "de.json"), deJSON, 0644); err != nil {
		t.Fatal(err)
	}

	b := i18n.NewBundle()
	if err := b.LoadJSONDir(dir); err != nil {
		t.Fatal(err)
	}
	return b
}

func newTestGameData() *gamedata.GameData {
	return &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0, Types: []int{4}},
			{ID: 6, Form: 0}:  {PokemonID: 6, FormID: 0, Types: []int{4, 12}},
			{ID: 6, Form: 65}: {PokemonID: 6, FormID: 65, Types: []int{4, 12}},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{
			MegaName: map[int]string{
				1: "Mega {0}",
			},
		},
	}
}

// --- TranslateMonsterNamesEng tests ---

func TestTranslateMonsterNamesEng_BasicPokemon(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)
	gd := newTestGameData()
	tr := bundle.For("de")

	m := make(map[string]any)
	TranslateMonsterNamesEng(m, gd, tr, bundle, 6, 0, 0)

	// Translated name should be German
	name, ok := m["name"].(string)
	if !ok || name != "Glurak" {
		t.Errorf("name = %q, want %q", name, "Glurak")
	}

	// English name should be set from the bundle
	nameEng, ok := m["nameEng"].(string)
	if !ok || nameEng != "Charizard" {
		t.Errorf("nameEng = %q, want %q", nameEng, "Charizard")
	}

	// fullName for form 0 (Normal) should be just the name
	fullName, ok := m["fullName"].(string)
	if !ok || fullName != "Glurak" {
		t.Errorf("fullName = %q, want %q", fullName, "Glurak")
	}

	fullNameEng, ok := m["fullNameEng"].(string)
	if !ok || fullNameEng != "Charizard" {
		t.Errorf("fullNameEng = %q, want %q", fullNameEng, "Charizard")
	}
}

func TestTranslateMonsterNamesEng_WithForm(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)
	gd := newTestGameData()
	tr := bundle.For("de")

	m := make(map[string]any)
	TranslateMonsterNamesEng(m, gd, tr, bundle, 6, 65, 0)

	name := m["name"].(string)
	if name != "Glurak" {
		t.Errorf("name = %q, want %q", name, "Glurak")
	}

	formName := m["formName"].(string)
	if formName != "Alola" {
		t.Errorf("formName = %q, want %q", formName, "Alola")
	}

	formNormalised := m["formNormalised"].(string)
	if formNormalised != "Alola" {
		t.Errorf("formNormalised = %q, want %q (should not be empty since it is not Normal)", formNormalised, "Alola")
	}

	fullName := m["fullName"].(string)
	if fullName != "Glurak Alola" {
		t.Errorf("fullName = %q, want %q", fullName, "Glurak Alola")
	}

	fullNameEng := m["fullNameEng"].(string)
	if fullNameEng != "Charizard Alolan" {
		t.Errorf("fullNameEng = %q, want %q", fullNameEng, "Charizard Alolan")
	}
}

func TestTranslateMonsterNamesEng_NormalFormSuppressed(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)
	gd := newTestGameData()
	tr := bundle.For("en")

	m := make(map[string]any)
	// form_0 translates to "Normal", which should be suppressed as the normalised form
	TranslateMonsterNamesEng(m, gd, tr, bundle, 25, 0, 0)

	if m["formNormalised"] != "" {
		t.Errorf("formNormalised = %q, want empty string for Normal form", m["formNormalised"])
	}
}

func TestTranslateMonsterNames_NoBundleNoEngFields(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)
	gd := newTestGameData()
	tr := bundle.For("de")

	m := make(map[string]any)
	// TranslateMonsterNames (without Eng) should not set nameEng/fullNameEng
	TranslateMonsterNames(m, gd, tr, 6, 0, 0)

	if _, ok := m["nameEng"]; ok {
		t.Error("TranslateMonsterNames should not set nameEng (no bundle)")
	}
	if _, ok := m["fullNameEng"]; ok {
		t.Error("TranslateMonsterNames should not set fullNameEng (no bundle)")
	}

	// But name should still be set
	if m["name"] != "Glurak" {
		t.Errorf("name = %q, want %q", m["name"], "Glurak")
	}
}

// --- TranslateTypeNames tests ---

func TestTranslateTypeNames(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)
	tr := bundle.For("de")

	m := make(map[string]any)
	TranslateTypeNames(m, tr, []int{4, 12})

	typeName, ok := m["typeName"].(string)
	if !ok {
		t.Fatal("expected typeName to be set")
	}
	if typeName != "Gift, Pflanze" {
		t.Errorf("typeName = %q, want %q", typeName, "Gift, Pflanze")
	}
}

func TestTranslateTypeNames_English(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)
	tr := bundle.For("en")

	m := make(map[string]any)
	TranslateTypeNames(m, tr, []int{4, 12})

	typeName := m["typeName"].(string)
	if typeName != "Poison, Grass" {
		t.Errorf("typeName = %q, want %q", typeName, "Poison, Grass")
	}
}

// --- TranslateMoveName tests ---

func TestTranslateMoveName(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)

	enTr := bundle.For("en")
	if got := TranslateMoveName(enTr, 14); got != "Hyper Beam" {
		t.Errorf("TranslateMoveName(en, 14) = %q, want %q", got, "Hyper Beam")
	}

	deTr := bundle.For("de")
	if got := TranslateMoveName(deTr, 14); got != "Hyperstrahl" {
		t.Errorf("TranslateMoveName(de, 14) = %q, want %q", got, "Hyperstrahl")
	}
}

func TestTranslateMoveName_ZeroReturnsEmpty(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)
	tr := bundle.For("en")
	if got := TranslateMoveName(tr, 0); got != "" {
		t.Errorf("TranslateMoveName(0) = %q, want empty", got)
	}
}

// --- TranslateWeatherName tests ---

func TestTranslateWeatherName(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)

	enTr := bundle.For("en")
	if got := TranslateWeatherName(enTr, 1); got != "Clear" {
		t.Errorf("TranslateWeatherName(en, 1) = %q, want %q", got, "Clear")
	}

	deTr := bundle.For("de")
	if got := TranslateWeatherName(deTr, 1); got != "Sonnig" {
		t.Errorf("TranslateWeatherName(de, 1) = %q, want %q", got, "Sonnig")
	}
}

func TestTranslateWeatherName_ZeroReturnsEmpty(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)
	tr := bundle.For("en")
	if got := TranslateWeatherName(tr, 0); got != "" {
		t.Errorf("TranslateWeatherName(0) = %q, want empty", got)
	}
}

// --- TranslateItemName tests ---

func TestTranslateItemName(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)

	enTr := bundle.For("en")
	if got := TranslateItemName(enTr, 1); got != "Poke Ball" {
		t.Errorf("TranslateItemName(en, 1) = %q, want %q", got, "Poke Ball")
	}

	deTr := bundle.For("de")
	if got := TranslateItemName(deTr, 1); got != "Pokeball" {
		t.Errorf("TranslateItemName(de, 1) = %q, want %q", got, "Pokeball")
	}
}

func TestTranslateItemName_ZeroReturnsEmpty(t *testing.T) {
	bundle := newTestBundleWithTranslations(t)
	tr := bundle.For("en")
	if got := TranslateItemName(tr, 0); got != "" {
		t.Errorf("TranslateItemName(0) = %q, want empty", got)
	}
}

// --- isNormalForm tests ---

func TestIsNormalForm(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"Normal", true},
		{"normal", true},
		{"NORMAL", true},
		{"Unset", true},
		{"unset", true},
		{"", true},
		{"Alolan", false},
		{"Shadow", false},
		{"Galarian", false},
	}
	for _, tt := range tests {
		got := IsNormalForm(tt.input)
		if got != tt.want {
			t.Errorf("IsNormalForm(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
