package main

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessWeather(raw json.RawMessage) error {
	var weather webhook.WeatherWebhook
	if err := json.Unmarshal(raw, &weather); err != nil {
		log.Errorf("Failed to parse weather webhook: %s", err)
		return err
	}

	cellID := weather.S2CellID.String()
	if cellID == "" || cellID == "0" {
		cellID = tracker.GetWeatherCellID(weather.Latitude, weather.Longitude)
	}

	ps.weather.UpdateFromWebhook(cellID, weather.GameplayCondition, weather.Updated, weather.Latitude, weather.Longitude, weather.Polygon)
	return nil
}

// consumeWeatherChanges reads weather change events and processes them for
// matching and delivery.
func (ps *ProcessorService) consumeWeatherChanges() {
	for change := range ps.weather.Changes() {
		l := log.WithField("ref", change.S2CellID)

		caringUsers := ps.weatherCares.GetCaringUsers(change.S2CellID)
		if len(caringUsers) == 0 {
			l.Debugf("Weather changed to %d (from %d, source=%s) but no users care",
				change.GameplayCondition, change.OldGameplayCondition, change.Source)
			continue
		}

		l.Debugf("Weather changed to %d (from %d, source=%s) and %d users care, checking for affected pokemon",
			change.GameplayCondition, change.OldGameplayCondition, change.Source, len(caringUsers))

		// Build matched users, skipping those with no affected pokemon
		var matched []webhook.MatchedUser
		for _, u := range caringUsers {
			mu := webhook.MatchedUser{
				ID:       u.ID,
				Name:     u.Name,
				Type:     u.Type,
				Language: u.Language,
				Template: u.Template,
				Clean:    u.Clean,
				Ping:     u.Ping,
			}

			// Attach active pokemon affected by this weather change
			if ps.activePokemon != nil {
				affected := ps.activePokemon.GetAffectedPokemon(
					change.S2CellID, u.ID,
					change.OldGameplayCondition, change.GameplayCondition,
					ps.cfg.Weather.ShowAlteredPokemonMaxCount,
				)
				if len(affected) == 0 {
					l.Debugf("User %s (%s) cares about cell but has no affected pokemon, skipping", u.Name, u.ID)
					continue
				}
				entries := make([]webhook.ActivePokemonEntry, len(affected))
				for j, ap := range affected {
					entries[j] = webhook.ActivePokemonEntry{
						PokemonID:     ap.PokemonID,
						Form:          ap.Form,
						IV:            ap.IV,
						CP:            ap.CP,
						Latitude:      ap.Latitude,
						Longitude:     ap.Longitude,
						DisappearTime: ap.DisappearTime,
					}
				}
				mu.ActivePokemons = entries
			}

			matched = append(matched, mu)
		}

		matched = ps.filterRateLimited(matched)

		if len(matched) == 0 {
			l.Debugf("Weather changed to %d (from %d, source=%s) but no users have affected pokemon",
				change.GameplayCondition, change.OldGameplayCondition, change.Source)
			continue
		}

		// Build matched areas from cell center
		st := ps.stateMgr.Get()
		areas := st.Geofence.PointInAreas(change.Latitude, change.Longitude)
		matchedAreas := make([]webhook.MatchedArea, len(areas))
		for i, a := range areas {
			matchedAreas[i] = webhook.MatchedArea{
				Name:             a.Name,
				DisplayInMatches: a.DisplayInMatches,
				Group:            a.Group,
			}
		}

		l.Infof("Weather changed %s -> %s (source=%s) areas(%s) and %d users have affected pokemon",
			ps.weatherName(change.OldGameplayCondition), ps.weatherName(change.GameplayCondition),
			change.Source, areaNames(matchedAreas), len(matched))

		// Build weather change message
		msg, _ := json.Marshal(change)
		baseEnrichment, baseTilePending := ps.enricher.Weather(change.Latitude, change.Longitude, change.GameplayCondition, change.Coords, ps.cfg.Weather.ShowAlteredPokemonStaticMap)

		// Per-user: each gets their own render job with per-language enrichment and tile
		if ps.renderCh == nil {
			continue
		}

		webhookFields := parseWebhookFields(msg)

		for _, user := range matched {
			lang := user.Language
			if lang == "" {
				lang = ps.cfg.General.Locale
				if lang == "" {
					lang = "en"
				}
			}

			var perLang map[string]map[string]any
			var userTilePending *staticmap.TilePending
			if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
				var langEnrichment map[string]any
				langEnrichment, userTilePending = ps.enricher.WeatherTranslate(
					baseEnrichment,
					change.OldGameplayCondition,
					change.GameplayCondition,
					user.ActivePokemons,
					lang,
					ps.cfg.Weather.ShowAlteredPokemonStaticMap,
				)
				perLang = map[string]map[string]any{lang: langEnrichment}
			}

			// For clean weather alerts, TTH is the latest pokemon despawn time.
			// This ensures the weather change message is cleaned when the last
			// affected pokemon despawns (not at the weather hour boundary).
			userEnrichment := baseEnrichment
			if user.Clean && len(user.ActivePokemons) > 0 {
				var maxDespawn int64
				for _, ap := range user.ActivePokemons {
					if ap.DisappearTime > maxDespawn {
						maxDespawn = ap.DisappearTime
					}
				}
				if maxDespawn > 0 {
					// Copy base enrichment to avoid mutating shared map
					userEnrichment = make(map[string]any, len(baseEnrichment)+1)
					for k, v := range baseEnrichment {
						userEnrichment[k] = v
					}
					userEnrichment["tth"] = geo.ComputeTTH(maxDespawn)
				}
			}

			// Use per-user tile if available, otherwise base tile
			tp := baseTilePending
			if userTilePending != nil {
				tp = userTilePending
			}

			ps.renderCh <- RenderJob{
				TemplateType:      "weatherchange",
				Enrichment:        userEnrichment,
				PerLangEnrichment: perLang,
				WebhookFields:     webhookFields,
				MatchedUsers:      []webhook.MatchedUser{user},
				MatchedAreas:      matchedAreas,
				TilePending:       tp,
				LogReference:      change.S2CellID,
			}
		}
	}
}
