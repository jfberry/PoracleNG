package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type telegramCall struct {
	Method string
	Body   map[string]any
}

func setupTelegramServer(t *testing.T, handler func(method string, body map[string]any) (int, any)) (*httptest.Server, *TelegramSender, *[]telegramCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []telegramCall

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract method from URL: /bot{token}/{method}
		parts := strings.SplitN(r.URL.Path, "/", 4)
		if len(parts) < 3 {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		apiMethod := parts[len(parts)-1]

		bodyBytes, _ := io.ReadAll(r.Body)
		var bodyMap map[string]any
		json.Unmarshal(bodyBytes, &bodyMap) //nolint:errcheck

		mu.Lock()
		calls = append(calls, telegramCall{Method: apiMethod, Body: bodyMap})
		mu.Unlock()

		statusCode, resp := handler(apiMethod, bodyMap)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))

	sender := &TelegramSender{
		token:   "test-token",
		baseURL: server.URL,
		client:  server.Client(),
	}

	return server, sender, &calls
}

func okResponse(msgID int) (int, any) {
	return http.StatusOK, map[string]any{
		"ok":     true,
		"result": map[string]any{"message_id": msgID},
	}
}

func TestTelegramSendText(t *testing.T) {
	server, sender, calls := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		return okResponse(42)
	})
	defer server.Close()

	msg := json.RawMessage(`{"content":"Hello world","parse_mode":"HTML","webpage_preview":true}`)
	job := &Job{Target: "12345", Type: "telegram:user", Message: msg}

	result, err := sender.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ID != "12345:42" {
		t.Errorf("expected sentID '12345:42', got %q", result.ID)
	}

	c := *calls
	if len(c) != 1 {
		t.Fatalf("expected 1 call, got %d", len(c))
	}
	if c[0].Method != "sendMessage" {
		t.Errorf("expected method sendMessage, got %s", c[0].Method)
	}
	if c[0].Body["chat_id"] != "12345" {
		t.Errorf("expected chat_id '12345', got %v", c[0].Body["chat_id"])
	}
	if c[0].Body["text"] != "Hello world" {
		t.Errorf("expected text 'Hello world', got %v", c[0].Body["text"])
	}
	if c[0].Body["parse_mode"] != "HTML" {
		t.Errorf("expected parse_mode 'HTML', got %v", c[0].Body["parse_mode"])
	}
	// webpage_preview is true, so disable_web_page_preview should be false
	if c[0].Body["disable_web_page_preview"] != false {
		t.Errorf("expected disable_web_page_preview false, got %v", c[0].Body["disable_web_page_preview"])
	}
}

func TestTelegramSendOrder(t *testing.T) {
	msgCounter := 0
	server, sender, calls := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		msgCounter++
		return okResponse(msgCounter)
	})
	defer server.Close()

	msg := json.RawMessage(`{
		"content":"Hello",
		"sticker":"sticker123",
		"photo":"https://example.com/photo.png",
		"send_order":["sticker","photo","text"]
	}`)
	job := &Job{Target: "999", Type: "telegram:user", Message: msg}

	result, err := sender.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := *calls
	if len(c) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(c))
	}
	if c[0].Method != "sendSticker" {
		t.Errorf("expected sendSticker first, got %s", c[0].Method)
	}
	if c[1].Method != "sendPhoto" {
		t.Errorf("expected sendPhoto second, got %s", c[1].Method)
	}
	if c[2].Method != "sendMessage" {
		t.Errorf("expected sendMessage third, got %s", c[2].Method)
	}

	// Text message ID (3) should be the primary ID
	if result.ID != "999:3" {
		t.Errorf("expected sentID '999:3', got %q", result.ID)
	}
}

func TestTelegramSendDefaultOrder(t *testing.T) {
	server, sender, calls := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		return okResponse(10)
	})
	defer server.Close()

	// No send_order field — should use default: sticker, photo, text, location, venue
	// Only content is provided, so only sendMessage should be called.
	msg := json.RawMessage(`{"content":"test message"}`)
	job := &Job{Target: "111", Type: "telegram:user", Message: msg}

	_, err := sender.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := *calls
	if len(c) != 1 {
		t.Fatalf("expected 1 call (only text), got %d", len(c))
	}
	if c[0].Method != "sendMessage" {
		t.Errorf("expected sendMessage, got %s", c[0].Method)
	}
}

func TestTelegramSendLocation(t *testing.T) {
	server, sender, calls := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		return okResponse(50)
	})
	defer server.Close()

	msg := json.RawMessage(`{"location":true,"send_order":["location"]}`)
	job := &Job{Target: "222", Type: "telegram:user", Message: msg, Lat: 51.5074, Lon: -0.1278}

	result, err := sender.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := *calls
	if len(c) != 1 {
		t.Fatalf("expected 1 call, got %d", len(c))
	}
	if c[0].Method != "sendLocation" {
		t.Errorf("expected sendLocation, got %s", c[0].Method)
	}
	if c[0].Body["latitude"] != 51.5074 {
		t.Errorf("expected latitude 51.5074, got %v", c[0].Body["latitude"])
	}
	if c[0].Body["longitude"] != -0.1278 {
		t.Errorf("expected longitude -0.1278, got %v", c[0].Body["longitude"])
	}
	if result.ID != "222:50" {
		t.Errorf("expected sentID '222:50', got %q", result.ID)
	}
}

