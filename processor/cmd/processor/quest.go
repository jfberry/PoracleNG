package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessQuest(raw json.RawMessage) error {
	if ps.cfg.General.DisableQuest {
		return nil
	}

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

		// Build rewards key for dedup. AR / non-AR are separate quests on
		// the same stop with independent objectives — keying on rewards
		// alone collapsed colliding-reward pairs into one alert.
		rewardsKey := buildQuestRewardsKey(quest.WithAR, quest.Rewards)
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
		matched, buffered, matchedAreas := ps.questMatcher.Match(data, st)
		matched = ps.filterBlocked(matched)
		matched = ps.filterValidation("quest", raw, matchedAreas, matched)

		// Append buffered (summary-bit) matches to the summary buffer.
		// These users get a grouped delivery later from the summary
		// scheduler (PR 5); we don't enrich or render now.
		if len(buffered) > 0 {
			ps.bufferQuestMatches(buffered, &quest, rewards, raw)
		}

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("quest").Inc()
			metrics.MatchedUsers.WithLabelValues("quest").Add(float64(len(matched)))
			metrics.IntervalMatched.Add(1)

			l.Infof("Quest at %s areas(%s) and %d humans cared", quest.Name, areaNames(matchedAreas), len(matched))

			mode := ps.tileMode("quest", matched)
			enrichmentData, tilePending := ps.enricher.Quest(quest.Latitude, quest.Longitude, quest.PokestopID, quest.URL, rewards, mode)

			// Compute per-language translated enrichment
			var perLang map[string]map[string]any
			if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
				perLang = make(map[string]map[string]any)
				for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
					perLang[lang] = ps.enricher.QuestTranslate(enrichmentData, &quest, rewards, lang)
				}
			}

			if ps.renderCh == nil {
				return
			}
			webhookFields := parseWebhookFields(raw)

			ps.renderCh <- RenderJob{
				TemplateType:      "quest",
				Enrichment:        enrichmentData,
				PerLangEnrichment: perLang,
				WebhookFields:     webhookFields,
				MatchedUsers:      matched,
				MatchedAreas:      matchedAreas,
				TileGate:          ps.newTileGate(tilePending),
				LogReference:      quest.PokestopID,
			}
		} else {
			l.Debugf("Quest at %s and 0 humans cared", quest.Name)
		}
	}()
	return nil
}

// buildQuestRewardsKey creates a dedup key from quest rewards plus the
// AR flag. The AR prefix segregates the two quests Niantic can attach to
// a single pokestop (regular vs AR-required) so they don't collapse on
// each other when their rewards happen to match.
func buildQuestRewardsKey(withAR bool, rewards []webhook.QuestReward) string {
	var key strings.Builder
	if withAR {
		key.WriteString("ar:")
	} else {
		key.WriteString("std:")
	}
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

// bufferQuestMatches appends one entry per matched user to the summary
// buffer. The full webhook bytes are stored verbatim in the BufferedQuest
// payload so the summary scheduler can re-enrich at delivery time
// (picking up any language change since the buffer was written).
//
// The bufferKey on each entry is (RewardType, Reward, PokestopID,
// WithAR), so we pick the first reward as the keying pair — multiple
// rewards on the same stop+AR-flag still collapse to one entry per user
// because the same quest can't simultaneously be tracked under
// different reward primary keys (the matcher already routed once for
// THIS webhook).
func (ps *ProcessorService) bufferQuestMatches(
	users []webhook.MatchedUser,
	quest *webhook.QuestWebhook,
	rewards []matching.QuestRewardData,
	raw []byte,
) {
	if ps.summaryBuffer == nil || len(users) == 0 {
		return
	}

	// Pick a representative (RewardType, Reward) for the bufferKey.
	// Quests almost always carry a single reward; when they don't, the
	// first reward is enough to dedup repeat firings of the same stop.
	var rewardType, reward int
	if len(rewards) > 0 {
		rewardType = rewards[0].Type
		switch rewardType {
		case 7, 4, 12: // pokemon / candy / mega energy → pokemon ID
			reward = rewards[0].PokemonID
		case 2: // item
			reward = rewards[0].ItemID
		case 3: // stardust → amount
			reward = rewards[0].Amount
		}
	}

	expiresAt := geo.EndOfDay(quest.Latitude, quest.Longitude)
	now := time.Now().Unix()
	payload := append([]byte(nil), raw...) // detach from caller's buffer

	for _, u := range users {
		ps.summaryBuffer.Append(u.ID, "quest", tracker.BufferedQuest{
			RewardType: rewardType,
			Reward:     reward,
			PokestopID: quest.PokestopID,
			WithAR:     quest.WithAR,
			Payload:    payload,
			ExpiresAt:  expiresAt,
			CreatedAt:  now,
		})
	}
}
