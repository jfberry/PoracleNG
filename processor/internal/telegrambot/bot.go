// Package telegrambot runs a Telegram bot using go-telegram/bot for polling.
// It receives messages, parses commands, executes them via the bot framework,
// and sends replies back to the chat.
package telegrambot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	gotgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/bot/commands"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/nlp"
)

// Bot is the Telegram bot polling handler.
type Bot struct {
	bot.BotDeps
	api            *gotgbot.Bot
	username       string // resolved at startup via getMe
	nlpParser      *nlp.Parser
	reconciliation *TelegramReconciliation
	cancelStart    context.CancelFunc
	stopCh         chan struct{}
}

// Config holds everything needed to create a Telegram bot.
type Config struct {
	Token string
	bot.BotDeps
}

// New creates and starts a Telegram bot. Returns the bot (for shutdown) or an error.
func New(cfg Config) (*Bot, error) {
	b := &Bot{
		BotDeps:   cfg.BotDeps,
		nlpParser: cfg.NLPParser,
		stopCh:    make(chan struct{}),
	}

	api, err := gotgbot.New(cfg.Token, gotgbot.WithDefaultHandler(b.handleUpdate))
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}
	b.api = api

	// Resolve our own username for log lines and config validation echo.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if me, err := api.GetMe(ctx); err == nil && me != nil {
		b.username = me.Username
	}
	log.Infof("Telegram bot connected as @%s", b.username)

	// Validate configured Telegram IDs
	b.validateConfig()

	// Initialize reconciliation — needed for check_role periodic sync AND
	// for /start DM registration (verifies channel membership via API).
	if cfg.DTS != nil {
		b.reconciliation = NewTelegramReconciliation(api, cfg.Humans, cfg.Cfg, cfg.Translations, cfg.DTS)
		if cfg.Cfg.Telegram.CheckRole {
			go b.reconciliationLoop()
		}
	}

	// Start polling in the background. Cancelling the context stops it.
	startCtx, cancelStart := context.WithCancel(context.Background())
	b.cancelStart = cancelStart
	go api.Start(startCtx)

	return b, nil
}

