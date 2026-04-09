package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func newTestDiscordSender(serverURL string) *DiscordSender {
	ds := NewDiscordSender("test-bot-token", false, 0)
	ds.baseURL = serverURL
	return ds
}

func TestDiscordSendChannel(t *testing.T) {
	var gotAuth string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg123"}`)) //nolint:errcheck
	}))
	defer server.Close()

	ds := newTestDiscordSender(server.URL)
	job := &Job{
		Target:  "channel456",
		Type:    "discord:channel",
		Message: json.RawMessage(`{"content":"hello"}`),
	}

	sent, err := ds.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotAuth != "Bot test-bot-token" {
		t.Errorf("expected auth 'Bot test-bot-token', got %q", gotAuth)
	}

	if !strings.Contains(string(gotBody), `"content":"hello"`) {
		t.Errorf("expected body to contain content, got %s", gotBody)
	}

	expectedID := "channel456:msg123"
	if sent.ID != expectedID {
		t.Errorf("expected sent ID %q, got %q", expectedID, sent.ID)
	}
}

func TestDiscordSendDM(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/users/@me/channels" {
			// DM channel creation
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"recipient_id":"user789"`) {
				t.Errorf("expected recipient_id in DM create body, got %s", body)
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":"dm_chan_001"}`)) //nolint:errcheck
			return
		}

		// Message send
		if !strings.Contains(r.URL.Path, "/channels/dm_chan_001/messages") {
			t.Errorf("expected message to dm channel, got path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg456"}`)) //nolint:errcheck
	}))
	defer server.Close()

	ds := newTestDiscordSender(server.URL)
	job := &Job{
		Target:  "user789",
		Type:    "discord:user",
		Message: json.RawMessage(`{"content":"dm hello"}`),
	}

	sent, err := ds.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := atomic.LoadInt32(&requestCount)
	if count != 2 {
		t.Errorf("expected 2 requests (DM create + send), got %d", count)
	}

	expectedID := "dm_chan_001:msg456"
	if sent.ID != expectedID {
		t.Errorf("expected sent ID %q, got %q", expectedID, sent.ID)
	}
}

func TestDiscordSendDMCached(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/users/@me/channels" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":"dm_chan_cached"}`)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg789"}`)) //nolint:errcheck
	}))
	defer server.Close()

	ds := newTestDiscordSender(server.URL)
	job := &Job{
		Target:  "user_cached",
		Type:    "discord:user",
		Message: json.RawMessage(`{"content":"first"}`),
	}

	// First send: creates DM channel
	_, err := ds.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("first send error: %v", err)
	}

	firstCount := atomic.LoadInt32(&requestCount)
	if firstCount != 2 {
		t.Errorf("expected 2 requests for first send, got %d", firstCount)
	}

	// Second send: should reuse cached channel
	job.Message = json.RawMessage(`{"content":"second"}`)
	_, err = ds.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("second send error: %v", err)
	}

	finalCount := atomic.LoadInt32(&requestCount)
	if finalCount != 3 {
		t.Errorf("expected 3 total requests (1 DM create + 2 sends), got %d", finalCount)
	}
}

func TestDiscordSendThread(t *testing.T) {
	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_thread"}`)) //nolint:errcheck
	}))
	defer server.Close()

	ds := newTestDiscordSender(server.URL)
	job := &Job{
		Target:  "thread123",
		Type:    "discord:thread",
		Message: json.RawMessage(`{"content":"thread msg"}`),
	}

	sent, err := ds.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/channels/thread123/messages" {
		t.Errorf("expected path /channels/thread123/messages, got %s", gotPath)
	}

	expectedID := "thread123:msg_thread"
	if sent.ID != expectedID {
		t.Errorf("expected sent ID %q, got %q", expectedID, sent.ID)
	}
}

func TestDiscordSendWebhook(t *testing.T) {
	var gotAuth string
	var gotQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_webhook"}`)) //nolint:errcheck
	}))
	defer server.Close()

	webhookURL := server.URL + "/api/webhooks/123/abc"
	ds := newTestDiscordSender(server.URL)
	job := &Job{
		Target:  webhookURL,
		Type:    "webhook",
		Message: json.RawMessage(`{"content":"webhook msg"}`),
	}

	sent, err := ds.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotAuth != "" {
		t.Errorf("webhook should not have Authorization header, got %q", gotAuth)
	}

	if gotQuery != "wait=true" {
		t.Errorf("expected query 'wait=true', got %q", gotQuery)
	}

	expectedID := webhookURL + ":msg_webhook"
	if sent.ID != expectedID {
		t.Errorf("expected sent ID %q, got %q", expectedID, sent.ID)
	}
}

