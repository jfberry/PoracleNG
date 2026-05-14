package enrichment

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// QuestRewardData holds structured reward data for quest enrichment.
type QuestRewardData struct {
	Monsters       []QuestMonsterReward
	Items          []QuestItemReward
	DustAmount     int
	EnergyMonsters []QuestEnergyReward
	Candy          []QuestCandyReward
}

// QuestMonsterReward holds a pokemon encounter reward.
type QuestMonsterReward struct {
	PokemonID int
	FormID    int
	Shiny     bool
}

// QuestItemReward holds an item reward.
type QuestItemReward struct {
	ID     int
	Amount int
}

// QuestEnergyReward holds a mega energy reward.
type QuestEnergyReward struct {
	PokemonID int
	Amount    int
}

// QuestCandyReward holds a candy reward.
type QuestCandyReward struct {
	PokemonID int
	Amount    int
}

// Quest builds enrichment fields for a quest webhook.
func (e *Enricher) Quest(lat, lon float64, pokestopID, pokestopURL string, rewards []matching.QuestRewardData, tileMode int) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)

	// Pokestop identity
	m["pokestop_id"] = pokestopID
	if pokestopURL != "" {
		m["pokestop_url"] = pokestopURL
	}

	endOfDay := geo.EndOfDay(lat, lon)
	tz := geo.GetTimezone(lat, lon)
	m["disappearTime"] = geo.FormatTime(endOfDay, tz, e.TimeLayout)
	m["disappear_time"] = endOfDay
	m["tth"] = geo.ComputeTTH(endOfDay)

	// Map URLs
	e.addMapURLs(m, lat, lon, "pokestops", pokestopID)

	// Icon URLs based on first reward type
	e.addQuestIconURLs(m, rewards)

	// Reverse geocoding
	e.addGeoResult(m, lat, lon)

	// Static map tile
	pending := e.addStaticMap(m, "quest", lat, lon, nil, tileMode)

	// Future event check
	if e.EventChecker != nil {
		now := time.Now().Unix()
		if result := e.EventChecker.EventChangesQuest(now, endOfDay, tz); result != nil {
			m["futureEvent"] = result.FutureEvent
			m["futureEventTime"] = result.FutureEventTime
			m["futureEventName"] = result.FutureEventName
			m["futureEventTrigger"] = result.FutureEventTrigger
		}
	}

	// Structure reward data for per-language enrichment
	rewardData := buildQuestRewardData(rewards)
	m["dustAmount"] = rewardData.DustAmount
	if len(rewardData.Items) > 0 {
		m["itemAmount"] = rewardData.Items[0].Amount
	}
	// Deprecated aliases for backwards compatibility
	if len(rewardData.EnergyMonsters) > 0 {
		m["energyAmount"] = rewardData.EnergyMonsters[0].Amount
	}
	if len(rewardData.Candy) > 0 {
		m["candyAmount"] = rewardData.Candy[0].Amount
	}

	// Shiny possible and base stats for pokemon rewards
	if len(rewardData.Monsters) > 0 {
		m["isShiny"] = rewardData.Monsters[0].Shiny
		if e.ShinyProvider != nil {
			rate := e.ShinyProvider.GetShinyRate(rewardData.Monsters[0].PokemonID)
			m["shinyPossible"] = rate > 0
			if rate > 0 {
				m["shinyStats"] = int(math.Round(rate))
			}
		}
		// Base stats for pokemon reward (used by calculateCp Handlebars helper)
		if e.GameData != nil {
			mon := e.GameData.GetMonster(rewardData.Monsters[0].PokemonID, rewardData.Monsters[0].FormID)
			if mon != nil {
				m["baseStats"] = map[string]int{
					"baseAttack":  mon.Attack,
					"baseDefense": mon.Defense,
					"baseStamina": mon.Stamina,
				}
			}
		}
	}

	e.setFallbackImg(m, e.FallbackImgURL)
	if _, ok := m["pokestop_url"]; !ok && e.FallbackPokestopURL != "" {
		m["pokestop_url"] = e.FallbackPokestopURL
	}

	return m, pending
}

