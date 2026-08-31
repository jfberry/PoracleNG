# API Delivery Destination — Core Path Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A working `api` delivery platform — `api:user` / `api:channel` destinations that POST a rendered DTS payload, wrapped in a versioned envelope, to an operator-configured HTTPS endpoint, with send/edit/delete, retry/backoff, and full delivery-pipeline integration.

**Architecture:** A new `delivery.APISender` implementing the existing `delivery.Sender` interface, registered in `NewDispatcher` when `[api_delivery]` is configured. `PlatformFromType` already yields `"api"` for `api:*` types, and DTS selection is already keyed on platform, so the platform slots into the existing pipeline; this plan adds the sender, an `api` concurrency lane in `FairQueue`, the DM/channel rate-limit classification, a shared target-class helper, a human-type allow-list, and the renderer plumbing (`api` default template resolution, ping suppression, `.toml` fallback packs, `TemplateID` on the delivery job).

**Tech Stack:** Go, `net/http`, `github.com/google/uuid` (already vendored), BurntSushi/toml, `jfberry/raymond`.

## Global Constraints

- Pre-commit gate (run from `processor/` before every commit): `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`
- No new dependencies. `message_id` uses `github.com/google/uuid` (already in `go.mod`).
- `message_id` is a UUIDv4 (lowercase hex + hyphens, colon-free). It identifies the logical message and is stable across retries of the same delivery.
- `MessageTracker.SentID` for `api` is `<destinationID>:<messageID>[:<providerID>]`. All three parts are colon-free (destination ID by the Task 2 charset check, message ID by the UUID alphabet, provider ID by the §1.4 response validation), so `splitSentID` (splits on the LAST colon) and `SplitN(sentID, ":", 3)` both work.
- Envelope `version` is `1`. Fields marked "omitted when empty" use `omitempty` and must not serialise as `null`/`""`.
- `api` destinations get NO Discord buttons and NO ping appended (a Discord mention string would corrupt JSON).
- **Deferred to the follow-up plan (do NOT build here):** `revision` monotonicity across edits (edits send `revision: 0` for now), and the `in_reply_to`, `media.static_map`, `tracking_uids`, `areas` envelope fields, and the full 15-entry Diadem partner pack. This plan ships a minimal starter `api` template so the path is testable end-to-end.
- Spec: `docs/superpowers/specs/2026-07-19-api-delivery-destination-design.md` (Part 1 = wire contract, Part 2 = implementation notes).

## File Structure

- Create: `processor/internal/delivery/api.go` — `APISender`, envelope types, envelope builder, HTTP send/retry.
- Create: `processor/internal/delivery/api_test.go` — sender tests against `httptest`.
- Create: `processor/internal/delivery/targetclass.go` — shared `TargetClass` helper (Task 6).
- Create: `fallbacks/dts/api.toml` — minimal starter partner template (Task 8).
- Modify: `processor/internal/config/config.go` — `APIDeliveryConfig` + defaults + validation.
- Modify: `processor/internal/store/human.go` — `ValidHumanTypes` + `ValidateHumanType`.
- Modify: `processor/internal/api/humans.go`, `processor/internal/api/v2_humans.go` — enforce type validation.
- Modify: `processor/internal/delivery/dispatcher.go`, `processor/internal/delivery/queue.go` — register sender, `api` lane.
- Modify: `processor/cmd/processor/main.go` — wire `DispatcherConfig` from config.
- Modify: `processor/internal/ratelimit/ratelimit.go` — `isUserType` adds `api:user`.
- Modify: `processor/cmd/processor/render.go`, `processor/internal/dts/renderer.go` — `TargetClass` callers, ping skip, `ResolveTemplate(platform)`, `TemplateID` plumbing.
- Modify: `processor/internal/dts/templates.go` — `.toml` fallback walker.

---

### Task 1: `[api_delivery]` config section

**Files:**
- Modify: `processor/internal/config/config.go`
- Test: `processor/internal/config/config_test.go` (or the existing config test file — grep for `func Test` in `config_test.go`; add there)

**Interfaces:**
- Produces: `config.APIDeliveryConfig` and `Config.APIDelivery`. Consumed by Tasks 5 (dispatcher wiring) and 7 (default template).

- [ ] **Step 1: Write the failing test**

Add to `processor/internal/config/config_test.go`:

```go
func TestAPIDeliveryDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.APIDelivery.Enabled = true
	cfg.APIDelivery.Endpoint = "https://example.test/hook"
	applyAPIDeliveryDefaults(cfg)
	if cfg.APIDelivery.SecretHeader != "X-Poracle-Secret" {
		t.Errorf("SecretHeader default = %q, want X-Poracle-Secret", cfg.APIDelivery.SecretHeader)
	}
	if cfg.APIDelivery.TimeoutMs != 10000 {
		t.Errorf("TimeoutMs default = %d, want 10000", cfg.APIDelivery.TimeoutMs)
	}
	if cfg.APIDelivery.MaxRetries != 3 {
		t.Errorf("MaxRetries default = %d, want 3", cfg.APIDelivery.MaxRetries)
	}
	if cfg.APIDelivery.Concurrency != 4 {
		t.Errorf("Concurrency default = %d, want 4", cfg.APIDelivery.Concurrency)
	}
	if cfg.APIDelivery.Template != "default" {
		t.Errorf("Template default = %q, want default", cfg.APIDelivery.Template)
	}
}

func TestAPIDeliveryValidation(t *testing.T) {
	cfg := &Config{}
	cfg.APIDelivery.Enabled = true
	cfg.APIDelivery.Endpoint = ""
	if err := validateAPIDelivery(cfg); err == nil {
		t.Fatal("expected error when enabled with empty endpoint, got nil")
	}
	cfg.APIDelivery.Endpoint = "https://example.test/hook"
	if err := validateAPIDelivery(cfg); err != nil {
		t.Fatalf("unexpected error with valid config: %v", err)
	}
	cfg.APIDelivery.Enabled = false
	cfg.APIDelivery.Endpoint = ""
	if err := validateAPIDelivery(cfg); err != nil {
		t.Fatalf("disabled+empty endpoint should be valid, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/config/ -run 'TestAPIDelivery' -v`
Expected: FAIL — `APIDelivery` undefined / `applyAPIDeliveryDefaults` undefined.

- [ ] **Step 3: Add the config struct and field**

In `processor/internal/config/config.go`, add the struct type near `SnapshotsConfig` (after its closing `}`, ~line 530):

