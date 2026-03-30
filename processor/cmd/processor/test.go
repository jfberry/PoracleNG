package main

import (
	"encoding/json"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// ProcessTest processes a test webhook through the enrichment pipeline
// without matching or dedup, sending the result to the specified target.
func (ps *ProcessorService) ProcessTest(webhookType string, raw json.RawMessage, target api.TestTarget) error {
	matchedUser := webhook.MatchedUser{
		ID:       target.ID,
		Name:     target.Name,
		Type:     target.Type,
		Language: target.Language,
		Latitude: target.Latitude,
		Longitude: target.Longitude,
		Template: target.Template,
		Clean:    false,
	}

	switch webhookType {
	case "pokemon":
		return ps.processTestPokemon(raw, matchedUser)
	case "raid", "egg":
		return ps.processTestRaid(raw, matchedUser, webhookType)
	case "invasion":
		return ps.processTestInvasion(raw, matchedUser)
	case "quest":
		return ps.processTestQuest(raw, matchedUser)
	case "gym":
		return ps.processTestGym(raw, matchedUser)
	case "nest":
		return ps.processTestNest(raw, matchedUser)
	case "fort_update":
		return ps.processTestFort(raw, matchedUser)
	case "max_battle":
		return ps.processTestMaxbattle(raw, matchedUser)
	case "pokestop":
		return ps.processTestPokestop(raw, matchedUser)
	default:
		return fmt.Errorf("unsupported test webhook type: %s", webhookType)
	}
}

func (ps *ProcessorService) processTestPokemon(raw json.RawMessage, target webhook.MatchedUser) error {
	var pokemon webhook.PokemonWebhook
	if err := json.Unmarshal(raw, &pokemon); err != nil {
		return fmt.Errorf("parse pokemon: %w", err)
	}

	rarityGroup := ps.stats.GetRarityGroup(pokemon.PokemonID)
	processed := matching.ProcessPokemonWebhook(&pokemon, rarityGroup, ps.pvpCfg)
	enrichment, tilePending := ps.enricher.Pokemon(&pokemon, processed)

	matched := []webhook.MatchedUser{target}
	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.PokemonTranslate(enrichment, &pokemon, target.Language),
		}
	}

	var perUser map[string]map[string]any
	if ps.enricher.PVPDisplay != nil && perLang != nil {
		perUser = ps.enricher.PokemonPerUser(perLang, matched)
	}

	log.Infof("[Test] Pokemon %d at [%.3f,%.3f] → %s %s", pokemon.PokemonID, pokemon.Latitude, pokemon.Longitude, target.Type, target.ID)

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		IsPokemon:         true,
		IsEncountered:     processed.Encountered,
		Enrichment:        enrichment,
		PerLangEnrichment: perLang,
		PerUserEnrichment: perUser,
		WebhookFields:     webhookFields,
		MatchedUsers:      matched,
		MatchedAreas:      []webhook.MatchedArea{},
		TilePending:       tilePending,
		LogReference:      "test",
	}
	return nil
}

func (ps *ProcessorService) processTestRaid(raw json.RawMessage, target webhook.MatchedUser, msgType string) error {
	var raid webhook.RaidWebhook
	if err := json.Unmarshal(raw, &raid); err != nil {
		return fmt.Errorf("parse raid: %w", err)
	}

	if msgType == "" {
		if raid.PokemonID > 0 {
			msgType = "raid"
		} else {
			msgType = "egg"
		}
	}

	enrichment, tilePending := ps.enricher.Raid(&raid, true)
	matched := []webhook.MatchedUser{target}

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.RaidTranslate(enrichment, &raid, target.Language),
		}
	}

	log.Infof("[Test] %s level %d at [%.3f,%.3f] → %s %s", msgType, raid.Level, raid.Latitude, raid.Longitude, target.Type, target.ID)

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      msgType,
		Enrichment:        enrichment,
		PerLangEnrichment: perLang,
		WebhookFields:     webhookFields,
		MatchedUsers:      matched,
		MatchedAreas:      []webhook.MatchedArea{},
		TilePending:       tilePending,
		LogReference:      "test",
	}
	return nil
}

