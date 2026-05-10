package main

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// DispatchQuestSummary is the SummaryDispatch callback installed on the
// SummaryScheduler for alertType=="quest" (and is also reused by the
// !summary now command in PR 7). It re-enriches the buffered raw
// webhooks per the human's current language, groups them by
// (rewardType, reward), renders one questSummary message per group,
// dispatches via the delivery dispatcher, and clears the bucket.
//
// Errors at any stage are logged and tolerated — a single bad webhook
// must not strand the scheduler. The buffer is cleared on every exit
// path so empty/expired buckets don't pile up.
func (ps *ProcessorService) DispatchQuestSummary(humanID, alertType string) {
	if alertType != AlertTypeQuest {
		return
	}
	if ps.summaryBuffer == nil {
		return
	}

	rows := ps.summaryBuffer.List(humanID, alertType)
	if len(rows) == 0 {
		return
	}

	// From here on, we always end the bucket — either successfully
	// dispatched or because we couldn't (expired, missing human, no
	// groups, no renderer). Defer clears the buffer once.
	defer ps.summaryBuffer.Clear(humanID, alertType)

	// Drop expired entries up-front so we never render or deliver a
	// quest the player can no longer collect.
	now := time.Now().Unix()
	fresh := rows[:0]
	for _, r := range rows {
		if r.ExpiresAt > now {
			fresh = append(fresh, r)
		}
	}
	if len(fresh) == 0 {
		return
	}

	human, err := ps.humans.Get(humanID)
	if err != nil {
		log.Warnf("summary dispatch: humans.Get(%s): %v — clearing buffer", humanID, err)
		return
	}
	if human == nil {
		return
	}

	lang := human.Language
	if lang == "" {
		lang = ps.cfg.General.Locale
	}

	// Group buffered entries by reward, preserving first-seen order so
	// users see groups in the order their quests arrived.
	type groupKey struct{ Type, Reward int }
	groups := map[groupKey][]map[string]any{}
	order := []groupKey{}

	for _, r := range fresh {
		view := ps.questEnrichOne(r.Payload, lang)
		if view == nil {
			continue
		}
		view["withAR"] = r.WithAR
		k := groupKey{r.RewardType, r.Reward}
		if _, exists := groups[k]; !exists {
			order = append(order, k)
		}
		groups[k] = append(groups[k], view)
	}

	if len(groups) == 0 {
		return
	}

	// Synthesise the recipient set for the renderer. The summary
	// schedule belongs to the human, so the only destination is the
	// human's own row.
	matched := []webhook.MatchedUser{{
		ID:        human.ID,
		Type:      human.Type,
		Name:      human.Name,
		Language:  lang,
		Latitude:  human.Latitude,
		Longitude: human.Longitude,
		// Template/Clean/Ping default to zero values; the user's
		// summary-bucket clean bit is intentionally not propagated to
		// the summary message (the summary itself is a fresh send).
	}}

	if ps.dtsRenderer == nil {
		log.Warnf("summary dispatch: DTS renderer not configured — dropping %d groups for %s", len(groups), humanID)
		return
	}

	tr := ps.translations.For(lang)
	for _, k := range order {
		view := dts.BuildQuestSummaryView(k.Type, k.Reward, groups[k], ps.enricher.StaticMap, tr)

		jobs := ps.dtsRenderer.RenderQuestSummary(
			view,
			matched,
			nil, // no matched-areas for the summary header
			"summary:"+humanID+":"+alertType,
			"", // no edit-key base; summary messages are not edit-tracked
		)
		if len(jobs) == 0 {
			continue
		}

		if ps.dispatcher == nil {
			log.Warnf("summary dispatch: dispatcher not configured — dropping %d jobs for %s", len(jobs), humanID)
			continue
		}
		for _, j := range jobs {
			ps.dispatcher.Dispatch(&delivery.Job{
				Target:       j.Target,
				Type:         j.Type,
				Message:      j.Message,
				TTH:          tthFromMap(j.TTH),
				Clean:        j.Clean,
				Name:         j.Name,
				LogReference: j.LogReference,
				Lat:          parseCoordFloat(j.Lat),
				Lon:          parseCoordFloat(j.Lon),
				EditKey:      j.EditKey,
				Language:     j.Language,
			})
		}
	}
}

// questEnrichOne re-runs the quest enrichment pipeline (base +
// per-language) on a buffered raw webhook for the given language,
// returning the merged per-pokestop view used by BuildQuestSummaryView.
//
// We deliberately skip the static map (TileModeSkip) because the
// summary tile is built once at the group level over all pokestops, not
// per-pokestop. The per-pokestop entries supply latitude/longitude/
// pokestopName, which BuildQuestSummaryView feeds into the multi-pin
// autoposition.
//
// Returns nil for malformed payloads so the caller can drop the entry
// without aborting the rest of the bucket.
func (ps *ProcessorService) questEnrichOne(raw []byte, lang string) map[string]any {
	if ps.enricher == nil {
		return nil
	}
	var qw webhook.QuestWebhook
	if err := json.Unmarshal(raw, &qw); err != nil {
		log.Debugf("summary dispatch: malformed quest payload: %v", err)
		return nil
	}

	rewards := make([]matching.QuestRewardData, 0, len(qw.Rewards))
	for _, r := range qw.Rewards {
		rewards = append(rewards, parseQuestReward(r))
	}

	base, _ := ps.enricher.Quest(qw.Latitude, qw.Longitude, qw.PokestopID, qw.URL, rewards, enrichment.TileModeSkip)
	if base == nil {
		base = make(map[string]any)
	}
	perLang := ps.enricher.QuestTranslate(base, &qw, rewards, lang)

	// Merge base ∪ perLang into a single per-pokestop view. Per-language
	// entries win on conflict (mirrors the LayeredView priority for
	// regular quest rendering).
	out := make(map[string]any, len(base)+len(perLang)+4)
	for k, v := range base {
		out[k] = v
	}
	for k, v := range perLang {
		out[k] = v
	}

	// Preserve original webhook fields the regular quest renderer would
	// expose via the LayeredView's webhook layer — pokestopName /
	// latitude / longitude in particular are needed by
	// BuildQuestSummaryView to build the multi-pin map.
	if _, ok := out["pokestopName"]; !ok {
		out["pokestopName"] = qw.Name
	}
	if _, ok := out["latitude"]; !ok {
		out["latitude"] = qw.Latitude
	}
	if _, ok := out["longitude"]; !ok {
		out["longitude"] = qw.Longitude
	}
	if _, ok := out["pokestop_id"]; !ok {
		out["pokestop_id"] = qw.PokestopID
	}
	return out
}
