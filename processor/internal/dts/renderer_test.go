package dts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// writeTestDTS writes a dts.json file to dir with the given entries.
func writeTestDTS(t *testing.T, dir string, entries []DTSEntry) {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dts.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func newTestRenderer(t *testing.T, entries []DTSEntry) *Renderer {
	t.Helper()
	configDir := t.TempDir()
	fallbackDir := t.TempDir()
	writeTestDTS(t, configDir, entries)

	r, err := NewRenderer(RendererConfig{
		ConfigDir:     configDir,
		FallbackDir:   fallbackDir,
		DefaultLocale: "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRenderPokemonBasic(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "{{name}} {{iv}}%"},
		},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"name":      "Pikachu",
		"iv":        100,
		"latitude":  51.123456,
		"longitude": 13.654321,
		"tth": map[string]any{
			"hours":        1,
			"minutes":      30,
			"seconds":      0,
			"totalSeconds": 5400,
		},
	}

	users := []webhook.MatchedUser{
		{ID: "user1", Name: "TestUser", Type: "discord:user", Template: "1", Language: "en"},
	}

	jobs := r.RenderPokemon(enrichment, nil, nil, users, nil, true, "test-ref")

	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	job := jobs[0]
	msg, ok := job.Message.(map[string]any)
	if !ok {
		t.Fatalf("expected map message, got %T", job.Message)
	}
	content, _ := msg["content"].(string)
	if content != "Pikachu 100%" {
		t.Errorf("expected 'Pikachu 100%%', got %q", content)
	}
	if job.Target != "user1" {
		t.Errorf("expected target user1, got %q", job.Target)
	}
	if job.Type != "discord:user" {
		t.Errorf("expected type discord:user, got %q", job.Type)
	}
	if job.LogReference != "test-ref" {
		t.Errorf("expected logReference test-ref, got %q", job.LogReference)
	}
	if job.Language != "en" {
		t.Errorf("expected language en, got %q", job.Language)
	}
}

func TestRenderPokemonMonsterNoIv(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "{{name}} with IV"},
		},
		{
			Type:     "monsterNoIv",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "{{name}} no IV"},
		},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"name":      "Bulbasaur",
		"latitude":  0.0,
		"longitude": 0.0,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "1"},
	}

	// Encountered -> monster template
	jobs := r.RenderPokemon(enrichment, nil, nil, users, nil, true, "")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	msg := jobs[0].Message.(map[string]any)
	if msg["content"] != "Bulbasaur with IV" {
		t.Errorf("expected encountered template, got %v", msg["content"])
	}

	// Not encountered -> monsterNoIv template
	jobs = r.RenderPokemon(enrichment, nil, nil, users, nil, false, "")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	msg = jobs[0].Message.(map[string]any)
	if msg["content"] != "Bulbasaur no IV" {
		t.Errorf("expected noIv template, got %v", msg["content"])
	}
}

func TestRenderPokemonMultiUser(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "discord: {{name}}"},
		},
		{
			Type:     "monster",
			ID:       "1",
			Platform: "telegram",
			Default:  true,
			Template: map[string]any{"content": "telegram: {{name}}"},
		},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"name":      "Eevee",
		"latitude":  0.0,
		"longitude": 0.0,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	users := []webhook.MatchedUser{
		{ID: "d1", Type: "discord:user", Template: "1"},
		{ID: "t1", Type: "telegram:user", Template: "1"},
	}

	jobs := r.RenderPokemon(enrichment, nil, nil, users, nil, true, "")
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}

	// First job: discord
	msg0 := jobs[0].Message.(map[string]any)
	if msg0["content"] != "discord: Eevee" {
		t.Errorf("expected discord template, got %v", msg0["content"])
	}
	if jobs[0].Type != "discord:user" {
		t.Errorf("expected discord:user, got %q", jobs[0].Type)
	}

	// Second job: telegram
	msg1 := jobs[1].Message.(map[string]any)
	if msg1["content"] != "telegram: Eevee" {
		t.Errorf("expected telegram template, got %v", msg1["content"])
	}
	if jobs[1].Type != "telegram:user" {
		t.Errorf("expected telegram:user, got %q", jobs[1].Type)
	}
}

