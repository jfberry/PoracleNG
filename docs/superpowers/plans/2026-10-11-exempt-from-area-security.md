# Fences Exempt from Area Security (#243) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an operator-declared `exemptFromAreaSecurity` geofence flag and collapse the three "which areas may this human have" functions into one `bot.PermittedAreas`, used by every grant and prune path, plus opt-in pruning of `humans.area` during reconciliation.

**Architecture:** One pure function, `bot.PermittedAreas(fences, cfg, human, admin, trusted)`, becomes the only definition of the permitted set. The API set-areas path and the per-rule `override_areas` validation call it as grant paths. Discord and Telegram reconciliation call it as prune paths, which means reconciliation needs the live fence list for the first time: a `*state.Manager` is threaded into both reconcilers. The flag is parsed in both geofence formats, reported by `GET /v2/humans/{id}/areas`, and never read by matching or delivery.

**Tech Stack:** Go 1.27, huma v2 on gin, sqlx/MySQL, BurntSushi/toml.

**Spec:** GitHub issue jfberry/PoracleNG#243 (body = approved design; comments = client answers). `gh issue view 243 --comments`. Background: #215, #228, #230.

## Global Constraints

- The fence property is spelled exactly `exemptFromAreaSecurity`. Its default is `false`, and absent means not exempt. Both the native and the GeoJSON `properties` parsers read it.
- The flag does **not** imply `userSelectable`. `trusted` keeps its #230 meaning: it lifts `userSelectable` only.
- The flag does **not** change matching or delivery. `area_restriction` / `strict_locations` behave exactly as today, so no file under `internal/matching/` reads it.
- The new config key is `[reconciliation.discord] prune_user_areas` and `[reconciliation.telegram] prune_user_areas`, a bool with default **false**. Do not make it default-on, and do not make the `humans.area` prune unconditional. The issue records why.
- The existing `override_areas` prune (Discord) stays unconditional on a community change, as today.
- `PermittedAreas` returns lowercased names. Callers comparing against `override_areas` key with the underscore→space normalisation (`geofence.NormalizeAreaKey`, introduced in Task 3).
- `ValidateAndPrune` is deleted along with its tests. `BuildPermittedSet` and `AreaLogic.buildAllowedSet` are deleted.
- The RED test `TestV2Humans_SetAreas_TrustedLiftsFenceInNoCommunity` is un-skipped and passes **with its `gamma` assertion unchanged**. That assertion is #228's regression guard.
- Pre-commit gate, run from `processor/` before **every** commit (all four must pass):
  `go build ./... && go vet ./... && go test -count=1 ./... && "$(go env GOPATH)/bin/golangci-lint" run --config ../.golangci.yml ./...`
- v1 (`internal/api/humans.go`) is frozen. Do not route v1 set-areas or v1 GET areas through `PermittedAreas`.
- #252 (the `?all_profiles=` doc string) and #253 (the per-name removal operation) are **out of scope** for this branch. They have their own plan: `2026-10-11-v2-set-areas-profile-ops.md`.

## Decision taken during planning (flag at review)

The three functions being collapsed disagree on **when the community filter applies**. Choosing one of their gates is unavoidable:

| Function | Community filter applies when |
|---|---|
| `api.settableAreaNames` | `Area.Enabled && !admin && human.AreaRestriction != nil` |
| `bot.BuildPermittedSet` (reconciliation) | always (it only knows memberships) |
| `AreaLogic.GetAvailableAreas` (bot `!area`) | `Area.Enabled && !admin` |

Reconciliation writes `area_restriction = CalculateLocationRestrictions(...)`, which is the **location fences**, and that is `nil` for a community with no `location_fence`. Under the API's gate, a reconciled member of such a community is never community-filtered. Set-areas then lets them select any fence, while `!area` and the reconciliation prune deny it. That is the grant-vs-prune disagreement this design exists to remove.

**Chosen gate for `PermittedAreas`:** `Area.Enabled && !admin && (human.AreaRestriction != nil || len(human.CommunityMembership) > 0)`.

- A human with neither field set is still unrestricted, as the API has always treated them. This covers humans created through the API without a community, and humans that predate area_security.
- A reconciled community member is always filtered. This matches `!area` and the prune.
- **Behaviour change:** v2 set-areas and v2 `override_areas` validation become stricter for reconciled members whose communities have no `location_fence`. Before, they could select any userSelectable fence. After, they get their community's areas plus exempt fences. This is pinned by a test in Task 4. If the reviewer rejects the change, it is a one-line edit to `subjectToCommunityFilter` plus that test.

`AreaLogic.GetAvailableAreas` keeps its own gate, because it takes a membership list rather than a human. It shares the community predicates (`communityAllowedSet`, `communityPermits`), so a fence is judged identically once the filter applies.

## Review Focus

