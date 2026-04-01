package dts

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	raymond "github.com/mailgun/raymond/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// RendererConfig holds configuration for creating a Renderer.
type RendererConfig struct {
	ConfigDir     string
	FallbackDir   string
	GameData      *gamedata.GameData
	Translations  *i18n.Bundle
	UtilEmojis    map[string]string  // from GameData.Util.Emojis
	ShlinkURL     string             // empty = no shortening
	ShlinkKey     string
	ShlinkDomain  string
	DTSDictionary map[string]any     // from config [general] dts_dictionary
	DefaultLocale string             // fallback language (e.g. "en")
	MinAlertTime  int                // minimum seconds remaining for alert
}

// Renderer ties together templates, enrichment, emoji, and URL shortening
// to produce DeliveryJobs from matched webhook data.
type Renderer struct {
	templates   *TemplateStore
	viewBuilder *ViewBuilder
	shortener   *ShlinkShortener // nil if not configured
	gd          *gamedata.GameData
	bundle      *i18n.Bundle
	emoji       *EmojiLookup
	locale      string
	minAlertSec int
}

// NewRenderer creates a Renderer from the given configuration.
func NewRenderer(cfg RendererConfig) (*Renderer, error) {
	ts, err := LoadTemplates(cfg.ConfigDir, cfg.FallbackDir)
	if err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}

	emoji := LoadEmoji(cfg.ConfigDir, cfg.UtilEmojis)

	RegisterHelpers()
	RegisterGameHelpers(cfg.GameData, cfg.Translations, emoji, cfg.ConfigDir)

	vb := NewViewBuilder(emoji, cfg.DTSDictionary)

	var shortener *ShlinkShortener
	if cfg.ShlinkURL != "" {
		shortener = NewShlinkShortener(cfg.ShlinkURL, cfg.ShlinkKey, cfg.ShlinkDomain)
	}

	locale := cfg.DefaultLocale
	if locale == "" {
		locale = "en"
	}

	return &Renderer{
		templates:   ts,
		viewBuilder: vb,
		shortener:   shortener,
		gd:          cfg.GameData,
		bundle:      cfg.Translations,
		emoji:       emoji,
		locale:      locale,
		minAlertSec: cfg.MinAlertTime,
	}, nil
}

// Templates returns the underlying TemplateStore.
func (r *Renderer) Templates() *TemplateStore { return r.templates }

// RenderPokemon renders pokemon alerts for all matched users and returns delivery jobs.
// Pokemon has special handling: user deduplication (the alerter historically deduped,
// but the renderer does it here) and template type selection based on encounter status.
func (r *Renderer) RenderPokemon(
	enrichment map[string]any,
	perLangEnrichment map[string]map[string]any,
	perUserEnrichment map[string]map[string]any,
	webhookFields map[string]any,
	matchedUsers []webhook.MatchedUser,
	matchedAreas []webhook.MatchedArea,
	isEncountered bool,
	logReference string,
) []webhook.DeliveryJob {
	// 1. Check TTH
	if r.isBelowMinAlertTime(enrichment) {
		return nil
	}

	// 2. Deduplicate users: keep first occurrence per user ID
	uniqueUsers := deduplicateUsers(matchedUsers)

	// 3. Select template type based on encounter status
	templateType := "monster"
	if !isEncountered {
		templateType = "monsterNoIv"
	}

	return r.renderForUsers(templateType, enrichment, perLangEnrichment, perUserEnrichment, webhookFields, uniqueUsers, matchedAreas, logReference)
}

// RenderAlert renders alerts for any non-pokemon type and returns delivery jobs.
// Unlike RenderPokemon, this does not deduplicate users or select template type
// dynamically — the caller provides the template type directly.
func (r *Renderer) RenderAlert(
	templateType string,
	enrichment map[string]any,
	perLangEnrichment map[string]map[string]any,
	webhookFields map[string]any,
	matchedUsers []webhook.MatchedUser,
	matchedAreas []webhook.MatchedArea,
	logReference string,
) []webhook.DeliveryJob {
	if r.isBelowMinAlertTime(enrichment) {
		return nil
	}

	return r.renderForUsers(templateType, enrichment, perLangEnrichment, nil, webhookFields, matchedUsers, matchedAreas, logReference)
}

// isBelowMinAlertTime checks whether the TTH in enrichment is below the configured minimum.
func (r *Renderer) isBelowMinAlertTime(enrichment map[string]any) bool {
	_, tthSeconds := extractTTH(enrichment)
	return r.minAlertSec > 0 && tthSeconds > 0 && tthSeconds < r.minAlertSec
}

