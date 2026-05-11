package main

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessPokemon(raw json.RawMessage) error {
	if ps.cfg.General.DisablePokemon {
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
			metrics.WebhookProcessingDuration.WithLabelValues("pokemon").Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

		var pokemon webhook.PokemonWebhook
		if err := json.Unmarshal(raw, &pokemon); err != nil {
			log.Errorf("Failed to parse pokemon webhook: %s", err)
			return
		}

		l := log.WithField("ref", pokemon.EncounterID)

		// Record for rarity and shiny tracking
		ivScanned := pokemon.IndividualAttack != nil
		isShiny := pokemon.Shiny != nil && *pokemon.Shiny
		ps.stats.RecordSighting(pokemon.PokemonID, ivScanned, isShiny)

		// Duplicate check
		verified := pokemon.Verified || pokemon.DisappearTimeVerified
		if ps.duplicates.CheckPokemon(pokemon.EncounterID, verified, pokemon.CP, pokemon.DisappearTime) {
			l.Debug("Wild encounter was sent again too soon, ignoring")
			metrics.DuplicatesSkipped.WithLabelValues("pokemon").Inc()
			return
		}

		// Weather inference
		if pokemon.Weather > 0 && ps.cfg.Weather.EnableInference {
			cellID := tracker.GetWeatherCellID(pokemon.Latitude, pokemon.Longitude)
			ps.weather.CheckWeatherOnMonster(cellID, pokemon.Latitude, pokemon.Longitude, pokemon.Weather)
		}

		// Encounter tracking (change detection)
		atk, def, sta := 0, 0, 0
		if pokemon.IndividualAttack != nil {
			atk = *pokemon.IndividualAttack
		}
		if pokemon.IndividualDefense != nil {
			def = *pokemon.IndividualDefense
		}
		if pokemon.IndividualStamina != nil {
			sta = *pokemon.IndividualStamina
		}
		weather := pokemon.Weather
		if pokemon.BoostedWeather > 0 {
			weather = pokemon.BoostedWeather
		}
		encounterState := tracker.EncounterState{
			PokemonID:     pokemon.PokemonID,
			Form:          pokemon.Form,
			Gender:        pokemon.Gender,
			Weather:       weather,
			CP:            pokemon.CP,
			ATK:           atk,
			DEF:           def,
			STA:           sta,
			DisappearTime: pokemon.DisappearTime,
		}

		// Get rarity group
		rarityGroup := ps.stats.GetRarityGroup(pokemon.PokemonID)

		// Process pokemon into matching format
		processed := matching.ProcessPokemonWebhook(&pokemon, rarityGroup, ps.pvpCfg)

		// Match (against the new state — `processed` was built from the
		// just-parsed webhook, so this set is correct for both initial
		// sightings and change events).
		matched, matchedAreas := ps.pokemonMatcher.Match(processed, ps.stateMgr.Get())
		matched = ps.filterBlocked(matched)
		matched = ps.filterValidation("pokemon", raw, matchedAreas, matched)

		if len(matched) == 0 {
			if processed.Encountered {
				l.Debugf("%s{CP%d/IV%.0f%%} appeared at [%.3f,%.3f] and 0 humans cared",
					ps.pokemonName(pokemon.PokemonID, pokemon.Form), processed.CP, processed.IV,
					pokemon.Latitude, pokemon.Longitude)
			} else {
				l.Debugf("%s appeared at [%.3f,%.3f] and 0 humans cared",
					ps.pokemonName(pokemon.PokemonID, pokemon.Form),
					pokemon.Latitude, pokemon.Longitude)
			}
			return
		}

		metrics.MatchedEvents.WithLabelValues("pokemon").Inc()
		metrics.MatchedUsers.WithLabelValues("pokemon").Add(float64(len(matched)))
		metrics.IntervalMatched.Add(1)

		// Encounter tracking (change detection). Gated on len(matched) > 0:
		// pokemon nobody is tracking can't produce a `monsterChanged` event
		// for anyone (that requires a prior per-user message), so we don't
		// need their state or webhook in the tracker. Skipping the unmatched
		// path saves the value copy + tracker mutex on the common "0 humans
		// cared" case. Duplicate elimination is handled separately via
		// ps.duplicates.CheckPokemon above and does not depend on the
		// encounter tracker.
		//
		// We store a value copy with the PVP maps/slices nilled — PVP
		// rankings are the only heavy field on PokemonWebhook and aren't
		// used by the {{original.X}} render. Reference-type fields cleared
		// on the copy don't affect the original `pokemon` value used by
		// matching / enrichment downstream.
		var change *tracker.EncounterChange
		if ps.cfg.Tracking.PokemonChangeTracking {
			stored := pokemon
			stored.PVP = nil
			stored.PVPRankingsGreatLeague = nil
			stored.PVPRankingsUltraLeague = nil
			stored.PVPRankingsLittleLeague = nil
			_, change = ps.encounters.Track(pokemon.EncounterID, encounterState, &stored)
		}

		// Register matched users as caring about weather in this cell
		if ps.cfg.Weather.ChangeAlert {
			cellID := tracker.GetWeatherCellID(pokemon.Latitude, pokemon.Longitude)
			for _, u := range matched {
				ps.weatherCares.Register(cellID, tracker.WeatherCareEntry{
					ID:         u.ID,
					Name:       u.Name,
					Type:       u.Type,
					Language:   u.Language,
					Template:   u.Template,
					Clean:      u.Clean,
					Ping:       u.Ping,
					CaresUntil: pokemon.DisappearTime,
				})
			}

			// Track active pokemon per user for weather change alerts.
			// pokemon.Weather (webhook "weather" field) is the in-game boost
			// weather: >0 means the pokemon IS weather-boosted, 0 means not.
			// This matches PoracleJS's data.weather used by getAlteringWeathers.
			if ps.activePokemon != nil {
				types := ps.pokemonTypes.GetTypes(pokemon.PokemonID, pokemon.Form)
				boosted := pokemon.Weather > 0
				for _, u := range matched {
					ps.activePokemon.Register(cellID, u.ID, pokemon.EncounterID, tracker.ActivePokemon{
						PokemonID:     pokemon.PokemonID,
						Form:          pokemon.Form,
						IV:            processed.IV,
						CP:            processed.CP,
						Latitude:      pokemon.Latitude,
						Longitude:     pokemon.Longitude,
						DisappearTime: pokemon.DisappearTime,
						Boosted:       boosted,
						Types:         types,
					})
				}
			}
		}

		if processed.Encountered {
			l.Infof("%s{CP%d/IV%.0f%%} at [%.3f,%.3f] areas(%s) and %d humans cared",
				ps.pokemonName(pokemon.PokemonID, pokemon.Form), processed.CP, processed.IV,
				pokemon.Latitude, pokemon.Longitude, areaNames(matchedAreas), len(matched))
		} else {
			l.Infof("%s appeared at [%.3f,%.3f] areas(%s) and %d humans cared",
				ps.pokemonName(pokemon.PokemonID, pokemon.Form),
				pokemon.Latitude, pokemon.Longitude, areaNames(matchedAreas), len(matched))
		}

		enrichStart := time.Now()
		mode := ps.tileMode("monster", matched)
		baseEnrichment, tilePending := ps.enricher.Pokemon(&pokemon, processed, mode)

		// Compute per-language translated enrichment
		var perLang map[string]map[string]any
		if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
			perLang = make(map[string]map[string]any)
			for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
				perLang[lang] = ps.enricher.PokemonTranslate(baseEnrichment, &pokemon, lang)
			}
		}

		var perUser map[string]map[string]any
		if ps.enricher.PVPDisplay != nil && perLang != nil {
			perUser = ps.enricher.PokemonPerUser(perLang, matched)
		}
		metrics.EnrichmentDuration.WithLabelValues("pokemon").Observe(time.Since(enrichStart).Seconds())

		if ps.renderCh == nil {
			return
		}
		webhookFields := parseWebhookFields(raw)

		if change != nil {
			l.Infof("Pokemon changed (%s) from %s to %s",
				change.Type.String(),
				ps.pokemonName(change.Old.PokemonID, change.Old.Form),
				ps.pokemonName(change.New.PokemonID, change.New.Form))

			// Config gate: when pokemon change tracking is disabled, fall
			// through to the regular initial-render path. Matched users (if
			// any) still get a regular `monster` send — just no reply
			// threading and no `monsterChanged` template.
			if ps.cfg.Tracking.PokemonChangeTracking {
				ps.dispatchPokemonChangeRender(pokemonChangeRenderInput{
					encounterID:   pokemon.EncounterID,
					change:        change,
					matched:       matched,
					matchedAreas:  matchedAreas,
					enrichment:    baseEnrichment,
					perLang:       perLang,
					perUser:       perUser,
					webhookFields: webhookFields,
					tilePending:   tilePending,
					isEncountered: processed.Encountered,
				})
				return
			}
		}

		ps.renderCh <- RenderJob{
			IsPokemon:         true,
			IsEncountered:     processed.Encountered,
			Enrichment:        baseEnrichment,
			PerLangEnrichment: perLang,
			PerUserEnrichment: perUser,
			WebhookFields:     webhookFields,
			MatchedUsers:      matched,
			MatchedAreas:      matchedAreas,
			TilePending:       tilePending,
			LogReference:      pokemon.EncounterID,
			// Index this initial sighting under the encounter ID so a
			// subsequent change-event handler can find it via
			// MessageTracker.LookupReply and thread the change alert as
			// a reply to this message.
			ReplyKey: pokemon.EncounterID,
		}
	}()
	return nil
}

