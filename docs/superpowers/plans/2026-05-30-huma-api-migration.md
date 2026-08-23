# Huma API Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the `/api/tracking/*`, `/api/humans/*`, and `/api/profiles/*` endpoint groups from hand-written Gin handlers to the huma framework, gaining a generated OpenAPI 3.1 spec + public docs UI, while preserving the legacy JSON wire envelope and lenient tolerance of broken clients.

**Architecture:** huma is mounted on the *existing* `*gin.Engine` via the `humagin` adapter under the already-authenticated `/api` group, so existing middleware is untouched. Migrated groups move from Gin route registrations to `huma.Register` calls; everything else stays on Gin. `flexBool`/`flexInt` gain `SchemaProvider` methods so huma documents a canonical type per field while still accepting legacy forms; request bodies allow additional properties so unknown fields don't 422. The legacy `huma.NewError` is overridden to emit `{status:"error",message}`.

**Tech Stack:** Go 1.26, gin-gonic, `github.com/danielgtaylor/huma/v2` (v2.38.0) + `humagin` adapter, sqlx/MySQL, logrus, testify-free table tests via `net/http/httptest`.

**Spec:** `docs/superpowers/specs/2026-05-30-huma-api-migration-design.md`

**Conventions for every task:** the four-check gate must pass before each commit — `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...` (run from `processor/`). All paths below are relative to the repo root unless prefixed `processor/`.

---

## Phase 0 — Foundation (de-risks the whole approach)

### Task 1: Add the huma dependency

**Files:**
- Modify: `processor/go.mod`, `processor/go.sum`

- [ ] **Step 1: Add the modules**

Run from `processor/`:
```bash
go get github.com/danielgtaylor/huma/v2@v2.38.0
go mod tidy
```

- [ ] **Step 2: Verify it resolves and the tree still builds**

Run: `go build ./...`
Expected: exit 0, and `grep huma go.mod` shows `github.com/danielgtaylor/huma/v2 v2.38.0`.

- [ ] **Step 3: Commit**

```bash
git add processor/go.mod processor/go.sum
git commit -m "build: add huma v2 dependency"
```

### Task 2: Legacy error model + huma config helper

Override the package-global `huma.NewError` so every huma-generated error serialises as `{"status":"error","message":"..."}` (not RFC 9457), and provide a single constructor for the API's `huma.Config`.

**Files:**
- Create: `processor/internal/api/huma_setup.go`
- Test: `processor/internal/api/huma_setup_test.go`

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestLegacyErrorModelSerialises(t *testing.T) {
	InstallLegacyErrorModel()
	err := humaNewError(http.StatusNotFound, "human not found")
	if err.GetStatus() != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", err.GetStatus())
	}
	b, e := json.Marshal(err)
	if e != nil {
		t.Fatalf("marshal: %v", e)
	}
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	if got["status"] != "error" {
		t.Errorf("status field = %v, want \"error\"", got["status"])
	}
	if got["message"] != "human not found" {
		t.Errorf("message field = %v, want \"human not found\"", got["message"])
	}
	if _, hasTitle := got["title"]; hasTitle {
		t.Errorf("legacy body must not contain RFC9457 \"title\" field: %s", b)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestLegacyErrorModelSerialises -v`
Expected: FAIL — `InstallLegacyErrorModel`, `humaNewError` undefined.

- [ ] **Step 3: Implement**

```go
package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// legacyError is the wire shape PoracleWeb/ReactMap already expect from /api.
// It implements huma.StatusError so huma uses it for every generated error.
type legacyError struct {
	StatusCode int    `json:"-"`
	Status     string `json:"status"`            // always "error"
	Message    string `json:"message"`           // human-readable detail
}

func (e *legacyError) Error() string  { return e.Message }
func (e *legacyError) GetStatus() int { return e.StatusCode }

// humaNewError is the value we assign into huma.NewError; kept as a named
// package func so tests can call it directly.
func humaNewError(status int, msg string, _ ...error) huma.StatusError {
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &legacyError{StatusCode: status, Status: "error", Message: msg}
}

// InstallLegacyErrorModel overrides huma's RFC-9457 error model with the
// legacy {status,message} envelope. Call once at startup before registering.
func InstallLegacyErrorModel() {
	huma.NewError = humaNewError
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestLegacyErrorModelSerialises -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/api/huma_setup.go processor/internal/api/huma_setup_test.go
git commit -m "feat(api): legacy {status,message} error model for huma"
```

### Task 3: huma API constructor + public docs/spec mounting

Build the `huma.API` on the existing engine and serve `/openapi.json` + `/docs` at public, unauthenticated top-level paths.

**Files:**
- Modify: `processor/internal/api/huma_setup.go`
- Test: `processor/internal/api/huma_setup_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPublicDocsUnauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	apiGroup := r.Group("/api")
	apiGroup.Use(RequireSecretGin("topsecret")) // gate /api
	_ = NewHumaAPI(r, apiGroup, "test-version")  // mounts docs on r (public)

	for _, path := range []string{"/openapi.json", "/docs"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s unauthenticated = %d, want 200", path, w.Code)
		}
	}
}
```

Add imports: `"net/http"`, `"net/http/httptest"`, `"github.com/gin-gonic/gin"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestPublicDocsUnauthenticated -v`
Expected: FAIL — `NewHumaAPI` undefined.

- [ ] **Step 3: Implement**

```go
import (
	// ...existing...
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
)

