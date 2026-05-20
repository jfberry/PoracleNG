package main

import (
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessLure(raw json.RawMessage) error {
	if ps.cfg.General.DisableLure {
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
			metrics.WebhookProcessingDuration.WithLabelValues("lure").Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

		var lure webhook.LureWebhook
		if err := json.Unmarshal(raw, &lure); err != nil {
			log.Errorf("Failed to parse lure webhook: %s", err)
			return
		}

		l := log.WithField("ref", lure.PokestopID)

		// Duplicate check
		if lure.LureExpiration > 0 && ps.duplicates.CheckLure(lure.PokestopID, lure.LureExpiration) {
			l.Debug("Lure duplicate, ignoring")
			metrics.DuplicatesSkipped.WithLabelValues("lure").Inc()
			return
		}

		data := &matching.LureData{
			PokestopID: lure.PokestopID,
			LureID:     lure.LureID,
			Latitude:   lure.Latitude,
			Longitude:  lure.Longitude,
		}

		st := ps.stateMgr.Get()
		matched, matchedAreas := ps.lureMatcher.Match(data, st)
		matched = ps.filterBlocked(matched)
		matched = ps.filterValidation("pokestop", raw, matchedAreas, matched)
		matched = ps.filterMuted(matched, matchedAreas, mute.Event{PokestopID: lure.PokestopID})

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("lure").Inc()
			metrics.MatchedUsers.WithLabelValues("lure").Add(float64(len(matched)))
			metrics.IntervalMatched.Add(1)

			l.Infof("%s at %s [%.3f,%.3f] areas(%s) and %d humans cared",
				ps.lureName(lure.LureID), lure.Name, lure.Latitude, lure.Longitude, areaNames(matchedAreas), len(matched))

			mode := ps.tileMode("lure", matched)
			enrichmentData, tilePending := ps.enricher.Lure(&lure, mode)

			// Compute per-language translated enrichment
			var perLang map[string]map[string]any
			if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
				perLang = make(map[string]map[string]any)
				for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
					perLang[lang] = ps.enricher.LureTranslate(enrichmentData, lure.LureID, lang)
				}
			}

			if ps.renderCh == nil {
				return
			}
			webhookFields := parseWebhookFields(raw)

			ps.renderCh <- RenderJob{
				AlertType:         "lure",
				TemplateType:      "lure",
				Enrichment:        enrichmentData,
				PerLangEnrichment: perLang,
				WebhookFields:     webhookFields,
				MatchedUsers:      matched,
				MatchedAreas:      matchedAreas,
				TileGate:          ps.newTileGate(tilePending),
				LogReference:      lure.PokestopID,
				// Edit key for users with Clean bit 2: a revised lure
				// expiration on the same (pokestop, lure_id) edits
				// the prior message in place.
				EditKey: fmt.Sprintf("lure:%s:%d", lure.PokestopID, lure.LureID),
			}
		} else {
			l.Debugf("%s at %s [%.3f,%.3f] and 0 humans cared",
				ps.lureName(lure.LureID), lure.Name, lure.Latitude, lure.Longitude)
		}
	}()
	return nil
}
