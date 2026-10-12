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

// TestBuildTarget_NameResolvesNamedTargets ensures `name:` resolves not just
// Discord webhooks but every named, non-command destination — notably
// telegram:channel (the Telegram analogue of a Discord webhook). A store
// refactor narrowed the lookup to type='webhook', breaking telegram:channel
// resolution; this guards against that regression.
func TestBuildTarget_NameResolvesNamedTargets(t *testing.T) {
	cases := []struct {
		hookType string
		hookName string
		hookID   string
	}{
		{TypeWebhook, "MyHook", "wh1"},
		{"telegram:channel", "innsbrucktop100", "-1001234567890"},
		{TypeTelegramGroup, "mygroup", "-100999"},
		{TypeDiscordChannel, "namedchan", "chX"},
	}
	for _, tc := range cases {
		t.Run(tc.hookType, func(t *testing.T) {
			humans := store.NewMockHumanStore()
			humans.AddHuman(&store.Human{ID: "user1", Type: TypeDiscordUser, Name: "Alice", Enabled: true})
			humans.AddHuman(&store.Human{ID: tc.hookID, Type: tc.hookType, Name: tc.hookName, Enabled: true})
			ctx := &CommandContext{
				UserID: "user1", Platform: "discord", IsDM: true, IsAdmin: true,
				TargetID: "user1", Humans: humans,
			}
			target, _, err := BuildTarget(ctx, []string{"name:" + tc.hookName, "pikachu"})
			if err != nil {
				t.Fatalf("name:%s (%s): unexpected error: %v", tc.hookName, tc.hookType, err)
			}
			if target == nil || target.ID != tc.hookID {
				t.Fatalf("name:%s (%s): expected target %s, got %+v", tc.hookName, tc.hookType, tc.hookID, target)
			}
		})
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

// A `user` override must be followed by a numeric ID, as in PoracleJS
// (`user:?(-?\d{1,20})`). Any token starting with "user" used to be taken
// as an override, swallowing words like an area named "Userton" (#282).
func TestBuildTarget_UserPrefixRequiresDigits(t *testing.T) {
	ctx := newTargetCtx(t, true, true)
	ctx.Humans.(*store.MockHumanStore).AddHuman(&store.Human{
		ID: "123456", Type: TypeDiscordUser, Name: "Bob", Enabled: true,
	})

	target, remaining, err := BuildTarget(ctx, []string{"userton", "usernames"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ID != "user1" || strings.Join(remaining, " ") != "userton usernames" {
		t.Errorf("target=%s remaining=%v, want sender and both words kept", target.ID, remaining)
	}

	for _, arg := range []string{"user:123456", "user123456"} {
		target, remaining, err = BuildTarget(ctx, []string{arg, "pikachu"})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", arg, err)
		}
		if target.ID != "123456" || strings.Join(remaining, " ") != "pikachu" {
			t.Errorf("%s: target=%s remaining=%v, want 123456 / [pikachu]", arg, target.ID, remaining)
		}
	}
}

func TestBuildTargetStrictKeepsUserPrefixedArgs(t *testing.T) {
	ctx := newTargetCtx(t, true, false)
	_, rest, err := BuildTargetFor(ctx, "cmd.account", []string{"add", "username123", "namesake"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 3 || rest[1] != "username123" || rest[2] != "namesake" {
		t.Fatalf("strict target ate args: %q", rest)
	}
	// user: needs a numeric ID; anything else is an ordinary value.
	if _, rest, err := BuildTargetFor(ctx, "cmd.account", []string{"add", "user:abc"}); err != nil || len(rest) != 2 || rest[1] != "user:abc" {
		t.Fatalf("user:abc rest = %q, err = %v", rest, err)
	}
	// Non-account commands keep the legacy behaviour.
	if _, rest, _ := BuildTargetFor(ctx, "cmd.track", []string{"pikachu"}); len(rest) != 1 {
		t.Fatalf("track args = %q", rest)
	}
}

func TestStripTargetTokens(t *testing.T) {
	got := StripTargetTokens([]string{"add", "User:123", "Abc_Def", "NAME:hook", "username1"})
	if len(got) != 3 || got[0] != "add" || got[1] != "Abc_Def" || got[2] != "username1" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildTargetFor_AccountAdminUserOverride(t *testing.T) {
	ctx := newTargetCtx(t, true, true)
	ctx.Humans.(*store.MockHumanStore).AddHuman(&store.Human{
		ID: "other", Type: TypeDiscordUser, Name: "Bob", Enabled: true,
	})
	target, rest, err := BuildTargetFor(ctx, "cmd.account", []string{"add", "username_9", "user:other"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target == nil || target.ID != "other" {
		t.Fatalf("expected override target other, got %+v", target)
	}
	if len(rest) != 2 || rest[1] != "username_9" {
		t.Fatalf("rest = %v", rest)
	}
}

func TestBuildTargetFor_AccountNonAdminUserOverrideRejected(t *testing.T) {
	ctx := newTargetCtx(t, true, false)
	ctx.Humans.(*store.MockHumanStore).AddHuman(&store.Human{
		ID: "other", Type: TypeDiscordUser, Name: "Bob", Enabled: true,
	})
	_, _, errFor := BuildTargetFor(ctx, "cmd.account", []string{"add", "x", "user:other"})
	_, _, errPlain := BuildTarget(ctx, []string{"user:other"})
	if errFor == nil || errPlain == nil {
		t.Fatalf("expected rejection, got %v / %v", errFor, errPlain)
	}
	if errFor.Error() != errPlain.Error() {
		t.Fatalf("BuildTargetFor err %q differs from BuildTarget err %q", errFor, errPlain)
	}
}
