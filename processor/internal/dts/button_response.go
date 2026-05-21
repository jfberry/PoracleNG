package dts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	raymond "github.com/mailgun/raymond/v2"

	"github.com/pokemon/poracleng/processor/internal/buttons"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// RenderButtonResponse compiles + executes the response payload of a
// click — exactly one of:
//
//   - def.ResponseText        → plain-text Handlebars output against view
//   - def.ResponseTemplateInline → JSON/embed Handlebars rendered like a normal template
//   - def.ResponseTemplateID  → DTS lookup against type="buttonResponse"
//
// Returns the rendered string (Discord-message JSON or plain text) ready
// for the ephemeral interaction reply.
//
// view is a *LayeredView built by the caller (typically via
// BuildLayeredViewFromSnapshot). raymond accepts it as the data
// argument because LayeredView implements FieldResolver — and going
// through LayeredView means aliases, computed fields, and emoji
// resolution work identically to the original alert render.
func (r *Renderer) RenderButtonResponse(def buttons.Def, view *LayeredView, platform, language string) (string, error) {
	switch def.DispatchMode() {
	case buttons.ModeResponseText:
		return r.renderHandlebarsString(def.ResponseText, view, platform, language)
	case buttons.ModeResponseTemplateInline:
		raw, ok := inlineTemplateString(def.ResponseTemplateInline)
		if !ok {
			return "", errors.New("response_template_inline must be a string or JSON-marshalable object")
		}
		return r.renderHandlebarsString(raw, view, platform, language)
	case buttons.ModeResponseTemplateID:
		tmpl := r.templates.Get("buttonResponse", platform, def.ResponseTemplateID, language)
		if tmpl == nil {
			return "", fmt.Errorf("buttonResponse template %q not found for platform=%s lang=%s",
				def.ResponseTemplateID, platform, language)
		}
		return r.executeTemplate(tmpl, view, platform, language)
	default:
		return "", fmt.Errorf("button has no response payload")
	}
}

func (r *Renderer) renderHandlebarsString(raw string, view *LayeredView, platform, language string) (string, error) {
	tmpl, err := raymond.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse response template: %w", err)
	}
	return r.executeTemplate(tmpl, view, platform, language)
}

func (r *Renderer) executeTemplate(tmpl *raymond.Template, view *LayeredView, platform, language string) (string, error) {
	df := raymond.NewDataFrame()
	df.Set("language", language)
	df.Set("platform", platform)
	df.Set("altLanguage", r.altLanguage)
	out, err := safeExecWith(tmpl, view, df)
	if err != nil {
		return "", fmt.Errorf("render response: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// BuildLayeredViewFromSnapshot reconstructs a LayeredView from a click-
// time Snapshot. Used by the host (discordbot) when wiring the
// ResponseRender hook — we need the renderer's view machinery available
// without holding the original render-time inputs.
//
// templateType comes from the snapshot rather than being inferred
// because some webhook types (raid → rsvpChanges) use a template type
// distinct from the alert type.
func (r *Renderer) BuildLayeredViewFromSnapshot(snap *snapshots.Snapshot) *LayeredView {
	if snap == nil {
		return nil
	}
	areas := make([]webhook.MatchedArea, 0, len(snap.MatchedAreas))
	for _, n := range snap.MatchedAreas {
		areas = append(areas, webhook.MatchedArea{Name: n})
	}
	return NewLayeredView(
		r.viewBuilder,
		snap.TemplateType,
		snap.Enrichment,
		snap.PerLang,
		snap.PerUser,
		snap.WebhookFields,
		snap.Platform,
		areas,
	)
}

// inlineTemplateString flattens response_template_inline into a string.
// Mirrors the main `template` field's accepted shapes so the editor's
// Form mode (which produces an object) round-trips through the same
// path the alert renderer uses:
//
//   - string             → used as raw Handlebars source.
//   - []any of strings   → newline-joined (same as description arrays).
//   - map[string]any /
//     []any of anything  → JSON-stringified with HTML escaping off so
//                          Handlebars expressions containing `<` / `>` /
//                          `&` survive intact.
func inlineTemplateString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case []any:
		// Pure string array → join. Mixed-type array falls through to
		// JSON-marshal below so non-string content is preserved.
		if pureStrings, joined := joinIfAllStrings(t); pureStrings {
			return joined, true
		}
	case map[string]any:
		// fallthrough to JSON marshal
	default:
		return "", false
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", false
	}
	return strings.TrimSpace(buf.String()), true
}

func joinIfAllStrings(items []any) (bool, string) {
	parts := make([]string, 0, len(items))
	for _, e := range items {
		s, ok := e.(string)
		if !ok {
			return false, ""
		}
		parts = append(parts, s)
	}
	return true, strings.Join(parts, "\n")
}
