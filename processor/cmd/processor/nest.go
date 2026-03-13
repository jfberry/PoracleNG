package main

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessNest(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var nest webhook.NestWebhook
		if err := json.Unmarshal(raw, &nest); err != nil {
			log.Errorf("Failed to parse nest webhook: %s", err)
			return
		}

		l := log.WithField("ref", nest.NestID)

		// Duplicate check
		if ps.duplicates.CheckNest(nest.NestID, nest.PokemonID, nest.ResetTime) {
			l.Debug("Nest duplicate, ignoring")
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

		if len(matched) > 0 {
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
