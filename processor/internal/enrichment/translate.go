package enrichment

import (
	"fmt"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// TranslateMonsterNames adds translated pokemon/form names to the enrichment map.
// Uses pogo-translations identifier keys: poke_{id}, form_{formId}.
// Also sets *Eng fields using the English translator from the bundle.
func TranslateMonsterNames(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, pokemonID, form, evolution int) {
	translateMonsterNamesWithEng(m, gd, tr, nil, pokemonID, form, evolution)
}

// TranslateMonsterNamesEng is like TranslateMonsterNames but also sets *Eng
// fields using the English translator from the bundle.
func TranslateMonsterNamesEng(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, bundle *i18n.Bundle, pokemonID, form, evolution int) {
	translateMonsterNamesWithEng(m, gd, tr, bundle, pokemonID, form, evolution)
}

func translateMonsterNamesWithEng(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, bundle *i18n.Bundle, pokemonID, form, evolution int) {
	nameKeys := gd.MonsterNameKeys(pokemonID, form, evolution)

	// Translated name
	name := tr.T(nameKeys.PokemonKey)
	m["name"] = name

	// Form name
	formName := ""
	formNormalised := ""
	if nameKeys.FormKey != "" {
		formName = tr.T(nameKeys.FormKey)
		if formName != "" && !IsNormalForm(formName) {
			formNormalised = formName
		}
	}
	m["formName"] = formName
	m["formNormalised"] = formNormalised

	// Full name
	fullName := name
	if formNormalised != "" {
		fullName = name + " " + formNormalised
	}
	if evolution > 0 {
		fullName = i18n.Format(nameKeys.MegaNamePattern, fullName)
	}
	m["fullName"] = fullName

	// English names for templates that show both translated + English
	if bundle != nil {
		enTr := bundle.For("en")
		enName := enTr.T(nameKeys.PokemonKey)
		enFormNormalised := ""
		if nameKeys.FormKey != "" {
			enForm := enTr.T(nameKeys.FormKey)
			if enForm != "" && !IsNormalForm(enForm) {
				enFormNormalised = enForm
			}
		}
		enFullName := enName
		if enFormNormalised != "" {
			enFullName = enName + " " + enFormNormalised
		}
		if evolution > 0 {
			enFullName = i18n.Format(nameKeys.MegaNamePattern, enFullName)
		}
		m["nameEng"] = enName
		m["formNormalisedEng"] = enFormNormalised
		m["fullNameEng"] = enFullName
	}
}

// IsNormalForm returns true if a form name is "Normal" in any common language.
// The form_0 key typically translates to "Normal" or equivalent.
func IsNormalForm(name string) bool {
	lower := strings.ToLower(name)
	return lower == "normal" || lower == "unset" || lower == ""
}

// TranslateTypeNames adds translated type names to the enrichment map.
func TranslateTypeNames(m map[string]any, tr *i18n.Translator, typeIDs []int) {
	names := make([]string, 0, len(typeIDs))
	for _, id := range typeIDs {
		names = append(names, tr.T(gamedata.TypeTranslationKey(id)))
	}
	m["typeName"] = strings.Join(names, ", ")
}

// TranslateMoveName returns the translated name for a move ID.
func TranslateMoveName(tr *i18n.Translator, moveID int) string {
	if moveID == 0 {
		return ""
	}
	return tr.T(gamedata.MoveTranslationKey(moveID))
}

// TranslateWeatherName returns the translated name for a weather ID.
func TranslateWeatherName(tr *i18n.Translator, weatherID int) string {
	if weatherID == 0 {
		return ""
	}
	return tr.T(gamedata.WeatherTranslationKey(weatherID))
}

// TranslateItemName returns the translated name for an item ID.
func TranslateItemName(tr *i18n.Translator, itemID int) string {
	if itemID == 0 {
		return ""
	}
	return tr.T(gamedata.ItemTranslationKey(itemID))
}

// TranslateWeaknessCategories translates type names in weakness categories.
func TranslateWeaknessCategories(categories []gamedata.WeaknessCategory, tr *i18n.Translator) []map[string]any {
	result := make([]map[string]any, 0, len(categories))
	for _, cat := range categories {
		translatedTypes := make([]map[string]any, 0, len(cat.TypeIDs))
		for _, typeID := range cat.TypeIDs {
			translatedTypes = append(translatedTypes, map[string]any{
				"typeId": typeID,
				"name":   tr.T(gamedata.TypeTranslationKey(typeID)),
			})
		}
		result = append(result, map[string]any{
			"value":     cat.Multiplier,
			"types":     translatedTypes,
		})
	}
	return result
}

// addGenderFields adds translated gender name and emoji key to the enrichment map.
func addGenderFields(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, gender int) {
	if info, ok := gd.Util.Genders[gender]; ok {
		m["genderName"] = tr.T(info.Name)
		m["genderEmojiKey"] = info.Emoji
	} else {
		m["genderName"] = ""
		m["genderEmojiKey"] = ""
	}
}

// addRarityFields adds translated rarity name to the enrichment map.
func addRarityFields(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, rarityGroup int) {
	if name, ok := gd.Util.Rarity[rarityGroup]; ok {
		m["rarityName"] = tr.T(name)
	} else {
		m["rarityName"] = ""
	}
}

// addSizeFields adds translated size name to the enrichment map.
func addSizeFields(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, size int) {
	if name, ok := gd.Util.Size[size]; ok {
		m["sizeName"] = tr.T(name)
	} else {
		m["sizeName"] = ""
	}
}

// addTeamFields adds translated team name and emoji key to the enrichment map.
func addTeamFields(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, teamID int) {
	if info, ok := gd.Util.Teams[teamID]; ok {
		m["teamName"] = tr.T(info.Name)
		m["teamNameEng"] = info.Name
		m["teamEmojiKey"] = info.Emoji
		m["teamColor"] = info.Color
	} else {
		m["teamName"] = ""
		m["teamNameEng"] = ""
		m["teamEmojiKey"] = ""
		m["teamColor"] = ""
	}
}

// addGenerationFields adds translated generation info to the enrichment map.
func addGenerationFields(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, pokemonID, form int) {
	gen := gd.GetGeneration(pokemonID, form)
	m["generation"] = gen
	info := gd.GetGenerationInfo(gen)
	if info != nil {
		m["generationName"] = tr.T(info.Name)
		m["generationRoman"] = info.Roman
	} else {
		m["generationName"] = fmt.Sprintf("Gen %d", gen)
		m["generationRoman"] = ""
	}
}

// addWeatherFields adds weather-related enrichment fields.
func addWeatherFields(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, typeIDs []int, weatherID int) {
	boosted := gd.IsBoostedByWeather(typeIDs, weatherID)
	m["boosted"] = boosted
	if boosted {
		m["boostWeatherId"] = weatherID
		m["boostWeatherName"] = TranslateWeatherName(tr, weatherID)
		if wInfo, ok := gd.Util.Weather[weatherID]; ok {
			m["boostWeatherEmojiKey"] = wInfo.Emoji
		}
	} else {
		m["boostWeatherId"] = ""
		m["boostWeatherName"] = ""
		m["boostWeatherEmojiKey"] = ""
	}
}

// addMoveFields adds translated move names and type emoji keys.
func addMoveFields(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, quickMoveID, chargeMoveID int) {
	m["quickMoveId"] = quickMoveID
	m["chargeMoveId"] = chargeMoveID
	m["quickMoveName"] = TranslateMoveName(tr, quickMoveID)
	m["chargeMoveName"] = TranslateMoveName(tr, chargeMoveID)

	// Move type emoji keys
	if quickMove := gd.GetMove(quickMoveID); quickMove != nil && quickMove.TypeID > 0 {
		if ti, ok := gd.Types[quickMove.TypeID]; ok {
			m["quickMoveTypeEmojiKey"] = ti.Emoji
		}
	}
	if chargeMove := gd.GetMove(chargeMoveID); chargeMove != nil && chargeMove.TypeID > 0 {
		if ti, ok := gd.Types[chargeMove.TypeID]; ok {
			m["chargeMoveTypeEmojiKey"] = ti.Emoji
		}
	}
}
