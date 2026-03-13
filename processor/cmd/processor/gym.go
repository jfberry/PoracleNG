package main

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessGym(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var gym webhook.GymWebhook
		if err := json.Unmarshal(raw, &gym); err != nil {
			log.Errorf("Failed to parse gym webhook: %s", err)
			return
		}

		// Resolve gym ID
		gymID := gym.GymID
		if gymID == "" {
			gymID = gym.ID
		}

		l := log.WithField("ref", gymID)

		// Resolve team ID
		teamID := gym.TeamID
		if teamID == 0 {
			teamID = gym.Team
		}

		// Resolve in-battle
		inBattle := bool(gym.IsInBattle) || bool(gym.InBattle)

		// Update gym state and get old state
		oldState := ps.gymState.Update(gymID, teamID, gym.SlotsAvailable, inBattle, gym.LastOwnerID)
		if oldState == nil {
			l.Debug("Gym first seen, no change detection yet")
			return
		}

		data := &matching.GymData{
			GymID:             gymID,
			TeamID:            teamID,
			OldTeamID:         oldState.TeamID,
			SlotsAvailable:    gym.SlotsAvailable,
			OldSlotsAvailable: oldState.SlotsAvailable,
			InBattle:          inBattle,
			OldInBattle:       oldState.InBattle,
			Latitude:          gym.Latitude,
			Longitude:         gym.Longitude,
		}

		st := ps.stateMgr.Get()
		matched := ps.gymMatcher.Match(data, st)

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(gym.Latitude, gym.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Gym %s changed (team %d->%d) and %d humans cared",
				gym.Name, oldState.TeamID, teamID, len(matched))

			enrichment := ps.enricher.Gym(gym.Latitude, gym.Longitude)

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "gym",
				Message:      raw,
				Enrichment:   enrichment,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Gym %s changed and 0 humans cared", gym.Name)
		}
	}()
	return nil
}
