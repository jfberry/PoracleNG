package discordbot

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

func (b *Bot) handleIDExport(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if !bot.IsAdmin(b.cfg, "discord", m.Author.ID) {
		return
	}

	guildID := m.GuildID

	// Support guild<id> arg override
	if override := parseGuildArg(args); override != "" {
		guildID = override
	}

	if guildID == "" {
		s.ChannelMessageSend(m.ChannelID, "No guild has been set, either execute inside a channel or specify guild<id>")
		return
	}

	// Fetch guild to validate access
	guild, err := s.Guild(guildID)
	if err != nil {
		log.Warnf("discord bot: fetch guild %s: %v", guildID, err)
		s.ChannelMessageSend(m.ChannelID, "I was not able to retrieve that guild")
		return
	}

	var sb strings.Builder

	// Emojis — match alerter format: "name":"<:name:id>" or "<a:name:id>" for animated
	emojis, err := s.GuildEmojis(guildID)
	if err != nil {
		log.Warnf("discord bot: fetch emojis for guild %s: %v", guildID, err)
	}
	for _, e := range emojis {
		prefix := ""
		if e.Animated {
			prefix = "a"
		}
		sb.WriteString(fmt.Sprintf("  \"%s\":\"<%s:%s:%s>\"\n", e.Name, prefix, e.Name, e.ID))
	}

	sb.WriteString("\n\n")

	// Roles
	roles := guild.Roles
	if roles == nil {
		roles, err = s.GuildRoles(guildID)
		if err != nil {
			log.Warnf("discord bot: fetch roles for guild %s: %v", guildID, err)
		}
	}
	for _, r := range roles {
		sb.WriteString(fmt.Sprintf("  \"%s\":\"%s\"\n", r.Name, r.ID))
	}

	_, err = s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Content: "Here's your guild ids!",
		Files: []*discordgo.File{{
			Name:   "id.txt",
			Reader: strings.NewReader(sb.String()),
		}},
	})
	if err != nil {
		log.Warnf("discord bot: send id export: %v", err)
		s.ChannelMessageSend(m.ChannelID, "Failed to send id export, check logs")
	}
}