// pokemonChangeRenderInput bundles the inputs to dispatchPokemonChangeRender so
// the call site doesn't drag a long argument list. All fields refer to the
// NEW state of the encounter (the just-arrived webhook).
type pokemonChangeRenderInput struct {
	encounterID   string
	change        *tracker.EncounterChange
	matched       []webhook.MatchedUser
	matchedAreas  []webhook.MatchedArea
	enrichment    map[string]any
	perLang       map[string]map[string]any
	perUser       map[string]map[string]any
	webhookFields map[string]any
	tilePending   *staticmap.TilePending
	isEncountered bool
}

// dispatchPokemonChangeRender enqueues 0–2 RenderJobs for a pokemon change
// event:
//   - users with a prior tracked message for this encounter receive a reply
//     using the regular `monster` template for an encounter event (CP 0→>0)
//     or `monsterChanged` for a post-encounter change (form/species/gender/
//     weather-boost). The {{original.X}} bag is threaded in for monsterChanged.
//   - users with no prior message receive a fresh `monster` render. Their
//     ReplyKey is still indexed so any subsequent change can chain.
//
// Tile resolution for the batch is hoisted into a single goroutine via
// tileGate — every RenderJob below shares the same Enrichment map, so we
// can't let one render worker mutate it via TilePending.Apply while sibling
// workers read from it. The gate uses chan-close happens-before to make the
// map writes visible to every worker before it renders. All jobs share
// enrichment, per-language data, and the resolved tile bytes.
func (ps *ProcessorService) dispatchPokemonChangeRender(in pokemonChangeRenderInput) {
	if ps.dispatcher == nil {
		// Without a dispatcher there's no MessageTracker to consult — fall
		// back to a single regular render so we don't silently drop the alert
		// in tests or partial-init scenarios.
		ps.renderCh <- RenderJob{
			IsPokemon:         true,
			IsEncountered:     in.isEncountered,
			Enrichment:        in.enrichment,
			PerLangEnrichment: in.perLang,
			PerUserEnrichment: in.perUser,
			WebhookFields:     in.webhookFields,
			MatchedUsers:      in.matched,
			MatchedAreas:      in.matchedAreas,
			TilePending:       in.tilePending,
			LogReference:      in.encounterID,
			ReplyKey:          in.encounterID,
		}
		return
	}

	withPrior, withoutPrior := partitionByPriorMessage(in.matched, in.encounterID, ps.dispatcher.MessageTracker())

	encounterEvent := in.change.Old.CP == 0 && in.change.New.CP > 0

	// Multi-job dispatch: every RenderJob below shares the same in.enrichment
	// map. If we passed TilePending to one job and let its render worker
	// resolve the tile (which mutates the map via Apply), a sibling worker
	// rendering another job would race with that write. Hoist the tile
	// resolution into a single goroutine and have every render worker wait
	// on the gate before reading the map. The single writer + chan-close
	// happens-before makes the map safe for concurrent readers afterwards.
	//
	// Also fixes a per-job-bytes bug: previously only the TilePending-holding
	// job received the inline tile bytes; sibling jobs in the same dispatch
	// got nil even though they're for the same tile. With the gate, every
	// job in the batch picks up the shared bytes.
	var gate *tileGate
	if in.tilePending != nil {
		gate = &tileGate{ready: make(chan struct{})}
		queueLen, queueCap := len(ps.renderCh), cap(ps.renderCh)
		pending := in.tilePending
		in.tilePending = nil
		go func() {
			defer close(gate.ready)
			gate.bytes = ps.resolveTilePending(pending, queueLen, queueCap)
		}()
	}
	if len(withPrior) > 0 {
		isChange := !encounterEvent
		// Build a per-language `original` map by re-running base + per-language
		// enrichment against the prior webhook bytes. Falls back to
		// dts.BuildOriginalView when we don't have prior bytes (e.g. the
		// tracker entry pre-dates the bytes-storage upgrade, or tests that
		// don't supply a webhook). For encounter events (CP 0→>0) we leave
		// original nil — the regular `monster` template fires there, not
		// monsterChanged.
		var perLangOriginal map[string]map[string]any
		var fallbackOriginal map[string]any
		if !encounterEvent {
			if in.change.OldWebhook != nil {
				perLangOriginal = ps.buildPerLanguageOriginal(in.change.OldWebhook, withPrior)
			}
			if perLangOriginal == nil {
				// Older path or test path — use the hand-picked subset.
				fallbackOriginal = dts.BuildOriginalView(in.change.Old, ps.enricher.GameData, ps.translatorFor(""))
			}
		}

		// Group withPrior users by language so we can dispatch one RenderJob
		// per language, each carrying the language-specific original. Encounter
		// events don't need grouping (no original to differentiate); a single
		// RenderJob suffices.
		if perLangOriginal != nil {
			byLang := groupByLanguage(withPrior, ps.cfg.General.Locale)
			for lang, users := range byLang {
				orig := perLangOriginal[lang]
				ps.renderCh <- RenderJob{
					IsPokemon:         true,
					IsChange:          isChange,
					IsEncountered:     in.isEncountered,
					Enrichment:        in.enrichment,
					PerLangEnrichment: in.perLang,
					PerUserEnrichment: in.perUser,
					WebhookFields:     in.webhookFields,
					MatchedUsers:      users,
					MatchedAreas:      in.matchedAreas,
					TileGate:          gate,
					LogReference:      in.encounterID,
					ReplyKey:          in.encounterID,
					OriginalView:      orig,
					ChangeType:        in.change.Type.String(),
				}
			}
		} else {
			ps.renderCh <- RenderJob{
				IsPokemon:         true,
				IsChange:          isChange,
				IsEncountered:     in.isEncountered,
				Enrichment:        in.enrichment,
				PerLangEnrichment: in.perLang,
				PerUserEnrichment: in.perUser,
				WebhookFields:     in.webhookFields,
				MatchedUsers:      withPrior,
				MatchedAreas:      in.matchedAreas,
				TileGate:          gate,
				LogReference:      in.encounterID,
				ReplyKey:          in.encounterID,
				OriginalView:      fallbackOriginal,
				ChangeType:        in.change.Type.String(),
			}
		}
	}

	if len(withoutPrior) > 0 {
		ps.renderCh <- RenderJob{
			IsPokemon:         true,
			IsEncountered:     in.isEncountered,
			Enrichment:        in.enrichment,
			PerLangEnrichment: in.perLang,
			PerUserEnrichment: in.perUser,
			WebhookFields:     in.webhookFields,
			MatchedUsers:      withoutPrior,
			MatchedAreas:      in.matchedAreas,
			TileGate:          gate,
			LogReference:      in.encounterID,
			ReplyKey:          in.encounterID,
		}
	}
}

