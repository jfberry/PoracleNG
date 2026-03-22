package main

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessFortUpdate(raw json.RawMessage) error {
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
			metrics.WebhookProcessingDuration.WithLabelValues("fort_update").Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

		var fort webhook.FortWebhook
		if err := json.Unmarshal(raw, &fort); err != nil {
			log.Errorf("Failed to parse fort_update webhook: %s", err)
			return
		}

		lat := fort.Latitude()
		lon := fort.Longitude()
		fortID := fort.FortID()

		if fortID == "" {
			log.Warn("Fort update webhook has no fort ID, skipping")
			return
		}

		l := log.WithField("ref", fortID)

		data := &matching.FortData{
			ID:          fortID,
			FortType:    fort.FortType(),
			IsEmpty:     fort.IsEmpty(),
			ChangeTypes: fort.AllChangeTypes(),
			Latitude:    lat,
			Longitude:   lon,
		}

		st := ps.stateMgr.Get()
		matched := ps.fortMatcher.Match(data, st)
		matched = ps.filterRateLimited(matched)

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("fort_update").Inc()
			metrics.MatchedUsers.WithLabelValues("fort_update").Add(float64(len(matched)))

			areas := st.Geofence.PointInAreas(lat, lon)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Fort update %s (%s, %s) and %d humans cared",
				fort.FortName(), fort.FortType(), fort.ChangeType, len(matched))

			enrichment := ps.enricher.FortUpdate(lat, lon, fortID, &fort)

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "fort_update",
				Message:      raw,
				Enrichment:   enrichment,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Fort update %s (%s, %s) and 0 humans cared",
				fort.FortName(), fort.FortType(), fort.ChangeType)
		}
	}()
	return nil
}
