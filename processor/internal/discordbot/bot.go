// Package discordbot runs a Discord bot using discordgo for gateway events.
// It receives messages, parses commands, executes them via the bot framework,
// and sends replies back to the channel or DM.
package discordbot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/bot/commands"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/nlp"
)

// Bot is the Discord bot gateway handler.
type Bot struct {
	bot.BotDeps
	session        *discordgo.Session
	nlpParser      *nlp.Parser
	reconciliation *Reconciliation
	stopCh         chan struct{}
}

// Config holds everything needed to create a Discord bot.
type Config struct {
	Token string
	bot.BotDeps
}

// New creates and starts a Discord bot. Returns the bot (for shutdown) or an error.
func New(cfg Config) (*Bot, error) {
	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, err
	}

	b := &Bot{
		BotDeps:   cfg.BotDeps,
		session:   session,
		nlpParser: cfg.NLPParser,
		stopCh:    make(chan struct{}),
	}

	session.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuildMessageReactions

	session.AddHandler(b.onMessageCreate)

	// Reconciliation event handlers.
	session.AddHandler(b.onGuildMemberUpdate)
	session.AddHandler(b.onGuildMemberRemove)
	session.AddHandler(b.onChannelDelete)

	if err := session.Open(); err != nil {
		return nil, err
	}

	log.Infof("Discord bot connected as %s", session.State.User.Username)

	// Log guild presence and validate config
	b.logGuildPresence()

	// Initialize reconciliation if check_role is enabled.
	if cfg.Cfg.Discord.CheckRole && cfg.DTS != nil {
		b.reconciliation = NewReconciliation(session, cfg.DB, cfg.Cfg, cfg.Translations, cfg.DTS)
		go b.reconciliationLoop()
	}

	return b, nil
}

