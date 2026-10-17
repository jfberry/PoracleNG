package api

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/state"
)

// Before the first state load there is no fence source; the override_areas
// check must fail closed (empty permitted set), not be skipped.
func TestNewOverrideContext_FailsClosedBeforeFirstStateLoad(t *testing.T) {
	deps, _ := newTestTrackingDeps(t)
	deps.StateMgr = state.NewManager()

	oc, msg, code := newOverrideContext(deps, "u1", false)
	if msg != "" || code != 0 {
		t.Fatalf("unexpected error: %q %d", msg, code)
	}
	if oc.permitted == nil {
		t.Fatal("permitted is nil: check would be skipped (fail open)")
	}
	if len(oc.permitted) != 0 {
		t.Fatalf("permitted = %v, want empty", oc.permitted)
	}
}
