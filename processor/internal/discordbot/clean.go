package discordbot

import (
	"time"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

func (b *Bot) handleClean(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !bot.IsAdmin(b.Cfg, "discord", m.Author.ID) {
		return
	}

	userLang, _, _, _, _ := bot.LookupUserStateFromStore(b.Humans, m.Author.ID, b.Cfg.General.Locale)
	tr := b.Translations.For(userLang)

	messages, err := s.ChannelMessages(m.ChannelID, 100, "", "", "")
	if err != nil {
		log.Warnf("discord bot: fetch messages for clean in %s: %v", m.ChannelID, err)
		s.ChannelMessageSend(m.ChannelID, tr.T("msg.poracle_clean.failed"))
		return
	}

	startMsg, _ := s.ChannelMessageSend(m.ChannelID, tr.T("msg.poracle_clean.start"))

	for _, msg := range messages {
		if msg.Author.ID == s.State.User.ID {
			if err := s.ChannelMessageDelete(m.ChannelID, msg.ID); err != nil {
				log.Debugf("discord bot: delete message %s: %v", msg.ID, err)
			}
		}
	}

	// Delete the start message
	if startMsg != nil {
		s.ChannelMessageDelete(m.ChannelID, startMsg.ID)
	}

	finishMsg, _ := s.ChannelMessageSend(m.ChannelID, tr.T("msg.poracle_clean.finished"))

	// Auto-delete the finish message after 15 seconds
	if finishMsg != nil {
		go func() {
			time.Sleep(15 * time.Second)
			s.ChannelMessageDelete(m.ChannelID, finishMsg.ID)
		}()
	}
}
