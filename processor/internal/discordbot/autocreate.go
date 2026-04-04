package discordbot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

// channelTemplate represents one entry in config/channelTemplate.json.
type channelTemplate struct {
	Name       string             `json:"name"`
	Definition channelDefinition  `json:"definition"`
}

type channelDefinition struct {
	Category *categoryDefinition  `json:"category"`
	Channels []channelEntry       `json:"channels"`
}

type categoryDefinition struct {
	CategoryName string         `json:"categoryName"`
	Roles        []roleEntry    `json:"roles"`
}

type channelEntry struct {
	ChannelName string      `json:"channelName"`
	ChannelType string      `json:"channelType"` // "text" or "voice"
	Topic       string      `json:"topic"`
	ControlType string      `json:"controlType"` // "bot" or "webhook"
	WebhookName string      `json:"webhookName"`
	Roles       []roleEntry `json:"roles"`
	Commands    []string    `json:"commands"`
}

type roleEntry struct {
	Name string `json:"name"`

	// Permission flags — true = allow, false = deny, absent = inherit.
	View                 *bool `json:"view"`
	ViewHistory          *bool `json:"viewHistory"`
	Send                 *bool `json:"send"`
	React                *bool `json:"react"`
	PingEveryone         *bool `json:"pingEveryone"`
	EmbedLinks           *bool `json:"embedLinks"`
	AttachFiles          *bool `json:"attachFiles"`
	SendTTS              *bool `json:"sendTTS"`
	ExternalEmoji        *bool `json:"externalEmoji"`
	ExternalStickers     *bool `json:"externalStickers"`
	CreatePublicThreads  *bool `json:"createPublicThreads"`
	CreatePrivateThreads *bool `json:"createPrivateThreads"`
	SendThreads          *bool `json:"sendThreads"`
	SlashCommands        *bool `json:"slashCommands"`
	Connect              *bool `json:"connect"`
	Speak                *bool `json:"speak"`
	AutoMic              *bool `json:"autoMic"`
	Stream               *bool `json:"stream"`
	VCActivities         *bool `json:"vcActivities"`
	PrioritySpeaker      *bool `json:"prioritySpeaker"`
	CreateInvite         *bool `json:"createInvite"`
	Channels             *bool `json:"channels"`
	Messages             *bool `json:"messages"`
	Roles                *bool `json:"roles"`
	Webhooks             *bool `json:"webhooks"`
	Threads              *bool `json:"threads"`
	Events               *bool `json:"events"`
	Mute                 *bool `json:"mute"`
	Deafen               *bool `json:"deafen"`
	Move                 *bool `json:"move"`
}

// rolePermissionFlags maps JSON field names to Discord permission bit values.
var rolePermissionFlags = map[string]int64{
	"view":                 0x0000000000000400, // ViewChannel
	"viewHistory":          0x0000000000010000, // ReadMessageHistory
	"send":                 0x0000000000000800, // SendMessages
	"react":                0x0000000000000040, // AddReactions
	"pingEveryone":         0x0000000000020000, // MentionEveryone
	"embedLinks":           0x0000000000004000, // EmbedLinks
	"attachFiles":          0x0000000000008000, // AttachFiles
	"sendTTS":              0x0000000000001000, // SendTTSMessages
	"externalEmoji":        0x0000000000040000, // UseExternalEmojis
	"externalStickers":     0x0000002000000000, // UseExternalStickers
	"createPublicThreads":  0x0000000800000000, // CreatePublicThreads
	"createPrivateThreads": 0x0000001000000000, // CreatePrivateThreads
	"sendThreads":          0x0000004000000000, // SendMessagesInThreads
	"slashCommands":        0x0000000080000000, // UseApplicationCommands
	"connect":              0x0000000000100000, // Connect
	"speak":                0x0000000000200000, // Speak
	"autoMic":              0x0000000002000000, // UseVAD
	"stream":               0x0000000000000200, // Stream
	"vcActivities":         0x0000008000000000, // UseEmbeddedActivities
	"prioritySpeaker":      0x0000000000000100, // PrioritySpeaker
	"createInvite":         0x0000000000000001, // CreateInstantInvite
	"channels":             0x0000000000000010, // ManageChannels
	"messages":             0x0000000000002000, // ManageMessages
	"roles":                0x0000000010000000, // ManageRoles
	"webhooks":             0x0000000020000000, // ManageWebhooks
	"threads":              0x0000000400000000, // ManageThreads
	"events":               0x0000002000000000, // ManageEvents — Note: shares bit with externalStickers; match JS behavior
	"mute":                 0x0000000000400000, // MuteMembers
	"deafen":               0x0000000000800000, // DeafenMembers
	"move":                 0x0000000001000000, // MoveMembers
}

