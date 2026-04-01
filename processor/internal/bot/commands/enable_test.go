package commands

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/store"
)

func TestEnableCommand_AdminEnablesUser(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.IsAdmin = true
	mock.AddHuman(&store.Human{ID: "target1", AdminDisable: true})

	cmd := &EnableCommand{}
	replies := cmd.Run(ctx, []string{"target1"})

	assertReact(t, replies, "✅")
	assertCall(t, mock, "SetAdminDisable")

	h, _ := mock.Get("target1")
	if h.AdminDisable {
		t.Error("expected target to be admin-enabled")
	}
}

func TestEnableCommand_NotAdmin(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = false

	cmd := &EnableCommand{}
	replies := cmd.Run(ctx, []string{"target1"})

	assertReact(t, replies, "🙅")
}

func TestEnableCommand_NoArgs(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &EnableCommand{}
	replies := cmd.Run(ctx, nil)

	assertReact(t, replies, "🙅")
}

func TestEnableCommand_MultipleMentions(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.IsAdmin = true
	mock.AddHuman(&store.Human{ID: "111", AdminDisable: true})
	mock.AddHuman(&store.Human{ID: "222", AdminDisable: true})

	cmd := &EnableCommand{}
	replies := cmd.Run(ctx, []string{"<@111>", "<@!222>"})

	assertReact(t, replies, "✅")

	h1, _ := mock.Get("111")
	h2, _ := mock.Get("222")
	if h1.AdminDisable || h2.AdminDisable {
		t.Error("both targets should be admin-enabled")
	}
}

func TestDisableCommand_AdminDisablesUser(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.IsAdmin = true
	mock.AddHuman(&store.Human{ID: "target1", AdminDisable: false})

	cmd := &DisableCommand{}
	replies := cmd.Run(ctx, []string{"target1"})

	assertReact(t, replies, "✅")

	h, _ := mock.Get("target1")
	if !h.AdminDisable {
		t.Error("expected target to be admin-disabled")
	}
}

func TestDisableCommand_NotAdmin(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = false

	cmd := &DisableCommand{}
	replies := cmd.Run(ctx, []string{"target1"})

	assertReact(t, replies, "🙅")
}
