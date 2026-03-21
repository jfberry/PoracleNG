package main

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessNest(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
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
			Latitude:   nest.Latitude,
			Longitude:  nest.Longitude,
		}

		st := ps.stateMgr.Get()
		matched := ps.nestMatcher.Match(data, st)
		matched = ps.filterRateLimited(matched)

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("nest").Inc()
			metrics.MatchedUsers.WithLabelValues("nest").Add(float64(len(matched)))

			areas := st.Geofence.PointInAreas(nest.Latitude, nest.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Nest pokemon %d (avg %.1f) and %d humans cared",
				nest.PokemonID, nest.PokemonAvg, len(matched))

			enrichment := ps.enricher.Nest(&nest)

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "nest",
				Message:      raw,
				Enrichment:   enrichment,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Nest pokemon %d (avg %.1f) and 0 humans cared",
				nest.PokemonID, nest.PokemonAvg)
		}
	}()
	return nil
}
