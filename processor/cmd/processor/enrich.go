package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// enrichResult holds the ingredients needed to build a LayeredView.
type enrichResult struct {
	templateType  string
	base          map[string]any
	perLang       map[string]any
	perUser       map[string]any
	webhookFields map[string]any
	tilePending   *staticmap.TilePending
	extras        map[string]any // derived-type state: original, affected, rsvp, group. Nil for plain types.
}

// enrichForType resolves a DTS-name synonym (e.g. "monster", "monsterNoIv",
// "egg") via dtsAlias, dispatches to the enrich* function for the underlying
// webhook type, and returns the resulting enrichResult with its templateType
// forced to the alias's TemplateType.
//
// The alias is the single source of truth for template-type selection.
// Without the override at the end of this function, names that share a
// WebhookType but must render under distinct DTS template types collapse
// onto whatever the enrich* function hardcodes or infers from the payload:
// "monster" and "monsterNoIv" both dispatch through enrichPokemon (which
// always sets templateType "monster"), and "raid"/"egg" both dispatch
// through enrichRaid (which infers "raid" vs "egg" from the payload's
// PokemonID, not from which name the caller asked for).
func (ps *ProcessorService) enrichForType(name string, raw json.RawMessage, language string, freshenStaleTime bool) (*enrichResult, error) {
	src, ok := dtsAlias(name)
	if !ok {
		return nil, fmt.Errorf("unsupported webhook type: %s", name)
	}

	var result *enrichResult
	var err error

	// Derived names (monsterChanged, rsvpChanges, questSummary, incident,
	// weatherchange) don't arrive as their own Golbat wire event — they're
	// synthesized from processor-internal state (an encounter change, an
	// RSVP update, a summary digest, a weather-cell transition, ...).
	// "incident" is one exception: it IS parsed from a raw webhook — the
	// same PokestopEvent shape as "invasion" — just rendered under a
	// distinct DTS template type (see enrichIncident), so it's marked
	// Derived in the alias table purely to flag that it has no direct 1:1
	// webhook-type spelling of its own for the switch below, not because
	// it's synthesized from internal state. "weatherchange" is a partial
	// exception: enrichWeatherChange parses a fully-specified partial (the
	// testdata.json sample / editor preview payload) supplying the old/new
	// weather IDs and affected-pokemon list directly, rather than deriving
	// them from live tracker state the way the real consumeWeatherChanges
	// path does — see enrichWeatherChange's doc comment. "questSummary" is
	// likewise a partial exception: enrichQuestSummary takes a
	// fully-specified single reward group (the testdata.json sample /
	// editor preview payload) rather than pulling buffered quests from the
	// live SummaryBuffer and grouping across possibly-different reward
	// keys the way the real DispatchQuestSummary does — see
	// enrichQuestSummary's doc comment. "monster-changed" is likewise a
	// partial exception: enrichMonsterChanged takes a fully-specified
	// {old,new} pokemon-webhook pair (the testdata.json sample / editor
	// preview payload) rather than diffing two live sightings of the same
	// encounter_id via tracker.EncounterTracker.Track the way the real
	// dispatchPokemonAlert path does — see enrichMonsterChanged's doc
	// comment. "rsvp-changes" is likewise a partial exception:
	// enrichRsvpChanges takes a fully-specified raid webhook with its
	// `rsvps` array populated (the testdata.json sample / editor preview
	// payload) rather than reading the live RSVP list off an in-flight raid
	// webhook the way the real ProcessRaid path does — see
	// enrichRsvpChanges's doc comment.
	if src.Derived {
		switch src.WebhookType {
		case "incident":
			result, err = ps.enrichIncident(raw, language, freshenStaleTime)
		case "weatherchange":
			result, err = ps.enrichWeatherChange(raw, language, freshenStaleTime)
		case "quest_summary":
			result, err = ps.enrichQuestSummary(raw, language, freshenStaleTime)
		case "monster_changed":
			result, err = ps.enrichMonsterChanged(raw, language, freshenStaleTime)
		case "rsvp_changes":
			result, err = ps.enrichRsvpChanges(raw, language, freshenStaleTime)
		default:
			return nil, fmt.Errorf("derived type not yet supported (implemented in a later task): %s", name)
		}
	} else {
		// Resolve to the webhook type the switch below dispatches on, so
		// enrichForType("monster", ...) behaves exactly like
		// enrichForType("pokemon", ...).
		//
		// "pokestop" is deliberately excluded from this rewrite even though it's
		// the canonical WebhookType for both "invasion" and "lure": it isn't a
		// name this switch dispatches on (disambiguating it requires peeking at
		// the payload, see resolveDTSTypeFromRaw in test.go), so rewriting to it
		// would break the "invasion" and "lure" cases that already work today
		// under their own names.
		webhookType := src.WebhookType
		if webhookType == "pokestop" {
			webhookType = name
		}

		switch webhookType {
		case "pokemon", "monster":
			result, err = ps.enrichPokemon(raw, language, freshenStaleTime)
		case "raid":
			result, err = ps.enrichRaid(raw, language, false, freshenStaleTime)
		case "egg":
			result, err = ps.enrichRaid(raw, language, true, freshenStaleTime)
		case "quest":
			result, err = ps.enrichQuest(raw, language)
		case "invasion":
			result, err = ps.enrichInvasion(raw, language, freshenStaleTime)
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
		case "showcase":
			result, err = ps.enrichShowcase(raw, language, freshenStaleTime)
		default:
			return nil, fmt.Errorf("unsupported webhook type: %s", webhookType)
		}
	}
	if err != nil {
		return nil, err
	}

	// The alias is authoritative: it disambiguates names that share a
	// WebhookType but must render under distinct DTS template types (see
	// the doc comment above).
	if src.TemplateType != "" {
		result.templateType = src.TemplateType
	}

	return result, nil
}

