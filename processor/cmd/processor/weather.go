package main

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessWeather(raw json.RawMessage) error {
	if ps.cfg.General.DisableWeather {
		return nil
	}

	var weather webhook.WeatherWebhook
	if err := json.Unmarshal(raw, &weather); err != nil {
		log.Errorf("Failed to parse weather webhook: %s", err)
		return err
	}

	cellID := weather.S2CellID.String()
	if cellID == "" || cellID == "0" {
		cellID = tracker.GetWeatherCellID(weather.Latitude, weather.Longitude)
	}

	ps.weather.UpdateFromWebhook(cellID, weather.GameplayCondition, weather.Updated, weather.Latitude, weather.Longitude, weather.Polygon)
	return nil
}

// consumeWeatherChanges reads weather change events and processes them for
// matching and delivery.
func (ps *ProcessorService) consumeWeatherChanges() {
	for change := range ps.weather.Changes() {
		l := log.WithField("ref", change.S2CellID)

		caringUsers := ps.weatherCares.GetCaringUsers(change.S2CellID)
		if len(caringUsers) == 0 {
			l.Debugf("Weather changed to %d (from %d, source=%s) but no users care",
				change.GameplayCondition, change.OldGameplayCondition, change.Source)
			continue
		}

		l.Debugf("Weather changed to %d (from %d, source=%s) and %d users care, checking for affected pokemon",
			change.GameplayCondition, change.OldGameplayCondition, change.Source, len(caringUsers))

		// Build matched users, skipping those with no affected pokemon
		var matched []webhook.MatchedUser
		minAlert := int64(ps.cfg.General.AlertMinimumTime)
		now := time.Now().Unix()
		for _, u := range caringUsers {
			// Squash clean weather alerts whose TTH (CaresUntil) is too short
			// to be usefully tracked. Without this, the alert ships with
			// Clean=1 but a TTL of a few seconds — the delivery queue either
			// drops it on the "TTL already expired" path or the user sees a
			// message that vanishes almost immediately.
			if u.Clean > 0 {
				remaining := u.CaresUntil - now
				if u.CaresUntil == 0 || remaining < minAlert {
					l.Debugf("Weather alert suppressed for %s (%s) — TTH %ds below alert_minimum_time %ds",
						u.Name, u.ID, remaining, minAlert)
					continue
				}
			}
			mu := webhook.MatchedUser{
				ID:         u.ID,
				Name:       u.Name,
				Type:       u.Type,
				Language:   u.Language,
				Template:   u.Template,
				Clean:      u.Clean,
				Ping:       u.Ping,
				CaresUntil: u.CaresUntil,
			}

			// Attach active pokemon affected by this weather change. When
			// show_altered_pokemon is enabled (and the tracker is therefore
			// non-nil), the alert is also gated on at least one of the
			// user's tracked pokemon actually flipping boost status —
			// matches the original PoracleJS behaviour. Weather transitions
			// where every tracked pokemon either keeps or stays-without
			// boost (e.g. a Normal/Flying pokemon under Windy → Partly
			// Cloudy: boosted under both) produce no useful information.
			//
			// When show_altered_pokemon is off, we have no active-pokemon
			// data to filter on, so the cell-cares set is the only signal
			// and the alert fires for everyone who registered weather care
			// for this cell.
			if ps.activePokemon != nil {
				affected := ps.activePokemon.GetAffectedPokemon(
					change.S2CellID, u.ID,
					change.OldGameplayCondition, change.GameplayCondition,
					ps.cfg.Weather.ShowAlteredPokemonMaxCount,
				)
				if len(affected) == 0 {
					l.Debugf("Weather alert skipped for %s (%s) — no tracked pokemon affected by %d→%d transition",
						u.Name, u.ID, change.OldGameplayCondition, change.GameplayCondition)
					continue
				}
				entries := make([]webhook.ActivePokemonEntry, len(affected))
				for j, ap := range affected {
					entries[j] = webhook.ActivePokemonEntry{
						PokemonID:     ap.PokemonID,
						Form:          ap.Form,
						IV:            ap.IV,
						CP:            ap.CP,
						Latitude:      ap.Latitude,
						Longitude:     ap.Longitude,
						DisappearTime: ap.DisappearTime,
					}
				}
				mu.ActivePokemons = entries
			}

			matched = append(matched, mu)
		}

		matched = ps.filterBlocked(matched)

		if len(matched) == 0 {
			l.Debugf("Weather changed to %d (from %d, source=%s) but all caring users were filtered (rate limit / blocked alerts / clean TTH)",
				change.GameplayCondition, change.OldGameplayCondition, change.Source)
			continue
		}

		// Build matched areas from cell center
		st := ps.stateMgr.Get()
		areas := st.Geofence.PointInAreas(change.Latitude, change.Longitude)
		matchedAreas := make([]webhook.MatchedArea, len(areas))
		for i, a := range areas {
			matchedAreas[i] = webhook.MatchedArea{
				Name:             a.Name,
				DisplayInMatches: a.DisplayInMatches,
				Group:            a.Group,
			}
		}

		l.Infof("Weather changed %s -> %s (source=%s) areas(%s) and %d users care",
			ps.weatherName(change.OldGameplayCondition), ps.weatherName(change.GameplayCondition),
			change.Source, areaNames(matchedAreas), len(matched))

		// Build weather change message
		msg, _ := json.Marshal(change)
		mode := ps.tileMode("weatherchange", matched, change.S2CellID)
		baseEnrichment, baseTilePending := ps.enricher.Weather(change.Latitude, change.Longitude, change.GameplayCondition, change.Coords, ps.cfg.Weather.ShowAlteredPokemonStaticMap, mode, change.S2CellID)

		// Per-user: each gets their own render job with per-language enrichment and tile
		if ps.renderCh == nil {
			continue
		}

		webhookFields := parseWebhookFields(msg)

		ps.dispatchWeatherChange(weatherChangeDispatchInput{
			s2CellID:        change.S2CellID,
			oldCondition:    change.OldGameplayCondition,
			newCondition:    change.GameplayCondition,
			baseEnrichment:  baseEnrichment,
			baseTilePending: baseTilePending,
			matched:         matched,
			matchedAreas:    matchedAreas,
			webhookFields:   webhookFields,
			now:             now,
			minAlert:        minAlert,
		})
	}
}

