// Package discordbot runs a Discord bot using discordgo for gateway events.
// It receives messages, parses commands, executes them via the bot framework,
// and sends replies back to the channel or DM.
package discordbot

import (
	"bytes"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geocoding"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
)

// Bot is the Discord bot gateway handler.
type Bot struct {
	session    *discordgo.Session
	parser     *bot.Parser
	registry   *bot.Registry
	argMatcher *bot.ArgMatcher
	resolver   *bot.PokemonResolver
	db         *sqlx.DB
	cfg        *config.Config
	stateMgr   *state.Manager
	gameData   *gamedata.GameData
	translations *i18n.Bundle
	dispatcher *delivery.Dispatcher
	rowText    *rowtext.Generator
	geocoder   *geocoding.Geocoder
	staticMap  *staticmap.Resolver
	reloadFunc func()
}

// Config holds everything needed to create a Discord bot.
type Config struct {
	Token        string
	DB           *sqlx.DB
	Cfg          *config.Config
	StateMgr     *state.Manager
	GameData     *gamedata.GameData
	Translations *i18n.Bundle
	Dispatcher   *delivery.Dispatcher
	RowText      *rowtext.Generator
	Registry     *bot.Registry
	Parser       *bot.Parser
	ArgMatcher   *bot.ArgMatcher
	Resolver     *bot.PokemonResolver
	Geocoder     *geocoding.Geocoder
	StaticMap    *staticmap.Resolver
	ReloadFunc   func()
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
		reloadFunc:   cfg.ReloadFunc,
	}

	session.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsMessageContent

	session.AddHandler(b.onMessageCreate)

	if err := session.Open(); err != nil {
		return nil, err
	}

	log.Infof("Discord bot connected as %s", session.State.User.Username)
	return b, nil
}

// Close disconnects the Discord gateway.
func (b *Bot) Close() {
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
		return
	}

	isDM := m.GuildID == ""
	guildID := m.GuildID
	channelID := m.ChannelID

	// Look up user state
	userLang, profileNo, hasLocation, hasArea, isRegistered := lookupUserState(b.db, m.Author.ID, b.cfg.General.Locale)
	isAdmin := bot.IsAdmin(b.cfg, "discord", m.Author.ID)

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

	for _, cmd := range parsed {
		if cmd.CommandKey == "" {
			if isDM {
				s.ChannelMessageSend(channelID, "Unknown command. Try `"+b.cfg.Discord.Prefix+"help`")
			}
			continue
		}

		handler := b.registry.Lookup(cmd.CommandKey)
		if handler == nil {
			continue
		}

		// Registration check — skip for poracle (registration), poracle_test, and version commands
		if !isRegistered && cmd.CommandKey != "cmd.poracle" && cmd.CommandKey != "cmd.poracle_test" && cmd.CommandKey != "cmd.version" {
			tr := b.translations.For(userLang)
			s.ChannelMessageSend(channelID, tr.T("cmd.not_registered"))
			continue
		}

		// Check command security
		// TODO: fetch user roles from guild for full command_security check
		if !bot.CommandAllowed(b.cfg, "discord", cmd.CommandKey, m.Author.ID, nil) {
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
			ReloadFunc:   b.reloadFunc,
		}

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
		}

		replies := handler.Run(ctx, remainingArgs)
		b.sendReplies(s, m, replies)
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
			messages := splitMessage(reply.Text, 2000)
			for _, msg := range messages {
				s.ChannelMessageSend(targetChannel, msg)
			}
		}

		// Embed
		if len(reply.Embed) > 0 {
			// TODO: parse embed JSON and send as Discord embed
		}

		// Image
		if reply.ImageURL != "" {
			// TODO: send as embed with image
		}
	}
}

// splitMessage splits text into chunks that fit within maxLen, splitting at newlines.
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var messages []string
	lines := strings.Split(text, "\n")
	var current strings.Builder

	for _, line := range lines {
		if current.Len()+len(line)+1 > maxLen && current.Len() > 0 {
			messages = append(messages, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		messages = append(messages, current.String())
	}

	return messages
}

// lookupUserState loads basic user info for command context building.
// isRegistered is true when the user exists in the humans table.
func lookupUserState(database *sqlx.DB, userID, defaultLocale string) (lang string, profileNo int, hasLocation, hasArea, isRegistered bool) {
	lang = defaultLocale
	profileNo = 1

	var h struct {
		Language  *string `db:"language"`
		ProfileNo int     `db:"current_profile_no"`
		Latitude  float64 `db:"latitude"`
		Longitude float64 `db:"longitude"`
		Area      *string `db:"area"`
	}
	err := database.Get(&h, "SELECT language, current_profile_no, latitude, longitude, area FROM humans WHERE id = ? LIMIT 1", userID)
	if err != nil {
		return
	}

	isRegistered = true
	if h.Language != nil && *h.Language != "" {
		lang = *h.Language
	}
	profileNo = h.ProfileNo
	hasLocation = h.Latitude != 0 || h.Longitude != 0
	hasArea = h.Area != nil && *h.Area != "" && *h.Area != "[]"
	return
}