// handleAutocreate handles the !autocreate command.
// Loads a channel template, creates Discord categories and channels, and registers them.
func (b *Bot) handleAutocreate(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if !bot.IsAdmin(b.Cfg, "discord", m.Author.ID) {
		s.MessageReactionAdd(m.ChannelID, m.ID, "🙅")
		return
	}

	guildID := m.GuildID
	if gid := parseGuildArg(args); gid != "" {
		guildID = gid
		// Remove guild arg from args
		var filtered []string
		for _, arg := range args {
			if parseGuildArg([]string{arg}) == "" {
				filtered = append(filtered, arg)
			}
		}
		args = filtered
	}

	if guildID == "" {
		s.ChannelMessageSend(m.ChannelID, "No guild has been set, either execute inside a channel or specify guild<id>")
		return
	}

	// Load channel template.
	templatePath := filepath.Join(b.Cfg.BaseDir, "config", "channelTemplate.json")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "No channel templates defined - create config/channelTemplate.json")
		return
	}

	var templates []channelTemplate
	if err := json.Unmarshal(data, &templates); err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to parse channel template: %v", err))
		return
	}

	if len(args) == 0 {
		s.ChannelMessageSend(m.ChannelID, "Please specify a template name as the first parameter")
		return
	}

	templateName := args[0]
	subArgs := args[1:]

	var tmpl *channelTemplate
	for i := range templates {
		if templates[i].Name == templateName {
			tmpl = &templates[i]
			break
		}
	}

	if tmpl == nil {
		s.ChannelMessageSend(m.ChannelID, "I can't find that channel template! (remember it has to be your first parameter)")
		return
	}

	// Build substitution args (restore underscores for names).
	subArgsUnder := make([]string, len(subArgs))
	for i, arg := range subArgs {
		subArgsUnder[i] = strings.ReplaceAll(arg, " ", "_")
	}

	// Create category if defined.
	var categoryID string
	if tmpl.Definition.Category != nil {
		categoryName := formatTemplate(tmpl.Definition.Category.CategoryName, subArgs)

		createData := discordgo.GuildChannelCreateData{
			Name: categoryName,
			Type: discordgo.ChannelTypeGuildCategory,
		}

		if len(tmpl.Definition.Category.Roles) > 0 {
			overwrites := b.buildPermissionOverwrites(s, guildID, tmpl.Definition.Category.Roles, subArgs)
			createData.PermissionOverwrites = overwrites
		}

		cat, err := s.GuildChannelCreateComplex(guildID, createData)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to create category %s: %v", categoryName, err))
			return
		}
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Creating %s", categoryName))
		categoryID = cat.ID
	}

	// Create channels.
	for _, chDef := range tmpl.Definition.Channels {
		channelName := formatTemplate(chDef.ChannelName, subArgs)

		createData := discordgo.GuildChannelCreateData{
			Name: channelName,
		}

		switch chDef.ChannelType {
		case "text", "":
			createData.Type = discordgo.ChannelTypeGuildText
		case "voice":
			createData.Type = discordgo.ChannelTypeGuildVoice
		}

		if categoryID != "" {
			createData.ParentID = categoryID
		}

		if chDef.Topic != "" {
			createData.Topic = formatTemplate(chDef.Topic, subArgs)
		}

		if len(chDef.Roles) > 0 {
			createData.PermissionOverwrites = b.buildPermissionOverwrites(s, guildID, chDef.Roles, subArgs)
		}

		channel, err := s.GuildChannelCreateComplex(guildID, createData)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to create channel %s: %v", channelName, err))
			continue
		}
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Creating %s", channelName))

		// No control type — plain channel, skip registration.
		if chDef.ControlType == "" {
			continue
		}

		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Adding control type: %s", chDef.ControlType))

		var targetID, targetType, targetName string

		if chDef.ControlType == "bot" {
			targetID = channel.ID
			targetType = bot.TypeDiscordChannel
			targetName = formatTemplate(chDef.ChannelName, subArgsUnder)
		} else {
			// Create webhook.
			webhookName := formatTemplate(chDef.ChannelName, subArgsUnder)
			if chDef.WebhookName != "" {
				webhookName = formatTemplate(chDef.WebhookName, subArgsUnder)
			}

			wh, err := s.WebhookCreate(channel.ID, "Poracle", "")
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to create webhook for %s: %v", channelName, err))
				continue
			}

			targetID = fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", wh.ID, wh.Token)
			targetType = bot.TypeWebhook
			targetName = webhookName
		}

		// Register in DB.
		h := &db.HumanFull{
			ID:                  targetID,
			Type:                targetType,
			Name:                targetName,
			Enabled:             1,
			Area:                "[]",
			CommunityMembership: "[]",
		}
		if err := db.CreateHuman(b.DB, h); err != nil {
			log.Errorf("discord bot: autocreate register %s: %v", targetName, err)
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to register %s", targetName))
			continue
		}

		// Create default profile.
		if err := db.CreateDefaultProfile(b.DB, targetID, targetName, "[]", 0, 0); err != nil {
			log.Warnf("discord bot: autocreate default profile %s: %v", targetName, err)
		}

		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Executing as %s / %s %s",
			targetType, targetName, func() string {
				if targetType != bot.TypeWebhook {
					return targetID
				}
				return ""
			}()))

		// Execute configured commands for the channel.
		for _, cmdText := range chDef.Commands {
			expanded := formatTemplate(cmdText, subArgsUnder)
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">>> Executing %s", expanded))

			// Parse and execute the command through the shared registry.
			parsed := b.Parser.Parse(b.Cfg.Discord.Prefix + expanded)
			for _, pc := range parsed {
				handler := b.Registry.Lookup(pc.CommandKey)
				if handler == nil {
					s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Unknown command: %s", pc.CommandKey))
					continue
				}

				ctx := &bot.CommandContext{
					UserID:       m.Author.ID,
					UserName:     m.Author.Username,
					Platform:     "discord",
					ChannelID:    m.ChannelID,
					GuildID:      guildID,
					IsDM:         false,
					IsAdmin:      true,
					Language:     b.Cfg.General.Locale,
					ProfileNo:    1,
					TargetID:     targetID,
					TargetName:   targetName,
					TargetType:   targetType,
					DB:           b.DB,
					Humans:       b.Humans,
					Tracking:     b.Tracking,
					Config:       b.Cfg,
					StateMgr:     b.StateMgr,
					GameData:     b.GameData,
					Translations: b.Translations,
					Dispatcher:   b.Dispatcher,
					RowText:      b.RowText,
					Resolver:     b.Resolver,
					ArgMatcher:   b.ArgMatcher,
					Geocoder:     b.Geocoder,
					StaticMap:    b.StaticMap,
					Weather:      b.Weather,
					Stats:        b.Stats,
					DTS:          b.DTS,
					Emoji:        b.Emoji,
					NLP:           b.nlpParser,
					TestProcessor: b.TestProcessor,
					Registry:      b.Registry,
					ReloadFunc:    b.ReloadFunc,
				}

				st := b.StateMgr.Get()
				if st != nil {
					ctx.Geofence = st.Geofence
					ctx.Fences = st.Fences
					ctx.AreaLogic = bot.NewAreaLogic(st.Fences, b.Cfg)
				}

				replies := handler.Run(ctx, pc.Args)
				b.sendReplies(s, m, replies)
			}
		}
	}

	// Trigger reload after all creations.
	if b.ReloadFunc != nil {
		b.ReloadFunc()
	}

	s.ChannelMessageSend(m.ChannelID, "Autocreate complete!")
}