// buildQuestRewardData structures raw matching rewards into typed objects.
func buildQuestRewardData(rewards []matching.QuestRewardData) QuestRewardData {
	var result QuestRewardData
	for _, r := range rewards {
		switch r.Type {
		case 2: // Item
			result.Items = append(result.Items, QuestItemReward{ID: r.ItemID, Amount: r.Amount})
		case 3: // Stardust
			result.DustAmount = r.Amount
		case 4: // Candy
			result.Candy = append(result.Candy, QuestCandyReward{PokemonID: r.PokemonID, Amount: r.Amount})
		case 7: // Pokemon encounter
			result.Monsters = append(result.Monsters, QuestMonsterReward{
				PokemonID: r.PokemonID, FormID: r.FormID, Shiny: r.Shiny,
			})
		case 12: // Mega energy
			result.EnergyMonsters = append(result.EnergyMonsters, QuestEnergyReward{
				PokemonID: r.PokemonID, Amount: r.Amount,
			})
		}
	}
	return result
}

// QuestTranslate adds per-language translated fields for quest enrichment.
func (e *Enricher) QuestTranslate(base map[string]any, quest *webhook.QuestWebhook, rewards []matching.QuestRewardData, lang string) map[string]any {
	if e.Translations == nil {
		return nil
	}

	m := make(map[string]any, 20) // only translated fields; caller merges base + perLang

	tr := e.Translations.For(lang)
	enTr := e.Translations.For("en")
	gd := e.GameData

	// Quest title
	titleKey := "quest_title_" + strings.ToLower(quest.Title)
	namedArgs := map[string]string{"amount_0": strconv.Itoa(quest.Target)}
	m["questString"] = tr.TfNamed(titleKey, namedArgs)
	m["questStringEng"] = enTr.TfNamed(titleKey, namedArgs)

	// Build reward data from matching rewards
	rewardData := buildQuestRewardData(rewards)

	// Monster reward names
	var monsterNames, monsterNamesEng []string
	if gd != nil && len(rewardData.Monsters) > 0 {
		monsterList := make([]map[string]any, len(rewardData.Monsters))
		for i, mon := range rewardData.Monsters {
			nameInfo := make(map[string]any)
			TranslateMonsterNamesEng(nameInfo, gd, tr, e.Translations, mon.PokemonID, mon.FormID, 0)
			monsterList[i] = map[string]any{
				"pokemonId":   mon.PokemonID,
				"formId":      mon.FormID,
				"shiny":       mon.Shiny,
				"name":        nameInfo["name"],
				"formName":    nameInfo["formName"],
				"fullName":    nameInfo["fullName"],
				"nameEng":     nameInfo["nameEng"],
				"fullNameEng": nameInfo["fullNameEng"],
			}
			if fn, ok := nameInfo["fullName"].(string); ok {
				monsterNames = append(monsterNames, fn)
			}
			if fn, ok := nameInfo["fullNameEng"].(string); ok {
				monsterNamesEng = append(monsterNamesEng, fn)
			}
		}
		m["monsterList"] = monsterList
		m["monsters"] = monsterList // JS compat: spread from rewardData
	}
	m["monsterNames"] = strings.Join(monsterNames, ", ")
	m["monsterNamesEng"] = strings.Join(monsterNamesEng, ", ")

	// Item names + items array
	var itemNames, itemNamesEng []string
	itemsList := make([]map[string]any, 0, len(rewardData.Items))
	for _, item := range rewardData.Items {
		name := TranslateItemName(tr, item.ID)
		nameEng := TranslateItemName(enTr, item.ID)
		itemsList = append(itemsList, map[string]any{
			"id":      item.ID,
			"amount":  item.Amount,
			"name":    name,
			"nameEng": nameEng,
		})
		if item.Amount > 1 {
			itemNames = append(itemNames, fmt.Sprintf("%d %s", item.Amount, name))
			itemNamesEng = append(itemNamesEng, fmt.Sprintf("%d %s", item.Amount, nameEng))
		} else {
			itemNames = append(itemNames, name)
			itemNamesEng = append(itemNamesEng, nameEng)
		}
	}
	m["items"] = itemsList
	m["itemNames"] = strings.Join(itemNames, ", ")
	m["itemNamesEng"] = strings.Join(itemNamesEng, ", ")

	// Stardust
	var dustText, dustTextEng string
	if rewardData.DustAmount > 0 {
		dustName := tr.T("quest_reward_3")
		dustNameEng := enTr.T("quest_reward_3")
		dustText = fmt.Sprintf("%d %s", rewardData.DustAmount, dustName)
		dustTextEng = fmt.Sprintf("%d %s", rewardData.DustAmount, dustNameEng)
	}
	m["dustText"] = dustText
	m["dustTextEng"] = dustTextEng

	// Mega energy names + energyMonsters array
	var energyNames, energyNamesEng []string
	energyList := make([]map[string]any, 0, len(rewardData.EnergyMonsters))
	for _, e := range rewardData.EnergyMonsters {
		pokeName := tr.T(gamedata.PokemonTranslationKey(e.PokemonID))
		pokeNameEng := enTr.T(gamedata.PokemonTranslationKey(e.PokemonID))
		energyLabel := tr.T("quest_reward_12")
		energyLabelEng := enTr.T("quest_reward_12")
		energyList = append(energyList, map[string]any{
			"pokemonId": e.PokemonID,
			"amount":    e.Amount,
			"name":      pokeName,
			"nameEng":   pokeNameEng,
		})
		energyNames = append(energyNames, fmt.Sprintf("%d %s %s", e.Amount, pokeName, energyLabel))
		energyNamesEng = append(energyNamesEng, fmt.Sprintf("%d %s %s", e.Amount, pokeNameEng, energyLabelEng))
	}
	m["energyMonsters"] = energyList
	m["energyMonstersNames"] = strings.Join(energyNames, ", ")
	m["energyMonstersNamesEng"] = strings.Join(energyNamesEng, ", ")

	// Candy names + candy array
	var candyNames, candyNamesEng []string
	candyList := make([]map[string]any, 0, len(rewardData.Candy))
	for _, c := range rewardData.Candy {
		pokeName := tr.T(gamedata.PokemonTranslationKey(c.PokemonID))
		pokeNameEng := enTr.T(gamedata.PokemonTranslationKey(c.PokemonID))
		candyLabel := tr.T("quest_reward_4")
		candyLabelEng := enTr.T("quest_reward_4")
		candyList = append(candyList, map[string]any{
			"pokemonId": c.PokemonID,
			"amount":    c.Amount,
			"name":      pokeName,
			"nameEng":   pokeNameEng,
		})
		candyNames = append(candyNames, fmt.Sprintf("%d %s %s", c.Amount, pokeName, candyLabel))
		candyNamesEng = append(candyNamesEng, fmt.Sprintf("%d %s %s", c.Amount, pokeNameEng, candyLabelEng))
	}
	m["candy"] = candyList
	m["candyMonstersNames"] = strings.Join(candyNames, ", ")
	m["candyMonstersNamesEng"] = strings.Join(candyNamesEng, ", ")

	// Reward string (join all non-empty reward parts)
	var rewardParts, rewardPartsEng []string
	for _, s := range []string{strings.Join(monsterNames, ", "), dustText, strings.Join(itemNames, ", "), strings.Join(energyNames, ", "), strings.Join(candyNames, ", ")} {
		if s != "" {
			rewardParts = append(rewardParts, s)
		}
	}
	for _, s := range []string{strings.Join(monsterNamesEng, ", "), dustTextEng, strings.Join(itemNamesEng, ", "), strings.Join(energyNamesEng, ", "), strings.Join(candyNamesEng, ", ")} {
		if s != "" {
			rewardPartsEng = append(rewardPartsEng, s)
		}
	}
	m["rewardString"] = strings.Join(rewardParts, ", ")
	m["rewardStringEng"] = strings.Join(rewardPartsEng, ", ")

	// Shiny emoji key
	if sp, ok := base["shinyPossible"]; ok {
		if b, ok := sp.(bool); ok && b {
			m["shinyPossibleEmojiKey"] = "shiny"
		}
	}

	// Quest completion conditions (e.g. "Curve Ball", "Excellent Throw",
	// "Catch a Fire-type Pokemon"). Translation keys ship with
	// pogo-translations gamelocale: `quest_condition_{type}` for the bare
	// label and `quest_condition_{type}_formatted` for the variant with a
	// payload placeholder like `%{throw_type}` or `%{types}`.
	if len(quest.Conditions) > 0 {
		condList := make([]map[string]any, 0, len(quest.Conditions))
		condListEng := make([]map[string]any, 0, len(quest.Conditions))
		var condTexts, condTextsEng []string
		for _, c := range quest.Conditions {
			text, name := translateQuestCondition(tr, gd, c)
			textEng, nameEng := translateQuestCondition(enTr, gd, c)
			condList = append(condList, map[string]any{
				"type":      c.Type,
				"name":      name,
				"formatted": text,
			})
			condListEng = append(condListEng, map[string]any{
				"type":      c.Type,
				"name":      nameEng,
				"formatted": textEng,
			})
			if text != "" {
				condTexts = append(condTexts, text)
			}
			if textEng != "" {
				condTextsEng = append(condTextsEng, textEng)
			}
		}
		m["conditionList"] = condList
		m["conditionListEng"] = condListEng
		m["conditionString"] = strings.Join(condTexts, ", ")
		m["conditionStringEng"] = strings.Join(condTextsEng, ", ")
	}

	return m
}

