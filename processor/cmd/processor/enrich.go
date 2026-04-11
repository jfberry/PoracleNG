package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// enrichResult holds the ingredients needed to build a LayeredView.
type enrichResult struct {
	templateType  string
	base          map[string]any
	perLang       map[string]any
	perUser       map[string]any
	webhookFields map[string]any
}

// EnrichWebhook runs a raw webhook through the enrichment pipeline, builds a
// LayeredView (with aliases, emoji, computed fields), and returns a flattened
// variable map matching what Handlebars templates see during real rendering.
func (ps *ProcessorService) EnrichWebhook(webhookType string, raw json.RawMessage, language, platform string) (map[string]any, error) {
	var result *enrichResult
	var err error

	switch webhookType {
	case "pokemon", "monster":
		result, err = ps.enrichPokemon(raw, language)
	case "raid":
		result, err = ps.enrichRaid(raw, language, false)
	case "egg":
		result, err = ps.enrichRaid(raw, language, true)
	case "quest":
		result, err = ps.enrichQuest(raw, language)
	case "invasion":
		result, err = ps.enrichInvasion(raw, language)
	case "lure":
		result, err = ps.enrichLure(raw, language)
	case "nest":
		result, err = ps.enrichNest(raw, language)
	case "gym":
		result, err = ps.enrichGym(raw, language)
	case "fort_update", "fort-update":
		result, err = ps.enrichFort(raw, language)
	case "max_battle", "maxbattle":
		result, err = ps.enrichMaxbattle(raw, language)
	default:
		return nil, fmt.Errorf("unsupported webhook type: %s", webhookType)
	}
	if err != nil {
		return nil, err
	}

	// Build LayeredView with aliases, emoji resolution, and computed fields —
	// the same path as real template rendering.
	if ps.dtsRenderer != nil {
		lv := dts.NewLayeredView(
			ps.dtsRenderer.ViewBuilder(),
			result.templateType,
			result.base,
			result.perLang,
			result.perUser,
			result.webhookFields,
			platform,
			nil, // no matched areas
		)
		return lv.Flatten(), nil
	}

	// Fallback if DTS renderer isn't available — return raw merge
	return mergeEnrichment(result.base, result.perLang, result.webhookFields), nil
}

// mergeEnrichment is a simple fallback when the DTS renderer is not available.
func mergeEnrichment(base, perLang, webhookFields map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(perLang)+len(webhookFields))
	maps.Copy(result, webhookFields)
	maps.Copy(result, base)
	maps.Copy(result, perLang)
	return result
}

func (ps *ProcessorService) enrichPokemon(raw json.RawMessage, language string) (*enrichResult, error) {
	var pokemon webhook.PokemonWebhook
	if err := json.Unmarshal(raw, &pokemon); err != nil {
		return nil, fmt.Errorf("parse pokemon: %w", err)
	}

	if pokemon.DisappearTime > 0 && pokemon.DisappearTime < time.Now().Unix() {
		pokemon.DisappearTime = time.Now().Unix() + 600
	}

	rarityGroup := ps.stats.GetRarityGroup(pokemon.PokemonID)
	processed := matching.ProcessPokemonWebhook(&pokemon, rarityGroup, ps.pvpCfg)
	base, _ := ps.enricher.Pokemon(&pokemon, processed)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.PokemonTranslate(base, &pokemon, language)
	}

	// Compute PVP display data with a synthetic user (no filters = show all PVP entries).
	// In normal rendering this is per-user, but for the editor preview we show everything.
	var perUser map[string]any
	if ps.enricher.PVPDisplay != nil && perLang != nil {
		syntheticUser := webhook.MatchedUser{
			ID:       "_editor",
			Language: language,
		}
		perUserMap := ps.enricher.PokemonPerUser(
			map[string]map[string]any{language: perLang},
			[]webhook.MatchedUser{syntheticUser},
		)
		perUser = perUserMap["_editor"]
	}

	return &enrichResult{
		templateType:  "monster",
		base:          base,
		perLang:       perLang,
		perUser:       perUser,
		webhookFields: parseWebhookFields(raw),
	}, nil
}

func (ps *ProcessorService) enrichRaid(raw json.RawMessage, language string, isEgg bool) (*enrichResult, error) {
	var raid webhook.RaidWebhook
	if err := json.Unmarshal(raw, &raid); err != nil {
		return nil, fmt.Errorf("parse raid: %w", err)
	}

	if raid.Start > 0 && raid.End < time.Now().Unix() {
		raid.Start = time.Now().Unix() + 600
		raid.End = raid.Start + 1800
	}

	templateType := "raid"
	if isEgg || raid.PokemonID == 0 {
		templateType = "egg"
	}

	base, _ := ps.enricher.Raid(&raid, true)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.RaidTranslate(base, &raid, language)
	}

	return &enrichResult{
		templateType:  templateType,
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
	}, nil
}

