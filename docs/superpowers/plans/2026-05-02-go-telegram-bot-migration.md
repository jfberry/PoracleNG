# Telegram Library Migration — `go-telegram-bot-api/v5` → `go-telegram/bot`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the unmaintained `github.com/go-telegram-bot-api/telegram-bot-api/v5` (v5.5.1, 2021) with the actively maintained `github.com/go-telegram/bot` (v1.20.0, 2026). Side effect: delete most of `topic.go` because the new library has first-class forum-topic support, so the workaround is no longer needed.

**Architecture:** The new library exposes a `*bot.Bot` with context-aware methods that take `*XxxParams` structs and return `(*models.X, error)`. Polling is registration-based — you register a default `HandlerFunc` and call `b.Start(ctx)`. We adapt our existing flow to match: keep the `Bot` wrapper struct, change the embedded library type and the API call sites, replace the polling goroutine with a context that `Start` consumes. Public surface (`telegrambot.New`, `telegrambot.Bot.API`, `Bot.Close`) stays identical so consumers (`cmd/processor/main.go`, `api/config_resolve.go`) get a one-line import + type-name change.

**Tech Stack:** Go 1.22, `github.com/go-telegram/bot` v1.20.0 + its `models` subpackage, existing PoracleNG `bot.Parser` / `bot.Registry` / `store.HumanStore` / `delivery.Dispatcher`.

---

## File structure

| File | Responsibility | Action |
|------|----------------|--------|
| `processor/internal/telegrambot/bot.go` | Bot wrapper + polling + handleMessage + sendReplies | Major rewrite |
| `processor/internal/telegrambot/channel.go` | `/channel` command (group + topic register) | Refactor — replace `tgbotapi.NewMessage` calls with new send helpers |
| `processor/internal/telegrambot/reconciliation.go` | `GetChatMember` membership checks + DM sends | Refactor — `GetChatMember` returns a discriminated-union struct |
| `processor/internal/telegrambot/topic.go` | The v5.5.1 workaround | Mostly delete — keep only `composeTopicChannelID` |
| `processor/internal/api/config_resolve.go` | Resolve telegram chat IDs to names for the editor UI | Refactor `GetChat` signature |
| `processor/cmd/processor/main.go` | Construct the bot + pass `*BotAPI` to resolveDeps | Update type names; pass an embeddable `*bot.Bot` instead |
| `processor/go.mod` / `processor/go.sum` | Module declarations | Replace `go-telegram-bot-api/v5` with `go-telegram/bot` |

The migration touches imports across 6 files but the *substantive* change is concentrated in `bot.go` and `reconciliation.go`. Everything else is mechanical.

---

## API translation table

The recurring patterns. Keep this open during implementation.

| v5.5.1 (current) | v1.20.0 (target) | Notes |
|------------------|-------------------|-------|
| `tgbotapi.NewBotAPI(token)` | `bot.New(token, opts...)` | Returns `(*bot.Bot, error)`. Options replace what was tgbotapi setup state. |
| `tgbotapi.BotAPI` | `bot.Bot` | Pointer wrapper unchanged. |
| `b.api.Send(tgbotapi.NewMessage(chatID, text))` | `b.api.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})` | All Send* methods take ctx + Params struct. |
| `tgbotapi.NewMessage(chatID, text)` | `&bot.SendMessageParams{ChatID: chatID, Text: text}` | Direct struct literal, no constructor. |
| `msg.ParseMode = "Markdown"` | `msg.ParseMode = models.ParseModeMarkdownV1` | Constants in `models` package. **Note:** `models.ParseModeMarkdown` is MarkdownV2; v1 is `ParseModeMarkdownV1`. |
| `b.api.GetChat(tgbotapi.ChatInfoConfig{...})` | `b.api.GetChat(ctx, &bot.GetChatParams{ChatID: chatID})` | `ChatID` is `any` to allow string usernames; pass int64 directly. Returns `*models.ChatFullInfo`. |
| `b.api.GetChatMember(tgbotapi.GetChatMemberConfig{...})` | `b.api.GetChatMember(ctx, &bot.GetChatMemberParams{ChatID, UserID})` | Returns `*models.ChatMember`. |
| `member.Status` | `member.Type` | New library uses `Type` of type `models.ChatMemberType`. Constants: `ChatMemberTypeOwner`, `ChatMemberTypeAdministrator`, `ChatMemberTypeMember`, `ChatMemberTypeRestricted`, `ChatMemberTypeLeft`, `ChatMemberTypeBanned` (= "kicked"). |
| `b.api.GetUpdatesChan(...)` then channel loop | `bot.WithDefaultHandler(b.handleUpdate)` + `b.api.Start(ctx)` | `Start` blocks until ctx is cancelled. We run it in a goroutine (same as today). |
| `*tgbotapi.Update` | `*models.Update` | Field shapes near-identical. |
| `*tgbotapi.Message` | `*models.Message` | Has `MessageThreadID int` and `IsTopicMessage bool` first-class. |
| `b.api.StopReceivingUpdates()` | `cancel()` on the context passed to `Start` | Holds the context so `Close` can cancel it. |
| `tgbotapi.FileURL("https://...")` | `&models.InputFileString{Data: "https://..."}` | URL-as-photo wrapping. |
| `tgbotapi.FileBytes{Name, Bytes}` | `&models.InputFileUpload{Filename, Data: bytes.NewReader(b)}` | `Data` is `io.Reader` now. |