// logGuildPresence logs which guilds the bot is in and validates
// configured Discord IDs (channels, roles, etc.) at startup.
func (b *Bot) logGuildPresence() {
	if b.session.State == nil {
		return
	}

	// Build lookup maps from all guilds
	presentGuilds := make(map[string]string) // id → name
	allRoles := make(map[string]string)      // id → name
	allChannels := make(map[string]string)   // id → name

	for _, g := range b.session.State.Guilds {
		presentGuilds[g.ID] = g.Name

		// Fetch roles for this guild
		if roles, err := b.session.GuildRoles(g.ID); err == nil {
			for _, r := range roles {
				allRoles[r.ID] = r.Name + " (guild:" + g.Name + ")"
			}
		}

		// Fetch channels for this guild
		if channels, err := b.session.GuildChannels(g.ID); err == nil {
			for _, ch := range channels {
				allChannels[ch.ID] = "#" + ch.Name + " (guild:" + g.Name + ")"
			}
		}
	}

	// Log guild presence
	var guildNames []string
	for _, g := range b.session.State.Guilds {
		guildNames = append(guildNames, g.ID+":"+g.Name)
	}
	if len(guildNames) > 0 {
		log.Infof("Bot is present in guilds %s", strings.Join(guildNames, ", "))
	}

	// Validate configured guilds
	for _, gid := range b.Cfg.Discord.Guilds {
		if _, ok := presentGuilds[gid]; !ok {
			log.Warnf("config: discord.guilds contains %s but bot is not in that guild", gid)
		}
	}

	// Validate channels
	for _, chID := range b.Cfg.Discord.Channels {
		if name, ok := allChannels[chID]; ok {
			log.Infof("config: discord.channels %s → %s ✓", chID, name)
		} else {
			log.Warnf("config: discord.channels %s — NOT FOUND in any guild", chID)
		}
	}

	// Validate DM log channel
	if b.Cfg.Discord.DmLogChannelID != "" {
		if name, ok := allChannels[b.Cfg.Discord.DmLogChannelID]; ok {
			log.Infof("config: dm_log_channel_id %s → %s ✓", b.Cfg.Discord.DmLogChannelID, name)
		} else {
			log.Warnf("config: dm_log_channel_id %s — NOT FOUND in any guild", b.Cfg.Discord.DmLogChannelID)
		}
	}

	// Validate user_role IDs
	for _, roleID := range b.Cfg.Discord.UserRole {
		if name, ok := allRoles[roleID]; ok {
			log.Infof("config: discord.user_role %s → %s ✓", roleID, name)
		} else {
			log.Warnf("config: discord.user_role %s — NOT FOUND in any guild", roleID)
		}
	}

	// Validate role_subscriptions
	for _, sub := range b.Cfg.Discord.RoleSubscriptions {
		if _, ok := presentGuilds[sub.Guild]; !ok {
			log.Warnf("config: role_subscriptions guild %s — NOT FOUND", sub.Guild)
		}
		for desc, roleID := range sub.Roles {
			if name, ok := allRoles[roleID]; ok {
				log.Infof("config: role_subscriptions.roles %s (%s) → %s ✓", desc, roleID, name)
			} else {
				log.Warnf("config: role_subscriptions.roles %s (%s) — NOT FOUND in any guild", desc, roleID)
			}
		}
		for _, exSet := range sub.ExclusiveRoles {
			for desc, roleID := range exSet {
				if name, ok := allRoles[roleID]; ok {
					log.Infof("config: role_subscriptions.exclusive_roles %s (%s) → %s ✓", desc, roleID, name)
				} else {
					log.Warnf("config: role_subscriptions.exclusive_roles %s (%s) — NOT FOUND in any guild", desc, roleID)
				}
			}
		}
	}

	// Validate shame channel
	if b.Cfg.AlertLimits.ShameChannel != "" {
		if name, ok := allChannels[b.Cfg.AlertLimits.ShameChannel]; ok {
			log.Infof("config: alert_limits.shame_channel %s → %s ✓", b.Cfg.AlertLimits.ShameChannel, name)
		} else {
			log.Warnf("config: alert_limits.shame_channel %s — NOT FOUND in any guild", b.Cfg.AlertLimits.ShameChannel)
		}
	}

	// Validate community channels (area security)
	for _, comm := range b.Cfg.Area.Communities {
		for _, chID := range comm.Discord.Channels {
			if name, ok := allChannels[chID]; ok {
				log.Infof("config: community %s discord channel %s → %s ✓", comm.Name, chID, name)
			} else {
				log.Warnf("config: community %s discord channel %s — NOT FOUND in any guild", comm.Name, chID)
			}
		}
		for _, roleID := range comm.Discord.UserRole {
			if name, ok := allRoles[roleID]; ok {
				log.Infof("config: community %s user_role %s → %s ✓", comm.Name, roleID, name)
			} else {
				log.Warnf("config: community %s user_role %s — NOT FOUND in any guild", comm.Name, roleID)
			}
		}
	}
}

// Session returns the underlying discordgo session, or nil if the bot is not running.
// Used by API endpoints that need to make Discord REST calls.
func (b *Bot) Session() *discordgo.Session {
	return b.session
}

