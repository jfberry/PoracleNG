package commands

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/store"
)

func TestUnregisterCommand_SelfDelete(t *testing.T) {
	ctx, mock := testCtx(t)

	cmd := &UnregisterCommand{}
	replies := cmd.Run(ctx, nil)

	assertReact(t, replies, "✅")
	assertCall(t, mock, "Delete")

	// User should be gone
	h, _ := mock.Get("user1")
	if h != nil {
		t.Error("expected user to be deleted")
	}
}

func TestUnregisterCommand_AdminDeletesOthers(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.IsAdmin = true
	mock.AddHuman(&store.Human{ID: "target1", Name: "Target"})

	cmd := &UnregisterCommand{}
	replies := cmd.Run(ctx, []string{"target1"})

	assertReact(t, replies, "✅")

	// target1 should be deleted
	h, _ := mock.Get("target1")
	if h != nil {
		t.Error("expected target to be deleted")
	}

	// Admin's own user should NOT be deleted
	own, _ := mock.Get("user1")
	if own == nil {
		t.Error("admin should not be deleted when deleting others")
	}
}

func TestUnregisterCommand_AdminCannotDeleteSelf(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &UnregisterCommand{}
	// Passing own ID should be filtered out (safety)
	replies := cmd.Run(ctx, []string{"user1"})

	assertReact(t, replies, "🙅")
}

func TestUnregisterCommand_AdminNoTargets(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &UnregisterCommand{}
	replies := cmd.Run(ctx, nil)

	assertReact(t, replies, "🙅")
}
