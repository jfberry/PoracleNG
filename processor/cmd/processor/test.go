package main

import (
	"encoding/json"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessTest(webhookType string, raw json.RawMessage, target bot.TestTarget) error {
	if ps.dtsRenderer == nil {
		return fmt.Errorf("DTS templates not loaded — check startup logs for template loading errors")
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	if ps.dispatcher == nil {
		return fmt.Errorf("message delivery not configured — check Discord/Telegram token settings")
	}

	// Validate that a matching DTS template exists before enqueueing.
	// Resolve the actual DTS type by peeking at the webhook data for types
	// that branch (pokestop→lure/invasion, raid→egg/raid).
	dtsType := resolveDTSTypeFromRaw(webhookType, raw)
	platform := delivery.PlatformFromType(target.Type)
	language := target.Language
	if language == "" {
		language = ps.cfg.General.Locale
	}
	if err := ps.dtsRenderer.CheckTemplate(dtsType, platform, target.Template, language); err != nil {
		return err
	}

	matchedUser := webhook.MatchedUser{
		ID:       target.ID,
		Name:     target.Name,
		Type:     target.Type,
		Language: target.Language,
		Latitude: target.Latitude,
		Longitude: target.Longitude,
		Template: target.Template,
		Clean:    0,
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
	enrichmentData, tilePending := ps.enricher.Pokemon(&pokemon, processed, enrichment.TileModeURL)

	matched := []webhook.MatchedUser{target}
	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.PokemonTranslate(enrichmentData, &pokemon, target.Language),
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
		Enrichment:        enrichmentData,
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

	// Always determine type from webhook data — test data uses "raid" for both
	if raid.PokemonID > 0 {
		msgType = "raid"
	} else {
		msgType = "egg"
	}

	enrichmentData, tilePending := ps.enricher.Raid(&raid, true, enrichment.TileModeURL)
	matched := []webhook.MatchedUser{target}

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.RaidTranslate(enrichmentData, &raid, target.Language),
		}
	}

	log.Infof("[Test] %s level %d at [%.3f,%.3f] → %s %s", msgType, raid.Level, raid.Latitude, raid.Longitude, target.Type, target.ID)

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      msgType,
		Enrichment:        enrichmentData,
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
	enrichmentData, tilePending := ps.enricher.Invasion(inv.Latitude, inv.Longitude, expiration, inv.PokestopID, inv.URL, gruntTypeID, displayType, 0, enrichment.TileModeURL)
	matched := []webhook.MatchedUser{target}

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.InvasionTranslate(enrichmentData, gruntTypeID, inv.Lineup, target.Language),
		}
	}

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      "invasion",
		Enrichment:        enrichmentData,
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
	enrichmentData, tilePending := ps.enricher.Quest(quest.Latitude, quest.Longitude, quest.PokestopID, quest.URL, rewards, enrichment.TileModeURL)

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.QuestTranslate(enrichmentData, &quest, rewards, target.Language),
		}
	}

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      "quest",
		Enrichment:        enrichmentData,
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
	enrichmentData, tilePending := ps.enricher.Gym(gym.Latitude, gym.Longitude, teamID, 0, gym.SlotsAvailable, -1, inBattle, false, gymID, enrichment.TileModeURL)
	matched := []webhook.MatchedUser{target}

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.GymTranslate(enrichmentData, teamID, 0, 0, target.Language),
		}
	}

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      "gym",
		Enrichment:        enrichmentData,
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

	enrichmentData, tilePending := ps.enricher.Nest(&nest, enrichment.TileModeURL)
	matched := []webhook.MatchedUser{target}

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.NestTranslate(enrichmentData, nest.PokemonID, nest.Form, target.Language),
		}
	}

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      "nest",
		Enrichment:        enrichmentData,
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

	enrichmentData, tilePending := ps.enricher.FortUpdate(fort.Latitude(), fort.Longitude(), fort.FortID(), &fort, enrichment.TileModeURL)

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:  "fort-update",
		Enrichment:    enrichmentData,
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

	enrichmentData, tilePending := ps.enricher.Maxbattle(mb.Latitude, mb.Longitude, mb.BattleEnd, &mb, enrichment.TileModeURL)
	matched := []webhook.MatchedUser{target}

	var perLang map[string]map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = map[string]map[string]any{
			target.Language: ps.enricher.MaxbattleTranslate(enrichmentData, &mb, target.Language),
		}
	}

	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	webhookFields := parseWebhookFields(raw)
	ps.renderCh <- RenderJob{
		TemplateType:      "maxbattle",
		Enrichment:        enrichmentData,
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
		enrichmentData, tilePending := ps.enricher.Lure(&lure, enrichment.TileModeURL)
		matched := []webhook.MatchedUser{target}
		var perLang map[string]map[string]any
		if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
			perLang = map[string]map[string]any{
				target.Language: ps.enricher.LureTranslate(enrichmentData, lure.LureID, target.Language),
			}
		}
		if ps.renderCh == nil {
			return fmt.Errorf("render queue not available")
		}
		webhookFields := parseWebhookFields(raw)
		ps.renderCh <- RenderJob{
			TemplateType:      "lure",
			Enrichment:        enrichmentData,
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

// resolveDTSTypeFromRaw determines the DTS template type by peeking at the raw webhook JSON.
// Handles branching types: pokestop→lure/invasion, raid→egg/raid.
func resolveDTSTypeFromRaw(webhookType string, raw json.RawMessage) string {
	switch webhookType {
	case "pokemon":
		return "monster"
	case "raid":
		var peek struct {
			PokemonID int `json:"pokemon_id"`
		}
		if json.Unmarshal(raw, &peek) == nil && peek.PokemonID > 0 {
			return "raid"
		}
		return "egg"
	case "egg":
		return "egg"
	case "pokestop":
		var peek struct {
			LureExpiration int64 `json:"lure_expiration"`
		}
		if json.Unmarshal(raw, &peek) == nil && peek.LureExpiration > 0 {
			return "lure"
		}
		return "invasion"
	case "fort_update":
		return "fort-update"
	case "max_battle":
		return "maxbattle"
	default:
		return webhookType
	}
}

// Ensure ProcessorService implements TestProcessor
var _ bot.TestProcessor = (*ProcessorService)(nil)