// translateQuestCondition returns the formatted ("Curve Ball Throw") and
// bare ("Throw Type") translations for a quest condition. The formatted
// variant falls back to the bare name when the payload is missing or the
// `_formatted` key is absent for that locale.
func translateQuestCondition(tr *i18n.Translator, gd *gamedata.GameData, c webhook.QuestCondition) (formatted, name string) {
	bareKey := fmt.Sprintf("quest_condition_%d", c.Type)
	name = tr.T(bareKey)

	args := buildConditionPlaceholders(tr, gd, c)
	if len(args) > 0 {
		formattedKey := bareKey + "_formatted"
		if raw := tr.T(formattedKey); raw != formattedKey {
			formatted = i18n.FormatNamed(raw, args)
			// If a placeholder couldn't be resolved (still contains %{...})
			// fall back to the bare name to avoid surfacing raw template
			// markers to the user.
			if strings.Contains(formatted, "%{") {
				formatted = name
			}
			return formatted, name
		}
	}
	return name, name
}

// buildConditionPlaceholders maps a condition's `info` payload to the
// named placeholders referenced by its `_formatted` translation. Returns
// nil when the payload doesn't carry the required IDs (e.g. type 14 with
// only `{"hit": true}` — falls through to the bare name).
func buildConditionPlaceholders(tr *i18n.Translator, gd *gamedata.GameData, c webhook.QuestCondition) map[string]string {
	if len(c.Info) == 0 {
		return nil
	}

	switch c.Type {
	case 1: // Pokemon Type
		if names := translateIDList(tr, c.Info["pokemon_type_ids"], gamedata.TypeTranslationKey); names != "" {
			return map[string]string{"types": names}
		}
	case 2: // Pokemon Category — webhook payload is pokemon_ids
		if names := translateIDList(tr, c.Info["pokemon_ids"], gamedata.PokemonTranslationKey); names != "" {
			return map[string]string{"pokemon": names}
		}
	case 7: // Raid Level
		if names := joinIDList(c.Info["raid_levels"]); names != "" {
			return map[string]string{"levels": names}
		}
	case 8, 14: // Throw Type / Throw Type In a Row
		if id := toInt(c.Info["throw_type_id"]); id > 0 {
			return map[string]string{"throw_type": tr.T(fmt.Sprintf("throw_type_%d", id))}
		}
	case 11: // Item
		if id := toInt(c.Info["item_id"]); id > 0 {
			return map[string]string{"item": tr.T(gamedata.ItemTranslationKey(id))}
		}
	case 26: // Pokemon Alignment
		if names := translateIDList(tr, c.Info["alignment_ids"], func(id int) string {
			return fmt.Sprintf("alignment_%d", id)
		}); names != "" {
			return map[string]string{"alignments": names}
		}
	case 27: // Invasion Category
		if names := translateIDList(tr, c.Info["character_category_ids"], func(id int) string {
			return fmt.Sprintf("character_category_%d", id)
		}); names != "" {
			return map[string]string{"categories": names}
		}
	case 42: // Geotargeted POI Scan
		if poi, ok := c.Info["poi"].(string); ok && poi != "" {
			return map[string]string{"poi": poi}
		}
	case 43: // With Item Type
		if names := translateIDList(tr, c.Info["pokemon_type_ids"], gamedata.TypeTranslationKey); names != "" {
			return map[string]string{"types": names}
		}
	case 44: // Within Time (seconds)
		if t := toInt(c.Info["time"]); t > 0 {
			return map[string]string{"time": strconv.Itoa(t)}
		}
		if t := toInt(c.Info["seconds"]); t > 0 {
			return map[string]string{"time": strconv.Itoa(t)}
		}
	}
	_ = gd
	return nil
}

