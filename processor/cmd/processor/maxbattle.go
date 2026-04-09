package main

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessMaxbattle(raw json.RawMessage) error {
	select {
	case ps.workerPool <- struct{}{}:
	case <-ps.ctx.Done():
		return nil
	}
	metrics.WorkerPoolInUse.Inc()
	ps.wg.Add(1)
	go func() {
		start := time.Now()
		defer func() {
			metrics.WebhookProcessingDuration.WithLabelValues("maxbattle").Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

		var mb webhook.MaxbattleWebhook
		if err := json.Unmarshal(raw, &mb); err != nil {
			log.Errorf("Failed to parse maxbattle webhook: %s", err)
			return
		}

		l := log.WithField("ref", mb.ID)

		// Duplicate check
		if ps.duplicates.CheckMaxbattle(mb.ID, mb.BattleEnd, mb.BattlePokemonID) {
			l.Debug("Maxbattle duplicate, ignoring")
			metrics.DuplicatesSkipped.WithLabelValues("maxbattle").Inc()
			return
		}

		// Derive gmax from battle level
		gmax := 0
		if mb.BattleLevel > 6 {
			gmax = 1
		}

		data := &matching.MaxbattleData{
			StationID: mb.ID,
			PokemonID: mb.BattlePokemonID,
			Form:      mb.BattlePokemonForm,
			Level:     mb.BattleLevel,
			Gmax:      gmax,
			Evolution: 0,
			Move1:     mb.BattlePokemonMove1,
			Move2:     mb.BattlePokemonMove2,
			Latitude:  mb.Latitude,
			Longitude: mb.Longitude,
		}

		st := ps.stateMgr.Get()
		matchStart := time.Now()
		matched := ps.maxbattleMatcher.Match(data, st)
		metrics.MatchingDuration.WithLabelValues("maxbattle").Observe(time.Since(matchStart).Seconds())
		matched = ps.filterRateLimited(matched)

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("maxbattle").Inc()
			metrics.MatchedUsers.WithLabelValues("maxbattle").Add(float64(len(matched)))

			areas := st.Geofence.PointInAreas(mb.Latitude, mb.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Maxbattle L%d %s at %s [%.3f,%.3f] areas(%s) and %d humans cared",
				mb.BattleLevel, ps.pokemonName(mb.BattlePokemonID, mb.BattlePokemonForm),
				mb.Name, mb.Latitude, mb.Longitude, areaNames(matchedAreas), len(matched))

			enrichment, tilePending := ps.enricher.Maxbattle(mb.Latitude, mb.Longitude, mb.BattleEnd, &mb)

			// Compute per-language translated enrichment
			var perLang map[string]map[string]any
			if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
				perLang = make(map[string]map[string]any)
				for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
					perLang[lang] = ps.enricher.MaxbattleTranslate(enrichment, &mb, lang)
				}
			}

			if ps.renderCh == nil {
				return
			}
			webhookFields := parseWebhookFields(raw)

			ps.renderCh <- RenderJob{
				TemplateType:      "maxbattle",
				Enrichment:        enrichment,
				PerLangEnrichment: perLang,
				WebhookFields:     webhookFields,
				MatchedUsers:      matched,
				MatchedAreas:      matchedAreas,
				TilePending:       tilePending,
				LogReference:      mb.ID,
			}
		} else {
			l.Debugf("Maxbattle L%d %s at %s [%.3f,%.3f] and 0 humans cared",
				mb.BattleLevel, ps.pokemonName(mb.BattlePokemonID, mb.BattlePokemonForm),
				mb.Name, mb.Latitude, mb.Longitude)
		}
	}()
	return nil
}
