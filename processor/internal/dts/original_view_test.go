package dts

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

func TestBuildOriginalView_NotEncountered(t *testing.T) {
	prior := tracker.EncounterState{PokemonID: 25, Form: 0, CP: 0, Gender: 1}
	v := BuildOriginalView(prior, nil, nil)
	if v["pokemonId"] != 25 {
		t.Errorf("pokemonId = %v, want 25", v["pokemonId"])
	}
	if v["encountered"] != false {
		t.Errorf("encountered = %v, want false", v["encountered"])
	}
	if v["cp"] != 0 {
		t.Errorf("cp = %v, want 0", v["cp"])
	}
	if v["gender"] != 1 {
		t.Errorf("gender = %v, want 1", v["gender"])
	}
}

func TestBuildOriginalView_Encountered(t *testing.T) {
	prior := tracker.EncounterState{
		PokemonID: 25, Form: 0, CP: 1500,
		ATK: 15, DEF: 14, STA: 13, Gender: 1,
	}
	v := BuildOriginalView(prior, nil, nil)
	if v["encountered"] != true {
		t.Error("encountered should be true once CP > 0")
	}
	wantIV := float64(15+14+13) * 100.0 / 45.0
	if v["iv"].(float64) != wantIV {
		t.Errorf("iv = %v, want %v", v["iv"], wantIV)
	}
	if v["cp"] != 1500 {
		t.Errorf("cp = %v, want 1500", v["cp"])
	}
}

func TestBuildOriginalView_NameResolution(t *testing.T) {
	tr := i18n.NewTranslator("en", map[string]string{
		"poke_25": "Pikachu",
	})

	prior := tracker.EncounterState{PokemonID: 25, Form: 0, CP: 1500, ATK: 15, DEF: 15, STA: 15}
	v := BuildOriginalView(prior, nil, tr)
	if v["name"] != "Pikachu" {
		t.Errorf("name = %v, want Pikachu", v["name"])
	}
	if v["fullName"] != "Pikachu" {
		t.Errorf("fullName = %v, want Pikachu", v["fullName"])
	}
	if v["formName"] != "" {
		t.Errorf("formName = %v, want empty (form 0)", v["formName"])
	}
}

func TestBuildOriginalView_NameResolution_WithForm(t *testing.T) {
	tr := i18n.NewTranslator("en", map[string]string{
		"poke_103": "Exeggutor",
		"form_46":  "Alolan",
	})

	prior := tracker.EncounterState{PokemonID: 103, Form: 46, CP: 1500, ATK: 15, DEF: 15, STA: 15}
	v := BuildOriginalView(prior, nil, tr)
	if v["name"] != "Exeggutor" {
		t.Errorf("name = %v, want Exeggutor", v["name"])
	}
	if v["formName"] != "Alolan" {
		t.Errorf("formName = %v, want Alolan", v["formName"])
	}
	if v["fullName"] != "Exeggutor (Alolan)" {
		t.Errorf("fullName = %v, want \"Exeggutor (Alolan)\"", v["fullName"])
	}
}
