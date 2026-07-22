package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func newAPISenderForTest(endpoint string) *APISender {
	return NewAPISender(APIConfig{
		Endpoint:     endpoint,
		Secret:       "s3cr3t",
		SecretHeader: "X-Poracle-Secret",
		TimeoutMs:    2000,
		MaxRetries:   2,
	})
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