// translateIDList takes an arbitrary `info` value (expected to be a
// JSON-decoded number array), translates each ID via keyFn, and joins.
// Empty / wrong-typed input returns "".
func translateIDList(tr *i18n.Translator, v any, keyFn func(int) string) string {
	ids, ok := v.([]any)
	if !ok || len(ids) == 0 {
		return ""
	}
	names := make([]string, 0, len(ids))
	for _, raw := range ids {
		if id := toInt(raw); id > 0 {
			names = append(names, tr.T(keyFn(id)))
		}
	}
	return strings.Join(names, ", ")
}

// joinIDList comma-joins a JSON-decoded number array verbatim (used for
// raid levels which don't have a translation, just a digit).
func joinIDList(v any) string {
	ids, ok := v.([]any)
	if !ok || len(ids) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ids))
	for _, raw := range ids {
		if id := toInt(raw); id > 0 {
			parts = append(parts, strconv.Itoa(id))
		}
	}
	return strings.Join(parts, ", ")
}

// addQuestIconURLs resolves icon URLs based on the quest reward type.
// Reward types: 2=item, 3=stardust, 4=candy, 7=pokemon, 12=mega energy
func (e *Enricher) addQuestIconURLs(m map[string]any, rewards []matching.QuestRewardData) {
	if len(rewards) == 0 {
		return
	}

	r := rewards[0]
	switch {
	case r.Type == 7 && r.PokemonID > 0: // Pokemon reward
		shiny := r.Shiny || e.RequestShinyImages
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.PokemonIcon(r.PokemonID, r.FormID, 0, 0, 0, 0, shiny, 0)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.PokemonIcon(r.PokemonID, r.FormID, 0, 0, 0, 0, shiny, 0)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.PokemonIcon(r.PokemonID, r.FormID, 0, 0, 0, 0, shiny, 0)
		}
	case r.Type == 2 && r.ItemID > 0: // Item reward
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.RewardItemIcon(r.ItemID, r.Amount)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.RewardItemIcon(r.ItemID, r.Amount)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.RewardItemIcon(r.ItemID, r.Amount)
		}
	case r.Type == 3: // Stardust reward
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.RewardStardustIcon(r.Amount)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.RewardStardustIcon(r.Amount)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.RewardStardustIcon(r.Amount)
		}
	case r.Type == 12 && r.PokemonID > 0: // Mega energy reward
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.RewardMegaEnergyIcon(r.PokemonID, r.Amount)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.RewardMegaEnergyIcon(r.PokemonID, r.Amount)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.RewardMegaEnergyIcon(r.PokemonID, r.Amount)
		}
	case r.Type == 4 && r.PokemonID > 0: // Candy reward
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.RewardCandyIcon(r.PokemonID, r.Amount)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.RewardCandyIcon(r.PokemonID, r.Amount)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.RewardCandyIcon(r.PokemonID, r.Amount)
		}
	}
}
