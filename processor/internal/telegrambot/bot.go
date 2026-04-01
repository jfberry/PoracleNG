// Package telegrambot runs a Telegram bot using go-telegram-bot-api for polling.
// It receives messages, parses commands, executes them via the bot framework,
// and sends replies back to the chat.
package telegrambot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/bot/commands"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/nlp"
)

// Bot is the Telegram bot polling handler.
type Bot struct {
	bot.BotDeps
	api            *tgbotapi.BotAPI
	nlpParser      *nlp.Parser
	reconciliation *TelegramReconciliation
	stopCh         chan struct{}
}

// Config holds everything needed to create a Telegram bot.
type Config struct {
	Token string
	bot.BotDeps
}

// New creates and starts a Telegram bot. Returns the bot (for shutdown) or an error.
func New(cfg Config) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, err
	}

	b := &Bot{
		BotDeps:   cfg.BotDeps,
		api:       api,
		nlpParser: cfg.NLPParser,
		stopCh:    make(chan struct{}),
	}

	log.Infof("Telegram bot connected as @%s", api.Self.UserName)

	// Initialize reconciliation if check_role is enabled.
	if cfg.Cfg.Telegram.CheckRole && cfg.DTS != nil {
		b.reconciliation = NewTelegramReconciliation(api, cfg.DB, cfg.Cfg, cfg.Translations, cfg.DTS)
		go b.reconciliationLoop()
	}

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

	parsed := b.Parser.Parse(text)
	if len(parsed) == 0 {
		// No prefix match — try NLP suggestion for DMs
		isDM := m.Chat.Type == "private"
		if isDM && b.nlpParser != nil && b.Cfg.AI.SuggestOnDM {
			result := b.nlpParser.Parse(text)
			suggestion := commands.FormatNLPSuggestion(result, "/")
			if suggestion != "" {
				msg := tgbotapi.NewMessage(m.Chat.ID, suggestion)
				b.api.Send(msg)
			}
		}
		return
	}

	userID := formatInt64(m.From.ID)
	isDM := m.Chat.Type == "private"
	chatID := m.Chat.ID

	userLang, profileNo, hasLocation, hasArea, isRegistered := bot.LookupUserState(b.DB, userID, b.Cfg.General.Locale)
	isAdmin := bot.IsAdmin(b.Cfg, "telegram", userID)

	targetType := bot.TypeTelegramUser
	if !isDM {
		targetType = bot.TypeTelegramGroup
	}

	var spatialIndex *geofence.SpatialIndex
	var fences []geofence.Fence
	st := b.StateMgr.Get()
	if st != nil {
		spatialIndex = st.Geofence
		fences = st.Fences
	}

	// Merge consecutive cmd.apply pipe groups back into single invocations.
	parsed = bot.MergeApplyGroups(parsed)

	for _, cmd := range parsed {
		// Check disabled commands
		if bot.IsCommandDisabled(b.Cfg.General.DisabledCommands, cmd.CommandKey) {
			continue
		}

		// Handle Telegram-specific commands first (require tgbotapi directly).
		if b.handleTelegramCommand(m, cmd.CommandKey, cmd.Args) {
			continue
		}

		if cmd.CommandKey == "" {
			// Try NLP suggestion for unrecognised DM commands
			if isDM && b.nlpParser != nil && b.Cfg.AI.SuggestOnDM {
				result := b.nlpParser.Parse(text)
				suggestion := commands.FormatNLPSuggestion(result, "/")
				if suggestion != "" {
					msg := tgbotapi.NewMessage(chatID, suggestion)
					b.api.Send(msg)
					continue
				}
			}
			continue // don't spam about unknown commands in Telegram groups
		}

		handler := b.Registry.Lookup(cmd.CommandKey)
		if handler == nil {
			continue
		}

		// Registration check — skip for poracle (registration), poracle_test, and version commands
		if !isRegistered && cmd.CommandKey != "cmd.poracle" && cmd.CommandKey != "cmd.version" {
			tr := b.Translations.For(userLang)
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
			DB:           b.DB,
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

		// Handle target override
		target, remainingArgs, err := bot.BuildTarget(b.DB, ctx, cmd.Args)
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



// reconciliationLoop runs periodic reconciliation at the configured interval.
func (b *Bot) reconciliationLoop() {
	interval := time.Duration(b.Cfg.Telegram.CheckRoleInterval) * time.Hour
	if interval <= 0 {
		interval = 6 * time.Hour
	}

	// Run once at startup after a short delay.
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

// runReconciliation executes a full Telegram reconciliation cycle.
func (b *Bot) runReconciliation() {
	rcfg := b.Cfg.Reconciliation.Telegram
	b.reconciliation.SyncTelegramUsers(rcfg.UpdateUserNames, rcfg.RemoveInvalidUsers)
	b.reconciliation.UpdateTelegramChannels()
}

// handleTelegramCommand dispatches Telegram-specific commands that require the
// tgbotapi directly. Returns true if the command was handled.
func (b *Bot) handleTelegramCommand(m *tgbotapi.Message, cmdKey string, args []string) bool {
	switch cmdKey {
	case "cmd.channel":
		b.handleChannel(m, args)
		return true
	default:
		return false
	}
}

func formatInt64(n int64) string {
	return strconv.FormatInt(n, 10)
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 6, 64)
}
