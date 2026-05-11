package dts

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
)

// QuestSummaryGroup carries the inputs BuildQuestSummaryView needs for
// one render. `Quests` is both the per-line loop content AND the set
// of pins on this message's static map — viewer can correlate bullets
// to pins directly. `TotalCount` is the size of the whole reward group
// (carried separately so a chunked split still shows the full count
// in the header).
//
// Chunk / Chunks are 1-indexed pagination counters; for unsplit groups
// both are 1. Templates can opt into "(1/3)"-style headers via
// {{#if (gt chunks 1)}}…{{/if}}.
type QuestSummaryGroup struct {
	RewardType int
	RewardID   int
	RewardForm int              // pokemon form for type==7 rewards; 0 otherwise
	Quests     []map[string]any // bullets AND pins for THIS message
	TotalCount int              // total stops across all chunks of the group
	Chunk      int              // 1-indexed
	Chunks     int              // total chunks for this reward group
}

// BuildQuestSummaryView returns the template context for one
// questSummary message. Reward fields are shared across the group
// (name, icon, total count); per-pokestop fields live under `quests`;
// the static map shows just this chunk's pins so the bullet list and
// map line up.
func BuildQuestSummaryView(g QuestSummaryGroup, sm *staticmap.Resolver, tr *i18n.Translator) map[string]any {
	views := g.Quests

	// Shared reward icon and sticker — every per-pokestop view in a
	// single (rewardType, reward, form) group has the same icon/sticker
	// since both are reward-derived. The Discord template uses imgUrl
	// for the thumbnail; the Telegram template uses stickerUrl which is
	// resolved against the sticker iconset (typically webp, sized for
	// Telegram's sticker constraints) rather than the regular icon URL.
	var sharedImg, sharedSticker string
	if len(views) > 0 {
		if v, ok := views[0]["imgUrl"].(string); ok {
			sharedImg = v
		}
		if v, ok := views[0]["stickerUrl"].(string); ok {
			sharedSticker = v
		}
	}

	// Multi-pin static map URL — autoposition over THIS chunk's stops
	// so the bullet list and the pins on the map are the same set.
	var staticMapURL string
	if sm != nil && len(views) > 0 {
		markers := make([]staticmap.LatLon, 0, len(views))
		points := make([]map[string]any, 0, len(views))
		for _, q := range views {
			lat := numericFloat(q["latitude"])
			lon := numericFloat(q["longitude"])
			if lat == 0 && lon == 0 {
				continue
			}
			markers = append(markers, staticmap.LatLon{Latitude: lat, Longitude: lon})
			points = append(points, map[string]any{
				"latitude":  lat,
				"longitude": lon,
				"name":      q["pokestopName"],
				"imgUrl":    q["imgUrl"],
				"url":       q["pokestopUrl"],
			})
		}
		if len(markers) > 0 {
			pos := staticmap.Autoposition(staticmap.AutopositionShape{
				Markers: markers,
			}, 500, 250, 1.25, 17.5)
			if pos != nil {
				const tileMaptype = "questsummary"
				staticMapURL = sm.GetPregeneratedTileURL(tileMaptype, map[string]any{
					"points":    points,
					"zoom":      pos.Zoom,
					"latitude":  pos.Latitude,
					"longitude": pos.Longitude,
				}, sm.GetStaticMapType(tileMaptype))
			}
		}
	}

	chunk := g.Chunk
	if chunk <= 0 {
		chunk = 1
	}
	chunks := g.Chunks
	if chunks <= 0 {
		chunks = 1
	}
	total := g.TotalCount
	if total <= 0 {
		total = len(views)
	}

	return map[string]any{
		"rewardType": g.RewardType,
		"reward":     g.RewardID,
		"rewardForm": g.RewardForm,
		"rewardName": questSummaryRewardName(g.RewardType, g.RewardID, g.RewardForm, tr),
		"imgUrl":     sharedImg,
		"stickerUrl": sharedSticker,
		"staticMap":  staticMapURL,
		"count":      total, // total for the reward group, not just this chunk
		"chunk":      chunk,
		"chunks":     chunks,
		"quests":     views,
	}
}

// questSummaryRewardName resolves a (rewardType, rewardID, formID) tuple
// to a translated display name for the summary header. The lookups are
// simple identifier-key translations through the i18n bundle —
// pulling in the regular quest enrichment helpers would create an
// enrichment → dts import cycle.
//
// formID is only honoured for pokemon-encounter rewards (type 7); when
// it is non-zero the form name is appended in parentheses so two
// different forms of the same species (e.g. Spinda 01 vs Spinda 08)
// produce distinct headers.
func questSummaryRewardName(rewardType, rewardID, formID int, tr *i18n.Translator) string {
	if tr == nil {
		return ""
	}
	switch rewardType {
	case 2: // Item — rewardID is item ID
		if rewardID > 0 {
			return tr.T(gamedata.ItemTranslationKey(rewardID))
		}
	case 3: // Stardust — rewardID is the dust amount
		label := tr.T("quest_reward_3")
		if rewardID > 0 {
			return fmt.Sprintf("%d %s", rewardID, label)
		}
		return label
	case 4, 12: // Candy / Mega energy — per-species, no form
		if rewardID > 0 {
			return tr.T(gamedata.PokemonTranslationKey(rewardID))
		}
	case 7: // Pokemon encounter — include form when present
		if rewardID > 0 {
			name := tr.T(gamedata.PokemonTranslationKey(rewardID))
			if formID > 0 {
				if formName := tr.T(gamedata.FormTranslationKey(formID)); formName != "" && formName != gamedata.FormTranslationKey(formID) {
					return fmt.Sprintf("%s (%s)", name, formName)
				}
			}
			return name
		}
	}
	return ""
}

// numericFloat coerces an arbitrary value (typically from a webhook /
// enrichment map) to float64, returning 0 for nil/non-numeric values.
// Mirrors the helper-private toFloat in helpers.go but kept local to
// avoid widening that helper's surface.
func numericFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	}
	return 0
}