// EnrichWebhook runs a raw webhook through the enrichment pipeline, builds a
// LayeredView (with aliases, emoji, computed fields), and returns a flattened
// variable map matching what Handlebars templates see during real rendering.
func (ps *ProcessorService) EnrichWebhook(webhookType string, raw json.RawMessage, language, platform string) (map[string]any, error) {
	result, err := ps.enrichForType(webhookType, raw, language, true)
	if err != nil {
		return nil, err
	}

	// isPokemon gates the synthetic-user PVP display block below on whether
	// the RESULT renders through a pokemon-family template — result.templateType,
	// not the requested name's underlying WebhookType. A prior version gated
	// on `dtsAlias(webhookType).WebhookType == "pokemon"`, which covered
	// "monster"/"monsterNoIv" (WebhookType "pokemon") but missed
	// "monsterChanged": its WebhookType is the derived "monster_changed"
	// spelling even though its base/perLang enrichment IS a pokemon spawn —
	// the NEW sighting, built via enrichPokemon (see enrichMonsterChanged's
	// doc comment) — so the editor preview rendered {{pvpGreat}}/etc. empty
	// while the live path (processTestMonsterChanged ->
	// renderJobFromEnrich(isPokemon=true)) always computed it. Gating on the
	// alias-authoritative result.templateType instead (set by enrichForType)
	// keeps all three pokemon-family template types in sync with
	// renderJobFromEnrich's own isPokemon=true call sites (processTestPokemon,
	// processTestMonsterChanged).
	isPokemon := result.templateType == "monster" || result.templateType == "monsterNoIv" || result.templateType == "monsterChanged"

	// Compute PVP display data with a synthetic user (no filters = show all PVP
	// entries). In normal rendering this is per-user, but for the editor preview
	// we show everything. Only pokemon carries per-user PVP display data.
	if isPokemon && ps.enricher.PVPDisplay != nil && result.perLang != nil {
		syntheticUser := webhook.MatchedUser{
			ID:       "_editor",
			Language: language,
		}
		perUserMap := ps.enricher.PokemonPerUser(
			map[string]map[string]any{language: result.perLang},
			[]webhook.MatchedUser{syntheticUser},
		)
		result.perUser = perUserMap["_editor"]
	}

	// Resolve tile synchronously if pending (the normal render path does this async)
	if result.tilePending != nil {
		select {
		case url := <-result.tilePending.Result:
			result.tilePending.Apply(url)
		case <-time.After(10 * time.Second):
			result.tilePending.Apply(result.tilePending.Fallback)
		}
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
		return injectOriginalExtra(lv.Flatten(), result), nil
	}

	// Fallback if DTS renderer isn't available — return raw merge
	return injectOriginalExtra(mergeEnrichment(result.base, result.perLang, result.webhookFields), result), nil
}

// injectOriginalExtra copies extras["original"] (built by
// enrichMonsterChanged) onto the flattened variable map so the
// /api/dts/enrich editor preview exposes {{original.X}} the same way the
// live RenderPokemonChanged path and !poracle-test do. LayeredView.Flatten
// doesn't include it: the "original" nested-map exposure is a raymond
// FieldResolver special-case (LayeredView.GetField) that only applies during
// actual template rendering, not the flattened variable-browser map this
// function builds. A no-op for every non-monsterChanged type, since only
// enrichMonsterChanged sets extras["original"].
func injectOriginalExtra(vars map[string]any, result *enrichResult) map[string]any {
	if original, ok := result.extras["original"].(map[string]any); ok {
		vars["original"] = original
	}
	return vars
}

