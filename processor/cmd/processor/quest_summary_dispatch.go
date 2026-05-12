package main

import (
	"encoding/json"
	"maps"
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
	// users see groups in the order their quests arrived. Form is part
	// of the key for pokemon-encounter rewards so different Spinda
	// forms (etc.) don't share an icon / rewardName / map.
	type groupKey struct{ Type, Reward, Form int }
	groups := map[groupKey][]map[string]any{}
	order := []groupKey{}

	for _, r := range fresh {
		view := ps.questEnrichOne(r.Payload, lang)
		if view == nil {
			continue
		}
		view["withAR"] = r.WithAR
		k := groupKey{r.RewardType, r.Reward, r.Form}
		if _, exists := groups[k]; !exists {
			order = append(order, k)
		}
		groups[k] = append(groups[k], view)
	}

	if len(groups) == 0 {
		return
	}

	// Aggregate clean-flag bitmask + latest expiry across the fresh
	// entries. The summary message inherits clean-deletion semantics
	// from any constituent rule that set the bit (OR), and lives until
	// the longest-running quest in the digest expires (max). Without
	// this, summary messages either never auto-deleted or deleted too
	// early relative to the quests they describe.
	aggregatedClean := 0
	latestExpiresAt := int64(0)
	for _, r := range fresh {
		aggregatedClean |= r.Clean
		if r.ExpiresAt > latestExpiresAt {
			latestExpiresAt = r.ExpiresAt
		}
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
		Clean:     aggregatedClean,
	}}

	// External-validator gate at delivery time. Immediate quests run
	// filterValidation in ProcessQuest; the buffered path defers it to
	// here so the validator's "is this destination valid right now?"
	// answer reflects current state rather than state from hours ago.
	// We send the first buffered quest's raw payload as a representative
	// webhook (the validator's user-level checks dominate; per-quest
	// fidelity isn't preserved here — operators with per-quest validator
	// logic should validate at match time instead). matchedAreas is nil:
	// the summary aggregates quests from many pokestops, so there's no
	// single area set to validate against.
	matched = ps.filterValidation("quest", fresh[0].Payload, nil, matched)
	if len(matched) == 0 {
		return
	}

	// Summary rate-limit check — separate bucket from the alert limiter
	// (see CLAUDE.md "Rate Limiting"). One fire = one against the bucket,
	// regardless of how many chunks the digest produces. If over the cap,
	// drop the whole dispatch (the buffer is already cleared by the
	// defer, so the quests are gone from the user's view either way) and
	// fire a one-time breach notification so they know.
	if ps.rateLimiter != nil {
		result := ps.rateLimiter.CheckSummary(human.ID, human.Type)
		if !result.Allowed {
			if result.JustBreached {
				ps.notifySummaryRateBreach(human.ID, human.Type, human.Name, lang, result.Limit)
			}
			log.Infof("summary dispatch: %s over summary limit (%d/window=%ds) — dropping digest of %d entries",
				human.ID, result.Limit, ps.cfg.AlertLimits.TimingPeriod, len(fresh))
			return
		}
	}

	if ps.dtsRenderer == nil {
		log.Warnf("summary dispatch: DTS renderer not configured — dropping %d groups for %s", len(groups), humanID)
		return
	}

	tr := ps.translations.For(lang)
	maxPerMessage := ps.cfg.Summariser.MaxPerMessage
	for _, k := range order {
		all := groups[k]
		for _, chunk := range chunkPerMessage(all, maxPerMessage) {
			view := dts.BuildQuestSummaryView(dts.QuestSummaryGroup{
				RewardType: k.Type,
				RewardID:   k.Reward,
				RewardForm: k.Form,
				Quests:     chunk.entries,
				TotalCount: len(all),
				Chunk:      chunk.index,
				Chunks:     chunk.total,
			}, ps.enricher.StaticMap, tr)

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
				// TTH on a summary message tracks the LATEST ExpiresAt
				// across the dispatched chunk, so clean-deletion fires
				// when the longest-running constituent quest expires.
				// Per-job TTH from the renderer is ignored — the summary
				// has no single "until" of its own; it inherits from the
				// quests it describes.
				ttlSecs := max(latestExpiresAt-time.Now().Unix(), 0)
				// DispatchBypass: the user opted into summary mode
				// explicitly (via the `summary` keyword on `!quest`) and
				// is expecting a scheduled digest message. Silently
				// dropping it at the limiter would be more confusing
				// than letting it through, and the buffer is naturally
				// bounded by the user's tracked-quest count so the
				// throughput risk is small. See the BypassRateLimit
				// section in CLAUDE.md for the documented exemption.
				ps.dispatcher.DispatchBypass(&delivery.Job{
					Target:       j.Target,
					Type:         j.Type,
					Message:      j.Message,
					TTH:          delivery.TTH{Seconds: int(ttlSecs)},
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
}

// summaryChunk is one slice of a reward group plus its 1-indexed
// position. For unsplit groups the result is a single chunk with
// index=1, total=1.
type summaryChunk struct {
	entries []map[string]any
	index   int
	total   int
}

// chunkPerMessage splits entries into chunks of at most size each. A
// non-positive size disables splitting (returns one chunk containing
// everything). Empty input returns no chunks.
func chunkPerMessage(entries []map[string]any, size int) []summaryChunk {
	if len(entries) == 0 {
		return nil
	}
	if size <= 0 || len(entries) <= size {
		return []summaryChunk{{entries: entries, index: 1, total: 1}}
	}
	total := (len(entries) + size - 1) / size
	out := make([]summaryChunk, 0, total)
	for i := 0; i < len(entries); i += size {
		end := min(i+size, len(entries))
		out = append(out, summaryChunk{
			entries: entries[i:end],
			index:   len(out) + 1,
			total:   total,
		})
	}
	return out
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
	maps.Copy(out, base)
	maps.Copy(out, perLang)

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