func TestRenderPokemonDeduplication(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "{{name}}"},
		},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"name":      "Mewtwo",
		"latitude":  0.0,
		"longitude": 0.0,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	// Same user ID appears twice (from different tracking rules)
	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "1", Name: "Alice"},
		{ID: "u1", Type: "discord:user", Template: "1", Name: "Alice"},
	}

	jobs := r.RenderPokemon(enrichment, nil, nil, users, nil, true, "")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job after dedup, got %d", len(jobs))
	}
}

func TestRenderPokemonPing(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "Found {{name}}"},
		},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"name":      "Dragonite",
		"latitude":  0.0,
		"longitude": 0.0,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "1", Ping: "<@&12345>"},
	}

	jobs := r.RenderPokemon(enrichment, nil, nil, users, nil, true, "")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	msg := jobs[0].Message.(map[string]any)
	content := msg["content"].(string)
	expected := "Found Dragonite <@&12345>"
	if content != expected {
		t.Errorf("expected %q, got %q", expected, content)
	}
}

func TestRenderPokemonTTHExpiry(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "{{name}}"},
		},
	}

	configDir := t.TempDir()
	fallbackDir := t.TempDir()
	writeTestDTS(t, configDir, entries)

	r, err := NewRenderer(RendererConfig{
		ConfigDir:     configDir,
		FallbackDir:   fallbackDir,
		DefaultLocale: "en",
		MinAlertTime:  60, // 60 second minimum
	})
	if err != nil {
		t.Fatal(err)
	}

	enrichment := map[string]any{
		"name":      "Pidgey",
		"latitude":  0.0,
		"longitude": 0.0,
		"tth":       map[string]any{"totalSeconds": 30}, // only 30 seconds left
	}
	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "1"},
	}

	jobs := r.RenderPokemon(enrichment, nil, nil, users, nil, true, "")
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs for expired TTH, got %d", len(jobs))
	}
}

func TestRenderPokemonMissingTemplate(t *testing.T) {
	// No matching template at all
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "99",
			Platform: "telegram",
			Template: map[string]any{"content": "wrong"},
		},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"name":      "Magikarp",
		"latitude":  0.0,
		"longitude": 0.0,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "42", Language: "en"},
	}

	jobs := r.RenderPokemon(enrichment, nil, nil, users, nil, true, "")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 fallback job, got %d", len(jobs))
	}

	msg := jobs[0].Message.(map[string]any)
	content, _ := msg["content"].(string)
	if content == "" {
		t.Error("expected fallback content, got empty string")
	}
	// Should mention "Template not found"
	if !contains(content, "Template not found") {
		t.Errorf("expected fallback content to contain 'Template not found', got %q", content)
	}
}

func TestRenderPokemonLanguageFallback(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "{{name}}"},
		},
	}

	configDir := t.TempDir()
	fallbackDir := t.TempDir()
	writeTestDTS(t, configDir, entries)

	r, err := NewRenderer(RendererConfig{
		ConfigDir:     configDir,
		FallbackDir:   fallbackDir,
		DefaultLocale: "de", // German default
	})
	if err != nil {
		t.Fatal(err)
	}

	enrichment := map[string]any{
		"name":      "Pikachu",
		"latitude":  0.0,
		"longitude": 0.0,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	// User with no language set
	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "1", Language: ""},
	}

	jobs := r.RenderPokemon(enrichment, nil, nil, users, nil, true, "")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Language != "de" {
		t.Errorf("expected fallback language 'de', got %q", jobs[0].Language)
	}
}

func TestRenderPokemonWebhookPlatform(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "{{name}}"},
		},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"name":      "Snorlax",
		"latitude":  0.0,
		"longitude": 0.0,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	// webhook type -> should use discord platform
	users := []webhook.MatchedUser{
		{ID: "wh1", Type: "webhook", Template: "1"},
	}

	jobs := r.RenderPokemon(enrichment, nil, nil, users, nil, true, "")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	// The template was found using "discord" platform
	msg := jobs[0].Message.(map[string]any)
	if msg["content"] != "Snorlax" {
		t.Errorf("expected Snorlax, got %v", msg["content"])
	}
}

