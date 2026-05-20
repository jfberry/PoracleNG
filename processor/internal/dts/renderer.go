package dts

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	raymond "github.com/mailgun/raymond/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// RendererConfig holds configuration for creating a Renderer.
type RendererConfig struct {
	ConfigDir           string
	FallbackDir         string
	GameData            *gamedata.GameData
	Translations        *i18n.Bundle
	UtilEmojis          map[string]string // from GameData.Util.Emojis
	ShlinkURL           string            // empty = no shortening
	ShlinkKey           string
	ShlinkDomain        string
	DTSDictionary       map[string]any // from config [general] dts_dictionary
	DefaultLocale       string         // fallback language (e.g. "en")
	AltLanguage         string         // alt language for *Alt helpers (default "en")
	DefaultTemplateName string         // from config default_template_name (e.g. "1")
	MinAlertTime        int            // minimum seconds remaining for alert
}

// Renderer ties together templates, enrichment, emoji, and URL shortening
// to produce DeliveryJobs from matched webhook data.
type Renderer struct {
	templates       *TemplateStore
	viewBuilder     *ViewBuilder
	shortener       *ShlinkShortener // nil if not configured
	gd              *gamedata.GameData
	bundle          *i18n.Bundle
	emoji           *EmojiLookup
	locale          string
	defaultTemplate string // config default_template_name, used when tracking has no explicit template
	altLanguage     string
	minAlertSec     int

	// errorNoticer is an optional callback the renderer fires when a
	// template render fails (per-user, per-group, or by template panic).
	// Set by the host (cmd/processor) to route errors to the admin
	// channel; nil means errors are only logged. The callback receives a
	// stable key (suitable for throttling) and the human-readable
	// message.
	errorNoticer ErrorNoticer

	// buttonsEnabled gates Discord component emission. When false the
	// renderer doesn't attempt to attach buttons even if the resolved
	// entry has them — equivalent to [snapshots] disabled in the host,
	// because buttons require a snapshot at click time.
	buttonsEnabled bool
}

// SetButtonsEnabled toggles Discord button component emission. Buttons
// require a snapshot at click time, so the host wires this from the
// [snapshots] config: true only when the snapshot store is open.
//
// Operators can leave Buttons[] in their DTS entries when snapshots are
// disabled — the loader still validates them, but the renderer just
// doesn't emit the components block.
func (r *Renderer) SetButtonsEnabled(v bool) {
	r.buttonsEnabled = v
}

// ErrorNoticer routes renderer errors to a host-side notification sink
// (typically PostAdminNoticeThrottled on the discord bot). Non-blocking;
// errors are logged regardless of whether the noticer is set.
type ErrorNoticer func(key, msg string)

// SetErrorNoticer wires a host-side callback to receive per-render-error
// notifications. Pass nil to disable. Safe to call before or after the
// bot is up; the renderer just stores the callback.
func (r *Renderer) SetErrorNoticer(fn ErrorNoticer) {
	r.errorNoticer = fn
}

// notice fires the host-side noticer if one is set. Always returns
// quickly; the host is expected to throttle internally. No-op when
// errorNoticer is nil.
func (r *Renderer) notice(key, msg string) {
	if r.errorNoticer != nil {
		r.errorNoticer(key, msg)
	}
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
	altLang := cfg.AltLanguage
	if altLang == "" {
		altLang = "en"
	}

	// Tell the template store which locale to prefer when a user's
	// language isn't shipped (step 5 of the selection chain). Without
	// this, legacy users with an untranslated language (e.g. lang="nl"
	// with no NL templates) silently fall to whichever default entry
	// loaded last — often DE rather than the operator's chosen EN.
	ts.SetDefaultLocale(locale)

	return &Renderer{
		templates:       ts,
		viewBuilder:     vb,
		shortener:       shortener,
		gd:              cfg.GameData,
		bundle:          cfg.Translations,
		emoji:           emoji,
		locale:          locale,
		altLanguage:     altLang,
		defaultTemplate: cfg.DefaultTemplateName,
		minAlertSec:     cfg.MinAlertTime,
	}, nil
}

