package discordbot

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

var (
	// nameRe matches name<value>, name:value, or namevalue (case-insensitive).
	nameRe = regexp.MustCompile(`(?i)^name[:<]?(\S+?)>?$`)
	// areaRe matches area<value>, area:value (case-insensitive).
	areaRe = regexp.MustCompile(`(?i)^area[:<]?(\S+?)>?$`)
	// languageRe matches language<value>, language:value (case-insensitive).
	languageRe = regexp.MustCompile(`(?i)^language[:<]?(\S+?)>?$`)
	// webhookURLRe matches a Discord webhook URL or similar HTTP(S) URL in the raw message.
	webhookURLRe = regexp.MustCompile(`(?i)https?://[-A-Z0-9+&@#/%=~_|$?!:,.]*[-A-Z0-9+&@#/%=~_|$]`)
)

func (b *Bot) handleChannel(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if !bot.IsAdmin(b.cfg, "discord", m.Author.ID) {
		s.MessageReactionAdd(m.ChannelID, m.ID, "🙅")
		return
	}

	isDM := m.GuildID == ""
	tr := b.translations.For(b.cfg.General.Locale)

	// Parse name<value>, area<name>, language<code> from args
	var webhookName, areaName, language string
	for _, arg := range args {
		if match := nameRe.FindStringSubmatch(arg); match != nil {
			webhookName = match[1]
		}
		if match := areaRe.FindStringSubmatch(arg); match != nil {
			areaName = match[1]
		}
		if match := languageRe.FindStringSubmatch(arg); match != nil {
			language = match[1]
		}
	}

	// Parse webhook URL from the raw message content
	var webhookURL string
	if urls := webhookURLRe.FindAllString(m.Content, -1); len(urls) > 0 {
		webhookURL = urls[0]
	}

	hasAdd := false
	hasRemove := false
	for _, arg := range args {
		if arg == "add" {
			hasAdd = true
		}
		if arg == "remove" {
			hasRemove = true
		}
	}

	if hasAdd {
		b.handleChannelAdd(s, m, isDM, webhookName, webhookURL, areaName, language, tr)
	} else if hasRemove {
		b.handleChannelRemove(s, m, isDM, webhookName, tr)
	}
}

func (b *Bot) handleChannelAdd(s *discordgo.Session, m *discordgo.MessageCreate, isDM bool, webhookName, webhookURL, areaName, language string, tr interface{ T(string) string; Tf(string, ...interface{}) string }) {
	// If only one of name/url provided, reject
	if (webhookName != "" && webhookURL == "") || (webhookName == "" && webhookURL != "") {
		s.ChannelMessageSend(m.ChannelID, "To add webhooks, provide both a name using the `name` parameter and a url")
		return
	}

	var targetID, targetName, targetType string

	if webhookName != "" && webhookURL != "" {
		// Webhook registration
		targetID = webhookURL
		targetName = webhookName
		targetType = "webhook"
	} else {
		// Channel registration — must not be DM
		if isDM {
			s.ChannelMessageSend(m.ChannelID, "Adding a bot controlled channel cannot be done from DM. To add webhooks, provide both a name using the `name` parameter and a url")
			return
		}
		targetID = m.ChannelID
		targetType = "discord:channel"

		// Get channel name
		ch, err := s.Channel(m.ChannelID)
		if err != nil {
			log.Warnf("discord bot: get channel %s: %v", m.ChannelID, err)
			targetName = m.ChannelID
		} else {
			targetName = ch.Name
		}
	}

	// Check if already registered
	existing, _ := db.SelectOneHumanFull(b.db, targetID)
	if existing != nil {
		s.MessageReactionAdd(m.ChannelID, m.ID, "👌")
		return
	}

	// Insert new human
	area := "[]"
	if areaName != "" {
		areaJSON, _ := json.Marshal([]string{areaName})
		area = string(areaJSON)
	}

	h := &db.HumanFull{
		ID:                  targetID,
		Type:                targetType,
		Name:                targetName,
		Enabled:             1,
		Area:                area,
		CommunityMembership: "[]",
	}
	if language != "" {
		h.Language.SetValid(language)
	}
	if err := db.CreateHuman(b.db, h); err != nil {
		log.Errorf("discord bot: create human for channel/webhook %s: %v", targetID, err)
		s.ChannelMessageSend(m.ChannelID, "Failed to register, check logs")
		return
	}

	// Create default profile
	if err := db.CreateDefaultProfile(b.db, targetID, targetName, "[]", 0, 0); err != nil {
		log.Warnf("discord bot: create default profile for %s: %v", targetID, err)
	}

	s.MessageReactionAdd(m.ChannelID, m.ID, "✅")

	var reply string
	if webhookName != "" {
		reply = tr.T("cmd.webhook.added")
	} else {
		reply = tr.T("cmd.channel.added")
	}

	s.ChannelMessageSend(m.ChannelID, reply)

	// Trigger reload so the new human is picked up
	if b.reloadFunc != nil {
		b.reloadFunc()
	}
}

func (b *Bot) handleChannelRemove(s *discordgo.Session, m *discordgo.MessageCreate, isDM bool, webhookName string, tr interface{ T(string) string; Tf(string, ...interface{}) string }) {
	if webhookName != "" {
		// Remove webhook by name
		var webhookID string
		err := b.db.Get(&webhookID, `SELECT id FROM humans WHERE name = ? AND type = 'webhook' LIMIT 1`, webhookName)
		if err != nil || webhookID == "" {
			s.ChannelMessageSend(m.ChannelID, "Webhook with that name does not appear to be registered")
			return
		}
		if err := db.DeleteHumanAndTracking(b.db, webhookID); err != nil {
			log.Errorf("discord bot: delete webhook %s: %v", webhookName, err)
			s.ChannelMessageSend(m.ChannelID, "Failed to remove webhook, check logs")
			return
		}
		s.MessageReactionAdd(m.ChannelID, m.ID, "✅")
	} else {
		// Remove channel
		if isDM {
			s.ChannelMessageSend(m.ChannelID, "Removing a bot controlled channel cannot be done from DM")
			return
		}

		targetID := m.ChannelID
		existing, _ := db.SelectOneHumanFull(b.db, targetID)
		if existing == nil {
			prefix := b.cfg.Discord.Prefix
			if prefix == "" {
				prefix = "!"
			}
			s.ChannelMessageSend(m.ChannelID,
				fmt.Sprintf("%s does not seem to be registered. Add it with %schannel add",
					m.ChannelID, prefix))
			return
		}
		if err := db.DeleteHumanAndTracking(b.db, targetID); err != nil {
			log.Errorf("discord bot: delete channel %s: %v", targetID, err)
			s.ChannelMessageSend(m.ChannelID, "Failed to remove channel, check logs")
			return
		}
		s.MessageReactionAdd(m.ChannelID, m.ID, "✅")
	}

	// Trigger reload
	if b.reloadFunc != nil {
		b.reloadFunc()
	}
}

// parseGuildArg extracts the value from a guild<id>, guild:id, or guildid argument.
func parseGuildArg(args []string) string {
	re := regexp.MustCompile(`(?i)^guild[:<]?(\d{1,20})>?$`)
	for _, arg := range args {
		if match := re.FindStringSubmatch(arg); match != nil {
			return match[1]
		}
	}
	return ""
}