// NewHumaAPI installs the legacy error model, builds a huma API bound to the
// authenticated /api group, declares the X-Poracle-Secret security scheme, and
// serves the OpenAPI spec + docs UI at PUBLIC top-level paths (no secret).
func NewHumaAPI(r *gin.Engine, apiGroup *gin.RouterGroup, version string) huma.API {
	InstallLegacyErrorModel()

	cfg := huma.DefaultConfig("PoracleNG API", version)
	// Disable huma's built-in mounts; we serve our own public copies on r.
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"poracleSecret": {Type: "apiKey", In: "header", Name: "X-Poracle-Secret"},
	}

	humaAPI := humagin.NewWithGroup(r, apiGroup, cfg)

	// Public spec + docs (top-level, outside /api, so RequireSecretGin never runs).
	r.GET("/openapi.json", func(c *gin.Context) {
		spec, err := humaAPI.OpenAPI().YAML() // YAML() returns canonical bytes; use MarshalJSON for JSON
		_ = err
		_ = spec
		b, _ := humaAPI.OpenAPI().MarshalJSON()
		c.Data(http.StatusOK, "application/json", b)
	})
	r.GET("/docs", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html", []byte(docsHTML))
	})
	return humaAPI
}

// docsHTML is a minimal Stoplight Elements page pointed at /openapi.json.
const docsHTML = `<!doctype html><html><head><meta charset="utf-8">
<title>PoracleNG API</title>
<script src="https://unpkg.com/@stoplight/elements/web-components.min.js"></script>
<link rel="stylesheet" href="https://unpkg.com/@stoplight/elements/styles.min.css">
</head><body><elements-api apiDescriptionUrl="/openapi.json" router="hash" layout="sidebar"/></body></html>`
```

> **Verify against the pinned version:** confirm the spec accessor is
> `humaAPI.OpenAPI().MarshalJSON()` (huma `OpenAPI` exposes `MarshalJSON`/`YAML`).
> If the method name differs, adjust; the test pins behaviour (200 + JSON body),
> not the accessor name. Remove the dead `YAML()`/`spec` lines once confirmed.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestPublicDocsUnauthenticated -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/api/huma_setup.go processor/internal/api/huma_setup_test.go
git commit -m "feat(api): huma API constructor + public openapi.json/docs"
```

### Task 4: Leniency spike — flex SchemaProvider + additionalProperties

This is the linchpin: huma validates the parsed body against the operation schema *before* binding, so lenient inputs must be permitted by the schema. Prove all three at once on a throwaway endpoint: (a) `flexInt`/`flexBool` accept `"90"`/`false`/`3`; (b) unknown fields don't 422; (c) the spec shows the `oneOf`.

**Files:**
- Modify: `processor/internal/api/tracking.go` (add `Schema` methods to `flexInt`/`flexBool`)
- Create: `processor/internal/api/flex_schema_test.go`

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
)

type spikeBody struct {
	N flexInt  `json:"n"`
	B flexBool `json:"b"`
}
type spikeInput struct{ Body lenient[spikeBody] }
type spikeOutput struct {
	Body struct {
		Status string `json:"status"`
		N      int    `json:"n"`
		B      int    `json:"b"`
	}
}

func TestLeniencySpike(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := NewHumaAPI(r, r.Group("/api"), "test")
	huma.Register(api, huma.Operation{
		OperationID: "spike", Method: http.MethodPost, Path: "/spike",
	}, func(ctx context.Context, in *spikeInput) (*spikeOutput, error) {
		out := &spikeOutput{}
		out.Body.Status = "ok"
		out.Body.N = in.Body.Value.N.intValue(0)
		out.Body.B = in.Body.Value.B.intValue(0)
		return out, nil
	})

	cases := []string{
		`{"n":"90","b":false}`,         // string int, bool
		`{"n":90,"b":3}`,               // native int, int-as-bool-field
		`{"n":90,"b":true,"extra":1}`,  // unknown field must NOT 422
	}
	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/spike", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("body %s -> %d (%s), want 200", body, w.Code, w.Body.String())
		}
	}
}
```

(Add `"context"` import.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestLeniencySpike -v`
Expected: FAIL — `lenient` undefined; flex types lack `Schema`.

- [ ] **Step 3: Implement the Schema methods and the `lenient` wrapper**

