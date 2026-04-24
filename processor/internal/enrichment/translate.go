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

	// Full name — for mega/primal pokemon, prefer the combined
	// poke_{id}_e{evolution} key from pogo-translations (e.g. poke_6_e2 =
	// "Mega Charizard X") since that gives a fully localised mega name.
	// Fall back to the util.json format pattern for species without a
	// dedicated translation.
	m["fullName"] = buildFullName(tr, nameKeys, name, formNormalised, pokemonID, evolution)

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
		enFormName := ""
		if nameKeys.FormKey != "" {
			enFormName = enTr.T(nameKeys.FormKey)
		}
		m["nameEng"] = enName
		m["formNameEng"] = enFormName
		m["formNormalisedEng"] = enFormNormalised
		m["fullNameEng"] = buildFullName(enTr, nameKeys, enName, enFormNormalised, pokemonID, evolution)
	}
}

// buildFullName constructs a pokemon's localized display name. For mega/primal
// evolutions it first tries the combo key poke_{id}_e{evolution} and falls
// back to applying the util.json MegaName format pattern to the base+form name.
func buildFullName(tr *i18n.Translator, nameKeys gamedata.MonsterNameInfo, name, formNormalised string, pokemonID, evolution int) string {
	if evolution > 0 && tr != nil {
		comboKey := fmt.Sprintf("poke_%d_e%d", pokemonID, evolution)
		if translated := tr.T(comboKey); translated != comboKey && translated != "" {
			return translated
		}
	}
	fullName := name
	if formNormalised != "" {
		fullName = name + " " + formNormalised
	}
	if evolution > 0 {
		fullName = i18n.Format(nameKeys.MegaNamePattern, fullName)
	}
	return fullName
}

// IsNormalForm returns true if a form name is "Normal" in any common language.
// The form_0 key typically translates to "Normal" or equivalent.
func IsNormalForm(name string) bool {
	lower := strings.ToLower(name)
	return lower == "normal" || lower == "unset" || lower == ""
}

