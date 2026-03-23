package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/ratelimit"
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

// filterRateLimited removes rate-limited users from matched results.
// It sends notifications for first breaches and disables users who exceed the violation threshold.
func (ps *ProcessorService) filterRateLimited(matched []webhook.MatchedUser) []webhook.MatchedUser {
	var allowed []webhook.MatchedUser
	for _, m := range matched {
		result := ps.rateLimiter.Check(m.ID, m.Type)
		if result.Allowed {
			allowed = append(allowed, m)
		} else if result.JustBreached {
			metrics.RateLimitBreaches.Inc()
			metrics.RateLimitDropped.Inc()
			log.Infof("Rate limit reached for %s %s %s (%d messages in %ds)",
				m.Type, m.ID, m.Name, result.Limit, result.ResetSeconds)
			ps.sendRateLimitNotification(m, result)
			if result.Banned {
				metrics.RateLimitDisabled.Inc()
				log.Infof("Rate limit: disabling %s %s %s (too many violations)", m.Type, m.ID, m.Name)
				ps.disableUser(m, result)
			}
		} else {
			metrics.RateLimitDropped.Inc()
			log.Debugf("Rate limited: dropping message for %s %s %s", m.Type, m.ID, m.Name)
		}
	}
	return allowed
}

// postMessage is a message payload for the alerter's POST /api/postMessage endpoint.
type postMessage struct {
	Target       string         `json:"target"`
	Type         string         `json:"type"`
	Name         string         `json:"name"`
	Message      messageContent `json:"message"`
	Language     string         `json:"language"`
	TTH          tth            `json:"tth"`
	Clean        bool           `json:"clean"`
	AlwaysSend   bool           `json:"alwaysSend"`
	LogReference string         `json:"logReference"`
}

type messageContent struct {
	Content string `json:"content"`
}

type tth struct {
	Hours   int `json:"hours"`
	Minutes int `json:"minutes"`
	Seconds int `json:"seconds"`
}

// sendRateLimitNotification sends a rate limit warning message to the user via the alerter.
func (ps *ProcessorService) sendRateLimitNotification(user webhook.MatchedUser, result ratelimit.RateResult) {
	tr := ps.translations.For(user.Language)
	msg := tr.Tf("rate_limit.reached", result.Limit, ps.cfg.AlertLimits.TimingPeriod)

	ps.postMessageToAlerter(postMessage{
		Target:       user.ID,
		Type:         user.Type,
		Name:         user.Name,
		Message:      messageContent{Content: msg},
		Language:     user.Language,
		TTH:          tth{Hours: 1},
		AlwaysSend:   true,
		LogReference: "RateLimit",
	})
}

// disableUser disables a user in the DB and sends a final notification.
func (ps *ProcessorService) disableUser(user webhook.MatchedUser, result ratelimit.RateResult) {
	tr := ps.translations.For(user.Language)
	var msg string
	if ps.cfg.AlertLimits.DisableOnStop {
		_, err := ps.database.Exec("UPDATE humans SET admin_disable = 1, disabled_date = NULL WHERE id = ?", user.ID)
		if err != nil {
			log.Errorf("Rate limit: failed to admin_disable user %s: %s", user.ID, err)
		}
		msg = tr.T("rate_limit.banned_hard")
	} else {
		_, err := ps.database.Exec("UPDATE humans SET enabled = 0 WHERE id = ?", user.ID)
		if err != nil {
			log.Errorf("Rate limit: failed to disable user %s: %s", user.ID, err)
		}
		prefix := ps.cfg.Discord.Prefix
		if user.Type == "telegram:user" || user.Type == "telegram:channel" || user.Type == "telegram:group" {
			prefix = "/"
		}
		msg = tr.Tf("rate_limit.banned_soft", prefix)
	}

	ps.postMessageToAlerter(postMessage{
		Target:       user.ID,
		Type:         user.Type,
		Name:         user.Name,
		Message:      messageContent{Content: msg},
		Language:     user.Language,
		TTH:          tth{Hours: 1},
		AlwaysSend:   true,
		LogReference: "RateLimit",
	})

	// Send shame message if configured
	if ps.cfg.AlertLimits.ShameChannel != "" {
		shameContent := tr.Tf("rate_limit.shame", user.ID)
		ps.postMessageToAlerter(postMessage{
			Target:       ps.cfg.AlertLimits.ShameChannel,
			Type:         "discord:channel",
			Name:         "Shame channel",
			Message:      messageContent{Content: shameContent},
			Language:     "en",
			LogReference: "RateLimit",
		})
	}

	// Trigger state reload so user is removed from matching
	go func() {
		if err := ps.reloadState(); err != nil {
			log.Errorf("Rate limit: failed to reload state after disabling user %s: %s", user.ID, err)
		}
	}()
}

// reloadState reloads the in-memory state from the database.
func (ps *ProcessorService) reloadState() error {
	return state.Load(ps.stateMgr, ps.database, ps.cfg.Geofence)
}

// postMessageToAlerter sends a message via the alerter's POST /api/postMessage endpoint.
func (ps *ProcessorService) postMessageToAlerter(msg postMessage) {
	data, err := json.Marshal([]postMessage{msg})
	if err != nil {
		log.Errorf("Rate limit: failed to marshal postMessage: %s", err)
		return
	}

	req, err := http.NewRequest("POST", ps.cfg.Processor.AlerterURL+"/api/postMessage", bytes.NewReader(data))
	if err != nil {
		log.Errorf("Rate limit: failed to create postMessage request: %s", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if ps.cfg.Processor.APISecret != "" {
		req.Header.Set("X-Poracle-Secret", ps.cfg.Processor.APISecret)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("Rate limit: failed to send postMessage to alerter: %s", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Errorf("Rate limit: alerter returned status %d for postMessage", resp.StatusCode)
	}
}
