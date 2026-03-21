package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessQuest(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	metrics.WorkerPoolInUse.Inc()
	ps.wg.Add(1)
	go func() {
		start := time.Now()
		defer func() {
			metrics.WebhookProcessingDuration.WithLabelValues("quest").Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

		var quest webhook.QuestWebhook
		if err := json.Unmarshal(raw, &quest); err != nil {
			log.Errorf("Failed to parse quest webhook: %s", err)
			return
		}

		l := log.WithField("ref", quest.PokestopID)

		// Build rewards key for dedup
		rewardsKey := buildQuestRewardsKey(quest.Rewards)
		if ps.duplicates.CheckQuest(quest.PokestopID, rewardsKey) {
			l.Debug("Quest duplicate, ignoring")
			metrics.DuplicatesSkipped.WithLabelValues("quest").Inc()
			return
		}

		// Parse rewards for matching
		rewards := make([]matching.QuestRewardData, 0, len(quest.Rewards))
		for _, r := range quest.Rewards {
			rewards = append(rewards, parseQuestReward(r))
		}

		data := &matching.QuestData{
			PokestopID: quest.PokestopID,
			Latitude:   quest.Latitude,
			Longitude:  quest.Longitude,
			Rewards:    rewards,
		}

		st := ps.stateMgr.Get()
		matched := ps.questMatcher.Match(data, st)
		matched = ps.filterRateLimited(matched)

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("quest").Inc()
			metrics.MatchedUsers.WithLabelValues("quest").Add(float64(len(matched)))

			areas := st.Geofence.PointInAreas(quest.Latitude, quest.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Quest at %s and %d humans cared", quest.Name, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "quest",
				Message:      raw,
				Enrichment:   ps.enricher.Quest(quest.Latitude, quest.Longitude, quest.PokestopID),
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Quest at %s and 0 humans cared", quest.Name)
		}
	}()
	return nil
}

// buildQuestRewardsKey creates a dedup key from quest rewards.
func buildQuestRewardsKey(rewards []webhook.QuestReward) string {
	var key strings.Builder
	for _, r := range rewards {
		key.WriteString(fmt.Sprintf("%d:", r.Type))
		if info, ok := r.Info["pokemon_id"]; ok {
			key.WriteString(fmt.Sprintf("p%v", info))
		}
		if info, ok := r.Info["item_id"]; ok {
			key.WriteString(fmt.Sprintf("i%v", info))
		}
		if info, ok := r.Info["amount"]; ok {
			key.WriteString(fmt.Sprintf("a%v", info))
		}
		key.WriteString(";")
	}
	return key.String()
}

// parseQuestReward converts a webhook QuestReward to a matching QuestRewardData.
func parseQuestReward(r webhook.QuestReward) matching.QuestRewardData {
	result := matching.QuestRewardData{
		Type: r.Type,
	}

	if v, ok := r.Info["pokemon_id"]; ok {
		result.PokemonID = toInt(v)
	}
	if v, ok := r.Info["item_id"]; ok {
		result.ItemID = toInt(v)
	}
	if v, ok := r.Info["amount"]; ok {
		result.Amount = toInt(v)
	}
	if v, ok := r.Info["form_id"]; ok {
		result.FormID = toInt(v)
	}
	if v, ok := r.Info["shiny"]; ok {
		if b, ok2 := v.(bool); ok2 {
			result.Shiny = b
		}
	}

	return result
}
