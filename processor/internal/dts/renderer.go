package dts

import (
	"encoding/json"
	"fmt"
	"strings"

	raymond "github.com/mailgun/raymond/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// DeliveryJob represents a rendered alert ready for delivery.
type DeliveryJob struct {
	Lat          string         `json:"lat"`
	Lon          string         `json:"lon"`
	Message      any            `json:"message"`     // parsed JSON (map or string)
	Target       string         `json:"target"`
	Type         string         `json:"type"`         // "discord:user", "telegram:group", etc.
	Name         string         `json:"name"`
	TTH          map[string]any `json:"tth"`
	Clean        bool           `json:"clean"`
	Emoji        []string       `json:"emoji"`
	LogReference string         `json:"logReference"`
	Language     string         `json:"language"`
}

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

// RenderPokemon renders pokemon alerts for all matched users and returns delivery jobs.
func (r *Renderer) RenderPokemon(
	enrichment map[string]any,
	perLangEnrichment map[string]map[string]any,
	perUserEnrichment map[string]map[string]any,
	matchedUsers []webhook.MatchedUser,
	matchedAreas []webhook.MatchedArea,
	isEncountered bool,
	logReference string,
) []DeliveryJob {
	// 1. Check TTH
	tthMap := extractTTH(enrichment)
	if r.minAlertSec > 0 {
		if secs, ok := toTTHSeconds(tthMap); ok && secs < r.minAlertSec {
			return nil
		}
	}

	// 2. Deduplicate users: keep first occurrence per user ID
	uniqueUsers := deduplicateUsers(matchedUsers)

	// 3. Extract lat/lon for jobs
	lat := truncateCoord(toFloat(enrichment["latitude"]))
	lon := truncateCoord(toFloat(enrichment["longitude"]))

	var jobs []DeliveryJob

	for _, user := range uniqueUsers {
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

		// e. Build view
		view := r.viewBuilder.BuildPokemonView(enrichment, perLang, perUser, platform, matchedAreas)

		// f. Select template type
		templateType := "monster"
		if !isEncountered {
			templateType = "monsterNoIv"
		}

		// g. Get template
		tmpl := r.templates.Get(templateType, platform, user.Template, language)
		if tmpl == nil {
			// Try fallback to "monster" if monsterNoIv not found
			if templateType == "monsterNoIv" {
				tmpl = r.templates.Get("monster", platform, user.Template, language)
			}
		}

		var rendered string
		if tmpl == nil {
			// h. Fallback error message
			rendered = fallbackMessage(templateType, platform, user.Template, language)
		} else {
			// i. Set up data frame with language/platform for helpers
			df := raymond.NewDataFrame()
			df.Set("language", language)
			df.Set("platform", platform)
			df.Set("altLanguage", "en")

			result, err := tmpl.ExecWith(view, df)
			if err != nil {
				log.Errorf("dts: render %s for user %s: %v", templateType, user.ID, err)
				rendered = fallbackMessage(templateType, platform, user.Template, language)
			} else {
				rendered = result
			}
		}

		// j. Post-process: shorten URLs
		rendered = ShortenMarkers(rendered, r.shortener)

		// Parse JSON result
		var message any
		if err := json.Unmarshal([]byte(rendered), &message); err != nil {
			log.Errorf("dts: parse rendered JSON for user %s: %v (raw: %.200s)", user.ID, err, rendered)
			message = fallbackMessageObject(templateType, platform, user.Template, language)
		}

		// Append ping to content
		if user.Ping != "" {
			appendPing(message, user.Ping)
		}

		// Extract emoji from view
		var emojiSlice []string
		if raw, ok := view["emoji"]; ok {
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

		// k. Build DeliveryJob
		jobs = append(jobs, DeliveryJob{
			Lat:          lat,
			Lon:          lon,
			Message:      message,
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

// truncateCoord formats a coordinate as a string, truncated to 8 characters.
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

// extractTTH extracts the tth map from enrichment.
func extractTTH(enrichment map[string]any) map[string]any {
	if raw, ok := enrichment["tth"]; ok {
		if m, ok := raw.(map[string]any); ok {
			return m
		}
	}
	return nil
}

// toTTHSeconds extracts the total seconds from a tth map.
func toTTHSeconds(tth map[string]any) (int, bool) {
	if tth == nil {
		return 0, false
	}
	if v, ok := tth["totalSeconds"]; ok {
		return int(toFloat(v)), true
	}
	return 0, false
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
