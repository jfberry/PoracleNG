package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

type humanAPIMockSender struct {
	mu   sync.Mutex
	jobs []*delivery.Job
	ch   chan *delivery.Job
}

func newHumanAPIMockSender() *humanAPIMockSender {
	return &humanAPIMockSender{ch: make(chan *delivery.Job, 4)}
}

func (m *humanAPIMockSender) Send(_ context.Context, job *delivery.Job) (*delivery.SentMessage, error) {
	m.mu.Lock()
	m.jobs = append(m.jobs, job)
	m.mu.Unlock()
	m.ch <- job
	return &delivery.SentMessage{ID: "sent-" + job.Target}, nil
}

func (m *humanAPIMockSender) Delete(_ context.Context, _ string) error { return nil }

func (m *humanAPIMockSender) Edit(_ context.Context, _ string, _ json.RawMessage, _ []byte) error {
	return nil
}

func (m *humanAPIMockSender) Platform() string { return "discord" }

func (m *humanAPIMockSender) WaitForRateLimit(string) {}

func newHumanAPITestDeps(t *testing.T, humans *store.MockHumanStore, sender *humanAPIMockSender) (*TrackingDeps, *int) {
	t.Helper()

	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"cmd.start":         "start",
		"msg.start.success": "Alert messages resumed",
		"msg.stop.success":  "All alert messages have been stopped, you can resume them with {0}{1}",
	}))

	cfg := &config.Config{}
	cfg.General.Locale = "en"
	cfg.Discord.Prefix = "?"

	senders := map[string]delivery.Sender{"discord": sender}
	tracker := delivery.NewMessageTracker(t.TempDir(), senders)
	dispatcher := delivery.NewDispatcherWithSenders(senders, tracker, 10, delivery.QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	dispatcher.Start()
	t.Cleanup(dispatcher.Stop)

	reloads := 0
	return &TrackingDeps{
		Humans:       humans,
		Config:       cfg,
		Translations: bundle,
		Dispatcher:   dispatcher,
		ReloadFunc: func() {
			reloads++
		},
	}, &reloads
}

func waitForHumanAPISend(t *testing.T, sender *humanAPIMockSender) *delivery.Job {
	t.Helper()

	select {
	case job := <-sender.ch:
		return job
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dispatched confirmation")
		return nil
	}
}

func TestHandleStartHumanResetsFailsAndDispatchesConfirmation(t *testing.T) {
	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{
		ID:               "user1",
		Type:             "discord:user",
		Name:             "Ash",
		Enabled:          false,
		Fails:            7,
		Language:         "en",
		CurrentProfileNo: 1,
	})
	sender := newHumanAPIMockSender()
	deps, reloads := newHumanAPITestDeps(t, humans, sender)

	r := gin.New()
	r.POST("/api/humans/:id/start", HandleStartHuman(deps))

	req := httptest.NewRequest(http.MethodPost, "/api/humans/user1/start", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := resp["message"]; got != "Alert messages resumed" {
		t.Fatalf("expected start message, got %v", got)
	}
	if *reloads != 1 {
		t.Fatalf("expected reload once, got %d", *reloads)
	}

	job := waitForHumanAPISend(t, sender)
	if job.Target != "user1" {
		t.Fatalf("expected target user1, got %s", job.Target)
	}
	if job.Type != "discord:user" {
		t.Fatalf("expected discord:user job, got %s", job.Type)
	}

	var payload map[string]string
	if err := json.Unmarshal(job.Message, &payload); err != nil {
		t.Fatalf("decode dispatched message: %v", err)
	}
	if payload["content"] != "Alert messages resumed" {
		t.Fatalf("expected dispatched start confirmation, got %q", payload["content"])
	}

	human, err := humans.Get("user1")
	if err != nil {
		t.Fatalf("lookup updated human: %v", err)
	}
	if !human.Enabled {
		t.Fatal("expected human to be enabled")
	}
	if human.Fails != 0 {
		t.Fatalf("expected fails reset to 0, got %d", human.Fails)
	}
}

func TestHandleStopHumanDispatchesConfirmationWithCommandHint(t *testing.T) {
	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{
		ID:               "user1",
		Type:             "discord:user",
		Name:             "Ash",
		Enabled:          true,
		Fails:            4,
		Language:         "en",
		CurrentProfileNo: 1,
	})
	sender := newHumanAPIMockSender()
	deps, reloads := newHumanAPITestDeps(t, humans, sender)

	r := gin.New()
	r.POST("/api/humans/:id/stop", HandleStopHuman(deps))

	req := httptest.NewRequest(http.MethodPost, "/api/humans/user1/stop", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	want := "All alert messages have been stopped, you can resume them with ?start"

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := resp["message"]; got != want {
		t.Fatalf("expected stop message %q, got %v", want, got)
	}
	if *reloads != 1 {
		t.Fatalf("expected reload once, got %d", *reloads)
	}

	job := waitForHumanAPISend(t, sender)
	var payload map[string]string
	if err := json.Unmarshal(job.Message, &payload); err != nil {
		t.Fatalf("decode dispatched message: %v", err)
	}
	if payload["content"] != want {
		t.Fatalf("expected dispatched stop confirmation %q, got %q", want, payload["content"])
	}

	human, err := humans.Get("user1")
	if err != nil {
		t.Fatalf("lookup updated human: %v", err)
	}
	if human.Enabled {
		t.Fatal("expected human to be disabled")
	}
	if human.Fails != 4 {
		t.Fatalf("expected fails to remain 4 on stop, got %d", human.Fails)
	}
}

func TestHandleStopHumanSilentSkipsDispatch(t *testing.T) {
	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{
		ID:               "user1",
		Type:             "discord:user",
		Name:             "Ash",
		Enabled:          true,
		Language:         "en",
		CurrentProfileNo: 1,
	})
	sender := newHumanAPIMockSender()
	deps, _ := newHumanAPITestDeps(t, humans, sender)

	r := gin.New()
	r.POST("/api/humans/:id/stop", HandleStopHuman(deps))

	req := httptest.NewRequest(http.MethodPost, "/api/humans/user1/stop?silent=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	select {
	case job := <-sender.ch:
		t.Fatalf("expected no dispatched confirmation, got job for %s", job.Target)
	case <-time.After(200 * time.Millisecond):
	}
}