// buildPermissionOverwrites resolves role names to IDs and builds permission overwrites.
func (b *Bot) buildPermissionOverwrites(s *discordgo.Session, guildID string, roles []roleEntry, args []string) []*discordgo.PermissionOverwrite {
	guild, err := s.Guild(guildID)
	if err != nil {
		log.Warnf("discord bot: autocreate fetch guild %s: %v", guildID, err)
		return nil
	}

	var overwrites []*discordgo.PermissionOverwrite

	for _, role := range roles {
		roleName := formatTemplate(role.Name, args)

		// Find existing role by name.
		var roleID string
		for _, r := range guild.Roles {
			if r.Name == roleName {
				roleID = r.ID
				break
			}
		}

		// Create role if not found.
		if roleID == "" {
			newRole, err := s.GuildRoleCreate(guildID, &discordgo.RoleParams{
				Name: roleName,
			})
			if err != nil {
				log.Warnf("discord bot: autocreate create role %s: %v", roleName, err)
				continue
			}
			roleID = newRole.ID
		}

		allow, deny := computePermissions(role)
		overwrites = append(overwrites, &discordgo.PermissionOverwrite{
			ID:    roleID,
			Type:  discordgo.PermissionOverwriteTypeRole,
			Allow: allow,
			Deny:  deny,
		})
	}

	return overwrites
}

