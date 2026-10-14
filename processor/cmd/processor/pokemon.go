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
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
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
		if ps.recentActivity != nil {
			if pokemon.Costume > 0 {
				ps.recentActivity.RecordCostume(pokemon.PokemonID, pokemon.Costume)
			}
			if pokemon.Form > 0 {
				ps.recentActivity.RecordForm(pokemon.PokemonID, pokemon.Form)
			}
		}

		// Duplicate check
		verified := pokemon.Verified || pokemon.DisappearTimeVerified
		if ps.duplicates.CheckPokemon(pokemon.EncounterID, pokemon.PokemonID, verified, pokemon.CP, pokemon.DisappearTime) {
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
		encounterState := tracker.EncounterStateFromPokemon(&pokemon)

		// Get rarity group
		rarityGroup := ps.stats.GetRarityGroup(pokemon.PokemonID)

		// Process pokemon into matching format
		processed := matching.ProcessPokemonWebhook(&pokemon, rarityGroup, ps.pvpCfg)

		// Match (against the new state — `processed` was built from the
		// just-parsed webhook, so this set is correct for both initial
		// sightings and change events).
		snap := ps.stateMgr.Get()
		if snap != nil {
			processed.Personal = personalshiny.NewChecker(ps.shinyRates.Evaluate(&pokemon), snap.Humans)
		}
		matched, matchedAreas := ps.pokemonMatcher.Match(processed, snap)
		matched = ps.filterBlocked(matched)
		matched = ps.filterValidation("pokemon", raw, matchedAreas, matched)
		matched = ps.filterMuted(matched, matchedAreas, mute.Event{PokemonID: pokemon.PokemonID})
		applyDefaultEdit(matched, ps.cfg.Tracking, "pokemon")

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
				if u := ps.rebuildMatchedUserForChange(targetID, prior.Clean, prior.Template); u != nil {
					priorOnlyUsers = append(priorOnlyUsers, *u)
				}
			}
		}

		enrichStart := time.Now()
		allRecipients := slices.Concat(matched, priorOnlyUsers)
		// Users the pokemon is personally shiny for get a static map of their
		// own with the shiny sprite, the base tile is only for the others.
		shinyUsers, otherMatched := splitPersonalShiny(matched, processed.Personal)
		mode := ps.tileMode("monster", slices.Concat(otherMatched, priorOnlyUsers), pokemon.EncounterID)
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
		// Like per-user PVP, prior-only recipients (priorOnlyUsers) deliberately get no personal fields.
		perUser = enrichment.AnnotatePersonal(perUser, matched, processed.Personal)
		ps.enricher.PersonalShinyIcons(perUser, &pokemon)
		var shiny *shinyAlert
		if len(shinyUsers) > 0 {
			enrichment, pending := ps.enricher.PersonalShinyTile(baseEnrichment, &pokemon,
				ps.tileMode("monster", shinyUsers, pokemon.EncounterID))
			shiny = &shinyAlert{users: shinyUsers, enrichment: enrichment, tilePending: pending}
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
				shiny:          shiny,
				isEncountered:  processed.Encountered,
			})
			return
		}

		// ReplyKey is set so enabling the flag later threads onto this send.
		editKey, editMaxAge := ps.pokemonEditFields(pokemon.EncounterID)
		job := RenderJob{
			AlertType:         "pokemon",
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
			EditKey:           editKey,
			EditMaxAge:        editMaxAge,
		}
		if shiny == nil {
			ps.renderCh <- job
			return
		}
		if len(otherMatched) > 0 {
			job.MatchedUsers = otherMatched
			ps.renderCh <- job
		}
		ps.renderCh <- shiny.job(ps, job, shinyUsers)
	}()
	return nil
}

// shinyAlert is the part of a pokemon alert for the users the pokemon is
// personally shiny for: they share an enrichment copy whose static map shows
// the shiny sprite, resolved by a tile gate of its own.
type shinyAlert struct {
	users       []webhook.MatchedUser
	enrichment  map[string]any
	tilePending *staticmap.TilePending
	gate        *tileGate
}

// job returns template for users with the shiny enrichment and tile gate; the
// gate is started on first use and shared by every job of the alert.
func (s *shinyAlert) job(ps *ProcessorService, template RenderJob, users []webhook.MatchedUser) RenderJob {
	if s.gate == nil {
		s.gate = ps.newTileGate(s.tilePending)
	}
	template.MatchedUsers = users
	template.Enrichment = s.enrichment
	template.TileGate = s.gate
	return template
}

// splitPersonalShiny separates the matched users the pokemon is personally
// shiny for (see enrichment.AnnotatePersonal) from the others.
func splitPersonalShiny(matched []webhook.MatchedUser, c *personalshiny.Checker) (shiny, others []webhook.MatchedUser) {
	if !c.Evaluated() {
		return nil, matched
	}
	for _, u := range matched {
		if res, linked := c.Result(u.ID); linked && len(res.Shiny) > 0 {
			shiny = append(shiny, u)
		} else {
			others = append(others, u)
		}
	}
	return shiny, others
}

// withoutUsers returns users minus the ones in remove.
func withoutUsers(users, remove []webhook.MatchedUser) []webhook.MatchedUser {
	skip := make(map[string]struct{}, len(remove))
	for _, u := range remove {
		skip[u.ID] = struct{}{}
	}
	var rest []webhook.MatchedUser
	for _, u := range users {
		if _, ok := skip[u.ID]; !ok {
			rest = append(rest, u)
		}
	}
	return rest
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
	shiny          *shinyAlert // nil when the pokemon is nobody's personal shiny
	isEncountered  bool
}

