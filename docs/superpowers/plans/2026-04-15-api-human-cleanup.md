# Retire remaining db.HumanAPI / db.SelectProfiles API consumers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the HumanStore migration for the remaining API handlers — `api/tracking.go`, `api/trackingAll.go`, `api/profiles.go`, `api/roles.go` — so no code outside `processor/internal/store/` calls `db.SelectOneHuman`, `db.SelectProfiles`, `db.HumanAPI`, `db.UpdateHumanEnabled`, `db.UpdateHumanAdminDisable`, `db.SwitchProfile`, `db.UpdateHumanLocation`, or `db.UpdateHumanAreas`. Those legacy helpers are then deleted.

**Architecture:** The central `lookupHuman` helper in `api/tracking.go` is hit on every tracking-CRUD request — ~30 handlers share it. To avoid parsing JSON-array columns (`area`, `community_membership`, `area_restriction`, `blocked_alerts`) on every hot-path lookup, add a `HumanStore.GetLite(id)` method returning a new lightweight `HumanLite` struct (id, type, name, enabled, admin_disable, current_profile_no). Cold-path sites (roles, profiles) use `Get` for simplicity. A new `api.ProfileResponse` DTO preserves the current JSON wire shape for `GET /api/profiles/{id}` and `GET /api/tracking/allProfiles/{id}`.

**Out of scope:** `db.CopyProfile` and `db.UpdateProfileHours` stay in the `db` package for now — they belong on the store but moving them is a separate focused commit after this lands.

**Tech Stack:** Go 1.22+, sqlx. Existing test framework.

## File Structure

Files modified:
- `processor/internal/store/human.go` — add `HumanLite` struct + `GetLite` method to the interface
- `processor/internal/store/human_sql.go` — implement `GetLite` against `humanRowColumns`-lite
- `processor/internal/store/mock_human.go` — add `GetLite` to the mock
- `processor/internal/api/tracking.go` — `lookupHuman` uses `GetLite`; signature changes from `*db.HumanAPI` to `*store.HumanLite`
- `processor/internal/api/trackingAll.go` — replace `db.SelectProfiles` with `humanStore.GetProfiles`; use `ProfileResponse` DTO
- `processor/internal/api/profile_response.go` — NEW DTO file, mirrors legacy ProfileRow JSON
- `processor/internal/api/profiles.go` — all 4 `db.SelectOneHuman` + 1 `db.SelectProfiles` sites → store
- `processor/internal/api/roles.go` — 3 `db.SelectOneHuman` sites → store (uses `Get`)
- `processor/internal/db/human_queries.go` — DELETE `HumanAPI`, `SelectOneHuman`, `SelectProfiles`, `UpdateHumanEnabled`, `UpdateHumanAdminDisable`, `SwitchProfile`, `UpdateHumanLocation`, `UpdateHumanAreas`

Shape reference (HumanAPI → HumanLite):

| Field | `db.HumanAPI` | `store.HumanLite` |
|-------|---------------|-------------------|
| `ID` | string | string |
| `Type` | string | string |
| `Name` | string | string |
| `Enabled` | int | bool |
| `AdminDisable` | int | bool |
| `CurrentProfileNo` | int | int |

Consumers of `lookupHuman` currently read `human.ID`, `human.CurrentProfileNo`, `human.Type`, `human.Name` — no int-as-bool comparisons downstream. The type change is low-risk.

Shape reference (ProfileRow → ProfileResponse):

Legacy `db.ProfileRow` JSON (with `json:"..."` tags): `uid`, `id`, `profile_no`, `name`, `area` (raw JSON string), `latitude`, `longitude`, `active_hours`.

`store.Profile` is typed (`Area []string`). `ProfileResponse` mirrors the legacy shape: `Area` as a JSON-encoded string for wire compatibility.

---

## Task 1: Add `HumanLite` + `GetLite` to HumanStore