// resolveTemplate returns the template ID to use for DTS lookup.
// If the tracking rule has no explicit template (empty string), the
// configured default_template_name is used first. If that is also empty,
// an empty string is returned which causes Get() to fall through to
// the DTS entry marked as default.
func (r *Renderer) resolveTemplate(trackingTemplate string) string {
	if trackingTemplate != "" {
		return trackingTemplate
	}
	return r.defaultTemplate
}

// ResolveTemplate returns the template ID to use, applying the default if empty.
func (r *Renderer) ResolveTemplate(trackingTemplate string) string {
	return r.resolveTemplate(trackingTemplate)
}

// CheckTemplate validates that a template can be found for the given parameters.
// Returns nil if a template exists, or an error describing what's missing.
func (r *Renderer) CheckTemplate(templateType, platform, templateID, language string) error {
	resolvedID := r.resolveTemplate(templateID)
	tmpl := r.templates.Get(templateType, platform, resolvedID, language)
	if tmpl != nil {
		return nil
	}
	// Try monsterNoIv → monster fallback
	if templateType == "monsterNoIv" {
		tmpl = r.templates.Get("monster", platform, resolvedID, language)
		if tmpl != nil {
			return nil
		}
	}
	if resolvedID == "" {
		return fmt.Errorf("no DTS template found for type=%q platform=%q (no default template configured)", templateType, platform)
	}
	return fmt.Errorf("no DTS template found for type=%q platform=%q template=%q language=%q", templateType, platform, resolvedID, language)
}

// Templates returns the underlying TemplateStore.
func (r *Renderer) Templates() *TemplateStore { return r.templates }

// Emoji returns the emoji lookup used by this renderer.
func (r *Renderer) Emoji() *EmojiLookup { return r.emoji }

// Shortener returns the URL shortener (nil if not configured).
func (r *Renderer) Shortener() *ShlinkShortener { return r.shortener }

// ViewBuilder returns the view builder used for LayeredView construction.
func (r *Renderer) ViewBuilder() *ViewBuilder { return r.viewBuilder }

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
	editKeyBase string,
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

	return r.renderForUsers(templateType, enrichment, perLangEnrichment, perUserEnrichment, webhookFields, nil, uniqueUsers, matchedAreas, logReference, editKeyBase)
}

// RenderPokemonChanged renders the monsterChanged template for pokemon-change
// events. The original parameter is the prior-sighting view (built by
// BuildOriginalView) — exposed in templates as {{original.X}}. Like
// RenderPokemon, matched users are deduplicated; unlike RenderPokemon the
// template type is fixed to "monsterChanged" (there is no encounter/no-IV
// distinction here — the change event always implies the new sighting was
// encountered enough to detect the change).
func (r *Renderer) RenderPokemonChanged(
	enrichment map[string]any,
	perLangEnrichment map[string]map[string]any,
	perUserEnrichment map[string]map[string]any,
	webhookFields map[string]any,
	original map[string]any,
	matchedUsers []webhook.MatchedUser,
	matchedAreas []webhook.MatchedArea,
	logReference string,
	editKeyBase string,
) []webhook.DeliveryJob {
	if r.isBelowMinAlertTime(enrichment) {
		return nil
	}

	uniqueUsers := deduplicateUsers(matchedUsers)

	return r.renderForUsers("monsterChanged", enrichment, perLangEnrichment, perUserEnrichment, webhookFields, original, uniqueUsers, matchedAreas, logReference, editKeyBase)
}

