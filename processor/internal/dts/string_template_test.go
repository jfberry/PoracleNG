package dts

import (
	"strings"
	"testing"
)

// TestResolveTemplate_StringValueIsRawHandlebars covers the bug where
// a TOML-loaded entry's template field (a string holding the JSON body)
// was being JSON-encoded a second time, producing a quoted-string
// instead of a JSON object at the wire. The fix: when entry.Template
// is a string, treat it as raw Handlebars source — same semantics as
// templateFile.
//
// Symptom in the field:
//
//	delivery: send failed: normalizing message: parsing discord message:
//	json: cannot unmarshal string into Go value of type map[string]interface{}
func TestResolveTemplate_StringValueIsRawHandlebars(t *testing.T) {
	entry := DTSEntry{
		Type:     "monster",
		ID:       "1",
		Platform: "discord",
		Language: "en",
		// The natural TOML form: `template = """..."""`.
		Template: `{
  "embed": {
    "title": "{{name}}"
  }
}`,
	}

	body, err := resolveTemplate(entry, t.TempDir())
	if err != nil {
		t.Fatalf("resolveTemplate: %v", err)
	}
	// Should be the raw JSON-object body (starts with `{`), not a JSON
	// string literal (would start with `"`).
	if !strings.HasPrefix(body, "{") {
		t.Errorf("body should start with '{' (raw JSON object), got: %q", body[:min(80, len(body))])
	}
	if strings.HasPrefix(body, `"`) {
		t.Errorf("body was double-encoded as a JSON string literal: %q", body[:min(80, len(body))])
	}
	// And the Handlebars expression survives intact.
	if !strings.Contains(body, "{{name}}") {
		t.Errorf("body lost the {{name}} expression: %q", body)
	}
}

// TestResolveTemplate_ObjectValueIsJSONEncoded confirms the JSON-DTS
// path still produces a JSON object (regression guard for the fix above).
func TestResolveTemplate_ObjectValueIsJSONEncoded(t *testing.T) {
	entry := DTSEntry{
		Type:     "monster",
		ID:       "1",
		Platform: "discord",
		Language: "en",
		Template: map[string]any{
			"embed": map[string]any{
				"title": "{{name}}",
			},
		},
	}

	body, err := resolveTemplate(entry, t.TempDir())
	if err != nil {
		t.Fatalf("resolveTemplate: %v", err)
	}
	if !strings.HasPrefix(body, "{") {
		t.Errorf("body should start with '{', got: %q", body[:min(80, len(body))])
	}
	if !strings.Contains(body, `"embed"`) {
		t.Errorf("body missing \"embed\" key: %q", body)
	}
	if !strings.Contains(body, "{{name}}") {
		t.Errorf("body lost the {{name}} expression: %q", body)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
