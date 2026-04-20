package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// pokemonName returns a human-readable pokemon name for logging.
// Uses English translation from the i18n bundle, falling back to "Pokemon {id}".
func (ps *ProcessorService) pokemonName(pokemonID, form int) string {
	if ps.enricher.Translations != nil {
		tr := ps.enricher.Translations.For("en")
		key := gamedata.PokemonTranslationKey(pokemonID)
		name := tr.T(key)
		if name != "" && name != key {
			return name
		}
	}
	return fmt.Sprintf("Pokemon %d", pokemonID)
}

// teamName returns a human-readable team name for logging.
func (ps *ProcessorService) teamName(teamID int) string {
	if ps.enricher.GameData != nil && ps.enricher.GameData.Util != nil {
		if info, ok := ps.enricher.GameData.Util.Teams[teamID]; ok {
			return info.Name
		}
	}
	return fmt.Sprintf("team %d", teamID)
}

// weatherName returns a human-readable weather name for logging.
func (ps *ProcessorService) weatherName(weatherID int) string {
	if ps.enricher.GameData != nil && ps.enricher.GameData.Util != nil {
		if info, ok := ps.enricher.GameData.Util.Weather[weatherID]; ok {
			return info.Name
		}
	}
	return fmt.Sprintf("weather %d", weatherID)
}

// lureName returns a human-readable lure name for logging.
func (ps *ProcessorService) lureName(lureID int) string {
	if ps.enricher.GameData != nil && ps.enricher.GameData.Util != nil {
		if info, ok := ps.enricher.GameData.Util.Lures[lureID]; ok {
			return info.Name
		}
	}
	return fmt.Sprintf("Lure %d", lureID)
}

// areaNames returns a comma-separated string of matched area names for logging.
func areaNames(areas []webhook.MatchedArea) string {
	if len(areas) == 0 {
		return ""
	}
	names := make([]string, 0, len(areas))
	for _, a := range areas {
		names = append(names, a.Name)
	}
	return strings.Join(names, ",")
}

// distinctLanguages returns the unique language codes from matched users.
// Users with no language set fall back to defaultLocale.
func distinctLanguages(matched []webhook.MatchedUser, defaultLocale string) []string {
	if defaultLocale == "" {
		defaultLocale = "en"
	}
	seen := make(map[string]bool, 4)
	var langs []string
	for _, m := range matched {
		lang := m.Language
		if lang == "" {
			lang = defaultLocale
		}
		if !seen[lang] {
			seen[lang] = true
			langs = append(langs, lang)
		}
	}
	return langs
}

// parseWebhookFields deserialises the raw webhook JSON into a map for use as
// a low-priority layer in the LayeredView. This gives custom DTS templates
// access to raw webhook fields not explicitly included in enrichment.
func parseWebhookFields(raw json.RawMessage) map[string]any {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	return fields
}

// buildMatchedAreas converts geofence areas to webhook MatchedArea structs.
func buildMatchedAreas(areas []geofence.MatchedArea) []webhook.MatchedArea {
	result := make([]webhook.MatchedArea, len(areas))
	for i, a := range areas {
		result[i] = webhook.MatchedArea{
			Name:             a.Name,
			DisplayInMatches: a.DisplayInMatches,
			Group:            a.Group,
		}
	}
	return result
}

// toInt converts a JSON number (float64) to int.
func toInt(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	}
	return 0
}

// filterBlocked drops matched users whose destination is currently over its
// rate limit. Non-mutating: it does not increment the counter or trigger
// notifications. The authoritative count lives in the delivery queue (see
// FairQueue.processJob) and only fires on actual delivery attempts.
//
// This is a cheap pre-filter to skip render/enrichment work for destinations
// we already know are paused.
func (ps *ProcessorService) filterBlocked(matched []webhook.MatchedUser) []webhook.MatchedUser {
	if ps.rateLimiter == nil {
		return matched
	}
	allowed := matched[:0]
	for _, m := range matched {
		if ps.rateLimiter.IsBlocked(m.ID, m.Type) {
			metrics.RateLimitDropped.Inc()
			log.Debugf("Rate limit pre-filter: skipping render for %s %s %s", m.Type, m.ID, m.Name)
			continue
		}
		allowed = append(allowed, m)
	}
	return allowed
}

// dispatchMessage sends a message directly via the delivery dispatcher.
func (ps *ProcessorService) dispatchMessage(target, typ, name, content, logRef string) {
	if ps.dispatcher == nil || content == "" {
		return
	}
	msgJSON, _ := json.Marshal(map[string]string{"content": content})
	ps.dispatcher.Dispatch(&delivery.Job{
		Target:       target,
		Type:         typ,
		Name:         name,
		Message:      msgJSON,
		TTH:          delivery.TTH{Hours: 1},
		LogReference: logRef,
	})
}

