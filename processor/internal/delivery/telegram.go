package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const defaultTelegramBaseURL = "https://api.telegram.org"

var defaultSendOrder = []string{"sticker", "photo", "text", "location", "venue"}

// TelegramSender delivers messages via the Telegram Bot API.
type TelegramSender struct {
	token   string
	baseURL string
	client  *http.Client
}

// NewTelegramSender creates a new Telegram sender.
func NewTelegramSender(token string) *TelegramSender {
	return &TelegramSender{
		token:   token,
		baseURL: defaultTelegramBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Platform returns the platform identifier.
func (ts *TelegramSender) Platform() string { return "telegram" }

// telegramMessage holds the parsed fields from a Telegram job message.
type telegramMessage struct {
	Content        string      `json:"content"`
	Sticker        string      `json:"sticker"`
	Photo          string      `json:"photo"`
	SendOrder      interface{} `json:"send_order"`
	ParseMode      string      `json:"parse_mode"`
	WebpagePreview bool        `json:"webpage_preview"`
	Location       bool        `json:"location"`
	Venue          *venue      `json:"venue"`
}

type venue struct {
	Title   string `json:"title"`
	Address string `json:"address"`
}

// telegramResponse is the Telegram Bot API response format.
type telegramResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		MessageID int `json:"message_id"`
	} `json:"result"`
	Parameters *struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// Send delivers a message to Telegram following the configured send order.
func (ts *TelegramSender) Send(ctx context.Context, job *Job) (*SentMessage, error) {
	var msg telegramMessage
	if err := json.Unmarshal(job.Message, &msg); err != nil {
		return nil, fmt.Errorf("parsing telegram message: %w", err)
	}

	log.Debugf("[telegram] Send to %s: location=%v lat=%.6f lon=%.6f content_len=%d sticker=%q photo=%q send_order=%v",
		job.Target, msg.Location, job.Lat, job.Lon, len(msg.Content), msg.Sticker, msg.Photo, msg.SendOrder)

	parseMode := normalizeTelegramParseMode(msg.ParseMode)
	sendOrder := parseSendOrder(msg.SendOrder)
	chatID := job.Target

	var textMsgID int
	var lastMsgID int
	var sentAny bool

	for i, step := range sendOrder {
		var msgID int
		var err error

		switch step {
		case "sticker":
			if msg.Sticker == "" {
				continue
			}
			msgID, err = ts.sendSticker(ctx, chatID, msg.Sticker)

		case "photo":
			if msg.Photo == "" {
				continue
			}
			msgID, err = ts.sendPhoto(ctx, chatID, msg.Photo)

		case "text":
			if msg.Content == "" {
				continue
			}
			msgID, err = ts.sendMessage(ctx, chatID, msg.Content, parseMode, msg.WebpagePreview)
			if err == nil {
				textMsgID = msgID
			}

		case "location":
			if !msg.Location || (job.Lat == 0 && job.Lon == 0) {
				continue
			}
			msgID, err = ts.sendLocation(ctx, chatID, job.Lat, job.Lon)

		case "venue":
			if msg.Venue == nil || msg.Venue.Title == "" || msg.Venue.Address == "" {
				continue
			}
			// disable_notification if text follows later in the send order
			hasTextFollowing := ts.hasStepAfter(sendOrder, i, "text") && msg.Content != ""
			msgID, err = ts.sendVenue(ctx, chatID, job.Lat, job.Lon, msg.Venue.Title, msg.Venue.Address, hasTextFollowing)

		default:
			continue
		}

		if err != nil {
			return nil, err
		}
		lastMsgID = msgID
		sentAny = true
	}

	if !sentAny {
		return nil, fmt.Errorf("telegram: no content to send")
	}

	primaryID := lastMsgID
	if textMsgID != 0 {
		primaryID = textMsgID
	}

	return &SentMessage{ID: chatID + ":" + strconv.Itoa(primaryID)}, nil
}

// Delete deletes a previously sent Telegram message.
func (ts *TelegramSender) Delete(ctx context.Context, sentID string) error {
	chatID, messageID, err := parseTelegramSentID(sentID)
	if err != nil {
		return err
	}

	body := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	resp, err := ts.doPost(ctx, "deleteMessage", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest, http.StatusForbidden:
		return nil // can't delete, don't retry
	default:
		return fmt.Errorf("telegram deleteMessage returned status %d", resp.StatusCode)
	}
}

// Edit edits a previously sent Telegram text message.
func (ts *TelegramSender) Edit(ctx context.Context, sentID string, message json.RawMessage) error {
	chatID, messageID, err := parseTelegramSentID(sentID)
	if err != nil {
		return err
	}

	var editMsg struct {
		Content   string `json:"content"`
		ParseMode string `json:"parse_mode"`
	}
	if err := json.Unmarshal(message, &editMsg); err != nil {
		return fmt.Errorf("parsing edit message: %w", err)
	}

	parseMode := normalizeTelegramParseMode(editMsg.ParseMode)

	body := map[string]interface{}{
		"chat_id":                  chatID,
		"message_id":              messageID,
		"text":                    editMsg.Content,
		"parse_mode":             parseMode,
		"disable_web_page_preview": true,
	}
	resp, err := ts.doPost(ctx, "editMessageText", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("telegram editMessageText returned status %d", resp.StatusCode)
}

// sendMessage sends a text message.
func (ts *TelegramSender) sendMessage(ctx context.Context, chatID, text, parseMode string, webpagePreview bool) (int, error) {
	body := map[string]interface{}{
		"chat_id":                  chatID,
		"text":                    text,
		"parse_mode":             parseMode,
		"disable_web_page_preview": !webpagePreview,
	}
	return ts.callWithRetry(ctx, "sendMessage", body)
}

// sendSticker sends a sticker.
func (ts *TelegramSender) sendSticker(ctx context.Context, chatID, stickerID string) (int, error) {
	body := map[string]interface{}{
		"chat_id":              chatID,
		"sticker":             stickerID,
		"disable_notification": true,
	}
	return ts.callWithRetry(ctx, "sendSticker", body)
}

// sendPhoto sends a photo by URL.
func (ts *TelegramSender) sendPhoto(ctx context.Context, chatID, photoURL string) (int, error) {
	body := map[string]interface{}{
		"chat_id":              chatID,
		"photo":               photoURL,
		"disable_notification": true,
	}
	return ts.callWithRetry(ctx, "sendPhoto", body)
}

// sendLocation sends a location.
func (ts *TelegramSender) sendLocation(ctx context.Context, chatID string, lat, lon float64) (int, error) {
	body := map[string]interface{}{
		"chat_id":              chatID,
		"latitude":            lat,
		"longitude":           lon,
		"disable_notification": true,
	}
	return ts.callWithRetry(ctx, "sendLocation", body)
}

// sendVenue sends a venue.
func (ts *TelegramSender) sendVenue(ctx context.Context, chatID string, lat, lon float64, title, address string, disableNotification bool) (int, error) {
	body := map[string]interface{}{
		"chat_id":              chatID,
		"latitude":            lat,
		"longitude":           lon,
		"title":               title,
		"address":             address,
		"disable_notification": disableNotification,
	}
	return ts.callWithRetry(ctx, "sendVenue", body)
}

// callWithRetry posts to a Telegram API method with retry logic.
func (ts *TelegramSender) callWithRetry(ctx context.Context, method string, body map[string]interface{}) (int, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("marshaling request body: %w", err)
	}

	const maxRetries = 5
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			default:
			}
		}

		resp, err := ts.doPostRaw(ctx, method, jsonBody)
		if err != nil {
			if attempt < maxRetries {
				time.Sleep(time.Second)
				continue
			}
			return 0, err
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return 0, fmt.Errorf("reading response body: %w", readErr)
		}

		if resp.StatusCode == http.StatusOK {
			var tgResp telegramResponse
			if err := json.Unmarshal(respBody, &tgResp); err != nil {
				return 0, fmt.Errorf("decoding telegram response: %w", err)
			}
			if tgResp.OK {
				log.Debugf("telegram: %s to %s ok (msg %d)", method, body["chat_id"], tgResp.Result.MessageID)
				return tgResp.Result.MessageID, nil
			}
		}

		if resp.StatusCode == http.StatusForbidden {
			log.Warnf("telegram: permanent error for %s %s: %s", method, body["chat_id"], respBody)
			return 0, &PermanentError{
				Err:    fmt.Errorf("telegram %s: forbidden (status 403): %s", method, respBody),
				Reason: "user blocked bot or bot was removed",
			}
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			var tgResp telegramResponse
			json.Unmarshal(respBody, &tgResp) //nolint:errcheck
			retryAfter := 1
			if tgResp.Parameters != nil && tgResp.Parameters.RetryAfter > 0 {
				retryAfter = tgResp.Parameters.RetryAfter
			}
			if retryAfter > 60 {
				log.Warnf("telegram: 429 rate limited for %s %s, retry_after=%ds is excessive — capping to 60s and giving up (attempt %d/%d)", method, body["chat_id"], retryAfter, attempt+1, maxRetries+1)
				return 0, fmt.Errorf("telegram rate limit too long: %ds", retryAfter)
			}
			log.Warnf("telegram: 429 rate limited for %s %s, retry_after=%ds (attempt %d/%d)", method, body["chat_id"], retryAfter, attempt+1, maxRetries+1)
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(time.Duration(retryAfter) * time.Second):
			}
			continue
		}

		if attempt < maxRetries {
			log.Warnf("telegram: %s to %s failed (attempt %d/%d): status=%d", method, body["chat_id"], attempt+1, maxRetries+1, resp.StatusCode)
			time.Sleep(time.Second)
			continue
		}

		return 0, fmt.Errorf("telegram %s returned status %d: %s", method, resp.StatusCode, respBody)
	}
	return 0, fmt.Errorf("telegram %s: max retries exceeded", method)
}