---

## Task 1: Add the new dependency, keep the old one

We can land the import swap one file at a time without a flag-day. Step 1 just adds the new module; existing code keeps compiling against the old.

**Files:**
- Modify: `processor/go.mod`
- Modify: `processor/go.sum`

- [ ] **Step 1: Add the new dependency**

```
cd /Users/james/GolandProjects/PoracleNG/processor
go get github.com/go-telegram/bot@v1.20.0
```
Expected: `go.mod` now lists both `github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1` and `github.com/go-telegram/bot v1.20.0`. `go.sum` updated.

- [ ] **Step 2: Build to confirm both can coexist**

```
go build ./...
```
Expected: succeeds.

- [ ] **Step 3: Commit**

```
git add processor/go.mod processor/go.sum
git commit -m "deps: add github.com/go-telegram/bot v1.20.0"
```

---

## Task 2: New skeleton in `bot.go` — adapter type + constructor

Rewrite the `Bot` struct to wrap the new library. Add a small `parseModeMarkdown` constant alias for readability. Keep the public `API() *bot.Bot` method (renamed return type).

**Files:**
- Modify: `processor/internal/telegrambot/bot.go:1-90`

- [ ] **Step 1: Replace the imports and type declarations**

In `processor/internal/telegrambot/bot.go`, replace the file's top section (everything down to and including the existing `New` constructor) with:

```go
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
```

- [ ] **Step 2: Replace `Close` and `API` methods**

Find the existing `Close()` and `API()` methods (~lines 187–194 in current bot.go). Replace with:

```go
// API returns the underlying Telegram bot API, or nil if the bot is not running.
func (b *Bot) API() *gotgbot.Bot { return b.api }

// Close stops the polling loop.
func (b *Bot) Close() {
	if b.cancelStart != nil {
		b.cancelStart()
	}
	close(b.stopCh)
}
```

