# Huma Easy-Wins (in-place /api) Implementation Plan

> **SUPERSEDED CONVENTION — errors.** The error convention below (`{status:"error",message}` via `humaNewError`) was superseded by the master plan (`2026-06-03-huma-full-api-master-plan.md`, Task 0.1): the built surface uses RFC 9457 `application/problem+json` via huma's default error model, and `InstallLegacyErrorModel`/`humaNewError` no longer exist. Everything else in this plan was executed as written (as P1/P2 of the master plan).

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:subagent-driven-development (or executing-plans) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Document and validate the ~30 "easy-win" `/api/*` endpoints (reloads, read-only data, tile-URL, masterdata, DTS-editor reads, snapshots, summaries, autocreate/run, command) by moving them to huma **in place** — same paths, same success JSON, no client changes — so they appear in the OpenAPI spec at `/openapi.json` + `/docs`.

**Architecture:** Register these as huma operations on the **existing** `huma.API` created by `api.NewHumaAPI(r, apiGroup, version)` (`internal/api/huma_setup.go`), which is already bound to the authenticated `/api` group and serves the public spec/docs. For each endpoint: define typed input/output structs whose JSON marshals **identically** to the current gin handler's success response, reuse the handler's business logic, register the huma op at the same path, and remove the old gin route. This is independent of the v2 CRUD redesign and of the frozen v1 tracking/humans/profiles contracts (this plan does **not** touch those three groups).

**Tech Stack:** Go 1.26, gin + `humagin`, `github.com/danielgtaylor/huma/v2` (already a dep), `net/http/httptest` tests.

**Reference:** triage in this session; the pokemon GET migration (`internal/api/huma_tracking.go` `RegisterTrackingMonster`) is the structural template for an in-place huma op.

---

## Conventions (every task)

- **Register on the existing instance.** Add `Register<X>(humaAPI, <deps>)` functions in the `api` package; call them from `main.go` where `humaAPI` and the relevant deps are in scope. Do **not** create a second huma API.
- **Preserve success JSON exactly.** Read the current handler; define an output struct (or `Body any`) that marshals to the identical shape. Where a handler returns `any` (e.g. stats), use `Body any`.
- **Error bodies normalize to `{status:"error",message}`.** Current handlers use ad-hoc `gin.H{"error": …}`; huma's single global error model (`humaNewError`) emits the legacy envelope. Success is byte-identical; error bodies change shape. This is accepted (internal endpoints, status codes unchanged). Use `humaNewError(code, msg)` for error returns.
- **Security + auth** are inherited from the `/api` gin group; add `Security: []map[string][]string{{"poracleSecret": {}}}` to each op for the docs.
- **Remove the gin route** for each migrated endpoint from `main.go` in the same task.
- **Tags:** group ops with `Tags` (e.g. `reload`, `stats`, `geofence`, `masterdata`, `dts`, `summaries`, `autocreate`, `system`).
- **Pre-commit gate** (from `processor/`): `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...` — all green before each commit.
- **Commit trailer:** end each commit message with a blank line then `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

## File structure

New files in `processor/internal/api/`, one per cluster (keeps each focused):
- `huma_system.go` — health, reloads.
- `huma_data_reads.go` — weather, stats, geocode, geofence reads, masterdata, config/schema, snapshots.
- `huma_tiles.go` — geofence tile-URL endpoints.
- `huma_dts_reads.go` — DTS editor read endpoints.
- `huma_features.go` — autocreate/run + templates(schema/delete), summaries, command.
- Tests alongside as `*_test.go`; one golden-spec test in `huma_easywins_golden_test.go`.

`main.go` loses the migrated gin route registrations and gains `api.Register<X>(humaAPI, …)` calls.

---

## Task 1: Worked example — reload endpoints (shared pattern)

The 7 reload endpoints all use `HandleReload(fn)` and return `{status:"ok"}`. One huma op type covers all; register each with its own `fn`.

**Files:** Create `processor/internal/api/huma_system.go`, `huma_system_test.go`; modify `main.go`.

- [ ] **Step 1: failing test** (`huma_system_test.go`)
```go
func TestHumaReload_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")
	called := false
	RegisterReload(humaAPI, "test-reload", http.MethodGet, "/reload", func() error { called = true; return nil })

	req := httptest.NewRequest(http.MethodGet, "/api/reload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK { t.Fatalf("status=%d body=%s", w.Code, w.Body.String()) }
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["status"] != "ok" { t.Errorf("status=%v want ok", got["status"]) }
	if !called { t.Error("reload fn not called") }
}

