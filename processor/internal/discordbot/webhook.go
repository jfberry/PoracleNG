package discordbot

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func (b *Bot) handleWebhook(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if !bot.IsAdmin(b.Cfg, "discord", m.Author.ID) {
		s.MessageReactionAdd(m.ChannelID, m.ID, "🙅")
		return
	}

	isDM := m.GuildID == ""

	// Must be in a guild channel, not DM
	if isDM {
		s.ChannelMessageSend(m.ChannelID, "This needs to be run from within a channel on the appropriate guild")
		return
	}

	// Parse name<value> from args
	var webhookName string
	for _, arg := range args {
		if match := nameRe.FindStringSubmatch(arg); match != nil {
			webhookName = match[1]
		}
	}

	// Determine subcommand
	subcommand := ""
	if len(args) > 0 {
		subcommand = args[0]
	}

	prefix := b.Cfg.Discord.Prefix
	if prefix == "" {
		prefix = "!"
	}

	switch subcommand {
	case "list":
		b.handleWebhookList(s, m)
	case "create":
		b.handleWebhookCreate(s, m, webhookName)
	case "add":
		b.handleWebhookAdd(s, m, webhookName)
	case "remove":
		b.handleWebhookRemove(s, m, webhookName)
	default:
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(
			"Usage: `%swebhook list` | `%swebhook add [name:webhookname]` | `%swebhook remove [name:webhookname]` | `%swebhook create`",
			prefix, prefix, prefix, prefix))
	}
}

func (b *Bot) handleWebhookList(s *discordgo.Session, m *discordgo.MessageCreate) {
	ch, _ := s.Channel(m.ChannelID)
	channelName := m.ChannelID
	channelType := "text"
	if ch != nil {
		channelName = ch.Name
		switch ch.Type {
		case discordgo.ChannelTypeGuildText:
			channelType = "GUILD_TEXT"
		case discordgo.ChannelTypeGuildNews:
			channelType = "GUILD_NEWS"
		default:
			channelType = fmt.Sprintf("type:%d", ch.Type)
		}
	}

	hooks, err := s.ChannelWebhooks(m.ChannelID)
	if err != nil {
		log.Warnf("discord bot: fetch webhooks for channel %s: %v", m.ChannelID, err)
		s.ChannelMessageSend(m.ChannelID, "I have not been allowed to manage webhooks! Check bot permissions.")
		return
	}

	// Send results to DM (matching alerter behavior)
	dmCh, err := s.UserChannelCreate(m.Author.ID)
	if err != nil {
		log.Warnf("discord bot: create DM for webhook list: %v", err)
		s.ChannelMessageSend(m.ChannelID, "Could not send DM — check your privacy settings")
		return
	}

	s.ChannelMessageSend(dmCh.ID, fmt.Sprintf("This is %s Channel: %s", channelType, channelName))

	if len(hooks) == 0 {
		s.ChannelMessageSend(dmCh.ID, "No webhooks found in this channel")
		return
	}

	for _, hook := range hooks {
		url := fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", hook.ID, hook.Token)
		s.ChannelMessageSend(dmCh.ID, fmt.Sprintf("%s | %s", hook.Name, url))
	}

	s.ChannelMessageSend(m.ChannelID, "Webhook list sent to your DMs")
}

func (b *Bot) handleWebhookCreate(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	if name == "" {
		name = "Poracle"
	}

	// Check if there's already a Poracle webhook
	hooks, err := s.ChannelWebhooks(m.ChannelID)
	if err != nil {
		log.Warnf("discord bot: fetch webhooks: %v", err)
		s.ChannelMessageSend(m.ChannelID, "Failed to fetch webhooks")
		return
	}

	dmCh, err := s.UserChannelCreate(m.Author.ID)
	if err != nil {
		log.Warnf("discord bot: create DM for webhook create: %v", err)
		return
	}

	for _, hook := range hooks {
		if hook.Name == "Poracle" {
			url := fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", hook.ID, hook.Token)
			s.ChannelMessageSend(dmCh.ID, fmt.Sprintf("There is an existing Poracle webhook %s", url))
			return
		}
	}

	wh, err := s.WebhookCreate(m.ChannelID, "Poracle", "")
	if err != nil {
		log.Warnf("discord bot: create webhook in %s: %v", m.ChannelID, err)
		s.ChannelMessageSend(m.ChannelID, "Failed to create webhook")
		return
	}

	url := fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", wh.ID, wh.Token)
	s.ChannelMessageSend(dmCh.ID, fmt.Sprintf("I created a new webhook %s %s", wh.Name, url))
}

