package commands

import (
	"testing"
)

func TestPoracleAdmin_NonAdmin_TextRefusal(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = false

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, nil)

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	// Must be a text reply, not a react (🙅 is reserved for command_security).
	if replies[0].React != "" {
		t.Errorf("non-admin refusal must be text, not a react %q", replies[0].React)
	}
	if replies[0].Text == "" {
		t.Error("non-admin refusal must have non-empty text")
	}
	// The not_admin key should appear in the reply (raw key fallback when bundle is empty).
	// With our test bundle (i18n.Load("")), keys that exist return their value.
	wantSubstr := "administrators"
	if !containsStr(replies[0].Text, wantSubstr) {
		t.Errorf("expected refusal text to contain %q, got %q", wantSubstr, replies[0].Text)
	}
}

func TestPoracleAdmin_NoArgs_ShowsHelp(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, nil)

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}

	text := replies[0].Text
	// All nine group names must appear in the help output.
	for _, group := range paSubgroupOrder {
		if !containsStr(text, group) {
			t.Errorf("top-level help missing group name %q", group)
		}
	}
}

func TestPoracleAdmin_UnknownGroup_Refusal(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"bogus"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if replies[0].Text == "" {
		t.Error("unknown-group refusal must have non-empty text")
	}
	if replies[0].React != "" {
		t.Errorf("unknown-group refusal must be text, not react %q", replies[0].React)
	}
}

func TestPoracleAdmin_KnownGroupNoArgs_ShowsGroupHelp(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply for slash help stub, got none")
	}
	if replies[0].Text == "" {
		t.Error("group help stub must return non-empty text")
	}
}

func TestPoracleAdmin_KnownGroupWithArgs_CallsSubgroupRun(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	// "slash sync" — subgroup run, args=["sync"]. Stub returns the stub text.
	replies := cmd.Run(ctx, []string{"slash", "sync"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply for slash run stub, got none")
	}
	if replies[0].Text == "" {
		t.Error("group run stub must return non-empty text")
	}
}

func TestPoracleAdmin_AllGroupsWired(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}

	for _, group := range paSubgroupOrder {
		t.Run("help/"+group, func(t *testing.T) {
			replies := cmd.Run(ctx, []string{group})
			if len(replies) == 0 {
				t.Fatalf("group %q help returned no replies", group)
			}
			if replies[0].Text == "" {
				t.Errorf("group %q help must return non-empty text", group)
			}
		})

		t.Run("run/"+group, func(t *testing.T) {
			replies := cmd.Run(ctx, []string{group, "somearg"})
			if len(replies) == 0 {
				t.Fatalf("group %q run returned no replies", group)
			}
			if replies[0].Text == "" {
				t.Errorf("group %q run must return non-empty text", group)
			}
		})
	}
}