// renderForUsers is the shared rendering loop that produces DeliveryJobs for each user.
func (r *Renderer) renderForUsers(
	templateType string,
	enrichment map[string]any,
	perLangEnrichment map[string]map[string]any,
	perUserEnrichment map[string]map[string]any,
	webhookFields map[string]any,
	users []webhook.MatchedUser,
	areas []webhook.MatchedArea,
	logReference string,
) []webhook.DeliveryJob {
	tthMap, _ := extractTTH(enrichment)
	lat := truncateCoord(lookupFloat(enrichment, webhookFields, "latitude"))
	lon := truncateCoord(lookupFloat(enrichment, webhookFields, "longitude"))

	// Per-call Shlink cache: avoids redundant HTTP requests when many users
	// receive the same template with identical URLs.
	var shlinkCache map[string]string
	if r.shortener != nil {
		shlinkCache = make(map[string]string)
	}

	// Group-render optimization for non-pokemon types: when there is no per-user
	// enrichment, users with the same (template, platform, language) get identical
	// rendered output. Render once per group and clone the result.
	if perUserEnrichment == nil {
		return r.renderGrouped(templateType, enrichment, perLangEnrichment, webhookFields, users, areas, logReference, tthMap, lat, lon, shlinkCache)
	}

	var jobs []webhook.DeliveryJob

	for _, user := range users {
		// a. Determine platform
		platform := platformFromType(user.Type)

		// b. Determine language
		language := user.Language
		if language == "" {
			language = r.locale
		}

		// c. Per-language enrichment
		perLang := mapOrEmpty(perLangEnrichment, language)

		// d. Per-user enrichment
		perUser := mapOrEmpty(perUserEnrichment, user.ID)

		// e. Build layered view (zero-copy — no map merging)
		view := NewLayeredView(r.viewBuilder, templateType, enrichment, perLang, perUser, webhookFields, platform, areas)

		// f. Get template (with monsterNoIv -> monster fallback)
		tmpl := r.templates.Get(templateType, platform, user.Template, language)
		if tmpl == nil && templateType == "monsterNoIv" {
			tmpl = r.templates.Get("monster", platform, user.Template, language)
		}

		var rendered string
		if tmpl == nil {
			rendered = fallbackMessage(templateType, platform, user.Template, language)
		} else {
			df := raymond.NewDataFrame()
			df.Set("language", language)
			df.Set("platform", platform)
			df.Set("altLanguage", "en")

			tStart := time.Now()
			result, err := tmpl.ExecWith(view, df)
			metrics.TemplateDuration.WithLabelValues(templateType).Observe(time.Since(tStart).Seconds())
			if err != nil {
				log.Errorf("dts: render %s for user %s: %v", templateType, user.ID, err)
				rendered = fallbackMessage(templateType, platform, user.Template, language)
				metrics.TemplateTotal.WithLabelValues(templateType, "error").Inc()
			} else {
				rendered = result
				metrics.TemplateTotal.WithLabelValues(templateType, "ok").Inc()
			}
		}

		// g. Post-process: shorten URLs
		rendered = ShortenMarkersWithCache(rendered, r.shortener, shlinkCache)

		// Validate rendered JSON
		rawMessage := json.RawMessage(rendered)
		if !json.Valid(rawMessage) {
			log.Errorf("dts: invalid rendered JSON for user %s (raw: %.200s)", user.ID, rendered)
			rawMessage = fallbackMessageRaw(templateType, platform, user.Template, language)
		}

		// Append ping to content
		if user.Ping != "" {
			rawMessage = appendPingToRaw(rawMessage, user.Ping)
		}

		// Extract emoji from view
		var emojiSlice []string
		if raw, ok := view.GetField("emoji"); ok {
			switch v := raw.(type) {
			case []string:
				emojiSlice = v
			case []any:
				for _, item := range v {
					if s, ok := item.(string); ok {
						emojiSlice = append(emojiSlice, s)
					}
				}
			}
		}

		// h. Build DeliveryJob
		jobs = append(jobs, webhook.DeliveryJob{
			Lat:          lat,
			Lon:          lon,
			Message:      rawMessage,
			Target:       user.ID,
			Type:         user.Type,
			Name:         user.Name,
			TTH:          tthMap,
			Clean:        user.Clean,
			Emoji:        emojiSlice,
			LogReference: logReference,
			Language:     language,
		})
	}

	return jobs
}

// renderGroupKey identifies a unique (template, platform, language) combination.
type renderGroupKey struct {
	templateID string
	platform   string
	language   string
}