- [ ] **Step 3: Build (will fail elsewhere — that's expected)**

```
cd /Users/james/GolandProjects/PoracleNG/processor
go build ./internal/telegrambot/ 2>&1 | head -30
```
Expected: many compile errors in handler / send code that we'll fix in subsequent tasks. The new types should at least be recognised; if Go reports `gotgbot.New undefined` or similar, the import line is wrong — fix that before proceeding.

- [ ] **Step 4: Commit the partial state**

We'll be in a broken state for several commits. That's fine because the migration is structural — incremental compilation isn't useful here.

```
git add processor/internal/telegrambot/bot.go
git commit -m "telegrambot: skeleton for go-telegram/bot constructor (WIP)"
```

---

## Task 3: Polling and the `handleUpdate` entry point

Replace the manual `pollUpdates` goroutine with a single `handleUpdate` method that the new library invokes per update.

**Files:**
- Modify: `processor/internal/telegrambot/bot.go` — delete `pollUpdates` and `handleChannelPost(*topicMessage)`, replace with `handleUpdate(ctx, *gotgbot.Bot, *models.Update)` and a `handleChannelPost(*models.Message)`.

- [ ] **Step 1: Delete the old polling code**

In `bot.go`, delete the `pollUpdates` function (the whole `func (b *Bot) pollUpdates() { ... }` block, ~lines 196–224 in current code).

- [ ] **Step 2: Add the new handler**

Insert this where `pollUpdates` was:

```go
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
```

- [ ] **Step 3: Commit**

```
git add processor/internal/telegrambot/bot.go
git commit -m "telegrambot: replace manual polling with default-handler entry point"
```

---

## Task 4: Rewrite `handleMessage` for `*models.Message`

The existing `handleMessage(tm *topicMessage)` takes our local wrapper. The new library gives us `*models.Message` directly with `MessageThreadID` and `IsTopicMessage` already populated.

**Files:**
- Modify: `processor/internal/telegrambot/bot.go` — `handleMessage` signature + every internal access.

- [ ] **Step 1: Replace the function signature and unwrap**

Find `func (b *Bot) handleMessage(tm *topicMessage) {` (~line 233 in current bot.go) and change the signature plus the first lines:

```go
func (b *Bot) handleMessage(m *models.Message) {
	if m.From == nil {
		return
	}
	threadID := m.MessageThreadID
```

Then **delete** the `m := tm.Message` line that came right after. Every reference to `tm.ThreadID` must be replaced with `threadID` (or `m.MessageThreadID` if you'd rather not use the local).

- [ ] **Step 2: Search and adjust references**

Inside `handleMessage`, find these patterns and replace:

| Find | Replace with |
|------|--------------|
| `tm.ThreadID` | `threadID` |
| `tm.Message` | `m` |
| `tm.IsTopicMessage` | `m.IsTopicMessage` (no current callers; should be zero hits) |

The existing code already uses local variables `threadID`, `chatID`, `userID`, etc., so most of the body needs no change other than the signature swap.

- [ ] **Step 3: Build to confirm only Send-related errors remain**

```
go build ./internal/telegrambot/ 2>&1 | head -30
```
Expected: errors come only from `tgbotapi.NewMessage`, `b.api.Send`, `tgbotapi.NewPhoto`, `tgbotapi.NewDocument`, `tgbotapi.MessageConfig`, etc. — anything that touches the *outgoing* API. Fixed in Tasks 5 + 6.

- [ ] **Step 4: Commit**

```
git add processor/internal/telegrambot/bot.go
git commit -m "telegrambot: handleMessage now consumes *models.Message"
```

---

## Task 5: Replace the topic.go workaround with thin send helpers on the new library

Most of `topic.go` becomes obsolete because the new library's `SendMessageParams` etc. natively carry `MessageThreadID`.

**Files:**
- Replace contents of: `processor/internal/telegrambot/topic.go`

- [ ] **Step 1: Replace the file**

Overwrite `processor/internal/telegrambot/topic.go` with:

```go
package telegrambot

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"time"

	gotgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// composeTopicChannelID returns the composite ID used to address a topic
// human row: "<chatID>:<topicID>". For non-topic messages (threadID==0
// or DMs), returns the bare chat ID.
func composeTopicChannelID(chatID int64, threadID int) string {
	if threadID > 0 {
		return formatInt64(chatID) + ":" + strconv.Itoa(threadID)
	}
	return formatInt64(chatID)
}

// requestCtx returns a 30-second context for outgoing API calls. Used
// by every send helper since the library's methods take a context.
func requestCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// sendTopicMessage posts a plain text reply, optionally threaded into a
// forum topic. Used everywhere the polling bot wants to reply with no
// parse mode.
func (b *Bot) sendTopicMessage(chatID int64, threadID int, text string) (*models.Message, error) {
	ctx, cancel := requestCtx()
	defer cancel()
	return b.api.SendMessage(ctx, &gotgbot.SendMessageParams{
		ChatID:          chatID,
		MessageThreadID: threadID,
		Text:            text,
	})
}

// sendMarkdownToTopic sends a MarkdownV1-parsed text message, threaded
// into a topic when threadID > 0. Used by the reply path which renders
// existing PoracleJS Markdown.
func (b *Bot) sendMarkdownToTopic(chatID int64, threadID int, text string) error {
	ctx, cancel := requestCtx()
	defer cancel()
	_, err := b.api.SendMessage(ctx, &gotgbot.SendMessageParams{
		ChatID:          chatID,
		MessageThreadID: threadID,
		Text:            text,
		ParseMode:       models.ParseModeMarkdownV1,
	})
	return err
}

// sendPlainToTopic sends an unparsed text message — used as a fallback
// when Markdown parsing fails.
func (b *Bot) sendPlainToTopic(chatID int64, threadID int, text string) error {
	ctx, cancel := requestCtx()
	defer cancel()
	_, err := b.api.SendMessage(ctx, &gotgbot.SendMessageParams{
		ChatID:          chatID,
		MessageThreadID: threadID,
		Text:            text,
	})
	return err
}

// sendPhotoURLToTopic sends a photo by URL. The new library accepts
// URL-typed photos via *models.InputFileString{Data: url}.
func (b *Bot) sendPhotoURLToTopic(chatID int64, threadID int, photoURL, caption string) error {
	ctx, cancel := requestCtx()
	defer cancel()
	_, err := b.api.SendPhoto(ctx, &gotgbot.SendPhotoParams{
		ChatID:              chatID,
		MessageThreadID:     threadID,
		Photo:               &models.InputFileString{Data: photoURL},
		Caption:             caption,
		DisableNotification: true,
	})
	return err
}

// sendDocumentBytesToTopic sends a byte-buffer document (e.g. a backup
// JSON). InputFileUpload wraps an io.Reader.
func (b *Bot) sendDocumentBytesToTopic(chatID int64, threadID int, filename string, data []byte, caption string) error {
	ctx, cancel := requestCtx()
	defer cancel()
	_, err := b.api.SendDocument(ctx, &gotgbot.SendDocumentParams{
		ChatID:              chatID,
		MessageThreadID:     threadID,
		Document:            &models.InputFileUpload{Filename: filename, Data: bytes.NewReader(data)},
		Caption:             caption,
		DisableNotification: true,
	})
	return err
}

// sentinel — keeps fmt referenced when other helpers are added.
var _ = fmt.Sprintf
```

- [ ] **Step 2: Build**

```
go build ./internal/telegrambot/ 2>&1 | head -30
```
Expected: errors only from `bot.go` and `channel.go` and `reconciliation.go` (the call sites that still use the old library types). Fixed in subsequent tasks.

- [ ] **Step 3: Commit**

```
git add processor/internal/telegrambot/topic.go
git commit -m "telegrambot: rewrite topic.go send helpers on go-telegram/bot"
```

---

## Task 6: Migrate `sendReplies` to the new helpers

The `sendReplies` flow already calls `sendTopicMessage` / `sendMarkdownToTopic` / `sendPlainToTopic`, so these are mostly preserved. The two image / document paths still build `tgbotapi.PhotoConfig` / `DocumentConfig` — those need rewiring.

**Files:**
- Modify: `processor/internal/telegrambot/bot.go` — `sendReplies` body.

- [ ] **Step 1: Replace the photo and attachment branches**

Find the existing `sendReplies` (it currently uses `tgbotapi.NewPhoto`, `b.sendPhotoToTopic(photo, ...)`, `tgbotapi.NewDocument`, `b.sendDocumentToTopic(doc, ...)`). Replace those branches with the URL/bytes-based helpers from Task 5:

```go
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
```

- [ ] **Step 2: Build**

```
go build ./internal/telegrambot/ 2>&1 | head -30
```
Expected: errors now only from `validateConfig` and `channel.go` and `reconciliation.go`. The `sendReplies` flow should compile.

- [ ] **Step 3: Commit**

```
git add processor/internal/telegrambot/bot.go
git commit -m "telegrambot: sendReplies photo + attachment paths use new library"
```

---

## Task 7: Rewrite `validateConfig`

`validateConfig` calls `b.api.GetChat(...)` for every configured chat ID and admin. The signature changes from `tgbotapi.ChatInfoConfig` to `*bot.GetChatParams`, and the return is `*models.ChatFullInfo` instead of `tgbotapi.Chat`.

**Files:**
- Modify: `processor/internal/telegrambot/bot.go` — `validateConfig` (~lines 70–185).

- [ ] **Step 1: Replace the function**

Find the existing `func (b *Bot) validateConfig() {` and replace its body (everything down to the matching closing `}`) with:

```go
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
```

- [ ] **Step 2: Build**

```
go build ./internal/telegrambot/ 2>&1 | head -30
```
Expected: errors only from `channel.go` and `reconciliation.go`.

- [ ] **Step 3: Commit**

```
git add processor/internal/telegrambot/bot.go
git commit -m "telegrambot: validateConfig uses GetChatParams + ChatFullInfo"
```

---

## Task 8: Migrate `channel.go`

`handleChannel` takes `*tgbotapi.Message`; change to `*models.Message`. The remaining `b.api.Send(...)` calls were already replaced with `sendTopicMessage` in the previous topic work, so this task is mostly the signature change.

**Files:**
- Modify: `processor/internal/telegrambot/channel.go`

- [ ] **Step 1: Update the import block**

In `processor/internal/telegrambot/channel.go`, find the current import block:

```go
import (
	"fmt"
	"regexp"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/community"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)
```

Replace with:

```go
import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/go-telegram/bot/models"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/community"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)
```

- [ ] **Step 2: Update `handleChannel` signature**

Find:
```go
func (b *Bot) handleChannel(m *tgbotapi.Message, threadID int, args []string) {
```

Replace with:
```go
func (b *Bot) handleChannel(m *models.Message, threadID int, args []string) {
```

- [ ] **Step 3: Build**

```
go build ./internal/telegrambot/ 2>&1 | head -30
```
Expected: only `reconciliation.go` errors remaining. `channel.go` should compile because the rest of the body uses field access (`m.From.ID`, `m.Chat.ID`, `m.Chat.Type`, `m.Chat.Title`) that's identical between the two libraries' Message types.

- [ ] **Step 4: Commit**

```
git add processor/internal/telegrambot/channel.go
git commit -m "telegrambot: channel.go uses *models.Message"
```

---

## Task 9: Migrate `handleTelegramCommand` signature in `bot.go`

The `handleTelegramCommand` method takes `*tgbotapi.Message`. Change to `*models.Message`.

**Files:**
- Modify: `processor/internal/telegrambot/bot.go` — `handleTelegramCommand`.

- [ ] **Step 1: Replace the function**

Find:
```go
func (b *Bot) handleTelegramCommand(m *tgbotapi.Message, threadID int, cmdKey string, args []string) bool {
```

Replace with:
```go
func (b *Bot) handleTelegramCommand(m *models.Message, threadID int, cmdKey string, args []string) bool {
```

- [ ] **Step 2: Build**

```
go build ./internal/telegrambot/ 2>&1 | head -30
```
Expected: only `reconciliation.go` errors remaining.

- [ ] **Step 3: Commit**

```
git add processor/internal/telegrambot/bot.go
git commit -m "telegrambot: handleTelegramCommand takes *models.Message"
```

---

## Task 10: Migrate `reconciliation.go` — types and constructor

The reconciliation struct holds an `*tgbotapi.BotAPI` and uses `GetChatMember` extensively. The new library's `GetChatMember` returns `*models.ChatMember` — a discriminated union — and the status constants live in the `models` package.

**Files:**
- Modify: `processor/internal/telegrambot/reconciliation.go`

- [ ] **Step 1: Update the import block**

Find the current `tgbotapi` import line and replace the import block with:

```go
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
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)
```

(Adjust `internal/...` imports to match what's already there — keep the same set.)

- [ ] **Step 2: Update the struct field type**

Find:
```go
type TelegramReconciliation struct {
	api          *tgbotapi.BotAPI
```

Replace `*tgbotapi.BotAPI` with `*gotgbot.Bot`. Same for the constructor's parameter type.

- [ ] **Step 3: Update GetChatMember + status check**

Find:
```go
		member, err := r.api.GetChatMember(tgbotapi.GetChatMemberConfig{
			ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
				ChatID: groupID,
				UserID: userID,
			},
		})
		if err != nil {
			// 400 typically means user not found in chat — skip silently.
			if strings.Contains(err.Error(), "Bad Request") || strings.Contains(err.Error(), "user not found") {
```

Replace with:
```go
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		member, err := r.api.GetChatMember(ctx, &gotgbot.GetChatMemberParams{
			ChatID: groupID,
			UserID: userID,
		})
		cancel()
		if err != nil {
			// 400 typically means user not found in chat — skip silently.
			if strings.Contains(err.Error(), "Bad Request") || strings.Contains(err.Error(), "user not found") {
```

Then find:
```go
		if member.Status != "left" && member.Status != "kicked" {
```

Replace with:
```go
		if member.Type != models.ChatMemberTypeLeft && member.Type != models.ChatMemberTypeBanned {
```

(Note: in the new library "kicked" = `ChatMemberTypeBanned`. The serialised wire value is still `"kicked"`, but the Go-side constant name changed.)

- [ ] **Step 4: Update the DM-send call sites**

Find each `r.api.Send(msg)` instance (~3 occurrences). Each is preceded by a `tgbotapi.NewMessage(chatID, text)` call. Replace each block following this template:

```go
// Old:
msg := tgbotapi.NewMessage(chatID, text[:splitAt])
r.api.Send(msg)

// New:
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
_, _ = r.api.SendMessage(ctx, &gotgbot.SendMessageParams{
    ChatID: chatID,
    Text:   text[:splitAt],
})
cancel()
```

There's also one with error handling:
```go
// Old:
msg := tgbotapi.NewMessage(chatID, goodbyeMsg)
if _, err := r.api.Send(msg); err != nil {
    log.Warnf("telegram reconciliation: failed to send goodbye to %d: %v", chatID, err)
}

// New:
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
_, err := r.api.SendMessage(ctx, &gotgbot.SendMessageParams{
    ChatID: chatID,
    Text:   goodbyeMsg,
})
cancel()
if err != nil {
    log.Warnf("telegram reconciliation: failed to send goodbye to %d: %v", chatID, err)
}
```

- [ ] **Step 5: Build the package**

```
go build ./internal/telegrambot/ 2>&1 | head -30
```
Expected: no errors. The whole `telegrambot` package now compiles.

- [ ] **Step 6: Commit**

```
git add processor/internal/telegrambot/reconciliation.go
git commit -m "telegrambot: reconciliation uses go-telegram/bot GetChatMember + SendMessage"
```

---

## Task 11: Migrate `api/config_resolve.go`

`config_resolve.go` is a thin consumer of `*tgbotapi.BotAPI` for `GetChat`. Updating its imports + the one call site finishes the migration.

**Files:**
- Modify: `processor/internal/api/config_resolve.go`

- [ ] **Step 1: Update the imports**

Find:
```go
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
```

Replace with:
```go
	gotgbot "github.com/go-telegram/bot"
```

- [ ] **Step 2: Update the struct field**

Find:
```go
	TelegramAPI    *tgbotapi.BotAPI            // nil if Telegram not configured
```

Replace with:
```go
	TelegramAPI    *gotgbot.Bot                // nil if Telegram not configured
```

- [ ] **Step 3: Update the GetChat call site (~line 361)**

Find:
```go
		chatCfg := tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}}
		chat, err := deps.TelegramAPI.GetChat(chatCfg)
```

Replace with:
```go
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		chat, err := deps.TelegramAPI.GetChat(ctx, &gotgbot.GetChatParams{ChatID: chatID})
		cancel()
```

If `context` and `time` aren't already imported in this file, add them:
```go
	"context"
	"time"
```

If the surrounding code already pulls a `c.Request.Context()`, reuse that — we don't need a new background context. Check the function body.

- [ ] **Step 4: Adapt the `chat` field accesses**

The new `chat` is `*models.ChatFullInfo`. The fields used (`chat.Title`, `chat.UserName`/`Username`, `chat.FirstName`, `chat.Type`) all exist with the same names (note: `Username` vs `UserName` — the new library uses `Username` without the camelCase).

Find every `chat.UserName` and replace with `chat.Username`.

- [ ] **Step 5: Build**

```
go build ./internal/api/ 2>&1 | head -10
```
Expected: succeeds.

- [ ] **Step 6: Commit**

```
git add processor/internal/api/config_resolve.go
git commit -m "api: config_resolve uses go-telegram/bot GetChat"
```

---

## Task 12: Drop the old library + final build

Now that nothing imports the old library, remove it from `go.mod` so future-you doesn't accidentally pull it back in.

**Files:**
- Modify: `processor/go.mod`
- Modify: `processor/go.sum`

- [ ] **Step 1: Confirm nothing still imports the old library**

```
cd /Users/james/GolandProjects/PoracleNG
grep -rn "go-telegram-bot-api/telegram-bot-api" processor/ --include="*.go"
```
Expected: zero matches. If anything matches, fix it before continuing.

- [ ] **Step 2: Remove the old dependency**

```
cd /Users/james/GolandProjects/PoracleNG/processor
go mod tidy
```
Expected: `go.mod` no longer lists `github.com/go-telegram-bot-api/telegram-bot-api/v5`. `go.sum` updated.

- [ ] **Step 3: Build the whole tree**

```
go build ./...
```
Expected: succeeds across every package.

- [ ] **Step 4: Run the full test suite**

```
go test ./...
```
Expected: every package green. The pre-existing `TestRateLimitBypass` flake in `delivery/` may show up; rerun once if it does.

- [ ] **Step 5: Build the binary**

```
go build -o /tmp/processor-build ./cmd/processor && ls -la /tmp/processor-build && rm /tmp/processor-build
```
Expected: ~50MB binary builds clean.

- [ ] **Step 6: Commit**

```
git add processor/go.mod processor/go.sum
git commit -m "deps: drop go-telegram-bot-api/telegram-bot-api/v5"
```

---

## Task 13: Push and surface any open follow-ups

- [ ] **Step 1: Push the branch**

```
git push
```

- [ ] **Step 2: Manual smoke test plan (recorded for the operator)**

Run the bot against a real test guild and exercise:
- `/identify` in a private chat → bot replies with user ID
- `/identify` in a group → bot replies with chat + user ID
- `/identify` in a forum topic → bot replies with chat + topic + user ID
- `/poracle` registration in a group
- `/track everything iv100` in a DM → tracking row added; `/tracked` shows it
- `/channel add` inside a forum topic → registers `telegram:topic`
- A real webhook fires and an alert lands in the topic

These can't be unit-tested (they need a live Telegram). Document them as the operator's checklist.

- [ ] **Step 3: Cleanup follow-ups**

Note in the PR description that:
- The `topic.go` workaround is much smaller (composite-ID helper + 5 send wrappers).
- No reconciliation behaviour changed — only the underlying SDK.
- No DTS template, command, or schema changes.

---

## Out of scope (deliberately)

- **Changing the bot framework's command interface.** `bot.CommandContext` keeps its existing shape; we adapt only the Telegram surface.
- **Async per-update workers.** The new library defaults to 1 worker via `WithWorkers`; the old library's polling was also single-threaded in our wiring. If we want concurrency, that's a separate change.
- **Webhook mode.** The library supports `StartWebhook`; we keep polling so the deployment story is unchanged.
- **Migrating `discordbot/`.** Different SDK entirely — `discordgo`. Untouched.
- **Switching parse mode to MarkdownV2.** Existing templates target Markdown (V1) and would need re-escaping for V2. We map our existing `"Markdown"` to `models.ParseModeMarkdownV1`.

---

## Reference files to read before starting

- `/Users/james/go/pkg/mod/github.com/go-telegram/bot@v1.20.0/bot.go` — `New`, `Start`, `Close` shapes.
- `/Users/james/go/pkg/mod/github.com/go-telegram/bot@v1.20.0/methods.go` — every `Send*` / `Get*` method.
- `/Users/james/go/pkg/mod/github.com/go-telegram/bot@v1.20.0/methods_params.go` — every `XxxParams` struct.
- `/Users/james/go/pkg/mod/github.com/go-telegram/bot@v1.20.0/options.go` — `WithDefaultHandler`, `WithErrorsHandler`, `WithWorkers`.
- `/Users/james/go/pkg/mod/github.com/go-telegram/bot@v1.20.0/models/message.go` — Message field shape.
- `/Users/james/go/pkg/mod/github.com/go-telegram/bot@v1.20.0/models/chat_member.go` — `ChatMember` discriminated union + `ChatMemberType` constants.
- `/Users/james/go/pkg/mod/github.com/go-telegram/bot@v1.20.0/models/input_file.go` — `InputFileString` and `InputFileUpload`.