In `tracking.go`, add (next to the flex types):

```go
import (
	"reflect"
	"github.com/danielgtaylor/huma/v2"
)

// Schema advertises integer as canonical while accepting numeric strings and
// booleans, so huma's validator permits the legacy forms flexInt unmarshals.
func (flexInt) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{
		OneOf: []*huma.Schema{{Type: "integer"}, {Type: "string"}, {Type: "boolean"}},
		Description: "Canonical: integer. Numeric strings and booleans accepted for legacy clients.",
	}
}

// Schema advertises boolean as canonical while accepting integers (legacy
// bitmask) and numeric strings.
func (flexBool) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{
		OneOf: []*huma.Schema{{Type: "boolean"}, {Type: "integer"}, {Type: "string"}},
		Description: "Canonical: boolean. Integers/strings accepted for legacy clients.",
	}
}

// lenient[T] wraps a request body so huma allows unknown/extra properties
// (matching the pre-huma json.Unmarshal behaviour) instead of huma's default
// additionalProperties:false. Access the decoded value via .Value.
type lenient[T any] struct{ Value T }

func (l *lenient[T]) UnmarshalJSON(b []byte) error { return json.Unmarshal(b, &l.Value) }
func (l lenient[T]) MarshalJSON() ([]byte, error)  { return json.Marshal(l.Value) }

func (lenient[T]) Schema(r huma.Registry) *huma.Schema {
	s := r.Schema(reflect.TypeOf(*new(T)), true, "")
	s.AdditionalProperties = true
	return s
}
```

> **Verify the `additionalProperties` mechanism against v2.38.0.** The
> `lenient[T]` wrapper is the primary approach: it derives the inner struct's
> schema via the registry, then flips `AdditionalProperties` (field type is
> `any`; `true` permits extras). If `r.Schema`'s argument shape or the
> `AdditionalProperties` field type differs in this version, the test in Step 1
> is the contract — make it pass. Fallback if the wrapper proves awkward: a
> `huma.Config` schema transformer that sets `AdditionalProperties = true` on
> request-body object schemas. Pick whichever passes the test cleanly; record
> the choice in a one-line comment.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestLeniencySpike -v`
Expected: PASS for all three bodies.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/api/tracking.go processor/internal/api/flex_schema_test.go
git commit -m "feat(api): flex SchemaProvider + lenient body wrapper (leniency spike)"
```

**Phase 0 exit criteria:** huma builds on the engine, errors use the legacy envelope, docs are public, and lenient bodies validate. The mechanics are proven; the rest is application.

---

## Phase 1 — Tracking group

### Task 5: Canonical-type & bitmask audit (deliverable, no code)

Produce the per-field audit that drives every tracking schema and the bitmask decompositions. This prevents inconsistent typing across the 10 types.

**Files:**
- Create: `docs/superpowers/specs/huma-tracking-field-audit.md`

- [ ] **Step 1: Build the audit table**

For each of the 10 request structs (`monsterInsertRequest` in `trackingMonster.go`, and the equivalents in `trackingRaid.go`, `trackingEgg.go`, `trackingQuest.go`, `trackingInvasion.go`, `trackingLure.go`, `trackingNest.go`, `trackingGym.go`, `trackingFort.go`, `trackingMaxbattle.go`), list every JSON field with columns: `field | current Go type | semantics (int / bool / bitmask / string) | canonical wire type | accepted-lenient forms | decompose? (target booleans + bit)`.

