package dts

import (
	"errors"
	"fmt"
	"strings"

	raymond "github.com/mailgun/raymond/v2"

	"github.com/pokemon/poracleng/processor/internal/buttons"
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
// Used by the host (cmd/processor) as the ResponseRender hook into
// internal/buttonactions — the package boundary keeps buttonactions free
// of raymond.
func (r *Renderer) RenderButtonResponse(def buttons.Def, view map[string]any, platform, language string) (string, error) {
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

func (r *Renderer) renderHandlebarsString(raw string, view map[string]any, platform, language string) (string, error) {
	tmpl, err := raymond.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse response template: %w", err)
	}
	return r.executeTemplate(tmpl, view, platform, language)
}

func (r *Renderer) executeTemplate(tmpl *raymond.Template, view map[string]any, platform, language string) (string, error) {
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

// inlineTemplateString flattens response_template_inline into a string.
// TOML/JSON arrays-of-strings get newline-joined (mirrors the existing
// description handling); raw strings pass through; everything else
// fails since we can't safely render an arbitrary nested map as a
// Handlebars template body.
func inlineTemplateString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, "\n"), true
	}
	return "", false
}
