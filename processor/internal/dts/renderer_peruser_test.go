package dts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// A non-pokemon template that references distance/bearing is rendered once
// per user, with that user's values. Before this, group rendering left
// distance at 0 and bearingEmoji empty for every non-pokemon alert.
func TestRenderAlertPerUserDistance(t *testing.T) {
	entries := []DTSEntry{{
		Type: "invasion", ID: "1", Platform: "discord", Default: true,
		Template: map[string]any{"content": "{{#if userDistanceTrack}}{{round distance}}m {{bearing}} {{bearingEmoji}} of {{userTrackDistance}}{{else}}area{{/if}}"},
	}}
	configDir := t.TempDir()
	writeTestDTS(t, configDir, entries)
	r, err := NewRenderer(RendererConfig{
		ConfigDir:     configDir,
		FallbackDir:   t.TempDir(),
		DefaultLocale: "en",
		UtilEmojis:    map[string]string{"northeast": "NE!", "south": "S!"},
	})
	if err != nil {
		t.Fatal(err)
	}

	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "1", Language: "en", Distance: 120, Bearing: 45, CardinalDirection: "northeast", TrackDistance: 1000},
		{ID: "u2", Type: "discord:user", Template: "1", Language: "en", Distance: 800, Bearing: 180, CardinalDirection: "south", TrackDistance: 900},
		{ID: "u3", Type: "discord:user", Template: "1", Language: "en", Distance: 5000, Bearing: 90, CardinalDirection: "east"},
	}
	jobs := r.RenderAlert("invasion", map[string]any{}, nil, nil, users, nil, "ref", "")
	if len(jobs) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(jobs))
	}
	want := map[string]string{
		"u1": "120m 45 NE! of 1000",
		"u2": "800m 180 S! of 900",
		"u3": "area",
	}
	for _, j := range jobs {
		got, _ := parseMessage(t, j.Message)["content"].(string)
		if got != want[j.Target] {
			t.Errorf("%s: got %q, want %q", j.Target, got, want[j.Target])
		}
	}
}

func TestUsesPerUserFields(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		partials string
		want     bool
	}{
		{"distance", "{{round distance}}m", "", true},
		{"bearingEmoji", "{{bearingEmoji}}", "", true},
		{"bearing", "{{bearing}}°", "", true},
		{"userTrackDistance", "{{userTrackDistance}}", "", true},
		{"group field only", "{{#if userDistanceTrack}}near{{/if}}", "", false},
		{"capitalised literal", "Distance: {{name}}", "", false},
		{"none", "{{name}}", "", false},
		{"partial uses distance", "{{> dist}}", `{"dist":"{{distance}}m"}`, true},
		{"partial without distance", "{{> other}}", `{"other":"{{name}}"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configDir := t.TempDir()
			writeTestDTS(t, configDir, []DTSEntry{{
				Type: "raid", ID: "1", Platform: "discord", Default: true,
				Template: map[string]any{"content": tc.content},
			}})
			if tc.partials != "" {
				writeTestFile(t, configDir, "partials.json", tc.partials)
			}
			ts, err := LoadTemplates(configDir, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if got := ts.UsesPerUserFields("raid", "discord", "1", "en"); got != tc.want {
				t.Errorf("UsesPerUserFields = %v, want %v", got, tc.want)
			}
		})
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
