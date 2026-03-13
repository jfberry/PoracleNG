package main

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessInvasion(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

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
			return
		}

		// Resolve grunt type and display type
		displayType := inv.DisplayType
		if displayType == 0 {
			displayType = inv.IncidentDisplayType
		}
		gruntType := matching.ResolveGruntType(inv.IncidentGruntType, inv.GruntType, displayType)

		data := &matching.InvasionData{
			PokestopID: inv.PokestopID,
			GruntType:  gruntType,
			Gender:     inv.Gender,
			Latitude:   inv.Latitude,
			Longitude:  inv.Longitude,
		}

		st := ps.stateMgr.Get()
		matched := ps.invasionMatcher.Match(data, st)

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(inv.Latitude, inv.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Invasion grunt %s at %s and %d humans cared",
				gruntType, inv.Name, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "invasion",
				Message:      raw,
				Enrichment:   ps.enricher.Invasion(inv.Latitude, inv.Longitude, expiration),
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Invasion grunt %s at %s and 0 humans cared", gruntType, inv.Name)
		}
	}()
	return nil
}
