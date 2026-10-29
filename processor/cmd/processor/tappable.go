package main

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// ProcessTappable handles Golbat "tappable" webhooks (reward tappables; Golbat
// sends one per new or changed reward). Tappables never support edit, so
// there is no applyDefaultEdit and no EditKey.
func (ps *ProcessorService) ProcessTappable(raw json.RawMessage) error {
	if ps.cfg.General.DisableTappable {
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
			metrics.WebhookProcessingDuration.WithLabelValues(metrics.TypeTappable).Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

		var tw webhook.TappableWebhook
		if err := json.Unmarshal(raw, &tw); err != nil {
			log.Errorf("Failed to parse tappable webhook: %s", err)
			return
		}

		l := log.WithField("ref", tw.EncounterID)

		// disappear_time 0 means unknown; a known time already past is stale.
		now := time.Now().Unix()
		if tw.DisappearTime > 0 && tw.DisappearTime <= now {
			l.Debug("Tappable already expired, ignoring")
			return
		}
		// alert_minimum_time applies only to a verified expiry, and is checked
		// here so no matching or enrichment is spent on a dropped alert. An
		// unverified one is Golbat's guess (a fort tappable is tap+120s, which
		// the default 120s minimum would always drop), so the render skips the
		// check for it too (SkipMinAlertTime).
		if minSec := int64(ps.cfg.General.AlertMinimumTime); tw.DisappearTimeVerified &&
			tw.DisappearTime > 0 && minSec > 0 && tw.DisappearTime-now < minSec {
			l.Debugf("Tappable expires in %ds, below alert_minimum_time %ds, ignoring", tw.DisappearTime-now, minSec)
			return
		}

		rewards := matching.ParseTappableRewards(&tw)

		if ps.duplicates.CheckTappable(tw.EncounterID, matching.TappableRewardSignature(rewards), tw.DisappearTime) {
			l.Debug("Tappable duplicate, ignoring")
			metrics.DuplicatesSkipped.WithLabelValues(metrics.TypeTappable).Inc()
			return
		}

		data := &matching.TappableData{
			EncounterID: tw.EncounterID,
			Latitude:    tw.Latitude,
			Longitude:   tw.Longitude,
			Rewards:     rewards,
		}
		ev := mute.Event{PokemonID: tappableRewardPokemonID(rewards)}
		if tw.HasPokestop() {
			data.PokestopID = tw.PokestopID
			ev.PokestopID = tw.PokestopID
		}

		st := ps.stateMgr.Get()
		matched, matchedAreas := ps.tappableMatcher.Match(data, st)
		matched = ps.filterBlocked(matched)
		matched = ps.filterValidation("tappable", raw, matchedAreas, matched)
		matched = ps.filterMuted(matched, matchedAreas, ev)

		if len(matched) == 0 {
			l.Debugf("Tappable %s %s [%.3f,%.3f] and 0 humans cared",
				tw.Type, matching.TappableRewardSignature(rewards), tw.Latitude, tw.Longitude)
			return
		}

		metrics.MatchedEvents.WithLabelValues(metrics.TypeTappable).Inc()
		metrics.MatchedUsers.WithLabelValues(metrics.TypeTappable).Add(float64(len(matched)))
		metrics.IntervalMatched.Add(1)

		l.Infof("Tappable %s %s [%.3f,%.3f] areas(%s) and %d humans cared",
			tw.Type, matching.TappableRewardSignature(rewards), tw.Latitude, tw.Longitude,
			areaNames(matchedAreas), len(matched))

		mode := ps.tileMode("tappable", matched, tw.EncounterID)
		enrichmentData, tilePending := ps.enricher.Tappable(&tw, rewards, mode)

		var perLang map[string]map[string]any
		if ps.enricher.Translations != nil {
			perLang = make(map[string]map[string]any)
			for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
				perLang[lang] = ps.enricher.TappableTranslate(enrichmentData, &tw, rewards, lang)
			}
		}

		if ps.renderCh == nil {
			return
		}

		ps.renderCh <- RenderJob{
			AlertType:         "tappable",
			TemplateType:      "tappable",
			Enrichment:        enrichmentData,
			PerLangEnrichment: perLang,
			WebhookFields:     parseWebhookFields(raw),
			MatchedUsers:      matched,
			MatchedAreas:      matchedAreas,
			TileGate:          ps.newTileGate(tilePending),
			LogReference:      tw.EncounterID,
			SkipMinAlertTime:  !tw.DisappearTimeVerified,
		}
	}()
	return nil
}

// tappableRewardPokemonID returns the pokemon of the first reward that has
// one (candy / XL candy / mega energy), for pokemon-scope mutes.
func tappableRewardPokemonID(rewards []matching.TappableRewardData) int {
	for _, r := range rewards {
		if r.PokemonID > 0 {
			return r.PokemonID
		}
	}
	return 0
}
