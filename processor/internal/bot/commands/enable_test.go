package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/ratelimit"
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

// !enable / !disable / !unregister reported success for IDs that aren't
// registered (#283). Unknown IDs are now reported, not claimed.
func TestEnableDisableUnregisterReportUnknownIDs(t *testing.T) {
	for name, cmd := range map[string]interface {
		Run(*bot.CommandContext, []string) []bot.Reply
	}{
		"enable": &EnableCommand{}, "disable": &DisableCommand{}, "unregister": &UnregisterCommand{},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, mock := testCtx(t)
			ctx.IsAdmin = true
			mock.AddHuman(&store.Human{ID: "222"})

			replies := cmd.Run(ctx, []string{"999", "222"})
			if len(replies) != 1 {
				t.Fatalf("replies = %+v", replies)
			}
			if !strings.Contains(replies[0].Text, "999") || !strings.Contains(replies[0].Text, "222") {
				t.Errorf("reply should name 222 as done and 999 as unknown: %q", replies[0].Text)
			}
			if strings.Count(replies[0].Text, "999") != 1 || !strings.Contains(replies[0].Text, ctx.Tr().Tf("msg.user.not_registered", "999")) {
				t.Errorf("999 should be reported as not registered: %q", replies[0].Text)
			}

			replies = cmd.Run(ctx, []string{"999"})
			if replies[0].React != "🙅" {
				t.Errorf("only unknown IDs: react = %q, want 🙅", replies[0].React)
			}
		})
	}
}

// Re-enabling a banned user re-arms the rate-limit ban (#285).
func TestEnableAndStartClearRateLimitViolations(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.IsAdmin = true
	limiter := ratelimit.New(ratelimit.Config{TimingPeriod: 60, DMLimit: 1, ChannelLimit: 5, MaxLimitsBeforeStop: 1})
	defer limiter.Close()
	ctx.Admin = &bot.AdminDeps{AlertLimiter: limiter}
	mock.AddHuman(&store.Human{ID: "222", Type: bot.TypeDiscordUser})

	banned := func(id string) bool {
		for _, s := range limiter.ListBlocked() {
			if s.ID == id && !s.BannedUntil.IsZero() {
				return true
			}
		}
		return false
	}

	for _, tc := range []struct {
		name string
		run  func()
		id   string
	}{
		{"enable", func() { (&EnableCommand{}).Run(ctx, []string{"222"}) }, "222"},
		{"start", func() { (&StartCommand{}).Run(ctx, nil) }, "user1"},
	} {
		limiter.Check(tc.id, "discord:user")
		limiter.Check(tc.id, "discord:user")
		if !banned(tc.id) {
			t.Fatalf("%s: setup should have banned %s", tc.name, tc.id)
		}
		tc.run()
		if banned(tc.id) {
			t.Errorf("%s: ban not cleared for %s", tc.name, tc.id)
		}
	}
}
