package telegrambot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func TestTelegramReconciliation_FencesFromStateManager(t *testing.T) {
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: []geofence.Fence{{Name: "alpha"}}})
	r := NewTelegramReconciliation(nil, store.NewMockHumanStore(), &config.Config{}, nil, nil, mgr)
	if got := r.fences(); len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("fences() = %v, want [alpha]", got)
	}
	r = NewTelegramReconciliation(nil, store.NewMockHumanStore(), &config.Config{}, nil, nil, nil)
	if got := r.fences(); got != nil {
		t.Fatalf("fences() with nil manager = %v, want nil", got)
	}
}