// Close disconnects the Discord gateway and stops background goroutines.
func (b *Bot) Close() {
	if b.stopCh != nil {
		close(b.stopCh)
	}
	if b.session != nil {
		b.session.Close()
	}
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore own messages
	if m.Author.ID == s.State.User.ID {
		return
	}
	// Ignore other bots (unless admin)
	if m.Author.Bot && !bot.IsAdmin(b.Cfg, "discord", m.Author.ID) {
		return
	}

	// Parse commands
	parsed := b.Parser.Parse(m.Content)
	if len(parsed) == 0 {
		// No prefix match — try NLP suggestion for DMs
		isDM := m.GuildID == ""
		if isDM && b.nlpParser != nil && b.Cfg.AI.SuggestOnDM {
			result := b.nlpParser.Parse(m.Content)
			prefix := b.Cfg.Discord.Prefix
			if prefix == "" {
				prefix = "!"
			}
			suggestion := commands.FormatNLPSuggestion(result, prefix)
			if suggestion != "" {
				s.ChannelMessageSend(m.ChannelID, suggestion)
			}
		}
		return
	}

	isDM := m.GuildID == ""
	guildID := m.GuildID
	channelID := m.ChannelID

	// Log DMs to configured log channel
	if isDM && b.Cfg.Discord.DmLogChannelID != "" {
		logMsg := fmt.Sprintf("DM from %s (%s): %s", m.Author.Username, m.Author.ID, m.Content)
		s.ChannelMessageSend(b.Cfg.Discord.DmLogChannelID, logMsg)
	}

	// Look up user state
	userLang, profileNo, hasLocation, hasArea, isRegistered := bot.LookupUserStateFromStore(b.Humans, m.Author.ID, b.Cfg.General.Locale)
	isAdmin := bot.IsAdmin(b.Cfg, "discord", m.Author.ID)

	// User roles are lazily fetched on first need (avoids REST call for disabled/unrecognised commands).
	var userRoles []string
	rolesFetched := false
	fetchRoles := func() []string {
		if rolesFetched {
			return userRoles
		}
		rolesFetched = true
		if guildID != "" {
			// Try gateway state cache first.
			if member, err := s.State.Member(guildID, m.Author.ID); err == nil && member != nil {
				userRoles = member.Roles
			} else if member, err := s.GuildMember(guildID, m.Author.ID); err == nil && member != nil {
				userRoles = member.Roles
			}
		}
		return userRoles
	}

	// Determine target type
	targetType := bot.TypeDiscordUser
	if !isDM {
		targetType = bot.TypeDiscordChannel
	}

	// Get geofence data
	var spatialIndex *geofence.SpatialIndex
	var fences []geofence.Fence
	st := b.StateMgr.Get()
	if st != nil {
		spatialIndex = st.Geofence
		fences = st.Fences
	}

	tr := b.Translations.For(userLang)

	// Merge consecutive cmd.apply pipe groups back into single invocations.
	// The parser splits "!apply t1 | track pikachu" into separate ParsedCommands,
	// but apply needs all pipe groups at once.
	parsed = bot.MergeApplyGroups(parsed)

	for _, cmd := range parsed {
		// Check disabled commands
		if bot.IsCommandDisabled(b.Cfg.General.DisabledCommands, cmd.CommandKey) {
			continue
		}

		// Try Discord-specific commands first (require discordgo session directly)
		if b.handleDiscordCommand(s, m, cmd.CommandKey, cmd.Args, isDM) {
			continue
		}

		if cmd.CommandKey == "" {
			if isDM {
				// Try NLP suggestion for unrecognised DM commands
				if b.nlpParser != nil && b.Cfg.AI.SuggestOnDM {
					result := b.nlpParser.Parse(m.Content)
					suggestion := commands.FormatNLPSuggestion(result, b.Cfg.Discord.Prefix)
					if suggestion != "" {
						s.ChannelMessageSend(channelID, suggestion)
						continue
					}
				}
				s.ChannelMessageSend(channelID, tr.Tf("cmd.unknown", b.Cfg.Discord.Prefix+"help"))
			}
			continue
		}

		handler := b.Registry.Lookup(cmd.CommandKey)
		if handler == nil {
			continue
		}

		// Registration check — skip for poracle (registration), poracle_test, and version commands
		if !isRegistered && cmd.CommandKey != "cmd.poracle" && cmd.CommandKey != "cmd.version" {
			s.ChannelMessageSend(channelID, tr.T("cmd.not_registered"))
			continue
		}

		// Check command security with user roles
		if !bot.CommandAllowed(b.Cfg, "discord", cmd.CommandKey, m.Author.ID, fetchRoles()) {
			s.MessageReactionAdd(channelID, m.ID, "🙅")
			continue
		}

		ctx := &bot.CommandContext{
			UserID:       m.Author.ID,
			UserName:     m.Author.Username,
			Platform:     "discord",
			ChannelID:    channelID,
			GuildID:      guildID,
			IsDM:         isDM,
			IsAdmin:      isAdmin,
			Language:     userLang,
			ProfileNo:    profileNo,
			HasLocation:  hasLocation,
			HasArea:      hasArea,
			TargetID:     m.Author.ID,
			TargetName:   m.Author.Username,
			TargetType:   targetType,
			AreaLogic:    bot.NewAreaLogic(fences, b.Cfg),
			DB:           b.DB,
			Humans:       b.Humans,
			Tracking:     b.Tracking,
			Config:       b.Cfg,
			StateMgr:     b.StateMgr,
			GameData:     b.GameData,
			Translations: b.Translations,
			Geofence:     spatialIndex,
			Fences:       fences,
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
			NLP:          b.nlpParser,
			Registry:     b.Registry,
			ReloadFunc:   b.ReloadFunc,
		}

		// Populate delegated admin permissions
		ctx.Permissions.ChannelTracking = bot.CalculateChannelPermissions(
			b.Cfg, m.Author.ID, fetchRoles(), channelID, guildID, "")

		// Handle target override
		target, remainingArgs, err := bot.BuildTarget(ctx, cmd.Args)
		if err != nil {
			s.MessageReactionAdd(channelID, m.ID, "🙅")
			s.ChannelMessageSend(channelID, err.Error())
			continue
		}
		if target != nil {
			ctx.TargetID = target.ID
			ctx.TargetName = target.Name
			ctx.TargetType = target.Type
			if target.Language != "" {
				ctx.Language = target.Language
			}
			ctx.ProfileNo = target.ProfileNo
			ctx.HasLocation = target.HasLocation
			ctx.HasArea = target.HasArea
			if target.ExecutionMessage != "" {
				s.ChannelMessageSend(channelID, target.ExecutionMessage)
			}
		}

		replies := handler.Run(ctx, remainingArgs)
		b.sendReplies(s, m, replies)
	}
}

