# Huma Full-API Master Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.
> **Execution:** build **all phases now (P0–P5)**. #138 implementor feedback is welcome but **non-blocking** — fold in any changes if they arrive. **Mega:** build v2 pokemon **without** `pvp_ranking_evolution` for now (it depends on the unmerged `pvp-mega-evolution` branch); add that one field as a follow-up after mega merges to `develop` and this branch rebases. Do **not** base this build on the mega branch (avoids carrying unmerged commits).
>
> **Build kickoff (fresh session):** "Execute this plan, **phases P0–P5**, using superpowers:subagent-driven-development. Companion specs: `docs/v2-api-design.md`, `docs/superpowers/specs/huma-tracking-field-audit.md`. Worktree `PoracleNG-huma-api`, branch `huma-api-migration`. Start at Task 0.1; Task 0.2 reverts the partial v1 pokemon huma migration before anything new is built."

**Goal:** Migrate the *entire* PoracleNG `/api` HTTP surface to huma in one coordinated effort: document the simple/new endpoints **in place** at `/api/*`, and deliver a **clean, strict `/api/v2`** for the tracking/humans/profiles CRUD — all in a single OpenAPI spec, with `problem+json` errors throughout.

**Architecture:** **One** huma API instance, mounted on the existing authenticated `/api` gin group via `humagin.NewWithGroup` (`api.NewHumaAPI`). Op paths are relative to `/api`: in-place ops use `/reload`, `/weather`, … (→ `/api/…`); v2 ops use `/v2/tracking/{type}`, `/v2/humans/{id}`, … (→ `/api/v2/…`). One spec at `/openapi.json`, one docs page at `/docs`. v1 tracking/humans/profiles stay on **gin, frozen, untouched**. Errors are RFC 9457 `problem+json` everywhere (the legacy `{status,message}` override is removed). Success bodies are per-op: in-place endpoints preserve their current success JSON; v2 endpoints are bare typed bodies.

**Tech Stack:** Go 1.26, gin + `humagin`, `huma/v2`, `net/http/httptest`.

