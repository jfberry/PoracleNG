package dts

import (
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// BuildOriginalView returns the field map exposed under {{original.X}}
// in monsterChanged templates. Carries the prior sighting's identity
// and battle-stat fields plus translated names when a translator is
// available.
//
// gd and tr may be nil — name fields are skipped in that case so the
// builder is safe to use in tests without full game data setup.
func BuildOriginalView(prior tracker.EncounterState, gd *gamedata.GameData, tr *i18n.Translator) map[string]any {
	encountered := prior.CP > 0
	out := map[string]any{
		"pokemonId":   prior.PokemonID,
		"formId":      prior.Form,
		"gender":      prior.Gender,
		"weatherId":   prior.Weather,
		"cp":          prior.CP,
		"atk":         prior.ATK,
		"def":         prior.DEF,
		"sta":         prior.STA,
		"encountered": encountered,
	}
	if encountered {
		out["iv"] = float64(prior.ATK+prior.DEF+prior.STA) * 100.0 / 45.0
	} else {
		out["iv"] = 0.0
	}

	if tr != nil {
		nameKey := gamedata.PokemonTranslationKey(prior.PokemonID)
		name := tr.T(nameKey)
		out["name"] = name

		var formName string
		if prior.Form != 0 {
			formKey := gamedata.FormTranslationKey(prior.Form)
			translated := tr.T(formKey)
			// If translator returned the raw key, treat as missing (no form name).
			if translated != formKey {
				formName = translated
			}
		}
		out["formName"] = formName

		weatherKey := gamedata.WeatherTranslationKey(prior.Weather)
		weatherName := tr.T(weatherKey)
		if weatherName == weatherKey {
			weatherName = ""
		}
		out["weatherName"] = weatherName

		// fullName: "Name (FormName)" when a form name is present, else "Name".
		if formName != "" {
			out["fullName"] = name + " (" + formName + ")"
		} else {
			out["fullName"] = name
		}
	}
	return out
}
