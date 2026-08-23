package main

import (
	"encoding/json"
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/delivery"
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
		ID:        target.ID,
		Name:      target.Name,
		Type:      target.Type,
		Language:  target.Language,
		Latitude:  target.Latitude,
		Longitude: target.Longitude,
		Template:  target.Template,
		Clean:     0,
	}

	switch webhookType {
	case "pokemon":
		return ps.processTestPokemon(raw, matchedUser)
	case "raid", "egg":
		return ps.processTestRaid(raw, matchedUser)
	case "invasion":
		return ps.processTestInvasion(raw, matchedUser)
	case "incident":
		return ps.processTestIncident(raw, matchedUser)
	case "weatherchange":
		return ps.processTestWeatherChange(raw, matchedUser)
	case "monster_changed":
		return ps.processTestMonsterChanged(raw, matchedUser)
	case "quest":
		return ps.processTestQuest(raw, matchedUser)
	case "quest_summary":
		return ps.processTestQuestSummary(raw, matchedUser)
	case "rsvp_changes":
		return ps.processTestRsvpChanges(raw, matchedUser)
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
	case "showcase":
		return ps.processTestShowcase(raw, matchedUser)
	default:
		return fmt.Errorf("unsupported test webhook type: %s", webhookType)
	}
}

// renderJobFromEnrich wraps a shared enrichResult into a delivery RenderJob for
// a single test target. perUser is computed here with the REAL target user
// (unlike the editor's synthetic user). raw is unused today (enrichment
// already parsed it) but kept in the signature for symmetry with the
// enrich* functions and for future derived-type handling (raid RSVP, etc.).
func (ps *ProcessorService) renderJobFromEnrich(r *enrichResult, target webhook.MatchedUser, alertType string, raw json.RawMessage, isPokemon, isEncountered bool) RenderJob {
	matched := []webhook.MatchedUser{target}
	perLang := map[string]map[string]any{}
	if r.perLang != nil {
		perLang[target.Language] = r.perLang
	}
	var perUser map[string]map[string]any
	if isPokemon && ps.enricher.PVPDisplay != nil && r.perLang != nil {
		perUser = ps.enricher.PokemonPerUser(perLang, matched)
	}
	return RenderJob{
		AlertType:         alertType,
		TemplateType:      r.templateType,
		IsPokemon:         isPokemon,
		IsEncountered:     isEncountered,
		Enrichment:        r.base,
		PerLangEnrichment: perLang,
		PerUserEnrichment: perUser,
		WebhookFields:     r.webhookFields,
		MatchedUsers:      matched,
		MatchedAreas:      []webhook.MatchedArea{},
		TileGate:          ps.newTileGate(r.tilePending),
		LogReference:      "test",
	}
}

func (ps *ProcessorService) processTestPokemon(raw json.RawMessage, target webhook.MatchedUser) error {
	r, err := ps.enrichPokemon(raw, target.Language, false)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	isEncountered := false
	if v, ok := r.extras["encountered"].(bool); ok {
		isEncountered = v
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, "pokemon", raw, true, isEncountered)
	return nil
}

func (ps *ProcessorService) processTestRaid(raw json.RawMessage, target webhook.MatchedUser) error {
	// isEgg=false: the actual type is always determined by raid.PokemonID
	// inside enrichRaid (isEgg only forces "egg" for the explicit /api/test
	// "egg" webhookType passthrough, which this test path never uses since
	// both "raid" and "egg" webhookType route here and let the payload decide).
	// freshenStaleTime=false: preserves this path's pre-existing behaviour of
	// never bumping a stale Start/End window (see enrichRaid's doc comment).
	r, err := ps.enrichRaid(raw, target.Language, false, false)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, r.templateType, raw, false, false)
	return nil
}

