package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
)

// Gym builds enrichment fields for a gym webhook.
func (e *Enricher) Gym(lat, lon float64, teamID, oldTeamID int, gymID string) map[string]any {
	m := make(map[string]any)

	tz := geo.GetTimezone(lat, lon)
	m["conqueredTime"] = geo.FormatNow(tz, e.TimeLayout)
	m["tth"] = geo.TTH{Hours: 1}
	addSunTimes(m, lat, lon, tz)

	// Map URLs
	e.addMapURLs(m, lat, lon, "gyms", gymID)

	// Game data enrichment
	if e.GameData != nil {
		if info, ok := e.GameData.Util.Teams[teamID]; ok {
			m["gymColor"] = info.Color
		}
	}

	return m
}

// GymTranslate adds per-language translated fields.
func (e *Enricher) GymTranslate(base map[string]any, teamID, oldTeamID int, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return base
	}

	m := make(map[string]any, len(base)+6)
	for k, v := range base {
		m[k] = v
	}

	tr := e.Translations.For(lang)
	addTeamFields(m, e.GameData, tr, teamID)
	if oldTeamID >= 0 {
		if info, ok := e.GameData.Util.Teams[oldTeamID]; ok {
			m["oldTeamName"] = tr.T(info.Name)
			m["oldTeamEmojiKey"] = info.Emoji
		}
	}

	return m
}