func TestTelegramSendVenue(t *testing.T) {
	server, sender, calls := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		return okResponse(60)
	})
	defer server.Close()

	msg := json.RawMessage(`{
		"venue":{"title":"Central Park","address":"New York, NY"},
		"send_order":["venue"]
	}`)
	job := &Job{Target: "333", Type: "telegram:user", Message: msg, Lat: 40.785, Lon: -73.968}

	_, err := sender.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := *calls
	if len(c) != 1 {
		t.Fatalf("expected 1 call, got %d", len(c))
	}
	if c[0].Method != "sendVenue" {
		t.Errorf("expected sendVenue, got %s", c[0].Method)
	}
	if c[0].Body["title"] != "Central Park" {
		t.Errorf("expected title 'Central Park', got %v", c[0].Body["title"])
	}
	if c[0].Body["address"] != "New York, NY" {
		t.Errorf("expected address 'New York, NY', got %v", c[0].Body["address"])
	}
}

func TestTelegramParseModeNormalization(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"markdown", "Markdown"},
		{"Markdown", "Markdown"},
		{"MARKDOWN", "Markdown"},
		{"markdownv2", "MarkdownV2"},
		{"MarkdownV2", "MarkdownV2"},
		{"html", "HTML"},
		{"HTML", "HTML"},
		{"Html", "HTML"},
		{"", "Markdown"},
	}

	for _, tt := range tests {
		result := normalizeTelegramParseMode(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeTelegramParseMode(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestTelegramDelete(t *testing.T) {
	server, sender, calls := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"ok": true}
	})
	defer server.Close()

	err := sender.Delete(context.Background(), "12345:99")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := *calls
	if len(c) != 1 {
		t.Fatalf("expected 1 call, got %d", len(c))
	}
	if c[0].Method != "deleteMessage" {
		t.Errorf("expected deleteMessage, got %s", c[0].Method)
	}
	if c[0].Body["chat_id"] != "12345" {
		t.Errorf("expected chat_id '12345', got %v", c[0].Body["chat_id"])
	}
	// message_id comes through as float64 from JSON unmarshaling
	if c[0].Body["message_id"] != float64(99) {
		t.Errorf("expected message_id 99, got %v", c[0].Body["message_id"])
	}
}

func TestTelegramEdit(t *testing.T) {
	server, sender, calls := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"ok": true, "result": map[string]any{"message_id": 99}}
	})
	defer server.Close()

	editMsg := json.RawMessage(`{"content":"Updated text","parse_mode":"HTML"}`)
	err := sender.Edit(context.Background(), "12345:99", editMsg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := *calls
	if len(c) != 1 {
		t.Fatalf("expected 1 call, got %d", len(c))
	}
	if c[0].Method != "editMessageText" {
		t.Errorf("expected editMessageText, got %s", c[0].Method)
	}
	if c[0].Body["text"] != "Updated text" {
		t.Errorf("expected text 'Updated text', got %v", c[0].Body["text"])
	}
	if c[0].Body["parse_mode"] != "HTML" {
		t.Errorf("expected parse_mode 'HTML', got %v", c[0].Body["parse_mode"])
	}
	if c[0].Body["disable_web_page_preview"] != true {
		t.Errorf("expected disable_web_page_preview true, got %v", c[0].Body["disable_web_page_preview"])
	}
}

func TestTelegramRateLimit429(t *testing.T) {
	attempt := 0
	server, sender, calls := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		attempt++
		if attempt == 1 {
			return http.StatusTooManyRequests, map[string]any{
				"ok":          false,
				"description": "Too Many Requests: retry after 1",
				"parameters":  map[string]any{"retry_after": 1},
			}
		}
		return okResponse(77)
	})
	defer server.Close()

	msg := json.RawMessage(`{"content":"retry test"}`)
	job := &Job{Target: "444", Type: "telegram:user", Message: msg}

	result, err := sender.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := *calls
	if len(c) != 2 {
		t.Fatalf("expected 2 calls (1 retry), got %d", len(c))
	}
	if result.ID != "444:77" {
		t.Errorf("expected sentID '444:77', got %q", result.ID)
	}
}

func TestTelegramPermanentError(t *testing.T) {
	server, sender, _ := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		return http.StatusForbidden, map[string]any{
			"ok":          false,
			"description": "Forbidden: bot was blocked by the user",
		}
	})
	defer server.Close()

	msg := json.RawMessage(`{"content":"blocked test"}`)
	job := &Job{Target: "555", Type: "telegram:user", Message: msg}

	_, err := sender.Send(context.Background(), job)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	permErr, ok := err.(*PermanentError)
	if !ok {
		t.Fatalf("expected *PermanentError, got %T: %v", err, err)
	}
	if permErr.Reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestTelegramSentIDFormat(t *testing.T) {
	server, sender, _ := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		return okResponse(12345)
	})
	defer server.Close()

	msg := json.RawMessage(`{"content":"test"}`)
	job := &Job{Target: "98765", Type: "telegram:user", Message: msg}

	result, err := sender.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify format is "chatID:messageID"
	parts := strings.SplitN(result.ID, ":", 2)
	if len(parts) != 2 {
		t.Fatalf("expected sentID in format 'chatID:messageID', got %q", result.ID)
	}
	if parts[0] != "98765" {
		t.Errorf("expected chatID '98765', got %q", parts[0])
	}
	msgID, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("expected numeric messageID, got %q: %v", parts[1], err)
	}
	if msgID != 12345 {
		t.Errorf("expected messageID 12345, got %d", msgID)
	}
}
