package ai

import (
	"os"
	"testing"
)

func TestTranslateIntegration(t *testing.T) {
	key := os.Getenv("OPENROUTER_KEY")
	if key == "" {
		t.Skip("OPENROUTER_KEY not set")
	}

	client := New("https://openrouter.ai/api/v1", key, "qwen/qwen-2.5-7b-instruct")

	tests := []struct {
		input    string
		contains string // expected substring in result
	}{
		{"track shiny pikachu", "!track pikachu iv0"},
		{"perfect dragonite", "!track dragonite iv100"},
		{"level 5 raids nearby", "!raid level5"},
		{"track hundos for pikachu eevee and dragonite", "!track pikachu iv100"},
		{"team rocket water invasions", "!invasion water"},
		{"stardust quest rewards", "!quest stardust"},
		{"stop tracking bulbasaur", "!track remove bulbasaur"},
		{"alolan vulpix within 1km", "!track vulpix"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := client.TranslateCommand(tt.input)
			if err != nil {
				t.Fatalf("error: %s", err)
			}
			t.Logf("%-45s → %s", tt.input, result)
			if tt.contains != "" && !containsIgnoreCase(result, tt.contains) {
				t.Errorf("expected result to contain %q, got %q", tt.contains, result)
			}
		})
	}
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstr(s, substr))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