Seed facts (confirm against the structs):
- Bitmask field `clean` (all types): bit 1 auto-delete, bit 2 edit, bit 4 summary (`db/clean.go`). Decompose to `clean:bool` (bit1) + `edit:bool` (bit2) + `summary:bool` (bit4); still accept legacy integer `clean` as the full bitmask.
- `gym`: `slot_changes`, `battle_changes` — DO NOT assume; read the handler validation + bot keywords to determine actual type (bool vs enum vs count) before modeling.
- `raid`/`egg`: `rsvp_changes` is a **3-value enum**, NOT a boolean. Stored `tinyint` `0|1|2`: `0`=`no_rsvp` (none), `1`=`rsvp` (RSVP changes + normal), `2`=`rsvp_only` (only RSVP changes) — per bot keywords `arg.no_rsvp`/`arg.rsvp`/`arg.rsvp_only` and the egg clamp `<0||>2→0`. Model as a **string enum** `"none"|"rsvp"|"rsvp_only"` canonical, ALSO accepting the legacy integer `0|1|2` for old clients (lenient), mapping to the stored int. Do NOT decompose into booleans.
- `quest`: confirm reward fields stay integer/string; `summary` opt-in maps to clean bit 4.
- `fort`: change-type flags.
- Everything else (`pokemon_id`, IVs, CP, level, gender, ranks, distance, weight, size, form): genuine integer → `flexInt` advertising integer.

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/specs/huma-tracking-field-audit.md
git commit -m "docs: per-field canonical-type audit for tracking migration"
```

### Task 6: Worked example — migrate `GET /tracking/pokemon/{id}`

The canonical read-endpoint template. Defines the huma input/output pattern, the `lookupHuman` huma sibling, the legacy success envelope, and the wiring swap.

**Files:**
- Create: `processor/internal/api/huma_tracking.go` (shared helpers + monster ops)
- Modify: `processor/cmd/processor/main.go` (remove the Gin monster-GET route; ensure huma is constructed)
- Test: `processor/internal/api/huma_tracking_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestHumaListMonster(t *testing.T) {
	deps := newTestTrackingDeps(t) // seeds a human "u1" with one pokemon rule, profile 0
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := NewHumaAPI(r, r.Group("/api"), "test")
	RegisterTrackingMonster(api, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/tracking/pokemon/u1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", w.Code, w.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	if _, ok := got["pokemon"].([]any); !ok {
		t.Errorf("missing pokemon array: %s", w.Body.String())
	}
}
```

> `newTestTrackingDeps` is a shared test helper. If one does not already exist
> in the `api` package tests, create it in `huma_tracking_test.go`: build a
> `*TrackingDeps` backed by the existing in-memory mocks (`store.NewMockHuman…`
> per `store/mock_human.go`) and a `TrackingStores` populated with one monster
> rule for id `u1`. Mirror the setup used by `tracking_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestHumaListMonster -v`
Expected: FAIL — `RegisterTrackingMonster` undefined.

- [ ] **Step 3: Implement the shared helpers + monster GET**

```go
package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// humaLookupHuman mirrors lookupHuman but takes plain params instead of *gin.Context.
func humaLookupHuman(deps *TrackingDeps, id string, profileQuery *int) (*store.HumanLite, int, error) {
	human, err := deps.Humans.GetLite(id)
	if err != nil {
		return nil, 0, err
	}
	if human == nil {
		return nil, 0, nil
	}
	profileNo := human.CurrentProfileNo
	if profileQuery != nil {
		profileNo = *profileQuery
	}
	return human, profileNo, nil
}

type listTrackingInput struct {
	ID        string `path:"id" doc:"Human/channel/webhook id"`
	ProfileNo *int   `query:"profile_no" doc:"Profile number; defaults to the user's active profile"`
}

type listMonsterOutput struct {
	Body struct {
		Status  string `json:"status"`
		Pokemon any    `json:"pokemon"`
	}
}