func (ps *ProcessorService) enrichQuest(raw json.RawMessage, language string) (*enrichResult, error) {
	var quest webhook.QuestWebhook
	if err := json.Unmarshal(raw, &quest); err != nil {
		return nil, fmt.Errorf("parse quest: %w", err)
	}

	rewards := make([]matching.QuestRewardData, 0, len(quest.Rewards))
	for _, r := range quest.Rewards {
		rewards = append(rewards, parseQuestReward(r))
	}
	base, _ := ps.enricher.Quest(quest.Latitude, quest.Longitude, quest.PokestopID, rewards)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.QuestTranslate(base, &quest, rewards, language)
	}

	return &enrichResult{
		templateType:  "quest",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
	}, nil
}

func (ps *ProcessorService) enrichInvasion(raw json.RawMessage, language string) (*enrichResult, error) {
	var inv webhook.InvasionWebhook
	if err := json.Unmarshal(raw, &inv); err != nil {
		return nil, fmt.Errorf("parse invasion: %w", err)
	}

	if inv.IncidentExpiration > 0 && inv.IncidentExpiration < time.Now().Unix() {
		inv.IncidentExpiration = time.Now().Unix() + 600
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

	base, _ := ps.enricher.Invasion(inv.Latitude, inv.Longitude, expiration, inv.PokestopID, gruntTypeID, displayType, 0)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.InvasionTranslate(base, gruntTypeID, inv.Lineup, language)
	}

	return &enrichResult{
		templateType:  "invasion",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
	}, nil
}

func (ps *ProcessorService) enrichLure(raw json.RawMessage, language string) (*enrichResult, error) {
	var lure webhook.LureWebhook
	if err := json.Unmarshal(raw, &lure); err != nil {
		return nil, fmt.Errorf("parse lure: %w", err)
	}

	base, _ := ps.enricher.Lure(&lure)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.LureTranslate(base, lure.LureID, language)
	}

	return &enrichResult{
		templateType:  "lure",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
	}, nil
}

func (ps *ProcessorService) enrichNest(raw json.RawMessage, language string) (*enrichResult, error) {
	var nest webhook.NestWebhook
	if err := json.Unmarshal(raw, &nest); err != nil {
		return nil, fmt.Errorf("parse nest: %w", err)
	}

	base, _ := ps.enricher.Nest(&nest)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.NestTranslate(base, nest.PokemonID, nest.Form, language)
	}

	return &enrichResult{
		templateType:  "nest",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
	}, nil
}

func (ps *ProcessorService) enrichGym(raw json.RawMessage, language string) (*enrichResult, error) {
	var gym webhook.GymWebhook
	if err := json.Unmarshal(raw, &gym); err != nil {
		return nil, fmt.Errorf("parse gym: %w", err)
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

	base, _ := ps.enricher.Gym(gym.Latitude, gym.Longitude, teamID, 0, gym.SlotsAvailable, -1, inBattle, false, gymID)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.GymTranslate(base, teamID, 0, 0, language)
	}

	return &enrichResult{
		templateType:  "gym",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
	}, nil
}

func (ps *ProcessorService) enrichFort(raw json.RawMessage, language string) (*enrichResult, error) {
	var fort webhook.FortWebhook
	if err := json.Unmarshal(raw, &fort); err != nil {
		return nil, fmt.Errorf("parse fort: %w", err)
	}

	base, _ := ps.enricher.FortUpdate(fort.Latitude(), fort.Longitude(), fort.FortID(), &fort)

	return &enrichResult{
		templateType:  "fort-update",
		base:          base,
		webhookFields: parseWebhookFields(raw),
	}, nil
}

func (ps *ProcessorService) enrichMaxbattle(raw json.RawMessage, language string) (*enrichResult, error) {
	var mb webhook.MaxbattleWebhook
	if err := json.Unmarshal(raw, &mb); err != nil {
		return nil, fmt.Errorf("parse maxbattle: %w", err)
	}

	base, _ := ps.enricher.Maxbattle(mb.Latitude, mb.Longitude, mb.BattleEnd, &mb)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.MaxbattleTranslate(base, &mb, language)
	}

	return &enrichResult{
		templateType:  "maxbattle",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
	}, nil
}
