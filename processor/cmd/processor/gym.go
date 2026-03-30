package main

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessGym(raw json.RawMessage) error {
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
			metrics.WebhookProcessingDuration.WithLabelValues("gym").Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

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

		// Battle cooldown: during battles, Golbat sends frequent updates.
		// Skip if same team + same slots and within 5-min battle cooldown.
		battleCooldown := ps.duplicates.GymInBattleCooldown(gymID, inBattle)

		// Update gym state and get old state
		oldState := ps.gymState.Update(gymID, teamID, gym.SlotsAvailable, inBattle, gym.LastOwnerID)
		if oldState == nil {
			l.Debug("Gym first seen, no change detection yet")
			return
		}

		if battleCooldown && oldState.TeamID == teamID && oldState.SlotsAvailable == gym.SlotsAvailable {
			l.Debug("Gym battle cooldown, no team/slot change, skipping")
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
		matchStart := time.Now()
		matched := ps.gymMatcher.Match(data, st)
		metrics.MatchingDuration.WithLabelValues("gym").Observe(time.Since(matchStart).Seconds())
		matched = ps.filterRateLimited(matched)

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("gym").Inc()
			metrics.MatchedUsers.WithLabelValues("gym").Add(float64(len(matched)))

			areas := st.Geofence.PointInAreas(gym.Latitude, gym.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Gym %s changed %s -> %s areas(%s) and %d humans cared",
				gym.Name, ps.teamName(oldState.TeamID), ps.teamName(teamID), areaNames(matchedAreas), len(matched))

			enrichment, tilePending := ps.enricher.Gym(gym.Latitude, gym.Longitude, teamID, oldState.TeamID, gym.SlotsAvailable, inBattle, false, gymID)

			// Compute per-language translated enrichment
			var perLang map[string]map[string]any
			if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
				perLang = make(map[string]map[string]any)
				for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
					perLang[lang] = ps.enricher.GymTranslate(enrichment, teamID, oldState.TeamID, gym.LastOwnerID, lang)
				}
			}

			if ps.renderCh == nil {
				return
			}
			webhookFields := parseWebhookFields(raw)

			ps.renderCh <- RenderJob{
				TemplateType:      "gym",
				Enrichment:        enrichment,
				PerLangEnrichment: perLang,
				WebhookFields:     webhookFields,
				MatchedUsers:      matched,
				MatchedAreas:      matchedAreas,
				TilePending:       tilePending,
				LogReference:      gymID,
			}
		} else {
			l.Debugf("Gym %s changed and 0 humans cared", gym.Name)
		}
	}()
	return nil
}
