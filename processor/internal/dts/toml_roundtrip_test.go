package dts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/buttons"
)

// TestEncodeTOML_RoundTrip writes a TOML file, loads it, re-encodes,
// and checks the re-encoded form still parses cleanly into the same
// shape. Comments are not preserved (acknowledged Option-C tradeoff);
// the test checks structural equivalence only.
func TestEncodeTOML_RoundTrip(t *testing.T) {
	source := []DTSEntry{
		{
			Type:        "raid",
			ID:          "1",
			Platform:    "discord",
			Language:    "en",
			Description: "Raid card",
			Template:    "{\n  \"embed\": {\n    \"title\": \"{{name}}\"\n  }\n}\n",
			Buttons: []buttons.Def{
				{
					ID:     "mute_gym_1h",
					Label:  "Mute this gym (1h)",
					Style:  buttons.StyleDanger,
					Action: buttons.ActionMute,
					Scope:  buttons.ScopeGym,
					Params: map[string]any{"duration_min": int64(60)},
				},
			},
		},
		{
			Type:     "raid",
			ID:       "compact",
			Platform: "discord",
			Language: "en",
			Template: `{"content":"compact"}`,
		},
	}

	encoded, err := encodeTOML(source)
	if err != nil {
		t.Fatalf("encodeTOML: %v", err)
	}
	// Sanity: emitted bytes contain the expected markers.
	if !strings.Contains(string(encoded), "[[entry]]") {
		t.Errorf("encoded TOML missing [[entry]] block: %q", encoded)
	}
	if !strings.Contains(string(encoded), "[[entry.buttons]]") {
		t.Errorf("encoded TOML missing [[entry.buttons]] block: %q", encoded)
	}
	// Multi-line template bodies must use triple-quoted strings rather
	// than single-line escaped form — operators saving via the editor
	// shouldn't see their templates collapsed into one line of escapes.
	if !strings.Contains(string(encoded), "template = \"\"\"") {
		t.Errorf("multi-line template not emitted as triple-quoted block:\n%s", encoded)
	}
	if strings.Contains(string(encoded), `\"embed\"`) {
		t.Errorf("multi-line template body still contains JSON-escapes (single-line form leaked):\n%s", encoded)
	}

	// Write + reload to confirm the wire format round-trips.
	dir := t.TempDir()
	path := filepath.Join(dir, "raid.toml")
	if err := os.WriteFile(path, encoded, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	reloaded, err := loadTOMLFile(path)
	if err != nil {
		t.Fatalf("loadTOMLFile: %v", err)
	}
	if len(reloaded) != len(source) {
		t.Fatalf("got %d entries after round-trip, want %d", len(reloaded), len(source))
	}
	if reloaded[0].Type != "raid" || reloaded[0].ID.String() != "1" {
		t.Errorf("first entry meta after reload: %+v", reloaded[0])
	}
	if len(reloaded[0].Buttons) != 1 || reloaded[0].Buttons[0].ID != "mute_gym_1h" {
		t.Errorf("button lost in round-trip: %+v", reloaded[0].Buttons)
	}
	// Body must round-trip byte-for-byte — operators saving from the
	// editor get back what they wrote, not the same content with
	// trailing whitespace bolted on by the closing-fence indent.
	originalBody := source[0].Template.(string)
	reloadedBody, ok := reloaded[0].Template.(string)
	if !ok {
		t.Fatalf("reloaded template is %T, want string", reloaded[0].Template)
	}
	if reloadedBody != originalBody {
		t.Errorf("template body changed across round-trip:\noriginal: %q\nreloaded: %q", originalBody, reloadedBody)
	}
}

// TestEncodeTOML_ObjectInlineTemplate covers the response_template_inline
// object-shaped path. JSON-stringified objects must not pick up a per-
// line prefix on encode — the editor's Form mode sends an object on
// save, and a stray prefix would silently inject leading whitespace
// into the parsed body next load.
func TestEncodeTOML_ObjectInlineTemplate(t *testing.T) {
	source := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Language: "en",
			Template: `{"content":"ok"}`,
			Buttons: []buttons.Def{
				{
					ID:    "details",
					Label: "Details",
					ResponseTemplateInline: map[string]any{
						"embed": map[string]any{
							"title": "Details for {{name}}",
						},
					},
				},
			},
		},
	}
	encoded, err := encodeTOML(source)
	if err != nil {
		t.Fatalf("encodeTOML: %v", err)
	}
	// The opening `{` of the marshalled body must be at column 0, not
	// indented — a per-line prefix bug would push it to "  {".
	got := string(encoded)
	if !strings.Contains(got, "response_template_inline = \"\"\"\n{\n") {
		t.Errorf("response_template_inline body has leading whitespace on opening brace:\n%s", got)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "monster.toml")
	if err := os.WriteFile(path, encoded, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	reloaded, err := loadTOMLFile(path)
	if err != nil {
		t.Fatalf("loadTOMLFile: %v", err)
	}
	body, ok := reloaded[0].Buttons[0].ResponseTemplateInline.(string)
	if !ok {
		t.Fatalf("reloaded inline is %T, want string", reloaded[0].Buttons[0].ResponseTemplateInline)
	}
	// First non-space character of the body must be `{` at column 0 —
	// any leading space would mean the encoder prefix leaked through.
	if !strings.HasPrefix(body, "{") {
		t.Errorf("body has leading whitespace, indent leaked into parsed value: %q", body)
	}
}
