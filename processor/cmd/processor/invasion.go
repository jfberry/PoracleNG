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
	if ps.cfg.General.DisableInvasion {
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

		// Resolve the raw grunt type integer for enrichment lookups
		gruntTypeID := inv.IncidentGruntType
		if gruntTypeID == 0 || gruntTypeID == 352 {
			gruntTypeID = inv.GruntType
		}

		// Resolve type name for matching against tracking rules.
		// The !invasion command stores the English type name lowercased (e.g. "electric")
		// or event name (e.g. "kecleon"). Two O(1) map lookups — fine for invasion volume.
		gruntType := matching.ResolveGruntTypeName(gruntTypeID, displayType, ps.enricher.GameData)

		// Gender + boss flag come from game data (the grunt definition), not
		// the webhook. Golbat doesn't send those in invasion webhooks.
		gender := inv.Gender
		boss := false
		if ps.enricher.GameData != nil {
			if grunt, ok := ps.enricher.GameData.Grunts[gruntTypeID]; ok {
				if gender == 0 {
					gender = grunt.Gender
				}
				boss = grunt.Boss
			}
		}

		data := &matching.InvasionData{
			PokestopID: inv.PokestopID,
			GruntType:  gruntType,
			Boss:       boss,
			Gender:     gender,
			Latitude:   inv.Latitude,
			Longitude:  inv.Longitude,
		}

		st := ps.stateMgr.Get()
		matchStart := time.Now()
		matched := ps.invasionMatcher.Match(data, st)
		metrics.MatchingDuration.WithLabelValues("invasion").Observe(time.Since(matchStart).Seconds())
		matched = ps.filterBlocked(matched)

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("invasion").Inc()
			metrics.MatchedUsers.WithLabelValues("invasion").Add(float64(len(matched)))

			areas := st.Geofence.PointInAreas(inv.Latitude, inv.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Invasion grunt %s at %s [%.3f,%.3f] areas(%s) and %d humans cared",
				gruntType, inv.Name, inv.Latitude, inv.Longitude, areaNames(matchedAreas), len(matched))

			mode := ps.tileMode("invasion", matched)
			baseEnrichment, tilePending := ps.enricher.Invasion(inv.Latitude, inv.Longitude, expiration, inv.PokestopID, gruntTypeID, displayType, 0, mode)

			// Compute per-language translated enrichment
			var perLang map[string]map[string]any
			if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
				perLang = make(map[string]map[string]any)
				for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
					perLang[lang] = ps.enricher.InvasionTranslate(baseEnrichment, gruntTypeID, inv.Lineup, lang)
				}
			}

			if ps.renderCh == nil {
				return
			}
			webhookFields := parseWebhookFields(raw)

			ps.renderCh <- RenderJob{
				TemplateType:      "invasion",
				Enrichment:        baseEnrichment,
				PerLangEnrichment: perLang,
				WebhookFields:     webhookFields,
				MatchedUsers:      matched,
				MatchedAreas:      matchedAreas,
				TilePending:       tilePending,
				LogReference:      inv.PokestopID,
			}
		} else {
			l.Debugf("Invasion grunt %s at %s [%.3f,%.3f] and 0 humans cared",
				gruntType, inv.Name, inv.Latitude, inv.Longitude)
		}
	}()
	return nil
}
