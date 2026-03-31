package nlp

import "testing"

func TestDetectIntent(t *testing.T) {
	invasionEvents := map[string]bool{
		"kecleon":   true,
		"showcase":  true,
		"gold-stop": true,
	}

	tests := []struct {
		input   string
		wantCmd string
		wantRm  bool
		wantRst string
	}{
		{"level 5 raids nearby", "raid", false, "level 5 nearby"},
		{"pikachu iv100", "track", false, "pikachu iv100"},
		{"stop tracking dragonite", "track", true, "dragonite"},
		{"remove raids", "raid", true, ""},
		{"kecleon pokestop", "invasion", false, "kecleon"},
		{"showcase", "invasion", false, "showcase"},
		{"gigantamax battles", "maxbattle", false, "level7"},
		{"dynamax snorlax", "maxbattle", false, "snorlax"},
		{"max battle snorlax", "maxbattle", false, "snorlax"},
		{"quest stardust", "quest", false, "stardust"},
		{"grunt water", "invasion", false, "water"},
		// Additional cases
		{"egg level 3", "egg", false, "level 3"},
		{"lure glacial", "lure", false, "glacial"},
		{"nesting pikachu", "nest", false, "pikachu"},
		{"gym team valor", "gym", false, "team valor"},
		{"pokestop changes", "fort", false, "changes"},
		{"delete quest stardust", "quest", true, "stardust"},
		{"untrack pikachu", "track", true, "pikachu"},
		{"research stardust", "quest", false, "stardust"},
		{"rocket leader", "invasion", false, "leader"},
		{"gold-stop nearby", "invasion", false, "gold-stop nearby"},
		{"kecleon invasion nearby", "invasion", false, "kecleon nearby"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := DetectIntent(tt.input, invasionEvents)
			if got.CommandType != tt.wantCmd {
				t.Errorf("DetectIntent(%q).CommandType = %q, want %q", tt.input, got.CommandType, tt.wantCmd)
			}
			if got.IsRemove != tt.wantRm {
				t.Errorf("DetectIntent(%q).IsRemove = %v, want %v", tt.input, got.IsRemove, tt.wantRm)
			}
			if got.Remaining != tt.wantRst {
				t.Errorf("DetectIntent(%q).Remaining = %q, want %q", tt.input, got.Remaining, tt.wantRst)
			}
		})
	}
}