// mergeEnrichment is a simple fallback when the DTS renderer is not available.
func mergeEnrichment(base, perLang, webhookFields map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(perLang)+len(webhookFields))
	maps.Copy(result, webhookFields)
	maps.Copy(result, base)
	maps.Copy(result, perLang)
	return result
}

// enrichPokemon parses and enriches a pokemon webhook. freshenStaleTime, when
// true, bumps an already-past DisappearTime into the near future — this is an
// editor-preview affordance (canned/typed sample JSON often carries a stale
// timestamp) and must stay false for the live /api/test path, which never
// applied this correction before enrichPokemon existed (see enrich_test.go /
// task-1 notes for the parity investigation).
func (ps *ProcessorService) enrichPokemon(raw json.RawMessage, language string, freshenStaleTime bool) (*enrichResult, error) {
	var pokemon webhook.PokemonWebhook
	if err := json.Unmarshal(raw, &pokemon); err != nil {
		return nil, fmt.Errorf("parse pokemon: %w", err)
	}

	if freshenStaleTime && pokemon.DisappearTime > 0 && pokemon.DisappearTime < time.Now().Unix() {
		pokemon.DisappearTime = time.Now().Unix() + 600
	}

	rarityGroup := ps.stats.GetRarityGroup(pokemon.PokemonID)
	processed := matching.ProcessPokemonWebhook(&pokemon, rarityGroup, ps.pvpCfg)
	base, tilePending := ps.enricher.Pokemon(&pokemon, processed, enrichment.TileModeURL)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.PokemonTranslate(base, &pokemon, language)
	}

	return &enrichResult{
		templateType:  "monster",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
		tilePending:   tilePending,
		extras:        map[string]any{"encountered": processed.Encountered},
	}, nil
}

// enrichRaid parses and enriches a raid/egg webhook. freshenStaleTime, when
// true, bumps an already-past Start/End window into the near future — an
// editor-preview affordance that must stay false for the live /api/test path
// (see enrichPokemon's freshenStaleTime doc for the full rationale).
func (ps *ProcessorService) enrichRaid(raw json.RawMessage, language string, isEgg, freshenStaleTime bool) (*enrichResult, error) {
	var raid webhook.RaidWebhook
	if err := json.Unmarshal(raw, &raid); err != nil {
		return nil, fmt.Errorf("parse raid: %w", err)
	}

	if freshenStaleTime && raid.Start > 0 && raid.End < time.Now().Unix() {
		raid.Start = time.Now().Unix() + 600
		raid.End = raid.Start + 1800
	}

	templateType := "raid"
	if isEgg || raid.PokemonID == 0 {
		templateType = "egg"
	}

	base, tilePending := ps.enricher.Raid(&raid, true, enrichment.TileModeURL)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.RaidTranslate(base, &raid, language)
	}

	return &enrichResult{
		templateType:  templateType,
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
		tilePending:   tilePending,
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
	base, tilePending := ps.enricher.Quest(quest.Latitude, quest.Longitude, quest.PokestopID, quest.URL, rewards, enrichment.TileModeURL)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.QuestTranslate(base, &quest, rewards, language)
	}

	return &enrichResult{
		templateType:  "quest",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
		tilePending:   tilePending,
	}, nil
}

// enrichInvasion parses and enriches an invasion/pokestop webhook.
// freshenStaleTime, when true, bumps an already-past IncidentExpiration into
// the near future — an editor-preview affordance that must stay false for the
// live /api/test path (see enrichPokemon's freshenStaleTime doc for the full
// rationale).
func (ps *ProcessorService) enrichInvasion(raw json.RawMessage, language string, freshenStaleTime bool) (*enrichResult, error) {
	return ps.enrichPokestopEvent(raw, language, freshenStaleTime, "invasion")
}

// enrichIncident parses and enriches an incident webhook (Gold Pokéstop,
// Kecleon, Pokémon-contest/Showcase display types). Incidents ride the exact
// same webhook.InvasionWebhook payload shape as invasions — Golbat
// distinguishes them only by gruntTypeID==0 with a display type >= 7 (see the
// isIncident split in cmd/processor/invasion.go's live ProcessInvasion path) —
// so this reuses enrichInvasion's enrichment core and only overrides the
// resulting templateType to "incident". That gives operators a simpler card
// without grunt/reward fields. freshenStaleTime has the same meaning as
// enrichInvasion's (see enrichPokemon's doc for the full rationale).
func (ps *ProcessorService) enrichIncident(raw json.RawMessage, language string, freshenStaleTime bool) (*enrichResult, error) {
	return ps.enrichPokestopEvent(raw, language, freshenStaleTime, "incident")
}