func (b *Bot) handleWebhookAdd(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	// Check if channel is already registered as bot-controlled
	existing, _ := b.Humans.Get(m.ChannelID)
	if existing != nil {
		dmCh, _ := s.UserChannelCreate(m.Author.ID)
		if dmCh != nil {
			s.ChannelMessageSend(dmCh.ID, "This channel is already registered under bot control - `channel remove` first")
		}
		s.MessageReactionAdd(m.ChannelID, m.ID, "👌")
		return
	}

	// Determine webhook name
	if name == "" {
		ch, err := s.Channel(m.ChannelID)
		if err != nil {
			log.Warnf("discord bot: get channel for webhook add: %v", err)
			name = m.ChannelID
		} else {
			name = ch.Name
		}
	}

	// Webhook names cannot contain underscores
	if strings.Contains(name, "_") {
		dmCh, _ := s.UserChannelCreate(m.Author.ID)
		if dmCh != nil {
			s.ChannelMessageSend(dmCh.ID, "A poracle webhook name cannot contain an underscore (_) - use name parameter to override")
		}
		s.MessageReactionAdd(m.ChannelID, m.ID, "👌")
		return
	}

	// Check if name is already in use
	var count int
	b.DB.Get(&count, "SELECT COUNT(*) FROM humans WHERE name = ?", name)
	if count > 0 {
		dmCh, _ := s.UserChannelCreate(m.Author.ID)
		if dmCh != nil {
			s.ChannelMessageSend(dmCh.ID, fmt.Sprintf("A webhook or channel with the name %s already exists", name))
		}
		s.MessageReactionAdd(m.ChannelID, m.ID, "👌")
		return
	}

	// Create or find a Discord webhook in the channel
	var webhookURL string
	hooks, err := s.ChannelWebhooks(m.ChannelID)
	if err != nil {
		log.Warnf("discord bot: fetch webhooks for add: %v", err)
		s.ChannelMessageSend(m.ChannelID, "Failed to fetch webhooks")
		return
	}

	dmCh, _ := s.UserChannelCreate(m.Author.ID)

	for _, hook := range hooks {
		if hook.Name == "Poracle" {
			webhookURL = fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", hook.ID, hook.Token)
			if dmCh != nil {
				s.ChannelMessageSend(dmCh.ID, fmt.Sprintf("There is an existing Poracle webhook %s", webhookURL))
			}
			break
		}
	}

	if webhookURL == "" {
		wh, err := s.WebhookCreate(m.ChannelID, "Poracle", "")
		if err != nil {
			log.Warnf("discord bot: create webhook in %s: %v", m.ChannelID, err)
			s.ChannelMessageSend(m.ChannelID, "Failed to create webhook")
			return
		}
		webhookURL = fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", wh.ID, wh.Token)
		if dmCh != nil {
			s.ChannelMessageSend(dmCh.ID, fmt.Sprintf("I created a new webhook %s %s", wh.Name, webhookURL))
		}
	}

	// Register the webhook
	h := &store.Human{
		ID:      webhookURL,
		Type:    bot.TypeWebhook,
		Name:    name,
		Enabled: true,
	}
	if err := b.Humans.Create(h); err != nil {
		log.Errorf("discord bot: create human for webhook %s: %v", name, err)
		s.ChannelMessageSend(m.ChannelID, "Failed to register webhook, check logs")
		return
	}

	// Create default profile
	if err := b.Humans.CreateDefaultProfile(webhookURL, name, nil, 0, 0); err != nil {
		log.Warnf("discord bot: create default profile for webhook %s: %v", name, err)
	}

	s.MessageReactionAdd(m.ChannelID, m.ID, "✅")

	if b.ReloadFunc != nil {
		b.ReloadFunc()
	}
}

func (b *Bot) handleWebhookRemove(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	// Check if channel is registered as bot-controlled
	existing, _ := b.Humans.Get(m.ChannelID)
	if existing != nil {
		dmCh, _ := s.UserChannelCreate(m.Author.ID)
		if dmCh != nil {
			s.ChannelMessageSend(dmCh.ID, "This channel is already registered under bot control - `channel remove` first")
		}
		s.MessageReactionAdd(m.ChannelID, m.ID, "👌")
		return
	}

	if name == "" {
		ch, err := s.Channel(m.ChannelID)
		if err != nil {
			log.Warnf("discord bot: get channel for webhook remove: %v", err)
			name = m.ChannelID
		} else {
			name = ch.Name
		}
	}

	var webhookID string
	err := b.DB.Get(&webhookID, `SELECT id FROM humans WHERE name = ? AND type = 'webhook' LIMIT 1`, name)
	if err != nil || webhookID == "" {
		dmCh, _ := s.UserChannelCreate(m.Author.ID)
		if dmCh != nil {
			s.ChannelMessageSend(dmCh.ID, fmt.Sprintf("A webhook or channel with the name %s cannot be found", name))
		}
		s.MessageReactionAdd(m.ChannelID, m.ID, "👌")
		return
	}

	if err := db.DeleteHumanAndTracking(b.DB, webhookID); err != nil {
		log.Errorf("discord bot: delete webhook %s: %v", name, err)
		s.ChannelMessageSend(m.ChannelID, "Failed to remove webhook, check logs")
		return
	}

	s.MessageReactionAdd(m.ChannelID, m.ID, "✅")

	if b.ReloadFunc != nil {
		b.ReloadFunc()
	}
}
