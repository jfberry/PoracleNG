package main

import (
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Key format strings used to build per-raid MessageTracker keys. Exported as
// constants so tests can validate the exact format without grepping source.
const (
	// raidReplyKeyFmt identifies the raid window for reply threading. The
	// "raidlife:" prefix lets the MessageTracker link successive alerts
	// (egg → boss) for the same gym+end_time pair.
	raidReplyKeyFmt = "raidlife:%s:%d"
	// raidEditKeyFmt identifies a specific raid boss for edit-mode updates.
	raidEditKeyFmt = "raid:%s:%d"
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

		// Record the boss for slash autocomplete recency (no-op when ID is 0/egg).
		if ps.recentActivity != nil {
			ps.recentActivity.RecordRaidBoss(raid.PokemonID)
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
		matched = ps.filterMuted(matched, matchedAreas, mute.Event{
			GymID:     raid.GymID,
			PokemonID: raid.PokemonID,
		})

		if len(matched) > 0 {
			metrics.MatchedEvents.WithLabelValues("raid").Inc()
			metrics.MatchedUsers.WithLabelValues("raid").Add(float64(len(matched)))
			metrics.IntervalMatched.Add(1)

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

			mode := ps.tileMode(msgType, matched, raid.GymID)
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
			replyKey := fmt.Sprintf(raidReplyKeyFmt, raid.GymID, raid.End)
			editKey := fmt.Sprintf(raidEditKeyFmt, raid.GymID, raid.End)

			// Partition matched users: users who should receive the full
			// raid/egg template vs. those who should receive the compact
			// rsvpChanges template.
			var ts *dts.TemplateStore
			var defaultTemplateID string
			if ps.dtsRenderer != nil {
				ts = ps.dtsRenderer.Templates()
				// ResolveTemplate("") returns the configured default template
				// name (e.g. "1" or "default"). Passing "" mirrors the logic
				// the renderer uses when a user's tracking rule has no explicit
				// template set.
				defaultTemplateID = ps.dtsRenderer.ResolveTemplate("")
			}
			var lookupReply func(string, string) *delivery.TrackedMessage
			if ps.dispatcher != nil {
				lookupReply = ps.dispatcher.MessageTracker().LookupReplyMessage
			}
			fullUsers, rsvpUsers := partitionRaidUsers(matched, replyKey, msgType, lookupReply, ts, defaultTemplateID)

			// Compute the latest RSVP timeslot for OverrideCleanTTH on
			// rsvpChanges jobs.
			latestTimeslotSec := latestFutureTimeslotSec(rsvps, nowMs)
			if latestTimeslotSec == 0 && len(rsvps) > 0 {
				// All known RSVP timeslots have already passed — fall back to raid.End.
				l.Debugf("rsvpChanges: no future RSVP timeslot for raid %s; cleaning at raid.End", raid.GymID)
			}

			// tileGate is shared between up to two jobs. The gate goroutine
			// closes gate.ready after mutating Enrichment via Apply, and
			// chan-close happens-before ensures the mutation is visible to
			// all readers after <-ready. Both jobs read gate.bytes after
			// blocking on ready, which is safe.
			gate := ps.newTileGate(tilePending)

			if len(fullUsers) > 0 {
				ps.renderCh <- RenderJob{
					AlertType:         msgType,
					TemplateType:      msgType,
					Enrichment:        baseEnrichment,
					PerLangEnrichment: perLang,
					WebhookFields:     webhookFields,
					MatchedUsers:      fullUsers,
					MatchedAreas:      matchedAreas,
					TileGate:          gate,
					LogReference:      raid.GymID,
					EditKey:           editKey,
					ReplyKey:          replyKey,
				}
			}
			if len(rsvpUsers) > 0 {
				job := RenderJob{
					AlertType:         msgType,
					TemplateType:      "rsvpChanges",
					Enrichment:        baseEnrichment,
					PerLangEnrichment: perLang,
					WebhookFields:     webhookFields,
					MatchedUsers:      rsvpUsers,
					MatchedAreas:      matchedAreas,
					TileGate:          gate,
					LogReference:      raid.GymID,
					EditKey:           editKey,
					ReplyKey:          replyKey,
				}
				// OverrideCleanTTH is only set when we have a real future
				// timeslot. Zero means "use the render pool's default path"
				// (raid.End from the enrichment map), which is the correct
				// fallback when all RSVP timeslots have already passed.
				if latestTimeslotSec > 0 {
					job.OverrideCleanTTH = latestTimeslotSec
				}
				ps.renderCh <- job
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

// partitionRaidUsers splits matched users into those who should receive the
// full raid/egg template (fullUsers) vs. those who should receive the compact
// rsvpChanges template (rsvpUsers).
//
// rsvpChanges is used when ALL of the following hold:
//   - the user has a prior tracked message for this raid lifecycle
//     (lookupReply returns non-nil for this user — i.e. not first-visible)
//   - the prior message's MsgType matches the current msgType (e.g. egg
//     prior does not count as a prior for a raid job — first lifecycle type
//     always uses the full template)
//   - the user is not in edit mode (edit always uses the full template)
//   - an rsvpChanges template exists for the user's platform AND the user's
//     effective template ID (u.Template if set, else defaultTemplateID)
//   - ts is non-nil (DTS renderer is wired)
//
// The effective template ID is resolved before calling ts.Exists because
// ts.Exists is a strict literal match — it does not apply the renderer's
// normal resolution of an empty u.Template to the configured default. Without
// this step, operators who install the example rsvpChanges template (id:default
// or id:1) and use default tracking rules (Template:"") would never match, so
// rsvpChanges would never fire.
//
// The rsvpChanges existence check uses strict equality on the resolved ID.
// If the user has template:"dark" but no rsvpChanges template with id:"dark"
// exists, the user falls through to the full raid/egg template — even if an
// rsvpChanges template with a different ID (e.g. id:"default") exists.
//
// "First-visible" is determined per-user via the MessageTracker reply index.
// This fixes the rsvp_only (rsvpChanges=2) case: the duplicate cache is
// updated on the suppressed initial webhook, so isFirstNotification would be
// false on the first visible message. Per-user tracker lookup correctly
// identifies that no prior message exists for this user.
//
// msgType is the current job's alert type ("raid" or "egg"). It is compared
// against the prior message's MsgType so that an egg-typed prior message does
// not suppress the full template for a subsequent raid job on the same gym
// lifecycle key.
//
// All other users receive the full msgType template.
func partitionRaidUsers(
	matched []webhook.MatchedUser,
	replyKey string,
	msgType string,
	lookupReply func(replyKey, target string) *delivery.TrackedMessage,
	ts *dts.TemplateStore,
	defaultTemplateID string,
) (fullUsers, rsvpUsers []webhook.MatchedUser) {
	for _, u := range matched {
		// Edit mode always uses the full template.
		if db.IsEdit(u.Clean) {
			log.Debugf("raid partition: replyKey=%s target=%s userTemplate=%q decision=edit",
				replyKey, u.ID, u.Template)
			fullUsers = append(fullUsers, u)
			continue
		}
		if ts == nil {
			log.Debugf("raid partition: replyKey=%s target=%s userTemplate=%q decision=nil-ts",
				replyKey, u.ID, u.Template)
			fullUsers = append(fullUsers, u)
			continue
		}
		// Per-user first-visible check: treat the user as first-visible when
		// either:
		//   (a) lookupReply is nil (dispatcher not yet wired), or
		//   (b) the tracker has no prior message for this (replyKey, user) pair, or
		//   (c) the prior message was for a different alert type (e.g. the
		//       tracker holds an "egg" entry but the current job is "raid").
		// Case (c) fixes the scenario where both egg + raid are tracked under
		// the same raidlife:gym:end key: the egg notification stores its
		// TrackedMessage with MsgType="egg"; when the raid arrives, the prior
		// entry exists but its MsgType differs — so the raid is correctly
		// treated as first-visible and rendered with the full raid template.
		var prior *delivery.TrackedMessage
		if lookupReply != nil {
			prior = lookupReply(replyKey, u.ID)
		}
		firstVisible := prior == nil || prior.MsgType != msgType
		if firstVisible {
			log.Debugf("raid partition: replyKey=%s target=%s userTemplate=%q decision=first-visible",
				replyKey, u.ID, u.Template)
			fullUsers = append(fullUsers, u)
			continue
		}
		// Resolve the user's effective template ID: use u.Template when
		// explicitly set, otherwise fall back to the configured default.
		// ts.Exists is a strict literal match; without this step, tracking
		// rules with no explicit template (Template=="") would never find
		// an rsvpChanges entry even when one is installed under the default
		// template ID.
		effectiveID := u.Template
		if effectiveID == "" {
			effectiveID = defaultTemplateID
		}
		platform := delivery.PlatformFromType(u.Type)
		// Pass "" for language — we're checking template existence at the type
		// level; the renderer's own selection chain handles language fallback
		// if the user's locale doesn't have an exact match.
		if ts.Exists("rsvpChanges", platform, effectiveID, "") {
			log.Debugf("raid partition: replyKey=%s target=%s userTemplate=%q effectiveID=%q decision=rsvpChanges",
				replyKey, u.ID, u.Template, effectiveID)
			rsvpUsers = append(rsvpUsers, u)
		} else {
			log.Debugf("raid partition: replyKey=%s target=%s userTemplate=%q effectiveID=%q decision=no-rsvpChanges-template",
				replyKey, u.ID, u.Template, effectiveID)
			fullUsers = append(fullUsers, u)
		}
	}
	return
}

// latestFutureTimeslotSec returns the latest RSVP timeslot (in seconds) that
// is in the future relative to nowMs (milliseconds). Timeslots are stored in
// milliseconds; the ceiling ms→s conversion (ms+999)/1000 matches the pattern
// used in enrichment/raid.go. Returns 0 if no future timeslot exists.
func latestFutureTimeslotSec(rsvps []tracker.RaidRSVP, nowMs int64) int64 {
	var latest int64
	for _, r := range rsvps {
		if r.Timeslot <= nowMs {
			continue
		}
		sec := (r.Timeslot + 999) / 1000
		if sec > latest {
			latest = sec
		}
	}
	return latest
}
