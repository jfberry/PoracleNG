package main

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessPokemon(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	metrics.WorkerPoolInUse.Inc()
	ps.wg.Add(1)
	go func() {
		start := time.Now()
		defer func() {
			metrics.WebhookProcessingDuration.WithLabelValues("pokemon").Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

		var pokemon webhook.PokemonWebhook
		if err := json.Unmarshal(raw, &pokemon); err != nil {
			log.Errorf("Failed to parse pokemon webhook: %s", err)
			return
		}

		l := log.WithField("ref", pokemon.EncounterID)

		// Record for rarity and shiny tracking
		ivScanned := pokemon.IndividualAttack != nil
		isShiny := pokemon.Shiny != nil && *pokemon.Shiny
		ps.stats.RecordSighting(pokemon.PokemonID, ivScanned, isShiny)

		// Duplicate check
		verified := pokemon.Verified || pokemon.DisappearTimeVerified
		if ps.duplicates.CheckPokemon(pokemon.EncounterID, verified, pokemon.CP, pokemon.DisappearTime) {
			l.Debug("Wild encounter was sent again too soon, ignoring")
			metrics.DuplicatesSkipped.WithLabelValues("pokemon").Inc()
			return
		}

		// Weather inference
		if pokemon.Weather > 0 && ps.cfg.Weather.EnableInference {
			cellID := tracker.GetWeatherCellID(pokemon.Latitude, pokemon.Longitude)
			ps.weather.CheckWeatherOnMonster(cellID, pokemon.Latitude, pokemon.Longitude, pokemon.Weather)
		}

		// Encounter tracking (change detection)
		atk, def, sta := 0, 0, 0
		if pokemon.IndividualAttack != nil {
			atk = *pokemon.IndividualAttack
		}
		if pokemon.IndividualDefense != nil {
			def = *pokemon.IndividualDefense
		}
		if pokemon.IndividualStamina != nil {
			sta = *pokemon.IndividualStamina
		}
		weather := pokemon.Weather
		if pokemon.BoostedWeather > 0 {
			weather = pokemon.BoostedWeather
		}
		encounterState := tracker.EncounterState{
			PokemonID:     pokemon.PokemonID,
			Form:          pokemon.Form,
			Weather:       weather,
			CP:            pokemon.CP,
			ATK:           atk,
			DEF:           def,
			STA:           sta,
			DisappearTime: pokemon.DisappearTime,
		}
		_, change := ps.encounters.Track(pokemon.EncounterID, encounterState)

		// Get rarity group
		rarityGroup := ps.stats.GetRarityGroup(pokemon.PokemonID)

		// Process pokemon into matching format
		processed := matching.ProcessPokemonWebhook(&pokemon, rarityGroup, ps.pvpCfg)

		// Match
		st := ps.stateMgr.Get()
		matched := ps.pokemonMatcher.Match(processed, st)
		matched = ps.filterRateLimited(matched)

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("pokemon").Inc()
			metrics.MatchedUsers.WithLabelValues("pokemon").Add(float64(len(matched)))

			// Get matched areas for the alerter
			areas := st.Geofence.PointInAreas(pokemon.Latitude, pokemon.Longitude)
			matchedAreas := make([]webhook.MatchedArea, len(areas))
			for i, a := range areas {
				matchedAreas[i] = webhook.MatchedArea{
					Name:             a.Name,
					DisplayInMatches: a.DisplayInMatches,
					Group:            a.Group,
				}
			}

			// Register matched users as caring about weather in this cell
			if ps.cfg.Weather.ChangeAlert {
				cellID := tracker.GetWeatherCellID(pokemon.Latitude, pokemon.Longitude)
				for _, u := range matched {
					ps.weatherCares.Register(cellID, tracker.WeatherCareEntry{
						ID:         u.ID,
						Name:       u.Name,
						Type:       u.Type,
						Language:   u.Language,
						Template:   u.Template,
						Clean:      u.Clean,
						Ping:       u.Ping,
						CaresUntil: pokemon.DisappearTime,
					})
				}

				// Track active pokemon per user for weather change alerts
				if ps.activePokemon != nil {
					types := ps.pokemonTypes.GetTypes(pokemon.PokemonID, pokemon.Form)
					pokWeather := pokemon.BoostedWeather
					if pokWeather == 0 {
						pokWeather = pokemon.Weather
					}
					for _, u := range matched {
						ps.activePokemon.Register(cellID, u.ID, pokemon.EncounterID, tracker.ActivePokemon{
							PokemonID:     pokemon.PokemonID,
							Form:          pokemon.Form,
							IV:            processed.IV,
							CP:            processed.CP,
							Latitude:      pokemon.Latitude,
							Longitude:     pokemon.Longitude,
							DisappearTime: pokemon.DisappearTime,
							Weather:       pokWeather,
							Types:         types,
						})
					}
				}
			}

			l.Infof("Pokemon %d appeared at [%.3f,%.3f] and %d humans cared",
				pokemon.PokemonID, pokemon.Latitude, pokemon.Longitude, len(matched))

			baseEnrichment := ps.enricher.Pokemon(&pokemon, processed)

			// Compute per-language translated enrichment
			var perLang map[string]map[string]any
			if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
				perLang = make(map[string]map[string]any)
				for _, lang := range distinctLanguages(matched) {
					perLang[lang] = ps.enricher.PokemonTranslate(baseEnrichment, &pokemon, lang)
				}
			}

			var perUser map[string]map[string]any
			if ps.enricher.PVPDisplay != nil && perLang != nil {
				perUser = ps.enricher.PokemonPerUser(perLang, matched)
			}

			ps.sender.Send(webhook.OutboundPayload{
				Type:                  "pokemon",
				Message:               raw,
				Enrichment:            baseEnrichment,
				PerLanguageEnrichment: perLang,
				PerUserEnrichment:     perUser,
				MatchedAreas:          matchedAreas,
				MatchedUsers:          matched,
			})
		} else {
			l.Debugf("Pokemon %d appeared at [%.3f,%.3f] and 0 humans cared",
				pokemon.PokemonID, pokemon.Latitude, pokemon.Longitude)
		}

		// Handle pokemon change
		if change != nil {
			ps.handlePokemonChange(l, raw, change, st)
		}
	}()
	return nil
}

func (ps *ProcessorService) handlePokemonChange(l *log.Entry, raw json.RawMessage, change *tracker.EncounterChange, st *state.State) {
	// Re-match with new state and send as pokemon_changed
	oldIV := float64(change.Old.ATK+change.Old.DEF+change.Old.STA) / 0.45

	l.Infof("Pokemon changed from %d to %d", change.Old.PokemonID, change.New.PokemonID)

	ps.sender.Send(webhook.OutboundPayload{
		Type:    "pokemon_changed",
		Message: raw,
		OldState: &webhook.EncounterOld{
			PokemonID: change.Old.PokemonID,
			Form:      change.Old.Form,
			Weather:   change.Old.Weather,
			CP:        change.Old.CP,
			IV:        oldIV,
		},
	})
}
