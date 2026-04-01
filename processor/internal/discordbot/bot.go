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
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/bot/commands"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geocoding"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/nlp"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// Bot is the Discord bot gateway handler.
type Bot struct {
	session        *discordgo.Session
	parser         *bot.Parser
	registry       *bot.Registry
	argMatcher     *bot.ArgMatcher
	resolver       *bot.PokemonResolver
	db             *sqlx.DB
	cfg            *config.Config
	stateMgr       *state.Manager
	gameData       *gamedata.GameData
	translations   *i18n.Bundle
	dispatcher     *delivery.Dispatcher
	rowText        *rowtext.Generator
	geocoder       *geocoding.Geocoder
	staticMap      *staticmap.Resolver
	weather        *tracker.WeatherTracker
	stats          *tracker.StatsTracker
	dts            *dts.TemplateStore
	emoji          *dts.EmojiLookup
	nlpParser      *nlp.Parser
	reloadFunc     func()
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
		session:      session,
		parser:       cfg.Parser,
		registry:     cfg.Registry,
		argMatcher:   cfg.ArgMatcher,
		resolver:     cfg.Resolver,
		db:           cfg.DB,
		cfg:          cfg.Cfg,
		stateMgr:     cfg.StateMgr,
		gameData:     cfg.GameData,
		translations: cfg.Translations,
		dispatcher:   cfg.Dispatcher,
		rowText:      cfg.RowText,
		geocoder:     cfg.Geocoder,
		staticMap:    cfg.StaticMap,
		weather:      cfg.Weather,
		stats:        cfg.Stats,
		dts:          cfg.DTS,
		emoji:        cfg.Emoji,
		nlpParser:    cfg.NLPParser,
		reloadFunc:   cfg.ReloadFunc,
		stopCh:       make(chan struct{}),
	}

	// Note: field access works via the embedded BotDeps struct

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

	// Initialize reconciliation if check_role is enabled.
	if cfg.Cfg.Discord.CheckRole && cfg.DTS != nil {
		b.reconciliation = NewReconciliation(session, cfg.DB, cfg.Cfg, cfg.Translations, cfg.DTS)
		go b.reconciliationLoop()
	}

	return b, nil
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
	if m.Author.Bot && !bot.IsAdmin(b.cfg, "discord", m.Author.ID) {
		return
	}

	// Parse commands
	parsed := b.parser.Parse(m.Content)
	if len(parsed) == 0 {
		// No prefix match — try NLP suggestion for DMs
		isDM := m.GuildID == ""
		if isDM && b.nlpParser != nil && b.cfg.AI.SuggestOnDM {
			result := b.nlpParser.Parse(m.Content)
			prefix := b.cfg.Discord.Prefix
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
	if isDM && b.cfg.Discord.DmLogChannelID != "" {
		logMsg := fmt.Sprintf("DM from %s (%s): %s", m.Author.Username, m.Author.ID, m.Content)
		s.ChannelMessageSend(b.cfg.Discord.DmLogChannelID, logMsg)
	}

	// Look up user state
	userLang, profileNo, hasLocation, hasArea, isRegistered := bot.LookupUserState(b.db, m.Author.ID, b.cfg.General.Locale)
	isAdmin := bot.IsAdmin(b.cfg, "discord", m.Author.ID)

	// Fetch user's Discord roles for command security and delegated admin checks
	var userRoles []string
	if guildID != "" {
		member, err := s.GuildMember(guildID, m.Author.ID)
		if err == nil && member != nil {
			userRoles = member.Roles
		}
	}

	// Determine target type
	targetType := "discord:user"
	if !isDM {
		targetType = "discord:channel"
	}

	// Get geofence data
	var spatialIndex *geofence.SpatialIndex
	var fences []geofence.Fence
	st := b.stateMgr.Get()
	if st != nil {
		spatialIndex = st.Geofence
		fences = st.Fences
	}

	tr := b.translations.For(userLang)

	// Merge consecutive cmd.apply pipe groups back into single invocations.
	// The parser splits "!apply t1 | track pikachu" into separate ParsedCommands,
	// but apply needs all pipe groups at once.
	parsed = bot.MergeApplyGroups(parsed)

	for _, cmd := range parsed {
		// Check disabled commands
		if isCommandDisabled(b.cfg.General.DisabledCommands, cmd.CommandKey) {
			continue
		}

		// Try Discord-specific commands first (require discordgo session directly)
		if b.handleDiscordCommand(s, m, cmd.CommandKey, cmd.Args, isDM) {
			continue
		}

		if cmd.CommandKey == "" {
			if isDM {
				// Try NLP suggestion for unrecognised DM commands
				if b.nlpParser != nil && b.cfg.AI.SuggestOnDM {
					result := b.nlpParser.Parse(m.Content)
					suggestion := commands.FormatNLPSuggestion(result, b.cfg.Discord.Prefix)
					if suggestion != "" {
						s.ChannelMessageSend(channelID, suggestion)
						continue
					}
				}
				s.ChannelMessageSend(channelID, tr.Tf("cmd.unknown", b.cfg.Discord.Prefix+"help"))
			}
			continue
		}

		handler := b.registry.Lookup(cmd.CommandKey)
		if handler == nil {
			continue
		}

		// Registration check — skip for poracle (registration), poracle_test, and version commands
		if !isRegistered && cmd.CommandKey != "cmd.poracle" && cmd.CommandKey != "cmd.version" {
			s.ChannelMessageSend(channelID, tr.T("cmd.not_registered"))
			continue
		}

		// Check command security with user roles
		if !bot.CommandAllowed(b.cfg, "discord", cmd.CommandKey, m.Author.ID, userRoles) {
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
			DB:           b.db,
			Config:       b.cfg,
			StateMgr:     b.stateMgr,
			GameData:     b.gameData,
			Translations: b.translations,
			Geofence:     spatialIndex,
			Fences:       fences,
			Dispatcher:   b.dispatcher,
			RowText:      b.rowText,
			Resolver:     b.resolver,
			ArgMatcher:   b.argMatcher,
			Geocoder:     b.geocoder,
			StaticMap:    b.staticMap,
			Weather:      b.weather,
			Stats:        b.stats,
			DTS:          b.dts,
			Emoji:        b.emoji,
			NLP:          b.nlpParser,
			Registry:     b.registry,
			ReloadFunc:   b.reloadFunc,
		}

		// Populate delegated admin permissions
		ctx.Permissions.ChannelTracking = bot.CalculateChannelPermissions(
			b.cfg, m.Author.ID, userRoles, channelID, guildID, "")

		// Handle target override
		target, remainingArgs, err := bot.BuildTarget(b.db, ctx, cmd.Args)
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

// isCommandDisabled checks if a command key (e.g. "cmd.track") matches any entry
// in the disabled_commands list (which uses short names like "track", "raid").
func isCommandDisabled(disabled []string, cmdKey string) bool {
	if len(disabled) == 0 {
		return false
	}
	cmdName := strings.TrimPrefix(cmdKey, "cmd.")
	for _, d := range disabled {
		if d == cmdName {
			return true
		}
	}
	return false
}

// onGuildMemberUpdate is called when a guild member's roles change.
func (b *Bot) onGuildMemberUpdate(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
	if b.reconciliation == nil || !b.cfg.Discord.CheckRole {
		return
	}
	if m.User == nil {
		return
	}
	go b.reconciliation.ReconcileSingleUser(m.User.ID, b.cfg.Reconciliation.Discord.RemoveInvalidUsers)
}

// onGuildMemberRemove is called when a member leaves a guild.
func (b *Bot) onGuildMemberRemove(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
	if b.reconciliation == nil || !b.cfg.Discord.CheckRole {
		return
	}
	if m.User == nil {
		return
	}
	go b.reconciliation.ReconcileSingleUser(m.User.ID, b.cfg.Reconciliation.Discord.RemoveInvalidUsers)
}

// onChannelDelete is called when a Discord channel is deleted.
// If the channel was registered as a tracking target, disable it.
func (b *Bot) onChannelDelete(s *discordgo.Session, ch *discordgo.ChannelDelete) {
	if ch == nil {
		return
	}
	_, err := b.db.Exec(
		`UPDATE humans SET admin_disable = 1, disabled_date = NOW() WHERE id = ? AND type = 'discord:channel'`,
		ch.ID)
	if err != nil {
		log.Warnf("discord bot: disable deleted channel %s: %v", ch.ID, err)
	}
}

// reconciliationLoop runs periodic reconciliation at the configured interval.
func (b *Bot) reconciliationLoop() {
	interval := time.Duration(b.cfg.Discord.CheckRoleInterval) * time.Hour
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
	rcfg := b.cfg.Reconciliation.Discord
	b.reconciliation.SyncDiscordRole(rcfg.RegisterNewUsers, rcfg.UpdateUserNames, rcfg.RemoveInvalidUsers)
	b.reconciliation.SyncDiscordChannels(rcfg.UpdateChannelNames, rcfg.UpdateChannelNotes, rcfg.UnregisterMissingChannels)
}
