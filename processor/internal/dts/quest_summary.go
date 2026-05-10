package dts

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
)

// BuildQuestSummaryView returns the template context for a single
// reward-group's questSummary message. Reward fields are shared across
// the group (name, icon URL, count); per-pokestop fields (including
// each entry's own withAR flag) live under the `quests` slice.
//
// When a static map resolver is configured and at least one pokestop
// carries valid coordinates, this also builds a multi-pin static map
// URL using the same Autoposition arguments `!area` uses for distance
// circles.
func BuildQuestSummaryView(
	rewardType, rewardID int,
	perPokestopViews []map[string]any,
	sm *staticmap.Resolver,
	gd *gamedata.GameData,
	tr *i18n.Translator,
) map[string]any {
	// Shared reward icon — copied from the first per-pokestop view (every
	// per-pokestop entry within a single (rewardType, reward) group has
	// the same imgUrl since the icon is reward-derived).
	var sharedImg string
	if len(perPokestopViews) > 0 {
		if v, ok := perPokestopViews[0]["imgUrl"].(string); ok {
			sharedImg = v
		}
	}

	// Multi-pin static map URL. Autoposition over the points to compute
	// centre + zoom so the rendered tile fits all pins. We only build a
	// URL when we have a resolver and at least one pokestop with a
	// non-zero coordinate — pokestops without coords are silently skipped.
	var staticMapURL string
	if sm != nil && len(perPokestopViews) > 0 {
		markers := make([]staticmap.LatLon, 0, len(perPokestopViews))
		points := make([]map[string]any, 0, len(perPokestopViews))
		for _, q := range perPokestopViews {
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
			})
		}
		if len(markers) > 0 {
			pos := staticmap.Autoposition(staticmap.AutopositionShape{
				Markers: markers,
			}, 500, 250, 1.25, 17.5)
			if pos != nil {
				// staticMapType is sourced from the resolver config —
				// admins map "questSummary" → e.g. "multiStaticMap" via
				// [geocoding.static_map_type]. Default falls back to
				// "staticMap" inside getConfigForTileType when no entry
				// is set.
				staticMapType := sm.GetStaticMapType("questSummary")
				staticMapURL = sm.GetPregeneratedTileURL("questSummary", map[string]any{
					"points":    points,
					"zoom":      pos.Zoom,
					"latitude":  pos.Latitude,
					"longitude": pos.Longitude,
				}, staticMapType)
			}
		}
	}

	return map[string]any{
		"rewardType": rewardType,
		"reward":     rewardID,
		"rewardName": questSummaryRewardName(rewardType, rewardID, gd, tr),
		"imgUrl":     sharedImg,
		"staticMap":  staticMapURL,
		"count":      len(perPokestopViews),
		"quests":     perPokestopViews,
	}
}

// questSummaryRewardName resolves a (rewardType, rewardID) pair to a
// translated display name for the summary header. We deliberately avoid
// pulling in the regular quest enrichment helpers here because the
// enrichment package would introduce an import cycle (enrichment → dts
// is the existing direction). The lookups are simple identifier-key
// translations through the i18n bundle.
func questSummaryRewardName(rewardType, rewardID int, gd *gamedata.GameData, tr *i18n.Translator) string {
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
	case 4: // Candy — rewardID is the pokemon ID
		if rewardID > 0 {
			return tr.T(gamedata.PokemonTranslationKey(rewardID))
		}
	case 7: // Pokemon encounter — rewardID is the pokemon ID
		if rewardID > 0 {
			return tr.T(gamedata.PokemonTranslationKey(rewardID))
		}
	case 12: // Mega energy — rewardID is the pokemon ID
		if rewardID > 0 {
			return tr.T(gamedata.PokemonTranslationKey(rewardID))
		}
	}
	_ = gd
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
