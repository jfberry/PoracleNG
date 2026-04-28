package main

import (
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func (ps *ProcessorService) ProcessRaid(raw json.RawMessage) error {
	if ps.cfg.General.DisableRaid {
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
			metrics.WebhookProcessingDuration.WithLabelValues("raid").Observe(time.Since(start).Seconds())
			metrics.WorkerPoolInUse.Dec()
			<-ps.workerPool
		}()
		defer ps.wg.Done()

		var raid webhook.RaidWebhook
		if err := json.Unmarshal(raw, &raid); err != nil {
			log.Errorf("Failed to parse raid webhook: %s", err)
			return
		}

		l := log.WithField("ref", raid.GymID)

		// Duplicate check — also tells us if this is the first notification for this raid
		rsvps := make([]tracker.RaidRSVP, len(raid.RSVPs))
		for i, r := range raid.RSVPs {
			rsvps[i] = tracker.RaidRSVP{
				Timeslot:   r.Timeslot,
				GoingCount: r.GoingCount,
				MaybeCount: r.MaybeCount,
			}
		}
		isDuplicate, isFirstNotification := ps.duplicates.CheckRaid(raid.GymID, raid.End, raid.PokemonID, rsvps)
		if isDuplicate {
			metrics.DuplicatesSkipped.WithLabelValues("raid").Inc()
			l.Debugf("Raid/egg level %d on gym %s is a duplicate, skipping", raid.Level, raid.GymID)
			return
		}

		// ignore_long_raids: skip raids/eggs with > 47 minutes remaining
		if ps.cfg.General.IgnoreLongRaids {
			tthSeconds := raid.End - time.Now().Unix()
			if tthSeconds > 47*60 {
				l.Debugf("Raid/egg on gym %s has %ds remaining (>47m), skipping (ignore_long_raids)", raid.GymID, tthSeconds)
				return
			}
		}

		st := ps.stateMgr.Get()
		ex := bool(raid.ExRaidEligible) || bool(raid.IsExRaidEligible)

		var matched []webhook.MatchedUser
		var matchedAreas []webhook.MatchedArea

		matchStart := time.Now()
		if raid.PokemonID > 0 {
			// Raid with boss
			raidData := &matching.RaidData{
				GymID:     raid.GymID,
				PokemonID: raid.PokemonID,
				Form:      raid.Form,
				Level:     raid.Level,
				TeamID:    raid.TeamID,
				Ex:        ex,
				Evolution: raid.Evolution,
				Move1:     raid.Move1,
				Move2:     raid.Move2,
				Latitude:  raid.Latitude,
				Longitude: raid.Longitude,
			}
			matched, matchedAreas = ps.raidMatcher.MatchRaid(raidData, st)
		} else {
			// Egg
			eggData := &matching.EggData{
				GymID:     raid.GymID,
				Level:     raid.Level,
				TeamID:    raid.TeamID,
				Ex:        ex,
				Latitude:  raid.Latitude,
				Longitude: raid.Longitude,
			}
			matched, matchedAreas = ps.raidMatcher.MatchEgg(eggData, st)
		}
		metrics.MatchingDuration.WithLabelValues("raid").Observe(time.Since(matchStart).Seconds())

		// Filter by rate limit
		matched = ps.filterBlocked(matched)

		// Filter by RSVP preference before sending.
		// Check for future RSVPs only — past timeslots are stripped during
		// enrichment so we must use the same cutoff here.
		nowMs := time.Now().UnixMilli()
		hasRSVPs := false
		for _, r := range raid.RSVPs {
			if r.Timeslot > nowMs {
				hasRSVPs = true
				break
			}
		}
		filtered := matched[:0]
		for _, m := range matched {
			switch m.RSVPChanges {
			case 0: // "no rsvp" — only first notification
				if !isFirstNotification {
					continue
				}
			case 2: // "rsvp only" — only when RSVPs present
				if !hasRSVPs {
					continue
				}
			}
			// case 1: "rsvp" — always notify (first + changes)
			filtered = append(filtered, m)
		}
		matched = filtered

		// External validation hook last so denied users don't burn validator
		// load when they would have been dropped by RSVP/rate-limit anyway.
		raidType := "raid"
		if raid.PokemonID == 0 {
			raidType = "egg"
		}
		matched = ps.filterValidation(raidType, raw, matchedAreas, matched)

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("raid").Inc()
			metrics.MatchedUsers.WithLabelValues("raid").Add(float64(len(matched)))

			msgType := "raid"
			if raid.PokemonID == 0 {
				msgType = "egg"
			}

			gymName := raid.GymName

			if raid.PokemonID > 0 {
				l.Infof("Raid %s L%d on %s at [%.3f,%.3f] areas(%s) and %d humans cared",
					ps.pokemonName(raid.PokemonID, raid.Form), raid.Level, gymName,
					raid.Latitude, raid.Longitude, areaNames(matchedAreas), len(matched))
			} else {
				l.Infof("Egg L%d on %s at [%.3f,%.3f] areas(%s) and %d humans cared",
					raid.Level, gymName, raid.Latitude, raid.Longitude, areaNames(matchedAreas), len(matched))
			}

			mode := ps.tileMode(msgType, matched)
			baseEnrichment, tilePending := ps.enricher.Raid(&raid, isFirstNotification, mode)

			var perLang map[string]map[string]any
			if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
				perLang = make(map[string]map[string]any)
				for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
					perLang[lang] = ps.enricher.RaidTranslate(baseEnrichment, &raid, lang)
				}
			}

			if ps.renderCh == nil {
				return
			}
			webhookFields := parseWebhookFields(raw)

			ps.renderCh <- RenderJob{
				TemplateType:      msgType,
				Enrichment:        baseEnrichment,
				PerLangEnrichment: perLang,
				WebhookFields:     webhookFields,
				MatchedUsers:      matched,
				MatchedAreas:      matchedAreas,
				TilePending:       tilePending,
				LogReference:      raid.GymID,
				EditKey:           fmt.Sprintf("raid:%s:%d", raid.GymID, raid.End),
			}
		} else {
			if raid.PokemonID > 0 {
				l.Debugf("Raid %s L%d at [%.3f,%.3f] and 0 humans cared",
					ps.pokemonName(raid.PokemonID, raid.Form), raid.Level,
					raid.Latitude, raid.Longitude)
			} else {
				l.Debugf("Egg L%d at [%.3f,%.3f] and 0 humans cared",
					raid.Level, raid.Latitude, raid.Longitude)
			}
		}
	}()
	return nil
}