func TestRenderPokemonAreas(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "{{name}} in {{areas}}"},
		},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"name":      "Geodude",
		"latitude":  0.0,
		"longitude": 0.0,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "1"},
	}
	areas := []webhook.MatchedArea{
		{Name: "Berlin", DisplayInMatches: true},
		{Name: "Hidden", DisplayInMatches: false},
		{Name: "Munich", DisplayInMatches: true},
	}

	jobs := r.RenderPokemon(enrichment, nil, nil, users, areas, true, "")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	msg := jobs[0].Message.(map[string]any)
	content := msg["content"].(string)
	expected := "Geodude in Berlin, Munich"
	if content != expected {
		t.Errorf("expected %q, got %q", expected, content)
	}
}

func TestRenderPokemonLatLonTruncation(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "ok"},
		},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"latitude":  51.123456789,
		"longitude": -13.654321987,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "1"},
	}

	jobs := r.RenderPokemon(enrichment, nil, nil, users, nil, true, "")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	// 51.123457 (Sprintf rounds to 6 decimal places by default)
	if len(jobs[0].Lat) > 8 {
		t.Errorf("lat too long: %q (len %d)", jobs[0].Lat, len(jobs[0].Lat))
	}
	if len(jobs[0].Lon) > 8 {
		t.Errorf("lon too long: %q (len %d)", jobs[0].Lon, len(jobs[0].Lon))
	}
}

func TestRenderPokemonClean(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "test"},
		},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"latitude":  0.0,
		"longitude": 0.0,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "1", Clean: true},
	}

	jobs := r.RenderPokemon(enrichment, nil, nil, users, nil, true, "")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if !jobs[0].Clean {
		t.Error("expected Clean=true")
	}
}

func TestTruncateCoord(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{51.123456, "51.12345"},  // "51.123456" -> 8 chars -> "51.12345"
		{1.5, "1.500000"},       // short enough
		{-13.6543, "-13.6543"},  // "-13.654300" -> 8 chars -> "-13.6543"
	}
	for _, tt := range tests {
		got := truncateCoord(tt.input)
		if len(got) > 8 {
			t.Errorf("truncateCoord(%v) = %q (len %d > 8)", tt.input, got, len(got))
		}
	}
}

func TestPlatformFromType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"discord:user", "discord"},
		{"discord:channel", "discord"},
		{"telegram:user", "telegram"},
		{"telegram:group", "telegram"},
		{"webhook", "discord"},
	}
	for _, tt := range tests {
		got := platformFromType(tt.input)
		if got != tt.want {
			t.Errorf("platformFromType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDeduplicateUsers(t *testing.T) {
	users := []webhook.MatchedUser{
		{ID: "a", Name: "Alice"},
		{ID: "b", Name: "Bob"},
		{ID: "a", Name: "Alice2"},
		{ID: "c", Name: "Charlie"},
		{ID: "b", Name: "Bob2"},
	}
	result := deduplicateUsers(users)
	if len(result) != 3 {
		t.Fatalf("expected 3 unique users, got %d", len(result))
	}
	if result[0].ID != "a" || result[1].ID != "b" || result[2].ID != "c" {
		t.Errorf("unexpected order: %v", result)
	}
	// First occurrence kept
	if result[0].Name != "Alice" {
		t.Errorf("expected first occurrence 'Alice', got %q", result[0].Name)
	}
}

func TestAppendPing(t *testing.T) {
	// Existing content
	msg := map[string]any{"content": "Hello"}
	appendPing(msg, "<@user>")
	if msg["content"] != "Hello <@user>" {
		t.Errorf("expected 'Hello <@user>', got %v", msg["content"])
	}

	// No existing content
	msg2 := map[string]any{}
	appendPing(msg2, "<@user>")
	if msg2["content"] != "<@user>" {
		t.Errorf("expected '<@user>', got %v", msg2["content"])
	}

	// Non-map message (should not panic)
	appendPing("not a map", "<@user>")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
