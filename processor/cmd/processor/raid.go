package main

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessRaid(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var raid webhook.RaidWebhook
		if err := json.Unmarshal(raw, &raid); err != nil {
			log.Errorf("Failed to parse raid webhook: %s", err)
			return
		}

		l := log.WithField("ref", raid.GymID)

		st := ps.stateMgr.Get()
		ex := bool(raid.ExRaidEligible) || bool(raid.IsExRaidEligible)

		var matched []webhook.MatchedUser

		if raid.PokemonID > 0 {
			// Raid with boss
			raidData := &matching.RaidData{
				GymID:     raid.GymID,
				PokemonID: raid.PokemonID,
				Form:      raid.Form,
				Level:     raid.Level,
				TeamID:    raid.TeamID,
				Ex:        ex,
				Evolution: raid.Evolution,
				Move1:     raid.Move1,
				Move2:     raid.Move2,
				Latitude:  raid.Latitude,
				Longitude: raid.Longitude,
			}
			matched = ps.raidMatcher.MatchRaid(raidData, st)
		} else {
			// Egg
			eggData := &matching.EggData{
				GymID:     raid.GymID,
				Level:     raid.Level,
				TeamID:    raid.TeamID,
				Ex:        ex,
				Latitude:  raid.Latitude,
				Longitude: raid.Longitude,
			}
			matched = ps.raidMatcher.MatchEgg(eggData, st)
		}

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(raid.Latitude, raid.Longitude)
			matchedAreas := make([]webhook.MatchedArea, len(areas))
			for i, a := range areas {
				matchedAreas[i] = webhook.MatchedArea{
					Name:             a.Name,
					DisplayInMatches: a.DisplayInMatches,
					Group:            a.Group,
				}
			}

			msgType := "raid"
			if raid.PokemonID == 0 {
				msgType = "egg"
			}

			gymName := raid.GymName
			if gymName == "" {
				gymName = raid.Name
			}

			l.Infof("%s level %d on %s appeared at [%.3f,%.3f] and %d humans cared",
				msgType, raid.Level, gymName, raid.Latitude, raid.Longitude, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         msgType,
				Message:      raw,
				Enrichment:   ps.enricher.Raid(&raid),
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Raid/egg level %d appeared at [%.3f,%.3f] and 0 humans cared",
				raid.Level, raid.Latitude, raid.Longitude)
		}
	}()
	return nil
}
