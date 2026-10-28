package dts

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestShippedTappableTemplatesRender renders the bundled default tappable
// templates (fallbacks/dts.json) for both platforms, with and without a
// known expiry and a pokestop, and checks the output is valid JSON carrying
// the reward string.
func TestShippedTappableTemplatesRender(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	fallbackDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "fallbacks")
	ts, err := LoadTemplates(t.TempDir(), fallbackDir)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	views := map[string]map[string]any{
		"spawn": {
			"rewardString": "6 Zubat Candy", "tappableTypeName": "Box",
			"disappear_time": 1793220544, "time": "21:09:04", "tthm": 9, "tths": 30,
			"addr": "Somewhere 1", "googleMapUrl": "https://g", "appleMapUrl": "https://a",
			"imgUrl": "https://i", "staticMap": "https://s", "color": "4CAF50",
		},
		"pokestop, unknown expiry": {
			"rewardString": "Rare Candy", "tappableTypeName": "Maple", "pokestopName": "Fountain",
			"disappear_time": 0, "googleMapUrl": "https://g", "appleMapUrl": "https://a",
		},
	}
	for _, platform := range []string{"discord", "telegram"} {
		tmpl := ts.Get("tappable", platform, "", "en")
		if tmpl == nil {
			t.Fatalf("%s: no default tappable template", platform)
		}
		for name, view := range views {
			out, err := tmpl.Exec(view)
			if err != nil {
				t.Fatalf("%s/%s: exec: %v", platform, name, err)
			}
			var parsed map[string]any
			if err := json.Unmarshal([]byte(out), &parsed); err != nil {
				t.Fatalf("%s/%s: rendered invalid JSON: %v\n%s", platform, name, err, out)
			}
			if !strings.Contains(out, view["rewardString"].(string)) {
				t.Errorf("%s/%s: reward string missing:\n%s", platform, name, out)
			}
			hasExpiry := view["disappear_time"] != 0
			if got := strings.Contains(out, "Disappears"); got != hasExpiry {
				t.Errorf("%s/%s: Disappears line present = %v, want %v:\n%s", platform, name, got, hasExpiry, out)
			}
			if _, ok := view["pokestopName"]; ok && !strings.Contains(out, "at Fountain") {
				t.Errorf("%s/%s: pokestop name missing:\n%s", platform, name, out)
			}
		}
	}
}
