package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// TestCommunityCommand_BareShowsCurrentMembership: bare `!community` falls
// through to runShow against ctx.TargetID. PoracleJS-equivalent: gives an
// admin a quick "what am I in?" answer in DM, or "what's this channel in?"
// when run in a registered channel.
func TestCommunityCommand_BareShowsCurrentMembership(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.IsAdmin = true
	mock.AddHuman(&store.Human{
		ID:                  "user1",
		Type:                bot.TypeDiscordUser,
		Name:                "TestUser",
		CommunityMembership: []string{"teamcity"},
	})

	cmd := &CommunityCommand{}
	replies := cmd.Run(ctx, nil)

	assertReact(t, replies, "✅")
	if !strings.Contains(replies[0].Text, "user1") || !strings.Contains(replies[0].Text, "teamcity") {
		t.Errorf("expected membership info in reply, got %q", replies[0].Text)
	}
}

// TestCommunityCommand_AddRefusesUserFallback: `!community add areaname` with
// no explicit targets and a user-type ctx target must NOT silently apply to
// the sender. Mirrors PoracleJS app.js refusal of bare-user adds.
func TestCommunityCommand_AddRefusesUserFallback(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	// ctx.TargetType is already TypeDiscordUser (sender)

	cmd := &CommunityCommand{}
	replies := cmd.Run(ctx, []string{"add", "teamcity"})

	assertReact(t, replies, "🙅")
}

// TestCommunityCommand_AddFallsBackToChannelTarget: when target is a
// channel and no explicit user is named, the channel itself becomes the
// target. Matches PoracleJS "No targets listed, assuming target of X".
func TestCommunityCommand_AddFallsBackToChannelTarget(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.IsAdmin = true
	ctx.TargetID = "channel1"
	ctx.TargetType = bot.TypeDiscordChannel
	ctx.TargetName = "areachannel"
	mock.AddHuman(&store.Human{
		ID:   "channel1",
		Type: bot.TypeDiscordChannel,
		Name: "areachannel",
	})
	ctx.Config = &config.Config{
		Area: config.AreaConfig{
			Enabled: true,
			Communities: []config.CommunityConfig{
				{Name: "teamcity", AllowedAreas: []string{"downtown"}},
			},
		},
	}

	cmd := &CommunityCommand{}
	replies := cmd.Run(ctx, []string{"add", "teamcity"})

	assertReact(t, replies, "✅")

	h, _ := mock.Get("channel1")
	if len(h.CommunityMembership) != 1 || h.CommunityMembership[0] != "teamcity" {
		t.Errorf("expected channel target to gain teamcity, got %v", h.CommunityMembership)
	}
}

// TestCommunityCommand_ClearRefusesUserFallback: clear is mutating; same
// no-self-act rule as add.
func TestCommunityCommand_ClearRefusesUserFallback(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &CommunityCommand{}
	replies := cmd.Run(ctx, []string{"clear"})

	assertReact(t, replies, "🙅")
}

// TestCommunityCommand_ShowAllowsUserFallback: read-only show may fall back
// to the sender so a user can quickly check their own membership.
func TestCommunityCommand_ShowAllowsUserFallback(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &CommunityCommand{}
	replies := cmd.Run(ctx, []string{"show"})

	assertReact(t, replies, "✅")
	if !strings.Contains(replies[0].Text, "user1") {
		t.Errorf("expected own membership info in reply, got %q", replies[0].Text)
	}
}