// processTestRsvpChanges handles !poracle-test rsvp-changes,<id> (wire type
// "rsvp_changes" — see resolveDTSTypeFromRaw). Mirrors the compact
// rsvpChanges RenderJob ProcessRaid constructs (cmd/processor/raid.go) for
// prior-tracked users who should get the RSVP-only update instead of the
// full raid/egg template: TemplateType "rsvpChanges", OverrideCleanTTH set
// to the latest future RSVP timeslot (computed by enrichRsvpChanges), and
// the same raidlife EditKey/ReplyKey convention so a test send lines up with
// any live raid/egg message already tracked for the same gym+end lifecycle.
// freshenStaleTime=false mirrors processTestRaid's pre-existing convention
// of never bumping a stale Start/End window on the live /api/test path (see
// enrichRaid's doc comment for the full rationale).
func (ps *ProcessorService) processTestRsvpChanges(raw json.RawMessage, target webhook.MatchedUser) error {
	r, err := ps.enrichRsvpChanges(raw, target.Language, false)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}

	var raid webhook.RaidWebhook
	if err := json.Unmarshal(raw, &raid); err != nil {
		return fmt.Errorf("parse rsvp changes: %w", err)
	}

	job := ps.renderJobFromEnrich(r, target, "raid", raw, false, false)
	job.TemplateType = "rsvpChanges"
	job.EditKey = fmt.Sprintf(raidEditKeyFmt, raid.GymID, raid.End)
	job.ReplyKey = fmt.Sprintf(raidReplyKeyFmt, raid.GymID, raid.End)
	// OverrideCleanTTH is only set when we have a real future timeslot —
	// mirrors ProcessRaid's own "0 means use the default path" convention
	// (see enrichRsvpChanges's doc comment).
	if tth, ok := r.extras["overrideCleanTTH"].(int64); ok && tth > 0 {
		job.OverrideCleanTTH = tth
	}

	ps.renderCh <- job
	return nil
}

func (ps *ProcessorService) processTestInvasion(raw json.RawMessage, target webhook.MatchedUser) error {
	// freshenStaleTime=false: preserves this path's pre-existing behaviour of
	// never bumping a stale IncidentExpiration (see enrichInvasion's doc comment).
	r, err := ps.enrichInvasion(raw, target.Language, false)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, "invasion", raw, false, false)
	return nil
}

func (ps *ProcessorService) processTestIncident(raw json.RawMessage, target webhook.MatchedUser) error {
	// freshenStaleTime=false: mirrors processTestInvasion's pre-existing
	// behaviour of never bumping a stale IncidentExpiration/ExpireTimestamp
	// (see enrichInvasion's doc comment; enrichIncident shares the same flag).
	r, err := ps.enrichIncident(raw, target.Language, false)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, "incident", raw, false, false)
	return nil
}

// processTestWeatherChange handles !poracle-test weatherchange,<id>. Like
// processTestIncident, freshenStaleTime=false preserves this path's
// pre-existing convention of never bumping stale timestamps in the live test
// path (see enrichPokemon's doc comment for the full rationale); the
// per-affected-pokemon disappear_time freshening !poracle-test needs instead
// happens in bot/commands/poracletest.go before ProcessTest is even called
// (mirroring how it freshens disappear_time/start/end for the other types).
func (ps *ProcessorService) processTestWeatherChange(raw json.RawMessage, target webhook.MatchedUser) error {
	r, err := ps.enrichWeatherChange(raw, target.Language, false)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, "weatherchange", raw, false, false)
	return nil
}

// processTestMonsterChanged handles !poracle-test monster-changed,<id> (wire
// type "monster_changed" — see resolveDTSTypeFromRaw). Unlike the live path
// (dispatchPokemonAlert in cmd/processor/pokemon.go), which buckets
// prior-only users into a monsterChanged render only after
// tracker.EncounterTracker.Track detects a real diff between two sightings
// of the same encounter_id, this renders the testdata partial's
// already-distinct old/new pair straight through: enrichMonsterChanged
// builds the `new` state (base/perLang, same as a plain pokemon test send)
// plus the {{original.X}} view from `old`. The resulting RenderJob carries
// IsChange=true / OriginalView so it goes through
// dtsRenderer.RenderPokemonChanged exactly like a live change notification
// would (see processRenderJob's IsChange branch).
func (ps *ProcessorService) processTestMonsterChanged(raw json.RawMessage, target webhook.MatchedUser) error {
	// freshenStaleTime=false: mirrors processTestPokemon's pre-existing
	// convention of never bumping a stale DisappearTime on the live
	// /api/test path (see enrichPokemon's doc comment).
	r, err := ps.enrichMonsterChanged(raw, target.Language, false)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}

	var partial monsterChangedPartial
	if err := json.Unmarshal(raw, &partial); err != nil {
		return fmt.Errorf("parse monster changed: %w", err)
	}
	// Peek encounter_id off `new` for ReplyKey — mirrors the live path,
	// where every pokemon RenderJob carries ReplyKey = encounterID so a
	// later change can find the prior message via the reply index.
	var newPeek struct {
		EncounterID string `json:"encounter_id"`
	}
	_ = json.Unmarshal(partial.New, &newPeek) // best-effort; empty ReplyKey just skips reply-threading

	isEncountered := false
	if v, ok := r.extras["encountered"].(bool); ok {
		isEncountered = v
	}

	job := ps.renderJobFromEnrich(r, target, "pokemon", partial.New, true, isEncountered)
	job.IsChange = true
	job.TemplateType = "monsterChanged"
	// ChangeType is logging-only (see RenderJob.ChangeType doc). Prefer the
	// bucket enrichMonsterChanged computed from old/new (extras["changeType"]);
	// fall back to the "test" placeholder if it's ever missing.
	job.ChangeType = "test"
	if ct, ok := r.extras["changeType"].(string); ok && ct != "" {
		job.ChangeType = ct
	}
	job.ReplyKey = newPeek.EncounterID
	if original, ok := r.extras["original"].(map[string]any); ok {
		job.OriginalView = original
	}

	ps.renderCh <- job
	return nil
}

