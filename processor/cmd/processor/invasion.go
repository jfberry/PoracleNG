package main

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessInvasion(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	metrics.WorkerPoolInUse.Inc()
	ps.wg.Add(1)
	go func() {
		start := time.Now()
		defer func() {
			metrics.WebhookProcessingDuration.WithLabelValues("invasion").Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

		var inv webhook.InvasionWebhook
		if err := json.Unmarshal(raw, &inv); err != nil {
			log.Errorf("Failed to parse invasion webhook: %s", err)
			return
		}

		l := log.WithField("ref", inv.PokestopID)

		// Resolve expiration
		expiration := inv.IncidentExpiration
		if expiration == 0 {
			expiration = inv.IncidentExpireTimestamp
		}

		// Duplicate check
		if expiration > 0 && ps.duplicates.CheckInvasion(inv.PokestopID, expiration) {
			l.Debug("Invasion duplicate, ignoring")
			metrics.DuplicatesSkipped.WithLabelValues("invasion").Inc()
			return
		}

		// Resolve grunt type and display type
		displayType := inv.DisplayType
		if displayType == 0 {
			displayType = inv.IncidentDisplayType
		}
		gruntType := matching.ResolveGruntType(inv.IncidentGruntType, inv.GruntType, displayType)

		// Resolve the raw grunt type integer for enrichment lookups
		gruntTypeID := inv.IncidentGruntType
		if gruntTypeID == 0 || gruntTypeID == 352 {
			gruntTypeID = inv.GruntType
		}

		data := &matching.InvasionData{
			PokestopID: inv.PokestopID,
			GruntType:  gruntType,
			Gender:     inv.Gender,
			Latitude:   inv.Latitude,
			Longitude:  inv.Longitude,
		}

		st := ps.stateMgr.Get()
		matched := ps.invasionMatcher.Match(data, st)
		matched = ps.filterRateLimited(matched)

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("invasion").Inc()
			metrics.MatchedUsers.WithLabelValues("invasion").Add(float64(len(matched)))

			areas := st.Geofence.PointInAreas(inv.Latitude, inv.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Invasion grunt %s at %s and %d humans cared",
				gruntType, inv.Name, len(matched))

			baseEnrichment := ps.enricher.Invasion(inv.Latitude, inv.Longitude, expiration, inv.PokestopID, gruntTypeID, displayType, 0)

			// Compute per-language translated enrichment
			var perLang map[string]map[string]any
			if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
				perLang = make(map[string]map[string]any)
				for _, lang := range distinctLanguages(matched) {
					perLang[lang] = ps.enricher.InvasionTranslate(baseEnrichment, gruntTypeID, lang)
				}
			}

			ps.sender.Send(webhook.OutboundPayload{
				Type:                  "invasion",
				Message:               raw,
				Enrichment:            baseEnrichment,
				PerLanguageEnrichment: perLang,
				MatchedAreas:          matchedAreas,
				MatchedUsers:          matched,
			})
		} else {
			l.Debugf("Invasion grunt %s at %s and 0 humans cared", gruntType, inv.Name)
		}
	}()
	return nil
}