// enrichShowcase parses and enriches a showcase (contest) webhook — the
// dedicated "showcase" DTS template type (leaderboard + featured focus),
// distinct from the plain "incident" card. Showcases arrive as their own
// webhook.ShowcaseWebhook shape (not the InvasionWebhook the other pokestop
// events share), so this can't route through enrichPokestopEvent; it mirrors
// the live ProcessShowcase / processTestShowcase enrichment exactly —
// enricher.Invasion with a synthesised display_type, InvasionTranslate over
// the rankings, and ShowcaseFocusTranslate merged into the same per-language
// map. The AlertType stays "incident" at render time (showcases are tracked,
// rate-limited and blocked as incidents); only the templateType is "showcase".
// freshenStaleTime, when true, bumps an already-past ShowcaseExpiry into the
// near future — the same editor-preview affordance as enrichInvasion (see
// enrichPokemon's freshenStaleTime doc for the full rationale).
func (ps *ProcessorService) enrichShowcase(raw json.RawMessage, language string, freshenStaleTime bool) (*enrichResult, error) {
	var sc webhook.ShowcaseWebhook
	if err := json.Unmarshal(raw, &sc); err != nil {
		return nil, fmt.Errorf("parse showcase: %w", err)
	}

	if freshenStaleTime && sc.ShowcaseExpiry > 0 && sc.ShowcaseExpiry < time.Now().Unix() {
		sc.ShowcaseExpiry = time.Now().Unix() + 600
	}

	base, tilePending := ps.enricher.Invasion(
		sc.Latitude, sc.Longitude, sc.ShowcaseExpiry, sc.PokestopID, sc.URL,
		0, showcaseDisplayType, 0, enrichment.TileModeURL)
	// The showcase webhook carries the pokéstop name in `name`; Invasion()
	// takes no name arg, so surface it as pokestop_name for the alias.
	base["pokestop_name"] = sc.Name

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.InvasionTranslate(base, sc.Latitude, sc.Longitude, 0, nil, sc.ShowcaseRankings, language)
		maps.Copy(perLang, ps.enricher.ShowcaseFocusTranslate(sc.ShowcaseFocus, language))
	}

	return &enrichResult{
		templateType:  "showcase",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
		tilePending:   tilePending,
	}, nil
}

// enrichPokestopEvent is the shared enrichment core for enrichInvasion and
// enrichIncident: both parse the same webhook.InvasionWebhook shape and
// enrich through enricher.Invasion/InvasionTranslate; only the resulting
// templateType differs between callers.
func (ps *ProcessorService) enrichPokestopEvent(raw json.RawMessage, language string, freshenStaleTime bool, templateType string) (*enrichResult, error) {
	var inv webhook.InvasionWebhook
	if err := json.Unmarshal(raw, &inv); err != nil {
		return nil, fmt.Errorf("parse invasion: %w", err)
	}

	if freshenStaleTime && inv.IncidentExpiration > 0 && inv.IncidentExpiration < time.Now().Unix() {
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

	base, tilePending := ps.enricher.Invasion(inv.Latitude, inv.Longitude, expiration, inv.PokestopID, inv.URL, gruntTypeID, displayType, 0, enrichment.TileModeURL)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.InvasionTranslate(base, inv.Latitude, inv.Longitude, gruntTypeID, inv.Lineup, inv.ShowcaseRankings, language)
	}

	return &enrichResult{
		templateType:  templateType,
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
		tilePending:   tilePending,
	}, nil
}

func (ps *ProcessorService) enrichLure(raw json.RawMessage, language string) (*enrichResult, error) {
	var lure webhook.LureWebhook
	if err := json.Unmarshal(raw, &lure); err != nil {
		return nil, fmt.Errorf("parse lure: %w", err)
	}

	base, tilePending := ps.enricher.Lure(&lure, enrichment.TileModeURL)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.LureTranslate(base, &lure, language)
	}

	return &enrichResult{
		templateType:  "lure",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
		tilePending:   tilePending,
	}, nil
}