// weatherChangeDispatchInput carries the shared, per-change context into
// dispatchWeatherChange. baseEnrichment/baseTilePending are computed once per
// weather change and shared across every matched user's RenderJob.
type weatherChangeDispatchInput struct {
	s2CellID        string
	oldCondition    int
	newCondition    int
	baseEnrichment  map[string]any
	baseTilePending *staticmap.TilePending
	matched         []webhook.MatchedUser
	matchedAreas    []webhook.MatchedArea
	webhookFields   map[string]any
	now             int64
	minAlert        int64
}

// dispatchWeatherChange fans a weather change out to one RenderJob per matched
// user, mirroring dispatchPokemonAlert's tile handling.
//
// When the base weather tile is SHARED (show_altered_pokemon_static_map off, so
// every user renders the same cell tile), all jobs share a SINGLE tileGate: the
// tile resolves once and its URL is written once into the shared baseEnrichment
// map, with chan-close happens-before making it visible to every render worker.
// Wrapping the shared *staticmap.TilePending in a gate PER USER was a bug — the
// pending's Result/ResultImg channels deliver to exactly one receiver, so only
// one message got the real tile URL and the rest blocked to their deadline and
// applied the fallback image; and the per-user gate goroutines raced writing
// baseEnrichment.
//
// Per-user tiles (the per-pokemon plot config, show_altered_pokemon_static_map
// on) keep their own gate — each user's pending is distinct, so there is no
// sharing to coordinate.
//
// Clean users carry their per-user clean-deletion TTH via RenderJob.
// OverrideCleanTTH (aligned to their last-tracked pokemon's despawn) instead of
// a pre-resolution COPY of baseEnrichment. The copy snapshotted the map before
// the async tile resolved, so a clean user's message lost the shared tile;
// sharing baseEnrichment + OverrideCleanTTH keeps both the resolved tile and the
// correct auto-delete timing. The weatherchange template renders no tth field,
// so this is a behaviour-preserving swap for the clean-deletion timing only.
func (ps *ProcessorService) dispatchWeatherChange(in weatherChangeDispatchInput) {
	if ps.renderCh == nil {
		return
	}
	l := log.WithField("ref", in.s2CellID)

	// Build every user's RenderJob FIRST, THEN spawn the shared base-tile gate.
	// WeatherTranslate READS in.baseEnrichment (lat/lon); the shared gate's
	// goroutine WRITES in.baseEnrichment via TilePending.Apply. Spawning that
	// gate before the loop finished would race the writes against these reads
	// (concurrent map read+write → runtime crash, reachable under render-queue
	// pressure where Apply fires synchronously). Deferring the single shared
	// gate until every read is done makes the reads happen-before the write via
	// goroutine-spawn ordering — mirroring dispatchPokemonAlert, whose loop
	// never touches the gated map. Per-user tiles get their own gate in the loop
	// (they write a per-user map, so there is no shared-map hazard).
	var jobs []RenderJob
	for _, user := range in.matched {
		lang := user.Language
		if lang == "" {
			lang = ps.cfg.General.Locale
			if lang == "" {
				lang = "en"
			}
		}

		var perLang map[string]map[string]any
		var userTilePending *staticmap.TilePending
		if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
			userMode := ps.tileMode("weatherchange", []webhook.MatchedUser{user}, in.s2CellID)
			langEnrichment, utp := ps.enricher.WeatherTranslate(
				in.baseEnrichment,
				in.oldCondition,
				in.newCondition,
				user.ActivePokemons,
				lang,
				ps.cfg.Weather.ShowAlteredPokemonStaticMap,
				userMode,
				in.s2CellID,
			)
			userTilePending = utp
			perLang = map[string]map[string]any{lang: langEnrichment}
		}

		// Clean users auto-delete when their longest-lived shown pokemon
		// despawns (weatherAlertCleanUntil), carried per-job via
		// OverrideCleanTTH so the shared baseEnrichment map is untouched.
		var overrideCleanTTH int64
		if user.Clean > 0 {
			cleanUntil := weatherAlertCleanUntil(user)
			if cleanUntil > 0 {
				// Re-check the min-alert threshold with the alert-accurate TTH
				// (the pre-filter used CaresUntil, which can over-estimate when
				// only short-lived active pokemon remain).
				if remaining := cleanUntil - in.now; remaining < in.minAlert {
					l.Debugf("Weather alert suppressed for %s (%s) — alert TTH %ds below alert_minimum_time %ds",
						user.Name, user.ID, remaining, in.minAlert)
					continue
				}
				overrideCleanTTH = cleanUntil
			}
		}

		job := RenderJob{
			AlertType:         "weatherchange",
			TemplateType:      "weatherchange",
			Enrichment:        in.baseEnrichment,
			PerLangEnrichment: perLang,
			WebhookFields:     in.webhookFields,
			MatchedUsers:      []webhook.MatchedUser{user},
			MatchedAreas:      in.matchedAreas,
			OverrideCleanTTH:  overrideCleanTTH,
			LogReference:      in.s2CellID,
		}
		// Per-user tile → its own gate now (writes a per-user map, no shared
		// hazard). Shared-tile users are left with a nil gate and get the single
		// shared baseGate attached below, after all baseEnrichment reads finish.
		if userTilePending != nil {
			job.TileGate = ps.newTileGate(userTilePending)
		}
		jobs = append(jobs, job)
	}

	// All in.baseEnrichment reads are complete — now spawn the single shared
	// gate (nil when there is no shared base tile) and enqueue.
	baseGate := ps.newTileGate(in.baseTilePending)
	for i := range jobs {
		if jobs[i].TileGate == nil {
			jobs[i].TileGate = baseGate
		}
		ps.renderCh <- jobs[i]
	}
}

// weatherAlertCleanUntil returns the unix timestamp at which a clean
// weather alert should auto-delete. Prefers max(ActivePokemons.DisappearTime)
// — those are the pokemon the alert actually mentions, and aligning TTH
// with them avoids orphan weather messages outliving the alerted pokemon.
// Falls back to CaresUntil (the user's cell-wide care window) when the
// active-pokemon tracker is off, since per-pokemon data isn't available.
func weatherAlertCleanUntil(user webhook.MatchedUser) int64 {
	if len(user.ActivePokemons) == 0 {
		return user.CaresUntil
	}
	var maxDisappear int64
	for _, ap := range user.ActivePokemons {
		if ap.DisappearTime > maxDisappear {
			maxDisappear = ap.DisappearTime
		}
	}
	return maxDisappear
}