func TestDiscordDelete(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ds := newTestDiscordSender(server.URL)
	err := ds.Delete(context.Background(), "channel123:msg456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", gotMethod)
	}

	if gotPath != "/channels/channel123/messages/msg456" {
		t.Errorf("expected path /channels/channel123/messages/msg456, got %s", gotPath)
	}

	if gotAuth != "Bot test-bot-token" {
		t.Errorf("expected auth header, got %q", gotAuth)
	}
}

func TestDiscordDeleteWebhook(t *testing.T) {
	var gotMethod string
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	webhookURL := server.URL + "/api/webhooks/123/abc"
	err := ds_deleteWebhook(t, server.URL, webhookURL, &gotMethod, &gotAuth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", gotMethod)
	}

	if gotAuth != "" {
		t.Errorf("webhook delete should not have auth, got %q", gotAuth)
	}
}

func ds_deleteWebhook(t *testing.T, serverURL, webhookURL string, gotMethod, gotAuth *string) error {
	t.Helper()
	ds := newTestDiscordSender(serverURL)
	return ds.Delete(context.Background(), webhookURL+":msg789")
}

func TestDiscordEdit(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg456"}`)) //nolint:errcheck
	}))
	defer server.Close()

	ds := newTestDiscordSender(server.URL)
	err := ds.Edit(context.Background(), "channel123:msg456", json.RawMessage(`{"content":"edited"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodPatch {
		t.Errorf("expected PATCH, got %s", gotMethod)
	}

	if gotPath != "/channels/channel123/messages/msg456" {
		t.Errorf("expected path /channels/channel123/messages/msg456, got %s", gotPath)
	}

	if !strings.Contains(string(gotBody), `"content":"edited"`) {
		t.Errorf("expected edited content in body, got %s", gotBody)
	}
}

func TestDiscordRateLimit429(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "application/json")
		if count == 1 {
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"retry_after":0.01,"message":"rate limited"}`)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_after_retry"}`)) //nolint:errcheck
	}))
	defer server.Close()

	ds := newTestDiscordSender(server.URL)
	job := &Job{
		Target:  "channel_rl",
		Type:    "discord:channel",
		Message: json.RawMessage(`{"content":"retry me"}`),
	}

	sent, err := ds.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	finalAttempts := atomic.LoadInt32(&attempts)
	if finalAttempts < 2 {
		t.Errorf("expected at least 2 attempts, got %d", finalAttempts)
	}

	if sent.ID != "channel_rl:msg_after_retry" {
		t.Errorf("expected sent ID 'channel_rl:msg_after_retry', got %q", sent.ID)
	}
}

func TestDiscordPermanentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"code":50007,"message":"Cannot send messages to this user"}`)) //nolint:errcheck
	}))
	defer server.Close()

	ds := newTestDiscordSender(server.URL)
	job := &Job{
		Target:  "channel_perm",
		Type:    "discord:channel",
		Message: json.RawMessage(`{"content":"fail"}`),
	}

	_, err := ds.Send(context.Background(), job)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	permErr, ok := err.(*PermanentError)
	if !ok {
		t.Fatalf("expected *PermanentError, got %T: %v", err, err)
	}

	if !strings.Contains(permErr.Reason, "50007") {
		t.Errorf("expected reason to contain 50007, got %q", permErr.Reason)
	}
}

func TestDiscordImageUpload(t *testing.T) {
	// Image server
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// Minimal valid PNG (1x1 pixel)
		w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) //nolint:errcheck
	}))
	defer imageServer.Close()

	var gotContentType string

	// Discord API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_img"}`)) //nolint:errcheck
	}))
	defer server.Close()

	ds := newTestDiscordSender(server.URL)
	ds.uploadImages = true

	msg := `{"content":"with image","embeds":[{"image":{"url":"` + imageServer.URL + `/map.png"}}]}`
	job := &Job{
		Target:  "channel_img",
		Type:    "discord:channel",
		Message: json.RawMessage(msg),
	}

	sent, err := ds.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Errorf("expected multipart content type, got %q", gotContentType)
	}

	if sent.ID != "channel_img:msg_img" {
		t.Errorf("expected sent ID 'channel_img:msg_img', got %q", sent.ID)
	}
}