func (ps *ProcessorService) enrichNest(raw json.RawMessage, language string) (*enrichResult, error) {
	var nest webhook.NestWebhook
	if err := json.Unmarshal(raw, &nest); err != nil {
		return nil, fmt.Errorf("parse nest: %w", err)
	}

	base, tilePending := ps.enricher.Nest(&nest, enrichment.TileModeURL)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.NestTranslate(base, &nest, language)
	}

	return &enrichResult{
		templateType:  "nest",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
		tilePending:   tilePending,
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

	base, tilePending := ps.enricher.Gym(gym.Latitude, gym.Longitude, teamID, 0, gym.SlotsAvailable, -1, inBattle, false, gymID, enrichment.TileModeURL)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		// Editor preview has no team-change context, so suppress previousControl* fields.
		perLang = ps.enricher.GymTranslate(base, gym.Latitude, gym.Longitude, teamID, -1, -1, language)
	}

	return &enrichResult{
		templateType:  "gym",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
		tilePending:   tilePending,
	}, nil
}

func (ps *ProcessorService) enrichFort(raw json.RawMessage, language string) (*enrichResult, error) {
	var fort webhook.FortWebhook
	if err := json.Unmarshal(raw, &fort); err != nil {
		return nil, fmt.Errorf("parse fort: %w", err)
	}

	base, tilePending := ps.enricher.FortUpdate(fort.Latitude(), fort.Longitude(), fort.FortID(), &fort, enrichment.TileModeURL)
	perLang := ps.enricher.FortUpdateTranslate(fort.Latitude(), fort.Longitude(), language)

	return &enrichResult{
		templateType:  "fort-update",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
		tilePending:   tilePending,
	}, nil
}

func (ps *ProcessorService) enrichMaxbattle(raw json.RawMessage, language string) (*enrichResult, error) {
	var mb webhook.MaxbattleWebhook
	if err := json.Unmarshal(raw, &mb); err != nil {
		return nil, fmt.Errorf("parse maxbattle: %w", err)
	}

	base, tilePending := ps.enricher.Maxbattle(mb.Latitude, mb.Longitude, mb.BattleEnd, &mb, enrichment.TileModeURL)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		perLang = ps.enricher.MaxbattleTranslate(base, &mb, language)
	}

	return &enrichResult{
		templateType:  "maxbattle",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
		tilePending:   tilePending,
	}, nil
}

// enrichWeatherChange builds enrichment for the derived "weatherchange" DTS
// test type: a weather-cell transition (old→new gameplay condition) plus a
// short list of nearby pokemon affected by the boost change.
//
// Unlike the live path (consumeWeatherChanges in cmd/processor/weather.go),
// which discovers caring users and affected pokemon from live tracker state
// (WeatherCareTracker.GetCaringUsers, ActivePokemonTracker.GetAffectedPokemon)
// fed by a channel of detected changes, this function takes a
// fully-specified partial — the old/new weather IDs and the affected-pokemon
// list are supplied directly by the caller (a testdata.json sample or an
// editor preview payload), not derived from live cell state. It reuses the
// exact same pure enrichment calls consumeWeatherChanges uses
// (enricher.Weather / enricher.WeatherTranslate), so the resulting
// base/perLang fields match what a live weather alert would carry — nothing
// about rendering is re-derived here.
//
// showAlteredPokemonStaticMap is read from ps.cfg.Weather (nil-safe), exactly
// like the live consumeWeatherChanges path, so !poracle-test faithfully
// reproduces the live alert. It matters because WeatherTranslate only
// populates the "activePokemons" key — the one the bundled and operator
// weatherchange templates iterate ({{#each activePokemons}}) — when this flag
// is set. (The sibling "enrichedActivePokemons" key is always populated, but
// the shipped templates don't read it.) Hardcoding the flag false made the
// affected-pokemon list vanish from the test render even though production,
// with show_altered_pokemon_static_map on, shows it. The nil guard preserves
// the flag-off default for the enrich-parity test harness (which leaves
// ps.cfg nil).
func (ps *ProcessorService) enrichWeatherChange(raw json.RawMessage, language string, freshenStaleTime bool) (*enrichResult, error) {
	var wc webhook.WeatherChangeWebhook
	if err := json.Unmarshal(raw, &wc); err != nil {
		return nil, fmt.Errorf("parse weather change: %w", err)
	}

	// freshenStaleTime, when true, bumps any already-past affected-pokemon
	// DisappearTime into the near future — an editor-preview affordance
	// mirroring enrichPokemon's DisappearTime freshening (see its doc
	// comment for the full rationale). Must stay false for the live
	// /api/test path (processTestWeatherChange).
	if freshenStaleTime {
		now := time.Now().Unix()
		for i, ap := range wc.Affected {
			if ap.DisappearTime > 0 && ap.DisappearTime < now {
				wc.Affected[i].DisappearTime = now + 600 + int64(i)*60
			}
		}
	}

	showAlteredPokemonStaticMap := false
	if ps.cfg != nil {
		showAlteredPokemonStaticMap = ps.cfg.Weather.ShowAlteredPokemonStaticMap
	}
	base, tilePending := ps.enricher.Weather(wc.Latitude, wc.Longitude, wc.GameplayCondition, wc.Coords, showAlteredPokemonStaticMap, enrichment.TileModeURL, wc.S2CellID)

	var perLang map[string]any
	if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
		var userTilePending *staticmap.TilePending
		perLang, userTilePending = ps.enricher.WeatherTranslate(base, wc.OldGameplayCondition, wc.GameplayCondition, wc.Affected, language, showAlteredPokemonStaticMap, enrichment.TileModeURL, wc.S2CellID)
		// When show_altered_pokemon_static_map is on, enricher.Weather returns
		// no base tile — WeatherTranslate produces the per-user tile (with
		// active-pokemon markers) instead. Prefer it, otherwise keep the base
		// tile, mirroring the live consumeWeatherChanges selection
		// ("use per-user tile if available, otherwise base tile").
		if userTilePending != nil {
			tilePending = userTilePending
		}
	}

	return &enrichResult{
		templateType:  "weatherchange",
		base:          base,
		perLang:       perLang,
		webhookFields: parseWebhookFields(raw),
		tilePending:   tilePending,
		extras:        map[string]any{"affected": wc.Affected},
	}, nil
}