// handleDiscordCommand dispatches Discord-specific commands that require the
// discordgo session directly. Returns true if the command was handled.
func (b *Bot) handleDiscordCommand(s *discordgo.Session, m *discordgo.MessageCreate, cmdKey string, args []string, isDM bool) bool {
	switch cmdKey {
	case "cmd.channel":
		b.handleChannel(s, m, args)
		return true
	case "cmd.poracle_clean":
		b.handleClean(s, m)
		return true
	case "cmd.poracle_id":
		b.handleIDExport(s, m, args)
		return true
	case "cmd.webhook":
		b.handleWebhook(s, m, args)
		return true
	case "cmd.role":
		b.handleRole(s, m, args)
		return true
	case "cmd.poracle_emoji":
		b.handleEmoji(s, m, args)
		return true
	case "cmd.autocreate":
		b.handleAutocreate(s, m, args)
		return true
	default:
		return false
	}
}

func (b *Bot) sendReplies(s *discordgo.Session, m *discordgo.MessageCreate, replies []bot.Reply) {
	for _, reply := range replies {
		// React
		if reply.React != "" {
			s.MessageReactionAdd(m.ChannelID, m.ID, reply.React)
		}

		targetChannel := m.ChannelID
		if reply.IsDM && m.GuildID != "" {
			ch, err := s.UserChannelCreate(m.Author.ID)
			if err != nil {
				log.Warnf("discord bot: create DM channel: %v", err)
				continue
			}
			targetChannel = ch.ID
		}

		// File attachment
		if reply.Attachment != nil {
			msgSend := &discordgo.MessageSend{
				Content: reply.Text,
				Files: []*discordgo.File{{
					Name:   reply.Attachment.Filename,
					Reader: bytes.NewReader(reply.Attachment.Content),
				}},
			}
			if _, err := s.ChannelMessageSendComplex(targetChannel, msgSend); err != nil {
				log.Warnf("discord bot: send attachment: %v", err)
			}
			continue
		}

		// Text message
		if reply.Text != "" {
			messages := bot.SplitMessage(reply.Text, 2000)
			for _, msg := range messages {
				s.ChannelMessageSend(targetChannel, msg)
			}
		}

		// Embed — the JSON is a full Discord message structure:
		// {"content": "...", "embed": {...}, "embeds": [{...}]}
		if len(reply.Embed) > 0 {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(reply.Embed, &raw); err != nil {
				log.Warnf("discord bot: parse embed JSON: %v", err)
			} else {
				msg := &discordgo.MessageSend{}

				// Content
				if c, ok := raw["content"]; ok {
					var content string
					json.Unmarshal(c, &content)
					msg.Content = content
				}

				// Embed (singular) → embeds array
				if e, ok := raw["embed"]; ok {
					var embed discordgo.MessageEmbed
					if json.Unmarshal(e, &embed) == nil {
						msg.Embeds = []*discordgo.MessageEmbed{&embed}
					}
				}

				// Embeds (plural)
				if e, ok := raw["embeds"]; ok {
					var embeds []*discordgo.MessageEmbed
					if json.Unmarshal(e, &embeds) == nil {
						msg.Embeds = embeds
					}
				}

				if msg.Content != "" || len(msg.Embeds) > 0 {
					if _, err := s.ChannelMessageSendComplex(targetChannel, msg); err != nil {
						log.Warnf("discord bot: send embed: %v", err)
					}
				}
			}
		}

		// Image
		if reply.ImageURL != "" {
			embed := &discordgo.MessageEmbed{
				Image: &discordgo.MessageEmbedImage{URL: reply.ImageURL},
			}
			if reply.Text != "" {
				embed.Description = reply.Text
			}
			if _, err := s.ChannelMessageSendEmbed(targetChannel, embed); err != nil {
				log.Warnf("discord bot: send image embed: %v", err)
			}
		}
	}
}