```go
// APIDeliveryConfig configures the generic HTTP "api" delivery platform:
// api:user / api:channel destinations POST a rendered envelope to a single
// operator-configured endpoint. See docs/superpowers/specs/2026-07-19-api-delivery-destination-design.md.
type APIDeliveryConfig struct {
	Enabled      bool   `toml:"enabled"`
	Endpoint     string `toml:"endpoint"`
	Secret       string `toml:"secret"`
	SecretHeader string `toml:"secret_header"` // default "X-Poracle-Secret"
	SecretPrefix string `toml:"secret_prefix"` // e.g. "Bearer " when SecretHeader="Authorization"
	Template     string `toml:"template"`      // DTS template id for the partner pack; default "default"
	TimeoutMs    int    `toml:"timeout_ms"`    // default 10000
	MaxRetries   int    `toml:"max_retries"`   // default 3
	Concurrency  int    `toml:"concurrency"`   // default 4
	LogOnly      bool   `toml:"log_only"`      // dry-run: log the envelope instead of POSTing
}
```

Add the field to the `Config` struct, next to `Snapshots SnapshotsConfig` (~line 37):

```go
	APIDelivery    APIDeliveryConfig    `toml:"api_delivery"`
```

- [ ] **Step 4: Add defaults + validation helpers and call them from Load**

Add these two functions near the snapshots defaults logic (grep for `cfg.Snapshots.Path == ""` to find the defaults area, ~line 1028):

```go
// applyAPIDeliveryDefaults fills unset [api_delivery] fields.
func applyAPIDeliveryDefaults(cfg *Config) {
	if cfg.APIDelivery.SecretHeader == "" {
		cfg.APIDelivery.SecretHeader = "X-Poracle-Secret"
	}
	if cfg.APIDelivery.Template == "" {
		cfg.APIDelivery.Template = "default"
	}
	if cfg.APIDelivery.TimeoutMs <= 0 {
		cfg.APIDelivery.TimeoutMs = 10000
	}
	if cfg.APIDelivery.MaxRetries <= 0 {
		cfg.APIDelivery.MaxRetries = 3
	}
	if cfg.APIDelivery.Concurrency <= 0 {
		cfg.APIDelivery.Concurrency = 4
	}
}

// validateAPIDelivery rejects an enabled api_delivery block with no endpoint.
func validateAPIDelivery(cfg *Config) error {
	if cfg.APIDelivery.Enabled && cfg.APIDelivery.Endpoint == "" {
		return fmt.Errorf("[api_delivery] enabled = true but endpoint is empty")
	}
	return nil
}
```