// validateConfig checks all configured Telegram IDs (channels, groups, communities)
// by calling getChat and logs whether they resolve.
func (b *Bot) validateConfig() {
	getChat := func(chatID int64) (*models.ChatFullInfo, error) {
		ctx, cancel := requestCtx()
		defer cancel()
		return b.api.GetChat(ctx, &gotgbot.GetChatParams{ChatID: chatID})
	}

	checkChat := func(label, chatIDStr string) {
		chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			log.Warnf("config: %s %s — invalid ID", label, chatIDStr)
			return
		}
		chat, err := getChat(chatID)
		if err != nil {
			log.Warnf("config: %s %s — NOT ACCESSIBLE (bot may not be a member): %v", label, chatIDStr, err)
			return
		}
		name := chat.Title
		if name == "" {
			name = chat.Username
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
		chat, err := getChat(chatID)
		if err != nil {
			return idStr
		}
		name := chat.FirstName
		if chat.LastName != "" {
			name += " " + chat.LastName
		}
		if name == "" {
			name = chat.Username
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

	// Community admins (area security) — resolve each so operators can spot
	// typos the same way they do for community channels.
	for _, comm := range b.Cfg.Area.Communities {
		if len(comm.Telegram.Admins) == 0 {
			continue
		}
		var descs []string
		for _, id := range comm.Telegram.Admins {
			descs = append(descs, resolveUser(id))
		}
		log.Infof("config: community %s telegram.admins: %s", comm.Name, strings.Join(descs, ", "))
	}

	// Log delegated admins (channel tracking)
	for target, admins := range b.Cfg.Telegram.DelegatedAdministration.ChannelTracking {
		targetDesc := target
		if chatID, err := strconv.ParseInt(target, 10, 64); err == nil {
			if chat, err := getChat(chatID); err == nil {
				name := chat.Title
				if name == "" {
					name = chat.Username
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

// API returns the underlying Telegram bot API, or nil if the bot is not running.
func (b *Bot) API() *gotgbot.Bot { return b.api }

// Close stops the polling loop.
func (b *Bot) Close() {
	if b.cancelStart != nil {
		b.cancelStart()
	}
	close(b.stopCh)
}

// handleUpdate is the default handler registered with the underlying
// library. It dispatches the update kind we care about and discards
// the rest. Runs in one of the library's worker goroutines.
func (b *Bot) handleUpdate(ctx context.Context, _ *gotgbot.Bot, u *models.Update) {
	if u == nil {
		return
	}
	if u.ChannelPost != nil {
		b.handleChannelPost(u.ChannelPost)
		return
	}
	if u.Message != nil {
		b.handleMessage(u.Message)
	}
}

// handleChannelPost reacts to /identify in a channel — the only
// channel-post case Poracle responds to.
func (b *Bot) handleChannelPost(m *models.Message) {
	if m.Text != "" && strings.HasPrefix(m.Text, "/identify") {
		reply := fmt.Sprintf("This channel is id: [ %d ] and your id is: unknown - this is a channel (and can't be used for bot registration)", m.Chat.ID)
		_, _ = b.sendTopicMessage(m.Chat.ID, m.MessageThreadID, reply)
	}
}

func (b *Bot) handleMessage(m *models.Message) {
	if m.From == nil {
		return
	}

	// /identify — always respond, no registration required
	if strings.HasPrefix(m.Text, "/identify") {
		var reply string
		if m.Chat.Type == "private" {
			reply = fmt.Sprintf("This is a private message and your id is: [ %d ]", m.From.ID)
		} else if m.MessageThreadID > 0 {
			reply = fmt.Sprintf("This channel is id: [ %d ], topic id: [ %d ] and your id is: [ %d ]", m.Chat.ID, m.MessageThreadID, m.From.ID)
		} else {
			reply = fmt.Sprintf("This channel is id: [ %d ] and your id is: [ %d ]", m.Chat.ID, m.From.ID)
		}
		_, _ = b.sendTopicMessage(m.Chat.ID, m.MessageThreadID, reply)
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
				_, _ = b.sendTopicMessage(m.Chat.ID, m.MessageThreadID, suggestion)
			}
		}
		return
	}

	userID := formatInt64(m.From.ID)
	isDM := m.Chat.Type == "private"
	chatID := m.Chat.ID
	threadID := m.MessageThreadID

	userLang, profileNo, hasLocation, hasArea, isRegistered := bot.LookupUserStateFromStore(b.Humans, userID, b.Cfg.General.Locale)
	isAdmin := bot.IsAdmin(b.Cfg, "telegram", userID)

	// targetType reflects where the message came from. For forum topics
	// we register and address the topic itself rather than the parent
	// supergroup; BuildTarget will look up the composite "<chatID>:<threadID>"
	// channel ID below to find the topic's human row.
	targetType := bot.TypeTelegramUser
	if !isDM {
		if threadID > 0 {
			targetType = bot.TypeTelegramTopic
		} else {
			targetType = bot.TypeTelegramGroup
		}
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
		if b.handleTelegramCommand(m, threadID, cmd.CommandKey, cmd.Args) {
			continue
		}

		// register_on_start: /start triggers registration.
		// In DMs: use reconciliation to verify channel membership via Telegram API
		// (matching PoracleJS behaviour where /start in DM checks getChatMember).
		// In groups: rewrite to cmd.poracle for normal group registration flow.
		if !isRegistered && cmd.CommandKey == "cmd.start" && b.Cfg.Telegram.RegisterOnStart {
			if isDM {
				if b.reconciliation != nil {
					log.Infof("telegram: /start DM from %s (%d), checking channel membership via reconciliation", userID, m.From.ID)
					b.reconciliation.SyncSingleUser(m.From.ID)
				}
				continue
			}
			cmd.CommandKey = "cmd.poracle"
		}

		// Registration check — skip for poracle (registration), poracle_test, and version commands
		if !isRegistered && cmd.CommandKey != "cmd.poracle" && cmd.CommandKey != "cmd.version" {
			if customMsg := b.Cfg.Telegram.UnregisteredUserMessage; customMsg != "" {
				_, _ = b.sendTopicMessage(chatID, threadID, customMsg)
			} else {
				tr := b.Translations.For(userLang)
				_, _ = b.sendTopicMessage(chatID, threadID, tr.T("msg.not_registered"))
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
						_, _ = b.sendTopicMessage(chatID, threadID, suggestion)
						continue
					}
				}
				if customMsg := b.Cfg.Telegram.UnrecognisedCommandMessage; customMsg != "" {
					_, _ = b.sendTopicMessage(chatID, threadID, customMsg)
				}
			}
			continue
		}

		handler := b.Registry.Lookup(cmd.CommandKey)
		if handler == nil {
			continue
		}

		ctx := &bot.CommandContext{
			UserID:        userID,
			UserName:      m.From.Username,
			Platform:      "telegram",
			ChannelID:     composeTopicChannelID(chatID, threadID),
			IsDM:          isDM,
			IsAdmin:       isAdmin,
			Language:      userLang,
			ProfileNo:     profileNo,
			HasLocation:   hasLocation,
			HasArea:       hasArea,
			TargetID:      userID,
			TargetName:    m.From.FirstName,
			TargetType:    targetType,
			AreaLogic:     bot.NewAreaLogic(fences, b.Cfg),
			DB:            b.DB,
			Humans:        b.Humans,
			Tracking:      b.Tracking,
			Config:        b.Cfg,
			StateMgr:      b.StateMgr,
			GameData:      b.GameData,
			Translations:  b.Translations,
			Geofence:      spatialIndex,
			Fences:        fences,
			Dispatcher:    b.Dispatcher,
			RowText:       b.RowText,
			Resolver:      b.Resolver,
			ArgMatcher:    b.ArgMatcher,
			Geocoder:      b.Geocoder,
			StaticMap:     b.StaticMap,
			Weather:       b.Weather,
			Stats:         b.Stats,
			DTS:           b.DTS,
			Emoji:         b.Emoji,
			NLP:           b.nlpParser,
			TestProcessor: b.TestProcessor,
			Registry:      b.Registry,
			ReloadFunc:    b.ReloadFunc,
			PostRegister:  b.postRegisterHook(),
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
				_, _ = b.sendTopicMessage(chatID, threadID, bot.LocalizeTargetError(b.Translations.For(userLang), err))
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
					_, _ = b.sendTopicMessage(chatID, threadID, target.ExecutionMessage)
				}
			}
		}

		replies := handler.Run(ctx, remainingArgs)
		b.sendReplies(chatID, threadID, m.From.ID, replies)
	}
}

func (b *Bot) sendReplies(chatID int64, threadID int, userID int64, replies []bot.Reply) {
	for _, reply := range replies {
		// IsDM replies go to the user's private chat, not the group/topic.
		// DMs by definition have no thread context.
		targetChat := chatID
		replyThreadID := threadID
		if reply.IsDM && chatID != userID {
			targetChat = userID
			replyThreadID = 0
		}

		// React — Telegram doesn't support message reactions, send as text
		if reply.React != "" && reply.Text == "" && len(reply.Embed) == 0 && reply.Attachment == nil {
			_, _ = b.sendTopicMessage(targetChat, replyThreadID, reply.React)
			continue
		}

		// Image URL — send as photo (e.g. area map tiles)
		if reply.ImageURL != "" {
			if err := b.sendPhotoURLToTopic(targetChat, replyThreadID, reply.ImageURL, reply.Text); err != nil {
				log.Warnf("telegram bot: send photo failed, falling back to text: %v", err)
				// Fall through to text handler below
			} else {
				continue
			}
		}

		// File attachment
		if reply.Attachment != nil {
			if err := b.sendDocumentBytesToTopic(targetChat, replyThreadID, reply.Attachment.Filename, reply.Attachment.Content, reply.Text); err != nil {
				log.Warnf("telegram bot: send document: %v", err)
			}
			continue
		}

		if reply.Text != "" {
			// Split long messages at 4095 char limit
			messages := bot.SplitMessage(reply.Text, 4095)
			for _, text := range messages {
				if err := b.sendMarkdownToTopic(targetChat, replyThreadID, text); err != nil {
					// Retry without parse mode in case the text has invalid Markdown
					if err2 := b.sendPlainToTopic(targetChat, replyThreadID, text); err2 != nil {
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

// postRegisterHook returns the bot.CommandContext.PostRegister callback
// for Telegram, or nil if reconciliation isn't configured. Invoked by
// !poracle / /poracle so a freshly-registered user has their channel
// memberships verified and area_restriction populated immediately
// rather than waiting for the next periodic cycle. The user ID arrives
// as a decimal string; SyncSingleUser takes int64 — silently skip if
// the parse fails (shouldn't happen for real Telegram IDs).
func (b *Bot) postRegisterHook() func(string) {
	if b.reconciliation == nil {
		return nil
	}
	r := b.reconciliation
	return func(userID string) {
		id, err := strconv.ParseInt(userID, 10, 64)
		if err != nil {
			return
		}
		go r.SyncSingleUser(id)
	}
}

// handleTelegramCommand dispatches Telegram-specific commands that require the
// tgbotapi directly. Returns true if the command was handled.
func (b *Bot) handleTelegramCommand(m *tgbotapi.Message, threadID int, cmdKey string, args []string) bool {
	switch cmdKey {
	case "cmd.channel":
		b.handleChannel(m, threadID, args)
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
