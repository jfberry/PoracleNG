# Huma API Migration — tracking, humans, profiles

**Date:** 2026-05-30
**Branch:** `huma-api-migration` (worktree off `develop`)
**Status:** Design — awaiting review

## Goal

Migrate the `/api/tracking/*`, `/api/humans/*`, and `/api/profiles/*` endpoint
groups from hand-written Gin handlers to the [huma](https://huma.rocks)
framework (`github.com/danielgtaylor/huma/v2`). The driver is **documentation
discoverability**: huma generates an OpenAPI 3.1 spec and hosted docs UI from
the Go types, giving integrators (PoracleWeb, ReactMap, third parties) a
single source of truth. A secondary driver is using the migration as an
opportunity to present a **cleaner, type-honest API** while remaining tolerant
of the broken/legacy clients the current `flexBool`/`flexInt` coercion exists
to serve.

This is a full migration of the three named groups only. Everything else stays
on Gin (see Out of Scope).

## Decisions (locked)

| Topic | Decision |
|---|---|
| Scope | Full migration of tracking (~43 routes), humans (~19), profiles (5). Nothing else. |
| Coexistence | `humagin` mounted on the **existing** `*gin.Engine`; migrated groups move from Gin wiring → `huma.Register`. |
| Wire format | **Preserve the legacy envelope.** `{status:"ok", ...}` on success; `huma.NewError` overridden to emit `{status:"error", message:"..."}`. `authError` is unchanged (emitted by existing Gin middleware before huma). |
| Leniency | flex coercion stays as a tolerance layer; each field declares its **canonical** schema via `SchemaProvider`; request bodies set `additionalProperties: true`. |
| Type cleanup | Per-field canonical-type audit; document the truest type; decompose packed bitmask fields to caller-facing booleans, collapse to the storage column internally; always keep accepting the legacy form. |
| Docs exposure | Public, unauthenticated `/openapi.json` + `/docs`; `X-Poracle-Secret` declared as an apiKey security scheme; `/api/*` itself stays gated. |
| Testing | Table-driven handler tests (envelope + leniency + bitmask collapse), a golden-file test over `openapi.json`, error-path tests. Existing 4-check gate stays green. |

## Architecture

### Coexistence (approach A)

`main.go` continues to build the same `*gin.Engine` with the same global
middleware (`gin.Recovery`, `CORSMiddleware`, `RequestLogger`, `IPFilter`) and
the same `/api` route group carrying `RequireSecretGin`. After that group is
created, a single huma API is bound to it. The snippet below is **illustrative**
— exact `huma.Config` field paths (security-scheme location, how to disable the
built-in docs/spec routes) are confirmed against the installed huma version
during implementation:

```go
humaCfg := huma.DefaultConfig("PoracleNG API", version)
// disable huma's built-in docs + spec auto-mount; we serve them ourselves
// at public top-level paths (see below).
api.OverrideHumaError()        // install legacy {status,message} error model
// declare the apiKey security scheme (X-Poracle-Secret) on the spec's components
humaAPI := humagin.NewWithGroup(r, apiGroup, humaCfg)
```

- Huma operations register as ordinary Gin routes under `apiGroup`, so they
  inherit the existing middleware unchanged. No auth/CORS/logging duplication.
- The tracking/humans/profiles route registrations are **removed** from the
  Gin wiring in `main.go` and re-expressed as `huma.Register(...)` calls in the
  `api` package (one registration function per group, or per type for
  tracking). The corresponding old `gin.HandlerFunc` handlers for these three
  groups are deleted once replaced.
- **Docs are public**: `r.GET("/openapi.json", ...)` and a docs-UI handler are
  registered directly on `r` (top level, outside `apiGroup`), so they require
  no secret. The spec advertises `poracleSecret` so the docs' "Authorize" box
  works and every operation shows its security requirement.

### Handler shape

The dependency-injection pattern is preserved; only the HTTP edge changes.
`TrackingDeps` and `roleDeps` (the humans/roles endpoints use a separate deps
struct) are reused verbatim.

```go
type listMonsterInput struct {
    ID        string `path:"id"`
    ProfileNo int    `query:"profile_no"`
}
type listMonsterOutput struct {
    Body struct {
        Status  string                  `json:"status"`           // always "ok"
        Pokemon []monsterTrackingDTO    `json:"pokemon"`
    }
}

func registerMonster(api huma.API, deps *TrackingDeps) {
    huma.Register(api, huma.Operation{
        OperationID: "list-monster-tracking",
        Method:      http.MethodGet,
        Path:        "/tracking/pokemon/{id}",
        Summary:     "List pokemon tracking rules for a user",
        Tags:        []string{"tracking"},
        Security:    []map[string][]string{{"poracleSecret": {}}},
    }, func(ctx context.Context, in *listMonsterInput) (*listMonsterOutput, error) {
        // identical body logic, reading in.ID / in.ProfileNo
        // returns &listMonsterOutput{...} or huma.Error404NotFound(...)
    })
}
```

- Inputs: typed structs with `path:`/`query:`/`header:` tags and an optional
  `Body` field. Outputs: typed structs with a `Body` field whose first member
  is `Status string json:"status"`.
- Business helpers (`lookupHuman`, `reloadState`, `sendConfirmation`,
  `validateOverrideFields`, store calls) are reused — they never touched Gin.
  `lookupHuman` gets a small huma-flavoured sibling that takes `(id, profileNo)`
  rather than `*gin.Context`, so the gin and huma versions share the underlying
  store logic.

### File layout

New files in `processor/internal/api/`:

- `huma_setup.go` — huma config, `OverrideHumaError`, security scheme, docs +
  spec mounting helpers.
- `huma_tracking.go` (+ per-type registration, mirroring the existing
  `trackingMonster.go` … split) — tracking operations.
- `huma_humans.go` — humans operations.
- `huma_profiles.go` — profiles operations.
- `flex.go` — `flexBool`/`flexInt` gain `Schema(...)` methods + the per-field
  canonical-type machinery (moved out of `tracking.go` or extended in place).

Old gin handlers for the three migrated groups are removed once their huma
replacements pass tests.

## Leniency & type cleanup

### flex types as a tolerance layer

`flexBool`/`flexInt` keep their existing `UnmarshalJSON` (accept
`true`/`false`/`0`/`1`/`"1"`/numbers). They gain a `Schema` method so huma's
validator — which validates the parsed body against the operation schema
*before* binding — permits those forms instead of rejecting them with 422:

```go
func (flexInt) Schema(huma.Registry) *huma.Schema {
    return &huma.Schema{
        OneOf: []*huma.Schema{{Type: "integer"}, {Type: "string"}, {Type: "boolean"}},
        Description: "Canonical form: integer. Numeric strings and booleans accepted for legacy clients.",
    }
}
```

The **canonical** type advertised is decided per field (below), not blanket
integer. Request body structs set `additionalProperties: true` (huma defaults
to `false`) so unknown/extra fields from lenient clients are not rejected —
matching current behaviour. This is set per request struct, not globally.

### Per-field canonical-type audit (deliverable)

Before/with the migration, produce a field-by-field table for every request
struct in the three groups, classifying each field as **genuine-int**,
**genuine-bool**, **bitmask-int**, or **string/other**, with its documented
canonical type and accepted-lenient forms. The audit output lives in this spec
(appendix, filled during planning) and drives the `Schema` methods.

Observed starting points:

- `monsterInsertRequest`: nearly all fields are genuine integers
  (`pokemon_id`, IVs, CP, level, gender, ranks, distance, weight, size) →
  `flexInt` advertising **integer** is correct. `clean` is the lone `flexBool`
  and is actually a **bitmask** (see below), not a boolean.

### `clean` bitmask decomposition (the template pattern)

`clean` is a bitmask (`db/clean.go`): bit 1 = auto-delete, bit 2 = edit,
bit 4 = summary. API callers historically understood `clean` only as a
true/false "clean it up" toggle (bit 1); bits 2 and 4 are set through other
surfaces (bot `edit` mode, quest `summary` keyword) and are not part of any
caller's mental model. So:

- **Wire (documented):**
  - `clean: boolean` → bit 1 (auto-delete)
  - `edit: boolean` → bit 2 (added where used: raid/egg rsvp)
  - `summary: boolean` → bit 4 (added where used: quest)
- **Back-compat tolerance:** still accept a legacy **integer** `clean` and
  interpret it as the full packed bitmask, so any caller that sent `clean:3`
  keeps working.
- **Collapse rule (handler/DTO layer):**
  ```
  packed  = 0
  packed |= cleanAsInt           // if clean arrived as an integer (legacy)
  packed |= 1  if cleanBool      // if clean arrived as a boolean true
  packed |= 2  if editBool
  packed |= 4  if summaryBool
  ```
- **Storage unchanged:** the single `clean` int column, the matcher, and
  `IsClean`/`IsEdit`/`IsSummary` are untouched.

The audit applies the *principle* — model the caller's mental model, stay
lenient about the legacy form — but the right representation is per-field and
must be read from each field's actual handler validation + bot keywords, NOT
assumed. It is NOT uniformly "boolean-on-the-wire":
- **Bitmask → named booleans**: `clean` (bits 1/2/4 → `clean`/`edit`/`summary`),
  always also accepting the legacy integer bitmask.
- **Enum → string enum**: `raid`/`egg` `rsvp_changes` is a 3-value enum
  (`tinyint 0|1|2` = `no_rsvp`/`rsvp`/`rsvp_only`, per the `!raid`/`!egg`
  keywords) — model as a string enum `"none"|"rsvp"|"rsvp_only"`, ALSO accepting
  the legacy integer `0|1|2`. NOT a boolean.
- **Genuine bool / int / count**: `gym` `slot_changes`/`battle_changes`, `fort`
  change flags, etc. — TBD by reading the handler; do not assume.
Storage columns and matcher logic are untouched in every case.

## Wire format (legacy envelope)

- **Errors:** `OverrideHumaError` reassigns the package-level `huma.NewError`
  to a custom `StatusError` whose JSON body is `{"status":"error","message":...}`
  with the same status codes huma would have used (422 validation, 404, etc.).
  Validation detail strings are folded into `message`.
- **authError:** unchanged. `RequireSecretGin` runs as Gin middleware *before*
  huma sees the request and already emits `{"status":"authError","reason":...}`.
- **Success:** every output `Body` struct begins with `Status string json:"status"`
  set to `"ok"`, followed by the existing per-endpoint fields. Response DTOs
  (`HumanResponse`, `ProfileResponse`, the tracking-list shapes) are reused
  unchanged as nested body types so the wire JSON is byte-compatible with today.

## Endpoint inventory

**Tracking** (`trackingDeps`): per type `GET /tracking/{type}/{id}`,
`POST /tracking/{type}/{id}`, `DELETE /tracking/{type}/{id}/byUid/{uid}`,
`POST /tracking/{type}/{id}/delete` for the 10 types (pokemon, raid, egg,
quest, invasion, lure, nest, gym, fort, maxbattle) = 40, plus
`GET /tracking/all/{id}`, `GET /tracking/allProfiles/{id}`, and
`GET /tracking/pokemon/refresh` (a reload alias) = **43**.

**Humans** (mixed `trackingDeps` + `roleDeps`): `GET /humans/one/{id}`,
`GET /humans/{id}`, `GET /humans/{id}/roles`,
`GET /humans/{id}/getAdministrationRoles`,
`GET /humans/{id}/checkLocation/{lat}/{lon}`, `GET /humans/{id}/locations`,
`GET /humans/{id}/locations/{label}`, `POST /humans/{id}/locations/add`,
`POST /humans/{id}/locations/{label}/delete`, `POST /humans/{id}/start`,
`POST /humans/{id}/stop`, `POST /humans/{id}/adminDisabled`,
`POST /humans/{id}/language`, `POST /humans/{id}/switchProfile/{profile}`,
`POST /humans/{id}/setLocation/{lat}/{lon}`, `POST /humans/{id}/setAreas`,
`POST /humans/{id}/roles/add/{roleId}`, `POST /humans/{id}/roles/remove/{roleId}`,
plus `POST /humans` (create) — **~19**.

**Profiles** (`trackingDeps`): `GET /profiles/{id}`,
`DELETE /profiles/{id}/byProfileNo/{profile_no}`, `POST /profiles/{id}/add`,
`POST /profiles/{id}/update`, `POST /profiles/{id}/copy/{from}/{to}` — **5**.

### Known special cases

- **Tracking POST accepts a single object OR an array** (current
  `rawBody[0]=='['` branch). Model the body as an array type with a wrapper
  that also accepts a single object (custom `UnmarshalJSON` on the wrapper,
  same trick as the flex types). Diff/insert/update logic is reused.
- **Float path params** (`{lat}`, `{lon}`) → typed `float64` path fields.
- **`silent` / `suppressMessage` query flags** → typed bool/string query fields
  (kept lenient: presence-based, as today).
- **Routing coexistence**: `GET /humans/one/{id}` and `GET /humans/{id}` share
  a level; this already works on Gin and humagin registers via Gin, so the
  same router resolves it — verified by test, not assumed.
- **Two deps structs** in the humans group (`trackingDeps`, `roleDeps`); both
  are captured by the registration closures, no change to either.

## Testing

- **Handler tests** (`httptest` against the huma API): for each operation,
  assert the legacy envelope shape and that leniency holds — send `clean:false`,
  `clean:3`, `"min_iv":"90"`, `edit:true`, and an unknown field, asserting the
  collapse rule and acceptance.
- **OpenAPI golden test**: marshal the generated spec and compare to a
  committed `openapi.golden.json`; schema drift shows up in diffs and the spec
  is reviewable in PRs.
- **Error-path tests**: malformed body / missing required field → 422 with
  `{status:error,message}`; missing human → 404 same shape; bad secret → 401
  `authError` (exercises the Gin-middleware path in front of huma).
- **Gate**: `go build ./... && go vet ./... && go test -count=1 ./... &&
  golangci-lint run ./...` stays green.

## Out of scope (explicitly unchanged)

Webhook receiver `POST /`, `/health`, `/metrics`, geofence/tile/image
endpoints, `/api/dts/*`, `/api/config/*`, `/api/masterdata/*`,
`/api/snapshots/*`, `/api/autocreate/*`, `/api/command`, `/api/test`,
`/api/stats/*`, `/api/weather`, `/api/geocode/*`, `/api/deliverMessages`,
`/api/resolve`, `/api/reload`, `/api/geofence/reload`. The full-API switch to
huma and any client-side changes are future work.

## Risks & open items

- **huma validation ordering**: huma validates the parsed body against the
  schema before binding; the `SchemaProvider` `OneOf` is what keeps lenient
  forms from 422-ing. Confirm with a test sending `clean:false` against an
  integer-canonical-but-lenient field early in implementation (de-risks the
  whole approach).
- **`additionalProperties:true` mechanism**: confirm the cleanest way to set
  it per request struct in the installed huma version (struct-level option vs
  registry transformer); spike if needed.
- **Error model override surface**: `huma.NewError` is package-global; confirm
  the override doesn't leak into non-migrated huma usage (there is none today,
  so safe) and is set once at startup.
- **Audit completeness**: the per-field audit must cover all 10 tracking
  request structs plus humans/profiles bodies before the schemas are
  considered final; partial audit = inconsistent canonical typing.
