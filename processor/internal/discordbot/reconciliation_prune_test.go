package discordbot

import (
	"errors"
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func newPruneReconciliation(t *testing.T, prune bool) (*Reconciliation, *store.MockHumanStore) {
	t.Helper()
	cfg := minimalConfig()
	cfg.Area.Enabled = true
	coast := config.CommunityConfig{Name: "coast", AllowedAreas: []string{"alpha"}}
	coast.Discord.UserRole = []string{"r-coast"}
	harbour := config.CommunityConfig{Name: "harbour", AllowedAreas: []string{"beta"}}
	harbour.Discord.UserRole = []string{"r-harbour"}
	cfg.Area.Communities = []config.CommunityConfig{coast, harbour}
	cfg.Reconciliation.Discord.PruneUserAreas = prune
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: []geofence.Fence{
		{Name: "alpha", UserSelectable: true},
		{Name: "beta", UserSelectable: true},
		{Name: "drawn", UserSelectable: false, ExemptFromAreaSecurity: true},
	}})
	hs := store.NewMockHumanStore()
	hs.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "n", Enabled: true, CurrentProfileNo: 1,
		Area: []string{"alpha", "beta", "drawn"}, CommunityMembership: []string{"coast", "harbour"}})
	hs.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha", "beta", "drawn"}})
	return NewReconciliation(nil, hs, cfg, nil, nil, mgr), hs
}

func TestReconcileAreaSecurity_LosingCommunityPrunesWhenEnabled(t *testing.T) {
	r, hs := newPruneReconciliation(t, true)
	user, _ := hs.Get("u1")
	r.reconcileAreaSecurity("u1", user, "n", []string{"r-coast"}, nil, false, false)
	h, _ := hs.Get("u1")
	if !slices.Equal(h.Area, []string{"alpha", "drawn"}) {
		t.Fatalf("humans.area = %v, want [alpha drawn]", h.Area)
	}
}

func TestReconcileAreaSecurity_DefaultLeavesHumanAreas(t *testing.T) {
	r, hs := newPruneReconciliation(t, false)
	user, _ := hs.Get("u1")
	r.reconcileAreaSecurity("u1", user, "n", []string{"r-coast"}, nil, false, false)
	h, _ := hs.Get("u1")
	if len(h.Area) != 3 {
		t.Fatalf("humans.area pruned without prune_user_areas: %v", h.Area)
	}
	if hs.StrippedAreaCalls["u1"] == nil || !hs.StrippedAreaCalls["u1"]["beta"] || hs.StrippedAreaCalls["u1"]["alpha"] {
		t.Fatalf("override strip must still run and deny beta, not alpha: %v", hs.StrippedAreaCalls["u1"])
	}
}

func TestDiscordReconcile_PruneFailureLeavesMembershipUnsaved(t *testing.T) {
	r, hs := newPruneReconciliation(t, true)
	hs.StripUserAreasErr = errors.New("boom")
	user, _ := hs.Get("u1")
	r.reconcileAreaSecurity("u1", user, "renamed", []string{"r-coast"}, nil, true, false)
	sawName := false
	for _, c := range hs.UpdateCalls {
		if _, ok := c.Fields["community_membership"]; ok {
			t.Fatalf("membership saved despite prune failure: %v", c.Fields)
		}
		if _, ok := c.Fields["area_restriction"]; ok {
			t.Fatalf("area_restriction saved despite prune failure: %v", c.Fields)
		}
		if c.Fields["name"] == "renamed" {
			sawName = true
		}
	}
	if !sawName {
		t.Fatal("name sync should still be saved")
	}

	hs.StripUserAreasErr = nil
	hs.UpdateCalls = nil
	hs.Calls = nil
	r.reconcileAreaSecurity("u1", user, "renamed", []string{"r-coast"}, nil, true, false)
	h, _ := hs.Get("u1")
	if !slices.Equal(h.Area, []string{"alpha", "drawn"}) {
		t.Fatalf("retry did not prune: %v", h.Area)
	}
	saved := false
	for _, c := range hs.UpdateCalls {
		if _, ok := c.Fields["community_membership"]; ok {
			saved = true
		}
	}
	if !saved {
		t.Fatal("membership not saved on retry")
	}
}

func TestDiscordReconcile_PruneThenSave(t *testing.T) {
	r, hs := newPruneReconciliation(t, true)
	user, _ := hs.Get("u1")
	hs.Calls = nil
	r.reconcileAreaSecurity("u1", user, "n", []string{"r-coast"}, nil, false, false)
	var order []string
	for _, c := range hs.Calls {
		if c == "StripOverrideAreas" || c == "StripUserAreas" || c == "Update" {
			order = append(order, c)
		}
	}
	want := []string{"StripOverrideAreas", "StripUserAreas", "Update"}
	if !slices.Equal(order, want) {
		t.Fatalf("call order = %v, want %v", order, want)
	}
}