// questSummaryPartial is the structured shape enrichQuestSummary consumes
// for the derived "questSummary" DTS type: one reward group's shared
// (type, reward, form) key plus the per-pokestop quest webhooks that belong
// to it. It's a test/editor-preview convenience, not a real Golbat wire
// event — the live path (DispatchQuestSummary, cmd/processor/quest_summary_dispatch.go)
// builds groups itself from many buffered tracker.BufferedQuest rows that
// may span several different reward keys, then splits each group into
// chunks per config; here the caller supplies exactly one already-formed
// group (see fallbacks/testdata.json's "quest_summary"/"stardust" sample —
// the underscore spelling is the !poracle-test/API wire type; the DTS
// template-type name and dtsAlias key are both "questSummary", see
// resolveDTSTypeFromRaw), rendered unchunked (chunk=1, chunks=1).
type questSummaryPartial struct {
	Reward struct {
		Type int `json:"type"`
		ID   int `json:"reward"`
		Form int `json:"form"`
	} `json:"reward"`
	Quests []json.RawMessage `json:"quests"`
}

// enrichQuestSummary parses the questSummary partial, re-enriches each
// per-pokestop quest via questEnrichOne — the exact same helper
// DispatchQuestSummary uses to re-enrich buffered quests at delivery time —
// and builds the group view via buildQuestSummaryGroupView, also shared
// with DispatchQuestSummary, so the resulting fields match a live summary
// digest byte-for-byte. Nothing about rendering is re-derived here.
//
// freshenStaleTime is accepted for signature symmetry with the other
// Derived enrich* functions dispatched from enrichForType's switch, but is
// a no-op here: quests carry no expiring timestamp of their own to freshen
// (enrichQuest, the regular quest path, has no freshenStaleTime parameter
// either) — the daily quest reset is computed from lat/lon at render time
// (geo.EndOfDay), not read off the webhook.
func (ps *ProcessorService) enrichQuestSummary(raw json.RawMessage, language string, freshenStaleTime bool) (*enrichResult, error) {
	_ = freshenStaleTime

	var partial questSummaryPartial
	if err := json.Unmarshal(raw, &partial); err != nil {
		return nil, fmt.Errorf("parse quest summary: %w", err)
	}
	if len(partial.Quests) == 0 {
		return nil, fmt.Errorf("quest summary partial has no quests")
	}

	views := make([]map[string]any, 0, len(partial.Quests))
	for _, q := range partial.Quests {
		view := ps.questEnrichOne(q, language)
		if view == nil {
			continue
		}
		views = append(views, view)
	}
	if len(views) == 0 {
		return nil, fmt.Errorf("quest summary partial: no quest entries could be enriched")
	}

	base := ps.buildQuestSummaryGroupView(
		partial.Reward.Type, partial.Reward.ID, partial.Reward.Form,
		views, len(views), 1, 1, language,
	)

	return &enrichResult{
		templateType:  "questSummary",
		base:          base,
		webhookFields: parseWebhookFields(raw),
		extras:        map[string]any{"quests": views},
	}, nil
}

