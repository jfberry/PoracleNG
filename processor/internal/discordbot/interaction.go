package discordbot

import (
	"encoding/json"
	"strings"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/delivery"
)

// onInteractionCreate routes Discord interactions. Application-command
// and autocomplete interactions go to the slash dispatcher (when
// configured); message-component interactions are routed to the
// thread-join handler (the only component-driven flow Poracle owns
// today). Other interaction types are ignored.
func (b *Bot) onInteractionCreate(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	switch ic.Type {
	case discordgo.InteractionApplicationCommand:
		if b.slash != nil {
			b.slash.HandleCommand(s, ic)
		}
		return
	case discordgo.InteractionApplicationCommandAutocomplete:
		if b.slash != nil {
			b.slash.HandleAutocomplete(s, ic)
		}
		return
	case discordgo.InteractionMessageComponent:
		// poracle button clicks claim first dibs; if the custom_id isn't
		// ours, fall through to the thread-join handler.
		if b.handleButtonClick(s, ic) {
			return
		}
	default:
		return
	}

	data := ic.MessageComponentData()
	masterID, threadID, ok := decodeThreadJoinID(data.CustomID)
	if !ok {
		return
	}

	userID := ""
	if ic.Member != nil && ic.Member.User != nil {
		userID = ic.Member.User.ID
	} else if ic.User != nil {
		userID = ic.User.ID
	}
	if userID == "" {
		return
	}

	// Authorisation: the user must currently see the master channel.
	// Discord populates ic.Member.Permissions with the effective bits
	// for the channel the interaction was raised from, which is the
	// master channel (the picker lives there).
	if ic.Member == nil || ic.Member.Permissions&discordgo.PermissionViewChannel == 0 {
		respondEphemeral(s, ic, "🙅 You don't have access to this channel.")
		return
	}

	// Already a thread member? No-op per design.
	tm, err := s.ThreadMember(threadID, userID, false)
	if err == nil && tm != nil {
		respondEphemeral(s, ic, "👌 You're already in this thread.")
		return
	}

	if err := s.ThreadMemberAdd(threadID, userID); err != nil {
		log.Warnf("discord bot: ThreadMemberAdd master=%s thread=%s user=%s: %v", masterID, threadID, userID, err)
		respondEphemeral(s, ic, "❌ Couldn't add you to the thread — please try again later.")
		return
	}
	respondEphemeral(s, ic, "✅ Joined.")
}

// respondEphemeral sends a message visible only to the interaction
// author. When msg is a JSON object containing "embed" / "embeds" /
// "content", those keys are parsed into the matching InteractionResponse
// fields so a DTS-rendered button response shows up as a proper Discord
// embed rather than a JSON blob in the text. Plain text passes through
// as Content unchanged.
func respondEphemeral(s *discordgo.Session, ic *discordgo.InteractionCreate, msg string) {
	data := &discordgo.InteractionResponseData{
		Flags: discordgo.MessageFlagsEphemeral,
	}
	populateInteractionResponseData(data, msg)
	err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: data,
	})
	if err != nil {
		log.Warnf("discord bot: InteractionRespond: %v", err)
	}
}

// populateInteractionResponseData fills data with the appropriate fields
// from msg. JSON-shaped input (the DTS render output for embed-style
// templates) is run through delivery.NormalizeAndExtractImage — same
// coercion the regular alert send path uses, so hex-string colors get
// turned into ints, singular "embed" becomes "embeds[]", and per-embed
// limits are enforced. Non-JSON input becomes Content directly.
func populateInteractionResponseData(data *discordgo.InteractionResponseData, msg string) {
	trimmed := strings.TrimSpace(msg)
	if !strings.HasPrefix(trimmed, "{") {
		data.Content = msg
		return
	}
	normalized, _, err := delivery.NormalizeAndExtractImage(json.RawMessage(trimmed), false)
	if err != nil {
		// Not valid JSON — fall back to treating the whole string as
		// content. Operators who meant to send an embed will see
		// the raw text in the ephemeral and notice the missing braces.
		data.Content = msg
		return
	}
	var parsed struct {
		Content string                    `json:"content"`
		Embeds  []*discordgo.MessageEmbed `json:"embeds"`
	}
	if err := json.Unmarshal(normalized, &parsed); err != nil {
		data.Content = msg
		return
	}
	data.Content = parsed.Content
	data.Embeds = parsed.Embeds
	// If nothing parsed out (both Content and Embeds empty), keep the
	// raw text as Content so the operator can still see something.
	if data.Content == "" && len(data.Embeds) == 0 {
		data.Content = msg
	}
}
