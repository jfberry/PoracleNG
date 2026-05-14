package telegrambot

import (
	"bytes"
	"context"
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

// sendMarkdownToTopic accepts text in our canonical Markdown format
// (the same syntax callers use for Discord), converts it to
// Telegram-flavored HTML, and sends with parse_mode=HTML. HTML's
// reserved-char surface is just <>&, so most messages parse on the
// first try; the polling bot's plain-text fallback covers any edge
// case that slips through.
func (b *Bot) sendMarkdownToTopic(chatID int64, threadID int, text string) error {
	ctx, cancel := requestCtx()
	defer cancel()
	_, err := b.api.SendMessage(ctx, &gotgbot.SendMessageParams{
		ChatID:          chatID,
		MessageThreadID: threadID,
		Text:            markdownToHTML(text),
		ParseMode:       models.ParseModeHTML,
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