// monsterChangedPartial is the structured shape enrichMonsterChanged
// consumes for the derived "monsterChanged" DTS type: the prior sighting's
// pokemon webhook (`old`) and the current sighting's pokemon webhook (`new`)
// for the same encounter. It's a test/editor-preview convenience, not a real
// Golbat wire event — the live path (dispatchPokemonAlert in
// cmd/processor/pokemon.go) derives both states from
// tracker.EncounterTracker.Track's diff against repeated sightings of the
// same encounter_id over time; here the caller supplies both snapshots
// directly (see fallbacks/testdata.json's "monster_changed"/"ditto-reveal"
// sample — the underscore spelling is the !poracle-test/API wire type; the
// DTS template-type name and dtsAlias key are both "monsterChanged", see
// resolveDTSTypeFromRaw).
type monsterChangedPartial struct {
	Old json.RawMessage `json:"old"`
	New json.RawMessage `json:"new"`
}

// enrichMonsterChanged parses the monsterChanged partial, enriches `new` via
// enrichPokemon — the exact same helper the plain "pokemon"/"monster" test
// path uses — and builds the {{original.X}} prior-sighting view from `old`
// via tracker.EncounterStateFromPokemon + dts.BuildOriginalView. This is the
// same BuildOriginalView call dispatchPokemonAlert falls back to when no
// per-language OldWebhook re-enrichment is available (see its doc comment);
// unlike that live path, this never attempts the richer
// buildPerLanguageOriginal re-enrichment, since that helper needs a live
// target-user list and a fully wired WeatherProvider — neither fits an
// editor preview / single-shot test render. So {{original.X}} here is always
// the narrower BuildOriginalView field set: identity, battle stats, and
// translated names — no map/icon URLs.
//
// freshenStaleTime only affects `new` (via enrichPokemon) — `old` is a fixed
// prior point in time with no expiring timestamp templates read
// (BuildOriginalView never surfaces DisappearTime).
func (ps *ProcessorService) enrichMonsterChanged(raw json.RawMessage, language string, freshenStaleTime bool) (*enrichResult, error) {
	var partial monsterChangedPartial
	if err := json.Unmarshal(raw, &partial); err != nil {
		return nil, fmt.Errorf("parse monster changed: %w", err)
	}
	if len(partial.New) == 0 {
		return nil, fmt.Errorf("monster changed partial missing 'new' webhook")
	}
	if len(partial.Old) == 0 {
		return nil, fmt.Errorf("monster changed partial missing 'old' webhook")
	}

	result, err := ps.enrichPokemon(partial.New, language, freshenStaleTime)
	if err != nil {
		return nil, fmt.Errorf("enrich monster changed (new): %w", err)
	}

	var oldPokemon webhook.PokemonWebhook
	if err := json.Unmarshal(partial.Old, &oldPokemon); err != nil {
		return nil, fmt.Errorf("parse monster changed (old): %w", err)
	}
	priorState := tracker.EncounterStateFromPokemon(&oldPokemon)
	original := dts.BuildOriginalView(priorState, ps.enricher.GameData, ps.translatorFor(language))

	// The live path (dispatchPokemonAlert) gets its "species"/"stats" bucket
	// from a tracker.ChangeType that tracker.EncounterTracker.Track already
	// diffed. Here old/new arrive as a fully-specified pair rather than being
	// diffed by Track, so there's no ChangeType to hand to changeTypeBucket —
	// the bucket is computed directly from the two EncounterState snapshots
	// (see monsterChangedBucket). Without this, changeType/changeTypeText
	// never land in perLang and the bundled monsterChanged templates render
	// a dangling "— " ({{changeTypeText}} is unguarded — see fallbacks/dts.json).
	var newPokemon webhook.PokemonWebhook
	if err := json.Unmarshal(partial.New, &newPokemon); err != nil {
		return nil, fmt.Errorf("parse monster changed (new): %w", err)
	}
	newState := tracker.EncounterStateFromPokemon(&newPokemon)
	bucket := monsterChangedBucket(priorState, newState)

	result.templateType = "monsterChanged"
	if result.perLang == nil {
		result.perLang = map[string]any{}
	}
	result.perLang["changeType"] = bucket
	result.perLang["changeTypeText"] = ps.changeTypeText(language, bucket)

	if result.extras == nil {
		result.extras = map[string]any{}
	}
	result.extras["original"] = original
	// extras["changeType"] feeds RenderJob.ChangeType (logging only — see
	// processTestMonsterChanged) with a representative value instead of the
	// generic "test" placeholder.
	result.extras["changeType"] = bucket

	return result, nil
}

