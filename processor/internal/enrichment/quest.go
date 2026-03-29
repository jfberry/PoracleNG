package enrichment

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
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
func (e *Enricher) Quest(lat, lon float64, pokestopID string, rewards []matching.QuestRewardData) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)

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
	pending := e.addStaticMap(m, "quest", lat, lon, nil)

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
	}
	m["monsterNames"] = strings.Join(monsterNames, ", ")
	m["monsterNamesEng"] = strings.Join(monsterNamesEng, ", ")

	// Item names
	var itemNames, itemNamesEng []string
	for _, item := range rewardData.Items {
		name := TranslateItemName(tr, item.ID)
		nameEng := TranslateItemName(enTr, item.ID)
		if item.Amount > 1 {
			itemNames = append(itemNames, fmt.Sprintf("%d %s", item.Amount, name))
			itemNamesEng = append(itemNamesEng, fmt.Sprintf("%d %s", item.Amount, nameEng))
		} else {
			itemNames = append(itemNames, name)
			itemNamesEng = append(itemNamesEng, nameEng)
		}
	}
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

	// Mega energy names
	var energyNames, energyNamesEng []string
	for _, e := range rewardData.EnergyMonsters {
		pokeName := tr.T(gamedata.PokemonTranslationKey(e.PokemonID))
		pokeNameEng := enTr.T(gamedata.PokemonTranslationKey(e.PokemonID))
		energyLabel := tr.T("quest_reward_12")
		energyLabelEng := enTr.T("quest_reward_12")
		energyNames = append(energyNames, fmt.Sprintf("%d %s %s", e.Amount, pokeName, energyLabel))
		energyNamesEng = append(energyNamesEng, fmt.Sprintf("%d %s %s", e.Amount, pokeNameEng, energyLabelEng))
	}
	m["energyMonstersNames"] = strings.Join(energyNames, ", ")
	m["energyMonstersNamesEng"] = strings.Join(energyNamesEng, ", ")

	// Candy names
	var candyNames, candyNamesEng []string
	for _, c := range rewardData.Candy {
		pokeName := tr.T(gamedata.PokemonTranslationKey(c.PokemonID))
		pokeNameEng := enTr.T(gamedata.PokemonTranslationKey(c.PokemonID))
		candyLabel := tr.T("quest_reward_4")
		candyLabelEng := enTr.T("quest_reward_4")
		candyNames = append(candyNames, fmt.Sprintf("%d %s %s", c.Amount, pokeName, candyLabel))
		candyNamesEng = append(candyNamesEng, fmt.Sprintf("%d %s %s", c.Amount, pokeNameEng, candyLabelEng))
	}
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

	return m
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
			m["imgUrl"] = e.ImgUicons.PokemonIcon(r.PokemonID, r.FormID, 0, 0, 0, 0, shiny)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.PokemonIcon(r.PokemonID, r.FormID, 0, 0, 0, 0, shiny)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.PokemonIcon(r.PokemonID, r.FormID, 0, 0, 0, 0, shiny)
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
