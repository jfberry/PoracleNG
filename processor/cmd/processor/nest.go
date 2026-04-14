package main

import (
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessNest(raw json.RawMessage) error {
	if ps.cfg.General.DisableNest {
		return nil
	}

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
			metrics.WebhookProcessingDuration.WithLabelValues("nest").Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

		var nest webhook.NestWebhook
		if err := json.Unmarshal(raw, &nest); err != nil {
			log.Errorf("Failed to parse nest webhook: %s", err)
			return
		}

		l := log.WithField("ref", nest.NestID)

		// Duplicate check
		if ps.duplicates.CheckNest(nest.NestID, nest.PokemonID, nest.ResetTime) {
			l.Debug("Nest duplicate, ignoring")
			metrics.DuplicatesSkipped.WithLabelValues("nest").Inc()
			return
		}

		data := &matching.NestData{
			NestID:     nest.NestID,
			PokemonID:  nest.PokemonID,
			Form:       nest.Form,
			PokemonAvg: nest.PokemonAvg,
			Latitude:   nest.Lat,
			Longitude:  nest.Lon,
		}

		st := ps.stateMgr.Get()
		matchStart := time.Now()
		matched := ps.nestMatcher.Match(data, st)
		metrics.MatchingDuration.WithLabelValues("nest").Observe(time.Since(matchStart).Seconds())
		matched = ps.filterRateLimited(matched)

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("nest").Inc()
			metrics.MatchedUsers.WithLabelValues("nest").Add(float64(len(matched)))

			areas := st.Geofence.PointInAreas(nest.Lat, nest.Lon)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Nest %s (avg %.1f/hr) areas(%s) and %d humans cared",
				ps.pokemonName(nest.PokemonID, nest.Form), nest.PokemonAvg, areaNames(matchedAreas), len(matched))

			mode := ps.tileMode("nest", matched)
			enrichmentData, tilePending := ps.enricher.Nest(&nest, mode)

			// Compute per-language translated enrichment
			var perLang map[string]map[string]any
			if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
				perLang = make(map[string]map[string]any)
				for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
					perLang[lang] = ps.enricher.NestTranslate(enrichmentData, nest.PokemonID, nest.Form, lang)
				}
			}

			if ps.renderCh == nil {
				return
			}
			webhookFields := parseWebhookFields(raw)

			ps.renderCh <- RenderJob{
				TemplateType:      "nest",
				Enrichment:        enrichmentData,
				PerLangEnrichment: perLang,
				WebhookFields:     webhookFields,
				MatchedUsers:      matched,
				MatchedAreas:      matchedAreas,
				TilePending:       tilePending,
				LogReference:      fmt.Sprintf("%d", nest.NestID),
			}
		} else {
			l.Debugf("Nest %s (avg %.1f/hr) and 0 humans cared",
				ps.pokemonName(nest.PokemonID, nest.Form), nest.PokemonAvg)
		}
	}()
	return nil
}
