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
}
