package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// showcaseDisplayType is the incident display-type value PoracleNG uses for
// Showcases (util.json pokestopEvent 9). Showcases arrive on the pokéstop
// webhook without a display_type, so we synthesise it to reuse the incident
// classification, matcher, enrichment and template.
const showcaseDisplayType = 9

// showcaseActive reports whether a showcase is currently running. showcase_expiry
// is the contest end time and the only active/ended signal Golbat provides;
// fields linger stale after a contest ends (and ride along on unrelated pokéstop
// fires), so an expiry in the future is required to alert.
func showcaseActive(showcaseExpiry, now int64) bool {
	return showcaseExpiry > now
}

// showcaseRank1 is a minimal view of showcase_rankings for the dedup
// fingerprint — only the fields whose change makes Golbat re-fire the webhook.
type showcaseRank1 struct {
	TotalEntries   int `json:"total_entries"`
	ContestEntries []struct {
		Rank      int     `json:"rank"`
		PokemonID int     `json:"pokemon_id"`
		Score     float64 `json:"score"`
	} `json:"contest_entries"`
}

// showcaseFingerprint summarises the rank-1 leaderboard state. Golbat re-fires
// only on rank-1 movement, so (total_entries, rank-1 pokemon, rank-1 score)
// captures every distinct fire; last_update (which churns per scan) is
// deliberately excluded so a re-send of the same state dedups.
func showcaseFingerprint(rankings json.RawMessage) string {
	if len(rankings) == 0 {
		return "0:none"
	}
	var r showcaseRank1
	if err := json.Unmarshal(rankings, &r); err != nil {
		return "parse-error"
	}
	top := "none"
	for _, e := range r.ContestEntries {
		if e.Rank == 1 {
			top = fmt.Sprintf("%d@%g", e.PokemonID, e.Score)
			break
		}
	}
	return fmt.Sprintf("%d:%s", r.TotalEntries, top)
}

// ProcessShowcase handles the showcase slice of a pokéstop webhook. Showcases
// reuse the incident tracking type (grunt_type="showcase") and the incident
// template; this handler synthesises display_type=9 and feeds the invasion
// matcher/enrichment. Edit-mode (EditKey + OverrideCleanTTH) collapses the
// empty-start fire and each rank-1 update into one self-editing message.
func (ps *ProcessorService) ProcessShowcase(raw json.RawMessage) error {
	if ps.cfg.General.DisableShowcase {
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
			metrics.WebhookProcessingDuration.WithLabelValues("showcase").Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

		var sc webhook.ShowcaseWebhook
		if err := json.Unmarshal(raw, &sc); err != nil {
			log.Errorf("Failed to parse showcase webhook: %s", err)
			return
		}

		l := log.WithField("ref", sc.PokestopID)

		// Only alert on a currently-running contest. Golbat never clears
		// showcase fields on end, so a stale showcase rides along on lure /
		// power-up fires; drop those.
		if !showcaseActive(sc.ShowcaseExpiry, time.Now().Unix()) {
			l.Debugf("Skipping expired/absent showcase: expiry=%d", sc.ShowcaseExpiry)
			return
		}

		// Dedup on (stop, contest end, rank-1 fingerprint) so each leaderboard
		// change reaches the edit path but repeats of the same state don't.
		if ps.duplicates.CheckShowcase(sc.PokestopID, sc.ShowcaseExpiry, showcaseFingerprint(sc.ShowcaseRankings)) {
			l.Debug("Showcase duplicate, ignoring")
			metrics.DuplicatesSkipped.WithLabelValues("showcase").Inc()
			return
		}

		// Resolve to the incident event name ("showcase") the same way the
		// invasion path does for display_type >= 7.
		gruntType := matching.ResolveGruntTypeName(0, showcaseDisplayType, ps.enricher.GameData)

		data := &matching.InvasionData{
			PokestopID: sc.PokestopID,
			GruntType:  gruntType,
			Latitude:   sc.Latitude,
			Longitude:  sc.Longitude,
		}

		st := ps.stateMgr.Get()
		matched, matchedAreas := ps.invasionMatcher.Match(data, st)
		matched = ps.filterBlocked(matched)
		matched = ps.filterValidation("pokestop", raw, matchedAreas, matched)
		matched = ps.filterMuted(matched, matchedAreas, mute.Event{PokestopID: sc.PokestopID})

		if len(matched) == 0 {
			l.Debugf("Showcase at %s [%.3f,%.3f] and 0 humans cared", sc.Name, sc.Latitude, sc.Longitude)
			return
		}

		metrics.MatchedEvents.WithLabelValues("showcase").Inc()
		metrics.MatchedUsers.WithLabelValues("showcase").Add(float64(len(matched)))
		metrics.IntervalMatched.Add(1)

		l.Infof("Showcase at %s [%.3f,%.3f] areas(%s) and %d humans cared",
			sc.Name, sc.Latitude, sc.Longitude, areaNames(matchedAreas), len(matched))

		mode := ps.tileMode("showcase", matched, sc.PokestopID)
		baseEnrichment, tilePending := ps.enricher.Invasion(
			sc.Latitude, sc.Longitude, sc.ShowcaseExpiry, sc.PokestopID, sc.URL,
			0, showcaseDisplayType, 0, mode)
		// The showcase webhook carries the pokéstop name in `name`; Invasion()
		// takes no name arg, so surface it as pokestop_name for the alias.
		baseEnrichment["pokestop_name"] = sc.Name

		var perLang map[string]map[string]any
		if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
			perLang = make(map[string]map[string]any)
			for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
				m := ps.enricher.InvasionTranslate(
					baseEnrichment, sc.Latitude, sc.Longitude, 0, nil, sc.ShowcaseRankings, lang)
				maps.Copy(m, ps.enricher.ShowcaseFocusTranslate(sc.ShowcaseFocus, lang))
				perLang[lang] = m
			}
		}

		if ps.renderCh == nil {
			return
		}
		webhookFields := parseWebhookFields(raw)

		ps.renderCh <- RenderJob{
			// AlertType stays "incident" — showcases are tracked, rate-limited
			// and blocked as incidents. TemplateType is the specialised
			// "showcase" display model (leaderboard + focus), distinct from the
			// plain incident card used by Gold-Stop / Kecleon.
			AlertType:         "incident",
			TemplateType:      "showcase",
			Enrichment:        baseEnrichment,
			PerLangEnrichment: perLang,
			WebhookFields:     webhookFields,
			MatchedUsers:      matched,
			MatchedAreas:      matchedAreas,
			TileGate:          ps.newTileGate(tilePending),
			LogReference:      sc.PokestopID,
			// Edit-mode: every fire of one contest (empty start + each rank-1
			// update) shares this key, so a user with the edit bit gets the
			// leaderboard edited in place rather than re-sent.
			EditKey:          fmt.Sprintf("showcase:%s:%d", sc.PokestopID, sc.ShowcaseExpiry),
			OverrideCleanTTH: sc.ShowcaseExpiry,
		}
	}()
	return nil
}
