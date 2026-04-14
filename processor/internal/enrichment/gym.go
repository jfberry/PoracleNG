package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
)

// Gym builds enrichment fields for a gym webhook.
func (e *Enricher) Gym(lat, lon float64, teamID, oldTeamID, slotsAvailable, oldSlotsAvailable int, inBattle, ex bool, gymID string, tileMode int) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)

	tz := geo.GetTimezone(lat, lon)
	m["conqueredTime"] = geo.FormatNow(tz, e.TimeLayout)
	m["tth"] = geo.TTH{Hours: 1}
	addSunTimes(m, lat, lon, tz)

	// Gym identity fields for DTS templates
	m["gymId"] = gymID

	// Slot and team fields for DTS templates
	m["slotsAvailable"] = slotsAvailable
	m["oldSlotsAvailable"] = oldSlotsAvailable
	m["trainerCount"] = 6 - slotsAvailable
	m["oldTrainerCount"] = 6 - oldSlotsAvailable
	m["teamId"] = teamID
	m["oldTeamId"] = oldTeamID
	m["ex"] = ex
	m["inBattle"] = inBattle

	// Icon URLs
	trainerCount := 6 - slotsAvailable
	if e.ImgUicons != nil {
		m["imgUrl"] = e.ImgUicons.GymIcon(teamID, trainerCount, inBattle, ex)
	}
	if e.ImgUiconsAlt != nil {
		m["imgUrlAlt"] = e.ImgUiconsAlt.GymIcon(teamID, trainerCount, inBattle, ex)
	}
	if e.StickerUicons != nil {
		m["stickerUrl"] = e.StickerUicons.GymIcon(teamID, trainerCount, inBattle, ex)
	}

	// Map URLs
	e.addMapURLs(m, lat, lon, "gyms", gymID)

	// Reverse geocoding
	e.addGeoResult(m, lat, lon)

	// Static map tile
	pending := e.addStaticMap(m, "gym", lat, lon, map[string]any{
		"team_id":        teamID,
		"slotsAvailable": slotsAvailable,
		"inBattle":       inBattle,
		"ex":             ex,
	}, tileMode)

	// Game data enrichment
	if e.GameData != nil {
		if info, ok := e.GameData.Util.Teams[teamID]; ok {
			m["gymColor"] = info.Color
			m["color"] = info.Color // deprecated alias
		}
	}

	e.setFallbackImg(m, e.FallbackImgGym)

	return m, pending
}

// GymTranslate adds per-language translated fields.
// lastOwnerID is the team ID of the last controller (from webhook last_owner_id).
func (e *Enricher) GymTranslate(base map[string]any, teamID, oldTeamID, lastOwnerID int, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return nil
	}

	m := make(map[string]any, 8) // only translated fields; caller merges base + perLang

	tr := e.Translations.For(lang)
	addTeamFields(m, e.GameData, tr, teamID)
	if oldTeamID >= 0 {
		if info, ok := e.GameData.Util.Teams[oldTeamID]; ok {
			m["oldTeamName"] = tr.T(info.Name)
			m["oldTeamNameEng"] = info.Name
			m["oldTeamEmojiKey"] = info.Emoji
		}
	}
	// Previous controller's team (from webhook last_owner_id — may differ from old_team_id)
	m["previousControlId"] = lastOwnerID
	if lastOwnerID >= 0 {
		if info, ok := e.GameData.Util.Teams[lastOwnerID]; ok {
			m["previousControlName"] = tr.T(info.Name)
			m["previousControlNameEng"] = info.Name
		}
	}

	return m
}