func TestHumaReload_Error(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")
	RegisterReload(humaAPI, "test-reload-err", http.MethodGet, "/reload", func() error { return errors.New("boom") })
	req := httptest.NewRequest(http.MethodGet, "/api/reload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError { t.Fatalf("status=%d", w.Code) }
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["status"] != "error" { t.Errorf("error body = %s", w.Body.String()) }
}
```

- [ ] **Step 2: run → FAIL** (`RegisterReload` undefined). `go test ./internal/api/ -run TestHumaReload -v`

- [ ] **Step 3: implement** (`huma_system.go`)
```go
package api

import (
	"context"
	"net/http"
	"github.com/danielgtaylor/huma/v2"
)

type statusOKOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

// RegisterReload registers a reload-style op (returns {"status":"ok"} or the
// legacy error envelope) for the given method/path on the shared huma API.
func RegisterReload(api huma.API, opID, method, path string, fn func() error) {
	huma.Register(api, huma.Operation{
		OperationID: opID, Method: method, Path: path,
		Summary: "Trigger a reload", Tags: []string{"reload"},
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(ctx context.Context, _ *struct{}) (*statusOKOutput, error) {
		if err := fn(); err != nil {
			return nil, humaNewError(http.StatusInternalServerError, err.Error())
		}
		out := &statusOKOutput{}
		out.Body.Status = "ok"
		return out, nil
	})
}
```
Note: huma allows GET and POST on the same path via two `huma.Register` calls with distinct `OperationID`s.

- [ ] **Step 4: run → PASS.**

- [ ] **Step 5: wire main.go** — replace the 7 gin reload registrations (`apiGroup.{GET,POST}("/reload", …)`, `/geofence/reload` ×2, `/tracking/pokemon/refresh`, `/dts/reload` ×2) with `api.RegisterReload(humaAPI, "<id>", "<METHOD>", "<path>", <fn>)` using the same `fn` closures already present. Keep the closures (they call `proc.triggerReloadErr` / `reloadDTS` / geofence reload) intact.

- [ ] **Step 6: gate + commit** `feat(api): huma in-place for reload endpoints`.

## Task 2: Worked example — weather (typed query + map response)

**Files:** `huma_data_reads.go`, `huma_data_reads_test.go`, `main.go`.

- [ ] **Step 1: failing test** — `GET /api/weather?cell=<id>` returns the same map JSON as `HandleWeather`; missing `cell` → 4xx with legacy error body. (Mirror Task 1's test style; use a stub `WeatherExporter`.)
- [ ] **Step 2: run → FAIL.**
- [ ] **Step 3: implement**
```go
type weatherInput struct {
	Cell string `query:"cell" required:"true" doc:"S2 cell id"`
}
type weatherOutput struct{ Body any } // ExportCellWeather returns a map; preserve shape

func RegisterWeather(api huma.API, weather WeatherExporter) {
	huma.Register(api, huma.Operation{
		OperationID: "get-weather", Method: http.MethodGet, Path: "/weather",
		Summary: "Weather for an S2 cell", Tags: []string{"data"},
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(ctx context.Context, in *weatherInput) (*weatherOutput, error) {
		return &weatherOutput{Body: weather.ExportCellWeather(in.Cell)}, nil
	})
}
```
(`required:"true"` makes huma return its validation error when `cell` is absent — replaces the manual 400.)
- [ ] **Step 4: run → PASS.**
- [ ] **Step 5: wire main.go** — replace `apiGroup.GET("/weather", …)` with `api.RegisterWeather(humaAPI, proc.weather)`.
- [ ] **Step 6: gate + commit** `feat(api): huma in-place for weather`.

## Task 3: Read-only data batch — stats, geocode, geofence reads, masterdata, config/schema, snapshots, health

Each is a faithful in-place port following Tasks 1–2. Per endpoint: define input (path/query as needed) + output (`Body any` or a typed struct matching the current response), register, remove the gin route, test (assert success JSON shape + that the route serves under `/api`). Add to `huma_data_reads.go` / `huma_system.go`.

**Per-endpoint checklist (repeat):**

| endpoint | method | input | output Body | source handler | tag |
|---|---|---|---|---|---|
| `/health` | GET | none | typed `{status,version,capabilities}` (struct exists: `Capabilities`) | `HandleHealth` | system |
| `/stats/rarity` | GET | none | `any` | `HandleStats(ExportGroups)` | stats |
| `/stats/shiny` | GET | none | `any` | `HandleStats(ExportShinyStats)` | stats |
| `/stats/shiny-possible` | GET | none | `any` | `HandleStats(ExportShinyPossible)` | stats |
| `/geocode/forward` | GET | `q` query (required) | `any` (results slice) | `HandleGeocode` | data |
| `/geofence/all` | GET | none | `{status, geofence}` typed | `HandleGeofenceAll` | geofence |
| `/geofence/all/hash` | GET | none | `{status, areas}` map | hash handler | geofence |
| `/geofence/all/geojson` | GET | none | `{status, geoJSON}` | geojson handler | geofence |
| `/masterdata/monsters` | GET | `locale` query (optional) | `map[string]*poracle2Monster` (pre-marshalled bytes — may use `huma.Register` with a raw body or `Body any`) | `HandleMasterdataMonsters` | masterdata |
| `/masterdata/grunts` | GET | none | `map[string]*poracle2Grunt` | `HandleMasterdataGrunts` | masterdata |
| `/config/schema` | GET | none | `[]ConfigSection` typed | `HandleConfigSchema` | config |
| `/snapshots/{messageID}` | GET | `messageID` path, `target` query (required) | `snapshots.Snapshot` (503 if disabled, 404 if missing) | `HandleSnapshot` | system |

- [ ] For each row: failing test → run FAIL → implement `Register<X>` (read the handler for the exact response struct/fields and any error codes like snapshots' 404/503, returned via `humaNewError`) → run PASS → remove gin route in main.go → gate + commit per small group (e.g. `feat(api): huma in-place for stats endpoints`).
- [ ] **Note on masterdata:** the current handlers serve pre-marshalled `[]byte` via `c.Data(...)`. For huma, either expose `Body any` of the typed map (re-marshals; same JSON) or, if the pre-marshalled bytes must be preserved verbatim, keep that endpoint on gin and note it. Prefer `Body` typed map unless a test shows a diff.

## Task 4: Geofence tile-URL endpoints (5)

All return `{status, url}` JSON (NOT image bytes). Add to `huma_tiles.go`.

| endpoint | input | source |
|---|---|---|
| `/geofence/{area}/map` | `area` path | `HandleAreaMap` |
| `/geofence/weatherMap/{lat}/{lon}` | `lat`,`lon` float path (+ `weather` optional query) | `HandleWeatherMap` |
| `/geofence/locationMap/{lat}/{lon}` | `lat`,`lon` float path | `HandleLocationMap` |
| `/geofence/distanceMap/{lat}/{lon}/{distance}` | 3 numeric path params | `HandleDistanceMap` |
| `/geofence/overviewMap` | body `{areas: []string}` (POST) | `HandleOverviewMap` |

- [ ] Per endpoint: TDD port, output `{status, url}` struct, remove gin route, test, gate + commit `feat(api): huma in-place for geofence tile endpoints`.
- [ ] Float path params are fine (`float64` path fields), per the pokemon precedent.

## Task 5: DTS editor read endpoints (8)

Add to `huma_dts_reads.go`. All have typed responses per the triage.

| endpoint | input | source |
|---|---|---|
| `/dts/emoji` | `platform` query (optional) | `HandleDtsEmoji` |
| `/dts/templates` (GET) | `type,platform,language,id` query (optional) | `HandleDtsTemplatesGet` |
| `/dts/templates` (DELETE) | `type,platform,language,id` query (required set) | `HandleDtsTemplatesDelete` |
| `/dts/fields` | none | `HandleDtsFields` |
| `/dts/fields/{type}` | `type` path | `HandleDtsFieldsType` |
| `/dts/partials` | none | `HandleDtsPartials` |
| `/dts/testdata` | `type` query (optional) | `HandleDtsTestdata` |
| `/dts/actions` | none | `HandleDtsActions` |
| `/dts/templates/file` (PUT) | body `{content}` + 4 query | `HandleDtsTemplateFile` |

- [ ] Per endpoint: TDD port (read each handler for the exact typed response struct), remove gin route, test, gate. Commit in 2–3 logical groups (`feat(api): huma in-place for dts read endpoints`).
- [ ] Skip the MODERATE DTS endpoints (`/dts/templates` POST, `/dts/render`, `/dts/enrich`, `/dts/sendtest`) — out of scope here (see Task 8).

## Task 6: New feature endpoints — autocreate, summaries, command

Add to `huma_features.go`.

| endpoint | method | input | source |
|---|---|---|---|
| `/autocreate/run` | POST | typed `{rule,dry_run,reset,removals,force}` | `HandleAutocreateRun` |
| `/autocreate/templates/{name}` | DELETE | `name` path | delete handler |
| `/autocreate/templates/schema` | GET | none | schema handler |
| `/summaries/{id}` | GET | `id` path | list handler |
| `/summaries/{id}/{alertType}` | GET | 2 path | get handler |
| `/summaries/{id}/{alertType}` | DELETE | 2 path | delete handler |
| `/summaries/{id}/{alertType}/trigger` | POST | 2 path | trigger handler |
| `/command` | POST | typed `commandRequest` | `HandleCommand` |

- [ ] Per endpoint: TDD port using the existing typed request/response structs (these already exist — reuse them as the huma `Body` types), remove gin route, test, gate. Commit per feature group.
- [ ] Skip `POST /summaries/{id}/{alertType}` (polymorphic `active_hours`) and the autocreate templates save/validate (raw-JSON body) — they're MODERATE (Task 8).

## Task 7: Golden OpenAPI spec test for the easy-wins surface

**Files:** `huma_easywins_golden_test.go`, `testdata/openapi-easywins.golden.json`.

- [ ] Build a huma API, register all easy-win groups against stub deps, marshal `humaAPI.OpenAPI().MarshalJSON()`, compare to a committed golden file (with `-update`). Eyeball: every easy-win path present, `poracleSecret` security on each, tags grouped. Gate + commit.

## Task 8: MODERATE endpoints — decision record (no code)

**Files:** append a short section to `docs/v2-api-design.md` or a new `docs/huma-moderate-endpoints.md`.

- [ ] Record the disposition for the ~15 MODERATE endpoints (freeform `map[string]any` / `json.RawMessage` bodies): which to huma-fy later with `Body any`/`json.RawMessage` (accepting open schemas) vs leave on gin (`config/values`+`validate` recommended to stay on gin until the editor wire format stabilises). No implementation in this plan.

## Task 9: Docs note

- [ ] Update `README.md` / CLAUDE.md API section: the listed `/api` read/reload/feature endpoints now appear in the OpenAPI spec (`/openapi.json`, `/docs`); note the error-body normalization to `{status,message}` for migrated endpoints.

---

## Self-review

- **Scope coverage:** all ~30 EASY endpoints from the triage are assigned (Tasks 1–6); golden test (7); MODERATE explicitly deferred (8); docs (9). Tracking/humans/profiles untouched (correct — they're v2/frozen).
- **Contract fidelity:** success JSON preserved per endpoint (read the handler); the one accepted change — error bodies normalize to `{status,message}` — is called out in Conventions and Task 9.
- **No second huma instance:** every task registers on the existing `NewHumaAPI` instance (Conventions) — avoids the global-`huma.NewError` conflict and reuses public docs.
- **Placeholders:** per-endpoint "read the handler for the exact response struct" is intentional (the handlers already define typed responses; enumerate them at implementation). The two worked examples (reload, weather) carry full code as the copy template.
- **Risk:** masterdata serves pre-marshalled bytes — Task 3 notes the verbatim-bytes caveat and a gin fallback if a test shows a diff.
- **Independence:** this plan stands alone (delivers a documented `/api` read surface) regardless of v2 progress or RFC feedback.
