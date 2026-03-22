package enrichment

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/matching"
)

// Quest builds enrichment fields for a quest webhook.
func (e *Enricher) Quest(lat, lon float64, pokestopID string, rewards []matching.QuestRewardData) map[string]any {
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
	e.addStaticMap(m, "quest", lat, lon, nil)

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
