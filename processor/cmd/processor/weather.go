package main

import (
	"encoding/json"
	"maps"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessWeather(raw json.RawMessage) error {
	if ps.cfg.General.DisableWeather {
		return nil
	}

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
		minAlert := int64(ps.cfg.General.AlertMinimumTime)
		now := time.Now().Unix()
		for _, u := range caringUsers {
			// Squash clean weather alerts whose TTH (CaresUntil) is too short
			// to be usefully tracked. Without this, the alert ships with
			// Clean=1 but a TTL of a few seconds — the delivery queue either
			// drops it on the "TTL already expired" path or the user sees a
			// message that vanishes almost immediately.
			if u.Clean > 0 {
				remaining := u.CaresUntil - now
				if u.CaresUntil == 0 || remaining < minAlert {
					l.Debugf("Weather alert suppressed for %s (%s) — TTH %ds below alert_minimum_time %ds",
						u.Name, u.ID, remaining, minAlert)
					continue
				}
			}
			mu := webhook.MatchedUser{
				ID:         u.ID,
				Name:       u.Name,
				Type:       u.Type,
				Language:   u.Language,
				Template:   u.Template,
				Clean:      u.Clean,
				Ping:       u.Ping,
				CaresUntil: u.CaresUntil,
			}

			// Attach active pokemon affected by this weather change.
			// show_altered_pokemon is a display-content flag (see
			// config.example.toml: "track weather changed pokemon to show
			// in DTS"), NOT a filter. An empty affected list must not
			// suppress the alert — the weather change itself is the news.
			if ps.activePokemon != nil {
				affected := ps.activePokemon.GetAffectedPokemon(
					change.S2CellID, u.ID,
					change.OldGameplayCondition, change.GameplayCondition,
					ps.cfg.Weather.ShowAlteredPokemonMaxCount,
				)
				if len(affected) > 0 {
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
			}

			matched = append(matched, mu)
		}

		matched = ps.filterBlocked(matched)

		if len(matched) == 0 {
			l.Debugf("Weather changed to %d (from %d, source=%s) but all caring users were filtered (rate limit / blocked alerts / clean TTH)",
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

		l.Infof("Weather changed %s -> %s (source=%s) areas(%s) and %d users care",
			ps.weatherName(change.OldGameplayCondition), ps.weatherName(change.GameplayCondition),
			change.Source, areaNames(matchedAreas), len(matched))

		// Build weather change message
		msg, _ := json.Marshal(change)
		mode := ps.tileMode("weatherchange", matched)
		baseEnrichment, baseTilePending := ps.enricher.Weather(change.Latitude, change.Longitude, change.GameplayCondition, change.Coords, ps.cfg.Weather.ShowAlteredPokemonStaticMap, mode)

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
				userMode := ps.tileMode("weatherchange", []webhook.MatchedUser{user})
				langEnrichment, userTilePending = ps.enricher.WeatherTranslate(
					baseEnrichment,
					change.OldGameplayCondition,
					change.GameplayCondition,
					user.ActivePokemons,
					lang,
					ps.cfg.Weather.ShowAlteredPokemonStaticMap,
					userMode,
				)
				perLang = map[string]map[string]any{lang: langEnrichment}
			}

			// For clean weather alerts, TTH aligns with the longest TTH of any
			// matched pokemon (CaresUntil). This is maintained by the weather
			// care tracker independently of show_altered_pokemon, so clean
			// weather alerts get a correct TTH even when the active-pokemon
			// tracker is disabled. Users below the min-alert threshold were
			// already dropped when matched was built.
			userEnrichment := baseEnrichment
			if user.Clean > 0 && user.CaresUntil > 0 {
				// Copy base enrichment to avoid mutating shared map
				userEnrichment = make(map[string]any, len(baseEnrichment)+1)
				maps.Copy(userEnrichment, baseEnrichment)
				userEnrichment["tth"] = geo.ComputeTTH(user.CaresUntil)
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
