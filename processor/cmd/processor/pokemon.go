package main

import (
	"encoding/json"
	"maps"
	"slices"
	"time"

	log "github.com/sirupsen/logrus"

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

		// Track when someone matches the new state OR an entry already
		// exists (post-match changes still need diffing so prior
		// recipients can be notified). Store a PVP-stripped copy —
		// rankings are heavy and unused by {{original.X}}.
		trackingEnabled := ps.cfg.Tracking.PokemonChangeTracking
		shouldTrack := trackingEnabled && (len(matched) > 0 || ps.encounters.Has(pokemon.EncounterID))
		var change *tracker.EncounterChange
		if shouldTrack {
			stored := pokemon
			stored.PVP = nil
			stored.PVPRankingsGreatLeague = nil
			stored.PVPRankingsUltraLeague = nil
			stored.PVPRankingsLittleLeague = nil
			_, change = ps.encounters.Track(pokemon.EncounterID, encounterState, &stored)
		}

		if len(matched) == 0 && change == nil {
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

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("pokemon").Inc()
			metrics.MatchedUsers.WithLabelValues("pokemon").Add(float64(len(matched)))
			metrics.IntervalMatched.Add(1)
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

		// Reconstruct prior-only recipients before enrichment so their
		// languages are included in the perLang map.
		var priorOnlyUsers []webhook.MatchedUser
		if change != nil && trackingEnabled && ps.dispatcher != nil {
			matchedIDs := make(map[string]struct{}, len(matched))
			for _, u := range matched {
				matchedIDs[u.ID] = struct{}{}
			}
			tracker := ps.dispatcher.MessageTracker()
			for _, targetID := range tracker.LookupReplyTargets(pokemon.EncounterID) {
				if _, ok := matchedIDs[targetID]; ok {
					continue
				}
				prior := tracker.LookupReplyMessage(pokemon.EncounterID, targetID)
				if prior == nil {
					// Prior already TTL-evicted; original is gone, nothing to reply to.
					continue
				}
				if u := ps.rebuildMatchedUserForChange(targetID, prior.Clean); u != nil {
					priorOnlyUsers = append(priorOnlyUsers, *u)
				}
			}
		}

		enrichStart := time.Now()
		allRecipients := slices.Concat(matched, priorOnlyUsers)
		mode := ps.tileMode("monster", allRecipients)
		baseEnrichment, tilePending := ps.enricher.Pokemon(&pokemon, processed, mode)

		var perLang map[string]map[string]any
		if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
			perLang = make(map[string]map[string]any)
			for _, lang := range distinctLanguages(allRecipients, ps.cfg.General.Locale) {
				perLang[lang] = ps.enricher.PokemonTranslate(baseEnrichment, &pokemon, lang)
			}
		}

		// PerUser PVP display only applies to currently-matched users;
		// prior-only users (no longer matching) have no rule-specific
		// rank to render, and monsterChanged doesn't show PVP anyway.
		var perUser map[string]map[string]any
		if ps.enricher.PVPDisplay != nil && perLang != nil && len(matched) > 0 {
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
		}

		if trackingEnabled {
			ps.dispatchPokemonAlert(pokemonDispatchInput{
				encounterID:    pokemon.EncounterID,
				change:         change,
				matched:        matched,
				priorOnlyUsers: priorOnlyUsers,
				matchedAreas:   matchedAreas,
				enrichment:     baseEnrichment,
				perLang:        perLang,
				perUser:        perUser,
				webhookFields:  webhookFields,
				tilePending:    tilePending,
				isEncountered:  processed.Encountered,
			})
			return
		}

		// ReplyKey is set so enabling the flag later threads onto this send.
		ps.renderCh <- RenderJob{
			IsPokemon:         true,
			IsEncountered:     processed.Encountered,
			Enrichment:        baseEnrichment,
			PerLangEnrichment: perLang,
			PerUserEnrichment: perUser,
			WebhookFields:     webhookFields,
			MatchedUsers:      matched,
			MatchedAreas:      matchedAreas,
			TileGate:          ps.newTileGate(tilePending),
			LogReference:      pokemon.EncounterID,
			ReplyKey:          pokemon.EncounterID,
		}
	}()
	return nil
}

// pokemonDispatchInput bundles inputs to dispatchPokemonAlert. All
// fields describe the new state; priorOnlyUsers carries recipients
// reconstructed from the reply index who no longer match.
type pokemonDispatchInput struct {
	encounterID    string
	change         *tracker.EncounterChange
	matched        []webhook.MatchedUser
	priorOnlyUsers []webhook.MatchedUser
	matchedAreas   []webhook.MatchedArea
	enrichment     map[string]any
	perLang        map[string]map[string]any
	perUser        map[string]map[string]any
	webhookFields  map[string]any
	tilePending    *staticmap.TilePending
	isEncountered  bool
}

