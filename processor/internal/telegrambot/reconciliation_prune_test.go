package telegrambot

import (
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func newTgPrune(t *testing.T, prune bool) (*TelegramReconciliation, *store.MockHumanStore) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{
		{Name: "coast", AllowedAreas: []string{"alpha"}},
		{Name: "harbour", AllowedAreas: []string{"beta"}},
	}
	coast := cfg.Area.Communities[0]
	coast.Telegram.Channels = []string{"c-coast"}
	cfg.Area.Communities[0] = coast

	harbour := cfg.Area.Communities[1]
	harbour.Telegram.Channels = []string{"c-harbour"}
	cfg.Area.Communities[1] = harbour

	cfg.Reconciliation.Telegram.PruneUserAreas = prune
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: []geofence.Fence{
		{Name: "alpha", UserSelectable: true}, {Name: "beta", UserSelectable: true},
		{Name: "drawn", ExemptFromAreaSecurity: true},
	}})
	hs := store.NewMockHumanStore()
	hs.AddHuman(&store.Human{ID: "7", Type: "telegram:user", Name: "n", Enabled: true, CurrentProfileNo: 1,
		Area: []string{"alpha", "beta", "drawn"}, CommunityMembership: []string{"coast", "harbour"}})
	hs.SeedProfile(store.Profile{ID: "7", ProfileNo: 1, Area: []string{"alpha", "beta", "drawn"}})
	return NewTelegramReconciliation(nil, hs, cfg, nil, nil, mgr), hs
}

func TestTelegramReconcile_PrunesWhenEnabled(t *testing.T) {
	r, hs := newTgPrune(t, true)
	u, _ := hs.Get("7")
	r.reconcileAreaSecurity("7", u, "n", []string{"c-coast"}, false, false)
	h, _ := hs.Get("7")
	if !slices.Equal(h.Area, []string{"alpha", "drawn"}) {
		t.Fatalf("humans.area = %v, want [alpha drawn]", h.Area)
	}
	if hs.StrippedAreaCalls["7"] != nil {
		t.Fatal("telegram must not gain an override prune in this change")
	}
}

func TestTelegramReconcile_DefaultLeavesAreas(t *testing.T) {
	r, hs := newTgPrune(t, false)
	u, _ := hs.Get("7")
	r.reconcileAreaSecurity("7", u, "n", []string{"c-coast"}, false, false)
	if h, _ := hs.Get("7"); len(h.Area) != 3 {
		t.Fatalf("humans.area pruned without prune_user_areas: %v", h.Area)
	}
}
