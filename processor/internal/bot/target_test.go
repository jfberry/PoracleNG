package bot

import (
	"errors"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// errorsAs is a thin alias around errors.As so the test body stays readable.
func errorsAs(err error, target any) bool { return errors.As(err, target) }

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

	// The error must be a *TargetError so callers can translate it.
	var te *TargetError
	if !errorsAs(err, &te) {
		t.Fatalf("expected *TargetError, got %T", err)
	}
	if te.Key != "msg.channel_admin_only" {
		t.Errorf("expected i18n key msg.channel_admin_only, got %q", te.Key)
	}
	if len(te.Args) == 0 {
		t.Error("expected command prefix to be passed as a format arg")
	}
}

// TestBuildTarget_NonAdminChannelUsesConfiguredPrefix verifies the error
// message points users at the operator's actual Discord prefix (e.g. "?")
// rather than the default "!".
func TestBuildTarget_NonAdminChannelUsesConfiguredPrefix(t *testing.T) {
	ctx := newTargetCtx(t, false, false)
	cfg := &config.Config{}
	cfg.Discord.Prefix = "?"
	ctx.Config = cfg

	_, _, err := BuildTarget(ctx, []string{"tracked"})
	if err == nil {
		t.Fatal("expected rejection")
	}
	var te *TargetError
	if !errorsAs(err, &te) {
		t.Fatalf("expected *TargetError, got %T", err)
	}
	if len(te.Args) != 1 || te.Args[0] != "?" {
		t.Errorf("expected prefix arg \"?\", got %v", te.Args)
	}
	if !strings.Contains(te.Fallback, "?yourcommand") {
		t.Errorf("fallback should reference ?yourcommand; got %q", te.Fallback)
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