// TranslateTypeNames adds translated type names to the enrichment map.
func TranslateTypeNames(m map[string]any, tr *i18n.Translator, enTr *i18n.Translator, typeIDs []int) {
	names := make([]string, 0, len(typeIDs))
	namesEng := make([]string, 0, len(typeIDs))
	for _, id := range typeIDs {
		key := gamedata.TypeTranslationKey(id)
		names = append(names, tr.T(key))
		if enTr != nil {
			namesEng = append(namesEng, enTr.T(key))
		}
	}
	m["typeName"] = strings.Join(names, ", ")
	if enTr != nil {
		m["typeNameEng"] = namesEng
	}
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
// Stores emoji keys on each type entry — emoji resolution to platform-specific
// strings happens later in NewLayeredView (which knows the platform).
func TranslateWeaknessCategories(categories []gamedata.WeaknessCategory, tr *i18n.Translator, gd *gamedata.GameData) []map[string]any {
	result := make([]map[string]any, 0, len(categories))
	for _, cat := range categories {
		translatedTypes := make([]map[string]any, 0, len(cat.TypeIDs))
		var typeNames []string
		var emojiKeys []string
		for _, typeID := range cat.TypeIDs {
			name := tr.T(gamedata.TypeTranslationKey(typeID))
			entry := map[string]any{
				"typeId": typeID,
				"name":   name,
			}
			if ti, ok := gd.Types[typeID]; ok && ti.Emoji != "" {
				entry["emojiKey"] = ti.Emoji
				emojiKeys = append(emojiKeys, ti.Emoji)
			}
			translatedTypes = append(translatedTypes, entry)
			typeNames = append(typeNames, name)
		}
		result = append(result, map[string]any{
			"value":         cat.Multiplier,
			"types":         translatedTypes,
			"typeName":      strings.Join(typeNames, ", "),
			"typeEmojiKeys": emojiKeys,
		})
	}
	return result
}

// addGenderFields adds translated gender name and emoji key to the enrichment
// map. Gender display names use pogo-translations identifier keys gender_0..3
// (gender_3 is supplied by processor/internal/i18n/locale/*.json since
// pogo-translations only ships 0..2). util.json is consulted only for the
// emoji key.
func addGenderFields(m map[string]any, gd *gamedata.GameData, tr, enTr *i18n.Translator, gender int) {
	if info, ok := gd.Util.Genders[gender]; ok {
		m["genderName"] = translateGenderName(tr, gender)
		m["genderNameEng"] = translateGenderName(enTr, gender)
		m["genderEmojiKey"] = info.Emoji
	} else {
		m["genderName"] = ""
		m["genderNameEng"] = ""
		m["genderEmojiKey"] = ""
	}
}

// translateGenderName returns the pogo-translations value for a gender id.
func translateGenderName(tr *i18n.Translator, gender int) string {
	if tr == nil {
		return ""
	}
	return tr.T(fmt.Sprintf("gender_%d", gender))
}

// addRarityFields adds translated rarity name to the enrichment map.
// Rarity names use identifier keys rarity_1..rarity_6 from the embedded
// i18n files; util.json is only used to detect valid ids.
func addRarityFields(m map[string]any, gd *gamedata.GameData, tr, enTr *i18n.Translator, rarityGroup int) {
	if _, ok := gd.Util.Rarity[rarityGroup]; ok {
		m["rarityName"] = translateRarityName(tr, rarityGroup)
		m["rarityNameEng"] = translateRarityName(enTr, rarityGroup)
	} else {
		m["rarityName"] = ""
		m["rarityNameEng"] = ""
	}
}

func translateRarityName(tr *i18n.Translator, rarityGroup int) string {
	if tr == nil {
		return ""
	}
	return tr.T(fmt.Sprintf("rarity_%d", rarityGroup))
}

// addSizeFields adds translated size name to the enrichment map.
// Size names use identifier keys size_1..size_5 from the embedded i18n
// files; util.json is only used to detect valid ids.
func addSizeFields(m map[string]any, gd *gamedata.GameData, tr, enTr *i18n.Translator, size int) {
	if _, ok := gd.Util.Size[size]; ok {
		m["sizeName"] = translateSizeName(tr, size)
		m["sizeNameEng"] = translateSizeName(enTr, size)
	} else {
		m["sizeName"] = ""
		m["sizeNameEng"] = ""
	}
}

func translateSizeName(tr *i18n.Translator, size int) string {
	if tr == nil {
		return ""
	}
	return tr.T(fmt.Sprintf("size_%d", size))
}

// addTeamFields adds translated team name and emoji key to the enrichment map.
// Team display names come from pogo-translations identifier keys team_0..team_3;
// util.json is only consulted for the emoji key and colour.
func addTeamFields(m map[string]any, gd *gamedata.GameData, tr, enTr *i18n.Translator, teamID int) {
	if info, ok := gd.Util.Teams[teamID]; ok {
		m["teamName"] = translateTeamName(tr, teamID)
		m["teamNameEng"] = translateTeamName(enTr, teamID)
		m["teamEmojiKey"] = info.Emoji
		m["teamColor"] = info.Color
	} else {
		m["teamName"] = ""
		m["teamNameEng"] = ""
		m["teamEmojiKey"] = ""
		m["teamColor"] = ""
	}
}

// translateTeamName returns the pogo-translations value for a team by id
// (team_0 = No Team, team_1 = Mystic, etc.). Returns "" when tr is nil.
func translateTeamName(tr *i18n.Translator, teamID int) string {
	if tr == nil {
		return ""
	}
	return tr.T(fmt.Sprintf("team_%d", teamID))
}

// addGenerationFields adds translated generation info to the enrichment map.
// Generation display names come from pogo-translations identifier keys
// generation_1..generation_N (Kanto / Johto / ...); util.json is only used
// for the roman numeral.
func addGenerationFields(m map[string]any, gd *gamedata.GameData, tr, enTr *i18n.Translator, pokemonID, form int) {
	gen := gd.GetGeneration(pokemonID, form)
	m["generation"] = gen
	info := gd.GetGenerationInfo(gen)
	if info != nil {
		m["generationName"] = translateGenerationName(tr, gen)
		m["generationNameEng"] = translateGenerationName(enTr, gen)
		m["generationRoman"] = info.Roman
	} else {
		m["generationName"] = fmt.Sprintf("Gen %d", gen)
		m["generationNameEng"] = fmt.Sprintf("Gen %d", gen)
		m["generationRoman"] = ""
	}
}

// translateGenerationName returns the pogo-translations value for a
// generation (generation_1 = Kanto, etc.). Returns "" when tr is nil.
func translateGenerationName(tr *i18n.Translator, gen int) string {
	if tr == nil {
		return ""
	}
	return tr.T(fmt.Sprintf("generation_%d", gen))
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
func addMoveFields(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, enTr *i18n.Translator, quickMoveID, chargeMoveID int) {
	m["quickMoveId"] = quickMoveID
	m["chargeMoveId"] = chargeMoveID
	m["quickMoveName"] = TranslateMoveName(tr, quickMoveID)
	m["chargeMoveName"] = TranslateMoveName(tr, chargeMoveID)
	if enTr != nil {
		m["quickMoveNameEng"] = TranslateMoveName(enTr, quickMoveID)
		m["chargeMoveNameEng"] = TranslateMoveName(enTr, chargeMoveID)
	}

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
