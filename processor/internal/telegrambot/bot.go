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

	// Validate configured Telegram IDs
	b.validateConfig()

	// Initialize reconciliation if check_role is enabled.
	if cfg.Cfg.Telegram.CheckRole && cfg.DTS != nil {
		b.reconciliation = NewTelegramReconciliation(api, cfg.DB, cfg.Cfg, cfg.Translations, cfg.DTS)
		go b.reconciliationLoop()
	}

	go b.pollUpdates()
	return b, nil
}

// validateConfig checks all configured Telegram IDs (channels, groups, communities)
// by calling getChat and logs whether they resolve.
func (b *Bot) validateConfig() {
	checkChat := func(label, chatIDStr string) {
		chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			log.Warnf("config: %s %s — invalid ID", label, chatIDStr)
			return
		}
		chatCfg := tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}}
		chat, err := b.api.GetChat(chatCfg)
		if err != nil {
			log.Warnf("config: %s %s — NOT ACCESSIBLE (bot may not be a member): %v", label, chatIDStr, err)
			return
		}
		name := chat.Title
		if name == "" {
			name = chat.UserName
		}
		if name == "" {
			name = chat.FirstName
		}
		log.Infof("config: %s %s → %s (%s) ✓", label, chatIDStr, name, chat.Type)
	}

	// Registration channels
	for _, chID := range b.Cfg.Telegram.Channels {
		checkChat("telegram.channels", chID)
	}

	// Community channels (area security)
	for _, comm := range b.Cfg.Area.Communities {
		for _, chID := range comm.Telegram.Channels {
			checkChat(fmt.Sprintf("community %s telegram channel", comm.Name), chID)
		}
	}

	// resolveUser describes a Telegram user ID by fetching their name via getChat.
	resolveUser := func(idStr string) string {
		chatID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return idStr
		}
		chatCfg := tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}}
		chat, err := b.api.GetChat(chatCfg)
		if err != nil {
			return idStr
		}
		name := chat.FirstName
		if chat.LastName != "" {
			name += " " + chat.LastName
		}
		if name == "" {
			name = chat.UserName
		}
		if name == "" {
			name = chat.Title
		}
		if name == "" {
			return idStr
		}
		return fmt.Sprintf("%s (%s)", name, idStr)
	}

	// Log admin list
	if len(b.Cfg.Telegram.Admins) > 0 {
		var descs []string
		for _, id := range b.Cfg.Telegram.Admins {
			descs = append(descs, resolveUser(id))
		}
		log.Infof("config: telegram.admins: %s", strings.Join(descs, ", "))
	} else {
		log.Warnf("config: telegram.admins is empty — no Telegram admins configured")
	}

	// Log delegated admins (channel tracking)
	for target, admins := range b.Cfg.Telegram.DelegatedAdministration.ChannelTracking {
		targetDesc := target
		chatID, err := strconv.ParseInt(target, 10, 64)
		if err == nil {
			chatCfg := tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}}
			if chat, err := b.api.GetChat(chatCfg); err == nil {
				name := chat.Title
				if name == "" {
					name = chat.UserName
				}
				targetDesc = fmt.Sprintf("%s (%s, %s)", name, target, chat.Type)
			}
		}
		var adminDescs []string
		for _, id := range admins {
			adminDescs = append(adminDescs, resolveUser(id))
		}
		log.Infof("config: telegram.delegated_admins target %s → admins: %s", targetDesc, strings.Join(adminDescs, ", "))
	}

	// Log user tracking admins
	if len(b.Cfg.Telegram.DelegatedAdministration.UserTracking) > 0 {
		var descs []string
		for _, id := range b.Cfg.Telegram.DelegatedAdministration.UserTracking {
			descs = append(descs, resolveUser(id))
		}
		log.Infof("config: telegram.user_tracking_admins: %s", strings.Join(descs, ", "))
	}
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

	userLang, profileNo, hasLocation, hasArea, isRegistered := bot.LookupUserStateFromStore(b.Humans, userID, b.Cfg.General.Locale)
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

		// register_on_start: /start triggers registration like /poracle
		if !isRegistered && cmd.CommandKey == "cmd.start" && b.Cfg.Telegram.RegisterOnStart {
			cmd.CommandKey = "cmd.poracle"
		}

		// Registration check — skip for poracle (registration), poracle_test, and version commands
		if !isRegistered && cmd.CommandKey != "cmd.poracle" && cmd.CommandKey != "cmd.version" {
			if customMsg := b.Cfg.Telegram.UnregisteredUserMessage; customMsg != "" {
				b.api.Send(tgbotapi.NewMessage(chatID, customMsg))
			} else {
				tr := b.Translations.For(userLang)
				b.api.Send(tgbotapi.NewMessage(chatID, tr.T("msg.not_registered")))
			}
			continue
		}

		if cmd.CommandKey == "" {
			if isDM {
				// Try NLP suggestion for unrecognised DM commands
				if b.nlpParser != nil && b.Cfg.AI.SuggestOnDM {
					result := b.nlpParser.Parse(text)
					suggestion := commands.FormatNLPSuggestion(result, "/")
					if suggestion != "" {
						b.api.Send(tgbotapi.NewMessage(chatID, suggestion))
						continue
					}
				}
				if customMsg := b.Cfg.Telegram.UnrecognisedCommandMessage; customMsg != "" {
					b.api.Send(tgbotapi.NewMessage(chatID, customMsg))
				}
			}
			continue
		}

		handler := b.Registry.Lookup(cmd.CommandKey)
		if handler == nil {
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
			NLP:           b.nlpParser,
			TestProcessor: b.TestProcessor,
			Registry:      b.Registry,
			ReloadFunc:    b.ReloadFunc,
		}

		// Populate delegated admin permissions
		ctx.Permissions.UserTracking = bot.CanTrackUsers(b.Cfg, "telegram", userID, nil)

		// Set language hint from available_languages
		ctx.SetLanguageHint(cmd.LanguageHint)

		// Handle target override.
		// /poracle skips BuildTarget — it's a registration command that always
		// targets the sender and has its own channel validation internally.
		var remainingArgs []string
		if cmd.CommandKey == "cmd.poracle" {
			remainingArgs = cmd.Args
		} else {
			target, args, err := bot.BuildTarget(ctx, cmd.Args)
			if err != nil {
				msg := tgbotapi.NewMessage(chatID, err.Error())
				b.api.Send(msg)
				continue
			}
			remainingArgs = args
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
		}

		replies := handler.Run(ctx, remainingArgs)
		b.sendReplies(chatID, m.From.ID, replies)
	}
}

