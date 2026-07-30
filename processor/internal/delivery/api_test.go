package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newAPISenderForTest(endpoint string) *APISender {
	s := NewAPISender(APIConfig{
		Endpoint:     endpoint,
		Secret:       "s3cr3t",
		SecretHeader: "X-Poracle-Secret",
		TimeoutMs:    2000,
		MaxRetries:   2,
	})
	s.retryBaseDur = 0 // no real sleeps in retry tests
	return s
}

func TestAPISendEnvelopeAndSentID(t *testing.T) {
	var gotBody map[string]any
	var gotOp, gotSecret, gotMsgID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOp = r.Header.Get("X-Poracle-Op")
		gotSecret = r.Header.Get("X-Poracle-Secret")
		gotMsgID = r.Header.Get("X-Poracle-Message-Id")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"abc123"}`))
	}))
	defer srv.Close()

	s := newAPISenderForTest(srv.URL)
	job := &Job{
		Target:   "u-42",
		Type:     "api:user",
		Name:     "James",
		Language: "en",
		MsgType:  "pokemon",
		Message:  json.RawMessage(`{"iv":100}`),
		Lat:      51.5, Lon: -0.1,
		Clean: 1, // clean bit
	}
	sent, err := s.Send(context.Background(), job)
	if err != nil {
		t.Fatalf("Send err = %v", err)
	}
	if gotOp != "send" {
		t.Errorf("X-Poracle-Op = %q, want send", gotOp)
	}
	if gotSecret != "s3cr3t" {
		t.Errorf("secret header = %q", gotSecret)
	}
	if gotBody["op"] != "send" || gotBody["version"].(float64) != 1 {
		t.Errorf("envelope op/version wrong: %v", gotBody)
	}
	dest := gotBody["destination"].(map[string]any)
	if dest["id"] != "u-42" || dest["type"] != "api:user" {
		t.Errorf("destination wrong: %v", dest)
	}
	if gotBody["alert_type"] != "pokemon" {
		t.Errorf("alert_type = %v", gotBody["alert_type"])
	}
	life := gotBody["lifecycle"].(map[string]any)
	if life["clean"] != true {
		t.Errorf("lifecycle.clean = %v, want true", life["clean"])
	}
	// SentID = "<dest>:<messageID>:<providerID>"
	parts := strings.SplitN(sent.ID, ":", 3)
	if len(parts) != 3 || parts[0] != "u-42" || parts[2] != "abc123" {
		t.Fatalf("SentID = %q, want u-42:<uuid>:abc123", sent.ID)
	}
	if gotMsgID != parts[1] {
		t.Errorf("header message id %q != sentID message id %q", gotMsgID, parts[1])
	}
	if _, err := uuid.Parse(parts[1]); err != nil {
		t.Errorf("message id %q not a uuid: %v", parts[1], err)
	}
}

func TestAPISendEnvelopeExtraFields(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := newAPISenderForTest(srv.URL)
	job := &Job{
		Target: "u-42", Type: "api:user", Message: json.RawMessage(`{}`),
		TrackingUIDs: []int64{45, 46},
		Areas:        []string{"london", "city"},
		ReplyToID:    "u-42:7c9e6a1f-0000-4000-8000-000000000000:abc123", // prior message SentID
	}
	if _, err := s.Send(context.Background(), job); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	uids, _ := body["tracking_uids"].([]any)
	if len(uids) != 2 || uids[0].(float64) != 45 {
		t.Errorf("tracking_uids = %v, want [45 46]", body["tracking_uids"])
	}
	areas, _ := body["areas"].([]any)
	if len(areas) != 2 || areas[0].(string) != "london" {
		t.Errorf("areas = %v, want [london city]", body["areas"])
	}
	// in_reply_to = the provider id half of the prior SentID
	if body["in_reply_to"] != "abc123" {
		t.Errorf("in_reply_to = %v, want abc123", body["in_reply_to"])
	}
}

func TestAPISendInReplyToFallsBackToMessageID(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := newAPISenderForTest(srv.URL)
	// prior message had no provider id (2-part SentID) → in_reply_to is the message id
	job := &Job{Target: "u-1", Type: "api:user", Message: json.RawMessage(`{}`),
		ReplyToID: "u-1:7c9e6a1f-0000-4000-8000-000000000000"}
	if _, err := s.Send(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if body["in_reply_to"] != "7c9e6a1f-0000-4000-8000-000000000000" {
		t.Errorf("in_reply_to = %v, want the message id", body["in_reply_to"])
	}
}

func TestAPISendNoProviderID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted) // 202, empty body
	}))
	defer srv.Close()
	s := newAPISenderForTest(srv.URL)
	sent, err := s.Send(context.Background(), &Job{Target: "u-1", Type: "api:user", Message: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("Send err = %v", err)
	}
	// No provider id → SentID is "<dest>:<messageID>" (2 parts)
	if parts := strings.SplitN(sent.ID, ":", 3); len(parts) != 2 {
		t.Fatalf("SentID = %q, want 2 parts", sent.ID)
	}
}

func TestAPIDeleteParsesSentID(t *testing.T) {
	var gotOp, gotProvider, gotMsgID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOp = r.Header.Get("X-Poracle-Op")
		var env map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &env)
		gotProvider, _ = env["provider_message_id"].(string)
		gotMsgID, _ = env["message_id"].(string)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	s := newAPISenderForTest(srv.URL)
	err := s.Delete(context.Background(), "u-42:7c9e6a1f-0000-4000-8000-000000000000:abc123")
	if err != nil {
		t.Fatalf("Delete err = %v", err)
	}
	if gotOp != "delete" || gotProvider != "abc123" || gotMsgID != "7c9e6a1f-0000-4000-8000-000000000000" {
		t.Errorf("delete envelope wrong: op=%q provider=%q msg=%q", gotOp, gotProvider, gotMsgID)
	}
}

func TestAPIDelete404IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	s := newAPISenderForTest(srv.URL)
	if err := s.Delete(context.Background(), "u-1:m1:p1"); err != nil {
		t.Errorf("Delete 404 should be success, got %v", err)
	}
}

func TestAPISend404IsPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	s := newAPISenderForTest(srv.URL)
	_, err := s.Send(context.Background(), &Job{Target: "u-1", Type: "api:user", Message: json.RawMessage(`{}`)})
	var perm *PermanentError
	if err == nil || !errors.As(err, &perm) {
		t.Fatalf("Send 404 = %v, want PermanentError", err)
	}
}

func TestAPISend401DropsWithoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	s := newAPISenderForTest(srv.URL)
	sent, err := s.Send(context.Background(), &Job{Target: "u-1", Type: "api:user", Message: json.RawMessage(`{}`)})
	if err != nil || sent != nil {
		t.Fatalf("Send 401 = (%v,%v), want (nil,nil) — drop without counting", sent, err)
	}
}

func TestAPISend5xxRetriesThenFails(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := newAPISenderForTest(srv.URL) // MaxRetries=2
	s.nowFunc = func() time.Time { return time.Unix(1770000000, 0) }
	_, err := s.Send(context.Background(), &Job{Target: "u-1", Type: "api:user", Message: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("Send 5xx should fail after retries")
	}
	if calls != 3 { // initial + 2 retries
		t.Errorf("server calls = %d, want 3 (1 initial + 2 retries)", calls)
	}
}

// TestAPISendAbsoluteExpiresAt pins that the envelope's expires_at uses the
// render-time absolute expiry (Job.ExpiresAt) when set, so queue latency
// between render and send cannot shift the reported expiry late. The TTH
// fallback (Job.ExpiresAt == 0) is covered by TestAPISendEnvelopeAndSentID.
func TestAPISendAbsoluteExpiresAt(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := newAPISenderForTest(srv.URL)
	job := &Job{
		Target: "u-1", Type: "api:user", Message: json.RawMessage(`{}`),
		ExpiresAt: 1770009999,
		TTH:       TTH{Hours: 1}, // must be ignored in favour of the absolute stamp
	}
	if _, err := s.Send(context.Background(), job); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	if got, _ := body["expires_at"].(float64); int64(got) != 1770009999 {
		t.Errorf("expires_at = %v, want 1770009999 (absolute Job.ExpiresAt)", body["expires_at"])
	}
}
