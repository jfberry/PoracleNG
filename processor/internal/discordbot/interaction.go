package discordbot

import (
	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"
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
		// fall through to the thread-join handler below
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

// respondEphemeral sends a message visible only to the interaction author.
func respondEphemeral(s *discordgo.Session, ic *discordgo.InteractionCreate, msg string) {
	err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Warnf("discord bot: InteractionRespond: %v", err)
	}
}
