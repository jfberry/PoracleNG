package slash

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

func TestRenderInitialIsAlwaysEphemeral(t *testing.T) {
	// All slash replies are ephemeral. Reply.IsDM=true triggers DM, not non-ephemeral.
	payload := renderInitial(bot.Reply{Text: "hello"})
	if payload.Content != "hello" {
		t.Errorf("content=%q", payload.Content)
	}
	if payload.Flags != discordgo.MessageFlagsEphemeral {
		t.Error("expected ephemeral")
	}
}

func TestRenderRepliesLongTextSplits(t *testing.T) {
	long := strings.Repeat("x", 2500)
	chunks := splitReplyText(long)
	if len(chunks) < 2 {
		t.Errorf("expected >1 chunk, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > 2000 {
			t.Errorf("chunk too long: %d", len(c))
		}
	}
}

func TestRawUserIDEmptyForMissingUser(t *testing.T) {
	// sendToDM's early-return guard: an interaction with no Member.User and no
	// User should yield empty userID, which sendToDM rejects without panicking.
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{}}
	if got := rawUserID(ic); got != "" {
		t.Errorf("rawUserID=%q, want empty", got)
	}
}

func TestRawUserIDPrefersMember(t *testing.T) {
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Member: &discordgo.Member{User: &discordgo.User{ID: "member-id"}},
		User:   &discordgo.User{ID: "user-id"},
	}}
	if got := rawUserID(ic); got != "member-id" {
		t.Errorf("rawUserID=%q, want member-id", got)
	}
}

func TestRawUserIDFallsBackToUser(t *testing.T) {
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		User: &discordgo.User{ID: "user-id"},
	}}
	if got := rawUserID(ic); got != "user-id" {
		t.Errorf("rawUserID=%q, want user-id", got)
	}
}