// doPost marshals body to JSON and posts to the Telegram API.
func (ts *TelegramSender) doPost(ctx context.Context, method string, body interface{}) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request body: %w", err)
	}
	return ts.doPostRaw(ctx, method, jsonBody)
}

// doPostRaw posts raw JSON to the Telegram API.
func (ts *TelegramSender) doPostRaw(ctx context.Context, method string, body []byte) (*http.Response, error) {
	url := ts.baseURL + "/bot" + ts.token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return ts.client.Do(req)
}

// hasStepAfter checks if a given step appears after position i in the send order.
func (ts *TelegramSender) hasStepAfter(order []string, currentIdx int, step string) bool {
	for j := currentIdx + 1; j < len(order); j++ {
		if order[j] == step {
			return true
		}
	}
	return false
}

// parseSendOrder parses the send_order field which can be a []string, a delimited string, or nil.
func parseSendOrder(raw interface{}) []string {
	if raw == nil {
		return defaultSendOrder
	}

	switch v := raw.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		if len(result) > 0 {
			return result
		}
		return defaultSendOrder
	case string:
		if v == "" {
			return defaultSendOrder
		}
		// Split on comma, pipe, or semicolon.
		var parts []string
		for _, sep := range []string{",", "|", ";"} {
			if strings.Contains(v, sep) {
				parts = strings.Split(v, sep)
				break
			}
		}
		if parts == nil {
			// Single item.
			parts = []string{v}
		}
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, p)
			}
		}
		if len(result) > 0 {
			return result
		}
		return defaultSendOrder
	default:
		return defaultSendOrder
	}
}

// normalizeTelegramParseMode normalizes Telegram parse mode strings.
func normalizeTelegramParseMode(mode string) string {
	switch strings.ToLower(mode) {
	case "markdown":
		return "Markdown"
	case "markdownv2":
		return "MarkdownV2"
	case "html":
		return "HTML"
	case "":
		return "Markdown"
	default:
		return mode
	}
}

// parseTelegramSentID parses "chatID:messageID" into its components.
func parseTelegramSentID(sentID string) (string, int, error) {
	idx := strings.LastIndex(sentID, ":")
	if idx < 0 {
		return "", 0, fmt.Errorf("invalid telegram sentID format: %s", sentID)
	}
	chatID := sentID[:idx]
	msgID, err := strconv.Atoi(sentID[idx+1:])
	if err != nil {
		return "", 0, fmt.Errorf("invalid message ID in sentID %s: %w", sentID, err)
	}
	return chatID, msgID, nil
}
