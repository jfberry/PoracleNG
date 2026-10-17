# v2 Set-Areas Profile Operations (#252, #253) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `?all_profiles=` doc string describe the whole-list replace it actually performs (#252). Add `DELETE /api/v2/humans/{id}/areas?name=…` to remove one area name from each targeted profile's own list, atomically (#253).

**Architecture:** #252 changes doc strings only. #253 adds one `HumanStore` method that runs as a single transaction, plus one huma operation. The operation is a removal, so it needs no permission or existence check: removing a name can never widen what a human receives, and the fence has usually already been deleted, which is the whole point.

**Tech Stack:** Go 1.27, huma v2 on gin, sqlx/MySQL.

**Spec:** GitHub issues jfberry/PoracleNG#252 and #253 (`gh issue view 252`, `gh issue view 253`). Both were split out of #243, and neither depends on it.

## Global Constraints

- Separate branch and PR from #243. Base it on `main` **after** #243 merges: both touch `internal/api/v2_humans.go` and `testdata/openapi.golden.json`. If it has to go first, expect to regenerate the golden on rebase.
- The main checkout has an unrelated staged index. Work in a worktree (`git worktree add .worktrees/252-253-area-profile-ops -b feat/252-253-area-profile-ops main`).
- `feat/v2-location-clear-max-distance` (unmerged) changes `SetArea` so that `humans.area` is written only when `profileNo == current_profile_no`. The new store method follows that same rule, so the two branches agree whichever merges first.
- Lint with the CI-pinned binary: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1`. Gate from `processor/` before every commit:
  `go build ./... && go vet ./... && go test -count=1 ./... && "$(go env GOPATH)/bin/golangci-lint" run --config ../.golangci.yml ./...`
- Trap: `MockHumanStore` has diverged from `SQLHumanStore` twice. Run the SQL test with `PORACLENG_TEST_DSN` set (see the #243 plan, Task 0 Step 4). CI skips it.

## Review Focus

1. **A name held in a different case or spelling.** `humans.area` is lowercased with underscores intact. Expected: `?name=Gent_Centrum` and `?name=gent centrum` both remove `gent_centrum`. Pinned in Task 2.
2. **A non-current profile is targeted.** Expected: the live `humans.area` is untouched, because it mirrors the current profile only. Pinned in Task 2.
3. **The name is held by no profile.** Expected: 200 with `removed_from: []`, not 404, because the request is idempotent. Pinned in Task 3.
4. **A failure part-way.** Expected: no profile is changed. This is the reason for the transaction. It is not unit-testable without fault injection, so verify it by code review: every write goes through `tx`, and the deferred rollback runs on any error.
5. **`all_profiles` together with `profile`.** Expected: 422, as on POST. Pinned in Task 3.

---

### Task 1: #252: say what `?all_profiles=` does

**Files:**
- Modify: `processor/internal/api/v2_humans.go` (`v2SetAreasInput.AllProfiles` doc tag; `registerV2HumanSetAreas` `Description`)
- Modify: `processor/internal/api/testdata/openapi.golden.json` (regenerated)

- [ ] **Step 1:** Replace the `AllProfiles` doc tag with:

```
doc:"Replace the areas on EVERY profile the human owns with the submitted list. This is a whole-list replace, not a per-name removal: profiles that currently hold different areas all end up with this list. To remove one area from every profile, use DELETE /api/v2/humans/{id}/areas?name=…&all_profiles=true. Mutually exclusive with profile."
```

In the operation `Description`, replace "`?all_profiles=true` writes every profile the human owns" with "`?all_profiles=true` replaces every profile's areas with the submitted list (a whole-list replace — to drop one name everywhere use DELETE …/areas?name=)".

- [ ] **Step 2:** `UPDATE_GOLDEN=1 go test ./internal/api/ -run Golden -count=1`, then check that `git diff` on the golden shows only these two strings. Run the gate.

- [ ] **Step 3: Commit** — `docs(api): ?all_profiles= on set-areas is a whole-list replace (#252)`.

(This step references the DELETE operation that Task 3 adds. Both land in the same PR, so the doc never points at something missing.)

---

### Task 2: `HumanStore.RemoveAreaFromProfiles`

**Files:**
- Modify: `processor/internal/store/human.go`, `human_sql.go`, `mock_human.go`
- Test: `processor/internal/store/mock_human_test.go`, `processor/internal/store/user_locations_sql_test.go` (the `profiles` table is added to `openTestDB` by #243 Task 6; if this branch somehow precedes #243, add the same `CREATE TABLE IF NOT EXISTS profiles` and cleanup here)

**Interfaces:**
- Produces: `RemoveAreaFromProfiles(id, name string, profileNos []int) (removedFrom []int, err error)`. In one transaction, it removes every stored area matching `name` from each listed profile row, and from `humans.area` when the human's `current_profile_no` is among `profileNos`. A stored area matches when `strings.ToLower(stored) == strings.ToLower(name)` or `geofence.NormalizeAreaKey(stored) == geofence.NormalizeAreaKey(name)`. It returns the profile numbers whose row changed, in ascending order. Other areas are left in place and keep their order.

- [ ] **Step 1: Write the failing mock test**

```go
func TestMockHumanStore_RemoveAreaFromProfiles(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "u1", CurrentProfileNo: 1, Area: []string{"alpha", "gent_centrum"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha", "gent_centrum"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 2, Area: []string{"gent_centrum", "beta"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 3, Area: []string{"beta"}})

	// Different spelling of the same fence.
	got, err := m.RemoveAreaFromProfiles("u1", "Gent Centrum", []int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int{1, 2}) {
		t.Errorf("removedFrom = %v, want [1 2]", got)
	}
	h, _ := m.Get("u1")
	if !slices.Equal(h.Area, []string{"alpha"}) {
		t.Errorf("humans.area = %v, want [alpha]", h.Area)
	}
	ps, _ := m.GetProfiles("u1")
	want := map[int][]string{1: {"alpha"}, 2: {"beta"}, 3: {"beta"}}
	for _, p := range ps {
		if !slices.Equal(p.Area, want[p.ProfileNo]) {
			t.Errorf("profile %d = %v, want %v", p.ProfileNo, p.Area, want[p.ProfileNo])
		}
	}
}

// Targeting a non-current profile leaves the live humans.area alone.
func TestMockHumanStore_RemoveAreaFromProfiles_InactiveLeavesLive(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "u1", CurrentProfileNo: 1, Area: []string{"alpha"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 2, Area: []string{"alpha"}})
	if _, err := m.RemoveAreaFromProfiles("u1", "alpha", []int{2}); err != nil {
		t.Fatal(err)
	}
	if h, _ := m.Get("u1"); !slices.Equal(h.Area, []string{"alpha"}) {
		t.Errorf("live humans.area changed: %v", h.Area)
	}
}
```

Add the SQL twin `TestRemoveAreaFromProfiles_SQL` in `user_locations_sql_test.go` with the **same** seed and the **same** assertions, reading back with `SELECT area FROM humans` and `SELECT profile_no, area FROM profiles … ORDER BY profile_no`. It expects humans `["alpha"]` and profiles `["alpha"]`, `["beta"]`, `["beta"]`.

- [ ] **Step 2: Run, confirm build failure** (`RemoveAreaFromProfiles undefined`).

- [ ] **Step 3: Implement.** Interface doc comment: one-name removal, the transaction, and the current-profile rule for `humans.area`. SQL:

```go
func (s *SQLHumanStore) RemoveAreaFromProfiles(id, name string, profileNos []int) (removedFrom []int, err error) {
	if len(profileNos) == 0 {
		return nil, nil
	}
	tx, err := s.db.Beginx()
	if err != nil {
		return nil, fmt.Errorf("remove area %s: begin: %w", id, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	strip := func(raw string) ([]string, bool) {
		areas := unmarshalStringSlice(raw)
		kept := make([]string, 0, len(areas))
		for _, a := range areas {
			if !areaNameMatches(a, name) {
				kept = append(kept, a)
			}
		}
		return kept, len(kept) != len(areas)
	}

	q, args, err := sqlx.In(`SELECT profile_no, area FROM profiles WHERE id = ? AND profile_no IN (?) ORDER BY profile_no FOR UPDATE`, id, profileNos)
	if err != nil {
		return nil, fmt.Errorf("remove area %s: build query: %w", id, err)
	}
	var rows []struct {
		ProfileNo int            `db:"profile_no"`
		Area      sql.NullString `db:"area"`
	}
	if err = tx.Select(&rows, tx.Rebind(q), args...); err != nil {
		return nil, fmt.Errorf("remove area %s: select profiles: %w", id, err)
	}
	for _, r := range rows {
		kept, changed := strip(r.Area.String)
		if !changed {
			continue
		}
		if _, err = tx.Exec(`UPDATE profiles SET area = ? WHERE id = ? AND profile_no = ?`,
			marshalStringSlice(kept), id, r.ProfileNo); err != nil {
			return nil, fmt.Errorf("remove area %s/%d: %w", id, r.ProfileNo, err)
		}
		removedFrom = append(removedFrom, r.ProfileNo)
	}

	var cur struct {
		ProfileNo int            `db:"current_profile_no"`
		Area      sql.NullString `db:"area"`
	}
	if err = tx.Get(&cur, `SELECT current_profile_no, area FROM humans WHERE id = ? FOR UPDATE`, id); err != nil {
		return nil, fmt.Errorf("remove area %s: select human: %w", id, err)
	}
	if slices.Contains(profileNos, cur.ProfileNo) {
		if kept, changed := strip(cur.Area.String); changed {
			if _, err = tx.Exec(`UPDATE humans SET area = ? WHERE id = ?`, marshalStringSlice(kept), id); err != nil {
				return nil, fmt.Errorf("remove area %s: update human: %w", id, err)
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("remove area %s: commit: %w", id, err)
	}
	return removedFrom, nil
}

// areaNameMatches compares a stored area name with a requested one across
// both storage spellings (humans.area keeps underscores, override_areas
// and typed bot input use spaces).
func areaNameMatches(stored, requested string) bool {
	return strings.EqualFold(stored, requested) ||
		geofence.NormalizeAreaKey(stored) == geofence.NormalizeAreaKey(requested)
}
```

If `geofence.NormalizeAreaKey` does not exist yet (because #243 has not merged), create `processor/internal/geofence/normalize.go` exactly as #243 Task 3 specifies. If `store` importing `geofence` creates an import cycle, which is unlikely since `geofence` imports nothing internal, inline the normaliser here instead.

Write the mock with the same rules and order: profiles ascending, and `humans.area` only when `CurrentProfileNo` is targeted. Reuse `areaNameMatches`.

- [ ] **Step 4:** Run the mock and SQL tests with the DSN exported. **Confirm the SQL test ran rather than skipped.** Then run the gate.

- [ ] **Step 5: Commit** — `feat(store): RemoveAreaFromProfiles removes one area name per profile in a transaction (#253)`.

---

### Task 3: `DELETE /api/v2/humans/{id}/areas`

**Files:**
- Modify: `processor/internal/api/v2_humans.go` (new `registerV2HumanRemoveArea`, registered in `RegisterV2Humans` after `registerV2HumanSetAreas`)
- Modify: `processor/internal/api/testdata/openapi.golden.json`, `API.md`
- Test: `processor/internal/api/v2_humans_test.go`

**Interfaces:**
- Consumes: `HumanStore.RemoveAreaFromProfiles`, `resolveFullHuman`, `profileSentinel`, `reloadState`.
- Produces: `DELETE /api/v2/humans/{id}/areas?name=<area>[&profile=N | &all_profiles=true]` → `200 {"name": "<as requested>", "removed_from": [<profile_no>...]}`.

- [ ] **Step 1: Write the failing tests**

```go
type v2RemoveAreaResult struct {
	Name        string `json:"name"`
	RemovedFrom []int  `json:"removed_from"`
}

func removeArea(t *testing.T, r *gin.Engine, query string) (int, v2RemoveAreaResult) {
	t.Helper()
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/areas?"+query, "")
	var got v2RemoveAreaResult
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v; raw: %s", err, w.Body.String())
		}
	}
	return w.Code, got
}

// #253: drop one fence from every profile, leaving each profile's other
// areas intact — what ?all_profiles= on POST cannot do (#252).
func TestV2Humans_RemoveArea_AllProfiles(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, nil)
	h, _ := humans.Get("u1")
	h.Area = []string{"alpha", "private"}
	humans.AddHuman(h)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha", "private"}})
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 2, Area: []string{"beta", "private"}})

	code, got := removeArea(t, r, "name=private&all_profiles=true")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if !slices.Equal(got.RemovedFrom, []int{1, 2}) {
		t.Errorf("removed_from = %v, want [1 2]", got.RemovedFrom)
	}
	ps, _ := humans.GetProfiles("u1")
	for _, p := range ps {
		if slices.Contains(p.Area, "private") || len(p.Area) != 1 {
			t.Errorf("profile %d = %v, want one surviving area", p.ProfileNo, p.Area)
		}
	}
	if atomic.LoadInt32(reloads) == 0 {
		t.Error("expected a state reload")
	}
}

// No permission check: removing can't widen alerts, and the fence is often
// already gone from the geofence feed.
func TestV2Humans_RemoveArea_UnknownFenceStillRemoved(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	h, _ := humans.Get("u1")
	h.Area = []string{"deleted_fence"}
	humans.AddHuman(h)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"deleted_fence"}})

	code, got := removeArea(t, r, "name=deleted_fence")
	if code != http.StatusOK || !slices.Equal(got.RemovedFrom, []int{1}) {
		t.Fatalf("code=%d removed_from=%v, want 200 [1]", code, got.RemovedFrom)
	}
}

func TestV2Humans_RemoveArea_NotHeldIsIdempotent(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha"}})
	code, got := removeArea(t, r, "name=beta")
	if code != http.StatusOK || len(got.RemovedFrom) != 0 || got.RemovedFrom == nil {
		t.Fatalf("code=%d removed_from=%#v, want 200 and an empty (non-null) list", code, got.RemovedFrom)
	}
}

func TestV2Humans_RemoveArea_Validation(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	if c, _ := removeArea(t, r, "name=a&profile=2&all_profiles=true"); c != http.StatusUnprocessableEntity {
		t.Errorf("profile+all_profiles: got %d, want 422", c)
	}
	if c, _ := removeArea(t, r, ""); c != http.StatusUnprocessableEntity {
		t.Errorf("missing name: got %d, want 422", c)
	}
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/nobody/areas?name=a", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown human: got %d, want 404", w.Code)
	}
}
```

- [ ] **Step 2: Run, confirm FAIL** (404/405 from gin, since the route does not exist).

- [ ] **Step 3: Implement**

```go
type v2RemoveAreaInput struct {
	ID          string `path:"id" doc:"Human id (the owning user)"`
	Name        string `query:"name" required:"true" minLength:"1" doc:"Area name to remove. Matched case-insensitively, with underscores and spaces treated alike. Need not be a current fence."`
	Profile     int    `query:"profile" default:"-1" doc:"Profile to remove it from; defaults to the active profile. Mutually exclusive with all_profiles."`
	AllProfiles bool   `query:"all_profiles" doc:"Remove it from every profile the human owns, leaving each profile's other areas intact. Mutually exclusive with profile."`
}

type v2RemoveAreaOutput struct {
	Body struct {
		Name        string `json:"name" doc:"The name as requested"`
		RemovedFrom []int  `json:"removed_from" doc:"Profile numbers whose area list held the name and no longer does. Empty when no targeted profile held it."`
	}
}

// registerV2HumanRemoveArea registers DELETE /v2/humans/{id}/areas (#253).
//
// POST …/areas is a whole-list replace, so dropping one name from profiles
// that each hold different sets needed a read-modify-write per profile, with
// no atomicity. This removes one name from each targeted profile's own list
// in a single transaction. No permission or existence check: a removal can
// never widen what the human is alerted for, and the fence is usually gone.
func registerV2HumanRemoveArea(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-remove-human-area", Method: "DELETE", Path: "/v2/humans/{id}/areas",
		Summary: "Remove one area from a human's profiles",
		Description: "Removes one area name from the active profile, `?profile=N`, or every profile with `?all_profiles=true`, " +
			"leaving each profile's other areas untouched. All targeted profiles are updated in one transaction. " +
			"Reports which profiles changed; removing a name no profile holds is a 200 with an empty list. " +
			"Triggers a state reload. 404 if the human does not exist.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2RemoveAreaInput) (*v2RemoveAreaOutput, error) {
		human, err := resolveFullHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}
		if in.AllProfiles && in.Profile != profileSentinel {
			return nil, huma.Error422UnprocessableEntity("profile and all_profiles are mutually exclusive")
		}

		targets := []int{human.CurrentProfileNo}
		switch {
		case in.AllProfiles:
			profiles, err := deps.Humans.GetProfiles(in.ID)
			if err != nil {
				log.Errorf("v2 humans: list profiles for %s: %s", in.ID, err)
				return nil, huma.Error500InternalServerError("database error")
			}
			targets = make([]int, 0, len(profiles))
			for _, p := range profiles {
				targets = append(targets, p.ProfileNo)
			}
		case in.Profile != profileSentinel:
			targets = []int{in.Profile}
		}

		removedFrom, err := deps.Humans.RemoveAreaFromProfiles(in.ID, in.Name, targets)
		if err != nil {
			log.Errorf("v2 humans: remove area %q from %s: %s", in.Name, in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if len(removedFrom) > 0 {
			reloadState(deps)
		}

		out := &v2RemoveAreaOutput{}
		out.Body.Name = in.Name
		out.Body.RemovedFrom = removedFrom
		if out.Body.RemovedFrom == nil {
			out.Body.RemovedFrom = []int{}
		}
		return out, nil
	})
}
```

The reload is skipped when nothing changed. `TestV2Humans_RemoveArea_AllProfiles` asserts a reload only on the changing path, which is consistent with that. If a reviewer prefers an unconditional reload to match the other v2 mutations, drop the `if`.

- [ ] **Step 4:** Regenerate the golden (`UPDATE_GOLDEN=1 go test ./internal/api/ -run Golden -count=1`). Add the endpoint to `API.md`, next to the v2 set-areas entry and to the CLAUDE.md v2 humans summary line. Run the gate.

- [ ] **Step 5: Commit** — `feat(api): DELETE /v2/humans/{id}/areas removes one area per profile (#253)`. The PR body should close #252 and #253, and say plainly that `RenameAreaInAllProfilesAsync` (the client's fourth workaround) is still not served.
