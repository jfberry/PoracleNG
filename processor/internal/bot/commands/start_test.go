package commands

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/store"
)

func TestStartCommand_Success(t *testing.T) {
	ctx, mock := testCtx(t)

	// Seed user as disabled
	mock.AddHuman(&store.Human{ID: "user1", Enabled: false, Fails: 5})

	cmd := &StartCommand{}
	replies := cmd.Run(ctx, nil)

	assertReact(t, replies, "✅")
	assertCall(t, mock, "SetEnabledWithFails")

	// Verify the user was re-enabled
	h, _ := mock.Get("user1")
	if !h.Enabled {
		t.Error("expected user to be enabled")
	}
	if h.Fails != 0 {
		t.Errorf("expected fails=0, got %d", h.Fails)
	}
}

func TestStopCommand_Success(t *testing.T) {
	ctx, mock := testCtx(t)

	cmd := &StopCommand{}
	replies := cmd.Run(ctx, nil)

	assertReact(t, replies, "✅")
	assertCall(t, mock, "SetEnabled")

	h, _ := mock.Get("user1")
	if h.Enabled {
		t.Error("expected user to be disabled")
	}
}

func TestStopCommand_WithArgs_Warns(t *testing.T) {
	ctx, mock := testCtx(t)

	cmd := &StopCommand{}
	replies := cmd.Run(ctx, []string{"pokemon"})

	assertReact(t, replies, "🙅")
	// Should NOT have called SetEnabled — it's a warning, not a stop
	assertNoCall(t, mock, "SetEnabled")

	h, _ := mock.Get("user1")
	if !h.Enabled {
		t.Error("user should still be enabled — stop with args should warn, not stop")
	}
}