// buildPerLanguageOriginal re-runs the regular pokemon enrichment pipeline
// against the prior-sighting webhook (PVP fields cleared at storage time)
// and returns one merged base+perLang map per distinct language among the
// supplied users. The result becomes RenderJob.OriginalView; LayeredView
// exposes it under {{original.X}}.
//
// Static-map tile generation is skipped (TileModeSkip) — the position
// doesn't change between prior and current sightings, so the tile from the
// new state's render is reused implicitly. Returns nil when the enricher
// isn't configured for translation; the caller should fall back to
// dts.BuildOriginalView in that case.
func (ps *ProcessorService) buildPerLanguageOriginal(prior *webhook.PokemonWebhook, users []webhook.MatchedUser) map[string]map[string]any {
	if ps.enricher == nil || prior == nil {
		return nil
	}
	// Without a WeatherProvider the regular Pokemon enrichment path panics
	// (e.WeatherProvider.GetCurrentWeatherInCell). Treat partial-enricher
	// setups (tests, early-init) as "no per-language original" so the caller
	// falls back to the safer dts.BuildOriginalView path.
	if ps.enricher.WeatherProvider == nil {
		return nil
	}
	rarityGroup := 0
	if ps.stats != nil {
		rarityGroup = ps.stats.GetRarityGroup(prior.PokemonID)
	}
	pvpCfg := ps.pvpCfg
	if pvpCfg == nil {
		// pvp.Calculate dereferences cfg; supply a zero-value to keep the
		// helper crash-safe in partial-init contexts (tests). The prior
		// webhook is PVP-stripped anyway, so PVP fields will be empty either
		// way — original.* doesn't expose PVP.
		pvpCfg = &pvp.Config{}
	}
	processed := matching.ProcessPokemonWebhook(prior, rarityGroup, pvpCfg)
	base, _ := ps.enricher.Pokemon(prior, processed, enrichment.TileModeSkip)
	if base == nil {
		return nil
	}
	if ps.enricher.GameData == nil || ps.enricher.Translations == nil {
		// Without translations the per-language layer is empty, but the base
		// view still carries identity / battle-stats / icon URLs etc. Return
		// it under every distinct language so templates don't see nil.
		out := make(map[string]map[string]any)
		for _, lang := range distinctLanguages(users, ps.cfg.General.Locale) {
			out[lang] = base
		}
		return out
	}
	out := make(map[string]map[string]any)
	for _, lang := range distinctLanguages(users, ps.cfg.General.Locale) {
		perLang := ps.enricher.PokemonTranslate(base, prior, lang)
		merged := make(map[string]any, len(base)+len(perLang))
		for k, v := range base {
			merged[k] = v
		}
		for k, v := range perLang {
			merged[k] = v
		}
		out[lang] = merged
	}
	return out
}