func (ps *ProcessorService) processTestInvasion(raw json.RawMessage, target webhook.MatchedUser) error {
	var inv webhook.InvasionWebhook
	if err := json.Unmarshal(raw, &inv); err != nil {
		return fmt.Errorf("parse invasion: %w", err)
	}

	expiration := inv.IncidentExpiration
	if expiration == 0 {
		expiration = inv.IncidentExpireTimestamp
	}
	gruntTypeID := inv.IncidentGruntType
	if gruntTypeID == 0 {
		gruntTypeID = inv.GruntType
	}

	displayType := inv.DisplayType
	if displayType == 0 {
		displayType = inv.IncidentDisplayType
	}
	enrichment, tilePending := ps.enricher.Invasion(inv.Latitude, inv.Longitude, expiration, inv.PokestopID, gruntTypeID, displayType, 0)
	matched := []webhook.MatchedUser{target}

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.InvasionTranslate(enrichment, gruntTypeID, target.Language),
		}
	}

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      "invasion",
		Enrichment:        enrichment,
		PerLangEnrichment: perLang,
		WebhookFields:     webhookFields,
		MatchedUsers:      matched,
		MatchedAreas:      []webhook.MatchedArea{},
		TilePending:       tilePending,
		LogReference:      "test",
	}
	return nil
}

func (ps *ProcessorService) processTestQuest(raw json.RawMessage, target webhook.MatchedUser) error {
	var quest webhook.QuestWebhook
	if err := json.Unmarshal(raw, &quest); err != nil {
		return fmt.Errorf("parse quest: %w", err)
	}

	rewards := make([]matching.QuestRewardData, 0, len(quest.Rewards))
	for _, r := range quest.Rewards {
		rewards = append(rewards, parseQuestReward(r))
	}
	enrichment, tilePending := ps.enricher.Quest(quest.Latitude, quest.Longitude, quest.PokestopID, rewards)

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.QuestTranslate(enrichment, &quest, rewards, target.Language),
		}
	}

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      "quest",
		Enrichment:        enrichment,
		PerLangEnrichment: perLang,
		WebhookFields:     webhookFields,
		MatchedUsers:      []webhook.MatchedUser{target},
		MatchedAreas:      []webhook.MatchedArea{},
		TilePending:       tilePending,
		LogReference:      "test",
	}
	return nil
}

func (ps *ProcessorService) processTestGym(raw json.RawMessage, target webhook.MatchedUser) error {
	var gym webhook.GymWebhook
	if err := json.Unmarshal(raw, &gym); err != nil {
		return fmt.Errorf("parse gym: %w", err)
	}

	gymID := gym.GymID
	if gymID == "" {
		gymID = gym.ID
	}
	teamID := gym.TeamID
	if teamID == 0 {
		teamID = gym.Team
	}

	inBattle := bool(gym.IsInBattle) || bool(gym.InBattle)
	enrichment, tilePending := ps.enricher.Gym(gym.Latitude, gym.Longitude, teamID, 0, gym.SlotsAvailable, inBattle, false, gymID)
	matched := []webhook.MatchedUser{target}

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.GymTranslate(enrichment, teamID, 0, 0, target.Language),
		}
	}

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      "gym",
		Enrichment:        enrichment,
		PerLangEnrichment: perLang,
		WebhookFields:     webhookFields,
		MatchedUsers:      matched,
		MatchedAreas:      []webhook.MatchedArea{},
		TilePending:       tilePending,
		LogReference:      "test",
	}
	return nil
}