func (ps *ProcessorService) processTestShowcase(raw json.RawMessage, target webhook.MatchedUser) error {
	// Shares the exact enrichment core with the editor's /api/dts/enrich path
	// (enrichForType -> enrichShowcase). AlertType is "incident" — showcases
	// are tracked, rate-limited and blocked as incidents — while the
	// enrichResult's templateType is the dedicated "showcase" display model.
	r, err := ps.enrichShowcase(raw, target.Language, false)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, "incident", raw, false, false)
	return nil
}

func (ps *ProcessorService) processTestQuest(raw json.RawMessage, target webhook.MatchedUser) error {
	r, err := ps.enrichQuest(raw, target.Language)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, "quest", raw, false, false)
	return nil
}

// processTestQuestSummary handles !poracle-test quest-summary,<id> (wire
// type "quest_summary" — see resolveDTSTypeFromRaw). Unlike the live
// scheduler (DispatchQuestSummary), which pulls buffered quests from the
// SummaryBuffer, groups them by (rewardType, reward, form), and dispatches
// one questSummary message per group via DispatchBypass, this test path
// renders exactly the single already-grouped reward the testdata partial
// supplies (see enrichQuestSummary) through the standard renderCh/RenderAlert
// pipeline used by every other !poracle-test handler — so a test digest
// goes through the normal render pool and rate limiting like any other test
// alert, rather than bypassing it the way a real scheduled summary does.
func (ps *ProcessorService) processTestQuestSummary(raw json.RawMessage, target webhook.MatchedUser) error {
	r, err := ps.enrichQuestSummary(raw, target.Language, false)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, AlertTypeQuest, raw, false, false)
	return nil
}

func (ps *ProcessorService) processTestGym(raw json.RawMessage, target webhook.MatchedUser) error {
	r, err := ps.enrichGym(raw, target.Language)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, "gym", raw, false, false)
	return nil
}

func (ps *ProcessorService) processTestNest(raw json.RawMessage, target webhook.MatchedUser) error {
	r, err := ps.enrichNest(raw, target.Language)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, "nest", raw, false, false)
	return nil
}

func (ps *ProcessorService) processTestFort(raw json.RawMessage, target webhook.MatchedUser) error {
	r, err := ps.enrichFort(raw, target.Language)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, "fort-update", raw, false, false)
	return nil
}

func (ps *ProcessorService) processTestMaxbattle(raw json.RawMessage, target webhook.MatchedUser) error {
	r, err := ps.enrichMaxbattle(raw, target.Language)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	ps.renderCh <- ps.renderJobFromEnrich(r, target, "maxbattle", raw, false, false)
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
		r, err := ps.enrichLure(raw, target.Language)
		if err != nil {
			return err
		}
		if ps.renderCh == nil {
			return fmt.Errorf("render queue not available")
		}
		ps.renderCh <- ps.renderJobFromEnrich(r, target, "lure", raw, false, false)
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
	case "showcase":
		return "showcase"
	case "quest_summary":
		return "questSummary"
	case "monster_changed":
		return "monsterChanged"
	case "rsvp_changes":
		return "rsvpChanges"
	default:
		return webhookType
	}
}

// Ensure ProcessorService implements TestProcessor
var _ bot.TestProcessor = (*ProcessorService)(nil)
