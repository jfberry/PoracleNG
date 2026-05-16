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

func TestInteractionUserIDEmptyForMissingUser(t *testing.T) {
	// sendToDM's early-return guard: an interaction with no Member.User and no
	// User should yield empty userID, which sendToDM rejects without panicking.
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{}}
	if got := interactionUserID(ic); got != "" {
		t.Errorf("interactionUserID=%q, want empty", got)
	}
}

func TestInteractionUserIDPrefersMember(t *testing.T) {
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Member: &discordgo.Member{User: &discordgo.User{ID: "member-id"}},
		User:   &discordgo.User{ID: "user-id"},
	}}
	if got := interactionUserID(ic); got != "member-id" {
		t.Errorf("interactionUserID=%q, want member-id", got)
	}
}

func TestInteractionUserIDFallsBackToUser(t *testing.T) {
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		User: &discordgo.User{ID: "user-id"},
	}}
	if got := interactionUserID(ic); got != "user-id" {
		t.Errorf("interactionUserID=%q, want user-id", got)
	}
}

// /area overview returns a Reply with Text + ImageURL — the slash sender
// must produce a single embed payload carrying both, not drop the image.
// We don't fetch the URL in this test (would hit the network); we let
// DownloadImage fail and assert the fallback URL-embed path.
func TestBuildReplyPayloadsImageURLFallback(t *testing.T) {
	// http://localhost:1/no-such-port is unreachable so DownloadImage
	// fails and we exercise the URL-embed fallback. The reply still has
	// to produce an embed with the URL set.
	payloads := buildReplyPayloads(bot.Reply{
		Text:     "Your areas: London",
		ImageURL: "http://localhost:1/area-overview.png",
	})
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}
	p := payloads[0]
	if len(p.embeds) != 1 {
		t.Fatalf("expected 1 embed, got %d", len(p.embeds))
	}
	embed := p.embeds[0]
	if embed.Description != "Your areas: London" {
		t.Errorf("embed description=%q, want text", embed.Description)
	}
	if embed.Image == nil || embed.Image.URL == "" {
		t.Errorf("embed has no image URL: %+v", embed)
	}
	if p.content != "" {
		t.Errorf("content should be empty when text rides in the embed; got %q", p.content)
	}
}

func TestBuildReplyPayloadsPlainTextChunks(t *testing.T) {
	r := bot.Reply{Text: strings.Repeat("x", 2500)}
	payloads := buildReplyPayloads(r)
	if len(payloads) < 2 {
		t.Errorf("expected ≥2 chunks for 2500-byte text, got %d", len(payloads))
	}
	for _, p := range payloads {
		if len(p.content) > 2000 {
			t.Errorf("chunk too long: %d", len(p.content))
		}
		if len(p.embeds) != 0 || len(p.files) != 0 {
			t.Errorf("plain text payload should not have embeds/files: %+v", p)
		}
	}
}

func TestBuildReplyPayloadsEmbedJSON(t *testing.T) {
	// A raw Embed JSON blob (used by /track confirmations etc.) is
	// parsed and reflected as a Discord MessageEmbed in the payload.
	raw := []byte(`{"content":"hi","embed":{"title":"Tracked","description":"ok"}}`)
	payloads := buildReplyPayloads(bot.Reply{Embed: raw})
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}
	p := payloads[0]
	if p.content != "hi" {
		t.Errorf("content=%q, want hi", p.content)
	}
	if len(p.embeds) != 1 || p.embeds[0].Title != "Tracked" {
		t.Errorf("embed parse failed: %+v", p.embeds)
	}
}

func TestBuildReplyPayloadsEmptyReplyReturnsNothing(t *testing.T) {
	if got := buildReplyPayloads(bot.Reply{}); len(got) != 0 {
		t.Errorf("expected no payloads for empty reply, got %+v", got)
	}
}