**Companion docs (authoritative for detail):**
- v2 contract: `docs/v2-api-design.md` (+ RFC issue #138)
- v2 field semantics: `docs/superpowers/specs/huma-tracking-field-audit.md`
- In-place easy-wins detail: `docs/superpowers/plans/2026-06-03-huma-easy-wins-inplace.md` (its tasks are P1 here; **its error convention is superseded** — errors are `problem+json`, not `{status,message}`)

---

## Locked decisions (this is the "how it works")

1. **One huma instance**, mounted at `/api`; in-place ops at `/x`, v2 ops at `/v2/x`. One OpenAPI spec covering both.
2. **Errors: `problem+json` everywhere.** Remove `InstallLegacyErrorModel`/the legacy override; use huma's default error model. Success bodies unchanged per-op.
3. **v1 frozen.** Revert the in-place pokemon huma migration; restore original gin pokemon routes. Remove v1-compat huma machinery (`lenient[T]`, the flex `SchemaProvider` leniency, `monsterRuleRows`, single-or-array) — **not** needed by strict v2.
4. **In-place coverage:** all EASY (~30) + all MODERATE (~15, **including** `config/values`+`validate` with open `any`/`RawMessage` bodies). LEAVE-ON-GIN: webhook `POST /`, `/metrics`, `/openapi.json`, `/docs`, pprof.
5. **v2 = strict:** `additionalProperties:false`, required enforced, **no lenient coercion**. Enums are **pure string** (no legacy-int acceptance); game-master IDs are int. **Human-scoped** resource model `/v2/humans/{id}/tracking/{type}[/{uid}]`, item ops scoped by `(human, uid)` (ownership guard, like v1).
6. **v2 humans/profiles:** **discrete action endpoints** (not PATCH-consolidated), cleaned/typed, under `/api/v2`.
7. **CHANGELOG** items: error-format change to problem+json on the new huma surface; `include_empty` default→true; v1→v2 migration encouragement.

---

## API Surface Inventory (complete — for review)

Every one of the 124 registered routes is accounted for below. **Built in huma** = A (in-place) + B (v2). **Left on gin** = C (permanent) + D (frozen v1).

### A. BUILT IN HUMA — in-place at `/api/*` (same paths, same success JSON, `problem+json` errors)

- **Reloads (6):** `GET|POST /api/reload`, `GET|POST /api/geofence/reload`, `GET|POST /api/dts/reload`
- **Read-only data (12):** `GET /health`, `GET /api/weather`, `GET /api/stats/{rarity,shiny,shiny-possible}`, `GET /api/geocode/forward`, `GET /api/geofence/{all,all/hash,all/geojson}`, `GET /api/masterdata/{monsters,grunts}`, `GET /api/config/schema`, `GET /api/snapshots/{messageID}`
- **Geofence tile-URL (5):** `GET /api/geofence/{area}/map`, `GET /api/geofence/weatherMap/{lat}/{lon}`, `GET /api/geofence/locationMap/{lat}/{lon}`, `GET /api/geofence/distanceMap/{lat}/{lon}/{distance}`, `POST /api/geofence/overviewMap`
- **DTS editor (13):** `GET /api/dts/{emoji,templates,fields,fields/{type},partials,testdata,actions}`, `DELETE /api/dts/templates`, `PUT /api/dts/templates/file`, `POST /api/dts/{render,enrich,sendtest,templates}`
- **Config editor (5):** `GET /api/config/{poracleWeb,templates,values}`, `POST /api/config/{values,validate}` *(open bodies)*
- **Autocreate (6):** `POST /api/autocreate/run`, `GET /api/autocreate/{templates,templates/schema}`, `POST /api/autocreate/{templates,templates/validate}`, `DELETE /api/autocreate/templates/{name}`
- **Summaries (5):** `GET /api/summaries/{id}`, `GET /api/summaries/{id}/{alertType}`, `POST /api/summaries/{id}/{alertType}` *(typed `active_hours`)*, `POST /api/summaries/{id}/{alertType}/trigger`, `DELETE /api/summaries/{id}/{alertType}`
- **Other (5):** `POST /api/command`, `POST /api/test`, `POST /api/deliverMessages`, `POST /api/postMessage`, `POST /api/resolve`

### B. BUILT IN HUMA — new clean `/api/v2/*`

- **Tracking (11 types × CRUD), human-scoped:** `GET|POST /api/v2/humans/{id}/tracking/{type}`, `GET|PUT|DELETE /api/v2/humans/{id}/tracking/{type}/{uid}` (scoped by `(human, uid)` — ownership guard), bulk `DELETE …/{type}?uid=`, plus **full snapshot** `GET /api/v2/humans/{id}/tracking` → `{human, tracking:{<type>:[...]}, profiles, locations, summaries}` (`?all_profiles=`, `?include_descriptions=`). Types: `pokemon, raid, egg, quest, invasion, incident (NEW), lure, nest, gym, fort, maxbattle`.
- **Humans (discrete actions):** `POST /api/v2/humans`, `GET /api/v2/humans/{id}`, `GET …/{id}/areas`, `POST …/{id}/{enable,disable,admin-disable,language,location,areas,profile}`, `GET …/{id}/check-location`, locations `GET (list)`, `GET/{label}`, `POST`, **`PUT/{label}` (NEW)**, `DELETE/{label}`, roles `GET`, `POST/DELETE …/{roleId}`, `GET …/{id}/admin-roles`.
- **Profiles:** `GET /api/v2/profiles/{id}`, `POST` (add), `PATCH …/{profile_no}` (active_hours), `DELETE …/{profile_no}`, `POST …/{profile_no}/copy`.

### C. LEFT ON GIN — permanently (not a huma fit, by design)

| route | why |
|---|---|
| `POST /` | Golbat webhook receiver — hot path, unauthenticated, mixed-type array |
| `GET /metrics` | Prometheus text exposition |
| `GET /openapi.json`, `GET /docs` | huma's own spec/docs output |
| `GET /debug/pprof/`, `GET /debug/pprof/{name}` | Go pprof, binary/text |

### D. LEFT ON GIN — frozen v1 (superseded by `/api/v2`; gin until clients migrate, then deprecated)

- **Tracking v1 (all 10 types):** `GET|POST /api/tracking/{type}/{id}`, `DELETE …/{id}/byUid/{uid}`, `POST …/{id}/delete`, plus `GET /api/tracking/{all/{id},allProfiles/{id},pokemon/refresh}`.
- **Humans v1:** `POST /api/humans` (create), `GET /api/humans/{one/{id},{id},{id}/roles,{id}/getAdministrationRoles,{id}/checkLocation/{lat}/{lon},{id}/locations,{id}/locations/{label}}`, `POST /api/humans/{id}/{start,stop,adminDisabled,language,switchProfile/{profile},setLocation/{lat}/{lon},setAreas,roles/add/{roleId},roles/remove/{roleId},locations/add,locations/{label}/delete}`.
- **Profiles v1:** `GET /api/profiles/{id}`, `POST /api/profiles/{id}/{add,update,copy/{from}/{to}}`, `DELETE /api/profiles/{id}/byProfileNo/{profile_no}`.

> **Note on `/api/tracking/pokemon/refresh`:** it's a reload alias living under the frozen `/api/tracking` namespace. Kept on gin (D) to avoid splitting that namespace; the documented reload is the huma `GET /api/reload` (A).

**Coverage confirmation:** A (≈52) + B (new v2) + C (6) + D (≈45 frozen) accounts for all 124 registered routes. Nothing is unclassified.

---

## Phase 0 — Foundation rework

### Task 0.1: Switch error model to problem+json
**Files:** `internal/api/huma_setup.go`, `huma_setup_test.go`, any test asserting `{status:error,message}`.
- [ ] Remove `InstallLegacyErrorModel` (and its call in `NewHumaAPI`); delete `legacyError`/`humaNewError` OR repoint `humaNewError` to `huma.NewError` so call sites compile. Handlers return errors via `huma.Error404NotFound(...)` etc. (huma's typed constructors).
- [ ] Update/replace tests that asserted the legacy envelope to assert `problem+json` (`status`, `detail`, `errors[]`; no `{status:"error"}`).
- [ ] Keep the `$schema`-suppression (`cfg.CreateHooks = nil`) — still wanted.
- [ ] Gate + commit `refactor(api): problem+json error model for the huma surface`.

### Task 0.2: Revert in-place pokemon migration (freeze v1)
**Files:** `main.go`, `huma_tracking.go`, `huma_post_monster*.go`, `huma_delete_monster*.go`, `tracking.go`.
- [ ] Restore the gin routes for `GET/POST/DELETE /tracking/pokemon/...` + bulk in `main.go` (the original `api.HandleGetMonster` etc. still exist in `trackingMonster.go`).
- [ ] Remove the huma pokemon ops + v1-compat machinery: `monsterRuleRows`, `lenient[T]`, the flex `SchemaProvider` methods on `flexInt`/`flexBool`, `collapseClean` (re-add in v2 if needed), and now-unused helpers. Keep `flexInt`/`flexBool` themselves (still used by gin v1).
- [ ] Remove the temporary `flex_enum.go` lint exclusion plan (the enum toolkit is reworked in P3).
- [ ] Gate + commit `refactor(api): revert in-place pokemon huma migration (v1 frozen)`.

### Task 0.3: Confirm single-instance dual-path mount
- [ ] Add a test: register one trivial in-place op (`/ping`) and one v2 op (`/v2/ping`) on the same `NewHumaAPI`, assert both serve and both appear in `OpenAPI().MarshalJSON()`. Confirms the one-instance/two-path-prefix model. Gate + commit.

---

## Phase 1 — In-place EASY endpoints (~30)

Execute the tasks in `docs/superpowers/plans/2026-06-03-huma-easy-wins-inplace.md` (Tasks 1–7), with these amendments: errors are `problem+json` (Task 0.1), so drop the legacy-error notes; register on the shared instance. Clusters: reloads, read-only data (health/stats/geocode/geofence-reads/masterdata/config-schema/snapshots), tile-URL, DTS reads, feature endpoints (autocreate/run, summaries GET/DELETE/trigger, command). Worked examples (reload, weather) are in that doc.
- [ ] Complete easy-wins Tasks 1–6 (per-cluster commits).
- [ ] Easy-wins Task 7 golden test folded into the master golden test (P5).

## Phase 2 — In-place MODERATE endpoints (~15)

Open schemas for freeform fields. Each: typed input for path/query, `Body json.RawMessage` or `Body any` for the freeform part, reuse handler logic, remove gin route, test (parse boundary + success shape), commit per group.

| endpoint | freeform part | source |
|---|---|---|
| `POST /test` | `webhook` RawMessage | `HandleTest` |
| `POST /dts/render` | `view` map; resp `message` any | render handler |
| `POST /dts/enrich` | `webhook` RawMessage | enrich handler |
| `POST /dts/sendtest` | `template` any, `variables` map | sendtest handler |
| `POST /dts/templates` | `[]DTSEntry` (polymorphic `template`) | save handler |
| `POST /deliverMessages` + `POST /postMessage` | `[]delivery.Job` (`Message` RawMessage) | deliver handler |
| `POST /resolve` | nested optional + per-entity `any` | resolve handler |
| `POST /summaries/{id}/{alertType}` | **typed** `active_hours` (`[]ActiveHourEntry`, see design §2b) — NOT freeform; v1 already validates this shape via `ParseActiveHours` | upsert handler |
| `GET/POST /autocreate/templates`, `POST …/validate` | raw-JSON templates | autocreate template handlers |
| `GET /config/templates`, `GET /config/poracleWeb` | dynamic-keyed map → `Body any` | config handlers |
| `GET/POST /config/values`, `POST /config/validate` | reflection `map[string]any` → open body/resp | config handlers |

- [ ] Per group: TDD port with open schemas, remove gin route, gate + commit `feat(api): huma in-place for <group>`.
- [ ] Document in the spec that these bodies are intentionally open (`description` noting the freeform contract).

## Phase 3 — v2 tracking

Strict, per `docs/v2-api-design.md` + the field audit. Resource model: **human-scoped** `/v2/humans/{id}/tracking/{type}` (GET list `?profile=&include_descriptions=`, POST create), `/v2/humans/{id}/tracking/{type}/{uid}` (GET/PUT/DELETE, **scoped by `(human, uid)`** — like v1's `DeleteByUID(id, uid)`), `?uid=` bulk delete; `?silent=true` on mutations.

### Task 3.1: Strict v2 building blocks
- [ ] **Strict enum types** — rework/parallel `flex_enum.go`: v2 enums are **string-only** (no int acceptance), `additionalProperties:false`-compatible. Keep the name↔int maps for storage translation. (team, gender, fort_type, rsvp_changes; reward_type/lure_id/league/pvp_ranking_evolution stay **int**.)
- [ ] **Strict request structs** — real `bool`/`int`/string-enum fields; `clean`/`edit`/`summary` bools → packed `clean` column; required `pokemon_id` etc.; `additionalProperties:false`.
- [ ] **Resource helpers** — human-scoped addressing (`{id}` path), `(human, uid)` ownership scoping on item ops, `profile`/`include_descriptions`/`silent` query binding; list → `{rules:[…]}`; create → `{created,updated,unchanged}` (delete → `{deleted}`) with uids. **`?include_descriptions=true` is uniform across reads AND mutations**: when set, each rule in the response (rules/created/updated/unchanged/deleted) gets a `description` (human's language). No assembled `message` field — status is the array placement; the prefixed confirmation message stays the Discord/Telegram push (gated by `silent`). Reuse the rowtext generator + `translatorFor`.
- [ ] Tests for the building blocks; gate + commit.

### Task 3.2: pokemon v2 (worked example) — GET list, POST create, GET/PUT/DELETE by uid, bulk delete, full snapshot. Faithful to the engine; strict schemas. **Omit `pvp_ranking_evolution`** (depends on the unmerged mega branch) — add it in a follow-up once `pvp-mega-evolution` is in `develop` and this branch rebases. Commit.

### Task 3.3: Fan-out the other 10 types (raid, egg, quest, invasion, **incident**, lure, nest, gym, fort, maxbattle)
- [ ] Per type: apply the audit's per-field modeling; **invasion** exactly-one-mode (`type_id`|`grunt_id`|`everything`|`boss`) with facade down-translation to the stored grunt-type name; **incident** new type keyed by `display_type` int; `fort.include_empty` default true. One commit per type.

### Task 3.4: v2 full snapshot — `GET /v2/humans/{id}/tracking` returns `{human, tracking:{<type>:[...]}, profiles, locations, summaries}` (replaces v1 `all/{id}`); `?all_profiles=true` spans all profiles (replaces `allProfiles/{id}`); `?include_descriptions=` adds rowtext. Reuses the per-type list logic + profile/location/summary reads. Commit.

## Phase 4 — v2 humans/profiles

**Discrete action endpoints**, cleaned/typed, under `/api/v2`. Mirror v1's actions with proper types + problem+json + strict bodies. Reuse the store/business logic.

| v2 endpoint | from v1 | shape |
|---|---|---|
| `POST /v2/humans` | create | typed body (id,type,name,…) |
| `GET /v2/humans/{id}` | one/{id} | typed human resource |
| `GET /v2/humans/{id}/areas` | `/{id}` | available areas |
| `POST /v2/humans/{id}/enable` / `/disable` | start/stop | no body |
| `POST /v2/humans/{id}/admin-disable` | adminDisabled | `{disabled: bool}` |
| `POST /v2/humans/{id}/language` | language | `{language: string}` |
| `POST /v2/humans/{id}/location` | setLocation/{lat}/{lon} | `{lat,lon}` floats body |
| `GET /v2/humans/{id}/check-location` | checkLocation | `?lat=&lon=` |
| `POST /v2/humans/{id}/areas` | setAreas | `{areas: []string}` |
| `GET/POST /v2/humans/{id}/locations`, **`PUT …/{label}`** (NEW — update coords), `DELETE …/{label}` | locations CRUD | typed `{label,lat,lon}` |
| `GET /v2/humans/{id}/roles`, `POST/DELETE …/{roleId}` | roles | typed |
| `GET /v2/humans/{id}/admin-roles` | getAdministrationRoles | typed |
| `POST /v2/humans/{id}/profile` | switchProfile/{n} | `{profile_no: int}` |
| `GET /v2/humans/{id}/profiles`, `POST` (add), `PATCH …/{profile_no}` (update active_hours), `DELETE …/{profile_no}`, `POST …/{profile_no}/copy` | profiles (sub-resource of human) | typed |

- [ ] Field modeling (all DEFINED — see `docs/v2-api-design.md` §2b): `enabled`/admin-disable → bool; `areas` → `[]string`; `location` → `{lat,lon}` floats; `language` → string (validate against locales); `blocked_alerts` → read-only `[]string` enum (`monster|pvp|raid|egg|quest|invasion|lure|nest|gym|fort|maxbattle|specificgym|specificstation`); `active_hours` → typed `[]ActiveHourEntry` (`day 0-6, hours 0-23, mins 0-59, optional step/end_hours/end_mins`, strict ints, no cross-midnight) shared by profile-schedule update **and** `POST /v2/summaries/{id}/{alertType}` (replaces the freeform passthrough).
- [ ] **NEW capability:** `PUT /v2/humans/{id}/locations/{label}` to update a saved location's coords (v1 has no update — only add/delete). Completes locations CRUD.
- [ ] Per cluster (status, location/areas, locations, roles, profiles, schedules): TDD, reuse handlers (add a small store method for the new locations PUT), commit.

## Phase 5 — Finalize

- [ ] **Golden OpenAPI test** over the whole spec (in-place + v2), committed `testdata/openapi.golden.json`.
- [ ] **Remove dead code** — any now-unused v1-compat helpers; confirm no orphaned gin handlers for migrated in-place endpoints; lint clean (remove temporary exclusions).
- [ ] **Docs** — README/CLAUDE.md: the `/api` surface and `/api/v2` are documented at `/docs`; note the migrated endpoints, the v1-frozen status, and the v1→v2 encouragement.
- [ ] **CHANGELOG** — problem+json on the huma surface; `include_empty` default→true; new v2 surface + `incident` type.

---

## Self-review
- **Coverage:** every endpoint from the triage is assigned — EASY (P1), MODERATE incl config/values (P2), v2 tracking incl incident (P3), v2 humans/profiles discrete actions (P4), LEAVE-ON-GIN explicitly excluded. v1 frozen via P0.2.
- **Decision fidelity:** problem+json everywhere (P0.1, supersedes easy-wins legacy note); one instance/two path prefixes (P0.3); discrete humans/profiles actions (P4); max in-place coverage (P2).
- **Gating:** P0–P2 independent; P3–P4 wait on #138 — flagged at top and per-phase.
- **Detail strategy:** worked examples live in the companion docs (easy-wins reload/weather; pokemon v2 in 3.2); fan-outs are delta tables driven by the audit — consistent with the prior plans' approach.
- **Open items to finalize at build time:** strict-enum reuse vs rework of `flex_enum.go` (3.1); `active_hours`/`blocked_alerts` shapes (P4); any #138 resource-shape feedback (P3).
