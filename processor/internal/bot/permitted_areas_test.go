package bot

import (
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// Fences covering every combination that matters:
//
//	alpha  selectable, in coast           beta   selectable, in harbour only
//	priv   NOT selectable, in coast       drawn  NOT selectable, exempt, in no community
//	pubx   selectable, exempt, in no community
//	lone   NOT selectable, NOT exempt, in no community
var permittedFences = []geofence.Fence{
	{Name: "Alpha", UserSelectable: true},
	{Name: "beta", UserSelectable: true},
	{Name: "priv", UserSelectable: false},
	{Name: "drawn", UserSelectable: false, ExemptFromAreaSecurity: true},
	{Name: "pubx", UserSelectable: true, ExemptFromAreaSecurity: true},
	{Name: "lone", UserSelectable: false},
}

func permittedCfg(enabled bool) *config.Config {
	cfg := &config.Config{}
	cfg.Area.Enabled = enabled
	cfg.Area.Communities = []config.CommunityConfig{
		{Name: "Coast", AllowedAreas: []string{"ALPHA", "priv"}},
		{Name: "harbour", AllowedAreas: []string{"beta"}},
	}
	return cfg
}

func TestPermittedAreas(t *testing.T) {
	member := &store.Human{ID: "u", CommunityMembership: []string{"coast"}, AreaRestriction: []string{"x"}}
	// Reconciled member of a community with no location_fence: restriction nil.
	memberNoRestriction := &store.Human{ID: "u", CommunityMembership: []string{"coast"}}
	// API-created human with no community: neither field set.
	unrestricted := &store.Human{ID: "u"}

	cases := []struct {
		name           string
		enabled        bool
		human          *store.Human
		admin, trusted bool
		want           []string
	}{
		// area_security off: only userSelectable matters; exempt is irrelevant.
		{"off/plain", false, member, false, false, []string{"alpha", "beta", "pubx"}},
		{"off/trusted", false, member, false, true, []string{"alpha", "beta", "priv", "drawn", "pubx", "lone"}},
		{"off/admin", false, member, true, false, []string{"alpha", "beta", "priv", "drawn", "pubx", "lone"}},

		// area_security on, community member.
		{"on/plain", true, member, false, false, []string{"alpha", "pubx"}},
		// The #243 case: trusted + exempt lifts drawn; beta (other community)
		// and lone (not exempt) stay out. beta staying out is #228.
		{"on/trusted", true, member, false, true, []string{"alpha", "priv", "drawn", "pubx"}},
		{"on/admin", true, member, true, false, []string{"alpha", "beta", "priv", "drawn", "pubx", "lone"}},

		// Gate: membership alone subjects the human to the filter (see plan
		// "Decision taken during planning").
		{"on/member-no-restriction", true, memberNoRestriction, false, false, []string{"alpha", "pubx"}},
		// Gate: no membership and no restriction stays unrestricted, as the API always had it.
		{"on/unrestricted", true, unrestricted, false, false, []string{"alpha", "beta", "pubx"}},
		{"on/nil-human", true, nil, false, false, []string{"alpha", "beta", "pubx"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PermittedAreas(permittedFences, permittedCfg(tc.enabled), tc.human, tc.admin, tc.trusted)
			if !slices.Equal(got, tc.want) {
				t.Errorf("PermittedAreas = %v, want %v", got, tc.want)
			}
		})
	}
}

// Membership matching is case-insensitive, and so is allowed_areas: config
// spells "Coast"/"ALPHA", the human row stores "coast", the fence is "Alpha".
// (Covered by the on/plain row; this asserts lowercased output explicitly.)
func TestPermittedAreas_ReturnsLowercase(t *testing.T) {
	got := PermittedAreas([]geofence.Fence{{Name: "Gent_Centrum", UserSelectable: true}}, permittedCfg(false), nil, false, false)
	if !slices.Equal(got, []string{"gent_centrum"}) {
		t.Fatalf("got %v, want [gent_centrum]", got)
	}
}

func TestPermittedAreas_NoFencesIsEmpty(t *testing.T) {
	if got := PermittedAreas(nil, permittedCfg(true), nil, true, true); len(got) != 0 {
		t.Fatalf("no fences must permit nothing, got %v", got)
	}
}

func TestPermittedAreaKeys_BothStorageConventions(t *testing.T) {
	keys := PermittedAreaKeys([]string{"gent_centrum"})
	if !keys["gent_centrum"] || !keys["gent centrum"] {
		t.Fatalf("keys = %v, want both the humans.area and override_areas spellings", keys)
	}
}