**Files:**
- Modify: `processor/internal/store/human.go`
- Modify: `processor/internal/store/human_sql.go`
- Modify: `processor/internal/store/mock_human.go`

- [ ] **Step 1: Add `HumanLite` struct and `GetLite` method signature to the interface**

In `processor/internal/store/human.go`, after the `Human` struct:

```go
// HumanLite is a minimal projection of the humans table used on hot paths
// (tracking CRUD handlers) where only identity and profile selection matter.
// Skips the JSON-column parsing that Get does for Area, CommunityMembership,
// AreaRestriction, and BlockedAlerts.
type HumanLite struct {
	ID               string
	Type             string
	Name             string
	Enabled          bool
	AdminDisable     bool
	CurrentProfileNo int
}
```

Add to `HumanStore` interface (near `Get`):

```go
// GetLite returns identity + profile fields for a human by ID without
// parsing any JSON columns. Cheaper than Get for hot-path handlers that
// only need ID / CurrentProfileNo / enable state.
GetLite(id string) (*HumanLite, error)
```

- [ ] **Step 2: Implement `GetLite` on `SQLHumanStore`**

In `processor/internal/store/human_sql.go`, near `Get`:

```go
// humanLiteColumns is the column list for HumanLite. Explicit so operator-
// added columns don't break scans.
const humanLiteColumns = `id, type, name, enabled, admin_disable, current_profile_no`

type humanLiteRow struct {
	ID               string `db:"id"`
	Type             string `db:"type"`
	Name             string `db:"name"`
	Enabled          int    `db:"enabled"`
	AdminDisable     int    `db:"admin_disable"`
	CurrentProfileNo int    `db:"current_profile_no"`
}

func (s *SQLHumanStore) GetLite(id string) (*HumanLite, error) {
	var r humanLiteRow
	err := s.db.Get(&r, `SELECT `+humanLiteColumns+` FROM humans WHERE id = ?`, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("select human lite %s: %w", id, err)
	}
	return &HumanLite{
		ID:               r.ID,
		Type:             r.Type,
		Name:             r.Name,
		Enabled:          r.Enabled != 0,
		AdminDisable:     r.AdminDisable != 0,
		CurrentProfileNo: r.CurrentProfileNo,
	}, nil
}
```

- [ ] **Step 3: Implement `GetLite` on the mock**

In `processor/internal/store/mock_human.go`, find the existing `Get` mock and add a parallel `GetLite`:

```go
func (m *MockHumanStore) GetLite(id string) (*HumanLite, error) {
	h, err := m.Get(id)
	if err != nil || h == nil {
		return nil, err
	}
	return &HumanLite{
		ID:               h.ID,
		Type:             h.Type,
		Name:             h.Name,
		Enabled:          h.Enabled,
		AdminDisable:     h.AdminDisable,
		CurrentProfileNo: h.CurrentProfileNo,
	}, nil
}
```

- [ ] **Step 4: Verify build + tests**

```bash
cd processor && go build ./... && go test ./internal/store/...
```
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/store/human.go \
        processor/internal/store/human_sql.go \
        processor/internal/store/mock_human.go
git commit -m "Add HumanStore.GetLite for hot-path lookups