// dispatchBypass sends a message that must skip the rate-limit count and
// not be dropped if the destination is over its limit. Used for rate-limit
// notifications and ban farewells.
func (ps *ProcessorService) dispatchBypass(target, typ, name, content, logRef string) {
	if ps.dispatcher == nil || content == "" {
		return
	}
	msgJSON, _ := json.Marshal(map[string]string{"content": content})
	ps.dispatcher.DispatchBypass(&delivery.Job{
		Target:       target,
		Type:         typ,
		Name:         name,
		Message:      msgJSON,
		TTH:          delivery.TTH{Hours: 1},
		LogReference: logRef,
	})
}

// OnBreach implements delivery.RateLimitHooks. Invoked from a delivery worker
// the first time a destination crosses the rate limit within the current
// window. Sends a single notification telling the user they have been muted.
func (ps *ProcessorService) OnBreach(target, typ, name, language string, limit, _ int) {
	tr := ps.translations.For(language)
	msg := tr.Tf("rate_limit.reached", limit, ps.cfg.AlertLimits.TimingPeriod)
	ps.dispatchBypass(target, typ, name, msg, "RateLimit")
}

// OnBan implements delivery.RateLimitHooks. Invoked when a destination has
// accumulated enough breaches in 24h to be banned. Disables the human in the
// DB, sends a farewell message, posts to the shame channel if configured, and
// triggers a debounced state reload so the user is removed from matching.
func (ps *ProcessorService) OnBan(target, typ, name, language string) {
	tr := ps.translations.For(language)
	var msg string
	if ps.cfg.AlertLimits.DisableOnStop {
		if ps.humans != nil {
			if err := ps.humans.SetAdminDisable(target, true); err != nil {
				log.Errorf("Rate limit: failed to admin_disable user %s: %s", target, err)
			}
		}
		msg = tr.T("rate_limit.banned_hard")
	} else {
		if ps.humans != nil {
			if err := ps.humans.SetEnabled(target, false); err != nil {
				log.Errorf("Rate limit: failed to disable user %s: %s", target, err)
			}
		}
		prefix := ps.cfg.Discord.Prefix
		if typ == "telegram:user" || typ == "telegram:channel" || typ == "telegram:group" {
			prefix = "/"
		}
		msg = tr.Tf("rate_limit.banned_soft", prefix)
	}

	ps.dispatchBypass(target, typ, name, msg, "RateLimit")

	if ps.cfg.AlertLimits.ShameChannel != "" {
		shameContent := tr.Tf("rate_limit.shame", target)
		ps.dispatchBypass(ps.cfg.AlertLimits.ShameChannel, "discord:channel", "Shame channel", shameContent, "RateLimit")
	}

	ps.triggerReload()
}

// disableUserForDeliveryFailure is invoked by the delivery queue after N consecutive
// send failures. Sets enabled=0 via the human store, posts to the shame channel
// (if configured), and triggers a state reload so the user is removed from matching.
//
// Called from a delivery worker goroutine — must be safe for concurrent use.
func (ps *ProcessorService) disableUserForDeliveryFailure(target, name, jobType string) {
	if ps.humans == nil {
		return
	}
	if err := ps.humans.SetEnabled(target, false); err != nil {
		log.Errorf("Delivery failure: failed to disable user %s: %s", target, err)
		return
	}

	// Post shame message if configured (Discord users only — Telegram doesn't have channel pings the same way)
	if ps.cfg.AlertLimits.ShameChannel != "" && strings.HasPrefix(jobType, "discord:") {
		tr := ps.translations.For(ps.cfg.General.Locale)
		shameContent := tr.Tf("delivery.shame", target)
		ps.dispatchMessage(ps.cfg.AlertLimits.ShameChannel, "discord:channel", "Shame channel", shameContent, "DeliveryFail")
	}

	ps.triggerReload()
}

// triggerReload schedules a debounced state reload. Multiple calls within 500ms
// are coalesced into a single reload. Safe to call from any goroutine.
// Used by: rate-limit disable, profile scheduler, and (on api branch) tracking mutations.
func (ps *ProcessorService) triggerReload() {
	ps.reloadMu.Lock()
	defer ps.reloadMu.Unlock()

	if ps.reloadTimer != nil {
		ps.reloadTimer.Stop()
	}
	ps.reloadTimer = time.AfterFunc(500*time.Millisecond, func() {
		if err := state.Load(ps.stateMgr, ps.database); err != nil {
			log.Errorf("Debounced state reload failed: %s", err)
		}
	})
}

