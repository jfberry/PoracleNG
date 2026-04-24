package bot

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// newTargetCtx builds a CommandContext and a MockHumanStore seeded with the
// sender (user1) and the channel (ch1) as a registered humans row.
func newTargetCtx(t *testing.T, isDM, isAdmin bool) *CommandContext {
	t.Helper()
	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{
		ID: "user1", Type: TypeDiscordUser, Name: "Alice", Enabled: true,
	})
	humans.AddHuman(&store.Human{
		ID: "ch1", Type: TypeDiscordChannel, Name: "general", Enabled: true,
	})
	return &CommandContext{
		UserID:    "user1",
		ChannelID: "ch1",
		Platform:  "discord",
		IsDM:      isDM,
		IsAdmin:   isAdmin,
		TargetID:  "user1",
		Humans:    humans,
	}
}

func TestBuildTarget_DMTargetsSender(t *testing.T) {
	ctx := newTargetCtx(t, true, false)
	target, _, err := BuildTarget(ctx, []string{"pikachu"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target == nil || target.ID != "user1" {
		t.Fatalf("expected sender target user1, got %+v", target)
	}
}

func TestBuildTarget_AdminInChannelTargetsChannel(t *testing.T) {
	ctx := newTargetCtx(t, false, true)
	target, _, err := BuildTarget(ctx, []string{"pikachu"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target == nil || target.ID != "ch1" {
		t.Fatalf("expected channel target ch1, got %+v", target)
	}
}

func TestBuildTarget_DelegatedChannelAdminTargetsChannel(t *testing.T) {
	ctx := newTargetCtx(t, false, false)
	ctx.Permissions.ChannelTracking = true
	target, _, err := BuildTarget(ctx, []string{"pikachu"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target == nil || target.ID != "ch1" {
		t.Fatalf("expected channel target ch1, got %+v", target)
	}
}

// TestBuildTarget_NonAdminInRegisteredChannelRejected locks in the behaviour
// that a plain guild member cannot silently mutate their own tracking from
// inside a registered channel — they must DM the bot.
func TestBuildTarget_NonAdminInRegisteredChannelRejected(t *testing.T) {
	ctx := newTargetCtx(t, false, false)
	target, _, err := BuildTarget(ctx, []string{"pikachu"})
	if err == nil {
		t.Fatalf("expected rejection error, got target %+v", target)
	}
	if target != nil {
		t.Errorf("expected nil target on rejection, got %+v", target)
	}
	if !strings.Contains(err.Error(), "channel admins") {
		t.Errorf("error message should mention channel admins; got %q", err.Error())
	}
}

func TestBuildTarget_NonAdminInUnregisteredChannelRejected(t *testing.T) {
	ctx := newTargetCtx(t, false, false)
	// Remove the channel from the mock store so it appears unregistered
	ctx.Humans = store.NewMockHumanStore()
	(ctx.Humans.(*store.MockHumanStore)).AddHuman(&store.Human{
		ID: "user1", Type: TypeDiscordUser, Name: "Alice", Enabled: true,
	})

	_, _, err := BuildTarget(ctx, []string{"pikachu"})
	if err == nil {
		t.Fatal("expected rejection error for unregistered channel")
	}
	if !strings.Contains(err.Error(), "does not seem to be registered") {
		t.Errorf("error should mention unregistered; got %q", err.Error())
	}
}