// RenderQuestSummary renders a single grouped quest-summary message for a
// reward bucket. Unlike RenderQuest, the view passed in is already fully
// composed (base + per-language fields merged per pokestop, plus shared
// reward fields and the autopositioned multi-pin static map URL) — see
// BuildQuestSummaryView. The matched-users slice is the recipient(s) of
// the summary, typically the single owner of the schedule.
//
// The summary template path doesn't run TTH gating; the caller has
// already filtered expired buffered quests before composing the view.
// The view is consumed as-is via the LayeredView's `base` layer.
func (r *Renderer) RenderQuestSummary(
	view map[string]any,
	matchedUsers []webhook.MatchedUser,
	matchedAreas []webhook.MatchedArea,
	logReference string,
	editKeyBase string,
) []webhook.DeliveryJob {
	return r.renderForUsers("questSummary", view, nil, nil, nil, nil, matchedUsers, matchedAreas, logReference, editKeyBase)
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
	editKeyBase string,
) []webhook.DeliveryJob {
	if r.isBelowMinAlertTime(enrichment) {
		return nil
	}

	return r.renderForUsers(templateType, enrichment, perLangEnrichment, nil, webhookFields, nil, matchedUsers, matchedAreas, logReference, editKeyBase)
}

// isBelowMinAlertTime checks whether the TTH in enrichment is below the configured minimum.
func (r *Renderer) isBelowMinAlertTime(enrichment map[string]any) bool {
	_, tthSeconds := extractTTH(enrichment)
	return r.minAlertSec > 0 && tthSeconds > 0 && tthSeconds < r.minAlertSec
}

// renderForUsers is the shared rendering loop that produces DeliveryJobs for each user.
// The original parameter (nil for non-change renders) is the prior-sighting snapshot
// installed onto each LayeredView so templates can reference {{original.X}}.
func (r *Renderer) renderForUsers(
	templateType string,
	enrichment map[string]any,
	perLangEnrichment map[string]map[string]any,
	perUserEnrichment map[string]map[string]any,
	webhookFields map[string]any,
	original map[string]any,
	users []webhook.MatchedUser,
	areas []webhook.MatchedArea,
	logReference string,
	editKeyBase string,
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
		return r.renderGrouped(templateType, enrichment, perLangEnrichment, webhookFields, original, users, areas, logReference, tthMap, lat, lon, shlinkCache, editKeyBase)
	}

	var jobs []webhook.DeliveryJob

	for _, user := range users {
		// a. Determine platform
		platform := delivery.PlatformFromType(user.Type)

		// b. Determine language
		language := user.Language
		if language == "" {
			language = r.locale
		}

		// c. Per-language enrichment
		perLang := mapOrEmpty(perLangEnrichment, language)

		// d. Per-user enrichment
		perUser := mapOrEmpty(perUserEnrichment, user.ID)

		// e. Build layered view (zero-copy — no map merging). Install the
		// prior-sighting snapshot (nil for non-change renders) so templates
		// can reference {{original.X}}.
		view := NewLayeredView(r.viewBuilder, templateType, enrichment, perLang, perUser, webhookFields, platform, areas)
		view.original = original

		// f. Get template (with monsterNoIv -> monster fallback)
		templateID := r.resolveTemplate(user.Template)
		tmpl := r.templates.Get(templateType, platform, templateID, language)
		if tmpl == nil && templateType == "monsterNoIv" {
			tmpl = r.templates.Get("monster", platform, templateID, language)
		}

		var rendered string
		if tmpl == nil {
			rendered = fallbackMessage(templateType, platform, templateID, language)
		} else {
			df := raymond.NewDataFrame()
			df.Set("language", language)
			df.Set("platform", platform)
			df.Set("altLanguage", r.altLanguage)

			tStart := time.Now()
			result, err := safeExecWith(tmpl, view, df)
			metrics.TemplateDuration.WithLabelValues(templateType).Observe(time.Since(tStart).Seconds())
			if err != nil {
				log.Errorf("dts: render %s for user %s: %v", templateType, user.ID, err)
				r.notice(
					fmt.Sprintf("dts.render:%s:%s:%s:%s", templateType, platform, language, templateID),
					fmt.Sprintf(":warning: DTS template `%s/%s/%s/%s` render error: %v — falling back to default message.", templateType, platform, language, templateID, err),
				)
				rendered = fallbackMessage(templateType, platform, templateID, language)
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
			r.notice(
				fmt.Sprintf("dts.invalid:%s:%s:%s:%s", templateType, platform, language, templateID),
				fmt.Sprintf(":warning: DTS template `%s/%s/%s/%s` produced invalid JSON — falling back to default message.", templateType, platform, language, templateID),
			)
			rawMessage = fallbackMessageRaw(templateType, platform, templateID, language)
		}

		// Append ping to content
		if user.Ping != "" {
			rawMessage = appendPingToRaw(rawMessage, user.Ping)
		}

		// Attach interactive button components (#109). Only Discord
		// supports the component shape we emit; Telegram clicks would
		// need a different bytes path and are deferred (#112).
		if r.buttonsEnabled && platform == "discord" {
			defs := r.templates.GetButtons(templateType, platform, templateID, language)
			if len(defs) > 0 {
				rawMessage = InjectDiscordComponents(rawMessage, defs, view, deliveryTargetType(user.Type), r.evalShowIf)
			}
		}

		emojiSlice := extractEmojiSlice(view)

		// h. Compute edit key
		editKey := ""
		if db.IsEdit(user.Clean) && editKeyBase != "" {
			editKey = editKeyBase + ":" + user.ID
		}

		// i. Build DeliveryJob
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
			EditKey:      editKey,
		})
	}

	return jobs
}