// partitionByPriorMessage splits matched users into two slices: those that
// already have a tracked message under (encounterID, user.ID) in the message
// tracker, and those that don't. Used by the change-event dispatch to decide
// which template to apply per-user.
//
// The tracker may be nil — in that case all users are treated as having no
// prior (i.e. they all get the fresh-message path).
func partitionByPriorMessage(matched []webhook.MatchedUser, encounterID string, tr *delivery.MessageTracker) (withPrior, withoutPrior []webhook.MatchedUser) {
	if tr == nil {
		return nil, append([]webhook.MatchedUser(nil), matched...)
	}
	for _, m := range matched {
		if tr.LookupReply(encounterID, m.ID) != "" {
			withPrior = append(withPrior, m)
		} else {
			withoutPrior = append(withoutPrior, m)
		}
	}
	return
}

// translatorFor returns an i18n translator for the given language. Empty
// language falls back to the configured default locale. Returns nil if
// translations are not configured (e.g. in tests with a partial enricher).
func (ps *ProcessorService) translatorFor(language string) *i18n.Translator {
	if ps.enricher == nil || ps.enricher.Translations == nil {
		return nil
	}
	if language == "" && ps.cfg != nil {
		language = ps.cfg.General.Locale
	}
	return ps.enricher.Translations.For(language)
}
