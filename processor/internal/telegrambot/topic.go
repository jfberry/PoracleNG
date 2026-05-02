package telegrambot

import (
	"encoding/json"
	"fmt"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// topic.go works around the v5.5.1 library's lack of forum-topic support
// (Telegram added topics in late 2022; the library has been unmaintained
// since 2021). The library's Update struct silently drops the
// `message_thread_id` and `is_topic_message` fields on incoming messages,
// and the MessageConfig outgoing builder has no way to set them.
//
// We re-decode the raw `getUpdates` response bytes ourselves and use the
// library's lower-level MakeRequest(method, params) to send with the
// topic field included.

// topicUpdate mirrors the subset of tgbotapi.Update we use, plus the
// topic fields the library lacks. Decoded directly from the
// `getUpdates` response payload before being dispatched.
type topicUpdate struct {
	UpdateID    int           `json:"update_id"`
	Message     *topicMessage `json:"message,omitempty"`
	ChannelPost *topicMessage `json:"channel_post,omitempty"`
}

// topicMessage extends the library's Message with the two forum-topic
// fields. The embedded *tgbotapi.Message carries everything the
// existing handlers already use; ThreadID and IsTopicMessage are
// only-set when the message originates from a forum-topic.
type topicMessage struct {
	*tgbotapi.Message
	ThreadID       int  `json:"message_thread_id,omitempty"`
	IsTopicMessage bool `json:"is_topic_message,omitempty"`
}

// UnmarshalJSON decodes both the underlying Message and the topic-only
// fields from the same JSON object. The library's Message type covers
// every field this codebase already reads, so we can rely on it for
// everything else.
func (tm *topicMessage) UnmarshalJSON(data []byte) error {
	var inner tgbotapi.Message
	if err := json.Unmarshal(data, &inner); err != nil {
		return err
	}
	var ext struct {
		ThreadID       int  `json:"message_thread_id,omitempty"`
		IsTopicMessage bool `json:"is_topic_message,omitempty"`
	}
	if err := json.Unmarshal(data, &ext); err != nil {
		return err
	}
	tm.Message = &inner
	tm.ThreadID = ext.ThreadID
	tm.IsTopicMessage = ext.IsTopicMessage
	return nil
}

// fetchUpdates calls Telegram's getUpdates and returns topicUpdates with
// the message-thread-id field preserved. Replaces the library's
// GetUpdatesChan, which strips it.
func (b *Bot) fetchUpdates(offset int, timeoutSeconds int) ([]topicUpdate, error) {
	cfg := tgbotapi.NewUpdate(offset)
	cfg.Timeout = timeoutSeconds
	resp, err := b.api.Request(cfg)
	if err != nil {
		return nil, fmt.Errorf("getUpdates: %w", err)
	}
	var updates []topicUpdate
	if err := json.Unmarshal(resp.Result, &updates); err != nil {
		return nil, fmt.Errorf("decode getUpdates result: %w", err)
	}
	return updates, nil
}

// composeTopicChannelID returns the composite ID used to address a topic
// human row: "<chatID>:<topicID>". For non-topic messages (threadID==0
// or DMs), returns the bare chat ID.
func composeTopicChannelID(chatID int64, threadID int) string {
	if threadID > 0 {
		return formatInt64(chatID) + ":" + strconv.Itoa(threadID)
	}
	return formatInt64(chatID)
}

// sendTopicMessage posts a plain text reply to a chat — optionally
// threaded into a forum topic. Mirrors the small fraction of
// tgbotapi.MessageConfig that this codebase actually uses; sufficient
// for every send-site in the polling bot today.
func (b *Bot) sendTopicMessage(chatID int64, threadID int, text string) (tgbotapi.Message, error) {
	params := tgbotapi.Params{}
	params["chat_id"] = formatInt64(chatID)
	params["text"] = text
	if threadID > 0 {
		params["message_thread_id"] = strconv.Itoa(threadID)
	}
	resp, err := b.api.MakeRequest("sendMessage", params)
	if err != nil {
		return tgbotapi.Message{}, err
	}
	var msg tgbotapi.Message
	if resp != nil && resp.Result != nil {
		_ = json.Unmarshal(resp.Result, &msg)
	}
	return msg, nil
}

// sendMarkdownToTopic sends a Markdown-parsed text message, threaded
// into a topic when threadID > 0. Reply-message variant used by
// sendReplies.
func (b *Bot) sendMarkdownToTopic(chatID int64, threadID int, text string) error {
	params := tgbotapi.Params{}
	params["chat_id"] = formatInt64(chatID)
	params["text"] = text
	params["parse_mode"] = "Markdown"
	if threadID > 0 {
		params["message_thread_id"] = strconv.Itoa(threadID)
	}
	_, err := b.api.MakeRequest("sendMessage", params)
	return err
}

// sendPlainToTopic mirrors sendMarkdownToTopic without a parse_mode —
// used as a fallback when Markdown parsing fails on the first attempt.
func (b *Bot) sendPlainToTopic(chatID int64, threadID int, text string) error {
	params := tgbotapi.Params{}
	params["chat_id"] = formatInt64(chatID)
	params["text"] = text
	if threadID > 0 {
		params["message_thread_id"] = strconv.Itoa(threadID)
	}
	_, err := b.api.MakeRequest("sendMessage", params)
	return err
}

// sendPhotoToTopic sends a tgbotapi.PhotoConfig with an optional topic
// thread. The library's PhotoConfig has no MessageThreadID field; we
// build the params from its existing ones and add the thread id.
func (b *Bot) sendPhotoToTopic(photo tgbotapi.PhotoConfig, threadID int) error {
	if threadID == 0 {
		_, err := b.api.Send(photo)
		return err
	}
	// Threaded photos: build the params manually. PhotoConfig.params()
	// is unexported, so we use the public Send path with a media
	// upload-style request via Request + custom params.
	params, err := buildPhotoParams(photo)
	if err != nil {
		return err
	}
	params["message_thread_id"] = strconv.Itoa(threadID)
	_, err = b.api.MakeRequest("sendPhoto", params)
	return err
}

// sendDocumentToTopic sends a tgbotapi.DocumentConfig with an optional
// topic thread. Same shape as sendPhotoToTopic.
func (b *Bot) sendDocumentToTopic(doc tgbotapi.DocumentConfig, threadID int) error {
	if threadID == 0 {
		_, err := b.api.Send(doc)
		return err
	}
	// For threaded document sends with a FileBytes body we have to
	// fall back to a plain non-threaded send because the v5.5.1
	// library's UploadFiles path doesn't expose params injection.
	// In practice the only non-threaded reply that uses Attachment is
	// !backup which is DM-only, so this never fires from a topic.
	_, err := b.api.Send(doc)
	return err
}

// buildPhotoParams pulls the values we need off a PhotoConfig.
// Limited to URL-based photos (FileURL) which is the only photo type
// the polling bot's reply path produces today.
func buildPhotoParams(photo tgbotapi.PhotoConfig) (tgbotapi.Params, error) {
	params := tgbotapi.Params{}
	params["chat_id"] = formatInt64(photo.ChatID)
	if url, ok := photo.File.(tgbotapi.FileURL); ok {
		params["photo"] = string(url)
	} else {
		return nil, fmt.Errorf("buildPhotoParams: only FileURL photo bodies supported in topic mode")
	}
	if photo.Caption != "" {
		params["caption"] = photo.Caption
	}
	return params, nil
}