// evalShowIf compiles and runs a button's show_if expression against the
// resolved view. Truthy means attach the button; falsy means drop it.
//
// The expression is wrapped in `{{...}}` so operators can write either
// `{{hasPVP}}` or just `hasPVP`. Output is considered truthy when
// non-empty AND not literally "false" / "0". This matches Handlebars'
// own truthiness model for {{#if}}.
//
// Errors are surfaced to the caller (InjectDiscordComponents) which
// logs them and drops the button — silent acceptance would attach
// buttons the operator didn't intend.
func (r *Renderer) evalShowIf(expr string, view any) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true, nil
	}
	tmpl := expr
	if !strings.HasPrefix(tmpl, "{{") {
		tmpl = "{{" + tmpl + "}}"
	}
	parsed, err := raymond.Parse(tmpl)
	if err != nil {
		return false, fmt.Errorf("parse show_if %q: %w", expr, err)
	}
	out, err := safeExecWith(parsed, view, raymond.NewDataFrame())
	if err != nil {
		return false, fmt.Errorf("exec show_if %q: %w", expr, err)
	}
	out = strings.TrimSpace(out)
	return out != "" && out != "false" && out != "0", nil
}

// deliveryTargetType maps a webhook user.Type ("discord:user", etc.) to
// the short noun the button schema uses for applies_to: "dm" / "channel"
// / "webhook". Mirrors the helper in cmd/processor; duplicated here to
// avoid an import cycle.
func deliveryTargetType(userType string) string {
	switch userType {
	case "discord:user", "telegram:user":
		return "dm"
	case "discord:channel", "discord:thread", "telegram:group", "telegram:channel":
		return "channel"
	case "webhook":
		return "webhook"
	default:
		return ""
	}
}

// renderGroupKey identifies a unique (template, platform, language) combination.
type renderGroupKey struct {
	templateID    string
	platform      string
	language      string
	distanceTrack bool
}

