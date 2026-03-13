package main

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessLure(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var lure webhook.LureWebhook
		if err := json.Unmarshal(raw, &lure); err != nil {
			log.Errorf("Failed to parse lure webhook: %s", err)
			return
		}

		l := log.WithField("ref", lure.PokestopID)

		// Duplicate check
		if lure.LureExpiration > 0 && ps.duplicates.CheckLure(lure.PokestopID, lure.LureExpiration) {
			l.Debug("Lure duplicate, ignoring")
			return
		}

		data := &matching.LureData{
			PokestopID: lure.PokestopID,
			LureID:     lure.LureID,
			Latitude:   lure.Latitude,
			Longitude:  lure.Longitude,
		}

		st := ps.stateMgr.Get()
		matched := ps.lureMatcher.Match(data, st)

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(lure.Latitude, lure.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Lure %d at %s and %d humans cared",
				lure.LureID, lure.Name, len(matched))

			enrichment := ps.enricher.Lure(&lure)

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "lure",
				Message:      raw,
				Enrichment:   enrichment,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Lure %d at %s and 0 humans cared", lure.LureID, lure.Name)
		}
	}()
	return nil
}
