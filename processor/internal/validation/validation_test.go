package validation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNoopWhenURLEmpty(t *testing.T) {
	v := New("", "open", 0, 0)
	if _, ok := v.(Noop); !ok {
		t.Fatalf("empty URL should produce Noop, got %T", v)
	}
	if v.Enabled() {
		t.Error("Noop must report Enabled() == false")
	}
	got := v.Validate(context.Background(), Request{})
	if !got.Allow {
		t.Error("Noop must allow")
	}
}

// newTestServer returns a test server that decodes the request, calls fn, and
// writes the response. The Request body is captured in *got for assertions.
func newTestServer(t *testing.T, got *Request, status int, payload Response) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if got != nil {
			_ = json.Unmarshal(body, got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
	}))
}

func TestValidateAllow(t *testing.T) {
	srv := newTestServer(t, nil, http.StatusOK, Response{Success: true})
	defer srv.Close()

	v := New(srv.URL, "open", 1000, 1)
	d := v.Validate(context.Background(), Request{
		Human: Human{ID: "u1", Type: "discord:user", Name: "Alice"},
	})
	if !d.Allow {
		t.Error("expected Allow=true")
	}
	if d.FailureMessage != "" {
		t.Errorf("expected no failure message, got %q", d.FailureMessage)
	}
}

func TestValidateDenyWithMessage(t *testing.T) {
	srv := newTestServer(t, nil, http.StatusOK, Response{
		Success:        false,
		FailureMessage: "Subscription expired",
	})
	defer srv.Close()

	v := New(srv.URL, "open", 1000, 1)
	d := v.Validate(context.Background(), Request{})
	if d.Allow {
		t.Error("expected Allow=false")
	}
	if d.FailureMessage != "Subscription expired" {
		t.Errorf("FailureMessage = %q", d.FailureMessage)
	}
}

func TestValidateDenySilent(t *testing.T) {
	srv := newTestServer(t, nil, http.StatusOK, Response{Success: false})
	defer srv.Close()

	v := New(srv.URL, "open", 1000, 1)
	d := v.Validate(context.Background(), Request{})
	if d.Allow {
		t.Error("expected Allow=false")
	}
	if d.FailureMessage != "" {
		t.Errorf("silent deny must have no message; got %q", d.FailureMessage)
	}
}

func TestValidateRequestPayload(t *testing.T) {
	var got Request
	srv := newTestServer(t, &got, http.StatusOK, Response{Success: true})
	defer srv.Close()

	v := New(srv.URL, "open", 1000, 1)
	v.Validate(context.Background(), Request{
		Human:       Human{ID: "u1", Type: "telegram:user", Name: "Bob"},
		Areas:       []string{"London/Westminster"},
		WebhookType: "raid",
		Webhook:     json.RawMessage(`{"gym_id":"abc"}`),
	})

	if got.Human.ID != "u1" || got.Human.Type != "telegram:user" || got.Human.Name != "Bob" {
		t.Errorf("human round-trip wrong: %+v", got.Human)
	}
	if len(got.Areas) != 1 || got.Areas[0] != "London/Westminster" {
		t.Errorf("areas round-trip wrong: %v", got.Areas)
	}
	if got.WebhookType != "raid" {
		t.Errorf("webhook_type round-trip wrong: %q", got.WebhookType)
	}
	if string(got.Webhook) != `{"gym_id":"abc"}` {
		t.Errorf("raw webhook round-trip wrong: %s", got.Webhook)
	}
}

func TestValidateFailOpen(t *testing.T) {
	// 500 response — fail-open allows.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	v := New(srv.URL, "open", 1000, 1)
	if d := v.Validate(context.Background(), Request{}); !d.Allow {
		t.Error("fail-open with 5xx must allow")
	}
}

func TestValidateFailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	v := New(srv.URL, "closed", 1000, 1)
	if d := v.Validate(context.Background(), Request{}); d.Allow {
		t.Error("fail-closed with 5xx must deny")
	}
}

func TestValidateTimeout(t *testing.T) {
	// Server sleeps longer than the client timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Response{Success: true})
	}))
	defer srv.Close()

	// Open: timeout → allow
	vOpen := New(srv.URL, "open", 50, 1)
	if d := vOpen.Validate(context.Background(), Request{}); !d.Allow {
		t.Error("fail-open timeout must allow")
	}

	// Closed: timeout → deny
	vClosed := New(srv.URL, "closed", 50, 1)
	if d := vClosed.Validate(context.Background(), Request{}); d.Allow {
		t.Error("fail-closed timeout must deny")
	}
}

func TestValidateMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	v := New(srv.URL, "open", 1000, 1)
	if d := v.Validate(context.Background(), Request{}); !d.Allow {
		t.Error("malformed response in fail-open must allow")
	}
}

func TestConcurrencyDefault(t *testing.T) {
	v := New("http://example/", "open", 0, 0)
	if c := v.Concurrency(); c != 16 {
		t.Errorf("default concurrency = %d, want 16", c)
	}
}

func TestNoopConcurrencyZero(t *testing.T) {
	if c := (Noop{}).Concurrency(); c != 0 {
		t.Errorf("Noop.Concurrency = %d, want 0", c)
	}
}
