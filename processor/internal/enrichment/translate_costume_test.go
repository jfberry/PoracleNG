package enrichment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// newCostumeTestBundle mirrors newTestBundleWithTranslations (translate_test.go)
// but adds a costume_1 key so we can assert costume weaving into fullName.
func newCostumeTestBundle(t *testing.T) *i18n.Bundle {
	t.Helper()

	dir := t.TempDir()

	enJSON := []byte(`{
		"poke_25": "Pikachu",
		"form_0": "Normal",
		"costume_1": "Holiday 2016"
	}`)

	if err := os.WriteFile(filepath.Join(dir, "en.json"), enJSON, 0644); err != nil {
		t.Fatal(err)
	}

	b := i18n.NewBundle()
	if err := b.LoadJSONDir(dir); err != nil {
		t.Fatal(err)
	}
	return b
}

func newCostumeTestGameData() *gamedata.GameData {
	return &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0, Types: []int{13}},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
		Util:  &gamedata.UtilData{},
	}
}

// --- buildFullName costume tests ---

func TestBuildFullName_WithCostume(t *testing.T) {
	bundle := newCostumeTestBundle(t)
	gd := newCostumeTestGameData()
	tr := bundle.For("en")

	nameKeys := gd.MonsterNameKeys(25, 0, 0)

	got := buildFullName(tr, nameKeys, "Pikachu", "", 25, 0, 1)
	want := "Pikachu (Holiday 2016)"
	if got != want {
		t.Errorf("buildFullName(costume=1) = %q, want %q", got, want)
	}
}

func TestBuildFullName_NoCostumeUnchanged(t *testing.T) {
	bundle := newCostumeTestBundle(t)
	gd := newCostumeTestGameData()
	tr := bundle.For("en")

	nameKeys := gd.MonsterNameKeys(25, 0, 0)

	got := buildFullName(tr, nameKeys, "Pikachu", "", 25, 0, 0)
	want := "Pikachu"
	if got != want {
		t.Errorf("buildFullName(costume=0) = %q, want %q", got, want)
	}
}

// --- TranslateMonsterNamesEng costume tests (public wrapper) ---

func TestTranslateMonsterNamesEng_WithCostume(t *testing.T) {
	bundle := newCostumeTestBundle(t)
	gd := newCostumeTestGameData()
	tr := bundle.For("en")

	m := make(map[string]any)
	TranslateMonsterNamesEng(m, gd, tr, bundle, 25, 0, 0, 1)

	if got, want := m["fullName"], "Pikachu (Holiday 2016)"; got != want {
		t.Errorf("fullName = %q, want %q", got, want)
	}
	if got, want := m["fullNameEng"], "Pikachu (Holiday 2016)"; got != want {
		t.Errorf("fullNameEng = %q, want %q", got, want)
	}
}

func TestTranslateMonsterNamesEng_NoCostumeUnchanged(t *testing.T) {
	bundle := newCostumeTestBundle(t)
	gd := newCostumeTestGameData()
	tr := bundle.For("en")

	m := make(map[string]any)
	TranslateMonsterNamesEng(m, gd, tr, bundle, 25, 0, 0, 0)

	if got, want := m["fullName"], "Pikachu"; got != want {
		t.Errorf("fullName = %q, want %q", got, want)
	}
	if got, want := m["fullNameEng"], "Pikachu"; got != want {
		t.Errorf("fullNameEng = %q, want %q", got, want)
	}
}

// --- costumeName field (Enricher.PokemonTranslate spawn site) ---

func TestPokemonTranslate_CostumeName(t *testing.T) {
	bundle := newCostumeTestBundle(t)
	gd := newCostumeTestGameData()

	e := &Enricher{GameData: gd, Translations: bundle}

	pokemon := &webhook.PokemonWebhook{
		PokemonID: 25,
		Form:      0,
		Costume:   1,
	}

	base := map[string]any{"encountered": false}
	m := e.PokemonTranslate(base, pokemon, "en")

	if got, want := m["costumeName"], "Holiday 2016"; got != want {
		t.Errorf("costumeName = %q, want %q", got, want)
	}
	if got, want := m["fullName"], "Pikachu (Holiday 2016)"; got != want {
		t.Errorf("fullName = %q, want %q", got, want)
	}
}

func TestPokemonTranslate_NoCostumeNameEmpty(t *testing.T) {
	bundle := newCostumeTestBundle(t)
	gd := newCostumeTestGameData()

	e := &Enricher{GameData: gd, Translations: bundle}

	pokemon := &webhook.PokemonWebhook{
		PokemonID: 25,
		Form:      0,
		Costume:   0,
	}

	base := map[string]any{"encountered": false}
	m := e.PokemonTranslate(base, pokemon, "en")

	if got, want := m["costumeName"], ""; got != want {
		t.Errorf("costumeName = %q, want empty", got)
	}
	if got, want := m["fullName"], "Pikachu"; got != want {
		t.Errorf("fullName = %q, want %q", got, want)
	}
}
