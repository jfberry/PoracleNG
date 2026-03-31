// Package telegrambot runs a Telegram bot using go-telegram-bot-api for polling.
// It receives messages, parses commands, executes them via the bot framework,
// and sends replies back to the chat.
package telegrambot

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geocoding"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// Bot is the Telegram bot polling handler.
type Bot struct {
	api        *tgbotapi.BotAPI
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
	weather    *tracker.WeatherTracker
	stats      *tracker.StatsTracker
	dts        *dts.TemplateStore
	emoji      *dts.EmojiLookup
	reloadFunc func()
	stopCh     chan struct{}
}

// Config holds everything needed to create a Telegram bot.
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
	Weather      *tracker.WeatherTracker
	Stats        *tracker.StatsTracker
	DTS          *dts.TemplateStore
	Emoji        *dts.EmojiLookup
	ReloadFunc   func()
}

// New creates and starts a Telegram bot. Returns the bot (for shutdown) or an error.
func New(cfg Config) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, err
	}

	b := &Bot{
		api:          api,
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
		reloadFunc:   cfg.ReloadFunc,
		stopCh:       make(chan struct{}),
	}

	log.Infof("Telegram bot connected as @%s", api.Self.UserName)

	go b.pollUpdates()
	return b, nil
}

// Close stops the polling loop.
func (b *Bot) Close() {
	close(b.stopCh)
	b.api.StopReceivingUpdates()
}

func (b *Bot) pollUpdates() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-b.stopCh:
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if update.ChannelPost != nil {
				b.handleChannelPost(update.ChannelPost)
			}
			if update.Message != nil {
				b.handleMessage(update.Message)
			}
		}
	}
}

func (b *Bot) handleChannelPost(m *tgbotapi.Message) {
	if m.Text != "" && strings.HasPrefix(m.Text, "/identify") {
		reply := fmt.Sprintf("This channel is id: [ %d ] and your id is: unknown - this is a channel (and can't be used for bot registration)", m.Chat.ID)
		msg := tgbotapi.NewMessage(m.Chat.ID, reply)
		b.api.Send(msg)
	}
}

func (b *Bot) handleMessage(m *tgbotapi.Message) {
	if m.From == nil {
		return
	}

	// /identify — always respond, no registration required
	if strings.HasPrefix(m.Text, "/identify") {
		var reply string
		if m.Chat.Type == "private" {
			reply = fmt.Sprintf("This is a private message and your id is: [ %d ]", m.From.ID)
		} else {
			reply = fmt.Sprintf("This channel is id: [ %d ] and your id is: [ %d ]", m.Chat.ID, m.From.ID)
		}
		msg := tgbotapi.NewMessage(m.Chat.ID, reply)
		b.api.Send(msg)
		return
	}

	text := m.Text
	if text == "" {
		// Handle location messages as /location lat,lon
		if m.Location != nil {
			text = "/location " + formatFloat(m.Location.Latitude) + "," + formatFloat(m.Location.Longitude)
		}
		if text == "" {
			return
		}
	}

	parsed := b.parser.Parse(text)
	if len(parsed) == 0 {
		return
	}

	userID := formatInt64(m.From.ID)
	isDM := m.Chat.Type == "private"
	chatID := m.Chat.ID

	userLang, profileNo, hasLocation, hasArea, isRegistered := bot.LookupUserState(b.db, userID, b.cfg.General.Locale)
	isAdmin := bot.IsAdmin(b.cfg, "telegram", userID)

	targetType := "telegram:user"
	if !isDM {
		targetType = "telegram:group"
	}

	var spatialIndex *geofence.SpatialIndex
	var fences []geofence.Fence
	st := b.stateMgr.Get()
	if st != nil {
		spatialIndex = st.Geofence
		fences = st.Fences
	}

	for _, cmd := range parsed {
		if cmd.CommandKey == "" {
			continue // don't spam about unknown commands in Telegram groups
		}

		handler := b.registry.Lookup(cmd.CommandKey)
		if handler == nil {
			continue
		}

		// Registration check — skip for poracle (registration), poracle_test, and version commands
		if !isRegistered && cmd.CommandKey != "cmd.poracle" && cmd.CommandKey != "cmd.version" {
			tr := b.translations.For(userLang)
			replyMsg := tgbotapi.NewMessage(chatID, tr.T("cmd.not_registered"))
			b.api.Send(replyMsg)
			continue
		}

		ctx := &bot.CommandContext{
			UserID:       userID,
			UserName:     m.From.UserName,
			Platform:     "telegram",
			ChannelID:    formatInt64(chatID),
			IsDM:         isDM,
			IsAdmin:      isAdmin,
			Language:     userLang,
			ProfileNo:    profileNo,
			HasLocation:  hasLocation,
			HasArea:      hasArea,
			TargetID:     userID,
			TargetName:   m.From.FirstName,
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
			ReloadFunc:   b.reloadFunc,
		}

		// Handle target override
		target, remainingArgs, err := bot.BuildTarget(b.db, ctx, cmd.Args)
		if err != nil {
			msg := tgbotapi.NewMessage(chatID, err.Error())
			b.api.Send(msg)
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
				msg := tgbotapi.NewMessage(chatID, target.ExecutionMessage)
				b.api.Send(msg)
			}
		}

		replies := handler.Run(ctx, remainingArgs)
		b.sendReplies(chatID, replies)
	}
}

func (b *Bot) sendReplies(chatID int64, replies []bot.Reply) {
	for _, reply := range replies {
		// File attachment
		if reply.Attachment != nil {
			doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
				Name:  reply.Attachment.Filename,
				Bytes: reply.Attachment.Content,
			})
			if reply.Text != "" {
				doc.Caption = reply.Text
			}
			if _, err := b.api.Send(doc); err != nil {
				log.Warnf("telegram bot: send document: %v", err)
			}
			continue
		}

		if reply.Text != "" {
			// Split long messages at 4095 char limit
			messages := bot.SplitMessage(reply.Text, 4095)
			for _, text := range messages {
				msg := tgbotapi.NewMessage(chatID, text)
				if _, err := b.api.Send(msg); err != nil {
					log.Warnf("telegram bot: send message: %v", err)
				}
			}
		}
	}
}



func formatInt64(n int64) string {
	return strconv.FormatInt(n, 10)
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 6, 64)
}