// renderGrouped renders once per unique (template, platform, language) group and
// creates DeliveryJobs for all users in that group. This avoids redundant template
// execution and URL shortening when there is no per-user enrichment.
func (r *Renderer) renderGrouped(
	templateType string,
	enrichment map[string]any,
	perLangEnrichment map[string]map[string]any,
	webhookFields map[string]any,
	users []webhook.MatchedUser,
	areas []webhook.MatchedArea,
	logReference string,
	tthMap map[string]any,
	lat, lon string,
	shlinkCache map[string]string,
) []webhook.DeliveryJob {
	// Group users by rendering key
	type groupEntry struct {
		key   renderGroupKey
		users []webhook.MatchedUser
	}
	groupOrder := make([]renderGroupKey, 0, 4)
	groupMap := make(map[renderGroupKey]*groupEntry, 4)

	for _, user := range users {
		platform := platformFromType(user.Type)
		language := user.Language
		if language == "" {
			language = r.locale
		}
		key := renderGroupKey{templateID: user.Template, platform: platform, language: language}
		if g, ok := groupMap[key]; ok {
			g.users = append(g.users, user)
		} else {
			groupOrder = append(groupOrder, key)
			groupMap[key] = &groupEntry{key: key, users: []webhook.MatchedUser{user}}
		}
	}

	var jobs []webhook.DeliveryJob

	for _, key := range groupOrder {
		g := groupMap[key]

		// Render once for this group
		perLang := mapOrEmpty(perLangEnrichment, key.language)
		view := NewLayeredView(r.viewBuilder, templateType, enrichment, perLang, nil, webhookFields, key.platform, areas)

		tmpl := r.templates.Get(templateType, key.platform, key.templateID, key.language)

		var rendered string
		if tmpl == nil {
			rendered = fallbackMessage(templateType, key.platform, key.templateID, key.language)
		} else {
			df := raymond.NewDataFrame()
			df.Set("language", key.language)
			df.Set("platform", key.platform)
			df.Set("altLanguage", "en")

			tStart := time.Now()
			result, err := tmpl.ExecWith(view, df)
			metrics.TemplateDuration.WithLabelValues(templateType).Observe(time.Since(tStart).Seconds())
			if err != nil {
				log.Errorf("dts: render %s for group (%s/%s/%s): %v", templateType, key.platform, key.templateID, key.language, err)
				rendered = fallbackMessage(templateType, key.platform, key.templateID, key.language)
				metrics.TemplateTotal.WithLabelValues(templateType, "error").Inc()
			} else {
				rendered = result
				metrics.TemplateTotal.WithLabelValues(templateType, "ok").Inc()
			}
		}

		rendered = ShortenMarkersWithCache(rendered, r.shortener, shlinkCache)

		rawMessage := json.RawMessage(rendered)
		if !json.Valid(rawMessage) {
			log.Errorf("dts: invalid rendered JSON for group (%s/%s/%s) (raw: %.200s)", key.platform, key.templateID, key.language, rendered)
			rawMessage = fallbackMessageRaw(templateType, key.platform, key.templateID, key.language)
		}

		// Extract emoji once for the group
		var emojiSlice []string
		if raw, ok := view.GetField("emoji"); ok {
			switch v := raw.(type) {
			case []string:
				emojiSlice = v
			case []any:
				for _, item := range v {
					if s, ok := item.(string); ok {
						emojiSlice = append(emojiSlice, s)
					}
				}
			}
		}

		// Create a job for each user in the group
		for _, user := range g.users {
			// json.RawMessage is a []byte — shared safely when no ping.
			// For users with a ping, parse+modify+re-serialize.
			userMessage := rawMessage
			if user.Ping != "" {
				userMessage = appendPingToRaw(rawMessage, user.Ping)
			}

			jobs = append(jobs, webhook.DeliveryJob{
				Lat:          lat,
				Lon:          lon,
				Message:      userMessage,
				Target:       user.ID,
				Type:         user.Type,
				Name:         user.Name,
				TTH:          tthMap,
				Clean:        user.Clean,
				Emoji:        emojiSlice,
				LogReference: logReference,
				Language:     key.language,
			})
		}
	}

	return jobs
}

// cloneMessage creates a shallow copy of a map[string]any message so that
// mutations (like appendPing) don't affect the shared rendered object.
// Only top-level keys are copied; nested maps/slices are shared by reference.
// This is safe because appendPing only replaces the top-level "content" key.
func cloneMessage(msg any) any {
	if m, ok := msg.(map[string]any); ok {
		clone := make(map[string]any, len(m))
		for k, v := range m {
			clone[k] = v
		}
		return clone
	}
	return msg
}

// truncateCoord formats a coordinate as a string, truncated to 8 characters.
// lookupFloat finds a float value by key, checking enrichment first then webhookFields.
func lookupFloat(enrichment, webhookFields map[string]any, key string) float64 {
	if v, ok := enrichment[key]; ok {
		return toFloatValue(v)
	}
	if webhookFields != nil {
		if v, ok := webhookFields[key]; ok {
			return toFloatValue(v)
		}
	}
	return 0
}

