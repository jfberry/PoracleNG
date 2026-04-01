package commands

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func poracleTestCtx(t *testing.T) (*bot.CommandContext, *store.MockHumanStore) {
	t.Helper()
	ctx, mock := testCtx(t)

	// Remove the default seeded user — poracle tests need fresh state
	mock.Delete("user1")
	mock.Calls = nil // reset call tracking

	// Set up a registration channel
	ctx.Config.Discord.Channels = []string{"ch1"}
	ctx.IsDM = false // poracle command must be in a channel
	ctx.ChannelID = "ch1"

	return ctx, mock
}

func TestPoracleCommand_NewUser(t *testing.T) {
	ctx, mock := poracleTestCtx(t)

	cmd := &PoracleCommand{}
	replies := cmd.Run(ctx, nil)

	assertReact(t, replies, "✅")
	assertCall(t, mock, "Create")
	assertCall(t, mock, "CreateDefaultProfile")

	h, _ := mock.Get("user1")
	if h == nil {
		t.Fatal("expected user to be created")
	}
	if !h.Enabled {
		t.Error("expected user to be enabled")
	}
	if h.Type != bot.TypeDiscordUser {
		t.Errorf("expected type %s, got %s", bot.TypeDiscordUser, h.Type)
	}
}

func TestPoracleCommand_ExistingUser_AlreadyActive(t *testing.T) {
	ctx, _ := poracleTestCtx(t)

	// Seed an existing active user
	ctx.Humans.(*store.MockHumanStore).AddHuman(&store.Human{
		ID:      "user1",
		Type:    bot.TypeDiscordUser,
		Enabled: true,
	})

	cmd := &PoracleCommand{}
	replies := cmd.Run(ctx, nil)

	// Already registered and active → should get 👌
	assertReact(t, replies, "👌")
}

func TestPoracleCommand_ExistingUser_Disabled(t *testing.T) {
	ctx, mock := poracleTestCtx(t)

	mock.AddHuman(&store.Human{
		ID:      "user1",
		Type:    bot.TypeDiscordUser,
		Enabled: false,
	})

	cmd := &PoracleCommand{}
	replies := cmd.Run(ctx, nil)

	assertReact(t, replies, "✅")
	assertCall(t, mock, "Update")
}

func TestPoracleCommand_NotRegistrationChannel(t *testing.T) {
	ctx, _ := poracleTestCtx(t)
	ctx.ChannelID = "other-channel" // not in registered channels

	cmd := &PoracleCommand{}
	replies := cmd.Run(ctx, nil)

	// Should silently ignore (nil replies)
	if len(replies) != 0 {
		t.Errorf("expected no replies for non-registration channel, got %d", len(replies))
	}
}

func TestPoracleCommand_TelegramDM_Rejected(t *testing.T) {
	ctx, _ := poracleTestCtx(t)
	ctx.Platform = "telegram"
	ctx.IsDM = true

	cmd := &PoracleCommand{}
	replies := cmd.Run(ctx, nil)

	if len(replies) == 0 {
		t.Fatal("expected rejection reply for telegram DM")
	}
	// Should get a text reply (not a react) explaining telegram requires group
}

func TestPoracleCommand_WithAreaSecurity(t *testing.T) {
	ctx, mock := poracleTestCtx(t)

	cc := config.CommunityConfig{
		Name:          "TestCommunity",
		AllowedAreas:  []string{"Downtown"},
		LocationFence: []string{"TestZone"},
	}
	cc.Discord.Channels = []string{"ch1"}

	ctx.Config.Area = config.AreaConfig{
		Enabled:     true,
		Communities: []config.CommunityConfig{cc},
	}

	cmd := &PoracleCommand{}
	replies := cmd.Run(ctx, nil)

	assertReact(t, replies, "✅")
	assertCall(t, mock, "Create")

	h, _ := mock.Get("user1")
	if h == nil {
		t.Fatal("expected user to be created")
	}
	if len(h.CommunityMembership) == 0 {
		t.Error("expected community membership to be set")
	}
	if len(h.AreaRestriction) == 0 {
		t.Error("expected area restriction to be set from community location fence")
	}
}