func RegisterTrackingMonster(api huma.API, deps *TrackingDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "list-monster-tracking",
		Method:      http.MethodGet,
		Path:        "/tracking/pokemon/{id}",
		Summary:     "List pokemon tracking rules",
		Tags:        []string{"tracking"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(ctx context.Context, in *listTrackingInput) (*listMonsterOutput, error) {
		human, profileNo, err := humaLookupHuman(deps, in.ID, in.ProfileNo)
		if err != nil {
			return nil, humaNewError(http.StatusInternalServerError, err.Error())
		}
		if human == nil {
			return nil, humaNewError(http.StatusNotFound, "User not found")
		}
		monsters, err := db.SelectMonstersByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			return nil, humaNewError(http.StatusInternalServerError, "database error")
		}
		tr := translatorFor(deps, human)
		type monsterWithDesc struct {
			db.MonsterTrackingAPI
			Description string `json:"description"`
		}
		result := make([]monsterWithDesc, len(monsters))
		for i := range monsters {
			mt := toMonsterTracking(&monsters[i])
			result[i] = monsterWithDesc{
				MonsterTrackingAPI: monsters[i],
				Description:        deps.RowText.MonsterRowText(tr, mt),
			}
		}
		out := &listMonsterOutput{}
		out.Body.Status = "ok"
		out.Body.Pokemon = result
		return out, nil
	})
}
```

This is a verbatim lift of `HandleGetMonster` (`trackingMonster.go`): same
`db.SelectMonstersByIDProfile`, `translatorFor`, `toMonsterTracking`, and
`deps.RowText.MonsterRowText` calls — only the gin context access and the
`trackingJSONOK`/`trackingJSONError` writes are replaced by typed input and
`humaNewError`/the output struct. Each per-type fan-out task (Task 9) lifts its
own `HandleGet<T>` the same way; read that handler for its exact store method
(e.g. `db.SelectRaidsByIDProfile`) and row-text helper.

- [ ] **Step 4: Run the test**

Run: `go test ./internal/api/ -run TestHumaListMonster -v`
Expected: PASS.

- [ ] **Step 5: Swap the wiring in main.go**

In `processor/cmd/processor/main.go`: construct the huma API once after `apiGroup` is created — `humaAPI := api.NewHumaAPI(r, apiGroup, version)` — then `api.RegisterTrackingMonster(humaAPI, trackingDeps)`. Remove the line `tracking.GET("/pokemon/:id", api.HandleGetMonster(trackingDeps))`. Leave the other monster routes on Gin for now (they migrate in Tasks 7–8).

- [ ] **Step 6: Build + full gate + commit**

Run: `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`
Expected: all pass.
```bash
git add processor/internal/api/huma_tracking.go processor/internal/api/huma_tracking_test.go processor/cmd/processor/main.go
git commit -m "feat(api): migrate GET /tracking/pokemon/{id} to huma"
```

### Task 7: Worked example — `POST /tracking/pokemon/{id}` (create/update + clean decomposition)

The richest task: single-object-or-array body, the `clean`/`edit`/`summary` decomposition with legacy-int tolerance, and reuse of the existing diff/insert/update logic.

**Files:**
- Modify: `processor/internal/api/huma_tracking.go`, `processor/internal/api/tracking.go` (add `collapseClean`)
- Modify: `processor/cmd/processor/main.go` (remove Gin monster POST)
- Test: `processor/internal/api/huma_tracking_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestCollapseClean(t *testing.T) {
	tt := []struct {
		name           string
		clean          flexBool
		edit, summary  *bool
		want           int
	}{
		{"bool true -> bit1", mkFlexBool(true), nil, nil, 1},
		{"bool false -> 0", mkFlexBool(false), nil, nil, 0},
		{"legacy int 3 preserved", mkFlexBoolInt(3), nil, nil, 3},
		{"edit adds bit2", mkFlexBool(true), boolp(true), nil, 3},
		{"summary adds bit4", mkFlexBool(true), nil, boolp(true), 5},
		{"all", mkFlexBool(true), boolp(true), boolp(true), 7},
		{"legacy int OR named", mkFlexBoolInt(1), nil, boolp(true), 5},
	}
	for _, c := range tt {
		if got := collapseClean(c.clean, c.edit, c.summary); got != c.want {
			t.Errorf("%s: collapseClean = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestHumaCreateMonsterLenientAndDecomposed(t *testing.T) {
	deps := newTestTrackingDeps(t)
	r := gin.New()
	api := NewHumaAPI(r, r.Group("/api"), "test")
	RegisterTrackingMonster(api, deps)

	// single object, boolean clean + named edit, unknown field, string int
	body := `{"pokemon_id":25,"min_iv":"90","clean":true,"edit":true,"unknownField":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/u1?silent=1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", w.Code, w.Body.String())
	}
	// assert the persisted rule has clean == 3 (bit1|bit2) via deps store inspection
	saved := deps.Tracking.Monsters.LastInserted() // test helper on the mock
	if saved.Clean != 3 {
		t.Errorf("persisted clean = %d, want 3", saved.Clean)
	}
	if saved.MinIV != 90 {
		t.Errorf("persisted min_iv = %d, want 90", saved.MinIV)
	}
}
```

> Helpers `mkFlexBool`/`mkFlexBoolInt`/`boolp` and the mock's `LastInserted()`
> go in the test file; `mkFlexBool(true)` constructs a `flexBool` whose decoded
> value is 1, `mkFlexBoolInt(3)` one whose value is 3.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run 'TestCollapseClean|TestHumaCreateMonster' -v`
Expected: FAIL — `collapseClean` undefined.

- [ ] **Step 3: Implement `collapseClean` + the POST op**

In `tracking.go`:
```go
// collapseClean packs the caller-facing booleans (and any legacy integer clean)
// into the storage bitmask: bit1 auto-delete, bit2 edit, bit4 summary.
func collapseClean(clean flexBool, edit, summary *bool) int {
	packed := clean.intValue(0) // bool->0/1, legacy int bitmask preserved as-is
	if edit != nil && *edit {
		packed |= 2
	}
	if summary != nil && *summary {
		packed |= 4
	}
	return packed
}
```

In `huma_tracking.go`, change `monsterInsertRequest` (or define a huma-facing
variant) so the body carries `Clean flexBool json:"clean"`, `Edit *bool json:"edit"`,
`Summary *bool json:"summary"`, and build the stored `clean` via
`collapseClean(req.Clean, req.Edit, req.Summary)` where the existing create
handler currently reads `req.Clean.intValue(0)`. Model the body as
`Body lenient[[]monsterInsertRequest]` and, before decoding, normalise a single
JSON object to a one-element array (reuse the existing `rawBody[0]=='['` logic
from `HandleCreateMonster`, applied inside the body wrapper's `UnmarshalJSON` or
a small `normaliseToArray` helper). The diff/insert/update + confirmation +
`reloadState` logic is reused verbatim from `HandleCreateMonster`.

> Open `trackingMonster.go:148+` and lift the body of `HandleCreateMonster`
> into the huma handler, replacing `c.Param`/`c.Query`/`c.GetRawData` with the
> typed input fields and the decoded `in.Body.Value` slice. Keep every store
> call, diff helper, and `sendConfirmation` call identical.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -run 'TestCollapseClean|TestHumaCreateMonster' -v`
Expected: PASS.

- [ ] **Step 5: Swap wiring**

Remove `tracking.POST("/pokemon/:id", api.HandleCreateMonster(trackingDeps))` from `main.go`; the huma op is registered by `RegisterTrackingMonster`.

- [ ] **Step 6: Gate + commit**

Run the four-check gate.
```bash
git add -A
git commit -m "feat(api): migrate POST /tracking/pokemon/{id} with clean/edit/summary decomposition"
```

### Task 8: Worked example — monster DELETE + bulk delete

**Files:** `processor/internal/api/huma_tracking.go`, `main.go`, `huma_tracking_test.go`

- [ ] **Step 1: Write failing tests** for `DELETE /tracking/pokemon/{id}/byUid/{uid}` (asserts `{status:ok}` and the row is gone) and `POST /tracking/pokemon/{id}/delete` (body `{"uids":[1,2]}`, asserts both removed). Follow the Task 6 test shape.

- [ ] **Step 2: Run — expect FAIL** (`RegisterTrackingMonster` doesn't yet register these ops).

- [ ] **Step 3: Implement** two more `huma.Register` calls inside `RegisterTrackingMonster`: input structs `deleteByUidInput{ ID string \`path:"id"\`; UID int \`path:"uid"\` }` and `bulkDeleteInput{ ID string \`path:"id"\`; Body lenient[struct{ UIDs []int \`json:"uids"\` }] }`. Reuse the delete store calls + `reloadState` from `HandleDeleteMonster`/`HandleBulkDeleteMonster`.

- [ ] **Step 4: Run — expect PASS.**

- [ ] **Step 5: Swap wiring** — remove the two Gin DELETE/delete routes for pokemon from `main.go`.

- [ ] **Step 6: Gate + commit** `feat(api): migrate monster delete + bulk-delete to huma`.

### Task 9: Fan-out — the other 9 tracking types (one commit each)

The four operations (GET, POST, DELETE byUid, POST delete) for each remaining type are structurally identical to Tasks 6–8. Apply the same transformation per type, using the Task 5 audit for that type's field schema and bitmask decomposition.

**Per-type checklist (repeat for each):** `raid`, `egg`, `quest`, `invasion`, `lure`, `nest`, `gym`, `fort`, `maxbattle`.

- [ ] For type `T`: create `RegisterTracking<T>(api, deps)` in `huma_tracking.go` with the 4 ops, mirroring `RegisterTrackingMonster`. Input path is `/tracking/<route>/{id}` (routes: raid, egg, quest, invasion, lure, nest, gym, fort, maxbattle).
- [ ] Reuse the existing `HandleGet<T>`/`HandleCreate<T>`/`HandleDelete<T>`/`HandleBulkDelete<T>` bodies; swap gin context access for typed input; apply `collapseClean` and the type-specific representation from the audit. NOTE: decomposition is NOT one-size-fits-all — `clean` is a bitmask→booleans; `raid`/`egg` `rsvp_changes` is a 3-value ENUM (string `none|rsvp|rsvp_only` + lenient legacy int); `quest` `summary` is the clean bit-4; `gym` slot/battle changes TBD by audit. Read each field's real validation before modeling.
- [ ] Write a per-type test mirroring `TestHumaListMonster` + a lenient-create assertion.
- [ ] Register `RegisterTracking<T>(humaAPI, trackingDeps)` in `main.go` and remove that type's 4 Gin routes.
- [ ] Gate + commit `feat(api): migrate <T> tracking to huma`.

**Delta table (per-type specifics to honour — fill exact fields from the audit):**

| Type | Route | Bitmask/flag fields to decompose | Notes |
|---|---|---|---|
| raid | `raid` | `clean`→clean/edit/summary; `rsvp_changes`=**enum** `none\|rsvp\|rsvp_only` (+legacy int 0/1/2) | level/pokemon/team/exclusive/move ints |
| egg | `egg` | `clean`…; `rsvp_changes`=**enum** `none\|rsvp\|rsvp_only` (+legacy int) | level/team/exclusive |
| quest | `quest` | `clean`… incl. `summary` opt-in | reward_type/reward ints, `shiny` bool |
| invasion | `invasion` | `clean`… | grunt_type/gender |
| lure | `lure` | `clean`… | lure_id |
| nest | `nest` | `clean`… | pokemon_id, min_spawn_avg |
| gym | `gym` | `clean`…; `slot_changes`,`battle_changes` | team |
| fort | `fort` | `clean`…; change-type flags | fort_type, include_empty |
| maxbattle | `maxbattle` | `clean`… | pokemon_id, level, gmax, move |

### Task 10: Tracking aggregate endpoints

**Files:** `processor/internal/api/huma_tracking.go`, `main.go`, test.

- [ ] **Step 1:** failing tests for `GET /tracking/all/{id}` and `GET /tracking/allProfiles/{id}` (assert `{status:ok}` + expected top-level keys).
- [ ] **Step 2:** run — FAIL.
- [ ] **Step 3:** implement two ops reusing `HandleGetAllTracking`/`HandleGetAllProfilesTracking`. For `GET /tracking/pokemon/refresh` (a reload alias), register a huma op that calls the same reload function `HandleReload` wraps and returns `{status:ok}`.
- [ ] **Step 4:** run — PASS.
- [ ] **Step 5:** remove the three Gin routes; register the huma ops.
- [ ] **Step 6:** gate + commit `feat(api): migrate tracking aggregate endpoints to huma`.

**Phase 1 exit:** the entire `/api/tracking/*` group is served by huma and documented in `/openapi.json`; Gin no longer registers any tracking route.

---

## Phase 2 — Humans group

The humans group uses **two** deps structs: most ops use `trackingDeps`; the four role ops use `roleDeps`. Responses reuse the existing `HumanResponse`/DTO shapes.

### Task 11: Humans read endpoints

**Files:** `processor/internal/api/huma_humans.go`, `main.go`, `processor/internal/api/huma_humans_test.go`

- [ ] **Step 1:** failing tests for `GET /humans/one/{id}` (full record → `HumanResponse` JSON, asserts e.g. `id`, `enabled` are present and unchanged in shape) and `GET /humans/{id}` (available areas).
- [ ] **Step 2:** run — FAIL.
- [ ] **Step 3:** implement `RegisterHumans(api, deps)` with these two ops, reusing `HandleGetOneHuman`/`HandleGetHumanAreas` bodies and the `humanToResponse` adapter so the wire JSON is byte-identical. The `one/{id}` vs `{id}` routing collision is resolved by Gin's router (huma registers via Gin) — assert both routes resolve correctly in the tests.
- [ ] **Step 4:** run — PASS.
- [ ] **Step 5:** remove the two Gin routes; register the ops.
- [ ] **Step 6:** gate + commit `feat(api): migrate humans read endpoints to huma`.

### Task 12: Humans location & profile mutation endpoints

- [ ] **Step 1:** failing tests for `GET /humans/{id}/checkLocation/{lat}/{lon}` (float path params), `GET /humans/{id}/locations`, `GET /humans/{id}/locations/{label}`, `POST /humans/{id}/locations/add`, `POST /humans/{id}/locations/{label}/delete` (asserts 409 when referenced), `POST /humans/{id}/setLocation/{lat}/{lon}`, `POST /humans/{id}/setAreas`, `POST /humans/{id}/switchProfile/{profile}`.
- [ ] **Step 2:** run — FAIL.
- [ ] **Step 3:** add these ops to `RegisterHumans`, `{lat}`/`{lon}` as `float64` path fields, reusing the existing handler bodies and the 409 path for referenced locations.
- [ ] **Step 4:** run — PASS.
- [ ] **Step 5:** remove the corresponding Gin routes.
- [ ] **Step 6:** gate + commit `feat(api): migrate humans location/profile mutations to huma`.

### Task 13: Humans status/language + create endpoints

- [ ] **Step 1:** failing tests for `POST /humans/{id}/start`, `/stop`, `/adminDisabled`, `/language`, and `POST /humans` (create).
- [ ] **Step 2:** run — FAIL.
- [ ] **Step 3:** add the ops, reusing `HandleStartHuman`/`HandleStopHuman`/`HandleAdminDisabled`/`HandleSetLanguage`/the create handler. `POST /humans` has no `{id}` path param — body-only input.
- [ ] **Step 4:** run — PASS.
- [ ] **Step 5:** remove the Gin routes.
- [ ] **Step 6:** gate + commit `feat(api): migrate humans status/language/create to huma`.

### Task 14: Humans role endpoints (roleDeps)

- [ ] **Step 1:** failing tests for `GET /humans/{id}/roles`, `GET /humans/{id}/getAdministrationRoles`, `POST /humans/{id}/roles/add/{roleId}`, `POST /humans/{id}/roles/remove/{roleId}`.
- [ ] **Step 2:** run — FAIL.
- [ ] **Step 3:** implement `RegisterHumanRoles(api, roleDeps)` (separate function because it closes over `roleDeps`, not `trackingDeps`), reusing `HandleGetRoles`/`HandleGetAdministrationRoles`/`HandleAddRole`/`HandleRemoveRole`.
- [ ] **Step 4:** run — PASS.
- [ ] **Step 5:** remove the four Gin role routes; register `RegisterHumanRoles(humaAPI, roleDeps)` in `main.go`.
- [ ] **Step 6:** gate + commit `feat(api): migrate humans role endpoints to huma`.

**Phase 2 exit:** all `/api/humans/*` routes served by huma; both deps structs wired.

---

## Phase 3 — Profiles group

### Task 15: Profiles endpoints

**Files:** `processor/internal/api/huma_profiles.go`, `main.go`, `processor/internal/api/huma_profiles_test.go`

- [ ] **Step 1:** failing tests for `GET /profiles/{id}` (→ `ProfileResponse` shape), `POST /profiles/{id}/add`, `POST /profiles/{id}/update`, `POST /profiles/{id}/copy/{from}/{to}` (int path params), `DELETE /profiles/{id}/byProfileNo/{profile_no}`.
- [ ] **Step 2:** run — FAIL.
- [ ] **Step 3:** implement `RegisterProfiles(api, deps)` with the five ops, reusing `HandleGetProfiles`/`HandleAddProfile`/`HandleUpdateProfile`/`HandleCopyProfile`/`HandleDeleteProfile` and `profilesToResponse`/`profileToResponse`.
- [ ] **Step 4:** run — PASS.
- [ ] **Step 5:** remove the five Gin profile routes; register `RegisterProfiles(humaAPI, trackingDeps)`.
- [ ] **Step 6:** gate + commit `feat(api): migrate profiles endpoints to huma`.

**Phase 3 exit:** all three groups served by huma.

---

## Phase 4 — Finalise

### Task 16: OpenAPI golden test

**Files:** Create `processor/internal/api/openapi_golden_test.go`, `processor/internal/api/testdata/openapi.golden.json`

- [ ] **Step 1:** write a test that builds a huma API, registers all three groups (`RegisterTracking*`, `RegisterHumans`, `RegisterHumanRoles`, `RegisterProfiles`) against `newTestTrackingDeps`, marshals `humaAPI.OpenAPI().MarshalJSON()`, and compares to `testdata/openapi.golden.json` (with a `-update` flag pattern to regenerate).
- [ ] **Step 2:** run with update to generate the golden file; eyeball it for the three groups, the `oneOf` flex schemas, the `poracleSecret` scheme, and `additionalProperties:true` on request bodies.
- [ ] **Step 3:** run without update — PASS.
- [ ] **Step 4:** gate + commit `test(api): golden OpenAPI spec for migrated groups`.

### Task 17: Remove dead Gin handlers + verify no references

**Files:** `processor/internal/api/trackingMonster.go` … `trackingMaxbattle.go`, `human*.go`, `profile*.go`

- [ ] **Step 1:** grep for the now-unused `Handle*` functions for the three groups: `grep -rn 'HandleGetMonster\|HandleCreateMonster\|…' processor/` — confirm they are referenced only by their own definitions/tests.
- [ ] **Step 2:** delete the dead Gin handler functions and any now-unused helpers (keep shared helpers like `lookupHuman` only if still used elsewhere; `go vet`/`golangci-lint` unused-function checks will flag stragglers).
- [ ] **Step 3:** run the four-check gate — must be green with no unused-symbol lint errors.
- [ ] **Step 4:** commit `refactor(api): remove Gin handlers superseded by huma`.

### Task 18: README/docs note

**Files:** `README.md` (or `API.md`), `CLAUDE.md` API section

- [ ] **Step 1:** add a short note: the API now publishes an OpenAPI spec at `/openapi.json` and interactive docs at `/docs` (public), with `/api/*` gated by `X-Poracle-Secret`. Note the canonical-vs-lenient field convention (prefer canonical types; legacy forms still accepted).
- [ ] **Step 2:** update the CLAUDE.md API section to mention huma serves tracking/humans/profiles while the rest stays on Gin.
- [ ] **Step 3:** commit `docs: document OpenAPI spec, public docs, and field conventions`.

**Final exit criteria:** all three groups served by huma with byte-compatible legacy envelopes, lenient bodies, decomposed bitmask fields, a public docs UI, a golden-tested spec, no dead Gin code, and a green four-check gate.

---

## Self-review notes

- **Spec coverage:** coexistence (Task 3/6), legacy envelope (Task 2 + every op's `{status:ok}`/`humaNewError`), leniency + SchemaProvider + additionalProperties (Task 4), per-field audit (Task 5), clean decomposition (Task 7 + fan-out), all 43 tracking / ~19 humans / 5 profiles routes (Tasks 6–15), public docs (Task 3), security scheme (Task 3), single-or-array body (Task 7), float path params (Task 12/15), `one/{id}` routing (Task 11), two deps structs (Task 14), golden test (Task 16), out-of-scope groups never touched. ✓
- **Risk-first ordering:** the three flagged risks (validation ordering, additionalProperties mechanism, error override) are all resolved in Phase 0 before any fan-out, so a wrong assumption is caught on one endpoint, not 60.
- **Placeholders:** the per-handler "reuse the existing body" instructions point at concrete existing functions by name; the exact store/helper method names must be read from the current handler being migrated (called out explicitly each time).
