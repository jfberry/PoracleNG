package enrichment

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func TestComputeSeenType(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Direct passthroughs / explicit normalisations
		{"nearby_stop", "pokestop"},
		{"nearby_cell", "cell"},
		{"lure", "lure"},
		{"lure_wild", "lure"},
		{"lure_encounter", "lure_encounter"},
		{"encounter", "encounter"},
		{"wild", "wild"},
		// Tappables: both Golbat sub-types collapse to a single template-friendly value
		{"tappable_encounter", "tappable"},
		{"tappable_lure_encounter", "tappable"},
		// Unknown/unmapped seen_type — guard against accidentally surfacing the raw value
		{"some_future_type", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := computeSeenType(&webhook.PokemonWebhook{SeenType: tc.in})
			if got != tc.want {
				t.Errorf("computeSeenType(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestComputeSeenTypeRDMFallback covers the legacy RDM-style fallback used
// when Golbat's seen_type is absent — nothing to do with tappables but worth
// locking in alongside the explicit case to prevent regression.
func TestComputeSeenTypeRDMFallback(t *testing.T) {
	cases := []struct {
		name        string
		pokestopID  string
		spawnID     string
		hasIVs      bool
		want        string
	}{
		{"both 'None' → cell", "None", "None", false, "cell"},
		{"pokestop 'None' no IVs → wild", "None", "abc", false, "wild"},
		{"pokestop 'None' with IVs → encounter", "None", "abc", true, "encounter"},
		{"has pokestop → pokestop", "stop1", "abc", false, "pokestop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &webhook.PokemonWebhook{
				PokestopID:   tc.pokestopID,
				SpawnpointID: webhook.FlexString(tc.spawnID),
			}
			if tc.hasIVs {
				v := 10
				p.IndividualAttack = &v
			}
			got := computeSeenType(p)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