1. **An empty or failed fence load must not wipe user data.** If `state.Fences` is empty, for example after a Koji outage at startup, then `PermittedAreas` returns nothing and a prune would delete every area and every override. Expected: reconciliation skips both prunes and logs a warning when there are no fences. Pinned in Task 7.
2. **The prune must not delete trusted-granted fences.** A non-userSelectable fence that set-areas stored via `trusted` has to survive reconciliation. Expected: prune paths call `PermittedAreas` with `trusted=true`, so only the community dimension prunes. Pinned in Task 7.
3. **Fences with underscores in `override_areas`.** Overrides are stored as `"gent centrum"` (underscore→space) and `humans.area` as `"gent_centrum"`. Expected: neither prune removes a permitted underscore fence. Pinned in Task 7. (Today's `BuildPermittedSet` keys with `ToLower` only, so it would prune it.)
4. **Reconciliation must judge the *new* membership, not the stale row.** The prune runs after the update. Expected: `PermittedAreas` receives a human carrying the freshly computed `communityList` and `areaRestriction`. Pinned in Task 7.
5. **The admin path through reconciliation.** Admins bypass the community filter on every grant path. Expected: an admin's areas are never pruned. Pinned in Task 7.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `processor/internal/geofence/geofence.go` | modify | `ExemptFromAreaSecurity` field, both parsers |
| `processor/internal/geofence/normalize.go` | create | `NormalizeAreaKey` (shared underscore→space key) |
| `processor/internal/geofence/rtree.go` | modify | use `NormalizeAreaKey` |
| `processor/internal/bot/permitted_areas.go` | create | `PermittedAreas`, `PermittedAreaKeys`, community predicates |
| `processor/internal/bot/permitted_areas_test.go` | create | matrix table test |
| `processor/internal/bot/area_logic.go` | modify | `GetAvailableAreas` honours exempt; delete `ValidateAndPrune`, `buildAllowedSet`, `BuildPermittedSet`; add `Fences()` |
| `processor/internal/bot/area_logic_test.go` | modify | delete the 4 `ValidateAndPrune` tests; add exempt picker test |
| `processor/internal/api/v2_humans.go` | modify | delete `settableAreaNames`; `settableAreaSet` and GET areas call `PermittedAreas`; `exemptFromAreaSecurity` on `v2AreaInfo` |
| `processor/internal/api/tracking.go` | modify | `newOverrideContext` calls `PermittedAreas`; `normalizeAreaKey` delegates |
| `processor/internal/store/human.go` / `human_sql.go` / `mock_human.go` | modify | `PruneUserAreas` |
| `processor/internal/store/user_locations_sql_test.go` | modify | `profiles` table in harness + SQL integration test |
| `processor/internal/store/mock_human_test.go` | modify | mock parity test |
| `processor/internal/config/config.go` | modify | `PruneUserAreas` on both reconciliation configs |
| `processor/internal/discordbot/reconciliation.go`, `bot.go` | modify | StateMgr threading; prune via `bot.PruneForMembership` |
| `processor/internal/telegrambot/reconciliation.go`, `bot.go` | modify | StateMgr threading; opt-in prune |
| `processor/internal/bot/reconcile_prune.go` | create | `PruneForMembership`, shared by both reconcilers |
| `processor/internal/api/config_schema.go`, `config/config.example.toml`, `API.md`, `CLAUDE.md` | modify | docs + config editor |

---

### Task 0: Isolated workspace and toolchain

The main checkout (`/Users/james/GolandProjects/PoracleNG`) has an **unrelated staged index**: clear-location, max-distance clamp, and SetArea-per-profile work that belongs to `feat/v2-location-clear-max-distance`. That index also **deletes the RED test**. Do not commit from it, and do not stash or reset it. It is someone's work in progress.

- [ ] **Step 1: Create a worktree off the RED commit**

```bash
cd /Users/james/GolandProjects/PoracleNG
git worktree add .worktrees/243-exempt-area-security -b feat/243-exempt-from-area-security 808bf0e7
cd .worktrees/243-exempt-area-security
cp ../../docs/superpowers/plans/2026-10-11-exempt-from-area-security.md docs/superpowers/plans/
grep -n "t.Skip(\"RED test for #243" processor/internal/api/v2_humans_test.go   # must print one line
```

- [ ] **Step 2: Install the CI-pinned linter.** The Homebrew binary was built with Go 1.26 and refuses to run against this go1.27 module. Running it from the repo root reports "0 issues" because it typechecks nothing, which is a false pass.

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1
"$(go env GOPATH)/bin/golangci-lint" version   # expect 2.13.1 built with go1.27
```

- [ ] **Step 3: Baseline the gate from `processor/`**

```bash
cd processor
go build ./... && go vet ./... && go test -count=1 ./... && "$(go env GOPATH)/bin/golangci-lint" run --config ../.golangci.yml ./...
```

Expected: all pass. If lint reports pre-existing issues, stop and report them. Do not fix unrelated code here.

- [ ] **Step 4: (Optional but recommended) a MySQL for the store integration tests in Task 6**

```bash
docker run -d --name poracle-test-mysql -e MYSQL_ROOT_PASSWORD=pw -e MYSQL_DATABASE=poracle_test -p 3307:3306 mysql:8
export PORACLENG_TEST_DSN='root:pw@tcp(127.0.0.1:3307)/poracle_test?parseTime=true&multiStatements=true'
```

Without it, the SQL tests `t.Skip`. CI does **not** set the DSN, so they also skip in CI. Running them locally is the only thing that catches a mock/SQL divergence (trap 3).

- [ ] **Step 5: Commit the plan**

```bash
git add docs/superpowers/plans/2026-10-11-exempt-from-area-security.md
git commit -m "docs(plan): implementation plan for #243

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 1: Thread the state manager into both reconcilers

This task has the widest blast radius and every prune task depends on it. It changes no behaviour, only gives reconciliation a way to see fences.

**Files:**
- Modify: `processor/internal/discordbot/reconciliation.go:28-59`
- Modify: `processor/internal/discordbot/bot.go:193-196`
- Modify: `processor/internal/discordbot/reconciliation_test.go:14-16`
- Modify: `processor/internal/telegrambot/reconciliation.go:29-56`
- Modify: `processor/internal/telegrambot/bot.go:69-74`
- Test: `processor/internal/discordbot/reconciliation_test.go`, `processor/internal/telegrambot/reconciliation_fences_test.go` (create)

**Interfaces:**
- Produces: `NewReconciliation(session, humanStore, cfg, translations, dtsStore, stateMgr *state.Manager)`, `(*Reconciliation).fences() []geofence.Fence`; `NewTelegramReconciliation(api, humanStore, cfg, translations, dtsStore, stateMgr *state.Manager)`, `(*TelegramReconciliation).fences() []geofence.Fence`. `fences()` returns nil when the manager or its state is nil.

- [ ] **Step 1: Write the failing tests**

Append to `processor/internal/discordbot/reconciliation_test.go` (add imports `geofence`, `state`):

```go
// Reconciliation had no state manager and so could not see userSelectable or
// exemptFromAreaSecurity (#243). fences() is its only window onto them.
func TestReconciliation_FencesFromStateManager(t *testing.T) {
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: []geofence.Fence{{Name: "alpha"}}})
	r := NewReconciliation(nil, store.NewMockHumanStore(), minimalConfig(), nil, nil, mgr)
	if got := r.fences(); len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("fences() = %v, want [alpha]", got)
	}
}

func TestReconciliation_FencesNilSafe(t *testing.T) {
	r := NewReconciliation(nil, store.NewMockHumanStore(), minimalConfig(), nil, nil, nil)
	if got := r.fences(); got != nil {
		t.Fatalf("fences() with nil manager = %v, want nil", got)
	}
	r = NewReconciliation(nil, store.NewMockHumanStore(), minimalConfig(), nil, nil, state.NewManager())
	if got := r.fences(); got != nil {
		t.Fatalf("fences() with empty manager = %v, want nil", got)
	}
}
```

Create `processor/internal/telegrambot/reconciliation_fences_test.go`:

```go
package telegrambot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func TestTelegramReconciliation_FencesFromStateManager(t *testing.T) {
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: []geofence.Fence{{Name: "alpha"}}})
	r := NewTelegramReconciliation(nil, store.NewMockHumanStore(), &config.Config{}, nil, nil, mgr)
	if got := r.fences(); len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("fences() = %v, want [alpha]", got)
	}
	r = NewTelegramReconciliation(nil, store.NewMockHumanStore(), &config.Config{}, nil, nil, nil)
	if got := r.fences(); got != nil {
		t.Fatalf("fences() with nil manager = %v, want nil", got)
	}
}
```

- [ ] **Step 2: Run them and confirm they fail to compile**

Run: `cd processor && go test ./internal/discordbot/ ./internal/telegrambot/ -run 'Fences' -count=1`
Expected: build failure, `too many arguments in call to NewReconciliation` / `r.fences undefined`.

- [ ] **Step 3: Implement.** In `discordbot/reconciliation.go`, add a field, a constructor param, and the accessor (imports `geofence`, `state`):

```go
type Reconciliation struct {
	session      *discordgo.Session
	humanStore   store.HumanStore
	cfg          *config.Config
	translations *i18n.Bundle
	dtsStore     *dts.TemplateStore
	// stateMgr gives reconciliation the live fence list, without which it
	// cannot apply userSelectable or exemptFromAreaSecurity when pruning (#243).
	stateMgr *state.Manager
	log      *log.Entry
	// ... greeting fields unchanged
}

func NewReconciliation(
	session *discordgo.Session,
	humanStore store.HumanStore,
	cfg *config.Config,
	translations *i18n.Bundle,
	dtsStore *dts.TemplateStore,
	stateMgr *state.Manager,
) *Reconciliation {
	return &Reconciliation{
		session:      session,
		humanStore:   humanStore,
		cfg:          cfg,
		translations: translations,
		dtsStore:     dtsStore,
		stateMgr:     stateMgr,
		log:          log.WithField("component", "reconciliation-discord"),
	}
}

// fences returns the live fence list, or nil when no state is loaded. nil
// means "unknown", and the prune paths treat it as a reason to skip.
func (r *Reconciliation) fences() []geofence.Fence {
	if r.stateMgr == nil {
		return nil
	}
	st := r.stateMgr.Get()
	if st == nil {
		return nil
	}
	return st.Fences
}
```

Check what `state.NewManager().Get()` returns before anything has been `Set`. If it returns a non-nil empty `*State`, then `st.Fences` is already nil, and the test still passes.

Update the caller at `discordbot/bot.go:194`:

```go
b.reconciliation = NewReconciliation(session, cfg.Humans, cfg.Cfg, cfg.Translations, cfg.DTS, b.StateMgr)
```

Update `newTestReconciliation` in `reconciliation_test.go` to pass `nil` as the sixth argument.

Make the identical change in `telegrambot/reconciliation.go`: add the `stateMgr *state.Manager` field, add it as the last constructor param, and add the `fences()` method with the same body. Update `telegrambot/bot.go:70`:

```go
b.reconciliation = NewTelegramReconciliation(api, cfg.Humans, cfg.Cfg, cfg.Translations, cfg.DTS, b.StateMgr)
```

Verify that `b.StateMgr` (from the embedded `bot.BotDeps`) is populated at both call sites, i.e. that `b` was built with its `BotDeps` before these lines. If it is not, use the deps value `b` was built from.

- [ ] **Step 4: Run the tests and the full gate**

Run: `go test ./internal/discordbot/ ./internal/telegrambot/ -count=1`, then the full gate.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot processor/internal/telegrambot
git commit -m "refactor(reconciliation): thread the state manager into both reconcilers

Reconciliation held no fence list, so it could not see userSelectable or the
forthcoming exemptFromAreaSecurity flag. No behaviour change. Refs #243.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `exemptFromAreaSecurity` in both geofence formats

**Files:**
- Modify: `processor/internal/geofence/geofence.go` (struct at :13, `fenceJSON` :32, `UnmarshalJSON` :45, `geoJSONProperties` :79, `parseGeoJSON` named keys :128 and constructor :184)
- Test: `processor/internal/geofence/geofence_test.go`

**Interfaces:**
- Produces: `geofence.Fence.ExemptFromAreaSecurity bool`.

- [ ] **Step 1: Write the failing tests** (append to `geofence_test.go`; it already has file-writing helpers for both formats, so mirror the existing `UserSelectable` tests at :49/:75/:134/:217 for how they write temp files. If the helper is not named `writeTemp`, use whatever those tests use):

```go
func TestExemptFromAreaSecurity_Native(t *testing.T) {
	path := writeTemp(t, `[
		{"name":"plain","path":[[0,0],[0,1],[1,1]]},
		{"name":"drawn","userSelectable":false,"exemptFromAreaSecurity":true,"path":[[0,0],[0,1],[1,1]]}
	]`)
	fences, err := LoadGeofenceFile(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if fences[0].ExemptFromAreaSecurity {
		t.Error("absent exemptFromAreaSecurity must default to false")
	}
	if !fences[1].ExemptFromAreaSecurity {
		t.Error("exemptFromAreaSecurity:true not read from native format")
	}
	if fences[1].UserSelectable {
		t.Error("the flag must not imply userSelectable")
	}
}

func TestExemptFromAreaSecurity_GeoJSON(t *testing.T) {
	path := writeTemp(t, `{"type":"FeatureCollection","features":[
		{"type":"Feature","properties":{"name":"plain"},
		 "geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}},
		{"type":"Feature","properties":{"name":"drawn","exemptFromAreaSecurity":true},
		 "geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}}
	]}`)
	fences, err := LoadGeofenceFile(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if fences[0].ExemptFromAreaSecurity {
		t.Error("absent exemptFromAreaSecurity must default to false")
	}
	if !fences[1].ExemptFromAreaSecurity {
		t.Error("exemptFromAreaSecurity:true not read from GeoJSON properties")
	}
	if _, leaked := fences[1].Properties["exemptFromAreaSecurity"]; leaked {
		t.Error("a named key must not also appear in Properties")
	}
}
```

- [ ] **Step 2: Run and confirm failure**

Run: `go test ./internal/geofence/ -run ExemptFromAreaSecurity -count=1`
Expected: build failure, `fences[0].ExemptFromAreaSecurity undefined`.

- [ ] **Step 3: Implement.** In `Fence`, after `DisplayInMatches`:

```go
	// ExemptFromAreaSecurity marks a fence as outside the community system: it is
	// in no community's allowed_areas by construction (a user-drawn fence), so the
	// community filter can only reject it and the prune can only delete it.
	// Default false. Does NOT affect strict_locations / area_restriction, and
	// does NOT imply UserSelectable (#243).
	ExemptFromAreaSecurity bool `json:"exemptFromAreaSecurity"`
```

Add `ExemptFromAreaSecurity bool \`json:"exemptFromAreaSecurity"\`` to both `fenceJSON` and `geoJSONProperties`. A plain bool is enough, because absent and false mean the same thing. In `UnmarshalJSON` add `f.ExemptFromAreaSecurity = raw.ExemptFromAreaSecurity`. In `parseGeoJSON` add `"exemptFromAreaSecurity": true` to `namedKeys` and `ExemptFromAreaSecurity: props.ExemptFromAreaSecurity,` to the `Fence{...}` literal.

- [ ] **Step 4: Run tests + gate.** Expected: PASS.

- [ ] **Step 5: Commit** — `feat(geofence): read exemptFromAreaSecurity in native and GeoJSON formats` (Refs #243, Co-Authored-By trailer).

---

### Task 3: `bot.PermittedAreas`, the single permitted-set definition

**Files:**
- Create: `processor/internal/geofence/normalize.go`
- Modify: `processor/internal/geofence/rtree.go:34`
- Create: `processor/internal/bot/permitted_areas.go`
- Test: `processor/internal/bot/permitted_areas_test.go`

**Interfaces:**
- Produces:
  - `geofence.NormalizeAreaKey(s string) string`: lowercase, `_`→space.
  - `bot.PermittedAreas(fences []geofence.Fence, cfg *config.Config, human *store.Human, admin, trusted bool) []string`: lowercased fence names. Never returns a name that is not a fence.
  - `bot.PermittedAreaKeys(names []string) map[string]bool`: each name keyed by **both** `strings.ToLower(n)` and `geofence.NormalizeAreaKey(n)`, so one set serves `humans.area` (stores underscores) and `override_areas` (stores spaces).
  - unexported `subjectToCommunityFilter`, `communityAllowedSet`, `communityPermits`. Task 5 reuses the last two.

- [ ] **Step 1: Write the failing table test** — `processor/internal/bot/permitted_areas_test.go`:

```go
package bot

import (
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// Fences covering every combination that matters:
//
//	alpha  selectable, in coast           beta   selectable, in harbour only
//	priv   NOT selectable, in coast       drawn  NOT selectable, exempt, in no community
//	pubx   selectable, exempt, in no community
//	lone   NOT selectable, NOT exempt, in no community
var permittedFences = []geofence.Fence{
	{Name: "Alpha", UserSelectable: true},
	{Name: "beta", UserSelectable: true},
	{Name: "priv", UserSelectable: false},
	{Name: "drawn", UserSelectable: false, ExemptFromAreaSecurity: true},
	{Name: "pubx", UserSelectable: true, ExemptFromAreaSecurity: true},
	{Name: "lone", UserSelectable: false},
}

func permittedCfg(enabled bool) *config.Config {
	cfg := &config.Config{}
	cfg.Area.Enabled = enabled
	cfg.Area.Communities = []config.CommunityConfig{
		{Name: "Coast", AllowedAreas: []string{"ALPHA", "priv"}},
		{Name: "harbour", AllowedAreas: []string{"beta"}},
	}
	return cfg
}

func TestPermittedAreas(t *testing.T) {
	member := &store.Human{ID: "u", CommunityMembership: []string{"coast"}, AreaRestriction: []string{"x"}}
	// Reconciled member of a community with no location_fence: restriction nil.
	memberNoRestriction := &store.Human{ID: "u", CommunityMembership: []string{"coast"}}
	// API-created human with no community: neither field set.
	unrestricted := &store.Human{ID: "u"}

	cases := []struct {
		name           string
		enabled        bool
		human          *store.Human
		admin, trusted bool
		want           []string
	}{
		// area_security off: only userSelectable matters; exempt is irrelevant.
		{"off/plain", false, member, false, false, []string{"alpha", "beta", "pubx"}},
		{"off/trusted", false, member, false, true, []string{"alpha", "beta", "priv", "drawn", "pubx", "lone"}},
		{"off/admin", false, member, true, false, []string{"alpha", "beta", "priv", "drawn", "pubx", "lone"}},

		// area_security on, community member.
		{"on/plain", true, member, false, false, []string{"alpha", "pubx"}},
		// The #243 case: trusted + exempt lifts drawn; beta (other community)
		// and lone (not exempt) stay out. beta staying out is #228.
		{"on/trusted", true, member, false, true, []string{"alpha", "priv", "drawn", "pubx"}},
		{"on/admin", true, member, true, false, []string{"alpha", "beta", "priv", "drawn", "pubx", "lone"}},

		// Gate: membership alone subjects the human to the filter (see plan
		// "Decision taken during planning").
		{"on/member-no-restriction", true, memberNoRestriction, false, false, []string{"alpha", "pubx"}},
		// Gate: no membership and no restriction stays unrestricted, as the API always had it.
		{"on/unrestricted", true, unrestricted, false, false, []string{"alpha", "beta", "pubx"}},
		{"on/nil-human", true, nil, false, false, []string{"alpha", "beta", "pubx"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PermittedAreas(permittedFences, permittedCfg(tc.enabled), tc.human, tc.admin, tc.trusted)
			if !slices.Equal(got, tc.want) {
				t.Errorf("PermittedAreas = %v, want %v", got, tc.want)
			}
		})
	}
}

// Membership matching is case-insensitive, and so is allowed_areas: config
// spells "Coast"/"ALPHA", the human row stores "coast", the fence is "Alpha".
// (Covered by the on/plain row; this asserts lowercased output explicitly.)
func TestPermittedAreas_ReturnsLowercase(t *testing.T) {
	got := PermittedAreas([]geofence.Fence{{Name: "Gent_Centrum", UserSelectable: true}}, permittedCfg(false), nil, false, false)
	if !slices.Equal(got, []string{"gent_centrum"}) {
		t.Fatalf("got %v, want [gent_centrum]", got)
	}
}

func TestPermittedAreas_NoFencesIsEmpty(t *testing.T) {
	if got := PermittedAreas(nil, permittedCfg(true), nil, true, true); len(got) != 0 {
		t.Fatalf("no fences must permit nothing, got %v", got)
	}
}

func TestPermittedAreaKeys_BothStorageConventions(t *testing.T) {
	keys := PermittedAreaKeys([]string{"gent_centrum"})
	if !keys["gent_centrum"] || !keys["gent centrum"] {
		t.Fatalf("keys = %v, want both the humans.area and override_areas spellings", keys)
	}
}
```

- [ ] **Step 2: Run and confirm failure**

Run: `go test ./internal/bot/ -run 'PermittedArea' -count=1`
Expected: build failure, `undefined: PermittedAreas`.

- [ ] **Step 3: Implement.** Create `processor/internal/geofence/normalize.go`:

```go
package geofence

import "strings"

// NormalizeAreaKey keys an area name the way override_areas and
// Fence.NormalizedName store it: lowercased, underscores as spaces.
// humans.area stores names lowercased with underscores intact, so code
// comparing against both must key both ways.
func NormalizeAreaKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", " "))
}
```

In `rtree.go:34` replace the inline expression with `f.NormalizedName = NormalizeAreaKey(f.Name)`, and drop the `strings` import if it becomes unused.

Create `processor/internal/bot/permitted_areas.go`:

```go
package bot

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// PermittedAreas returns the fence names this human may hold, lowercased.
//
// It is THE definition, called by every grant path (API set-areas, per-rule
// override_areas validation) and every prune path (reconciliation), so a
// fence cannot be granted by one rule and removed by another (#243).
//
// A name is only ever returned if it is a fence. On top of that:
//   - admin and trusted lift the userSelectable filter;
//   - the community filter is lifted only by admin, or by the fence declaring
//     itself exemptFromAreaSecurity.
//
// trusted deliberately does not lift the community filter (#228/#230).
func PermittedAreas(fences []geofence.Fence, cfg *config.Config,
	human *store.Human, admin, trusted bool) []string {
	filtered := subjectToCommunityFilter(cfg, human, admin)
	var allowed map[string]bool
	if filtered {
		allowed = communityAllowedSet(cfg.Area.Communities, human.CommunityMembership)
	}
	out := make([]string, 0, len(fences))
	for _, f := range fences {
		if !admin && !trusted && !f.UserSelectable {
			continue
		}
		if filtered && !communityPermits(f, allowed) {
			continue
		}
		out = append(out, strings.ToLower(f.Name))
	}
	return out
}

// PermittedAreaKeys turns PermittedAreas output into a lookup set holding
// both storage spellings: humans.area keeps underscores, override_areas
// stores them as spaces (geofence.NormalizeAreaKey).
func PermittedAreaKeys(names []string) map[string]bool {
	keys := make(map[string]bool, 2*len(names))
	for _, n := range names {
		keys[strings.ToLower(n)] = true
		keys[geofence.NormalizeAreaKey(n)] = true
	}
	return keys
}

// subjectToCommunityFilter reports whether area_security narrows this
// human's areas. A human with neither a community nor an area_restriction —
// created through the API without one, or predating area_security — is
// unrestricted, as the API has always treated them. A community member is
// filtered even when area_restriction is nil, which it is whenever their
// communities configure no location_fence.
func subjectToCommunityFilter(cfg *config.Config, human *store.Human, admin bool) bool {
	if admin || cfg == nil || !cfg.Area.Enabled || human == nil {
		return false
	}
	return human.AreaRestriction != nil || len(human.CommunityMembership) > 0
}

// communityAllowedSet is the lowercase union of allowed_areas across the
// communities memberOf names (case-insensitive).
func communityAllowedSet(communities []config.CommunityConfig, memberOf []string) map[string]bool {
	allowed := make(map[string]bool)
	for _, comm := range communities {
		for _, m := range memberOf {
			if strings.EqualFold(comm.Name, m) {
				for _, a := range comm.AllowedAreas {
					allowed[strings.ToLower(a)] = true
				}
			}
		}
	}
	return allowed
}

// communityPermits reports whether the community filter lets f through: it
// is in one of the human's communities, or it declares itself outside the
// community system.
func communityPermits(f geofence.Fence, allowed map[string]bool) bool {
	return f.ExemptFromAreaSecurity || allowed[strings.ToLower(f.Name)]
}
```

- [ ] **Step 4: Run tests + gate.** Expected: PASS. (`BuildPermittedSet` still exists alongside; it is removed in Task 7.)

- [ ] **Step 5: Commit** — `feat(bot): PermittedAreas, one definition of the areas a human may hold` (Refs #243).

---

### Task 4: API grant paths use `PermittedAreas`; un-skip the RED test

**Files:**
- Modify: `processor/internal/api/v2_humans.go` (delete `settableAreaNames` :654-699; rewrite `settableAreaSet` :701-710)
- Modify: `processor/internal/api/tracking.go` (`normalizeAreaKey`, `newOverrideContext`)
- Modify: `processor/internal/bot/area_logic.go` (add `Fences()`)
- Test: `processor/internal/api/v2_humans_test.go`, `processor/internal/api/v2_pokemon_test.go`

**Interfaces:**
- Consumes: `bot.PermittedAreas`, `geofence.NormalizeAreaKey` (Task 3), `Fence.ExemptFromAreaSecurity` (Task 2).
- Produces: `(*bot.AreaLogic).Fences() []geofence.Fence`; test helper `drawnFence(name, lat, lon, half)` in `v2_humans_test.go`.

- [ ] **Step 1: Un-skip the RED test and mark the fixture fence exempt.** In `v2_humans_test.go`:

1. Delete the `t.Skip(...)` line from `TestV2Humans_SetAreas_TrustedLiftsFenceInNoCommunity`. Replace the "SKIPPED: …" comment block with a one-line pointer: `// RED test for #243, committed skipped in 808bf0e7. The gamma assertion is #228's guard.` Do **not** touch any assertion.
2. Add a helper next to `privateFence`:

```go
// drawnFence is the shape PoracleWeb.NET serves for a user-drawn geofence
// once #243 ships: userSelectable:false to keep it out of the picker, and
// exemptFromAreaSecurity because no community publishes a user's own fence.
func drawnFence(name string, lat, lon, half float64) geofence.Fence {
	f := privateFence(name, lat, lon, half)
	f.ExemptFromAreaSecurity = true
	return f
}
```

3. In `newV2HumansTestAPI`, change `privateFence("private", 30, 30, 1)` to `drawnFence("private", 30, 30, 1)`, and add one more fence, `privateFence("lone", 50, 50, 1)` (non-selectable, **not** exempt). Then update `TestV2Humans_AreasGet_AllWhenUnrestricted`'s count from 4 to 5. That is the only count assertion on this fixture; grep `len(areas) !=` in the file to confirm.

This fixture change is the fix's contract, not a weakening. The RED test's own comment says the fixture's `private` stands in for a user-drawn fence. Under #243, a user-drawn fence carries the flag.

- [ ] **Step 2: Add the remaining API-level tests** (append to `v2_humans_test.go`):

```go
// The exempt flag lifts the community filter, not userSelectable: without
// trusted, a drawn fence is still refused under area_security.
func TestV2Humans_SetAreas_ExemptStillNeedsTrustedWhenNotSelectable(t *testing.T) {
	r, _ := newCoastOnlyAPI(t)
	code, got := setAreas(t, r, `{"areas":["private"]}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 0 || len(got.Rejected) != 1 {
		t.Fatalf("areas=%v rejected=%v, want private rejected without trusted", got.Areas, got.Rejected)
	}
}

// Without the flag, trusted cannot reach a fence outside the community: the
// flag, not trusted, is what makes the drawn fence settable (#243).
func TestV2Humans_SetAreas_TrustedDoesNotLiftUnflaggedFenceInNoCommunity(t *testing.T) {
	r, _ := newCoastOnlyAPI(t)
	code, got := setAreas(t, r, `{"areas":["lone"],"trusted":true}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 0 || len(got.Rejected) != 1 || got.Rejected[0] != "lone" {
		t.Fatalf("areas=%v rejected=%v, want lone rejected", got.Areas, got.Rejected)
	}
}

// Gate decision (see plan): a community member is community-filtered even
// when area_restriction is nil, which reconciliation writes for communities
// with no location_fence. Before #243 such a member could set any fence.
func TestV2Humans_SetAreas_MemberWithoutRestrictionIsCommunityFiltered(t *testing.T) {
	cfg := &config.Config{}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{{Name: "coast", AllowedAreas: []string{"alpha"}}}
	r, humans, _ := newV2HumansTestAPI(t, cfg)
	h, _ := humans.Get("u1")
	h.CommunityMembership = []string{"coast"}
	h.AreaRestriction = nil
	humans.AddHuman(h)

	code, got := setAreas(t, r, `{"areas":["alpha","beta"]}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 1 || got.Areas[0] != "alpha" {
		t.Fatalf("areas=%v rejected=%v, want only alpha", got.Areas, got.Rejected)
	}
}

// newCoastOnlyAPI: area_security on, u1 in "coast" (alpha, beta), and the
// drawn "private" fence in NO community, as a real drawn fence is.
func newCoastOnlyAPI(t *testing.T) (*gin.Engine, *store.MockHumanStore) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{
		{Name: "coast", AllowedAreas: []string{"alpha", "beta"}},
	}
	r, humans, _ := newV2HumansTestAPI(t, cfg)
	h, _ := humans.Get("u1")
	h.CommunityMembership = []string{"coast"}
	h.AreaRestriction = []string{"alpha", "beta"}
	humans.AddHuman(h)
	return r, humans
}
```

**Trap 1 regression test.** Append to `v2_pokemon_test.go`. It **must** enable area_security: the client reported the override rejection "did not reproduce" on three instances, all with area_security off. First refactor `newV2PokemonTestAPI` into a thin wrapper over a new `newV2PokemonTestAPIWithConfig(t *testing.T, cfg *config.Config, setup func(*store.MockHumanStore))`. Pass `cfg` into `deps.Config` (nil → `&config.Config{}`) and call `setup(humans)` after seeding u1/u2. Then, in the fence list, change `privateFence("drawn", …)` to `drawnFence("drawn", …)` and add `privateFence("lone", 40, 40, 1)`:

```go
// Trap: this path only diverges with area_security ON. With it off, the
// community filter never runs, which is why the client's three instances
// "did not reproduce" the userSelectable rejection.
func TestV2Pokemon_OverrideAreas_ExemptDrawnFenceUnderAreaSecurity(t *testing.T) {
	cfg := &config.Config{}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{{Name: "coast", AllowedAreas: []string{"alpha"}}}
	r, monsterStore, _, restore := newV2PokemonTestAPIWithConfig(t, cfg, func(h *store.MockHumanStore) {
		u, _ := h.Get("u1")
		u.CommunityMembership = []string{"coast"}
		u.AreaRestriction = []string{"alpha"}
		h.AddHuman(u)
	})
	defer restore()

	post := func(q, areas string) int {
		return v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon?silent=true"+q,
			`[{"pokemon_id":25,"override_areas":`+areas+`}]`).Code
	}
	if c := post("", `["drawn"]`); c != http.StatusBadRequest && c != http.StatusUnprocessableEntity {
		t.Fatalf("untrusted drawn: got %d, want 4xx (not userSelectable)", c)
	}
	if c := post("&trusted=true", `["lone"]`); c != http.StatusBadRequest && c != http.StatusUnprocessableEntity {
		t.Fatalf("trusted unflagged lone: got %d, want 4xx (not in community, not exempt)", c)
	}
	if c := post("&trusted=true", `["beta"]`); c != http.StatusBadRequest && c != http.StatusUnprocessableEntity {
		t.Fatalf("trusted beta: got %d, want 4xx (#228: other community stays out)", c)
	}
	if c := post("&trusted=true", `["drawn"]`); c != http.StatusOK {
		t.Fatalf("trusted exempt drawn: got %d, want 200", c)
	}
	if rows := monsterStore.AllRows(); len(rows) != 1 || rows[0].OverrideAreas[0] != "drawn" {
		t.Fatalf("stored = %+v, want one rule scoped to drawn", rows)
	}
}
```

- [ ] **Step 3: Run and confirm the right failures**

Run: `go test ./internal/api/ -run 'SetAreas|OverrideAreas_Exempt|AreasGet' -count=1`
Expected FAIL: `TrustedLiftsFenceInNoCommunity` (`areas=[alpha] rejected=[gamma private]`), `MemberWithoutRestrictionIsCommunityFiltered` (beta stored), and `OverrideAreas_ExemptDrawnFenceUnderAreaSecurity` (trusted drawn → 4xx). The two "still rejected" tests already pass. That is expected, since they guard behaviour that must not change.

- [ ] **Step 4: Implement.** In `area_logic.go` add:

```go
// Fences returns the fence list this AreaLogic was built from.
func (a *AreaLogic) Fences() []geofence.Fence { return a.fences }
```

In `v2_humans.go`, delete `settableAreaNames`. The policy history in its comment moves into `PermittedAreas`' doc comment, which already carries it. Rewrite `settableAreaSet`:

```go
// settableAreaSet keys bot.PermittedAreas for the set-areas path, which
// stores names lowercased. (The per-rule override path keys the same policy
// with normalizeAreaKey instead, because that is how it stores.)
func settableAreaSet(deps *TrackingDeps, human *store.Human, admin, trusted bool) map[string]bool {
	set := make(map[string]bool)
	if deps.StateMgr == nil {
		return set
	}
	st := deps.StateMgr.Get()
	if st == nil {
		return set
	}
	for _, a := range bot.PermittedAreas(st.Fences, deps.Config, human, admin, trusted) {
		set[a] = true
	}
	return set
}
```

Remove the now-unused `community` import from `v2_humans.go` **only if** nothing else uses it. Create and GET areas still do at this point.

In `tracking.go`, make `normalizeAreaKey` delegate: `return geofence.NormalizeAreaKey(s)`. Replace the `names` block in `newOverrideContext`:

```go
	var fences []geofence.Fence
	switch {
	case deps.AreaLogic != nil:
		fences = deps.AreaLogic.Fences()
	case deps.StateMgr != nil && deps.StateMgr.Get() != nil:
		// Production path: AreaLogic is nil in main.go (#211).
		fences = deps.StateMgr.Get().Fences
	default:
		return overrideContext{human: human}, "", 0
	}
	names := bot.PermittedAreas(fences, deps.Config, human, admin, trusted)
```

This also fixes a latent test-only divergence: the old `AreaLogic` branch called `GetAvailableAreas`, which ignored `trusted`. Keep the rest of the function (building `permitted` with `normalizeAreaKey`) unchanged.

- [ ] **Step 5: Run the API package, then the full gate**

Run: `go test ./internal/api/ -count=1`, then the gate.
Expected: PASS, including `TestV2Humans_SetAreas_TrustedDoesNotLiftCommunityRestriction` and `…TrustedStillLiftsUserSelectableWithinCommunity` unchanged. If any other existing test fails, read it before changing it. A failure under area_security with a nil `AreaRestriction` is the gate decision showing up, and has to be reported rather than edited away.

- [ ] **Step 6: Commit** — `fix(api): exempt fences survive the community filter on set-areas and override_areas` (body: un-skips the #243 RED test; notes the gate decision; Refs #243, #228).

---

### Task 5: Bot `!area` honours the flag; delete `ValidateAndPrune`

**Files:**
- Modify: `processor/internal/bot/area_logic.go` (`GetAvailableAreas` :66-80; delete `ValidateAndPrune` :233-256 and `buildAllowedSet` :258-261)
- Modify: `processor/internal/bot/area_logic_test.go` (delete `TestAreaValidateAndPrune_*` at :332, :345, :362, :376)

**Interfaces:**
- Consumes: `communityAllowedSet`, `communityPermits` (Task 3).

- [ ] **Step 1: Write the failing test** (append to `area_logic_test.go`):

```go
// A selectable fence declared exempt is offered by !area to every member
// under area_security; a non-selectable exempt fence is not, because the
// flag lifts the community filter only, never userSelectable (#243).
func TestGetAvailableAreas_HonoursExempt(t *testing.T) {
	cfg := &config.Config{}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{{Name: "coast", AllowedAreas: []string{"alpha"}}}
	al := NewAreaLogic([]geofence.Fence{
		{Name: "alpha", UserSelectable: true},
		{Name: "beta", UserSelectable: true},
		{Name: "pubx", UserSelectable: true, ExemptFromAreaSecurity: true},
		{Name: "drawn", UserSelectable: false, ExemptFromAreaSecurity: true},
	}, cfg)
	var got []string
	for _, a := range al.GetAvailableAreas([]string{"coast"}, false) {
		got = append(got, a.Name)
	}
	if !slices.Equal(got, []string{"alpha", "pubx"}) {
		t.Fatalf("available = %v, want [alpha pubx]", got)
	}
}
```

- [ ] **Step 2: Run and confirm failure** — `go test ./internal/bot/ -run HonoursExempt -count=1`. Expected FAIL: `available = [alpha]`.

- [ ] **Step 3: Implement.** In `GetAvailableAreas`, replace the area-security block:

```go
	// Area security enabled. Same community predicates as PermittedAreas, so
	// a fence is judged identically by !area and the API. The gate differs on
	// purpose: !area always filters, because it is handed a membership list
	// rather than a human.
	allowedSet := communityAllowedSet(a.cfg.Area.Communities, communities)

	var areas []AreaInfo
	for _, f := range a.fences {
		if f.UserSelectable && communityPermits(f, allowedSet) {
```

(the body of the loop is unchanged). Delete `ValidateAndPrune` and `buildAllowedSet`, and delete the four `TestAreaValidateAndPrune_*` tests. Leave `BuildPermittedSet` for now, because reconciliation still calls it until Task 7. Confirm nothing else references the deleted symbols: `grep -rn "ValidateAndPrune\|buildAllowedSet" processor/` should print nothing.

- [ ] **Step 4: Run tests + gate.** Expected: PASS.

- [ ] **Step 5: Commit** — `refactor(bot): !area honours exemptFromAreaSecurity; delete uncalled ValidateAndPrune` (body: ValidateAndPrune arrived in #59 with no caller and was mistaken for a live path; Refs #243).

---

### Task 6: `HumanStore.PruneUserAreas`, SQL and mock in parity

Trap 3: `MockHumanStore` has twice diverged from `SQLHumanStore` in ways that let a wrong test pass. Write one scenario, assert it against both, and run the SQL half locally with the DSN from Task 0.

**Files:**
- Modify: `processor/internal/store/human.go` (interface, after `PruneOverrideAreas` :253)
- Modify: `processor/internal/store/human_sql.go` (new method)
- Modify: `processor/internal/store/mock_human.go` (new method)
- Modify: `processor/internal/store/user_locations_sql_test.go` (harness: `profiles` table + cleanup; new test)
- Modify: `processor/internal/store/mock_human_test.go` (new test)

**Interfaces:**
- Produces: `PruneUserAreas(humanID string, permitted map[string]bool) (removed []string, err error)`. It drops every name not in `permitted` (looked up by `strings.ToLower(name)`) from `humans.area` **and from every `profiles` row** of that human, in one transaction. It returns the distinct lowercased names removed, in first-seen order: humans row first, then profiles by `profile_no`. Every profile is pruned because `SwitchProfile` copies `profiles.area` into `humans.area`, so pruning only the live row would bring the area back on the next switch. A row whose areas are all pruned is written as `[]` (matching `marshalStringSlice`), not NULL.

- [ ] **Step 1: Write the failing tests.** Mock (`mock_human_test.go`):

```go
func TestMockHumanStore_PruneUserAreas(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "u1", CurrentProfileNo: 1, Area: []string{"alpha", "beta", "gent_centrum"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha", "beta", "gent_centrum"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 2, Area: []string{"beta", "delta"}})

	removed, err := m.PruneUserAreas("u1", map[string]bool{"alpha": true, "gent_centrum": true, "delta": true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(removed, []string{"beta"}) {
		t.Errorf("removed = %v, want [beta]", removed)
	}
	h, _ := m.Get("u1")
	if !slices.Equal(h.Area, []string{"alpha", "gent_centrum"}) {
		t.Errorf("humans.area = %v", h.Area)
	}
	ps, _ := m.GetProfiles("u1")
	byNo := map[int][]string{}
	for _, p := range ps {
		byNo[p.ProfileNo] = p.Area
	}
	if !slices.Equal(byNo[1], []string{"alpha", "gent_centrum"}) || !slices.Equal(byNo[2], []string{"delta"}) {
		t.Errorf("profiles = %v, want 1:[alpha gent_centrum] 2:[delta]", byNo)
	}
	// Idempotent: nothing left to remove.
	if removed, _ := m.PruneUserAreas("u1", map[string]bool{"alpha": true, "gent_centrum": true, "delta": true}); len(removed) != 0 {
		t.Errorf("second prune removed %v, want nothing", removed)
	}
}
```

SQL (`user_locations_sql_test.go`). First extend `openTestDB`: after the `user_locations` table, add

```go
	exec(`CREATE TABLE IF NOT EXISTS profiles (
		uid          INT PRIMARY KEY AUTO_INCREMENT,
		id           VARCHAR(64) NOT NULL,
		profile_no   INT NOT NULL DEFAULT 1,
		name         VARCHAR(64) NOT NULL DEFAULT '',
		area         TEXT,
		latitude     DOUBLE NOT NULL DEFAULT 0,
		longitude    DOUBLE NOT NULL DEFAULT 0,
		active_hours TEXT,
		UNIQUE KEY uniq_id_profile (id, profile_no)
	) ENGINE=InnoDB`)
```

and `_, _ = db.Exec(\`DELETE FROM profiles WHERE id = 'u1'\`)` in the cleanup. Before relying on that table shape, compare it against the real `profiles` DDL in `processor/internal/db/migrations/`, and copy the real column types if they differ. Then add:

```go
func TestPruneUserAreas_SQL(t *testing.T) {
	dbx := openTestDB(t)
	s := &SQLHumanStore{db: dbx}
	mustExec(t, dbx, `UPDATE humans SET area = '["alpha","beta","gent_centrum"]', current_profile_no = 1 WHERE id = 'u1'`)
	mustExec(t, dbx, `INSERT INTO profiles (id, profile_no, area) VALUES
		('u1', 1, '["alpha","beta","gent_centrum"]'),
		('u1', 2, '["beta","delta"]')`)

	removed, err := s.PruneUserAreas("u1", map[string]bool{"alpha": true, "gent_centrum": true, "delta": true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(removed, []string{"beta"}) {
		t.Errorf("removed = %v, want [beta]", removed)
	}
	var humanArea string
	if err := dbx.Get(&humanArea, `SELECT area FROM humans WHERE id='u1'`); err != nil {
		t.Fatal(err)
	}
	if humanArea != `["alpha","gent_centrum"]` {
		t.Errorf("humans.area = %s", humanArea)
	}
	var rows []struct {
		ProfileNo int    `db:"profile_no"`
		Area      string `db:"area"`
	}
	if err := dbx.Select(&rows, `SELECT profile_no, area FROM profiles WHERE id='u1' ORDER BY profile_no`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Area != `["alpha","gent_centrum"]` || rows[1].Area != `["delta"]` {
		t.Errorf("profiles = %+v", rows)
	}
	// Everything pruned writes [] (not NULL), matching marshalStringSlice.
	if _, err := s.PruneUserAreas("u1", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if err := dbx.Get(&humanArea, `SELECT area FROM humans WHERE id='u1'`); err != nil {
		t.Fatal(err)
	}
	if humanArea != `[]` {
		t.Errorf("fully pruned humans.area = %q, want []", humanArea)
	}
}
```

- [ ] **Step 2: Run and confirm failure** — `go test ./internal/store/ -run PruneUserAreas -count=1` (with `PORACLENG_TEST_DSN` exported). Expected: build failure, `m.PruneUserAreas undefined`.

- [ ] **Step 3: Implement.** Interface (`human.go`):

```go
	// PruneUserAreas drops every area not in permitted (keyed lowercase) from
	// humans.area and from EVERY profile row of this human, in one
	// transaction. Every profile, because SwitchProfile copies profiles.area
	// into humans.area. Returns the distinct names removed. Destroys
	// user-entered data: callers gate it behind prune_user_areas (#243).
	PruneUserAreas(humanID string, permitted map[string]bool) (removed []string, err error)
```

SQL (`human_sql.go`):

```go
func (s *SQLHumanStore) PruneUserAreas(humanID string, permitted map[string]bool) (removed []string, err error) {
	tx, err := s.db.Beginx()
	if err != nil {
		return nil, fmt.Errorf("prune user areas %s: begin: %w", humanID, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	seen := map[string]bool{}
	prune := func(raw string) (kept []string, changed bool) {
		areas := unmarshalStringSlice(raw)
		kept = make([]string, 0, len(areas))
		for _, a := range areas {
			low := strings.ToLower(a)
			if permitted[low] {
				kept = append(kept, a)
				continue
			}
			changed = true
			if !seen[low] {
				seen[low] = true
				removed = append(removed, low)
			}
		}
		return kept, changed
	}

	var humanArea sql.NullString
	if err = tx.Get(&humanArea, `SELECT area FROM humans WHERE id = ? FOR UPDATE`, humanID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
			_ = tx.Rollback()
			return nil, nil
		}
		return nil, fmt.Errorf("prune user areas %s: select human: %w", humanID, err)
	}
	if kept, changed := prune(humanArea.String); changed {
		if _, err = tx.Exec(`UPDATE humans SET area = ? WHERE id = ?`, marshalStringSlice(kept), humanID); err != nil {
			return nil, fmt.Errorf("prune user areas %s: update human: %w", humanID, err)
		}
	}

	var profiles []struct {
		ProfileNo int            `db:"profile_no"`
		Area      sql.NullString `db:"area"`
	}
	if err = tx.Select(&profiles, `SELECT profile_no, area FROM profiles WHERE id = ? ORDER BY profile_no FOR UPDATE`, humanID); err != nil {
		return nil, fmt.Errorf("prune user areas %s: select profiles: %w", humanID, err)
	}
	for _, p := range profiles {
		if kept, changed := prune(p.Area.String); changed {
			if _, err = tx.Exec(`UPDATE profiles SET area = ? WHERE id = ? AND profile_no = ?`,
				marshalStringSlice(kept), humanID, p.ProfileNo); err != nil {
				return nil, fmt.Errorf("prune user areas %s/%d: update profile: %w", humanID, p.ProfileNo, err)
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("prune user areas %s: commit: %w", humanID, err)
	}
	return removed, nil
}
```

Notes:
- `marshalStringSlice` on a non-nil empty slice yields `[]`. `kept` is always non-nil here. Add `errors`/`database/sql` imports if they are not already present.
- Check what type `humans.area` is in the real schema. If it is `NOT NULL` with a default, `sql.NullString` still scans it fine.

Mock (`mock_human.go`), mirroring the SQL order exactly (humans first, then profiles sorted by `ProfileNo`):

```go
func (m *MockHumanStore) PruneUserAreas(humanID string, permitted map[string]bool) ([]string, error) {
	m.record("PruneUserAreas")
	m.mu.Lock()
	defer m.mu.Unlock()
	var removed []string
	seen := map[string]bool{}
	prune := func(areas []string) []string {
		kept := make([]string, 0, len(areas))
		for _, a := range areas {
			low := strings.ToLower(a)
			if permitted[low] {
				kept = append(kept, a)
			} else if !seen[low] {
				seen[low] = true
				removed = append(removed, low)
			}
		}
		return kept
	}
	if h, ok := m.humans[humanID]; ok {
		h.Area = prune(h.Area)
	}
	ps := m.profiles[humanID]
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].ProfileNo < ps[j].ProfileNo })
	for i := range ps {
		ps[i].Area = prune(ps[i].Area)
	}
	return removed, nil
}
```

Check whether `m.humans[id]` is stored by pointer. The `h, _ := humans.Get(...); h.X = …; humans.AddHuman(h)` pattern in API tests suggests `Get` may return a copy. If it does, write back with `m.humans[humanID] = h` the same way other mock setters do. Copy whatever `SetArea` in this file does.

- [ ] **Step 4: Run both tests with the DSN exported, then the gate.** Expected: PASS for both, and `TestPruneUserAreas_SQL` must actually **run**, not skip. Confirm with `go test ./internal/store/ -run PruneUserAreas -count=1 -v | grep -E "^(=== RUN|--- (PASS|SKIP|FAIL))"`. If you cannot get a MySQL up, say so in the commit body and the PR, and do not claim parity was verified.

- [ ] **Step 5: Commit** — `feat(store): PruneUserAreas prunes humans.area and every profile in one transaction` (Refs #243).

---

### Task 7: Config flag + Discord reconciliation prunes through `PermittedAreas`

**Files:**
- Modify: `processor/internal/config/config.go:62-75`
- Create: `processor/internal/bot/reconcile_prune.go`
- Test: `processor/internal/bot/reconcile_prune_test.go`
- Modify: `processor/internal/discordbot/reconciliation.go:433-444`
- Modify: `processor/internal/bot/area_logic.go` (delete `BuildPermittedSet`)
- Test: `processor/internal/discordbot/reconciliation_prune_test.go` (create)

**Interfaces:**
- Consumes: `bot.PermittedAreas`, `bot.PermittedAreaKeys`, `HumanStore.PruneUserAreas`, `HumanStore.PruneOverrideAreas`, `(*Reconciliation).fences()`.
- Produces:
  - `config.ReconciliationDiscordConfig.PruneUserAreas bool \`toml:"prune_user_areas"\``, and the same field on `ReconciliationTelegramConfig`.
  - `bot.PruneForMembership(hs store.HumanStore, fences []geofence.Fence, cfg *config.Config, human *store.Human, admin bool, opts PruneOptions) (removedUserAreas []string, err error)` with `type PruneOptions struct { Overrides, UserAreas bool }`.

- [ ] **Step 1: Write the failing tests.** `processor/internal/bot/reconcile_prune_test.go`:

```go
package bot

import (
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func pruneFixture() ([]geofence.Fence, *config.Config, *store.MockHumanStore, *store.Human) {
	fences := []geofence.Fence{
		{Name: "alpha", UserSelectable: true},
		{Name: "beta", UserSelectable: true},
		{Name: "priv", UserSelectable: false}, // granted via trusted, in coast
		{Name: "drawn", UserSelectable: false, ExemptFromAreaSecurity: true},
		{Name: "gent_centrum", UserSelectable: true},
	}
	cfg := &config.Config{}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{
		{Name: "coast", AllowedAreas: []string{"alpha", "priv", "gent_centrum"}},
		{Name: "harbour", AllowedAreas: []string{"beta"}},
	}
	hs := store.NewMockHumanStore()
	all := []string{"alpha", "beta", "priv", "drawn", "gent_centrum"}
	hs.AddHuman(&store.Human{ID: "u1", CurrentProfileNo: 1, Area: all, CommunityMembership: []string{"coast", "harbour"}})
	hs.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: all})
	// The human as reconciliation sees it AFTER the change: harbour lost.
	after := &store.Human{ID: "u1", CommunityMembership: []string{"coast"}}
	return fences, cfg, hs, after
}

// prune_user_areas on: harbour's beta goes; coast's areas, the trusted-granted
// non-selectable priv (Review Focus 2), the exempt drawn fence, and the
// underscore fence (Review Focus 3) all stay.
func TestPruneForMembership_UserAreasOn(t *testing.T) {
	fences, cfg, hs, after := pruneFixture()
	removed, err := PruneForMembership(hs, fences, cfg, after, false, PruneOptions{Overrides: true, UserAreas: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(removed, []string{"beta"}) {
		t.Fatalf("removed = %v, want [beta]", removed)
	}
	h, _ := hs.Get("u1")
	if !slices.Equal(h.Area, []string{"alpha", "priv", "drawn", "gent_centrum"}) {
		t.Errorf("humans.area = %v", h.Area)
	}
	// override_areas are keyed with spaces: "gent centrum" must be permitted.
	p := hs.PrunedAreaCalls["u1"]
	for _, k := range []string{"alpha", "priv", "drawn", "gent centrum", "gent_centrum"} {
		if !p[k] {
			t.Errorf("override permitted set missing %q: %v", k, p)
		}
	}
	if p["beta"] {
		t.Errorf("override permitted set must not hold beta: %v", p)
	}
}

// Default (prune_user_areas off): humans.area is untouched, but the
// override_areas prune still runs, as it does today.
func TestPruneForMembership_UserAreasOff(t *testing.T) {
	fences, cfg, hs, after := pruneFixture()
	if _, err := PruneForMembership(hs, fences, cfg, after, false, PruneOptions{Overrides: true}); err != nil {
		t.Fatal(err)
	}
	h, _ := hs.Get("u1")
	if len(h.Area) != 5 {
		t.Errorf("humans.area pruned with prune_user_areas off: %v", h.Area)
	}
	if hs.PrunedAreaCalls["u1"] == nil {
		t.Error("override prune must still run")
	}
}

// Review Focus 1: no fences loaded must never wipe anything.
func TestPruneForMembership_NoFencesSkipsEverything(t *testing.T) {
	_, cfg, hs, after := pruneFixture()
	removed, err := PruneForMembership(hs, nil, cfg, after, false, PruneOptions{Overrides: true, UserAreas: true})
	if err != nil || removed != nil {
		t.Fatalf("removed=%v err=%v, want a skip", removed, err)
	}
	h, _ := hs.Get("u1")
	if len(h.Area) != 5 || hs.PrunedAreaCalls["u1"] != nil {
		t.Fatalf("pruned with no fences: area=%v overrides=%v", h.Area, hs.PrunedAreaCalls["u1"])
	}
}

// Review Focus 5: an admin is never community-filtered, so never pruned.
func TestPruneForMembership_AdminKeepsAll(t *testing.T) {
	fences, cfg, hs, after := pruneFixture()
	removed, _ := PruneForMembership(hs, fences, cfg, after, true, PruneOptions{Overrides: true, UserAreas: true})
	if len(removed) != 0 {
		t.Fatalf("admin pruned %v", removed)
	}
}
```

`processor/internal/discordbot/reconciliation_prune_test.go` covers Review Focus 4, the wiring: the prune judges the new membership, and the config flag reaches it.

```go
package discordbot

import (
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func newPruneReconciliation(t *testing.T, prune bool) (*Reconciliation, *store.MockHumanStore) {
	t.Helper()
	cfg := minimalConfig()
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{
		{Name: "coast", AllowedAreas: []string{"alpha"}, Discord: config.CommunityDiscordConfig{UserRole: []string{"r-coast"}}},
		{Name: "harbour", AllowedAreas: []string{"beta"}, Discord: config.CommunityDiscordConfig{UserRole: []string{"r-harbour"}}},
	}
	cfg.Reconciliation.Discord.PruneUserAreas = prune
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: []geofence.Fence{
		{Name: "alpha", UserSelectable: true},
		{Name: "beta", UserSelectable: true},
		{Name: "drawn", UserSelectable: false, ExemptFromAreaSecurity: true},
	}})
	hs := store.NewMockHumanStore()
	hs.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "n", Enabled: true, CurrentProfileNo: 1,
		Area: []string{"alpha", "beta", "drawn"}, CommunityMembership: []string{"coast", "harbour"}})
	hs.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha", "beta", "drawn"}})
	return NewReconciliation(nil, hs, cfg, nil, nil, mgr), hs
}

func TestReconcileAreaSecurity_LosingCommunityPrunesWhenEnabled(t *testing.T) {
	r, hs := newPruneReconciliation(t, true)
	user, _ := hs.Get("u1")
	r.reconcileAreaSecurity("u1", user, "n", []string{"r-coast"}, nil, false, false)
	h, _ := hs.Get("u1")
	if !slices.Equal(h.Area, []string{"alpha", "drawn"}) {
		t.Fatalf("humans.area = %v, want [alpha drawn]", h.Area)
	}
}

func TestReconcileAreaSecurity_DefaultLeavesHumanAreas(t *testing.T) {
	r, hs := newPruneReconciliation(t, false)
	user, _ := hs.Get("u1")
	r.reconcileAreaSecurity("u1", user, "n", []string{"r-coast"}, nil, false, false)
	h, _ := hs.Get("u1")
	if len(h.Area) != 3 {
		t.Fatalf("humans.area pruned without prune_user_areas: %v", h.Area)
	}
	if hs.PrunedAreaCalls["u1"] == nil || hs.PrunedAreaCalls["u1"]["beta"] {
		t.Fatalf("override prune must still run against the new membership: %v", hs.PrunedAreaCalls["u1"])
	}
}
```

Check the actual names of the community Discord sub-struct and its `UserRole` field in `config.CommunityConfig` before running this. `reconcileAreaSecurity` reads `comm.Discord.UserRole`, so the field exists, but the type name may differ. Use whatever `config.go` declares.

- [ ] **Step 2: Run and confirm failure** — `go test ./internal/bot/ ./internal/discordbot/ -run 'PruneForMembership|ReconcileAreaSecurity' -count=1`. Expected: build failure (`PruneForMembership`, `PruneUserAreas` field undefined).

- [ ] **Step 3: Implement.** In `config.go`, add to **both** reconciliation config structs:

```go
	// PruneUserAreas removes areas a membership change no longer permits from
	// humans.area and every profile. Default false: it destroys user-entered
	// data, and an incomplete allowed_areas would empty a whole community.
	// Exempt fences are never pruned (#243).
	PruneUserAreas bool `toml:"prune_user_areas"`
```

Create `processor/internal/bot/reconcile_prune.go`:

```go
package bot

import (
	"errors"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// PruneOptions selects which prunes PruneForMembership runs.
type PruneOptions struct {
	// Overrides prunes per-rule override_areas. Low-value derived data whose
	// loss is visible and self-correcting, so callers run it unconditionally.
	Overrides bool
	// UserAreas prunes humans.area and every profile. Irreversible, so it is
	// gated behind [reconciliation.*] prune_user_areas (default false).
	UserAreas bool
}

// PruneForMembership removes, after a community-membership change, every
// area the human may no longer hold, judged by PermittedAreas so the prune
// can never undo a grant. human must carry the NEW membership and
// area_restriction.
//
// trusted is passed as true: prunes act only on the community dimension, so
// a non-userSelectable fence granted via trusted survives. No fences ⇒ skip
// entirely: an empty list means the load failed, not that nothing is allowed.
func PruneForMembership(hs store.HumanStore, fences []geofence.Fence, cfg *config.Config,
	human *store.Human, admin bool, opts PruneOptions) ([]string, error) {
	if len(fences) == 0 || human == nil {
		return nil, nil
	}
	permitted := PermittedAreaKeys(PermittedAreas(fences, cfg, human, admin, true))

	var errs []error
	if opts.Overrides {
		if err := hs.PruneOverrideAreas(human.ID, permitted); err != nil {
			errs = append(errs, err)
		}
	}
	var removed []string
	if opts.UserAreas {
		r, err := hs.PruneUserAreas(human.ID, permitted)
		if err != nil {
			errs = append(errs, err)
		}
		removed = r
	}
	return removed, errors.Join(errs...)
}
```

In `discordbot/reconciliation.go`, replace the `if communityChanged { ... BuildPermittedSet ... }` block:

```go
		// A membership change may leave areas this human can no longer hold.
		// Pruned through PermittedAreas so exempt and trusted-granted fences
		// survive. override_areas always; humans.area only when the operator
		// has asserted their community config is complete (#243).
		if communityChanged {
			fences := r.fences()
			if len(fences) == 0 {
				r.log.Warnf("prune areas for %s skipped: no geofences loaded", id)
			} else {
				after := &store.Human{ID: id, CommunityMembership: communityList, AreaRestriction: areaRestriction}
				removed, err := bot.PruneForMembership(r.humanStore, fences, r.cfg, after,
					slices.Contains(r.cfg.Discord.Admins, id),
					bot.PruneOptions{Overrides: true, UserAreas: r.cfg.Reconciliation.Discord.PruneUserAreas})
				if err != nil {
					r.log.Errorf("prune areas for %s: %v", id, err) // best effort — don't abort reconciliation
				}
				if len(removed) > 0 {
					r.log.Infof("Pruned areas %v from %s after community change", removed, id)
				}
			}
		}
```

Confirm that the config root field is named `Reconciliation`, of type `ReconciliationConfig`, and adjust if not. Then delete `BuildPermittedSet` from `area_logic.go`; `grep -rn BuildPermittedSet processor/` must print nothing. `slices` is already imported in this file.

**Note the precondition.** The existing code only reaches this block on the `before && after` path. When membership goes to zero (`after == false`), the user is disabled instead, and nothing is pruned. That is unchanged and correct.

- [ ] **Step 4: Run tests + gate.** Expected: PASS.

- [ ] **Step 5: Commit** — `feat(reconciliation): opt-in prune_user_areas; prunes go through PermittedAreas` (body: override prune unchanged in gating but now honours exempt + trusted-granted fences and the space-keyed override spelling; empty fence list skips; Refs #243).

---

### Task 8: Telegram reconciliation honours `prune_user_areas`

**Files:**
- Modify: `processor/internal/telegrambot/reconciliation.go` (`reconcileAreaSecurity` `before && after` block, ~:300-327)
- Test: `processor/internal/telegrambot/reconciliation_prune_test.go` (create)

**Interfaces:**
- Consumes: `bot.PruneForMembership`, `(*TelegramReconciliation).fences()`, `cfg.Reconciliation.Telegram.PruneUserAreas`.

Telegram reconciliation has **never** pruned `override_areas`. This task adds only the opt-in `humans.area` prune the design specifies (`Overrides: false`). Do not add the override prune here. That would be new unconditional behaviour on Telegram, which no one has agreed. Record the asymmetry in a code comment, and in the PR description as a candidate follow-up issue.

- [ ] **Step 1: Write the failing test**

```go
package telegrambot

import (
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func newTgPrune(t *testing.T, prune bool) (*TelegramReconciliation, *store.MockHumanStore) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{
		{Name: "coast", AllowedAreas: []string{"alpha"}, Telegram: config.CommunityTelegramConfig{Channels: []string{"c-coast"}}},
		{Name: "harbour", AllowedAreas: []string{"beta"}, Telegram: config.CommunityTelegramConfig{Channels: []string{"c-harbour"}}},
	}
	cfg.Reconciliation.Telegram.PruneUserAreas = prune
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: []geofence.Fence{
		{Name: "alpha", UserSelectable: true}, {Name: "beta", UserSelectable: true},
		{Name: "drawn", ExemptFromAreaSecurity: true},
	}})
	hs := store.NewMockHumanStore()
	hs.AddHuman(&store.Human{ID: "7", Type: "telegram:user", Name: "n", Enabled: true, CurrentProfileNo: 1,
		Area: []string{"alpha", "beta", "drawn"}, CommunityMembership: []string{"coast", "harbour"}})
	hs.SeedProfile(store.Profile{ID: "7", ProfileNo: 1, Area: []string{"alpha", "beta", "drawn"}})
	return NewTelegramReconciliation(nil, hs, cfg, nil, nil, mgr), hs
}

func TestTelegramReconcile_PrunesWhenEnabled(t *testing.T) {
	r, hs := newTgPrune(t, true)
	u, _ := hs.Get("7")
	r.reconcileAreaSecurity("7", u, "n", []string{"c-coast"}, false, false)
	h, _ := hs.Get("7")
	if !slices.Equal(h.Area, []string{"alpha", "drawn"}) {
		t.Fatalf("humans.area = %v, want [alpha drawn]", h.Area)
	}
	if hs.PrunedAreaCalls["7"] != nil {
		t.Fatal("telegram must not gain an override prune in this change")
	}
}

func TestTelegramReconcile_DefaultLeavesAreas(t *testing.T) {
	r, hs := newTgPrune(t, false)
	u, _ := hs.Get("7")
	r.reconcileAreaSecurity("7", u, "n", []string{"c-coast"}, false, false)
	if h, _ := hs.Get("7"); len(h.Area) != 3 {
		t.Fatalf("humans.area pruned without prune_user_areas: %v", h.Area)
	}
}
```

Use the real names of the community Telegram sub-struct and its channels field from `config.go`. `reconcileAreaSecurity` reads `comm.Telegram.Channels`.

- [ ] **Step 2: Run, confirm FAIL** (`humans.area = [alpha beta drawn]`).

- [ ] **Step 3: Implement.** In Telegram `reconcileAreaSecurity`, hoist `communityChanged := !bot.HaveSameContents(communityList, user.CommunityMembership)`, use it in the existing `community_membership` check, and after the `if len(updates) > 0 { ... }` block add:

```go
		// Opt-in humans.area prune (#243), through PermittedAreas so exempt
		// fences survive. Telegram has never pruned override_areas; Discord
		// does. That asymmetry predates #243 and is deliberately not changed here.
		if communityChanged && r.cfg.Reconciliation.Telegram.PruneUserAreas {
			fences := r.fences()
			if len(fences) == 0 {
				r.log.Warnf("prune areas for %s skipped: no geofences loaded", id)
			} else {
				after := &store.Human{ID: id, CommunityMembership: communityList, AreaRestriction: areaRestriction}
				removed, err := bot.PruneForMembership(r.humanStore, fences, r.cfg, after,
					slices.Contains(r.cfg.Telegram.Admins, id), bot.PruneOptions{UserAreas: true})
				if err != nil {
					r.log.Errorf("prune areas for %s: %v", id, err)
				}
				if len(removed) > 0 {
					r.log.Infof("Pruned areas %v from %s after community change", removed, id)
				}
			}
		}
```

Add a `slices` import if it is missing.

- [ ] **Step 4: Run tests + gate.** Expected: PASS.

- [ ] **Step 5: Commit** — `feat(telegram): honour prune_user_areas on community change` (Refs #243).

---

### Task 9: `GET /v2/humans/{id}/areas` reports the flag (D4)

**Files:**
- Modify: `processor/internal/api/v2_humans.go` (`v2AreaInfo` :190-195, handler :205-246)
- Modify: `processor/internal/api/testdata/openapi.golden.json` (regenerated)
- Test: `processor/internal/api/v2_humans_test.go`

**Interfaces:**
- Consumes: `bot.PermittedAreas`.
- Produces: `v2AreaInfo.ExemptFromAreaSecurity bool \`json:"exemptFromAreaSecurity"\``.

The listing is the set a **trusted** caller could select: every fence for an unrestricted human, community-filtered otherwise, with exempt fences included under area_security. That is `PermittedAreas(…, admin, trusted=true)`. It replaces the handler's inline `community.FilterAreas` call, so all three v2 surfaces now share one definition. Including exempt fences under area_security is what makes D4 work: without it, the field would only ever be reported for fences that do not need it.

- [ ] **Step 1: Write the failing test**

```go
// D4: the listing reports exemptFromAreaSecurity, and under area_security
// includes exempt fences so a client can explain why they behave differently.
func TestV2Humans_AreasGet_ReportsExempt(t *testing.T) {
	r, _ := newCoastOnlyAPI(t) // private is drawn+exempt, in no community
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/areas", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	exempt := map[string]bool{}
	for _, m := range v2RulesArray(t, v2DecodeBody(t, w), "areas") {
		name, _ := m["name"].(string)
		ex, ok := m["exemptFromAreaSecurity"].(bool)
		if !ok {
			t.Fatalf("area %q has no exemptFromAreaSecurity field: %v", name, m)
		}
		exempt[name] = ex
	}
	if _, listed := exempt["private"]; !listed || !exempt["private"] {
		t.Errorf("private must be listed and exempt: %v", exempt)
	}
	if _, listed := exempt["gamma"]; listed {
		t.Errorf("gamma is another community's fence and must not be listed: %v", exempt)
	}
	if _, listed := exempt["lone"]; listed {
		t.Errorf("lone is in no community and not exempt; must not be listed: %v", exempt)
	}
	if exempt["alpha"] {
		t.Errorf("alpha is not exempt: %v", exempt)
	}
}
```

- [ ] **Step 2: Run, confirm FAIL** (no `exemptFromAreaSecurity` field; `private` not listed).

- [ ] **Step 3: Implement.** Add the field to `v2AreaInfo`, with a huma `doc:` tag, e.g. `doc:"The fence is outside the community system: area_security does not filter it. Does not imply userSelectable, and does not affect strict_locations delivery."`. Replace the handler body after `resolveFullHuman`:

```go
		st := deps.StateMgr.Get()
		listed := make(map[string]bool)
		for _, a := range bot.PermittedAreas(st.Fences, deps.Config, human, isAdmin(deps, in.ID), true) {
			listed[a] = true
		}
		out := &v2AreasOutput{}
		out.Body.Areas = []v2AreaInfo{}
		for _, f := range st.Fences {
			if listed[strings.ToLower(f.Name)] {
				out.Body.Areas = append(out.Body.Areas, v2AreaInfo{
					Name:                   f.Name,
					Group:                  f.Group,
					Description:            f.Description,
					UserSelectable:         f.UserSelectable,
					ExemptFromAreaSecurity: f.ExemptFromAreaSecurity,
				})
			}
		}
		return out, nil
```

Update the operation `Description` to say the listing is what a trusted caller may select, and that it includes exempt fences under area_security. If `community` and `lowerFenceNames` are now unused in the package, delete them; the linter will flag them (`lowerFenceNames` is still used by create).

- [ ] **Step 4: Regenerate the golden and run the gate**

```bash
UPDATE_GOLDEN=1 go test ./internal/api/ -run Golden -count=1
git diff --stat processor/internal/api/testdata/openapi.golden.json   # expect only the areas schema/description
```

Then the full gate. Expected: PASS.

- [ ] **Step 5: Commit** — `feat(api): GET /v2/humans/{id}/areas reports exemptFromAreaSecurity` (Refs #243).

---

### Task 10: Delivery is pinned unchanged; docs and config editor

**Files:**
- Test: `processor/internal/matching/generic_override_test.go`
- Modify: `processor/internal/api/config_schema.go:497, :511`
- Modify: `config/config.example.toml:657, :665`
- Modify: `API.md` (geofence format, ~:429; v2 areas response)
- Modify: `CLAUDE.md:422`
- Modify: `docs/superpowers/specs/2026-05-25-per-rule-location-area-overrides-design.md:155`

- [ ] **Step 1: Pin delivery.** Append to `generic_override_test.go`:

```go
// #243 D3: exemptFromAreaSecurity lifts the community filter on SELECTION
// only. A human holding an exempt fence, alerted inside it but outside their
// area_restriction, gets nothing when strict_locations is on. Matching never
// reads the flag; this test fails if someone wires it in.
func TestStrictLocations_IgnoresExemptSelection(t *testing.T) {
	h := newHuman(0, 0, []string{"drawn"}, nil)
	h.AreaRestriction = []string{"coast_fence"}
	humans := map[string]*db.Human{"u1": h}
	rules := []trackingUserData{newRule(1, 0, "", nil)}
	matched := map[string]bool{"drawn": true} // event inside the exempt fence only

	if out := ValidateHumansGeneric(rules, 0, 0, matched, true, humans, "test"); len(out) != 0 {
		t.Fatalf("strict_locations must still gate an exempt fence; got %d matches", len(out))
	}
	if out := ValidateHumansGeneric(rules, 0, 0, matched, false, humans, "test"); len(out) != 1 {
		t.Fatalf("without strict_locations the selection alone delivers (the documented sharp edge); got %d", len(out))
	}
}
```

Run: `go test ./internal/matching/ -run IgnoresExempt -count=1`. Expected: PASS immediately. This is a pin, not a red test, and that is acceptable because it guards an *unchanged* behaviour. Also run `grep -rn ExemptFromAreaSecurity processor/internal/matching/` and expect only this test.

- [ ] **Step 2: Config editor + example config.** In `config_schema.go`, add after each `remove_invalid_users` entry:

```go
			{Name: "prune_user_areas", Type: "bool", Default: false, Description: "On a community change, remove areas the user may no longer select from their area list and every profile. Irreversible: a wrong or incomplete allowed_areas empties that community's members, and fixing the config does not restore them. Exempt fences are never removed"},
```

Match the neighbouring entries' fields exactly. If they carry `HotReload`, copy their value.

In `config.example.toml`, under `[reconciliation.discord]` and `[reconciliation.telegram]`:

```toml
prune_user_areas = false                 # on a community change, delete areas the user may no longer select from
                                         # humans.area and every profile. IRREVERSIBLE: an incomplete allowed_areas
                                         # empties that community's members, and fixing the config won't restore them.
                                         # Fences marked exemptFromAreaSecurity are never pruned.
```

If there is a config-schema test that asserts every TOML key has a schema entry, run it now.

- [ ] **Step 3: API.md.** In the geofence format section, document `exemptFromAreaSecurity` next to `userSelectable` (default false; read in native and GeoJSON). Include the sharp edge **verbatim in substance**:

> **Known sharp edge.** On an instance with `area_security.enabled = true` and `strict_locations = false` (the default), there is no delivery-time gate, so selection *is* enforcement. A fence marked `exemptFromAreaSecurity` is selectable by any user regardless of community, and they will receive its alerts. Set `strict_locations = true` if delivery must stay community-gated.

Also state that the flag does not imply `userSelectable` (a non-selectable exempt fence still needs `trusted: true`), and that it does not affect `strict_locations`. Add `exemptFromAreaSecurity` to the documented `GET /api/v2/humans/{id}/areas` element. Add `prune_user_areas` wherever API.md or README documents reconciliation options (`grep -n remove_invalid_users API.md README.md`).

- [ ] **Step 4: CLAUDE.md and the stale spec.** Replace `CLAUDE.md:422` ("`AreaLogic.ValidateAndPrune` removes areas…") with:

> `bot.PermittedAreas` is the single definition of the areas a human may hold. It is used by API set-areas, `override_areas` validation, and both reconcilers' prunes; `!area` shares its community predicates. Fences with `exemptFromAreaSecurity` bypass the community filter (not `userSelectable`, not `strict_locations`). Reconciliation prunes `override_areas` on Discord community change, and prunes `humans.area` + every profile only when `[reconciliation.*] prune_user_areas = true` (default false — irreversible).

In the 2026-05-25 spec at :155, append a correction line rather than rewriting history: `> Correction (#243): ValidateAndPrune never had a caller and was deleted; the prune runs through bot.PruneForMembership.`

- [ ] **Step 5: Gate + commit** — `docs: exemptFromAreaSecurity, prune_user_areas, and the strict_locations sharp edge` (Refs #243).

---

### Task 11: Branch verification

- [ ] **Step 1:** Run the full gate from `processor/` once more, plus the store SQL tests with `PORACLENG_TEST_DSN` exported (`go test ./internal/store/ -count=1 -v | grep -E -- "--- (SKIP|FAIL)"` should list no `PruneUserAreas` skip).
- [ ] **Step 2:** `grep -rn "t.Skip(\"RED test for #243" processor/` → nothing. `grep -rn "BuildPermittedSet\|buildAllowedSet\|ValidateAndPrune\|settableAreaNames" processor/` → nothing.
- [ ] **Step 3:** Confirm the main checkout's staged index is untouched: `git -C /Users/james/GolandProjects/PoracleNG diff --cached --stat | tail -1` still reports 24 files.
- [ ] **Step 4:** Hand off to superpowers:finishing-a-development-branch. The PR body must call out three things: (a) the community-filter gate decision and its behaviour change; (b) Telegram still has no `override_areas` prune (pre-existing; candidate follow-up issue); (c) whether the SQL parity test was run against a real MySQL.