func (ps *ProcessorService) processTestNest(raw json.RawMessage, target webhook.MatchedUser) error {
	var nest webhook.NestWebhook
	if err := json.Unmarshal(raw, &nest); err != nil {
		return fmt.Errorf("parse nest: %w", err)
	}

	enrichment, tilePending := ps.enricher.Nest(&nest)
	matched := []webhook.MatchedUser{target}

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.NestTranslate(enrichment, nest.PokemonID, nest.Form, target.Language),
		}
	}

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      "nest",
		Enrichment:        enrichment,
		PerLangEnrichment: perLang,
		WebhookFields:     webhookFields,
		MatchedUsers:      matched,
		MatchedAreas:      []webhook.MatchedArea{},
		TilePending:       tilePending,
		LogReference:      "test",
	}
	return nil
}

func (ps *ProcessorService) processTestFort(raw json.RawMessage, target webhook.MatchedUser) error {
	var fort webhook.FortWebhook
	if err := json.Unmarshal(raw, &fort); err != nil {
		return fmt.Errorf("parse fort: %w", err)
	}

	enrichment, tilePending := ps.enricher.FortUpdate(fort.Latitude(), fort.Longitude(), fort.FortID(), &fort)

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:  "fort-update",
		Enrichment:    enrichment,
		WebhookFields: webhookFields,
		MatchedUsers:  []webhook.MatchedUser{target},
		MatchedAreas:  []webhook.MatchedArea{},
		TilePending:   tilePending,
		LogReference:  "test",
	}
	return nil
}

func (ps *ProcessorService) processTestMaxbattle(raw json.RawMessage, target webhook.MatchedUser) error {
	var mb webhook.MaxbattleWebhook
	if err := json.Unmarshal(raw, &mb); err != nil {
		return fmt.Errorf("parse maxbattle: %w", err)
	}

	enrichment, tilePending := ps.enricher.Maxbattle(mb.Latitude, mb.Longitude, mb.BattleEnd, &mb)
	matched := []webhook.MatchedUser{target}

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.MaxbattleTranslate(enrichment, &mb, target.Language),
		}
	}

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      "maxbattle",
		Enrichment:        enrichment,
		PerLangEnrichment: perLang,
		WebhookFields:     webhookFields,
		MatchedUsers:      matched,
		MatchedAreas:      []webhook.MatchedArea{},
		TilePending:       tilePending,
		LogReference:      "test",
	}
	return nil
}

func (ps *ProcessorService) processTestPokestop(raw json.RawMessage, target webhook.MatchedUser) error {
	// Pokestop can be invasion or lure — peek at fields
	var peek struct {
		LureExpiration     int64 `json:"lure_expiration"`
		IncidentExpiration int64 `json:"incident_expiration"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return fmt.Errorf("parse pokestop: %w", err)
	}

	if peek.LureExpiration > 0 {
		var lure webhook.LureWebhook
		if err := json.Unmarshal(raw, &lure); err != nil {
			return fmt.Errorf("parse lure: %w", err)
		}
		enrichment, tilePending := ps.enricher.Lure(&lure)
		matched := []webhook.MatchedUser{target}
		var perLang map[string]map[string]any
		if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
			perLang = map[string]map[string]any{
				target.Language: ps.enricher.LureTranslate(enrichment, lure.LureID, target.Language),
			}
		}
		if ps.renderCh == nil {
			return fmt.Errorf("render queue not available")
		}
		webhookFields := parseWebhookFields(raw)
		ps.renderCh <- RenderJob{
			TemplateType:      "lure",
			Enrichment:        enrichment,
			PerLangEnrichment: perLang,
			WebhookFields:     webhookFields,
			MatchedUsers:      matched,
			MatchedAreas:      []webhook.MatchedArea{},
			TilePending:       tilePending,
			LogReference:      "test",
		}
		return nil
	}

	return ps.processTestInvasion(raw, target)
}

// Ensure ProcessorService implements TestProcessor
var _ api.TestProcessor = (*ProcessorService)(nil)