// pokemonEditFields returns the base edit key and edit window for a pokemon
// RenderJob. The renderer appends ":<userID>" to the key only for rules with
// the edit bit. With [tracking] pokemon_edit off, both are zero so no job
// edits and follow-ups thread as replies.
func (ps *ProcessorService) pokemonEditFields(encounterID string) (string, time.Duration) {
	if !ps.cfg.Tracking.PokemonEdit {
		return "", 0
	}
	return "pokemon:" + encounterID, ps.cfg.Tracking.PokemonEditMaxAge()
}

// dispatchPokemonAlert emits one RenderJob per language per bucket:
// matched users get `monster`; prior-only users get `monsterChanged`
// with a per-language {{original.X}} view. ReplyKey is set on every
// job so the delivery queue attaches reply metadata when a prior
// exists. Fresh sends seed the reply-index for future changes. A
// single tileGate ensures the shared enrichment map is written once
// before any render worker reads it.
func (ps *ProcessorService) dispatchPokemonAlert(in pokemonDispatchInput) {
	editKey, editMaxAge := ps.pokemonEditFields(in.encounterID)
	// Every job below starts from this template and only sets what differs
	// (users, tile gate, change fields, the personal-shiny enrichment), so a
	// field added here reaches every bucket.
	template := RenderJob{
		AlertType:         "pokemon",
		IsPokemon:         true,
		IsEncountered:     in.isEncountered,
		Enrichment:        in.enrichment,
		PerLangEnrichment: in.perLang,
		PerUserEnrichment: in.perUser,
		WebhookFields:     in.webhookFields,
		MatchedAreas:      in.matchedAreas,
		LogReference:      in.encounterID,
		ReplyKey:          in.encounterID,
		EditKey:           editKey,
		EditMaxAge:        editMaxAge,
	}

	if ps.dispatcher == nil {
		if len(in.matched) == 0 {
			return
		}
		job := template
		job.MatchedUsers = in.matched
		job.TileGate = ps.newTileGate(in.tilePending)
		if in.shiny == nil {
			ps.renderCh <- job
			return
		}
		if others := withoutUsers(in.matched, in.shiny.users); len(others) > 0 {
			job.MatchedUsers = others
			ps.renderCh <- job
		}
		ps.renderCh <- in.shiny.job(ps, job, in.shiny.users)
		return
	}

	template.TileGate = ps.newTileGate(in.tilePending)

	// Bucket 1: matched users → `monster`, grouped by language. Users the
	// pokemon is personally shiny for go out separately with their own map.
	matched := in.matched
	if in.shiny != nil {
		matched = withoutUsers(matched, in.shiny.users)
		for _, users := range groupByLanguage(in.shiny.users, ps.cfg.General.Locale) {
			ps.renderCh <- in.shiny.job(ps, template, users)
		}
	}
	for _, users := range groupByLanguage(matched, ps.cfg.General.Locale) {
		job := template
		job.MatchedUsers = users
		ps.renderCh <- job
	}

	// Bucket 2: prior-only users → `monsterChanged`, grouped by language.
	// ChangeEncountered (CP 0 → >0) is filtered out — IV reveal isn't a
	// change worth notifying about for users whose filter excluded the
	// new state; the matched-still-matches path already covers users
	// whose rule was IV-insensitive.
	if len(in.priorOnlyUsers) > 0 && in.change != nil && in.change.Type != tracker.ChangeEncountered {
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
			job := template
			job.IsChange = true
			job.PerLangEnrichment = ps.perLangWithChangeFields(in.perLang, lang, bucket)
			job.MatchedUsers = users
			job.OriginalView = orig
			job.ChangeType = in.change.Type.String()
			ps.renderCh <- job
		}
	}
}

// changeTypeBucket collapses the tracker.ChangeType enum into the
// two template-facing values:
//   - "stats" for ChangeWeatherBoost (post-encounter CP shift) and
//     ChangeStats (raw IV re-report from scanner). Both are "same
//     pokemon, different effective stats" from the user's view.
//   - "species" for ChangeSpecies/Form/Gender — identity changes
//     (gender alone effectively never fires).
//
// ChangeEncountered is filtered upstream and never reaches here.
func changeTypeBucket(t tracker.ChangeType) string {
	switch t {
	case tracker.ChangeWeatherBoost, tracker.ChangeStats:
		return "stats"
	}
	return "species"
}

// changeTypeText resolves the translated changeTypeText string for a given
// bucket ("stats"/"species") in the given language. Shared by the live
// per-language change-notification path (perLangWithChangeFields) and the
// monsterChanged derived test/editor-preview path (enrichMonsterChanged in
// enrich.go), so the "change_type_text_" + bucket translation-key
// convention lives in exactly one place.
func (ps *ProcessorService) changeTypeText(lang, bucket string) string {
	return ps.translatorFor(lang).T("change_type_text_" + bucket)
}

// perLangWithChangeFields returns a per-language enrichment map that
// adds `changeType` and `changeTypeText` to the slot for `lang`,
// leaving other languages shared by reference. Used only for
// monsterChanged RenderJobs so the fields don't leak into `monster`
// jobs that share the upstream perLang map.
func (ps *ProcessorService) perLangWithChangeFields(perLang map[string]map[string]any, lang, bucket string) map[string]map[string]any {
	text := ps.changeTypeText(lang, bucket)

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
// Clean and Template are inherited from the prior tracked message so the
// monsterChanged reply follows the same auto-delete behaviour AND uses the
// same DTS template name as the original alert (rather than falling back to
// the default). Ping / Distance stay at zero values — the T1 tracking rule
// isn't known here. Returns nil for unknown or disabled humans; their
// reply-index entry expires on its own.
func (ps *ProcessorService) rebuildMatchedUserForChange(targetID string, clean int, template string) *webhook.MatchedUser {
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
		Template: template,
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
