package gamedata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultForm(t *testing.T) {
	dir := t.TempDir()
	pk := filepath.Join(dir, "pokemon.json")
	fm := filepath.Join(dir, "forms.json")
	if err := os.WriteFile(pk, []byte(`{"25":{"pokedexId":25,"defaultFormId":598,"forms":[598,599]},"1":{"pokedexId":1,"forms":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fm, []byte(`{"598":{"formId":598,"formName":"Normal"},"599":{"formId":599,"formName":"Libre"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	monsters, err := LoadMonsters(pk, fm)
	if err != nil {
		t.Fatal(err)
	}
	gd := &GameData{Monsters: monsters}
	if got := gd.DefaultForm(25); got != 598 {
		t.Fatalf("DefaultForm(25) = %d, want 598", got)
	}
	if got := gd.DefaultForm(1); got != 0 {
		t.Fatalf("no default form = %d, want 0", got)
	}
	if got := gd.DefaultForm(999); got != 0 {
		t.Fatalf("unknown species = %d, want 0", got)
	}
	var nilGD *GameData
	if got := nilGD.DefaultForm(25); got != 0 {
		t.Fatalf("nil GameData = %d, want 0", got)
	}
}
