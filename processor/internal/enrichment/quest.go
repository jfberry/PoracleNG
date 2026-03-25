package enrichment

import (
	"math"
	"time"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
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

	// Shiny possible for pokemon rewards
	if len(rewardData.Monsters) > 0 {
		m["isShiny"] = rewardData.Monsters[0].Shiny
		if e.ShinyProvider != nil {
			rate := e.ShinyProvider.GetShinyRate(rewardData.Monsters[0].PokemonID)
			m["shinyPossible"] = rate > 0
			if rate > 0 {
				m["shinyStats"] = int(math.Round(rate))
			}
		}
	}

	m["_rewardData"] = rewardData // internal: used by QuestTranslate, not sent to alerter

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
