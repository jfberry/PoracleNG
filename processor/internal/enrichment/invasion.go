package enrichment

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// contestEntry mirrors the JSON structure of a single Showcase leaderboard entry.
type contestEntry struct {
	Rank                  int     `json:"rank"`
	Score                 float64 `json:"score"`
	PokemonID             int     `json:"pokemon_id"`
	Form                  int     `json:"form"`
	Costume               int     `json:"costume"`
	Gender                int     `json:"gender"`
	Shiny                 bool    `json:"shiny"`
	TempEvolution         int     `json:"temp_evolution"`
	TempEvolutionFinishMs int64   `json:"temp_evolution_finish_ms"`
	Alignment             int     `json:"alignment"`
	Badge                 int     `json:"badge"`
	Background            *int64  `json:"background,omitempty"`
}

// contestJSON mirrors the top-level JSON structure of the showcase_rankings field.
type contestJSON struct {
	TotalEntries   int            `json:"total_entries"`
	LastUpdate     int64          `json:"last_update"`
	ContestEntries []contestEntry `json:"contest_entries"`
}

// Invasion builds enrichment fields for an invasion webhook.
func (e *Enricher) Invasion(lat, lon float64, expiration int64, pokestopID, pokestopURL string, gruntTypeID, displayType, lureID int, tileMode int) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)

	tz := geo.GetTimezone(lat, lon)
	addSunTimes(m, lat, lon, tz)

	// Pokestop identity
	m["pokestop_id"] = pokestopID
	if pokestopURL != "" {
		m["pokestop_url"] = pokestopURL
	}

	cellID := tracker.GetWeatherCellID(lat, lon)
	m["gameWeatherId"] = e.WeatherProvider.GetCurrentWeatherInCell(cellID)

	if expiration > 0 {
		m["expiration"] = expiration
		m["incidentExpiration"] = expiration
		m["incident_expire_timestamp"] = expiration
		m["expirationTimestamp"] = expiration // consistent unix int for Discord <t:N:R>
		m["disappearTime"] = geo.FormatTime(expiration, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(expiration)
	}

	// Icon URLs: event invasions (displayType >= 7 with no grunt) use pokestop icon,
	// regular invasions use invasion icon
	if (gruntTypeID == 0) && displayType >= 7 {
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.PokestopIcon(lureID, true, displayType, false)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.PokestopIcon(lureID, true, displayType, false)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.PokestopIcon(lureID, true, displayType, false)
		}
	} else {
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.InvasionIcon(gruntTypeID)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.InvasionIcon(gruntTypeID)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.InvasionIcon(gruntTypeID)
		}
	}

	// Map URLs
	e.addMapURLs(m, lat, lon, "pokestops", pokestopID)

	// Reverse geocoding
	e.addGeoResult(m, lat, lon)

	// Grunt and display type IDs for DTS templates
	m["gruntTypeId"] = gruntTypeID
	m["displayTypeId"] = displayType

	// Static map tile — only pass non-zero IDs so tileserver template nil checks work
	tileFields := make(map[string]any)
	if gruntTypeID != 0 {
		tileFields["gruntTypeId"] = gruntTypeID
	}
	if displayType != 0 {
		tileFields["displayTypeId"] = displayType
	}
	if lureID != 0 {
		tileFields["lureTypeId"] = lureID
	}
	pending := e.addStaticMap(m, "pokestop", lat, lon, tileFields, tileMode, pokestopID)

	// Grunt data
	if e.GameData != nil {
		grunt := e.GameData.GetGrunt(gruntTypeID)
		if grunt != nil {
			m["gruntTypeID"] = grunt.TypeID
			m["gruntGender"] = grunt.Gender

			// Type color, emoji key, and English type name via TypeInfo
			// (keyed by numeric type ID). gruntType is the English title-case
			// type name (e.g. "Fire", "Grass") for {{#if (eq gruntType 'Fire')}}
			// template comparisons.
			if grunt.TypeID > 0 {
				if typeInfo, ok := e.GameData.Types[grunt.TypeID]; ok {
					m["gruntTypeColor"] = typeInfo.Color
					m["gruntTypeEmojiKey"] = typeInfo.Emoji
					m["gruntType"] = typeInfo.Name
				}
			}

			// Reward pokemon IDs for first slot
			if len(grunt.Team[0]) > 0 {
				rewardIDs := make([]map[string]int, len(grunt.Team[0]))
				for i, r := range grunt.Team[0] {
					rewardIDs[i] = map[string]int{"pokemon_id": r.ID, "form": r.FormID}
				}
				m["gruntRewardIDs"] = rewardIDs
			}
		}

		// Event invasions (gruntTypeID == 0 && displayType >= 7) use PokestopEvent data.
		// gruntType is the lowercase util.json event name
		// (e.g. "kecleon", "showcase", "gold-stop") so templates can do
		// {{#if (eq gruntType 'kecleon')}} dispatch.
		if gruntTypeID == 0 && displayType >= 7 {
			if eventInfo, ok := e.GameData.Util.PokestopEvent[displayType]; ok {
				m["gruntTypeID"] = 0
				m["gruntTypeColor"] = eventInfo.Color
				m["gruntTypeEmojiKey"] = eventInfo.Emoji
				m["gruntType"] = strings.ToLower(eventInfo.Name)
			}
		}
	}

	e.setFallbackImg(m, e.FallbackImgPokestop)
	if _, ok := m["pokestop_url"]; !ok && e.FallbackPokestopURL != "" {
		m["pokestop_url"] = e.FallbackPokestopURL
	}

	return m, pending
}

