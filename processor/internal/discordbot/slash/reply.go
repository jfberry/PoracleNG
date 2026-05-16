package slash

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/delivery"
)

// Send dispatches a Reply slice to Discord. Pre: interaction has been deferred.
// All slash responses are ephemeral. Reply.IsDM=true triggers a real DM send
// (persistent in user's DM history) plus an ephemeral confirmation in-channel.
// First non-DM reply → InteractionResponseEdit. Subsequent → FollowupCreate.
//
// Replies carrying ImageURL (e.g. /area overview's static-map tile) or a raw
// Embed JSON blob are rendered as Discord embeds; long Text is chunked into
// 2000-byte pieces. Image bytes are downloaded server-side and attached as a
// multipart file so localhost/internal tileserver URLs still render in client.
func Send(s *discordgo.Session, ic *discordgo.InteractionCreate, replies []bot.Reply) error {
	firstInteractionUsed := false
	for _, r := range replies {
		if r.IsDM {
			if err := sendToDM(s, ic, r); err != nil {
				return err
			}
			if !firstInteractionUsed {
				confirm := "✅ Sent to DM"
				if _, err := s.InteractionResponseEdit(ic.Interaction, &discordgo.WebhookEdit{
					Content: &confirm,
				}); err != nil {
					return err
				}
				firstInteractionUsed = true
			}
			continue
		}

		// Build the payload (text + embeds + files). When the reply has
		// an ImageURL, all the text rides in the embed's description so
		// it isn't doubled up as bare content alongside the image.
		payloads := buildReplyPayloads(r)
		for _, p := range payloads {
			if !firstInteractionUsed {
				edit := &discordgo.WebhookEdit{}
				if p.content != "" {
					edit.Content = &p.content
				}
				if len(p.embeds) > 0 {
					edit.Embeds = &p.embeds
				}
				if len(p.files) > 0 {
					edit.Files = p.files
				}
				if _, err := s.InteractionResponseEdit(ic.Interaction, edit); err != nil {
					return err
				}
				firstInteractionUsed = true
			} else {
				if _, err := s.FollowupMessageCreate(ic.Interaction, true, &discordgo.WebhookParams{
					Content: p.content,
					Embeds:  p.embeds,
					Files:   p.files,
					Flags:   discordgo.MessageFlagsEphemeral,
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// replyPayload is the per-message subset of fields we feed to either the
// initial InteractionResponseEdit or a follow-up.
type replyPayload struct {
	content string
	embeds  []*discordgo.MessageEmbed
	files   []*discordgo.File
}

// buildReplyPayloads produces one or more payloads for a single bot.Reply.
// A plain-text reply chunks into multiple payloads when over Discord's
// 2000-char content limit. A reply with an ImageURL produces one payload
// (the image embed carries the text in its description). A reply with a
// raw Embed JSON blob produces one payload using that embed.
//
// Mirrors the text bot's discordbot/bot.go handling at the same Reply
// shape so /area overview, /track confirmation embeds, etc. render the
// same regardless of which surface the user invoked.
func buildReplyPayloads(r bot.Reply) []replyPayload {
	// ImageURL — download bytes + attach as multipart, with text in the
	// embed description. Falls back to a URL-only embed if download fails.
	if r.ImageURL != "" {
		embed := &discordgo.MessageEmbed{}
		if r.Text != "" {
			embed.Description = r.Text
		}
		imageData, err := delivery.DownloadImage(http.DefaultClient, r.ImageURL)
		if err == nil {
			embed.Image = &discordgo.MessageEmbedImage{URL: "attachment://map.png"}
			return []replyPayload{{
				embeds: []*discordgo.MessageEmbed{embed},
				files: []*discordgo.File{{
					Name:        "map.png",
					ContentType: "image/png",
					Reader:      bytes.NewReader(imageData),
				}},
			}}
		}
		log.Warnf("slash: download image %s: %v — falling back to URL embed", r.ImageURL, err)
		embed.Image = &discordgo.MessageEmbedImage{URL: r.ImageURL}
		return []replyPayload{{embeds: []*discordgo.MessageEmbed{embed}}}
	}

	// Raw Embed JSON (full Discord message shape: content/embed/embeds).
	if len(r.Embed) > 0 {
		if p, ok := parseEmbedJSON(r.Embed); ok {
			return []replyPayload{p}
		}
	}

	// Plain text — chunk to fit the 2000-char per-message limit.
	if r.Text == "" {
		return nil
	}
	chunks := splitReplyText(r.Text)
	out := make([]replyPayload, len(chunks))
	for i, chunk := range chunks {
		out[i] = replyPayload{content: chunk}
	}
	return out
}

// parseEmbedJSON decodes a Reply.Embed blob. Same accepted shapes as the
// text-bot handler: `content`, singular `embed`, and plural `embeds`.
func parseEmbedJSON(raw json.RawMessage) (replyPayload, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		log.Warnf("slash: parse embed JSON: %v", err)
		return replyPayload{}, false
	}
	var p replyPayload
	if c, ok := fields["content"]; ok {
		var content string
		_ = json.Unmarshal(c, &content)
		p.content = content
	}
	if e, ok := fields["embed"]; ok {
		var embed discordgo.MessageEmbed
		if json.Unmarshal(e, &embed) == nil {
			p.embeds = append(p.embeds, &embed)
		}
	}
	if e, ok := fields["embeds"]; ok {
		var embeds []*discordgo.MessageEmbed
		if json.Unmarshal(e, &embeds) == nil {
			p.embeds = append(p.embeds, embeds...)
		}
	}
	if p.content == "" && len(p.embeds) == 0 {
		return replyPayload{}, false
	}
	return p, true
}

// sendToDM opens a DM channel with the interaction's invoking user and sends
// the reply text there. The caller is responsible for any ephemeral
// confirmation in the originating channel.
func sendToDM(s *discordgo.Session, ic *discordgo.InteractionCreate, r bot.Reply) error {
	userID := rawUserID(ic)
	if userID == "" {
		return fmt.Errorf("cannot resolve user for DM")
	}
	ch, err := s.UserChannelCreate(userID)
	if err != nil {
		return fmt.Errorf("open DM channel: %w", err)
	}
	_, err = s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{
		Content: r.Text,
	})
	return err
}

// rawUserID extracts the invoking user's ID from an interaction. Guild
// interactions populate Member.User; DM interactions populate User directly.
func rawUserID(ic *discordgo.InteractionCreate) string {
	if ic == nil || ic.Interaction == nil {
		return ""
	}
	if ic.Member != nil && ic.Member.User != nil {
		return ic.Member.User.ID
	}
	if ic.User != nil {
		return ic.User.ID
	}
	return ""
}

// splitReplyText chunks text into pieces ≤2000 bytes (Discord's per-message
// content limit), preferring line boundaries via bot.SplitMessage.
func splitReplyText(text string) []string {
	return bot.SplitMessage(text, 2000)
}

// renderInitial is kept for backwards-compat with existing tests that
// pin the ephemeral-flag invariant. New code should call buildReplyPayloads.
func renderInitial(r bot.Reply) *discordgo.InteractionResponseData {
	return &discordgo.InteractionResponseData{
		Content: r.Text,
		Flags:   discordgo.MessageFlagsEphemeral,
	}
}
