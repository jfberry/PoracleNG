package personalshiny

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func TestPersonalShinyScanRolls(t *testing.T) {
	// Pin the independently verified game calculation, including opaque IDs.
	for _, tc := range []struct {
		id                string
		encounter         uint64
		shiny, background float64
	}{
		{"0123456789abcdef", 1, 0.22137850522994995, 0.1431974172592163},
		{"Player-ID:ABC/123", 1, 0.3684830665588379, 0.9563266038894653},
		{" Trainer-é ", 1, 0.03772324323654175, 0.7365694642066956},
	} {
		p := newShinyPlayer(tc.id)
		for i := 0; i < 2; i++ {
			shiny, background := p.rolls(tc.encounter)
			if got := float64(shiny) / shinyScale; got != tc.shiny {
				t.Fatalf("shiny roll=%v want=%v", got, tc.shiny)
			}
			if got := float64(background) / shinyScale; got != tc.background {
				t.Fatalf("background roll=%v want=%v", got, tc.background)
			}
		}
	}
}

func TestShinyZonePerSpawn(t *testing.T) {
	// All four corners of this box are America/Denver, but the point inside is
	// America/Phoenix; overrides must follow the spawn, not the box.
	r := NewRates("", "", 0)
	for _, c := range [][2]float64{{35.5, -111.2}, {35.5, -109.8}, {36.3, -111.2}, {36.3, -109.8}} {
		if z := r.zone(c[0], c[1]); z != "America/Denver" {
			t.Skipf("timezone data changed: corner %v is %s", c, z)
		}
	}
	s := &shinyScan{rates: &shinyRates{zoned: true}, zone: r.zone}
	if z := s.zoneAt(map[string]uint32{"America/Phoenix": 1}, 35.812, -110.195); z != "America/Phoenix" {
		t.Fatalf("zone = %q", z)
	}
}

func TestPersonalRollSeenTypes(t *testing.T) {
	rolled := map[string]bool{
		"wild":                    true,
		"encounter":               true,
		"tappable_encounter":      true,
		"tappable_lure_encounter": true,
	}
	// Poracle: Golbat walks every SeenTypeCode; these are its seenTypeNames
	// plus the unset ("") and unknown values a webhook can carry.
	for _, seenType := range []string{"", "bogus", "wild", "encounter", "nearby_stop",
		"nearby_cell", "lure_wild", "lure_encounter", "tappable_encounter", "tappable_lure_encounter"} {
		p := &webhook.PokemonWebhook{PokemonID: 94, Costume: 3, SeenType: seenType}
		want := uint8(0)
		if rolled[seenType] {
			want = 4
		}
		if got := personalLookup(p); got != want {
			t.Errorf("personalLookup with seen type %q = %d, want %d", seenType, got, want)
		}
	}
}