Lightweight human lookup that skips JSON-column parsing — used by the
tracking CRUD lookupHuman helper, hit on every POST /api/tracking/*."
```

---

## Task 2: Migrate `api/tracking.go` lookupHuman to GetLite

**Files:**
- Modify: `processor/internal/api/tracking.go` (line 37-62 region — `lookupHuman`)

Central chokepoint. `lookupHuman` is called by every tracking CRUD handler (pokemon, raid, egg, quest, invasion, lure, nest, gym, fort, maxbattle — 10 types × ~3 handlers each). Signature change from `*db.HumanAPI` to `*store.HumanLite` ripples through every caller.

- [ ] **Step 1: Read the current lookupHuman and its call sites**

```bash
grep -rn 'lookupHuman' processor/internal/api/ --include='*.go'
```

Note every file that calls it. The return type changes, so every caller that reads fields on the returned pointer needs checking.

- [ ] **Step 2: Change lookupHuman signature and body**

```go
func lookupHuman(deps *TrackingDeps, c *gin.Context) (*store.HumanLite, int, error) {
	id := c.Param("id")
	if id == "" {
		return nil, 0, fmt.Errorf("missing id parameter")
	}

	human, err := deps.Humans.GetLite(id)
	if err != nil {
		return nil, 0, fmt.Errorf("lookup human: %w", err)
	}
	if human == nil {
		return nil, 0, nil
	}

	profileNo := human.CurrentProfileNo
	if pq := c.Query("profile_no"); pq != "" {
		if n, err := strconv.Atoi(pq); err == nil {
			profileNo = n
		}
	}

	return human, profileNo, nil
}
```

Add the `store` import if not present. Remove the `db` import if no longer used.

- [ ] **Step 3: Fix any downstream field access**

Grep each caller file for `human.Enabled`, `human.AdminDisable` — those are now `bool` not `int`. In most files, callers only read `human.ID` / `human.CurrentProfileNo` / `human.Type` / `human.Name` — no change needed.

- [ ] **Step 4: Verify build + full tests**

```bash
cd processor && go build ./... && go test ./...
```
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/api/tracking.go
git commit -m "Route lookupHuman through humanStore.GetLite"
```

---

## Task 3: Define `api.ProfileResponse` DTO

**Files:**
- Create: `processor/internal/api/profile_response.go`

Preserves the legacy JSON shape (`uid`, `id`, `profile_no`, `name`, `area` as JSON-string, `latitude`, `longitude`, `active_hours`) when API handlers return profiles built from `store.Profile`.

- [ ] **Step 1: Write the file**

```go
package api

import "github.com/pokemon/poracleng/processor/internal/store"

// ProfileResponse is the JSON shape returned by /api/profiles/* endpoints
// and by tracking responses that include profile info. Mirrors the legacy
// db.ProfileRow JSON layout so existing clients (PoracleWeb) continue to
// receive the same wire format after the internal migration to store.Profile.
type ProfileResponse struct {
	UID         int     `json:"uid"`
	ID          string  `json:"id"`
	ProfileNo   int     `json:"profile_no"`
	Name        string  `json:"name"`
	Area        string  `json:"area"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	ActiveHours string  `json:"active_hours"`
}

// profileToResponse converts a typed store.Profile into the legacy API
// response shape.
func profileToResponse(p store.Profile) ProfileResponse {
	return ProfileResponse{
		UID:         p.UID,
		ID:          p.ID,
		ProfileNo:   p.ProfileNo,
		Name:        p.Name,
		Area:        stringSliceToJSON(p.Area),
		Latitude:    p.Latitude,
		Longitude:   p.Longitude,
		ActiveHours: p.ActiveHours,
	}
}

// profilesToResponse converts a slice of store.Profile to a slice of DTOs.
func profilesToResponse(profiles []store.Profile) []ProfileResponse {
	out := make([]ProfileResponse, len(profiles))
	for i, p := range profiles {
		out[i] = profileToResponse(p)
	}
	return out
}
```

Note: `stringSliceToJSON` already exists in `api/human_response.go` — reuse it.

- [ ] **Step 2: Verify build**

```bash
cd processor && go build ./...
```
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add processor/internal/api/profile_response.go
git commit -m "Add api.ProfileResponse DTO for wire compat"
```

---

## Task 4: Migrate `api/profiles.go`

**Files:**
- Modify: `processor/internal/api/profiles.go` (lines 23, 34, 82, 163, 236 — 5 sites total)

4 `db.SelectOneHuman` lookups + 1 `db.SelectProfiles` call. Use `humanStore.Get` for existence checks (cold path, JSON parse cost acceptable) and `humanStore.GetProfiles` for profile listing, adapting through `profilesToResponse`.

- [ ] **Step 1: Replace HandleGetProfiles (lines 15-43)**

```go
func HandleGetProfiles(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Profiles API: lookup human: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		profiles, err := deps.Humans.GetProfiles(id)
		if err != nil {
			log.Errorf("Profiles API: get profiles: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		trackingJSONOK(c, map[string]any{"profile": profilesToResponse(profiles)})
	}
}
```

- [ ] **Step 2: Replace HandleAddProfile existence check (line 82)**

```go
human, err := deps.Humans.Get(id)
```

- [ ] **Step 3: Replace HandleUpdateProfile lookup (line 163)**

Same pattern — `deps.Humans.Get(id)`.

- [ ] **Step 4: Replace HandleCopyProfile lookup (line 236)**

Same pattern.

- [ ] **Step 5: Drop the `db` import if now unused, or confirm it is still needed for trackingTables etc.**

Run `grep -n 'db\.' processor/internal/api/profiles.go` — if empty, remove the import.

- [ ] **Step 6: Verify build + tests**

```bash
cd processor && go build ./... && go test ./internal/api/...
```
Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/api/profiles.go
git commit -m "Route api/profiles.go through humanStore"
```

---

## Task 5: Migrate `api/trackingAll.go` profiles call

**Files:**
- Modify: `processor/internal/api/trackingAll.go` (line 391)

One `db.SelectProfiles` call in `HandleGetAllProfilesTracking`. Swap to `humanStore.GetProfiles` and wrap with `profilesToResponse`.

- [ ] **Step 1: Replace the SelectProfiles call**

```go
if profiles, err := deps.Humans.GetProfiles(id); err == nil {
	result["profiles"] = profilesToResponse(profiles)
}
```

(Match the existing `if profiles, err := ...; err == nil` pattern and verify the response key — confirm by reading a few lines above/below.)

- [ ] **Step 2: Verify build + tests**

```bash
cd processor && go build ./... && go test ./internal/api/...
```
Expected: pass.

- [ ] **Step 3: Commit**

```bash
git add processor/internal/api/trackingAll.go
git commit -m "Route trackingAll.go profiles through humanStore"
```

---

## Task 6: Migrate `api/roles.go`

**Files:**
- Modify: `processor/internal/api/roles.go` (lines 45, 102, 162 — 3 sites)

Simple existence checks. All three swap to `deps.Humans.Get(id)`.

- [ ] **Step 1: Replace all three `db.SelectOneHuman` calls**

`sed -i '' 's|db\.SelectOneHuman(deps\.DB, id)|deps.Humans.Get(id)|g' internal/api/roles.go`

Then verify with grep that none remain: `grep -n 'db\.SelectOneHuman' internal/api/roles.go` → empty.

- [ ] **Step 2: Drop `db` import if unused**

`grep -n 'db\.' internal/api/roles.go` — remove the import line if empty.

- [ ] **Step 3: Verify build + tests**

```bash
cd processor && go build ./... && go test ./internal/api/...
```
Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add processor/internal/api/roles.go
git commit -m "Route api/roles.go through humanStore"
```

---

## Task 7: Delete legacy db helpers

**Files:**
- Modify: `processor/internal/db/human_queries.go` — delete `HumanAPI`, `SelectOneHuman`, `SelectProfiles`, `UpdateHumanEnabled`, `UpdateHumanAdminDisable`, `SwitchProfile`, `UpdateHumanLocation`, `UpdateHumanAreas`

- [ ] **Step 1: Verify zero references to each symbol**

```bash
cd processor
grep -rn 'db\.HumanAPI\b' --include='*.go' .
grep -rn 'db\.SelectOneHuman\b' --include='*.go' .
grep -rn 'db\.SelectProfiles\b' --include='*.go' .
grep -rn 'db\.UpdateHumanEnabled\b' --include='*.go' .
grep -rn 'db\.UpdateHumanAdminDisable\b' --include='*.go' .
grep -rn 'db\.SwitchProfile\b' --include='*.go' .
grep -rn 'db\.UpdateHumanLocation\b' --include='*.go' .
grep -rn 'db\.UpdateHumanAreas\b' --include='*.go' .
```

Each must return no output (or only internal db/ matches being deleted). Fix any stray consumer first.

- [ ] **Step 2: Delete the five functions and HumanAPI struct from `db/human_queries.go`**

Read the current file. Delete:
- `type HumanAPI struct { ... }` and any helpers
- `func SelectOneHuman`
- `func SelectProfiles`
- `func UpdateHumanEnabled`
- `func UpdateHumanAdminDisable`
- `func SwitchProfile`
- `func UpdateHumanLocation`
- `func UpdateHumanAreas`

Keep `ProfileRow`, `trackingTables`, `CopyProfile`, `UpdateProfileHours`, `DeleteHumanAndTracking`. These are the "follow-up refactor" scope.

- [ ] **Step 3: Verify build + full test suite**

```bash
cd processor && go build ./... && go test ./...
```
Expected: every package passes.

- [ ] **Step 4: Commit**

```bash
git add processor/internal/db/human_queries.go
git commit -m "Delete db.HumanAPI / SelectOneHuman and related helpers

After routing api/tracking.go, api/trackingAll.go, api/profiles.go,
and api/roles.go through humanStore, the legacy HumanAPI-shape
helpers and the redundant UpdateHuman* wrappers have no callers."
```

---

## Task 8: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (HumanStore boundary section)

- [ ] **Step 1: Tighten the claim**

Change the existing note from "All `humans` and `profiles` table access outside the store implementation goes through the `store.HumanStore` interface" to reflect completeness, and mention `CopyProfile` / `UpdateProfileHours` as remaining `db/` residents for a future commit.

```markdown
### HumanStore boundary

All `humans` and `profiles` table reads and mutations outside the store
implementation go through `store.HumanStore` (`processor/internal/store/human.go`).
The interface includes `Get` for full records, `GetLite` for hot-path
identity-only lookups, `ListByType{,Enabled,s}` for bulk queries,
`GetProfiles` / `SwitchProfile` / `AddProfile` / `DeleteProfile` for
profile management, and per-field setters (`SetEnabled`, `SetArea`, etc.)
plus a dynamic `Update(id, map)` escape hatch.

Two concessions in `db/human_queries.go` remain for a follow-up commit:
`CopyProfile` (cross-table tracking copy) and `UpdateProfileHours` —
both trivially movable once someone cares to make the corresponding
store methods.

API handlers serialise through DTOs to preserve the legacy JSON wire
format: `api.HumanResponse` for humans, `api.ProfileResponse` for
profiles. `humanToResponse` / `profileToResponse` adapt at the API
boundary.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "CLAUDE.md: expand HumanStore boundary documentation"
```

---

## Self-Review

Spec coverage: every remaining `db.SelectOneHuman` / `db.SelectProfiles` / `db.UpdateHuman*` / `db.SwitchProfile` / `db.HumanAPI` reference named in the review is addressed in Tasks 2–6. Task 1 (GetLite) is required before Task 2 can land. Task 7 (deletion) depends on 2–6 being complete.

Placeholders: every step contains the actual code to write or the exact command to run. No "TBD" or "similar to Task N".

Type consistency: `HumanLite` fields are used the same way in every reference. `ProfileResponse.Area` is JSON-string throughout. `lookupHuman` signature change propagates cleanly because downstream callers only read `ID`, `CurrentProfileNo`, `Type`, `Name`.

Known deferral: `CopyProfile` and `UpdateProfileHours` are acknowledged out-of-scope in the plan header and CLAUDE.md update; they are a follow-up.
