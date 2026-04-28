// Package validation implements the post-match / pre-enrichment HTTP hook.
//
// For every matched user (after rate-limit pre-filtering, before enrichment)
// the processor POSTs a Request to the configured URL. The hook can:
//
//   - approve the user (response: {"success": true})
//   - silently deny the user (response: {"success": false})
//   - deny with a message that is delivered to the user as a bypass
//     notification (response: {"success": false, "failure_message": "..."})
//
// On HTTP error / timeout / malformed response, behaviour is governed by
// fail_mode ("open" = approve, "closed" = deny).
//
// The configured URL is empty by default and the resulting Validator is a
// no-op — no HTTP work, no overhead, drop-in safe.
package validation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/metrics"
)

// Request is the JSON body POSTed to the validator URL for each matched user.
type Request struct {
	Human       Human           `json:"human"`
	Areas       []string        `json:"areas"`
	WebhookType string          `json:"webhook_type"`
	Webhook     json.RawMessage `json:"webhook,omitempty"`
}

// Human identifies the matched user / channel / webhook destination.
type Human struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// Response is the validator's reply.
type Response struct {
	Success        bool   `json:"success"`
	FailureMessage string `json:"failure_message,omitempty"`
}

// Decision is what the processor does with a single user after validation.
type Decision struct {
	// Allow is true when the user should keep flowing through the pipeline.
	Allow bool
	// FailureMessage is the operator-supplied text to deliver to a denied
	// user. Empty when not present (silent deny) or when Allow is true.
	FailureMessage string
}

// Validator validates a single matched user.
type Validator interface {
	// Validate returns a Decision for the given request. Implementations
	// must honour ctx cancellation. A zero-value Decision (Allow=false,
	// no message) is a silent deny.
	Validate(ctx context.Context, req Request) Decision
	// Concurrency returns the recommended fan-out cap for the calling
	// processor. The HTTP implementation returns the configured value;
	// the no-op returns 0 (unbounded — the caller can skip its semaphore).
	Concurrency() int
	// Enabled reports whether this validator does real work. Callers can
	// use this to short-circuit setup (build the Request once, etc.).
	Enabled() bool
}

// Noop is returned when no URL is configured. It approves every request
// without any work.
type Noop struct{}

// Validate always allows.
func (Noop) Validate(context.Context, Request) Decision { return Decision{Allow: true} }

// Concurrency returns 0, signalling "no fan-out cap needed".
func (Noop) Concurrency() int { return 0 }

// Enabled reports false — the no-op skips all work.
func (Noop) Enabled() bool { return false }

// HTTP is the production validator. One shared *http.Client is reused for
// every call.
type HTTP struct {
	url            string
	client         *http.Client
	failClosed     bool
	maxConcurrent  int
}

// New constructs a Validator for the given config. If url is empty a Noop
// is returned. timeoutMs and maxConcurrent are taken from [tuning] with
// fallbacks for zero values.
func New(url, failMode string, timeoutMs, maxConcurrent int) Validator {
	if url == "" {
		return Noop{}
	}
	if timeoutMs <= 0 {
		timeoutMs = 1500
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 16
	}
	return &HTTP{
		url: url,
		client: &http.Client{
			Timeout: time.Duration(timeoutMs) * time.Millisecond,
		},
		failClosed:    failMode == "closed",
		maxConcurrent: maxConcurrent,
	}
}

// Concurrency returns the configured fan-out cap.
func (v *HTTP) Concurrency() int { return v.maxConcurrent }

// Enabled returns true.
func (v *HTTP) Enabled() bool { return true }

// Validate POSTs a single Request to the configured URL.
func (v *HTTP) Validate(ctx context.Context, req Request) Decision {
	start := time.Now()
	defer func() {
		metrics.ValidationDuration.Observe(time.Since(start).Seconds())
	}()

	body, err := json.Marshal(req)
	if err != nil {
		// Marshalling our own Request must never fail in practice; treat
		// it as an error and fall through to the failure mode.
		log.Warnf("validation: marshal request: %s", err)
		return v.onError("marshal_error")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, v.url, bytes.NewReader(body))
	if err != nil {
		log.Warnf("validation: build request: %s", err)
		return v.onError("build_error")
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(httpReq)
	if err != nil {
		log.Debugf("validation: HTTP error: %s", err)
		return v.onError("http_error")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Debugf("validation: non-2xx status %d", resp.StatusCode)
		return v.onError(fmt.Sprintf("status_%d", resp.StatusCode))
	}

	var payload Response
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		log.Debugf("validation: decode response: %s", err)
		return v.onError("decode_error")
	}

	if payload.Success {
		metrics.ValidationTotal.WithLabelValues("allow").Inc()
		return Decision{Allow: true}
	}

	metrics.ValidationTotal.WithLabelValues("deny").Inc()
	return Decision{Allow: false, FailureMessage: payload.FailureMessage}
}

// onError returns a Decision based on fail_mode. The reason is recorded as a
// label on the metrics counter so operators can spot e.g. timeout vs 5xx vs
// decode failures.
func (v *HTTP) onError(reason string) Decision {
	metrics.ValidationTotal.WithLabelValues("error_" + reason).Inc()
	if v.failClosed {
		return Decision{Allow: false}
	}
	return Decision{Allow: true}
}

// Verify the interface is implemented.
var _ Validator = (*HTTP)(nil)
var _ Validator = Noop{}