(Note: `MaxRetries <= 0` defaults to 3. If an operator genuinely wants zero retries, that's a follow-up config nuance — the core plan treats "unset" and "0" identically, which is fine because 3 is the safe default.)

Call both from `Load` where the other defaults/validation run: add `applyAPIDeliveryDefaults(cfg)` alongside the snapshots defaults, and in the validation phase (grep for where `Load` returns a validation error, or add right before the successful `return cfg, nil`):

```go
	applyAPIDeliveryDefaults(cfg)
	if err := validateAPIDelivery(cfg); err != nil {
		return nil, err
	}
```

(If `Load` applies defaults and validation in separate passes, place each call in the matching pass. `fmt` is already imported in config.go.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd processor && go test ./internal/config/ -run 'TestAPIDelivery' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd processor && go build ./... && go vet ./... && golangci-lint run ./internal/config/
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add [api_delivery] section with defaults and validation"
```

---

### Task 2: Human-type allow-list validation

**Files:**
- Modify: `processor/internal/store/human.go` (add `ValidHumanTypes`, `ValidateHumanType`, `ValidAPIDestinationID`)
- Modify: `processor/internal/api/v2_humans.go` (enforce in v2 create)
- Modify: `processor/internal/api/humans.go` (enforce in v1 create)
- Test: `processor/internal/store/human_test.go` (grep for an existing store test file; if none, create `processor/internal/store/human_types_test.go`)

**Interfaces:**
- Produces: `store.ValidateHumanType(t string) error`, `store.ValidAPIDestinationID(id string) bool`, `store.ValidHumanTypes []string` (includes `api:user`, `api:channel`).

- [ ] **Step 1: Write the failing test**

Create `processor/internal/store/human_types_test.go`:

```go
package store

import "testing"

func TestValidateHumanType(t *testing.T) {
	valid := []string{"discord:user", "discord:channel", "discord:thread",
		"telegram:user", "telegram:group", "telegram:channel", "telegram:topic",
		"webhook", "api:user", "api:channel"}
	for _, ty := range valid {
		if err := ValidateHumanType(ty); err != nil {
			t.Errorf("ValidateHumanType(%q) = %v, want nil", ty, err)
		}
	}
	for _, ty := range []string{"api:users", "discord", "", "slack:user"} {
		if err := ValidateHumanType(ty); err == nil {
			t.Errorf("ValidateHumanType(%q) = nil, want error", ty)
		}
	}
}

func TestValidAPIDestinationID(t *testing.T) {
	for _, id := range []string{"u-42", "user.42", "abc_123~x"} {
		if !ValidAPIDestinationID(id) {
			t.Errorf("ValidAPIDestinationID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"has:colon", "has space", "", string(make([]byte, 129))} {
		if ValidAPIDestinationID(id) {
			t.Errorf("ValidAPIDestinationID(%q) = true, want false", id)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/store/ -run 'TestValidateHumanType|TestValidAPIDestinationID' -v`
Expected: FAIL — undefined `ValidateHumanType` / `ValidAPIDestinationID`.

- [ ] **Step 3: Add the validators**

In `processor/internal/store/human.go`, add near `NamedTargetTypes` (~line 22). Add `"regexp"` and `"fmt"` to the import block if not present (grep the import block first):

```go
// ValidHumanTypes is the canonical allow-list of human destination types.
// Human.type is a free-form varchar in the DB, but create endpoints validate
// against this set so a typo (e.g. "api:users") can't produce a human whose
// jobs are silently dropped at delivery for having no registered sender.
var ValidHumanTypes = []string{
	"discord:user", "discord:channel", "discord:thread",
	"telegram:user", "telegram:group", "telegram:channel", "telegram:topic",
	"webhook", "api:user", "api:channel",
}

var apiDestinationIDRe = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,128}$`)

// ValidateHumanType returns an error if t is not a known destination type.
func ValidateHumanType(t string) error {
	for _, v := range ValidHumanTypes {
		if t == v {
			return nil
		}
	}
	return fmt.Errorf("invalid human type %q", t)
}

// ValidAPIDestinationID reports whether id is a legal api:* destination ID.
// api SentIDs are composed as "<id>:<messageID>:<providerID>", so the ID must
// be colon-free (and bounded); the charset matches the envelope spec §1.3.
func ValidAPIDestinationID(id string) bool {
	return apiDestinationIDRe.MatchString(id)
}
```

- [ ] **Step 4: Run the store tests to verify they pass**

Run: `cd processor && go test ./internal/store/ -run 'TestValidateHumanType|TestValidAPIDestinationID' -v`
Expected: PASS.

- [ ] **Step 5: Enforce in v2 create**

In `processor/internal/api/v2_humans.go`, in `registerV2HumanCreate`'s handler, after the default-type assignment (`if human.Type == "" { human.Type = "discord:user" }`, ~line 120), add:

```go
		if err := store.ValidateHumanType(human.Type); err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if (human.Type == "api:user" || human.Type == "api:channel") && !store.ValidAPIDestinationID(human.ID) {
			return nil, huma.Error422UnprocessableEntity("api destination id must match ^[A-Za-z0-9._~-]{1,128}$ (colon-free)")
		}
```

- [ ] **Step 6: Enforce in v1 create**

In `processor/internal/api/humans.go`, in the create handler after `if human.Type == "" { human.Type = "discord:user" }` (~line 617), add:

```go
		if err := store.ValidateHumanType(human.Type); err != nil {
			trackingJSONError(c, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if (human.Type == "api:user" || human.Type == "api:channel") && !store.ValidAPIDestinationID(human.ID) {
			trackingJSONError(c, http.StatusUnprocessableEntity, "api destination id must match ^[A-Za-z0-9._~-]{1,128}$ (colon-free)")
			return
		}
```

(`store` and `http` are already imported in both files; confirm with the import block. `trackingJSONError` is the existing v1 error helper used elsewhere in humans.go.)

- [ ] **Step 7: Run the full gate**

Run: `cd processor && go build ./... && go vet ./... && go test -count=1 ./internal/store/ ./internal/api/ && golangci-lint run ./internal/store/ ./internal/api/`
Expected: PASS. (If the huma OpenAPI golden test in `internal/api` fails because the doc strings changed, that's expected — the golden regeneration is handled in the follow-up plan; if it fails here, note it as a concern and check whether these edits touched a documented schema. They don't add fields, so the golden should be unaffected.)

- [ ] **Step 8: Commit**

```bash
cd processor
git add internal/store/human.go internal/store/human_types_test.go internal/api/v2_humans.go internal/api/humans.go
git commit -m "feat(api): validate human type against allow-list; add api:user/api:channel"
```

---

### Task 3: `APISender` — envelope + Send

**Files:**
- Create: `processor/internal/delivery/api.go`
- Test: `processor/internal/delivery/api_test.go`

**Interfaces:**
- Consumes: `delivery.Job`, `delivery.Sender`, `delivery.SentMessage`, `db.IsClean`/`db.IsEdit`.
- Produces: `type APIConfig struct{...}`, `func NewAPISender(APIConfig) *APISender`, and `APISender` implementing `Send`/`Platform`/`WaitForRateLimit` (Edit/Delete added in Task 4). Consumed by Task 5.

- [ ] **Step 1: Write the failing test**

Create `processor/internal/delivery/api_test.go`:

```go
package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		Clean:    1, // clean bit
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
	if _, err := parseUUID(parts[1]); err != nil {
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
```

Note: `parseUUID` is a tiny test helper you will add in `api.go` as `func parseUUID(s string) (uuid.UUID, error) { return uuid.Parse(s) }` — no, simpler: the test can call `uuid.Parse` directly. Replace the `parseUUID(parts[1])` call with `uuid.Parse(parts[1])` and add `"github.com/google/uuid"` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/delivery/ -run 'TestAPISend' -v`
Expected: FAIL — `NewAPISender` / `APIConfig` / `APISender` undefined.

- [ ] **Step 3: Implement `api.go` (Send path)**

Create `processor/internal/delivery/api.go`:

```go
package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
```

Note: this file references `s.do(...)` and its `apiResponse` return type, which are implemented in Task 4. To keep Task 3 independently buildable, add a **minimal** `do` + `apiResponse` at the end of `api.go` now, and Task 4 replaces the body with the full retry/classification logic:

```go
// apiResponse is the outcome of a successful (2xx) POST.
type apiResponse struct {
	providerID string // the receiver's optional {"id":...}
}

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
```

Add the provider-ID regexp and the `"regexp"` import at the top of `api.go`:

```go
var providerIDRe = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,128}$`)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd processor && go test ./internal/delivery/ -run 'TestAPISend' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd processor && go build ./... && go vet ./... && golangci-lint run ./internal/delivery/
git add internal/delivery/api.go internal/delivery/api_test.go
git commit -m "feat(delivery): APISender send path + envelope"
```

---

### Task 4: `APISender` — Edit, Delete, retry & status classification

**Files:**
- Modify: `processor/internal/delivery/api.go` (add `Edit`, `Delete`, replace `do`)
- Test: `processor/internal/delivery/api_test.go`

**Interfaces:**
- Produces: `APISender.Edit`, `APISender.Delete`, completing the `delivery.Sender` interface. Full status classification in `do`.

- [ ] **Step 1: Write the failing tests**

Add to `processor/internal/delivery/api_test.go`:

```go
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
	if err == nil || !errorsAs(err, &perm) {
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
```

Add a tiny `errorsAs` helper + `"time"` import to the test file, or use `errors.As` directly with `"errors"` imported. Prefer `errors.As`: import `"errors"` and replace `errorsAs(err, &perm)` with `errors.As(err, &perm)`.

For the 5xx retry test to not sleep for real, the retry backoff in `do` must use a zero/short base when a test hook is set. Implement backoff as `time.Duration(attempt) * s.retryBase()` where `retryBase()` returns `s.retryBaseDur` if set (tests set it to 0) else a default (e.g. 500ms). Add field `retryBaseDur time.Duration` to `APISender` and in `newAPISenderForTest` set `s.retryBaseDur = 0`. Document this in the test setup.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/delivery/ -run 'TestAPIDelete|TestAPISend404|TestAPISend401|TestAPISend5xx' -v`
Expected: FAIL — `Delete` undefined, and `do` doesn't classify statuses.

- [ ] **Step 3: Add Edit/Delete and the full `do`**

In `processor/internal/delivery/api.go`, add the `retryBaseDur` field to the struct and a `retryBase()` accessor:

```go
	retryBaseDur time.Duration // 0 in tests; default 500ms
```

```go
func (s *APISender) retryBase() time.Duration {
	if s.retryBaseDur != 0 {
		return s.retryBaseDur
	}
	return 500 * time.Millisecond
}
```

Add `splitAPISentID`, `Edit`, `Delete`:

```go
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
```

Add `"strings"`, `"strconv"`, `"errors"` to the imports as needed. Replace the Task 3 stub `do` with the full version:

```go
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

		resp, derr := s.client.Do(req)
		if derr != nil {
			lastErr = derr
			continue // transient: retry
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		retryAfter := resp.Header.Get("Retry-After")
		status := resp.StatusCode
		resp.Body.Close()

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
```

Remove the Task 3 stub `do` and its now-duplicate helpers, keeping one copy of `parseProviderID`. Verify `errors` is imported only if used; it is used by the tests, not `api.go` — do not add unused imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd processor && go test ./internal/delivery/ -run 'TestAPI' -v`
Expected: PASS (all api tests).

- [ ] **Step 5: Commit**

```bash
cd processor && go build ./... && go vet ./... && golangci-lint run ./internal/delivery/
git add internal/delivery/api.go internal/delivery/api_test.go
git commit -m "feat(delivery): APISender edit/delete + retry and status classification"
```

---

### Task 5: Dispatcher + FairQueue `api` lane

**Files:**
- Modify: `processor/internal/delivery/dispatcher.go` (register APISender; new `DispatcherConfig` fields)
- Modify: `processor/internal/delivery/queue.go` (`apiSem`, `apiInFlight`, `ConcurrentAPI`, `APIDepth`, `semaphoreFor`/`counterFor` cases)
- Modify: `processor/cmd/processor/main.go` (populate `DispatcherConfig` from `cfg.APIDelivery`)
- Test: `processor/internal/delivery/queue_test.go` (grep for existing queue tests to mirror setup)

**Interfaces:**
- Consumes: `NewAPISender` (Task 3/4).
- Produces: `DispatcherConfig.API*` fields, `QueueConfig.ConcurrentAPI`, `FairQueue.APIDepth()`.

- [ ] **Step 1: Add DispatcherConfig fields and register the sender**

In `processor/internal/delivery/dispatcher.go`, add to `DispatcherConfig`:

```go
	APIEndpoint     string
	APISecret       string
	APISecretHeader string
	APISecretPrefix string
	APITimeoutMs    int
	APIMaxRetries   int
	APIConcurrency  int
	APILogOnly      bool
	Version         string // PoracleNG version for API User-Agent
```

In `NewDispatcher`, after the telegram registration block (`if cfg.TelegramToken != "" { ... }`):

```go
	if cfg.APIEndpoint != "" {
		senders["api"] = NewAPISender(APIConfig{
			Endpoint:     cfg.APIEndpoint,
			Secret:       cfg.APISecret,
			SecretHeader: cfg.APISecretHeader,
			SecretPrefix: cfg.APISecretPrefix,
			TimeoutMs:    cfg.APITimeoutMs,
			MaxRetries:   cfg.APIMaxRetries,
			LogOnly:      cfg.APILogOnly,
			Version:      cfg.Version,
		})
	}
```

Pass `cfg.Queue.ConcurrentAPI` through — it's already inside `cfg.Queue` (next step), so `NewFairQueue` receives it via the existing `cfg.Queue` argument. No change needed here beyond the sender registration.

- [ ] **Step 2: Add the `api` concurrency lane in the queue**

In `processor/internal/delivery/queue.go`:

Add to `QueueConfig` (next to `ConcurrentTelegram int`):

```go
	ConcurrentAPI int
```

Add to the `FairQueue` struct (next to `telegramSem chan struct{}`):

```go
	apiSem chan struct{}
```

and (next to `telegramInFlight atomic.Int64`):

```go
	apiInFlight atomic.Int64
```

In `NewFairQueue`, default and construct it (mirror the telegram lines):

```go
	if cfg.ConcurrentAPI <= 0 {
		cfg.ConcurrentAPI = 1
	}
```

and in the returned `&FairQueue{...}` literal:

```go
		apiSem: make(chan struct{}, cfg.ConcurrentAPI),
```

Extend `semaphoreFor` and `counterFor` with an explicit `api` case (before the `default`):

```go
	case "api":
		return fq.apiSem
```

```go
	case "api":
		return &fq.apiInFlight
```

Add the depth accessor next to `TelegramDepth`:

```go
// APIDepth returns the number of api jobs currently in-flight.
func (fq *FairQueue) APIDepth() int { return int(fq.apiInFlight.Load()) }
```

Add a `Dispatcher.APIDepth()` passthrough in `dispatcher.go` next to `TelegramDepth`:

```go
// APIDepth returns the number of api jobs currently in-flight.
func (d *Dispatcher) APIDepth() int { return d.queue.APIDepth() }
```

- [ ] **Step 3: Wire it in main.go**

In `processor/cmd/processor/main.go`, change the dispatcher-init guard and config (~line 191). Replace `if discordToken != "" || telegramToken != "" {` with:

```go
	if discordToken != "" || telegramToken != "" || cfg.APIDelivery.Enabled {
```

and add these fields to the `delivery.DispatcherConfig{...}` literal:

```go
			APIEndpoint:     apiEndpoint(cfg),
			APISecret:       cfg.APIDelivery.Secret,
			APISecretHeader: cfg.APIDelivery.SecretHeader,
			APISecretPrefix: cfg.APIDelivery.SecretPrefix,
			APITimeoutMs:    cfg.APIDelivery.TimeoutMs,
			APIMaxRetries:   cfg.APIDelivery.MaxRetries,
			APILogOnly:      cfg.APIDelivery.LogOnly,
			Version:         version,
```

and add `ConcurrentAPI: cfg.APIDelivery.Concurrency,` inside the nested `delivery.QueueConfig{...}`.

Add a small helper near the top-level funcs in `main.go` so a disabled block never registers a sender even if an endpoint is present:

```go
// apiEndpoint returns the configured api endpoint only when api delivery is
// enabled, so a stale endpoint with enabled=false does not register a sender.
func apiEndpoint(cfg *config.Config) string {
	if cfg.APIDelivery.Enabled {
		return cfg.APIDelivery.Endpoint
	}
	return ""
}
```

(`version` is the existing build version variable used for `PoracleNG/<version>` and the health endpoint — grep `main.go` for `version` to confirm the identifier; if it's a package-level `var version`, use it directly.)

- [ ] **Step 4: Add a queue test proving the api lane is independent**

Add to `processor/internal/delivery/queue_test.go` (mirror an existing test's fake-sender setup; if the file defines a `fakeSender`, reuse it — otherwise add a minimal one):

```go
func TestFairQueueAPILaneIndependent(t *testing.T) {
	fq := &FairQueue{
		discordSem:  make(chan struct{}, 1),
		webhookSem:  make(chan struct{}, 1),
		telegramSem: make(chan struct{}, 1),
		apiSem:      make(chan struct{}, 3),
	}
	if got := fq.semaphoreFor("api:user"); got != fq.apiSem {
		t.Error("api:user should map to apiSem, not discordSem")
	}
	if got := fq.semaphoreFor("api:channel"); got != fq.apiSem {
		t.Error("api:channel should map to apiSem")
	}
	if got := fq.counterFor("api:user"); got != &fq.apiInFlight {
		t.Error("api:user should map to apiInFlight")
	}
	if got := fq.semaphoreFor("discord:user"); got != fq.discordSem {
		t.Error("discord:user must still map to discordSem")
	}
}
```

- [ ] **Step 5: Run tests and the gate**

Run: `cd processor && go test ./internal/delivery/ -run 'TestFairQueueAPILane|TestAPI' -v && go build ./... && go vet ./... && golangci-lint run ./internal/delivery/ ./cmd/processor/`
Expected: PASS, build clean.

- [ ] **Step 6: Commit**

```bash
cd processor
git add internal/delivery/dispatcher.go internal/delivery/queue.go internal/delivery/queue_test.go cmd/processor/main.go
git commit -m "feat(delivery): register APISender and add api concurrency lane"
```

---

### Task 6: Rate-limit classification + shared `TargetClass`

**Files:**
- Modify: `processor/internal/ratelimit/ratelimit.go` (`isUserType`)
- Create: `processor/internal/delivery/targetclass.go` (`TargetClass`)
- Modify: `processor/cmd/processor/render.go` (`snapshotTargetType` → `delivery.TargetClass`)
- Modify: `processor/internal/dts/renderer.go` (`deliveryTargetType` → `delivery.TargetClass`)
- Test: `processor/internal/ratelimit/ratelimit_test.go`, `processor/internal/delivery/targetclass_test.go`

**Interfaces:**
- Produces: `delivery.TargetClass(jobType string) string` returning `"dm"|"channel"|"webhook"|""`.

- [ ] **Step 1: Write the failing tests**

Add to `processor/internal/ratelimit/ratelimit_test.go`:

```go
func TestIsUserTypeAPI(t *testing.T) {
	if !isUserType("api:user") {
		t.Error("api:user should use the DM limit")
	}
	if isUserType("api:channel") {
		t.Error("api:channel should use the channel limit")
	}
}
```

Create `processor/internal/delivery/targetclass_test.go`:

```go
package delivery

import "testing"

func TestTargetClass(t *testing.T) {
	cases := map[string]string{
		"discord:user":    "dm",
		"telegram:user":   "dm",
		"api:user":        "dm",
		"discord:channel": "channel",
		"discord:thread":  "channel",
		"telegram:group":  "channel",
		"telegram:channel": "channel",
		"telegram:topic":  "channel",
		"api:channel":     "channel",
		"webhook":         "webhook",
		"bogus":           "",
	}
	for in, want := range cases {
		if got := TargetClass(in); got != want {
			t.Errorf("TargetClass(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/ratelimit/ -run TestIsUserTypeAPI ./internal/delivery/ -run TestTargetClass -v`
Expected: FAIL — `api:user` not a user type; `TargetClass` undefined.

- [ ] **Step 3: Add `api:user` to `isUserType`**

In `processor/internal/ratelimit/ratelimit.go`, change `isUserType`:

```go
func isUserType(t string) bool {
	return t == "discord:user" || t == "telegram:user" || t == "api:user"
}
```

Update the doc comment above it to list `api:channel` among the channel-limit types.

- [ ] **Step 4: Add the shared `TargetClass` and switch both callers to it**

Create `processor/internal/delivery/targetclass.go`:

```go
package delivery

// TargetClass maps a delivery Job.Type to the coarse destination class used by
// snapshots (Snapshot.TargetType) and button applies_to checks: "dm",
// "channel", "webhook", or "" for an unknown type. This is the single source
// of truth previously duplicated as snapshotTargetType (cmd/processor) and
// deliveryTargetType (internal/dts); both now delegate here. telegram:topic is
// included as a channel class (it was missing from both duplicates).
func TargetClass(jobType string) string {
	switch jobType {
	case "discord:user", "telegram:user", "api:user":
		return "dm"
	case "discord:channel", "discord:thread", "telegram:group", "telegram:channel", "telegram:topic", "api:channel":
		return "channel"
	case "webhook":
		return "webhook"
	default:
		return ""
	}
}
```

In `processor/cmd/processor/render.go`, replace the `snapshotTargetType` function body with a delegation (keep the name for its callers, or replace call sites — delegation is the smaller diff):

```go
func snapshotTargetType(jobType string) string {
	return delivery.TargetClass(jobType)
}
```

(`delivery` is already imported in render.go.)

In `processor/internal/dts/renderer.go`, `deliveryTargetType` (~line 588) similarly delegates:

```go
func deliveryTargetType(userType string) string {
	return delivery.TargetClass(userType)
}
```

(`delivery` is already imported in renderer.go — it uses `delivery.PlatformFromType`.)

- [ ] **Step 5: Run tests + gate**

Run: `cd processor && go test -count=1 ./internal/ratelimit/ ./internal/delivery/ ./internal/dts/ ./cmd/processor/ && go build ./... && go vet ./... && golangci-lint run ./internal/ratelimit/ ./internal/delivery/ ./internal/dts/ ./cmd/processor/`
Expected: PASS. Existing snapshot/button tests that exercised `snapshotTargetType`/`deliveryTargetType` still pass (behaviour identical except `telegram:topic` now returns `"channel"` instead of `""` — a latent fix; if any test asserted the old `""` for topic, update it and note it).

- [ ] **Step 6: Commit**

```bash
cd processor
git add internal/ratelimit/ratelimit.go internal/delivery/targetclass.go internal/delivery/targetclass_test.go internal/ratelimit/ratelimit_test.go cmd/processor/render.go internal/dts/renderer.go
git commit -m "feat: api:user DM rate limit; unify TargetClass (fixes telegram:topic)"
```

---

### Task 7: Renderer plumbing — ping skip, `ResolveTemplate(platform)`, `TemplateID`, `.toml` fallback packs

**Files:**
- Modify: `processor/internal/dts/renderer.go` (ping skip for api; `resolveTemplate`/`ResolveTemplate` platform-aware)
- Modify: `processor/internal/dts/templates.go` (fallback walker accepts `.toml`)
- Modify: `processor/cmd/processor/render.go` (populate `delivery.Job.TemplateID`)
- Modify: `processor/internal/delivery/delivery.go` (add `Job.TemplateID`)
- Test: `processor/internal/dts/renderer_test.go`, `processor/internal/dts/templates_test.go`

**Interfaces:**
- Consumes: `config.APIDeliveryConfig.Template` (via renderer construction — see Step 4).
- Produces: `delivery.Job.TemplateID`; `Renderer.resolveTemplate(platform, trackingTemplate)`.

- [ ] **Step 1: Write the failing tests**

Add to `processor/internal/dts/renderer_test.go`:

```go
func TestRenderAlertAPINoPing(t *testing.T) {
	entries := []DTSEntry{
		{Type: "raid", ID: "default", Platform: "api",
			Template: map[string]any{"gym": "{{gymName}}"}},
	}
	r := newTestRenderer(t, entries)
	enrichment := map[string]any{"gymName": "Gym A", "latitude": 1.0, "longitude": 2.0, "tth": map[string]any{"totalSeconds": 600}}
	users := []webhook.MatchedUser{
		{ID: "u-1", Type: "api:user", Template: "default", Language: "en", Ping: "<@123>"},
	}
	jobs := r.RenderAlert("raid", enrichment, nil, nil, users, nil, "ref", "")
	if len(jobs) != 1 {
		t.Fatalf("want 1 job, got %d", len(jobs))
	}
	// Ping must NOT be appended for api destinations — the message stays valid JSON with no mention.
	msg := parseMessage(t, jobs[0].Message)
	if _, hasContent := msg["content"]; hasContent {
		// appendPingToRaw writes into "content"; api output must be untouched.
		t.Errorf("api job should not have ping-injected content: %v", msg)
	}
}
```

Add to `processor/internal/dts/templates_test.go`:

```go
func TestFallbackTomlPackLoads(t *testing.T) {
	configDir := t.TempDir()
	fallbackDir := t.TempDir()
	// minimal dts.json so LoadTemplates has a source
	writeTestDTS(t, configDir, []DTSEntry{{Type: "raid", ID: "default", Platform: "discord", Template: map[string]any{"c": "x"}}})
	// a .toml pack in fallbacks/dts/
	if err := os.MkdirAll(filepath.Join(fallbackDir, "dts"), 0o755); err != nil {
		t.Fatal(err)
	}
	toml := "[[entry]]\ntype = \"raid\"\nplatform = \"api\"\nid = \"default\"\ntemplate = \"\"\"{\"gym\":\"{{gymName}}\"}\"\"\"\n"
	if err := os.WriteFile(filepath.Join(fallbackDir, "dts", "api.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	ts, err := LoadTemplates(configDir, fallbackDir)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl := ts.Get("raid", "api", "default", "en"); tmpl == nil {
		t.Error("expected the fallback api.toml raid/api/default template to load")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/dts/ -run 'TestRenderAlertAPINoPing|TestFallbackTomlPackLoads' -v`
Expected: FAIL — ping still appended for api; `.toml` fallback not loaded.

- [ ] **Step 3: Skip ping for api in `renderPerUser` and `renderGrouped`**

In `processor/internal/dts/renderer.go`, both `renderPerUser` (~line 435) and `renderGrouped` (~line 648) append the ping. Guard both with a platform check. In `renderPerUser`, `platform` is already in scope:

```go
		if user.Ping != "" && platform != "api" {
			rawMessage = appendPingToRaw(rawMessage, user.Ping)
		}
```

In `renderGrouped`, the ping is appended inside the per-user loop where `key.platform` is available:

```go
			if user.Ping != "" && key.platform != "api" {
				userMessage = appendPingToRaw(rawMessage, user.Ping)
			}
```

(Match the existing variable names in each function — `rawMessage`/`userMessage`. Only add the `&& … != "api"` clause.)

- [ ] **Step 4: Make template resolution platform-aware**

In `processor/internal/dts/renderer.go`, the `Renderer` needs the api default template name. Add a field and a setter, and thread it from construction. Add to the `Renderer` struct:

```go
	apiDefaultTemplate string // config [api_delivery] template; used to resolve empty template for api destinations
```

Add a setter (near `SetButtonsEnabled`):

```go
// SetAPIDefaultTemplate records the [api_delivery] template id used to resolve
// an empty tracking-rule template for api destinations.
func (r *Renderer) SetAPIDefaultTemplate(id string) { r.apiDefaultTemplate = id }
```

Change `resolveTemplate` to take a platform and prefer the api default for api destinations:

```go
func (r *Renderer) resolveTemplate(platform, trackingTemplate string) string {
	if trackingTemplate != "" {
		return trackingTemplate
	}
	if platform == "api" && r.apiDefaultTemplate != "" {
		return r.apiDefaultTemplate
	}
	return r.defaultTemplate
}
```

Update all callers of `resolveTemplate`/`ResolveTemplate` to pass the platform:
- `renderPerUser`: `templateID := r.resolveTemplate(platform, user.Template)`
- `renderGrouped` group-key loop: `templateID: r.resolveTemplate(platform, user.Template)` (platform is computed just above as `delivery.PlatformFromType(user.Type)`)
- the split loop added by the distance/bearing work (`r.resolveTemplate(user.Template)` → `r.resolveTemplate(platform, user.Template)`)
- `CheckTemplate`: `resolvedID := r.resolveTemplate(platform, templateID)`
- The exported `ResolveTemplate` wrapper: change its signature to `ResolveTemplate(platform, trackingTemplate string) string` and update its callers in `cmd/processor/tilemode.go` (`ps.dtsRenderer.ResolveTemplate("")` → `ps.dtsRenderer.ResolveTemplate(platform, "")` — `platform` is already computed there as `delivery.PlatformFromType(u.Type)`).

Wire the default from config where the renderer is constructed (grep `cmd/processor` for `NewRenderer(` or `dtsRenderer =`): after construction call `proc.dtsRenderer.SetAPIDefaultTemplate(cfg.APIDelivery.Template)`.

- [ ] **Step 5: Add `Job.TemplateID` and populate it**

In `processor/internal/delivery/delivery.go`, add to the `Job` struct (near `Name`):

```go
	TemplateID string `json:"templateId"` // resolved DTS template id (for api envelope template_id)
```

In `processor/cmd/processor/render.go`, in the `delivery.Job{...}` literal, add:

```go
			TemplateID:    j.TemplateSelected,
```

(`webhook.DeliveryJob` already carries `TemplateSelected` — confirmed in renderer.go job construction.)

- [ ] **Step 6: Extend the fallback walker to `.toml`**

In `processor/internal/dts/templates.go`, the `fallbacks/dts/` walker (~line 193) currently matches `.json` only. Replace the walk callback so it handles `.toml` via the existing TOML loader (grep for how `config/dts/*.toml` is loaded — a `loadTOMLEntries`/`parseTOML` helper exists; reuse it). Change the guard from:

```go
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
```

to handle both, and for `.toml` files call the same loader used for `config/dts/*.toml`, marking entries `Readonly = true` and `sourceFormat = SourceFormatTOML`. Concretely:

```go
		if err != nil || d.IsDir() {
			return nil
		}
		switch {
		case strings.HasSuffix(path, ".json"):
			// (existing JSON handling, unchanged)
		case strings.HasSuffix(path, ".toml"):
			tomlEntries, terr := loadTOMLEntriesFromFile(path) // reuse the config/dts TOML loader
			if terr != nil {
				log.Warnf("dts: failed to parse %s: %s", path, terr)
				return nil
			}
			for i := range tomlEntries {
				tomlEntries[i].sourceFile = path
				tomlEntries[i].sourceFormat = SourceFormatTOML
				tomlEntries[i].Readonly = true
			}
			entries = append(entries, tomlEntries...)
		default:
			return nil
		}
		return nil
```

Grep `internal/dts/toml_loader.go` for the actual exported/unexported loader name (e.g. `loadTOMLFile`, `parseTOMLEntries`) and use it verbatim; do not invent `loadTOMLEntriesFromFile` if a differently-named helper exists.

- [ ] **Step 7: Run tests + full gate**

Run: `cd processor && go test -count=1 ./... && go build ./... && go vet ./... && golangci-lint run ./...`
Expected: PASS. The `.toml` fallback and no-ping tests are green; existing renderer/template tests unaffected.

- [ ] **Step 8: Commit**

```bash
cd processor
git add internal/dts/renderer.go internal/dts/templates.go internal/delivery/delivery.go cmd/processor/render.go cmd/processor/tilemode.go cmd/processor/main.go
git commit -m "feat(dts): api ping-skip, platform-aware template resolution, TemplateID plumbing, toml fallback packs"
```

---

### Task 8: Minimal starter `api` template + end-to-end log_only test

**Files:**
- Create: `fallbacks/dts/api.toml` (2 alert types, minimal)
- Test: `processor/cmd/processor/api_delivery_e2e_test.go` (or extend an existing processor-level test)

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Ship a minimal starter pack**

Create `fallbacks/dts/api.toml` with two representative types so the path is exercisable (the full 15-entry Diadem pack is the follow-up plan). Numeric fields are guarded per the spec's authoring rule:

```toml
# Minimal starter api-platform templates. The full partner pack (all alert
# types, one self-contained file per partner) lands in the follow-up plan.

[[entry]]
type = "raid"
platform = "api"
id = "default"
template = """
{
  "level": {{#if level}}{{level}}{{else}}0{{/if}},
  "boss": "{{name}}",
  "gym_name": "{{gymName}}",
  "types": [{{#each typeNameEng}}"{{this}}"{{#unless @last}},{{/unless}}{{/each}}],
  "end_at": {{#if endTimestamp}}{{endTimestamp}}{{else}}0{{/if}}
}
"""

[[entry]]
type = "pokemon"
platform = "api"
id = "default"
template = """
{
  "pokemon_id": {{pokemonId}},
  "name": "{{name}}",
  "iv": {{#if iv}}{{iv}}{{else}}null{{/if}},
  "cp": {{#if cp}}{{cp}}{{else}}null{{/if}},
  "level": {{#if level}}{{level}}{{else}}null{{/if}},
  "distance_m": {{#if distance}}{{distance}}{{else}}0{{/if}},
  "despawn_at": {{#if despawnTimestamp}}{{despawnTimestamp}}{{else}}0{{/if}}
}
"""
```

- [ ] **Step 2: Write an end-to-end log_only test**

This test proves an `api:user` human's alert renders through the real renderer and the `APISender` produces a valid envelope. Because a full `ProcessorService` is heavy, test at the sender+renderer seam: render a raid via a renderer loaded with the starter pack, then feed the resulting `delivery.Job` to an `APISender` pointed at an `httptest` server, and assert the envelope's `payload` is the rendered raid JSON and `alert_type`/`destination` are correct.

Create `processor/cmd/processor/api_delivery_e2e_test.go` — but if constructing a `Renderer` from `cmd/processor` is awkward, place this test in `internal/delivery` using a hand-built `Job` whose `Message` is a rendered raid body, OR in `internal/dts` importing `delivery`. Choose the package with the least setup; the assertion is what matters:

```go
// Pseudocode shape — implement in whichever package builds a Renderer most
// cheaply (mirror newTestRenderer). The point: rendered api payload flows
// through APISender into a valid envelope.
//
// 1. renderer with fallbacks/dts/api.toml loaded (or an inline api raid entry)
// 2. jobs := r.RenderAlert("raid", enrichment, nil, nil, []MatchedUser{{ID:"u-1",Type:"api:user",...}}, ...)
// 3. httptest server capturing the POST body
// 4. APISender.Send(ctx, toDeliveryJob(jobs[0]))
// 5. assert captured envelope.payload unmarshals to {"level":5,"boss":...}
//    and envelope.alert_type=="raid", destination.type=="api:user"
```

Implement it concretely in `internal/dts/api_render_test.go` (that package already has `newTestRenderer` and imports `delivery`), converting the `webhook.DeliveryJob` to a `delivery.Job` inline (copy `Target`, `Type`, `Name`, `Language`, `Message`, and set `MsgType: "raid"`, `TemplateID: jobs[0].TemplateSelected`).

```go
func TestAPIEndToEndEnvelope(t *testing.T) {
	entries := []DTSEntry{
		{Type: "raid", ID: "default", Platform: "api",
			Template: map[string]any{"level": "{{level}}", "boss": "{{name}}"}},
	}
	r := newTestRenderer(t, entries)
	enrichment := map[string]any{"level": 5, "name": "Mewtwo", "latitude": 1.0, "longitude": 2.0, "tth": map[string]any{"totalSeconds": 600}}
	users := []webhook.MatchedUser{{ID: "u-1", Type: "api:user", Template: "default", Language: "en", Name: "James"}}
	jobs := r.RenderAlert("raid", enrichment, nil, nil, users, nil, "ref", "")
	if len(jobs) != 1 {
		t.Fatalf("want 1 job, got %d", len(jobs))
	}

	var payload map[string]any
	var alertType, destType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var env map[string]any
		b, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(b, &env)
		alertType, _ = env["alert_type"].(string)
		if d, ok := env["destination"].(map[string]any); ok {
			destType, _ = d["type"].(string)
		}
		if p, ok := env["payload"].(map[string]any); ok {
			payload = p
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := delivery.NewAPISender(delivery.APIConfig{Endpoint: srv.URL, Secret: "x", TimeoutMs: 2000})
	dj := &delivery.Job{
		Target: jobs[0].Target, Type: jobs[0].Type, Name: jobs[0].Name,
		Language: jobs[0].Language, Message: jobs[0].Message,
		MsgType: "raid", TemplateID: jobs[0].TemplateSelected,
	}
	if _, err := s.Send(context.Background(), dj); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	if alertType != "raid" || destType != "api:user" {
		t.Errorf("envelope alert_type=%q destType=%q", alertType, destType)
	}
	if payload["boss"] != "Mewtwo" {
		t.Errorf("payload.boss = %v, want Mewtwo", payload["boss"])
	}
}
```

(Add imports: `context`, `encoding/json`, `io`, `net/http`, `net/http/httptest`, and the `delivery` + `webhook` packages already used by the test file.)

- [ ] **Step 3: Run tests + full gate**

Run: `cd processor && go test -count=1 ./... && go build ./... && go vet ./... && golangci-lint run ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd processor
git add ../fallbacks/dts/api.toml internal/dts/api_render_test.go
git commit -m "feat(dts): minimal starter api template pack + end-to-end envelope test"
```

---

### Task 9: Ship & reference the receiver specification

The standalone receiver contract (`docs/api-delivery-receiver-spec.md`) is the artifact handed to third-party implementers (Diadem first). It already exists; this task makes it discoverable and keeps a single source of truth.

**Files:**
- Modify: `README.md` and/or `API.md` (add a link under delivery/integration docs — grep for where Discord/Telegram delivery or the v2 API is described)
- Modify: `docs/superpowers/specs/2026-07-19-api-delivery-destination-design.md` (Part 1 header points to the standalone doc as canonical for receivers)

- [ ] **Step 1: Link the receiver spec from the top-level docs**

Grep `README.md` and `API.md` for an existing delivery/integration section (e.g. where webhooks, Discord, or the v2 API are listed). Add a one-line link:

```markdown
- **API delivery (third-party receivers):** see [docs/api-delivery-receiver-spec.md](docs/api-delivery-receiver-spec.md) — the behavioural contract for an HTTP endpoint that receives Poracle alerts.
```

- [ ] **Step 2: Point Part 1 of the design doc at the standalone spec**

At the top of `# Part 1 — Receiver Specification` in the design doc, add:

```markdown
> **Canonical hand-off document:** the self-contained receiver contract is [docs/api-delivery-receiver-spec.md](../../api-delivery-receiver-spec.md). Part 1 below is retained as design context; the standalone doc is what a partner implements against. Keep the two in sync — the envelope and response tables must match.
```

- [ ] **Step 3: Commit**

```bash
git add README.md API.md docs/superpowers/specs/2026-07-19-api-delivery-destination-design.md
git commit -m "docs: publish and link the api-delivery receiver specification"
```

(Omit `API.md` from the `git add` if it has no natural section; README is sufficient.)

---

## Full-suite verification

After Task 8, from `processor/`:

```bash
go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...
```

Manual smoke (optional, requires a config): set `[api_delivery] enabled=true`, `endpoint="http://localhost:9999"`, `log_only=true`; create an `api:user` via `POST /api/v2/humans`; add a raid tracking rule; fire `!poracle-test raid,<id>` or replay a raid webhook; confirm a well-formed `send` envelope is logged.

## Self-Review notes

- **Spec coverage (Part 2):** config §2.1 → Task 1; APISender §2.2 → Tasks 3–4; enumeration points §2.3 → queue lane (Task 5), isUserType (Task 6), TargetClass unification (Task 6), ping-skip + ResolveTemplate + toml walker (Task 7); human-type validation §2.5 → Task 2; SentID composition §2.6 → Tasks 3–4 (`<dest>:<messageID>:<providerID>`); rendering §2.7 → Tasks 7–8.
- **Deferred (documented, follow-up plan):** revision monotonicity, `in_reply_to`/`media`/`tracking_uids`/`areas` envelope fields, full Diadem 15-type pack, config-editor `api_delivery_` nesting, `templates.go` metadata/log `"api"` counting, `userlist`/`broadcast` `api` handling, OpenAPI golden regeneration. None block a working core path; each is additive.
- **Deviations from spec to flag at review:** `message_id` is UUIDv4, not ULID (spec updated to match; the change is committed with the plan). Edits currently send `revision: 0`.
- **Type consistency:** `APIConfig` (Task 3) fields match `DispatcherConfig.API*` mapping (Task 5). `TargetClass` (Task 6) return values match the old `snapshotTargetType`/`deliveryTargetType` domains plus `api:*`. `Job.TemplateID` (Task 7) is read by `buildSendEnvelope` (Task 3).
