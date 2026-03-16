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
	ps.workerPool <- struct{}{}
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
		matched := ps.maxbattleMatcher.Match(data, st)

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("maxbattle").Inc()
			metrics.MatchedUsers.WithLabelValues("maxbattle").Add(float64(len(matched)))

			areas := st.Geofence.PointInAreas(mb.Latitude, mb.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Maxbattle level %d pokemon %d at %s and %d humans cared",
				mb.BattleLevel, mb.BattlePokemonID, mb.Name, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "maxbattle",
				Message:      raw,
				Enrichment:   ps.enricher.Maxbattle(mb.Latitude, mb.Longitude, mb.BattleEnd),
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Maxbattle level %d pokemon %d at %s and 0 humans cared",
				mb.BattleLevel, mb.BattlePokemonID, mb.Name)
		}
	}()
	return nil
}