// computePermissions computes allow and deny permission bit fields from a role entry.
func computePermissions(role roleEntry) (allow, deny int64) {
	checkPerm := func(flag *bool, permName string) {
		if flag == nil {
			return
		}
		bits, ok := rolePermissionFlags[permName]
		if !ok {
			return
		}
		if *flag {
			allow |= bits
		} else {
			deny |= bits
		}
	}

	checkPerm(role.View, "view")
	checkPerm(role.ViewHistory, "viewHistory")
	checkPerm(role.Send, "send")
	checkPerm(role.React, "react")
	checkPerm(role.PingEveryone, "pingEveryone")
	checkPerm(role.EmbedLinks, "embedLinks")
	checkPerm(role.AttachFiles, "attachFiles")
	checkPerm(role.SendTTS, "sendTTS")
	checkPerm(role.ExternalEmoji, "externalEmoji")
	checkPerm(role.ExternalStickers, "externalStickers")
	checkPerm(role.CreatePublicThreads, "createPublicThreads")
	checkPerm(role.CreatePrivateThreads, "createPrivateThreads")
	checkPerm(role.SendThreads, "sendThreads")
	checkPerm(role.SlashCommands, "slashCommands")
	checkPerm(role.Connect, "connect")
	checkPerm(role.Speak, "speak")
	checkPerm(role.AutoMic, "autoMic")
	checkPerm(role.Stream, "stream")
	checkPerm(role.VCActivities, "vcActivities")
	checkPerm(role.PrioritySpeaker, "prioritySpeaker")
	checkPerm(role.CreateInvite, "createInvite")
	checkPerm(role.Channels, "channels")
	checkPerm(role.Messages, "messages")
	checkPerm(role.Roles, "roles")
	checkPerm(role.Webhooks, "webhooks")
	checkPerm(role.Threads, "threads")
	checkPerm(role.Events, "events")
	checkPerm(role.Mute, "mute")
	checkPerm(role.Deafen, "deafen")
	checkPerm(role.Move, "move")

	return allow, deny
}

// formatTemplate replaces {0}, {1}, etc. placeholders with the provided arguments.
func formatTemplate(s string, args []string) string {
	result := s
	for i := len(args) - 1; i >= 0; i-- {
		result = strings.ReplaceAll(result, fmt.Sprintf("{%d}", i), args[i])
	}
	return result
}