// InvasionTranslate adds per-language translated fields.
// showcaseRaw is the raw JSON bytes of the showcase_rankings field from the
// Golbat webhook; pass nil for non-Showcase incidents and regular invasions.
func (e *Enricher) InvasionTranslate(base map[string]any, lat, lon float64, gruntTypeID int, lineup []webhook.InvasionLineupEntry, showcaseRaw json.RawMessage, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return nil
	}

	m := make(map[string]any, 25) // only translated fields; caller merges base + perLang
	defer e.addLocalizedGeoResult(m, lat, lon, lang)

	gd := e.GameData
	tr := e.Translations.For(lang)
	gameWeatherID := toInt(base["gameWeatherId"])
	m["gameWeatherName"] = TranslateWeatherName(tr, gameWeatherID)
	if gameWeatherID > 0 {
		if wInfo, ok := gd.Util.Weather[gameWeatherID]; ok {
			m["gameWeatherEmojiKey"] = wInfo.Emoji
		}
	}

	// Event invasions (kecleon, showcase, gold-stop) take priority over grunt ID 0
	// which maps to CHARACTER_UNSET in the grunt data.
	displayType := toInt(base["displayTypeId"])
	isEventInvasion := displayType >= 7
	var grunt *gamedata.Grunt

	if isEventInvasion && gd.Util != nil {
		if _, ok := gd.Util.PokestopEvent[displayType]; ok {
			// Event names come from pogo-translations display_type_N
			// (resources/gamelocale/), not util.json's English labels.
			name := tr.T(fmt.Sprintf("display_type_%d", displayType))
			m["gruntName"] = name
			m["gruntTypeName"] = name
		}
	} else {
		// Regular grunt name
		grunt = e.GameData.GetGrunt(gruntTypeID)
		if grunt != nil {
			m["gruntName"] = tr.T(grunt.CategoryKey())
			if typeKey := grunt.TypeKey(); typeKey != "" {
				m["gruntTypeName"] = tr.T(typeKey)
			} else {
				derived := gamedata.TypeNameFromTemplate(grunt.Template)
				if derived != "" {
					m["gruntTypeName"] = strings.ToUpper(derived[:1]) + derived[1:]
				} else {
					m["gruntTypeName"] = ""
				}
			}
		}
	}

	// Gender name and emoji (uses shared helper for consistent fallbacks)
	addGenderFields(m, gd, tr, e.Translations.For("en"), toInt(base["gruntGender"]))

	// Build gruntRewardsList with translated pokemon names
	if grunt != nil {
		type rewardSlot struct {
			chance     int
			encounters []gamedata.GruntEncounterEntry
		}

		var slots []rewardSlot

		if grunt.HasRewardSlot(1) && len(grunt.Team[1]) > 0 {
			slots = append(slots, rewardSlot{chance: 85, encounters: grunt.Team[0]})
			slots = append(slots, rewardSlot{chance: 15, encounters: grunt.Team[1]})
		}

		if len(slots) == 0 && grunt.HasRewardSlot(2) && len(grunt.Team[2]) > 0 {
			slots = append(slots, rewardSlot{chance: 100, encounters: grunt.Team[2]})
		}

		if len(slots) == 0 && len(grunt.Team[0]) > 0 {
			slots = append(slots, rewardSlot{chance: 100, encounters: grunt.Team[0]})
		}

		if len(slots) > 0 {
			// Build object with first/second keys (matching DTS template expectations)
			slotNames := []string{"first", "second", "third"}
			rewardsList := make(map[string]any, len(slots))
			var rewardsTextParts []string

			for i, slot := range slots {
				monsters := e.translateEncounterSlot(slot.encounters, gd, tr)
				rewardsList[slotNames[i]] = map[string]any{
					"chance":   slot.chance,
					"monsters": monsters,
				}

				// Build flat text
				names := make([]string, len(monsters))
				for j, mon := range monsters {
					names[j], _ = mon["fullName"].(string)
				}
				joined := strings.Join(names, ", ")
				if len(slots) > 1 {
					rewardsTextParts = append(rewardsTextParts, fmt.Sprintf("%d%%: %s", slot.chance, joined))
				} else {
					rewardsTextParts = append(rewardsTextParts, joined)
				}
			}

			m["gruntRewardsList"] = rewardsList
			m["gruntRewards"] = strings.Join(rewardsTextParts, "\\n")
		}
	}

	// Confirmed lineup from webhook (translated pokemon names)
	if len(lineup) > 0 {
		lineupMonsters := make([]map[string]any, 0, len(lineup))
		for _, entry := range lineup {
			nameInfo := make(map[string]any)
			TranslateMonsterNames(nameInfo, gd, tr, entry.PokemonID, entry.Form, 0)
			lineupMonsters = append(lineupMonsters, map[string]any{
				"id":       entry.PokemonID,
				"formId":   entry.Form,
				"name":     nameInfo["name"],
				"formName": nameInfo["formName"],
				"fullName": nameInfo["fullName"],
			})
		}
		m["gruntLineupList"] = map[string]any{
			"confirmed": true,
			"monsters":  lineupMonsters,
		}
	}

	// Showcase rankings (displayType == 9): parse and enrich top-3 contestants.
	// pokestop_id (set by the base Invasion enrichment) is the per-event ref.
	pokestopID, _ := base["pokestop_id"].(string)
	e.translateShowcaseRankings(m, showcaseRaw, gd, tr, lang, pokestopID)

	return m
}