// dispatchPokemonAlert emits one RenderJob per language per bucket:
// matched users get `monster`; prior-only users get `monsterChanged`
// with a per-language {{original.X}} view. ReplyKey is set on every
// job so the delivery queue attaches reply metadata when a prior
// exists. Fresh sends seed the reply-index for future changes. A
// single tileGate ensures the shared enrichment map is written once
// before any render worker reads it.
func (ps *ProcessorService) dispatchPokemonAlert(in pokemonDispatchInput) {
	if ps.dispatcher == nil {
		if len(in.matched) == 0 {
			return
		}
		ps.renderCh <- RenderJob{
			IsPokemon:         true,
			IsEncountered:     in.isEncountered,
			Enrichment:        in.enrichment,
			PerLangEnrichment: in.perLang,
			PerUserEnrichment: in.perUser,
			WebhookFields:     in.webhookFields,
			MatchedUsers:      in.matched,
			MatchedAreas:      in.matchedAreas,
			TileGate:          ps.newTileGate(in.tilePending),
			LogReference:      in.encounterID,
			ReplyKey:          in.encounterID,
		}
		return
	}

	gate := ps.newTileGate(in.tilePending)

	// Bucket 1: matched users → `monster`, grouped by language.
	if len(in.matched) > 0 {
		for _, users := range groupByLanguage(in.matched, ps.cfg.General.Locale) {
			ps.renderCh <- RenderJob{
				IsPokemon:         true,
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
			}
		}
	}

	// Bucket 2: prior-only users → `monsterChanged`, grouped by
	// language. ChangeEncountered fires too: the prior was a
	// monsterNoIv "this might be one you care about" alert and we
	// owe the user a follow-up when the IV/CP/level/etc. reveal
	// rejects their filter, otherwise the optimistic T1 alert is
	// left dangling.
	if len(in.priorOnlyUsers) > 0 && in.change != nil {
		bucket := changeTypeBucket(in.change.Type)

		var perLangOriginal map[string]map[string]any
		var fallbackOriginal map[string]any
		if in.change.OldWebhook != nil {
			perLangOriginal = ps.buildPerLanguageOriginal(in.change.OldWebhook, in.priorOnlyUsers)
		}
		if perLangOriginal == nil {
			fallbackOriginal = dts.BuildOriginalView(in.change.Old, ps.enricher.GameData, ps.translatorFor(""))
		}

		byLang := groupByLanguage(in.priorOnlyUsers, ps.cfg.General.Locale)
		for lang, users := range byLang {
			orig := fallbackOriginal
			if perLangOriginal != nil {
				orig = perLangOriginal[lang]
			}
			ps.renderCh <- RenderJob{
				IsPokemon:         true,
				IsChange:          true,
				IsEncountered:     in.isEncountered,
				Enrichment:        in.enrichment,
				PerLangEnrichment: ps.perLangWithChangeFields(in.perLang, lang, bucket),
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
	}
}

// changeTypeBucket collapses the 5-value tracker.ChangeType enum into
// the two template-facing values:
//   - "stats" for ChangeWeatherBoost (post-encounter CP shift) and
//     ChangeEncountered (IV/CP reveal that rejected the user's filter).
//     Both are "same pokemon, different effective stats".
//   - "species" for ChangeSpecies/Form/Gender — identity changes
//     (gender alone effectively never fires).
func changeTypeBucket(t tracker.ChangeType) string {
	switch t {
	case tracker.ChangeWeatherBoost, tracker.ChangeEncountered:
		return "stats"
	}
	return "species"
}

// perLangWithChangeFields returns a per-language enrichment map that
// adds `changeType` and `changeTypeText` to the slot for `lang`,
// leaving other languages shared by reference. Used only for
// monsterChanged RenderJobs so the fields don't leak into `monster`
// jobs that share the upstream perLang map.
func (ps *ProcessorService) perLangWithChangeFields(perLang map[string]map[string]any, lang, bucket string) map[string]map[string]any {
	text := ps.translatorFor(lang).T("change_type_text_" + bucket)

	if perLang == nil {
		return map[string]map[string]any{
			lang: {"changeType": bucket, "changeTypeText": text},
		}
	}

	clone := maps.Clone(perLang[lang])
	if clone == nil {
		clone = make(map[string]any, 2)
	}
	clone["changeType"] = bucket
	clone["changeTypeText"] = text

	out := make(map[string]map[string]any, len(perLang))
	maps.Copy(out, perLang)
	out[lang] = clone
	return out
}

// rebuildMatchedUserForChange synthesises a MatchedUser for a target
// that had a prior alert for this encounter but no longer matches.
// Clean is inherited from the prior tracked message so the
// monsterChanged reply follows the same auto-delete behaviour as
// the original. Template / Ping / Distance stay at zero values —
// the T1 tracking rule isn't known here. Returns nil for unknown
// or disabled humans; their reply-index entry expires on its own.
func (ps *ProcessorService) rebuildMatchedUserForChange(targetID string, clean int) *webhook.MatchedUser {
	if ps.humans == nil {
		return nil
	}
	human, err := ps.humans.GetLite(targetID)
	if err != nil || human == nil {
		return nil
	}
	if !human.Enabled || human.AdminDisable {
		return nil
	}
	return &webhook.MatchedUser{
		ID:       human.ID,
		Type:     human.Type,
		Name:     human.Name,
		Language: effectiveLanguage(webhook.MatchedUser{Language: human.Language}, ps.cfg.General.Locale),
		Clean:    clean,
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
		maps.Copy(merged, base)
		maps.Copy(merged, perLang)
		out[lang] = merged
	}
	return out
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
