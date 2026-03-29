package main

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/dts"
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

// consumeWeatherChanges reads weather change events and forwards them to the alerter
// with the list of users who care about that cell.
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

		// Merge raw webhook fields into enrichment (templates access both)
		mergeWebhookFields(baseEnrichment, msg)

		// Per-user: each gets their own payload with per-language enrichment and tile
		if ps.dtsRenderer != nil {
			// Resolve base tile
			if baseTilePending != nil {
				wait := time.Until(baseTilePending.Deadline)
				if wait <= 0 {
					wait = time.Millisecond
				}
				select {
				case url := <-baseTilePending.Result:
					baseTilePending.Apply(url)
				case <-time.After(wait):
					baseTilePending.Apply(baseTilePending.Fallback)
				}
			}

			var allJobs []dts.DeliveryJob
			for _, user := range matched {
				lang := user.Language
				if lang == "" {
					lang = ps.cfg.General.Locale
					if lang == "" {
						lang = "en"
					}
				}

				var perLang map[string]map[string]any
				if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
					langEnrichment, userTilePending := ps.enricher.WeatherTranslate(
						baseEnrichment,
						change.OldGameplayCondition,
						change.GameplayCondition,
						user.ActivePokemons,
						lang,
						ps.cfg.Weather.ShowAlteredPokemonStaticMap,
					)
					if userTilePending != nil {
						wait := time.Until(userTilePending.Deadline)
						if wait <= 0 {
							wait = time.Millisecond
						}
						select {
						case url := <-userTilePending.Result:
							userTilePending.Apply(url)
						case <-time.After(wait):
							userTilePending.Apply(userTilePending.Fallback)
						}
					}
					perLang = map[string]map[string]any{lang: langEnrichment}
				}

				jobs := ps.dtsRenderer.RenderAlert(
					"weatherchange",
					baseEnrichment,
					perLang,
					[]webhook.MatchedUser{user},
					matchedAreas,
					change.S2CellID,
				)
				allJobs = append(allJobs, jobs...)
			}
			if len(allJobs) > 0 {
				if err := ps.sender.DeliverMessages(allJobs); err != nil {
					l.Errorf("Failed to deliver weather messages: %s", err)
				}
			}
		} else {
			for _, user := range matched {
				lang := user.Language
				if lang == "" {
					lang = ps.cfg.General.Locale
					if lang == "" {
						lang = "en"
					}
				}

				var perLang map[string]map[string]any
				var tilePending = baseTilePending
				if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
					langEnrichment, userTilePending := ps.enricher.WeatherTranslate(
						baseEnrichment,
						change.OldGameplayCondition,
						change.GameplayCondition,
						user.ActivePokemons,
						lang,
						ps.cfg.Weather.ShowAlteredPokemonStaticMap,
					)
					if userTilePending != nil {
						tilePending = userTilePending
					}
					perLang = map[string]map[string]any{lang: langEnrichment}
				}

				ps.sender.Send(webhook.OutboundPayload{
					Type:                  "weather_change",
					Message:               msg,
					Enrichment:            baseEnrichment,
					PerLanguageEnrichment: perLang,
					MatchedAreas:          matchedAreas,
					MatchedUsers:          []webhook.MatchedUser{user},
					TilePending:           tilePending,
				})
			}
		}
	}
}