// translateShowcaseRankings parses showcase_rankings JSON and adds enriched
// top-level and per-entry fields to the translation map m. When the field is
// absent, empty, or unparseable the showcase* fields are set to safe zero
// values so templates can always {{#if showcasePresent}} guard cleanly.
func (e *Enricher) translateShowcaseRankings(m map[string]any, showcaseRaw json.RawMessage, gd *gamedata.GameData, tr *i18n.Translator, lang, pokestopID string) {
	// Safe defaults — always set so templates never see missing keys.
	m["showcasePresent"] = false
	m["showcaseTotalEntries"] = 0
	m["showcaseLastUpdate"] = int64(0)
	m["showcaseLastUpdateFormatted"] = ""
	m["showcase"] = []map[string]any{}
	m["showcaseFirst"] = nil

	if len(showcaseRaw) == 0 {
		return
	}

	var contest contestJSON
	if err := json.Unmarshal(showcaseRaw, &contest); err != nil {
		log.Debugf("[%s] showcase_rankings parse error: %v", pokestopID, err)
		return
	}

	if len(contest.ContestEntries) == 0 {
		return
	}

	m["showcasePresent"] = true
	m["showcaseTotalEntries"] = contest.TotalEntries
	m["showcaseLastUpdate"] = contest.LastUpdate

	// Format last_update timestamp using the operator's configured time layout.
	// The timezone is not available in InvasionTranslate (it's in base enrichment);
	// use empty string (UTC fallback) — sufficient for display purposes.
	if contest.LastUpdate > 0 {
		m["showcaseLastUpdateFormatted"] = geo.FormatTime(contest.LastUpdate, "", e.TimeLayout)
	}

	entries := make([]map[string]any, 0, len(contest.ContestEntries))
	for _, ce := range contest.ContestEntries {
		entry := e.translateContestEntry(ce, gd, tr)
		entries = append(entries, entry)
	}

	m["showcase"] = entries
	if len(entries) > 0 {
		m["showcaseFirst"] = entries[0]
	}
}

