package main

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessFortUpdate(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var fort webhook.FortWebhook
		if err := json.Unmarshal(raw, &fort); err != nil {
			log.Errorf("Failed to parse fort_update webhook: %s", err)
			return
		}

		l := log.WithField("ref", fort.ID)

		data := &matching.FortData{
			ID:          fort.ID,
			FortType:    fort.FortType,
			IsEmpty:     fort.IsEmpty,
			ChangeTypes: fort.ChangeTypes,
			Latitude:    fort.Latitude,
			Longitude:   fort.Longitude,
		}

		st := ps.stateMgr.Get()
		matched := ps.fortMatcher.Match(data, st)

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(fort.Latitude, fort.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Fort update %s (%s) and %d humans cared",
				fort.Name, fort.FortType, len(matched))

			enrichment := ps.enricher.Fort(&fort, fort.ResetTime)

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "fort_update",
				Message:      raw,
				Enrichment:   enrichment,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Fort update %s (%s) and 0 humans cared", fort.Name, fort.FortType)
		}
	}()
	return nil
}