// monsterChangedBucket computes the "species"/"stats" change bucket for the
// monsterChanged derived test/editor-preview path directly from the two
// EncounterState snapshots the testdata.json partial supplies (old/new).
// Mirrors changeTypeBucket's species-vs-stats classification (pokemon.go)
// but works off identity fields since there's no live tracker.ChangeType
// here: PokemonID/Form/Gender differing is an identity ("species") change —
// same field comparisons and gender zero-guard as
// tracker.EncounterTracker.Track's ChangeSpecies/ChangeForm/ChangeGender
// cases; anything else (encountered, weather-boost CP shift, raw stat
// drift) is bucketed as "stats".
func monsterChangedBucket(old, newState tracker.EncounterState) string {
	if old.PokemonID != newState.PokemonID ||
		old.Form != newState.Form ||
		(old.Gender != newState.Gender && old.Gender != 0 && newState.Gender != 0) {
		return "species"
	}
	return "stats"
}

// enrichRsvpChanges parses the derived "rsvpChanges" DTS test type's
// partial — a raid webhook with its `rsvps` array populated (see
// fallbacks/testdata.json's "rsvp_changes" sample — the underscore spelling
// is the !poracle-test/API wire type; the DTS template-type name and
// dtsAlias key are both "rsvpChanges", see resolveDTSTypeFromRaw). Unlike
// monsterChanged/questSummary/weatherchange, this derived type needs no
// bespoke partial shape: webhook.RaidWebhook already carries the RSVP list
// natively (`rsvps`), so this reuses enrichRaid wholesale — the same helper
// the plain "raid"/"egg" test path uses — and only adds
// extras["overrideCleanTTH"].
//
// The live path (ProcessRaid in cmd/processor/raid.go) computes
// OverrideCleanTTH as the latest future RSVP timeslot via
// latestFutureTimeslotSec, so that a compact rsvpChanges message cleans up
// at the RSVP window's edge rather than raid.End. This mirrors that exact
// computation off the same partial's `rsvps` list, rather than deriving it
// from a live in-flight raid the way ProcessRaid does.
//
// freshenStaleTime is forwarded to enrichRaid unchanged (bumping a stale
// Start/End window for the editor preview — see enrichRaid's doc comment);
// it does not touch the `rsvps` timeslots themselves; only future timeslots
// contribute to enricher.Raid's own `rsvps` field and to
// latestFutureTimeslotSec below (see internal/enrichment/raid.go), so a
// testdata sample must carry safely-future timeslot values on its own (as
// fallbacks/testdata.json's "rsvp_changes" sample does) for either
// freshenStaleTime setting to see a populated RSVP list and a non-zero
// OverrideCleanTTH.
func (ps *ProcessorService) enrichRsvpChanges(raw json.RawMessage, language string, freshenStaleTime bool) (*enrichResult, error) {
	// isEgg=false: rsvpChanges test sends always reuse enrichRaid's own
	// raid-vs-egg inference from the payload's PokemonID; the alias override
	// below (templateType = "rsvpChanges") applies regardless of which one
	// enrichRaid picked.
	result, err := ps.enrichRaid(raw, language, false, freshenStaleTime)
	if err != nil {
		return nil, fmt.Errorf("enrich rsvp changes: %w", err)
	}

	var raid webhook.RaidWebhook
	if err := json.Unmarshal(raw, &raid); err != nil {
		return nil, fmt.Errorf("parse rsvp changes: %w", err)
	}

	rsvps := make([]tracker.RaidRSVP, len(raid.RSVPs))
	for i, r := range raid.RSVPs {
		rsvps[i] = tracker.RaidRSVP{Timeslot: r.Timeslot, GoingCount: r.GoingCount, MaybeCount: r.MaybeCount}
	}
	latest := latestFutureTimeslotSec(rsvps, time.Now().UnixMilli())

	result.templateType = "rsvpChanges"
	if result.extras == nil {
		result.extras = map[string]any{}
	}
	// overrideCleanTTH is 0 when no future RSVP timeslot exists — mirrors
	// ProcessRaid's "use the render pool's default path" fallback (raid.End
	// from the enrichment map); processTestRsvpChanges only sets
	// RenderJob.OverrideCleanTTH when this is non-zero (see its doc comment).
	result.extras["overrideCleanTTH"] = latest

	return result, nil
}