func (b *Bot) sendReplies(chatID int64, userID int64, replies []bot.Reply) {
	for _, reply := range replies {
		// IsDM replies go to the user's private chat, not the group
		targetChat := chatID
		if reply.IsDM && chatID != userID {
			targetChat = userID
		}

		// React — Telegram doesn't support message reactions, send as text
		if reply.React != "" && reply.Text == "" && len(reply.Embed) == 0 && reply.Attachment == nil {
			msg := tgbotapi.NewMessage(targetChat, reply.React)
			b.api.Send(msg)
			continue
		}

		// Image URL — send as photo (e.g. area map tiles)
		if reply.ImageURL != "" {
			photo := tgbotapi.NewPhoto(targetChat, tgbotapi.FileURL(reply.ImageURL))
			if reply.Text != "" {
				photo.Caption = reply.Text
			}
			if _, err := b.api.Send(photo); err != nil {
				log.Warnf("telegram bot: send photo failed, falling back to text: %v", err)
				// Fall through to text handler below
			} else {
				continue
			}
		}

		// File attachment
		if reply.Attachment != nil {
			doc := tgbotapi.NewDocument(targetChat, tgbotapi.FileBytes{
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
				msg := tgbotapi.NewMessage(targetChat, text)
				msg.ParseMode = "Markdown"
				if _, err := b.api.Send(msg); err != nil {
					// Retry without parse mode in case the text has invalid Markdown
					msg.ParseMode = ""
					if _, err2 := b.api.Send(msg); err2 != nil {
						log.Warnf("telegram bot: send message: %v", err2)
					}
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
