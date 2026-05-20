package discordbot

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

// TestPopulateInteractionResponseData covers the three shapes the
// button response render can produce: plain text, single-embed JSON
// (legacy "embed" key), and multi-embed JSON ("embeds" array). The
// click handler hands a string to respondEphemeral; this helper has
// to route the bytes into Content vs Embeds[] so Discord renders
// them properly.
func TestPopulateInteractionResponseData(t *testing.T) {
	t.Run("plain text → Content", func(t *testing.T) {
		var d discordgo.InteractionResponseData
		populateInteractionResponseData(&d, "📍 51.27, 1.06")
		if d.Content != "📍 51.27, 1.06" {
			t.Errorf("Content: got %q", d.Content)
		}
		if len(d.Embeds) != 0 {
			t.Errorf("Embeds: got %d, want 0", len(d.Embeds))
		}
	})

	t.Run("single embed JSON → Embeds[0]", func(t *testing.T) {
		var d discordgo.InteractionResponseData
		populateInteractionResponseData(&d, `{"embed":{"title":"PVP for Dunsparce","color":2031120}}`)
		if d.Content != "" {
			t.Errorf("Content should be empty for embed-only response, got %q", d.Content)
		}
		if len(d.Embeds) != 1 {
			t.Fatalf("Embeds: got %d, want 1", len(d.Embeds))
		}
		if d.Embeds[0].Title != "PVP for Dunsparce" {
			t.Errorf("Embed title: got %q", d.Embeds[0].Title)
		}
	})

	t.Run("multi-embed JSON → Embeds slice", func(t *testing.T) {
		var d discordgo.InteractionResponseData
		populateInteractionResponseData(&d, `{"embeds":[{"title":"A"},{"title":"B"}]}`)
		if len(d.Embeds) != 2 {
			t.Fatalf("Embeds: got %d, want 2", len(d.Embeds))
		}
	})

	t.Run("content + embed JSON populates both", func(t *testing.T) {
		var d discordgo.InteractionResponseData
		populateInteractionResponseData(&d, `{"content":"hi","embed":{"title":"x"}}`)
		if d.Content != "hi" {
			t.Errorf("Content: got %q", d.Content)
		}
		if len(d.Embeds) != 1 {
			t.Errorf("Embeds: got %d, want 1", len(d.Embeds))
		}
	})

	t.Run("malformed JSON falls back to Content", func(t *testing.T) {
		var d discordgo.InteractionResponseData
		populateInteractionResponseData(&d, `{this isn't valid`)
		// Falls back to raw content so the operator can see the
		// broken render output rather than getting an empty response.
		if d.Content != `{this isn't valid` {
			t.Errorf("Content: got %q", d.Content)
		}
	})
}
