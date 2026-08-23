package enrichment

import (
	"encoding/json"
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// ShowcaseFocusTranslate turns a showcase_focus JSON object into a translated
// "what's featured" descriptor. Showcases feature one of 10 focus classes
// (enumerated in util.json showcaseFocus); only pokemon/type also populate the
// flat showcase_pokemon_* fields, so the focus JSON is the authoritative source.
// Returns per-language keys:
//
//	showcaseFocusPresent  (bool)   — a focus was decoded
//	showcaseFocusType     (string) — raw focus class, e.g. "type", "buddy"
//	showcaseFocusCategory (string) — translated category label (i18n showcase_focus_{type})
//	showcaseFocusName     (string) — translated specific value (e.g. "Steel", "3+")
//	showcaseFocusEmoji    (string) — optional emoji key from util.json
func (e *Enricher) ShowcaseFocusTranslate(focusRaw json.RawMessage, lang string) map[string]any {
	out := map[string]any{
		"showcaseFocusPresent":  false,
		"showcaseFocusType":     "",
		"showcaseFocusCategory": "",
		"showcaseFocusName":     "",
		"showcaseFocusEmoji":    "",
	}
	if len(focusRaw) == 0 {
		return out
	}
	var focus map[string]any
	if err := json.Unmarshal(focusRaw, &focus); err != nil {
		return out
	}
	ft, _ := focus["type"].(string)
	if ft == "" {
		return out
	}

	tr := e.Translations.For(lang)
	if tr == nil {
		tr = e.Translations.For("en")
	}

	out["showcaseFocusPresent"] = true
	out["showcaseFocusType"] = ft
	out["showcaseFocusCategory"] = tr.T("showcase_focus_" + ft)
	if e.GameData != nil && e.GameData.Util != nil {
		if info, ok := e.GameData.Util.ShowcaseFocus[ft]; ok {
			out["showcaseFocusEmoji"] = info.Emoji
		}
	}
	out["showcaseFocusName"] = e.showcaseFocusValue(ft, focus, tr)
	return out
}

// showcaseFocusValue resolves the specific featured value for a focus class.
// Enum values are taken from the game proto (see Golbat decoder/pokestop_showcase.go)
// and mapped to PoracleNG labels — deliberately NOT assuming the proto enum lines
// up with a gamelocale key, because for alignment and generation it does not.
func (e *Enricher) showcaseFocusValue(ft string, focus map[string]any, tr *i18n.Translator) string {
	switch ft {
	case "pokemon":
		name := tr.T(gamedata.PokemonTranslationKey(toInt(focus["pokemon_id"])))
		if form := toInt(focus["pokemon_form"]); form > 0 {
			name = tr.T(gamedata.FormTranslationKey(form)) + " " + name
		}
		return name
	case "type":
		name := tr.T(gamedata.TypeTranslationKey(toInt(focus["pokemon_type_1"])))
		if t2 := toInt(focus["pokemon_type_2"]); t2 > 0 {
			name += "/" + tr.T(gamedata.TypeTranslationKey(t2))
		}
		return name
	case "family":
		// The PokemonFamily enum matches the base species' Pokédex id in PoGO.
		return tr.T(gamedata.PokemonTranslationKey(toInt(focus["pokemon_family"])))
	case "buddy":
		return fmt.Sprintf("%d+", toInt(focus["min_level"]))
	case "class":
		// HoloPokemonClass: 0 normal, 1 legendary, 2 mythic, 3 ultra beast.
		switch toInt(focus["pokemon_class"]) {
		case 1:
			return tr.T("legendary")
		case 2:
			return tr.T("mythical")
		case 3:
			return tr.T("showcase_class_ultra_beast")
		}
		return ""
	case "alignment":
		// ContestPokemonAlignmentFocusProto: 0 unset, 1 PURIFIED, 2 SHADOW — this
		// is REVERSED vs the gamelocale alignment_{id} keys (alignment_1=Shadow,
		// alignment_2=Purified), so map explicitly rather than by index.
		switch toInt(focus["pokemon_alignment"]) {
		case 1:
			return tr.T("alignment_2") // Purified
		case 2:
			return tr.T("alignment_1") // Shadow
		}
		return ""
	case "generation":
		if genNum := showcaseGenerationNum(toInt(focus["generation"])); genNum > 0 {
			return tr.T(fmt.Sprintf("generation_%d", genNum))
		}
		return ""
	}
	// hatched, shiny, mega — the category label conveys it; no extra value.
	return ""
}

// showcaseGenerationNum maps the proto PokedexGenerationId to the gen number used
// by the gamelocale generation_{N} labels (1..9 = Kanto..Paldea). The proto
// (GEN1=1..GEN8=8, GEN8A=9 Hisui, GEN9=10 Paldea, MELTAN=1002) diverges, so map
// explicitly and return 0 for values without a mainline label (Hisui, Meltan).
func showcaseGenerationNum(protoGen int) int {
	switch {
	case protoGen >= 1 && protoGen <= 8:
		return protoGen
	case protoGen == 10: // GEN9 = Paldea = generation_9
		return 9
	default:
		return 0
	}
}