// renderGrouped renders once per unique (template, platform, language) group and
// creates DeliveryJobs for all users in that group. This avoids redundant template
// execution and URL shortening when there is no per-user enrichment.
// The original parameter (nil for non-change renders) is installed on each
// LayeredView so templates can reference {{original.X}}.
func (r *Renderer) renderGrouped(
	templateType string,
	enrichment map[string]any,
	perLangEnrichment map[string]map[string]any,
	webhookFields map[string]any,
	original map[string]any,
	users []webhook.MatchedUser,
	areas []webhook.MatchedArea,
	logReference string,
	tthMap map[string]any,
	lat, lon string,
	shlinkCache map[string]string,
	editKeyBase string,
) []webhook.DeliveryJob {
	// Group users by rendering key
	type groupEntry struct {
		key   renderGroupKey
		users []webhook.MatchedUser
	}
	groupOrder := make([]renderGroupKey, 0, 4)
	groupMap := make(map[renderGroupKey]*groupEntry, 4)

	for _, user := range users {
		platform := delivery.PlatformFromType(user.Type)
		language := user.Language
		if language == "" {
			language = r.locale
		}
		key := renderGroupKey{
			templateID:    r.resolveTemplate(user.Template),
			platform:      platform,
			language:      language,
			distanceTrack: user.TrackDistance > 0,
		}
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

		// Render once for this group. Distance-track flag is shared by every
		// user in the group (it's part of the group key), so we can stash it
		// in a tiny perUser map and let {{userDistanceTrack}} resolve from
		// the LayeredView's perUser layer.
		perLang := mapOrEmpty(perLangEnrichment, key.language)
		groupPerUser := map[string]any{"userDistanceTrack": key.distanceTrack}
		view := NewLayeredView(r.viewBuilder, templateType, enrichment, perLang, groupPerUser, webhookFields, key.platform, areas)
		view.original = original

		tmpl := r.templates.Get(templateType, key.platform, key.templateID, key.language)

		var rendered string
		if tmpl == nil {
			rendered = fallbackMessage(templateType, key.platform, key.templateID, key.language)
		} else {
			df := raymond.NewDataFrame()
			df.Set("language", key.language)
			df.Set("platform", key.platform)
			df.Set("altLanguage", r.altLanguage)

			tStart := time.Now()
			result, err := safeExecWith(tmpl, view, df)
			metrics.TemplateDuration.WithLabelValues(templateType).Observe(time.Since(tStart).Seconds())
			if err != nil {
				log.Errorf("dts: render %s for group (%s/%s/%s): %v", templateType, key.platform, key.templateID, key.language, err)
				r.notice(
					fmt.Sprintf("dts.render:%s:%s:%s:%s", templateType, key.platform, key.language, key.templateID),
					fmt.Sprintf(":warning: DTS template `%s/%s/%s/%s` render error: %v — falling back to default message.", templateType, key.platform, key.language, key.templateID, err),
				)
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
			r.notice(
				fmt.Sprintf("dts.invalid:%s:%s:%s:%s", templateType, key.platform, key.language, key.templateID),
				fmt.Sprintf(":warning: DTS template `%s/%s/%s/%s` produced invalid JSON — falling back to default message.", templateType, key.platform, key.language, key.templateID),
			)
			rawMessage = fallbackMessageRaw(templateType, key.platform, key.templateID, key.language)
		}

		emojiSlice := extractEmojiSlice(view)

		// Create a job for each user in the group
		for _, user := range g.users {
			// json.RawMessage is a []byte — shared safely when no ping.
			// For users with a ping, parse+modify+re-serialize.
			userMessage := rawMessage
			if user.Ping != "" {
				userMessage = appendPingToRaw(rawMessage, user.Ping)
			}

			editKey := ""
			if db.IsEdit(user.Clean) && editKeyBase != "" {
				editKey = editKeyBase + ":" + user.ID
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
				EditKey:      editKey,
			})
		}
	}

	return jobs
}

// lookupFloat finds a float value by key, checking enrichment first then webhookFields.
func lookupFloat(enrichment, webhookFields map[string]any, key string) float64 {
	if v, ok := enrichment[key]; ok {
		return toFloat(v)
	}
	if webhookFields != nil {
		if v, ok := webhookFields[key]; ok {
			return toFloat(v)
		}
	}
	return 0
}

func truncateCoord(f float64) string {
	s := fmt.Sprintf("%f", f)
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// deduplicateUsers returns a slice with only the first occurrence of each user ID.
// extractEmojiSlice gets the emoji array from a LayeredView, handling both []string and []any.
func extractEmojiSlice(view *LayeredView) []string {
	raw, ok := view.GetField("emoji")
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		var result []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

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

// safeExecWith wraps raymond Template.ExecWith with panic recovery.
// Malformed templates can cause panics in raymond (e.g. nil Options in helpers).
// This converts those panics into errors so a bad template doesn't crash the process.
func safeExecWith(tmpl *raymond.Template, ctx any, df *raymond.DataFrame) (result string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("template panic: %v", r)
		}
	}()
	return tmpl.ExecWith(ctx, df)
}
