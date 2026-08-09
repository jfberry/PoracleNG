package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/metrics"
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

	// sem is the wire-call concurrency semaphore (nil = unlimited until
	// SetConcurrency is called; the Dispatcher always calls it at wiring
	// time). A slot is held only for the duration of one HTTP round trip —
	// retry backoff runs slot-free, matching the Discord/Telegram senders
	// under per-destination lanes.
	sem   chan struct{}
	inFly atomic.Int64 // wire calls currently in flight (for [Status]/depth reporting)

	rlMu         sync.Mutex
	backoffUntil time.Time
	nowFunc      func() time.Time // injectable for tests; nil → time.Now

	retryBaseDur time.Duration // per-attempt backoff multiplier; NewAPISender defaults to 500ms, tests set 0
}

var _ Sender = (*APISender)(nil)

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
		cfg:          cfg,
		client:       &http.Client{Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond},
		retryBaseDur: 500 * time.Millisecond,
	}
}

func (s *APISender) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

// retryBase returns the per-attempt backoff multiplier. NewAPISender seeds
// this to 500ms; tests set it to 0 (via newAPISenderForTest) so retry loops
// don't actually sleep.
func (s *APISender) retryBase() time.Duration {
	return s.retryBaseDur
}

// APIInFlight reports current concurrent wire calls (for the [Status] log).
func (s *APISender) APIInFlight() int { return int(s.inFly.Load()) }

// SetConcurrency sizes the wire-call semaphore. n<=0 is clamped to 1 (never
// unlimited) — a configured sender always caps at >=1, matching the
// Discord/Telegram convention. Call once at wiring time, before traffic.
func (s *APISender) SetConcurrency(n int) {
	if n <= 0 {
		n = 1
	}
	s.sem = makeSem(n)
}

// roundTrip executes ONE api HTTP request while holding the wire-call
// concurrency slot, and releases the slot before returning — so the caller's
// retry backoff runs slot-free. Returns the (limited) response body, status,
// and Retry-After header.
func (s *APISender) roundTrip(ctx context.Context, req *http.Request) (respBody []byte, status int, retryAfter string, err error) {
	if s.sem != nil {
		select {
		case s.sem <- struct{}{}:
			defer func() { <-s.sem }()
		case <-ctx.Done():
			return nil, 0, "", ctx.Err()
		}
	}
	s.inFly.Add(1)
	metrics.DeliveryInFlight.WithLabelValues("api").Inc()
	defer func() {
		s.inFly.Add(-1)
		metrics.DeliveryInFlight.WithLabelValues("api").Dec()
	}()
	resp, derr := s.client.Do(req)
	if derr != nil {
		return nil, 0, "", derr
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	ra := resp.Header.Get("Retry-After")
	st := resp.StatusCode
	resp.Body.Close()
	return body, st, ra, nil
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
	TemplateType      string          `json:"template_type,omitempty"`
	TemplateID        string          `json:"template_id,omitempty"`
	TrackingUIDs      []int64         `json:"tracking_uids,omitempty"`
	Areas             []string        `json:"areas,omitempty"`
	InReplyTo         string          `json:"in_reply_to,omitempty"`
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
		Version:      apiEnvelopeVersion,
		Op:           "send",
		MessageID:    messageID,
		Revision:     0,
		SentAt:       s.now().Unix(),
		AlertType:    job.MsgType,
		TemplateType: job.TemplateType,
		TemplateID:   job.TemplateID,
		Destination:  apiDestination{ID: job.Target, Type: job.Type, Name: job.Name, Language: job.Language},
		Lifecycle:    &apiLifecycle{Clean: db.IsClean(job.Clean), Editable: db.IsEdit(job.Clean)},
		Payload:      json.RawMessage(job.Message),
	}
	if job.Lat != 0 || job.Lon != 0 {
		env.Location = &apiLocation{Lat: job.Lat, Lon: job.Lon}
	}
	if job.ExpiresAt > 0 {
		// Absolute expiry stamped at render time — immune to queue latency
		// between render and send.
		env.ExpiresAt = job.ExpiresAt
	} else if d := job.TTH.Duration(); d > 0 {
		env.ExpiresAt = s.now().Add(d).Unix()
	}
	env.TrackingUIDs = job.TrackingUIDs
	env.Areas = job.Areas
	if job.ReplyToID != "" {
		_, mid, pid := splitAPISentID(job.ReplyToID)
		if pid != "" {
			env.InReplyTo = pid
		} else {
			env.InReplyTo = mid
		}
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

// --- Edit / Delete ------------------------------------------------------------

// splitAPISentID parses "<dest>:<messageID>[:<providerID>]".
func splitAPISentID(sentID string) (dest, messageID, providerID string) {
	parts := strings.SplitN(sentID, ":", 3)
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2]
	case 2:
		return parts[0], parts[1], ""
	default:
		return sentID, "", ""
	}
}