func toFloatValue(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

func truncateCoord(f float64) string {
	s := fmt.Sprintf("%f", f)
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// platformFromType extracts the platform from a user type string.
// "discord:user" -> "discord", "telegram:group" -> "telegram", "webhook" -> "discord"
func platformFromType(userType string) string {
	parts := strings.SplitN(userType, ":", 2)
	platform := parts[0]
	if platform == "webhook" {
		return "discord"
	}
	return platform
}

// deduplicateUsers returns a slice with only the first occurrence of each user ID.
func deduplicateUsers(users []webhook.MatchedUser) []webhook.MatchedUser {
	seen := make(map[string]bool, len(users))
	var result []webhook.MatchedUser
	for _, u := range users {
		if !seen[u.ID] {
			seen[u.ID] = true
			result = append(result, u)
		}
	}
	return result
}

// extractTTH extracts the tth data from enrichment, returning a map for the delivery job
// and the total seconds for expiry checking. Handles both geo.TTH struct and map[string]any.
func extractTTH(enrichment map[string]any) (tthMap map[string]any, totalSeconds int) {
	raw, ok := enrichment["tth"]
	if !ok {
		return nil, 0
	}

	switch tth := raw.(type) {
	case geo.TTH:
		secs := tth.Days*86400 + tth.Hours*3600 + tth.Minutes*60 + tth.Seconds
		if tth.FirstDateWasLater {
			secs = 0
		}
		return map[string]any{
			"days": tth.Days, "hours": tth.Hours,
			"minutes": tth.Minutes, "seconds": tth.Seconds,
			"firstDateWasLater": tth.FirstDateWasLater,
		}, secs
	case *geo.TTH:
		if tth == nil {
			return nil, 0
		}
		secs := tth.Days*86400 + tth.Hours*3600 + tth.Minutes*60 + tth.Seconds
		if tth.FirstDateWasLater {
			secs = 0
		}
		return map[string]any{
			"days": tth.Days, "hours": tth.Hours,
			"minutes": tth.Minutes, "seconds": tth.Seconds,
			"firstDateWasLater": tth.FirstDateWasLater,
		}, secs
	case map[string]any:
		secs := 0
		if v, ok := tth["totalSeconds"]; ok {
			secs = int(toFloat(v))
		} else {
			secs = int(toFloat(tth["days"]))*86400 + int(toFloat(tth["hours"]))*3600 +
				int(toFloat(tth["minutes"]))*60 + int(toFloat(tth["seconds"]))
		}
		if b, ok := tth["firstDateWasLater"].(bool); ok && b {
			secs = 0
		}
		return tth, secs
	}
	return nil, 0
}

// mapOrEmpty returns the sub-map for the given key, or an empty map if not found.
func mapOrEmpty(m map[string]map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[key]; ok {
		return v
	}
	return nil
}

// fallbackMessage returns a JSON string for a fallback error message.
func fallbackMessage(templateType, platform, templateID, language string) string {
	msg := fmt.Sprintf("Template not found: %s/%s/%s/%s", templateType, platform, templateID, language)
	obj := map[string]string{"content": msg}
	b, _ := json.Marshal(obj)
	return string(b)
}

// fallbackMessageObject returns a map for a fallback error message.
func fallbackMessageObject(templateType, platform, templateID, language string) map[string]any {
	return map[string]any{
		"content": fmt.Sprintf("Template not found: %s/%s/%s/%s", templateType, platform, templateID, language),
	}
}

// appendPing appends a ping string to the message's "content" field.
func appendPing(message any, ping string) {
	if m, ok := message.(map[string]any); ok {
		if content, ok := m["content"].(string); ok {
			m["content"] = content + " " + ping
		} else {
			m["content"] = ping
		}
	}
}

// appendPingToRaw parses a JSON message, appends ping to "content", and re-serializes.
// If parsing fails, the original raw message is returned unchanged.
func appendPingToRaw(raw json.RawMessage, ping string) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	if content, ok := m["content"].(string); ok {
		m["content"] = content + " " + ping
	} else {
		m["content"] = ping
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// fallbackMessageRaw returns a json.RawMessage for a fallback error message.
func fallbackMessageRaw(templateType, platform, templateID, language string) json.RawMessage {
	obj := map[string]string{
		"content": fmt.Sprintf("Template not found: %s/%s/%s/%s", templateType, platform, templateID, language),
	}
	b, _ := json.Marshal(obj)
	return b
}

func mapKeys(m map[string]map[string]any) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