// translateContestEntry converts a single raw contest entry into a template-ready map.
func (e *Enricher) translateContestEntry(ce contestEntry, gd *gamedata.GameData, tr *i18n.Translator) map[string]any {
	entry := make(map[string]any, 24)

	entry["rank"] = ce.Rank
	entry["score"] = ce.Score
	entry["scoreFormatted"] = fmt.Sprintf("%.2f", ce.Score)
	entry["pokemonId"] = ce.PokemonID
	entry["formId"] = ce.Form
	entry["costumeId"] = ce.Costume
	entry["genderId"] = ce.Gender
	entry["shiny"] = ce.Shiny
	if ce.Shiny {
		entry["shinyEmoji"] = "✨"
	} else {
		entry["shinyEmoji"] = ""
	}
	entry["tempEvolutionId"] = ce.TempEvolution
	entry["alignment"] = ce.Alignment
	entry["badge"] = ce.Badge
	bgVal := int64(0)
	if ce.Background != nil {
		bgVal = *ce.Background
	}
	entry["background"] = bgVal

	if gd == nil || tr == nil {
		entry["pokemonName"] = ""
		entry["formName"] = ""
		entry["fullName"] = ""
		entry["costumeName"] = ""
		entry["genderName"] = ""
		entry["genderEmojiKey"] = ""
		entry["tempEvolutionName"] = ""
		entry["alignmentName"] = ""
		entry["imgUrl"] = ""
		return entry
	}

	// Pokemon and form names
	nameKeys := gd.MonsterNameKeys(ce.PokemonID, ce.Form, ce.TempEvolution)
	pokemonName := tr.T(nameKeys.PokemonKey)
	entry["pokemonName"] = pokemonName

	formName := ""
	formNormalised := ""
	if nameKeys.FormKey != "" {
		formName = tr.T(nameKeys.FormKey)
		if formName != "" && !IsNormalForm(formName) {
			formNormalised = formName
		}
	}
	entry["formName"] = formName

	// fullName: alignment prefix + base+form + mega wrap
	entry["fullName"] = BuildFullNameWithAlignment(tr, nameKeys, pokemonName, formNormalised, ce.PokemonID, ce.TempEvolution, ce.Alignment)

	// Costume name (costume_N key from gamelocale)
	costumeName := ""
	if ce.Costume > 0 {
		key := fmt.Sprintf("costume_%d", ce.Costume)
		translated := tr.T(key)
		if translated != key {
			costumeName = translated
		}
	}
	entry["costumeName"] = costumeName

	// Gender name and emoji key
	genderName := tr.T(fmt.Sprintf("gender_%d", ce.Gender))
	if genderName == fmt.Sprintf("gender_%d", ce.Gender) {
		genderName = ""
	}
	entry["genderName"] = genderName
	genderEmojiKey := ""
	if gd.Util != nil {
		if info, ok := gd.Util.Genders[ce.Gender]; ok {
			genderEmojiKey = info.Emoji
		}
	}
	entry["genderEmojiKey"] = genderEmojiKey

	// Temp evolution (mega) name — use same combo-key approach as buildFullName
	tempEvolutionName := ""
	if ce.TempEvolution > 0 {
		// Already reflected in fullName via buildFullName; expose raw name too.
		tempEvolutionName = buildFullName(tr, nameKeys, pokemonName, formNormalised, ce.PokemonID, ce.TempEvolution)
	}
	entry["tempEvolutionName"] = tempEvolutionName

	// Alignment name
	alignmentName := ""
	if ce.Alignment > 0 {
		key := fmt.Sprintf("alignment_%d", ce.Alignment)
		translated := tr.T(key)
		if translated != key && translated != "" {
			alignmentName = translated
		} else {
			switch ce.Alignment {
			case 1:
				alignmentName = "Shadow"
			case 2:
				alignmentName = "Purified"
			}
		}
	}
	entry["alignmentName"] = alignmentName

	// Icon URL via primary uicons resolver
	imgURL := ""
	if e.ImgUicons != nil {
		imgURL = e.ImgUicons.PokemonIcon(ce.PokemonID, ce.Form, ce.TempEvolution, ce.Gender, ce.Costume, ce.Alignment, ce.Shiny, 0)
	}
	entry["imgUrl"] = imgURL

	return entry
}

// translateEncounterSlot translates a slice of grunt encounter entries into enrichment maps.
func (e *Enricher) translateEncounterSlot(entries []gamedata.GruntEncounterEntry, gd *gamedata.GameData, tr *i18n.Translator) []map[string]any {
	result := make([]map[string]any, len(entries))
	for i, enc := range entries {
		nameInfo := make(map[string]any)
		TranslateMonsterNames(nameInfo, gd, tr, enc.ID, enc.FormID, 0)
		result[i] = map[string]any{
			"id":       enc.ID,
			"formId":   enc.FormID,
			"name":     nameInfo["name"],
			"formName": nameInfo["formName"],
			"fullName": nameInfo["fullName"],
		}
	}
	return result
}