// Edit POSTs an op:"edit" replacement. revision stays 0 in this plan
// (monotonic revision is a follow-up); the receiver keys on message_id.
func (s *APISender) Edit(ctx context.Context, sentID string, message json.RawMessage, _ []byte) error {
	dest, messageID, providerID := splitAPISentID(sentID)
	env := apiEnvelope{
		Version:           apiEnvelopeVersion,
		Op:                "edit",
		MessageID:         messageID,
		Revision:          0,
		SentAt:            s.now().Unix(),
		Destination:       apiDestination{ID: dest},
		ProviderMessageID: providerID,
		Payload:           json.RawMessage(message),
	}
	_, err := s.do(ctx, env, "edit")
	return err
}

// Delete POSTs an op:"delete". A 404 is treated as success (already gone).
func (s *APISender) Delete(ctx context.Context, sentID string) error {
	dest, messageID, providerID := splitAPISentID(sentID)
	env := apiEnvelope{
		Version:           apiEnvelopeVersion,
		Op:                "delete",
		MessageID:         messageID,
		Revision:          0,
		SentAt:            s.now().Unix(),
		Destination:       apiDestination{ID: dest},
		ProviderMessageID: providerID,
	}
	_, err := s.do(ctx, env, "delete")
	return err
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
	if d := until.Sub(s.now()); d > 0 {
		time.Sleep(d)
	}
}

// apiResponse is the outcome of a successful (2xx) POST.
type apiResponse struct {
	providerID string // the receiver's optional {"id":...}
}

var providerIDRe = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,128}$`)

// do POSTs the envelope with retry/backoff and status classification.
//
// Return contract (matches FairQueue.processJob expectations):
//   - 2xx                → (*apiResponse, nil)
//   - 404/410            → (nil, *PermanentError)   [counts → auto-disable]; but for op=="delete", 404 → (&apiResponse{}, nil) (already gone)
//   - 401/403/other 4xx  → (nil, nil)               [drop, do NOT count]
//   - 429                → honour Retry-After, retry up to MaxRetries
//   - 5xx / network      → backoff retry up to MaxRetries, then (nil, error) [counts]
func (s *APISender) do(ctx context.Context, env apiEnvelope, op string) (*apiResponse, error) {
	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal envelope: %w", err)
	}
	if s.cfg.LogOnly {
		log.Infof("api-delivery[log_only]: %s %s", op, string(body))
		return &apiResponse{}, nil
	}

	var lastErr error
	for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * s.retryBase())
		}
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Endpoint, bytes.NewReader(body))
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set(s.cfg.SecretHeader, s.cfg.SecretPrefix+s.cfg.Secret)
		req.Header.Set("X-Poracle-Op", op)
		req.Header.Set("X-Poracle-Message-Id", env.MessageID)
		req.Header.Set("User-Agent", "PoracleNG/"+s.cfg.Version)

		respBody, status, retryAfter, derr := s.roundTrip(ctx, req)
		if derr != nil {
			if ctx.Err() != nil {
				return nil, derr // shutting down — don't spin the retry loop
			}
			lastErr = derr
			continue // transient: retry
		}

		switch {
		case status >= 200 && status < 300:
			return &apiResponse{providerID: parseProviderID(respBody)}, nil
		case status == http.StatusNotFound || status == http.StatusGone:
			if op == "delete" {
				return &apiResponse{}, nil // already gone → success
			}
			return nil, &PermanentError{Err: fmt.Errorf("api endpoint %d", status), Reason: fmt.Sprintf("destination gone (%d)", status)}
		case status == http.StatusTooManyRequests:
			s.applyBackoff(retryAfter)
			lastErr = fmt.Errorf("api endpoint 429")
			continue
		case status >= 400 && status < 500:
			// 401/403/other 4xx: log and drop WITHOUT counting a failure.
			log.Errorf("api-delivery: %s dropped, endpoint returned %d (check secret/payload): %s", op, status, truncateForLog(respBody))
			return nil, nil
		default: // 5xx
			lastErr = fmt.Errorf("api endpoint %d", status)
			continue
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("api endpoint: retries exhausted")
	}
	return nil, lastErr
}

// applyBackoff records a Retry-After deadline so WaitForRateLimit can honour it.
func (s *APISender) applyBackoff(retryAfter string) {
	d := 1 * time.Second
	if retryAfter != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs > 0 {
			d = time.Duration(secs) * time.Second
		}
	}
	s.rlMu.Lock()
	s.backoffUntil = s.now().Add(d)
	s.rlMu.Unlock()
}

func truncateForLog(b []byte) string {
	const max = 300
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
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