// onGuildMemberUpdate is called when a guild member's roles change.
func (b *Bot) onGuildMemberUpdate(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
	if b.reconciliation == nil || !b.Cfg.Discord.CheckRole {
		return
	}
	if m.User == nil {
		return
	}
	go b.reconciliation.ReconcileSingleUser(m.User.ID, b.Cfg.Reconciliation.Discord.RemoveInvalidUsers)
}

// onGuildMemberRemove is called when a member leaves a guild.
func (b *Bot) onGuildMemberRemove(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
	if b.reconciliation == nil || !b.Cfg.Discord.CheckRole {
		return
	}
	if m.User == nil {
		return
	}
	go b.reconciliation.ReconcileSingleUser(m.User.ID, b.Cfg.Reconciliation.Discord.RemoveInvalidUsers)
}

// onChannelDelete is called when a Discord channel is deleted.
// If the channel was registered as a tracking target, disable it.
func (b *Bot) onChannelDelete(s *discordgo.Session, ch *discordgo.ChannelDelete) {
	if ch == nil {
		return
	}
	_, err := b.DB.Exec(
		`UPDATE humans SET admin_disable = 1, disabled_date = NOW() WHERE id = ? AND type = 'discord:channel'`,
		ch.ID)
	if err != nil {
		log.Warnf("discord bot: disable deleted channel %s: %v", ch.ID, err)
	}
}

// reconciliationLoop runs periodic reconciliation at the configured interval.
func (b *Bot) reconciliationLoop() {
	interval := time.Duration(b.Cfg.Discord.CheckRoleInterval) * time.Hour
	if interval <= 0 {
		interval = 6 * time.Hour
	}

	// Run once at startup after a short delay to let the gateway settle.
	select {
	case <-b.stopCh:
		return
	case <-time.After(30 * time.Second):
	}
	b.runReconciliation()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			b.runReconciliation()
		}
	}
}

// runReconciliation executes a full reconciliation cycle.
func (b *Bot) runReconciliation() {
	rcfg := b.Cfg.Reconciliation.Discord
	b.reconciliation.SyncDiscordRole(rcfg.RegisterNewUsers, rcfg.UpdateUserNames, rcfg.RemoveInvalidUsers)
	b.reconciliation.SyncDiscordChannels(rcfg.UpdateChannelNames, rcfg.UpdateChannelNotes, rcfg.UnregisterMissingChannels)
}
