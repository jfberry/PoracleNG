package gamedata

import "testing"

func TestTypeNameFromTemplate(t *testing.T) {
	cases := []struct {
		template string
		want     string
	}{
		// Typed grunts (still use the early _GRUNT extraction path)
		{"CHARACTER_ELECTRIC_GRUNT_FEMALE", "electric"},
		{"CHARACTER_FIRE_GRUNT_MALE", "fire"},
		{"CHARACTER_DRAGON_GRUNT_FEMALE", "dragon"},
		// Specials
		{"CHARACTER_METAL_GRUNT_FEMALE", "steel"},
		{"CHARACTER_DARKNESS_GRUNT_MALE", "darkness"},
		{"CHARACTER_GRUNT_MALE", "mixed"},
		{"CHARACTER_GRUNT_FEMALE", "mixed"},
		// Bosses — regular and event variants must collapse to the same bare name
		{"CHARACTER_GIOVANNI", "giovanni"},
		{"CHARACTER_EXECUTIVE_ARLO", "arlo"},
		{"CHARACTER_EXECUTIVE_CLIFF", "cliff"},
		{"CHARACTER_EXECUTIVE_SIERRA", "sierra"},
		{"CHARACTER_EVENT_GIOVANNI_UNTICKETED", "giovanni"},
		{"CHARACTER_EVENT_ARLO_UNTICKETED", "arlo"},
		{"CHARACTER_EVENT_CLIFF_UNTICKETED", "cliff"},
		{"CHARACTER_EVENT_SIERRA_UNTICKETED", "sierra"},
		// Team leaders strip the CHARACTER_ prefix
		{"CHARACTER_BLANCHE", "blanche"},
		{"CHARACTER_CANDELA", "candela"},
		{"CHARACTER_SPARK", "spark"},
		// Decoys
		{"CHARACTER_DECOY_FEMALE", "decoy"},
	}
	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			if got := TypeNameFromTemplate(tc.template); got != tc.want {
				t.Errorf("TypeNameFromTemplate(%q) = %q, want %q", tc.template, got, tc.want)
			}
		})
	}
}

func TestCategoryFromTemplate(t *testing.T) {
	cases := []struct {
		template string
		want     int // character_category_N — see resources/gamelocale/en.json
	}{
		// Bosses (all variants must map to the right category so the DTS
		// gruntName field renders the localized leader name rather than
		// "Unset").
		{"CHARACTER_GIOVANNI", 6},
		{"CHARACTER_EVENT_GIOVANNI_UNTICKETED", 6},
		{"CHARACTER_EXECUTIVE_ARLO", 3},
		{"CHARACTER_EVENT_ARLO_UNTICKETED", 3},
		{"CHARACTER_EXECUTIVE_CLIFF", 4},
		{"CHARACTER_EVENT_CLIFF_UNTICKETED", 4},
		{"CHARACTER_EXECUTIVE_SIERRA", 5},
		{"CHARACTER_EVENT_SIERRA_UNTICKETED", 5},
		// Regular grunts
		{"CHARACTER_FIRE_GRUNT_MALE", 2},
		{"CHARACTER_GRUNT_FEMALE", 2},
		// Team leaders
		{"CHARACTER_BLANCHE", 1},
		{"CHARACTER_CANDELA", 1},
		{"CHARACTER_SPARK", 1},
		// Anything else
		{"CHARACTER_DECOY_FEMALE", 0},
		{"", 0},
	}
	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			if got := categoryFromTemplate(tc.template); got != tc.want {
				t.Errorf("categoryFromTemplate(%q) = %d, want %d", tc.template, got, tc.want)
			}
		})
	}
}
