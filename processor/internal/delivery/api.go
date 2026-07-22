package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/pokemon/poracleng/processor/internal/db"
	log "github.com/sirupsen/logrus"
)

const apiEnvelopeVersion = 1

// APIConfig configures an APISender.
type APIConfig struct {
	Endpoint     string
	Secret       string
	SecretHeader string // default applied by caller; e.g. "X-Poracle-Secret"
	SecretPrefix string // e.g. "Bearer "
	TimeoutMs    int
	MaxRetries   int
	LogOnly      bool
	Version      string // PoracleNG version, for User-Agent
}

// APISender delivers messages to a generic HTTP endpoint (the "api" platform).
// It implements delivery.Sender.
type APISender struct {
	cfg    APIConfig
	client *http.Client

	rlMu         sync.Mutex
	backoffUntil time.Time
	nowFunc      func() time.Time // injectable for tests; nil → time.Now
}

// NewAPISender constructs an APISender. Timeout/MaxRetries fall back to sane
// values if unset so tests and misconfigurations don't hang or spin.
func NewAPISender(cfg APIConfig) *APISender {
	if cfg.SecretHeader == "" {
		cfg.SecretHeader = "X-Poracle-Secret"
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 10000
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	return &APISender{
		cfg:    cfg,
		client: &http.Client{Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond},
	}
}

func (s *APISender) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

// Platform identifies the sender.
func (s *APISender) Platform() string { return "api" }

// --- envelope ---------------------------------------------------------------

type apiDestination struct {
	ID       string `json:"id"`
	Type     string `json:"type,omitempty"`
	Name     string `json:"name,omitempty"`
	Language string `json:"language,omitempty"`
}

type apiLocation struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type apiLifecycle struct {
	Clean    bool `json:"clean"`
	Editable bool `json:"editable"`
}

type apiEnvelope struct {
	Version           int             `json:"version"`
	Op                string          `json:"op"`
	MessageID         string          `json:"message_id"`
	Revision          int             `json:"revision"`
	SentAt            int64           `json:"sent_at"`
	AlertType         string          `json:"alert_type,omitempty"`
	TemplateID        string          `json:"template_id,omitempty"`
	Destination       apiDestination  `json:"destination"`
	Location          *apiLocation    `json:"location,omitempty"`
	ExpiresAt         int64           `json:"expires_at,omitempty"`
	Lifecycle         *apiLifecycle   `json:"lifecycle,omitempty"`
	ProviderMessageID string          `json:"provider_message_id,omitempty"`
	Payload           json.RawMessage `json:"payload,omitempty"`
}

// buildSendEnvelope constructs the op:"send" envelope for a job.
func (s *APISender) buildSendEnvelope(job *Job, messageID string) apiEnvelope {
	env := apiEnvelope{
		Version:     apiEnvelopeVersion,
		Op:          "send",
		MessageID:   messageID,
		Revision:    0,
		SentAt:      s.now().Unix(),
		AlertType:   job.MsgType,
		TemplateID:  job.TemplateID,
		Destination: apiDestination{ID: job.Target, Type: job.Type, Name: job.Name, Language: job.Language},
		Lifecycle:   &apiLifecycle{Clean: db.IsClean(job.Clean), Editable: db.IsEdit(job.Clean)},
		Payload:     json.RawMessage(job.Message),
	}
	if job.Lat != 0 || job.Lon != 0 {
		env.Location = &apiLocation{Lat: job.Lat, Lon: job.Lon}
	}
	if d := job.TTH.Duration(); d > 0 {
		env.ExpiresAt = s.now().Add(d).Unix()
	}
	return env
}

// --- Send -------------------------------------------------------------------

// Send POSTs an op:"send" envelope and returns a SentMessage whose ID encodes
// "<destID>:<messageID>[:<providerID>]" so Edit/Delete can address the message.
func (s *APISender) Send(ctx context.Context, job *Job) (*SentMessage, error) {
	messageID := uuid.NewString()
	env := s.buildSendEnvelope(job, messageID)

	resp, err := s.do(ctx, env, "send")
	if err != nil {
		return nil, err
	}
	if resp == nil {
		// Dropped without a failure (e.g. 401/403/other-4xx or log_only). The
		// queue treats (nil,nil) as "handled, don't count, don't track".
		return nil, nil
	}

	sentID := job.Target + ":" + messageID
	if resp.providerID != "" {
		sentID += ":" + resp.providerID
	}
	return &SentMessage{ID: sentID}, nil
}

// WaitForRateLimit blocks until any backoff learned from a prior 429 elapses.
// Called before the platform semaphore is acquired (Sender interface contract).
func (s *APISender) WaitForRateLimit(target string) {
	s.rlMu.Lock()
	until := s.backoffUntil
	s.rlMu.Unlock()
	if until.IsZero() {
		return
	}
	if d := time.Until(until); d > 0 {
		time.Sleep(d)
	}
}

// apiResponse is the outcome of a successful (2xx) POST.
type apiResponse struct {
	providerID string // the receiver's optional {"id":...}
}

var providerIDRe = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,128}$`)

// do POSTs the envelope. Task 3 stub: happy-path + basic non-2xx handling.
// Task 4 replaces this with full retry/backoff/status classification.
func (s *APISender) do(ctx context.Context, env apiEnvelope, op string) (*apiResponse, error) {
	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal envelope: %w", err)
	}
	if s.cfg.LogOnly {
		log.Infof("api-delivery[log_only]: %s %s", op, string(body))
		return &apiResponse{}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set(s.cfg.SecretHeader, s.cfg.SecretPrefix+s.cfg.Secret)
	req.Header.Set("X-Poracle-Op", op)
	req.Header.Set("X-Poracle-Message-Id", env.MessageID)
	req.Header.Set("User-Agent", "PoracleNG/"+s.cfg.Version)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return &apiResponse{providerID: parseProviderID(respBody)}, nil
	}
	return nil, fmt.Errorf("api endpoint returned %d", resp.StatusCode)
}

// parseProviderID extracts a colon-free {"id":...} from a 2xx response body.
// Anything else (malformed, wrong charset, empty) yields "".
func parseProviderID(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var r struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return ""
	}
	if r.ID == "" || !providerIDRe.MatchString(r.ID) {
		if r.ID != "" {
			log.Warnf("api-delivery: ignoring invalid provider id %q", r.ID)
		}
		return ""
	}
	return r.ID
}
