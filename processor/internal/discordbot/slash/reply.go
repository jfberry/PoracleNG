package slash

import (
	"fmt"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// Send dispatches a Reply slice to Discord. Pre: interaction has been deferred.
// All slash responses are ephemeral. Reply.IsDM=true triggers a real DM send
// (persistent in user's DM history) plus an ephemeral confirmation in-channel.
// First non-DM reply → InteractionResponseEdit. Subsequent → FollowupCreate.
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
		chunks := splitReplyText(r.Text)
		for _, chunk := range chunks {
			data := renderInitial(bot.Reply{Text: chunk, Embed: r.Embed, ImageURL: r.ImageURL})
			if !firstInteractionUsed {
				if _, err := s.InteractionResponseEdit(ic.Interaction, &discordgo.WebhookEdit{
					Content: &data.Content,
				}); err != nil {
					return err
				}
				firstInteractionUsed = true
			} else {
				if _, err := s.FollowupMessageCreate(ic.Interaction, true, &discordgo.WebhookParams{
					Content: data.Content,
					Flags:   discordgo.MessageFlagsEphemeral,
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// renderInitial builds the ephemeral InteractionResponseData payload for a
// single reply. ALWAYS sets MessageFlagsEphemeral — slash command output is
// never broadcast in-channel.
func renderInitial(r bot.Reply) *discordgo.InteractionResponseData {
	return &discordgo.InteractionResponseData{
		Content: r.Text,
		Flags:   discordgo.MessageFlagsEphemeral,
	}
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
