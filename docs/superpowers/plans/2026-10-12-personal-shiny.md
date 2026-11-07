# Personal shiny and background alerting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users link game accounts and get pokemon alerts that are shiny (or carry the event background) for *their* accounts, computed exactly as Golbat#423 computes them, plus feed-backed `shinyPossible`/`shinyRate`.

**Architecture:** A new `internal/personalshiny` package is a near-verbatim copy of Golbat#423's roll and rate-feed code with a thin webhook adapter. Player accounts live in a new `player_accounts` table, are loaded into the immutable state on each human, and a per-webhook `Checker` answers the `monsters.personal` bitmask in the pokemon matcher and fills per-user DTS fields. The same rate feed backs `shinyPossible`/`shinyRate` in enrichment, falling back to the existing observed stats.

**Tech Stack:** Go 1.27, MySQL/MariaDB (sqlx, golang-migrate SQL files), huma v2 (v2 API), gin (frozen v1), discordgo (slash), jfberry/raymond (DTS).

**Spec:** `docs/superpowers/specs/2026-10-12-personal-shiny-design.md`

**Upstream reference:** UnownHash/Golbat#423 at `13f90b3d31e2cf5245754b7715a79aff1f75b954`. A local clone with that commit checked out lives at `/tmp/shinyanalysis/golbat` (branch `pr423`). If it is missing:

```bash
git clone -q https://github.com/UnownHash/Golbat /tmp/shinyanalysis/golbat
git -C /tmp/shinyanalysis/golbat fetch -q origin pull/423/head:pr423
git -C /tmp/shinyanalysis/golbat checkout -q 13f90b3d31e2cf5245754b7715a79aff1f75b954
```

All paths below are relative to the worktree root `/Users/james/GolandProjects/PoracleNG/.worktrees/personal-shiny`. Run Go commands from `processor/`.

## Global Constraints

- Match Golbat#423 exactly for the same spawn, rates and player ID. Copied code keeps Golbat's identifiers, comments and control flow; every intentional difference carries a `// Poracle:` comment and is listed in `processor/internal/personalshiny/SYNC.md`.
- No Ditto prediction. Eligibility is Golbat's `personalLookup`: never pokemon 132, never disguised (`display_pokemon_id` set and different), never costume 255, `seen_type` only `wild` or `encounter`.
- Rate feed: `GET <url>/v1/rates?compact=1`, optional `Authorization: Bearer <token>`, 10 s timeout, redirects not followed, 4 MB limit, refresh every 5 min or 30 s after a failure, keep last good snapshot, reject a response with no `rates` list.
- Fail closed: before the first successful feed load, personal rules match nothing.
- No `[shiny_rates] url` configured ⇒ `shinyPossible`/`shinyStats`/`!info` behave exactly as today.
- `monsters.personal` bitmask: 1 = shiny, 2 = background, 4 and 8 reserved. All set bits must hold (AND).
- Player IDs: 1–256 bytes, opaque, **case-sensitive**, at most 50 per human. Labels optional, ≤ 64 chars, unique per human, case-insensitive.
- `[player_accounts] self_service = false` by default (admins/API only). Channel/webhook/group targets are admin-only regardless.
- v1 tracking stays frozen except the additive `personal` field, which is **kept** on a by-uid update that omits it.
- Do not name or reference any other community port in repo files. Attribute only Golbat#423 / #424.
- Migration number `000010` (the last one on `develop` is `000009_profile_zero_to_one`).
- i18n: add new keys to `processor/internal/i18n/locale/en.json` only; other locales fall back per key to English and are filled by Crowdin.
- Every commit passes, from `processor/`: `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`.

## Review Focus

1. **Player IDs typed with underscores, mixed case, or starting with `user`/`name`** (e.g. `!account add User_ABC`) must be stored byte-for-byte. The parser lowercases and turns `_` into spaces, and `BuildTarget` swallows any argument beginning `user`/`name`. Tests in Task 10 and Task 11.
2. **The feed is unreachable at startup, or a later refresh fails.** Personal rules match nothing until the first load, nothing crashes, `shinyPossible` falls back to observed stats, and a failed refresh keeps the old rates. Tests in Task 2, Task 6 and Task 8.
3. **An older v1 client edits a personal rule by uid without sending `personal`.** The personal bits must survive. Test in Task 14.
4. **Ineligible spawns:** non-numeric encounter IDs, a disguise revealed as Ditto on a re-send, `nearby_cell`/`nearby_stop` sightings. None of these may produce a personal match. Tests in Task 3 and Task 6.
5. **One webhook, many humans sharing a player ID** (a user and a channel both linking the same account). Each human is answered correctly, and the per-player memo never leaks one human's labels into another's fields. Tests in Task 3 and Task 7.

---

## File Structure

**New**
- `processor/internal/personalshiny/javarandom/javarandom.go`, `javarandom_test.go` — verbatim copy of Golbat `derive/javarandom`.
- `processor/internal/personalshiny/rates.go` — feed types, cutoff, refresh loop (copied).
- `processor/internal/personalshiny/roll.go` — seed/rolls, eligibility, cutoffs (copied).
- `processor/internal/personalshiny/adapter.go` — `Evaluate`, `Evaluation.For`, `Checker`, `ShinyVariant`, `Boosted`, `DisplayName` (Poracle).
- `processor/internal/personalshiny/*_test.go`, `SYNC.md`.
- `processor/internal/db/migrations/000010_personal_shiny.{up,down}.sql`
- `processor/internal/db/personal.go`, `processor/internal/db/player_accounts.go`
- `processor/internal/store/player_accounts.go`, `player_accounts_sql.go`, `mock_player_accounts.go` (+ tests)
- `processor/internal/enrichment/personal.go`, `processor/internal/enrichment/shinyinfo.go` (+ tests)
- `processor/internal/bot/commands/account.go` (+ test)
- `processor/internal/discordbot/slash/mappers/account.go` (+ test)
- `processor/internal/api/v2_accounts.go` (+ test)

**Modified (main touch points)**
- `internal/geo/timezone.go` (raw zone lookup), `internal/config/config.go`, `config/config.example.toml`, `internal/api/config_schema.go`
- `internal/db/monsters.go`, `internal/db/tracking_queries.go`, `internal/db/humans.go`, `internal/db/human_queries.go`, `internal/store/human_sql.go` (one list entry), `internal/state/loader.go`
- `internal/matching/pokemon.go`, `cmd/processor/pokemon.go`, `cmd/processor/main.go`
- `internal/enrichment/{enrichment,pokemon,raid,quest,nest,maxbattle}.go`, `internal/api/dts_fields.go`
- `internal/bot/{parser,command,target}.go`, `internal/discordbot/bot.go`, `internal/telegrambot/bot.go`, `internal/api/huma_features.go`, `internal/discordbot/slash/{dispatcher,definitions}.go`, `internal/discordbot/slash/mappers/track.go`
- `internal/bot/commands/{track,conversions,script,info,poracle_admin_status}.go`, `internal/bot/argmatch.go`, `internal/rowtext/monster.go`, `internal/discordbot/slash/autocomplete/listers/tracking.go`
- `internal/api/{trackingMonster,v2_pokemon,v2_snapshot,api}.go`, `internal/api/testdata/openapi.golden.json`
- Docs: `API.md`, `DTS.md`, `CLAUDE.md`, `CHANGELOG.md`, the spec (small corrections in Task 1).

---

### Task 1: Copy Golbat's `javarandom` and correct the spec's file table

**Files:**
- Create: `processor/internal/personalshiny/javarandom/javarandom.go` (copy)
- Create: `processor/internal/personalshiny/javarandom/javarandom_test.go` (copy)
- Modify: `docs/superpowers/specs/2026-10-12-personal-shiny-design.md`

**Interfaces:**
- Produces: package `javarandom` with `New(seed uint64) *Random`, `(*Random).Next(bits uint) uint32` (plus Golbat's `Int`, `Float`, `Double`, `Gaussian`, unused by Poracle but kept so the files diff clean).

- [ ] **Step 1: Copy the files verbatim**

```bash
mkdir -p processor/internal/personalshiny/javarandom
cp /tmp/shinyanalysis/golbat/derive/javarandom/javarandom.go processor/internal/personalshiny/javarandom/
cp /tmp/shinyanalysis/golbat/derive/javarandom/javarandom_test.go processor/internal/personalshiny/javarandom/
diff -r /tmp/shinyanalysis/golbat/derive/javarandom processor/internal/personalshiny/javarandom && echo identical
```
Expected: `identical`. The package name is already `javarandom` and the files import only the standard library.

- [ ] **Step 2: Run the copied tests**

Run: `cd processor && go test -count=1 ./internal/personalshiny/javarandom/`
Expected: `ok`.

- [ ] **Step 3: Correct the spec's file table and schema**

In `docs/superpowers/specs/2026-10-12-personal-shiny-design.md`:
- In the `personalshiny` table, replace the `javarandom.go` row with:
  `| javarandom/ (subpackage) | derive/javarandom/ | verbatim copy, both files (package name unchanged), so it diffs clean |`
- In the `player_accounts` SQL, replace `` `player_id`  VARCHAR(256) NOT NULL, `` with `` `player_id`  VARBINARY(256) NOT NULL, `` and `` UNIQUE KEY `uniq_id_player` (`id`, `player_id`(191)), `` with `` UNIQUE KEY `uniq_id_player` (`id`, `player_id`), ``. Add this bullet under the SQL: "`player_id` is `VARBINARY` so uniqueness and lookups are case-sensitive and byte-exact under any table collation; 50×4 + 256 bytes keeps the unique key under InnoDB's 767-byte limit on older MariaDB."
- In "Public API", replace `func (e *Evaluation) For(accounts []Account) Result` and `type Account …` with:
  ```go
  func (e *Evaluation) For(accounts []db.PlayerAccount) Result
  type Result struct{ Shiny, Background []db.PlayerAccount }
  type Checker struct{ … }   // per webhook: Evaluation + state humans, memoised per human
  func NewChecker(ev *Evaluation, humans map[string]*db.Human) *Checker
  func (c *Checker) Allows(humanID string, bits int) bool
  func (c *Checker) Result(humanID string) (Result, bool)
  ```
- In "Commands → `!account`", replace the `VerbatimArgs` bullet with: "**Player IDs must survive parsing verbatim.** `ParsedCommand` gains `VerbatimArgs` (original case, no underscore substitution), copied to `CommandContext.VerbatimArgs`; `!account` reads IDs and labels from it. `BuildTarget` strips any argument beginning `user`/`name`, so hosts call `BuildTargetFor`, which for `cmd.account` only recognises `user:<id>` / `name:<name>`."

- [ ] **Step 4: Commit**

```bash
git add processor/internal/personalshiny/javarandom docs/superpowers/specs/2026-10-12-personal-shiny-design.md
git commit -m "personalshiny: copy Golbat's javarandom verbatim (Golbat#423)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Rate feed (`rates.go`) and raw timezone lookup

**Files:**
- Modify: `processor/internal/geo/timezone.go`
- Create: `processor/internal/personalshiny/rates.go`
- Test: `processor/internal/personalshiny/rates_test.go`, `processor/internal/geo/timezone_test.go` (append)

**Interfaces:**
- Consumes: `javarandom` (Task 1, not used here yet).
- Produces:
  - `geo.TimezoneName(lat, lon float64) string` — IANA name or `""`, no UTC fallback.
  - `personalshiny.NewRates(url, token string) *Rates`, `(*Rates).Start(ctx context.Context)`, `(*Rates).Stop()`, `(*Rates).Loaded() bool`, `(*Rates).Status() Status`.
  - `type Status struct { Configured, Loaded bool; Variants int; LastSuccess, LastAttempt time.Time; LastError string }`
  - Unexported (same package, used by Task 3): `shinyScale`, `defaultShinyCutoff`, `shinyPrediction` (+ `cutoff`), `shinyVariant`, `shinyRate`, `shinyRates`, `(*Rates).snapshot` (`atomic.Pointer[shinyRates]`), `(*Rates).zone` (`func(lat, lon float64) string`), `(*Rates).refreshShinyRates(ctx) error`.

- [ ] **Step 1: Write the failing tests**

Append to `processor/internal/geo/timezone_test.go`:

```go
func TestTimezoneNameHasNoUTCFallback(t *testing.T) {
	if got := TimezoneName(33.45, -112.07); got != "America/Phoenix" {
		t.Fatalf("Phoenix = %q", got)
	}
	// Open ocean has no zone polygon in the default finder's data; Golbat's
	// tz.SearchTimezone returns "" there and so must this.
	if got := TimezoneName(0, -140); got != "" && got != "Etc/GMT+9" {
		t.Fatalf("ocean = %q, want empty (or the Etc zone tzf assigns)", got)
	}
	if got := GetTimezone(0, -140); got == "" {
		t.Fatal("GetTimezone must keep its UTC fallback")
	}
}
```

Create `processor/internal/personalshiny/rates_test.go`:

```go
package personalshiny

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testFeed = `{"rates":[{"pokemon":25,"form":0,"costume":0,"prediction":{"probability":0.1,"supported":true},"overrides":{"America/Phoenix":{"probability":0.05,"supported":true}},"background":{"probability":0.2,"supported":true},"background_overrides":{"America/Phoenix":{"probability":0.15,"supported":true}}}]}`

func TestRefreshLoadsFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rates" || r.URL.Query().Get("compact") != "1" || r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("incorrect rates request %s %q", r.URL, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(testFeed))
	}))
	defer server.Close()
	r := NewRates(server.URL+"/", "test")
	if r.Loaded() {
		t.Fatal("loaded before refresh")
	}
	if err := r.refreshShinyRates(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.Loaded() {
		t.Fatal("not loaded after refresh")
	}
	st := r.Status()
	if !st.Configured || !st.Loaded || st.Variants != 1 || st.LastError != "" || st.LastSuccess.IsZero() {
		t.Fatalf("status = %+v", st)
	}
}

func TestShinyRatesRejectMissingList(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"error":"down"}`, `{"rates":null}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		r := NewRates(server.URL, "")
		good := &shinyRates{}
		r.snapshot.Store(good)
		if err := r.refreshShinyRates(context.Background()); err == nil {
			t.Errorf("%s: accepted", body)
		}
		if r.snapshot.Load() != good {
			t.Errorf("%s: replaced the last good snapshot", body)
		}
		if r.Status().LastError == "" {
			t.Errorf("%s: last error not recorded", body)
		}
		server.Close()
	}
}

func TestRefreshKeepsSnapshotOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer server.Close()
	r := NewRates(server.URL, "")
	good := &shinyRates{}
	r.snapshot.Store(good)
	if err := r.refreshShinyRates(context.Background()); err == nil {
		t.Fatal("502 accepted")
	}
	if r.snapshot.Load() != good {
		t.Fatal("replaced the last good snapshot")
	}
}

func TestRefreshDoesNotFollowRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(testFeed)) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.String(), http.StatusFound)
	}))
	defer redirect.Close()
	r := NewRates(redirect.URL, "secret")
	if err := r.refreshShinyRates(context.Background()); err == nil {
		t.Fatal("redirect followed (and would leak the bearer token)")
	}
}

func TestNilAndUnconfiguredRates(t *testing.T) {
	var nilRates *Rates
	if nilRates.Loaded() || nilRates.Status().Configured {
		t.Fatal("nil Rates must report unconfigured and unloaded")
	}
	nilRates.Start(context.Background()) // must not panic
	nilRates.Stop()
	r := NewRates("", "")
	r.Start(context.Background()) // no URL: no goroutine
	r.Stop()
	if r.Status().Configured {
		t.Fatal("empty URL reported as configured")
	}
}

func TestCutoff(t *testing.T) {
	for _, tc := range []struct {
		p    shinyPrediction
		fb   uint32
		want uint32
	}{
		{shinyPrediction{Probability: 1.0 / 512, Supported: true}, defaultShinyCutoff, 32768},
		{shinyPrediction{Probability: 0.04, Supported: true}, defaultShinyCutoff, 671089},
		{shinyPrediction{Probability: 0.5, Supported: false}, defaultShinyCutoff, 0},
		{shinyPrediction{Probability: -1, Supported: true}, defaultShinyCutoff, defaultShinyCutoff},
		{shinyPrediction{Probability: 2, Supported: true}, 0, 0},
	} {
		if got := tc.p.cutoff(tc.fb); got != tc.want {
			t.Errorf("cutoff(%+v, %d) = %d, want %d", tc.p, tc.fb, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd processor && go test -count=1 ./internal/geo/ ./internal/personalshiny/`
Expected: compile errors (`undefined: TimezoneName`, `undefined: NewRates`).

- [ ] **Step 3: Add `geo.TimezoneName`**

In `processor/internal/geo/timezone.go`, directly after `GetTimezone`:

```go
// TimezoneName returns the IANA zone containing (lat, lon), or "" when the
// finder has none. Unlike GetTimezone it has no UTC fallback: personal shiny
// rate overrides are keyed by zone name exactly as Golbat's
// tz.SearchTimezone returns it, and "" must not match a "UTC" key.
func TimezoneName(lat, lon float64) string {
	return finder.GetTimezoneName(lon, lat)
}
```

- [ ] **Step 4: Write `rates.go`**

Create `processor/internal/personalshiny/rates.go`. Everything not marked `Poracle:` is copied from `/tmp/shinyanalysis/golbat/decoder/api_pokemon_shiny.go` — check it side by side.

```go
// Package personalshiny predicts per-player shinies and event backgrounds
// for pokemon webhooks. It is a copy of Golbat's personal shiny scan
// (UnownHash/Golbat#423); see SYNC.md for the file mapping, the pinned
// upstream commit and the list of intentional differences. Keep Golbat's
// names, comments and control flow so the two can be diffed.
package personalshiny

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/geo"
)

const shinyScale = 1 << 24
const defaultShinyCutoff = shinyScale / 512

type shinyPrediction struct {
	Probability float64 `json:"probability"`
	Supported   bool    `json:"supported"`
}

// cutoff converts a probability to a threshold on a 24-bit roll. Invalid
// probabilities use fallback: 1/512 for shiny, never for backgrounds.
func (p shinyPrediction) cutoff(fallback uint32) uint32 {
	if !p.Supported {
		return 0
	}
	if math.IsNaN(p.Probability) || p.Probability < 0 || p.Probability > 1 {
		return fallback
	}
	return uint32(math.Ceil(p.Probability * shinyScale))
}

type shinyVariant struct{ Pokemon, Form, Costume int }
type shinyRate struct {
	Pokemon             int                        `json:"pokemon"`
	Form                int                        `json:"form"`
	Costume             int                        `json:"costume"`
	Prediction          shinyPrediction            `json:"prediction"`
	Overrides           map[string]shinyPrediction `json:"overrides"`
	Background          *shinyPrediction           `json:"background"`
	BackgroundOverrides map[string]shinyPrediction `json:"background_overrides"`
}
type shinyRates struct {
	variants          map[shinyVariant]shinyRate
	maximum           map[int16]uint32
	backgroundMaximum map[int16]uint32
}

// Status is a point-in-time view of the feed for admin status output.
// Poracle: Golbat only logs refresh outcomes.
type Status struct {
	Configured  bool
	Loaded      bool
	Variants    int
	LastSuccess time.Time
	LastAttempt time.Time
	LastError   string
}

// Rates owns the current rate snapshot and its refresher.
// Poracle: Golbat keeps the snapshot in the package global
// shinyRatesSnapshot and reads config.Config.ShinyRates; Poracle keeps both
// on this struct so the processor and tests own an instance.
type Rates struct {
	url, token string
	// Scans read the current snapshot without locking; only the refresher swaps it.
	snapshot atomic.Pointer[shinyRates]
	// zone resolves the IANA zone of a spawn. Poracle: Golbat calls
	// tz.SearchTimezone (tzf full finder); Poracle uses geo.TimezoneName
	// (tzf default finder). They can disagree within ~111 m of a zone
	// border; accepted, see SYNC.md.
	zone   func(lat, lon float64) string
	status atomic.Pointer[Status]
	cancel context.CancelFunc
	done   chan struct{}
}

// NewRates returns an unstarted feed. An empty url means the feature is off.
func NewRates(url, token string) *Rates {
	r := &Rates{url: url, token: token, zone: geo.TimezoneName}
	r.status.Store(&Status{Configured: url != ""})
	return r
}

// Loaded reports whether a snapshot has ever been loaded.
func (r *Rates) Loaded() bool {
	return r != nil && r.snapshot.Load() != nil
}

// Status returns the feed's current state. Nil-safe.
func (r *Rates) Status() Status {
	if r == nil {
		return Status{}
	}
	st := *r.status.Load()
	if snap := r.snapshot.Load(); snap != nil {
		st.Loaded = true
		st.Variants = len(snap.variants)
	}
	return st
}

// Start refreshes rates in the background every five minutes, or 30 seconds
// after a failure. A failed refresh keeps the last snapshot.
// Poracle: Golbat's StartShinyRates; a method, with Stop to wait for exit.
func (r *Rates) Start(ctx context.Context) {
	if r == nil || r.url == "" {
		return
	}
	ctx, r.cancel = context.WithCancel(ctx)
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		for {
			wait := 5 * time.Minute
			if err := r.refreshShinyRates(ctx); err != nil {
				wait = 30 * time.Second
				if r.snapshot.Load() == nil {
					// Poracle: "personal shiny alerts" for Golbat's "personal scans".
					log.Warnf("Shiny rates have not loaded yet; personal shiny alerts are unavailable: %v", err)
				} else {
					log.Warnf("Shiny rate refresh failed; retaining cached rates: %v", err)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
}

// Stop ends the refresher and waits for it. Poracle only.
func (r *Rates) Stop() {
	if r == nil || r.cancel == nil {
		return
	}
	r.cancel()
	<-r.done
}

func (r *Rates) refreshShinyRates(ctx context.Context) (err error) {
	// Poracle: record the outcome for Status.
	attempt := time.Now()
	defer func() {
		st := *r.status.Load()
		st.LastAttempt = attempt
		if err != nil {
			st.LastError = err.Error()
		} else {
			st.LastError = ""
			st.LastSuccess = attempt
		}
		r.status.Store(&st)
	}()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.url, "/")+"/v1/rates?compact=1", nil)
	if err != nil {
		return err
	}
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", res.StatusCode)
	}
	var feed struct {
		Rates *[]shinyRate `json:"rates"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&feed); err != nil {
		return err
	}
	// {} , null or an error object would otherwise replace good rates with none.
	if feed.Rates == nil {
		return fmt.Errorf("rates response has no rates list")
	}
	next := &shinyRates{variants: make(map[shinyVariant]shinyRate), maximum: make(map[int16]uint32), backgroundMaximum: make(map[int16]uint32)}
	for _, rt := range *feed.Rates {
		next.variants[shinyVariant{rt.Pokemon, rt.Form, rt.Costume}] = rt
		// Unknown costumes/forms still default to 1/512 shiny and no background.
		// The per-species maxima are a cheap rejection before the exact variant.
		bound := max(defaultShinyCutoff, rt.Prediction.cutoff(defaultShinyCutoff))
		for _, p := range rt.Overrides {
			bound = max(bound, p.cutoff(defaultShinyCutoff))
		}
		next.maximum[int16(rt.Pokemon)] = max(next.maximum[int16(rt.Pokemon)], bound)
		background := uint32(0)
		if rt.Background != nil {
			background = rt.Background.cutoff(0)
		}
		for _, p := range rt.BackgroundOverrides {
			background = max(background, p.cutoff(0))
		}
		next.backgroundMaximum[int16(rt.Pokemon)] = max(next.backgroundMaximum[int16(rt.Pokemon)], background)
	}
	r.snapshot.Store(next)
	return nil
}
```

Note: Golbat's loop variable is `r`; it is renamed `rt` because `r` is the receiver. Record that in SYNC.md (Task 3). The `maximum`/`backgroundMaximum` maps only feed Golbat's scan pre-filter. They are kept so the code diffs clean; Task 3's adapter reads `backgroundMaximum` as a cheap rejection, which keeps the `unused` linter quiet.

- [ ] **Step 5: Run the tests**

Run: `cd processor && go test -count=1 ./internal/geo/ ./internal/personalshiny/...`
Expected: PASS. If the ocean assertion fails because tzf's default data assigns a different `Etc/…` zone at (0, −140), change that assertion to only require `GetTimezone(0,-140) != ""`. The Phoenix assertion must hold.

- [ ] **Step 6: Lint and commit**

```bash
cd processor && go vet ./internal/personalshiny/... ./internal/geo/ && golangci-lint run ./internal/personalshiny/... ./internal/geo/...
git add processor/internal/geo processor/internal/personalshiny/rates.go processor/internal/personalshiny/rates_test.go
git commit -m "personalshiny: shiny-rate feed copied from Golbat#423

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Rolls, eligibility and the webhook adapter

**Files:**
- Create: `processor/internal/personalshiny/roll.go`, `processor/internal/personalshiny/adapter.go`, `processor/internal/personalshiny/SYNC.md`
- Create: `processor/internal/db/player_accounts.go` (only the `PlayerAccount` type; its loader comes in Task 4)
- Test: `processor/internal/personalshiny/roll_test.go`, `processor/internal/personalshiny/adapter_test.go`

**Interfaces:**
- Consumes: Task 2's `Rates`, `shinyRates`, `shinyPrediction`, `shinyRate`, `shinyVariant`, `defaultShinyCutoff`, `shinyScale`; `javarandom.New`.
- Produces:
  - `db.PlayerAccount{UID int64; ID, PlayerID, Label string; CreatedAt time.Time}` (db tags `uid,id,player_id,label,created_at`).
  - `(*Rates).Evaluate(p *webhook.PokemonWebhook) *Evaluation` (nil when unloaded or ineligible).
  - `(*Evaluation).For(accounts []db.PlayerAccount) Result`, `type Result struct{ Shiny, Background []db.PlayerAccount }`.
  - `NewChecker(ev *Evaluation, humans map[string]*db.Human) *Checker`, `(*Checker).Allows(humanID string, bits int) bool`, `(*Checker).Result(humanID string) (Result, bool)`. Nil-safe.
  - `(*Rates).ShinyVariant(pokemonID, form, costume int, lat, lon float64) (supported bool, probability float64, known bool)`.
  - `(*Rates).Boosted(lat, lon float64) []VariantRate`, `type VariantRate struct{ PokemonID, Form, Costume, OneIn int }`.
  - `DisplayName(a db.PlayerAccount) string`.
  - Constants `MaxPlayers = 50`, `MaxPlayerIDBytes = 256`.
- Note: `db.Human.Accounts` is added in Task 4. `Checker` reads it, so this task adds that one field to `db.Human` too (Step 3).

- [ ] **Step 1: Write the failing tests**

Create `processor/internal/personalshiny/roll_test.go` (vectors copied from Golbat's `decoder/api_pokemon_shiny_test.go`):

```go
package personalshiny

import (
	"testing"
)

func TestPersonalShinyScanRolls(t *testing.T) {
	// Pin the independently verified game calculation, including opaque IDs.
	for _, tc := range []struct {
		id                string
		encounter         uint64
		shiny, background float64
	}{
		{"0123456789abcdef", 1, 0.22137850522994995, 0.1431974172592163},
		{"Player-ID:ABC/123", 1, 0.3684830665588379, 0.9563266038894653},
		{" Trainer-é ", 1, 0.03772324323654175, 0.7365694642066956},
	} {
		p := newShinyPlayer(tc.id)
		for i := 0; i < 2; i++ {
			shiny, background := p.rolls(tc.encounter)
			if got := float64(shiny) / shinyScale; got != tc.shiny {
				t.Fatalf("shiny roll=%v want=%v", got, tc.shiny)
			}
			if got := float64(background) / shinyScale; got != tc.background {
				t.Fatalf("background roll=%v want=%v", got, tc.background)
			}
		}
	}
}

func TestShinyZonePerSpawn(t *testing.T) {
	// All four corners of this box are America/Denver, but the point inside is
	// America/Phoenix; overrides must follow the spawn, not the box.
	r := NewRates("", "")
	for _, c := range [][2]float64{{35.5, -111.2}, {35.5, -109.8}, {36.3, -111.2}, {36.3, -109.8}} {
		if z := r.zone(c[0], c[1]); z != "America/Denver" {
			t.Skipf("timezone data changed: corner %v is %s", c, z)
		}
	}
	s := &shinyScan{zone: r.zone}
	if z := s.zoneAt(map[string]shinyPrediction{"America/Phoenix": {}}, 35.812, -110.195); z != "America/Phoenix" {
		t.Fatalf("zone = %q", z)
	}
}
```

Create `processor/internal/personalshiny/adapter_test.go`:

```go
package personalshiny

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

var testPlayers = []string{"0123456789abcdef", "Player-ID:ABC/123", " Trainer-é "}

func accounts(ids ...string) []db.PlayerAccount {
	out := make([]db.PlayerAccount, len(ids))
	for i, id := range ids {
		out[i] = db.PlayerAccount{PlayerID: id}
	}
	return out
}

func playerIDs(as []db.PlayerAccount) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.PlayerID
	}
	return out
}

func loadedRates(t *testing.T, feed string) *Rates {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(feed)) }))
	t.Cleanup(server.Close)
	r := NewRates(server.URL, "")
	if err := r.refreshShinyRates(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}

func wildPikachu() *webhook.PokemonWebhook {
	return &webhook.PokemonWebhook{EncounterID: "1", PokemonID: 25, Latitude: 33.45, Longitude: -112.07, SeenType: "wild"}
}

// Mirrors Golbat's TestPersonalShinyScan annotation block: same feed, same
// spawn (Phoenix, so the America/Phoenix overrides apply), same players.
func TestEvaluateMatchesGolbatAnnotate(t *testing.T) {
	r := loadedRates(t, testFeed)
	ev := r.Evaluate(wildPikachu())
	if ev == nil {
		t.Fatal("eligible wild pokemon not evaluated")
	}
	res := ev.For(accounts(testPlayers...))
	if got := playerIDs(res.Shiny); !slices.Equal(got, []string{" Trainer-é "}) {
		t.Fatalf("shiny = %v", got)
	}
	if got := playerIDs(res.Background); !slices.Equal(got, []string{"0123456789abcdef"}) {
		t.Fatalf("background = %v", got)
	}
}

// Mirrors Golbat's TestPersonalDrawOrder.
func TestPersonalDrawOrder(t *testing.T) {
	background := &shinyPrediction{Probability: 0.2, Supported: true}
	r := NewRates("x", "")
	r.snapshot.Store(&shinyRates{variants: map[shinyVariant]shinyRate{
		{25, 0, 0}: {Pokemon: 25, Prediction: shinyPrediction{Probability: 0.1, Supported: true}, Background: background},
		{26, 0, 0}: {Pokemon: 26, Prediction: shinyPrediction{Supported: false}, Background: background},
	}, backgroundMaximum: map[int16]uint32{25: background.cutoff(0), 26: background.cutoff(0)}})
	at := func(id int) Result {
		return r.Evaluate(&webhook.PokemonWebhook{EncounterID: "1", PokemonID: id, SeenType: "wild"}).For(accounts(testPlayers...))
	}
	// Shiny-capable: shiny is the first draw, background the second.
	if res := at(25); !slices.Equal(playerIDs(res.Shiny), []string{" Trainer-é "}) || !slices.Equal(playerIDs(res.Background), []string{"0123456789abcdef"}) {
		t.Fatalf("capable: %+v", res)
	}
	// Never shiny: no shiny draw, so the background is the first draw.
	if res := at(26); len(res.Shiny) != 0 || !slices.Equal(playerIDs(res.Background), []string{" Trainer-é "}) {
		t.Fatalf("not capable: %+v", res)
	}
	// backgroundRoll (Golbat's filter path) agrees with the annotate path.
	for _, id := range testPlayers {
		want := id == " Trainer-é "
		if got := backgroundRoll(false, newShinyPlayer(id), 1) < background.cutoff(0); got != want {
			t.Errorf("backgroundRoll %q = %v, want %v", id, got, want)
		}
	}
}

func TestEvaluateEligibility(t *testing.T) {
	r := loadedRates(t, testFeed)
	for name, mutate := range map[string]func(p *webhook.PokemonWebhook){
		"ditto":            func(p *webhook.PokemonWebhook) { p.PokemonID = 132 },
		"revealed ditto":   func(p *webhook.PokemonWebhook) { p.PokemonID = 132; p.DisplayPokemonID = 16 },
		"disguised":        func(p *webhook.PokemonWebhook) { p.DisplayPokemonID = 16 },
		"nearby stop":      func(p *webhook.PokemonWebhook) { p.SeenType = "nearby_stop" },
		"nearby cell":      func(p *webhook.PokemonWebhook) { p.SeenType = "nearby_cell" },
		"no seen type":     func(p *webhook.PokemonWebhook) { p.SeenType = "" },
		"costume 255":      func(p *webhook.PokemonWebhook) { p.Costume = 255 },
		"bad encounter id": func(p *webhook.PokemonWebhook) { p.EncounterID = "abc" },
	} {
		p := wildPikachu()
		mutate(p)
		if ev := r.Evaluate(p); ev != nil {
			t.Errorf("%s: evaluated, want nil", name)
		}
	}
	p := wildPikachu()
	p.SeenType = "encounter"
	p.DisplayPokemonID = 25 // display equal to species is not a disguise
	if r.Evaluate(p) == nil {
		t.Error("encounter with display == species: want eligible")
	}
}

func TestEvaluateUnloadedAndUnknownVariant(t *testing.T) {
	if NewRates("x", "").Evaluate(wildPikachu()) != nil {
		t.Fatal("evaluated before the feed loaded (must fail closed)")
	}
	var nilRates *Rates
	if nilRates.Evaluate(wildPikachu()) != nil {
		t.Fatal("nil Rates evaluated")
	}
	// Unknown variant: 1/512 shiny, no background (Golbat's default).
	r := loadedRates(t, `{"rates":[]}`)
	ev := r.Evaluate(&webhook.PokemonWebhook{EncounterID: "1", PokemonID: 1, SeenType: "wild"})
	if ev == nil || !ev.shinyCapable || ev.shinyCutoff != defaultShinyCutoff || ev.backgroundCutoff != 0 {
		t.Fatalf("unknown variant evaluation = %+v", ev)
	}
}

func TestCheckerAllowsAndMemo(t *testing.T) {
	r := loadedRates(t, testFeed)
	humans := map[string]*db.Human{
		"shinyuser": {ID: "shinyuser", Accounts: []db.PlayerAccount{{PlayerID: " Trainer-é ", Label: "main"}}},
		"bguser":    {ID: "bguser", Accounts: []db.PlayerAccount{{PlayerID: "0123456789abcdef"}}},
		"channel":   {ID: "channel", Accounts: []db.PlayerAccount{{PlayerID: " Trainer-é ", Label: "group"}, {PlayerID: "0123456789abcdef"}}},
		"noaccts":   {ID: "noaccts"},
	}
	c := NewChecker(r.Evaluate(wildPikachu()), humans)
	for _, tc := range []struct {
		human string
		bits  int
		want  bool
	}{
		{"shinyuser", db.PersonalShiny, true},
		{"shinyuser", db.PersonalBackground, false},
		{"shinyuser", db.PersonalShiny | db.PersonalBackground, false},
		{"bguser", db.PersonalBackground, true},
		{"channel", db.PersonalShiny | db.PersonalBackground, true},
		{"noaccts", db.PersonalShiny, false},
		{"missing", db.PersonalShiny, false},
		{"shinyuser", db.PersonalShiny | 4, false}, // reserved bit: never matches
	} {
		if got := c.Allows(tc.human, tc.bits); got != tc.want {
			t.Errorf("Allows(%s, %d) = %v, want %v", tc.human, tc.bits, got, tc.want)
		}
	}
	// Same player ID on two humans: each sees its own label.
	a, _ := c.Result("shinyuser")
	b, _ := c.Result("channel")
	if a.Shiny[0].Label != "main" || b.Shiny[0].Label != "group" {
		t.Fatalf("labels leaked across humans: %+v / %+v", a.Shiny, b.Shiny)
	}
	if _, ok := c.Result("noaccts"); ok {
		t.Fatal("human without accounts reported as linked")
	}
	var nilChecker *Checker
	if nilChecker.Allows("shinyuser", db.PersonalShiny) {
		t.Fatal("nil checker allowed a personal rule")
	}
	// Feed not loaded: linked humans still report linked, but nothing matches.
	c = NewChecker(nil, humans)
	if c.Allows("shinyuser", db.PersonalShiny) {
		t.Fatal("matched without an evaluation")
	}
	if res, ok := c.Result("shinyuser"); !ok || len(res.Shiny) != 0 {
		t.Fatalf("unloaded Result = %+v, %v", res, ok)
	}
}

func TestShinyVariantAndBoosted(t *testing.T) {
	r := loadedRates(t, testFeed)
	if s, p, ok := r.ShinyVariant(25, 0, 0, 33.45, -112.07); !ok || !s || p != 0.05 {
		t.Fatalf("Phoenix override = %v %v %v", s, p, ok)
	}
	if s, p, ok := r.ShinyVariant(25, 0, 0, 51.5, 0); !ok || !s || p != 0.1 {
		t.Fatalf("base = %v %v %v", s, p, ok)
	}
	if _, _, ok := r.ShinyVariant(1, 0, 0, 0, 0); ok {
		t.Fatal("unlisted variant reported known")
	}
	if _, _, ok := NewRates("x", "").ShinyVariant(25, 0, 0, 0, 0); ok {
		t.Fatal("unloaded feed reported known")
	}
	got := r.Boosted(51.5, 0)
	if len(got) != 1 || got[0].PokemonID != 25 || got[0].OneIn != 10 {
		t.Fatalf("Boosted = %+v", got)
	}
}

func TestDisplayName(t *testing.T) {
	for _, tc := range []struct {
		a    db.PlayerAccount
		want string
	}{
		{db.PlayerAccount{PlayerID: "0123456789abcdef", Label: "main"}, "main"},
		{db.PlayerAccount{PlayerID: "0123456789abcdef"}, "…cdef"},
		{db.PlayerAccount{PlayerID: "abc"}, "abc"},
		{db.PlayerAccount{PlayerID: " Trainer-é "}, "…r-é "},
	} {
		if got := DisplayName(tc.a); got != tc.want {
			t.Errorf("DisplayName(%+v) = %q, want %q", tc.a, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd processor && go test -count=1 ./internal/personalshiny/`
Expected: compile errors (`undefined: newShinyPlayer`, `db.PlayerAccount`, `db.PersonalShiny`, …).

- [ ] **Step 3: Add the db types this package needs**

Create `processor/internal/db/personal.go`:

```go
package db

// Personal bitmask on monsters.personal: which per-player conditions a rule
// requires. All set bits must hold.
//   1 = shiny for one of the human's linked accounts
//   2 = special (event) background for one of the human's linked accounts
//   4, 8 = reserved for per-player XXL / XXS (Golbat#424)
const (
	PersonalShiny      = 1
	PersonalBackground = 2
	// PersonalKnownMask is every bit this binary can evaluate. Rules with any
	// other bit set never match, so a newer binary's rule is not silently
	// widened by an older one.
	PersonalKnownMask = PersonalShiny | PersonalBackground
)

// IsPersonalShiny reports whether the rule requires a personal shiny.
func IsPersonalShiny(personal int) bool { return personal&PersonalShiny != 0 }

// IsPersonalBackground reports whether the rule requires a personal background.
func IsPersonalBackground(personal int) bool { return personal&PersonalBackground != 0 }

// PackPersonal builds the bitmask from its booleans.
func PackPersonal(shiny, background bool) int {
	v := 0
	if shiny {
		v |= PersonalShiny
	}
	if background {
		v |= PersonalBackground
	}
	return v
}
```

Create `processor/internal/db/player_accounts.go` (the loader is added in Task 4):

```go
package db

import "time"

// PlayerAccount is a game account (player ID) linked to a human, from the
// player_accounts table. PlayerID is opaque and case-sensitive.
type PlayerAccount struct {
	UID       int64     `db:"uid"`
	ID        string    `db:"id"`
	PlayerID  string    `db:"player_id"`
	Label     string    `db:"label"`
	CreatedAt time.Time `db:"created_at"`
}
```

In `processor/internal/db/humans.go`, add to `type Human struct` after `Locations`:

```go
	Accounts         []PlayerAccount          // linked game accounts, oldest first; nil if none
```

- [ ] **Step 4: Write `roll.go`**

Create `processor/internal/personalshiny/roll.go`. Copied from `decoder/api_pokemon_shiny.go` except where marked.

```go
package personalshiny

import (
	"crypto/sha1" //nolint:gosec // the game seeds its generator with SHA-1; this is not a security use
	"encoding/binary"
	"math"

	"github.com/pokemon/poracleng/processor/internal/personalshiny/javarandom"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Cost is candidates x players x ID length, so both are bounded. 256 bytes
// covers every known player ID format with room to spare.
// Poracle: exported, and applied per human (store limits) rather than per
// request.
const MaxPlayers = 50
const MaxPlayerIDBytes = 256

const dittoPokemonID = 132

// personalLookup packs roll eligibility and costume into one byte:
// 0 means never personally shiny or background-eligible (Ditto, disguised,
// or seen only as nearby), otherwise costume+1.
// Poracle: Golbat reads its Pokemon record and stores the byte in
// PokemonLookup; Poracle reads the webhook. The webhook has no is_ditto:
// Golbat sends a detected Ditto as pokemon_id 132 with the disguise in
// display_pokemon_id, which the pokemon_id and display checks cover.
// Golbat's costume is a uint8, so out-of-range webhook values are rejected
// with 255.
func personalLookup(p *webhook.PokemonWebhook) uint8 {
	costume := p.Costume
	if p.PokemonID == dittoPokemonID || costume < 0 || costume >= math.MaxUint8 ||
		p.DisplayPokemonID != 0 && p.DisplayPokemonID != p.PokemonID ||
		p.SeenType != "wild" && p.SeenType != "encounter" {
		return 0
	}
	return uint8(costume) + 1
}

// shinyPlayer is used by one scan goroutine. It remembers only the last
// encounter's rolls, so a player listed in several clauses is hashed at most
// once per candidate, and only if a clause gets that far.
type shinyPlayer struct {
	input                 []byte
	last                  uint64
	lastValid             bool
	lastFirst, lastSecond uint32
}

// newShinyPlayer builds the hash input for a player ID.
// Poracle: Golbat inlines this in ScanPokemonWithShiny's add closure.
func newShinyPlayer(id string) *shinyPlayer {
	input := make([]byte, 8+len(id))
	copy(input[8:], id)
	return &shinyPlayer{input: input}
}

// rolls returns the first two java.util.Random nextFloat draws of the
// player's generator for an encounter, kept as their 24-bit integers. The
// shiny roll is the first; the background is the next draw after it, so the
// first when the variant cannot be shiny (see backgroundRoll). Recomputing
// (one SHA-1 block) is cheaper than a cache.
func (p *shinyPlayer) rolls(encounter uint64) (first, second uint32) {
	if p.lastValid && p.last == encounter {
		return p.lastFirst, p.lastSecond
	}
	rng := p.generator(encounter)
	first, second = rng.Next(24), rng.Next(24)
	p.last, p.lastValid, p.lastFirst, p.lastSecond = encounter, true, first, second
	return first, second
}

// generator is the player's fresh generator for an encounter.
func (p *shinyPlayer) generator(encounter uint64) *javarandom.Random {
	binary.LittleEndian.PutUint64(p.input[:8], encounter)
	digest := sha1.Sum(p.input) //nolint:gosec // see import
	return javarandom.New(binary.LittleEndian.Uint64(digest[:8]))
}

// shinyScan holds what a scan resolves rates against.
// Poracle: Golbat's shinyScan also carries the request ctx and player set;
// Poracle evaluates one webhook at a time (see adapter.go).
type shinyScan struct {
	rates *shinyRates
	zone  func(lat, lon float64) string
}

func (s *shinyScan) rate(pokemon, form, costume int) (shinyRate, bool) {
	rate, ok := s.rates.variants[shinyVariant{pokemon, form, costume}]
	return rate, ok
}

func (s *shinyScan) zoneAt(overrides map[string]shinyPrediction, lat, lon float64) string {
	if len(overrides) == 0 {
		return ""
	}
	// Resolved per spawn: matching bbox corners do not imply the whole box
	// shares a zone (e.g. enclaves), and only variants with overrides pay.
	return s.zone(lat, lon)
}

// shinyCutoff resolves the shiny threshold at a location. capable reports
// whether the variant can be shiny there at all; only then does the game take
// a draw for the shiny roll, which shifts every later draw.
func (s *shinyScan) shinyCutoff(rate shinyRate, ok bool, lat, lon float64) (cutoff uint32, capable bool) {
	if !ok {
		return defaultShinyCutoff, true
	}
	prediction := rate.Prediction
	if override, found := rate.Overrides[s.zoneAt(rate.Overrides, lat, lon)]; found {
		prediction = override
	}
	return prediction.cutoff(defaultShinyCutoff), prediction.Supported
}

func (s *shinyScan) backgroundCutoff(rate shinyRate, ok bool, lat, lon float64) uint32 {
	if !ok {
		return 0
	}
	if override, found := rate.BackgroundOverrides[s.zoneAt(rate.BackgroundOverrides, lat, lon)]; found {
		return override.cutoff(0)
	}
	if rate.Background == nil {
		return 0
	}
	return rate.Background.cutoff(0)
}

// backgroundRoll is the draw after the shiny roll, which the game takes only
// when the variant can be shiny: the second draw if so, otherwise the first.
func backgroundRoll(shinyCapable bool, player *shinyPlayer, encounter uint64) uint32 {
	first, second := player.rolls(encounter)
	if shinyCapable {
		return second
	}
	return first
}
```

If `golangci-lint` does not flag `crypto/sha1` (gosec G401/G505 not enabled), remove the two `//nolint:gosec` comments rather than leaving unused suppressions. Check with `golangci-lint run ./internal/personalshiny/...` in Step 7.

- [ ] **Step 5: Write `adapter.go`**

Create `processor/internal/personalshiny/adapter.go`:

```go
package personalshiny

// adapter.go is Poracle-only: it runs Golbat's annotate logic (see roll.go)
// against one pokemon webhook and the humans matched to it.

import (
	"sort"
	"strconv"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Evaluation is one webhook's resolved variant: built once by Evaluate,
// then asked per account. Not safe for concurrent use; a webhook is matched
// and enriched on one goroutine.
type Evaluation struct {
	encounter        uint64
	shinyCutoff      uint32
	shinyCapable     bool
	backgroundCutoff uint32
	memo             map[string]playerResult // keyed by player ID
}

type playerResult struct{ shiny, background bool }

// Result lists which of a human's accounts the spawn is shiny / has a
// background for, in the human's account order.
type Result struct {
	Shiny      []db.PlayerAccount
	Background []db.PlayerAccount
}

// Evaluate resolves the webhook's variant, cutoffs and eligibility once. It
// returns nil when the feed has not loaded (fail closed) or the pokemon can
// never be personally shiny or background-eligible.
// The body is the first half of Golbat's shinyScan.annotate.
func (r *Rates) Evaluate(p *webhook.PokemonWebhook) *Evaluation {
	if r == nil {
		return nil
	}
	snap := r.snapshot.Load()
	if snap == nil {
		return nil
	}
	id, err := strconv.ParseUint(p.EncounterID, 10, 64)
	if err != nil {
		return nil
	}
	personal := personalLookup(p)
	if personal == 0 {
		return nil
	}
	s := &shinyScan{rates: snap, zone: r.zone}
	rate, ok := s.rate(p.PokemonID, p.Form, int(personal)-1)
	ev := &Evaluation{encounter: id}
	ev.shinyCutoff, ev.shinyCapable = s.shinyCutoff(rate, ok, p.Latitude, p.Longitude)
	ev.backgroundCutoff = s.backgroundCutoff(rate, ok, p.Latitude, p.Longitude)
	// Species maximum as a cheap rejection, as Golbat's match does: no
	// variant of this species ever offers a background.
	if snap.backgroundMaximum[int16(p.PokemonID)] == 0 {
		ev.backgroundCutoff = 0
	}
	return ev
}

// player is the per-player loop body of Golbat's shinyScan.annotate.
func (e *Evaluation) player(id string) playerResult {
	if v, ok := e.memo[id]; ok {
		return v
	}
	var res playerResult
	rng := newShinyPlayer(id).generator(e.encounter)
	if e.shinyCapable && rng.Next(24) < e.shinyCutoff {
		res.shiny = true
	}
	// A background is on offer exactly when its cutoff is nonzero.
	if e.backgroundCutoff > 0 && rng.Next(24) < e.backgroundCutoff {
		res.background = true
	}
	if e.memo == nil {
		e.memo = make(map[string]playerResult)
	}
	e.memo[id] = res
	return res
}

// For evaluates a human's accounts. Nil-safe: a nil Evaluation hits nothing.
func (e *Evaluation) For(accounts []db.PlayerAccount) Result {
	var out Result
	if e == nil {
		return out
	}
	for _, a := range accounts {
		res := e.player(a.PlayerID)
		if res.shiny {
			out.Shiny = append(out.Shiny, a)
		}
		if res.background {
			out.Background = append(out.Background, a)
		}
	}
	return out
}

// Checker answers personal rule bits for the humans of one state snapshot,
// memoised per human, for one webhook. Not safe for concurrent use.
type Checker struct {
	ev     *Evaluation
	humans map[string]*db.Human
	memo   map[string]Result
}

// NewChecker binds an evaluation (nil when the feed is not loaded or the
// pokemon is ineligible) to the state's humans.
func NewChecker(ev *Evaluation, humans map[string]*db.Human) *Checker {
	return &Checker{ev: ev, humans: humans}
}

// Result returns the human's hits; ok is false when the human has no linked
// accounts (or is unknown). A nil evaluation yields an empty Result with ok
// still true for linked humans.
func (c *Checker) Result(humanID string) (Result, bool) {
	if c == nil {
		return Result{}, false
	}
	h := c.humans[humanID]
	if h == nil || len(h.Accounts) == 0 {
		return Result{}, false
	}
	if r, ok := c.memo[humanID]; ok {
		return r, true
	}
	r := c.ev.For(h.Accounts)
	if c.memo == nil {
		c.memo = make(map[string]Result)
	}
	c.memo[humanID] = r
	return r, true
}

// Allows reports whether a rule with these personal bits matches for the
// human. Unknown (reserved) bits never match.
func (c *Checker) Allows(humanID string, bits int) bool {
	if c == nil || c.ev == nil || bits&^db.PersonalKnownMask != 0 {
		return false
	}
	r, ok := c.Result(humanID)
	if !ok {
		return false
	}
	if db.IsPersonalShiny(bits) && len(r.Shiny) == 0 {
		return false
	}
	if db.IsPersonalBackground(bits) && len(r.Background) == 0 {
		return false
	}
	return true
}

// prediction resolves a variant's shiny prediction at a location: the
// timezone override when one applies, else the base prediction. Poracle only
// (used for shinyPossible / shinyRate / !info, not for rolls).
func (s *shinyScan) prediction(rate shinyRate, lat, lon float64) shinyPrediction {
	if override, found := rate.Overrides[s.zoneAt(rate.Overrides, lat, lon)]; found {
		return override
	}
	return rate.Prediction
}

// ShinyVariant reports the feed's view of a variant at a location. known is
// false when the feed has not loaded or does not list the variant; callers
// then fall back to observed stats (Golbat's "unknown ⇒ 1/512" is for rolls
// only — as a "shiny possible" badge it would flag unreleased shinies).
func (r *Rates) ShinyVariant(pokemonID, form, costume int, lat, lon float64) (supported bool, probability float64, known bool) {
	if r == nil {
		return false, 0, false
	}
	snap := r.snapshot.Load()
	if snap == nil {
		return false, 0, false
	}
	s := &shinyScan{rates: snap, zone: r.zone}
	rate, ok := s.rate(pokemonID, form, costume)
	if !ok {
		return false, 0, false
	}
	p := s.prediction(rate, lat, lon)
	return p.Supported, p.Probability, true
}

// VariantRate is a variant whose shiny rate is above the 1/512 base.
type VariantRate struct {
	PokemonID, Form, Costume int
	OneIn                    int
}

// Boosted lists the variants whose shiny probability at the location is
// above 1/512 (an event), sorted by pokemon, form, costume.
func (r *Rates) Boosted(lat, lon float64) []VariantRate {
	if r == nil {
		return nil
	}
	snap := r.snapshot.Load()
	if snap == nil {
		return nil
	}
	s := &shinyScan{rates: snap, zone: r.zone}
	var out []VariantRate
	for v, rate := range snap.variants {
		p := s.prediction(rate, lat, lon)
		if p.Supported && p.Probability > 1.0/512 && p.Probability <= 1 {
			out = append(out, VariantRate{PokemonID: v.Pokemon, Form: v.Form, Costume: v.Costume, OneIn: oneIn(p.Probability)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.PokemonID != b.PokemonID {
			return a.PokemonID < b.PokemonID
		}
		if a.Form != b.Form {
			return a.Form < b.Form
		}
		return a.Costume < b.Costume
	})
	return out
}

// OneIn converts a probability to the nearest "1 in N" (0 for p <= 0).
func OneIn(p float64) int { return oneIn(p) }

func oneIn(p float64) int {
	if p <= 0 {
		return 0
	}
	return int(1/p + 0.5)
}

// DisplayName is how an account is shown to users: its label, else the
// player ID shortened to "…" plus its last four characters.
func DisplayName(a db.PlayerAccount) string {
	if a.Label != "" {
		return a.Label
	}
	runes := []rune(a.PlayerID)
	if len(runes) <= 4 {
		return a.PlayerID
	}
	return "…" + string(runes[len(runes)-4:])
}
```

- [ ] **Step 6: Write `SYNC.md`**

Create `processor/internal/personalshiny/SYNC.md`:

````markdown
# Keeping personalshiny in sync with Golbat

This package copies Golbat's personal shiny implementation so Poracle's
alerts agree with a Golbat-backed map for the same spawn, rates and player.

- **Upstream:** UnownHash/Golbat#423 — "Add personal shiny/background filtering and annotation to the pokemon v3 scan"
- **Pinned commit:** `13f90b3d31e2cf5245754b7715a79aff1f75b954` (PR head; re-pin to the merge commit once #423 merges)

## File mapping

| Poracle | Golbat |
|---|---|
| `javarandom/javarandom.go`, `javarandom/javarandom_test.go` | `derive/javarandom/` — verbatim |
| `rates.go` | `decoder/api_pokemon_shiny.go`: `shinyScale`, `defaultShinyCutoff`, `shinyPrediction`/`cutoff`, `shinyVariant`, `shinyRate`, `shinyRates`, `StartShinyRates`, `refreshShinyRates` |
| `roll.go` | `decoder/api_pokemon_shiny.go`: `maxPersonalPlayers`, `maxPersonalPlayerIDBytes`, `personalLookup`, `shinyPlayer` (`rolls`, `generator`), `shinyScan` (`rate`, `zoneAt`, `shinyCutoff`, `backgroundCutoff`), `backgroundRoll` |
| `adapter.go` | Poracle only; `Evaluate` + `Evaluation.player` are the two halves of `shinyScan.annotate` |
| `roll_test.go`, `adapter_test.go`, `rates_test.go` | `decoder/api_pokemon_shiny_test.go` vectors: `TestPersonalShinyScan` (rolls + annotation), `TestPersonalDrawOrder`, `TestShinyZonePerSpawn`, `TestShinyRatesRejectMissingList` |

Not copied: the scan request plumbing (`ScanPokemonWithShiny`, `matchClause`,
`match`, `first`, the `add` closure, `ErrPersonalRequest`) — Poracle evaluates
webhooks, not scan requests.

## Intentional differences (each marked `// Poracle:` in the code)

1. Package globals (`shinyRatesSnapshot`, `config.Config.ShinyRates`) are fields on `Rates`; `StartShinyRates` is `(*Rates).Start` with a matching `Stop`; refresh outcomes are recorded for `Status`.
2. `refreshShinyRates`' loop variable `r` is `rt` (the receiver is `r`).
3. Timezone lookup uses `geo.TimezoneName` (tzf **default** finder, no UTC fallback) instead of `tz.SearchTimezone` (tzf **full** finder). Within ~111 m of a zone border the two can disagree, so results can differ only where the feed has per-timezone overrides active there. Accepted.
4. `personalLookup` reads the webhook, not Golbat's Pokemon record. The webhook has no `is_ditto`; a detected Ditto arrives as `pokemon_id` 132 with the disguise in `display_pokemon_id`, which the existing checks cover. Out-of-range costumes are rejected like costume 255.
5. `maxPersonalPlayers` / `maxPersonalPlayerIDBytes` are exported as `MaxPlayers` / `MaxPlayerIDBytes` and enforced per human by the account store.
6. The "not loaded yet" warning says "personal shiny alerts" instead of "personal scans".
7. Poracle-only uses of the feed: `ShinyVariant` / `Boosted` (shinyPossible, shinyRate, `!info`).

## Updating

```bash
cd /path/to/Golbat && git fetch origin
git diff 13f90b3d31e2cf5245754b7715a79aff1f75b954..origin/main -- \
  decoder/api_pokemon_shiny.go decoder/api_pokemon_shiny_test.go \
  derive/javarandom docs/personal-shinies.md
```

Port each change into the mapped file, re-copy any changed test vectors,
run `go test ./internal/personalshiny/...`, then bump the pinned commit above.
If Golbat changes draw order, eligibility or the feed format, its updated
vectors will fail here until ported.
````

- [ ] **Step 7: Run tests and lint**

Run: `cd processor && go test -count=1 ./internal/personalshiny/... ./internal/db/ && golangci-lint run ./internal/personalshiny/... ./internal/db/...`
Expected: PASS, no lint findings. `TestEvaluateMatchesGolbatAnnotate` depends on (33.45, −112.07) resolving to `America/Phoenix`; Task 2's test already pins that.

- [ ] **Step 8: Commit**

```bash
git add processor/internal/personalshiny processor/internal/db/personal.go processor/internal/db/player_accounts.go processor/internal/db/humans.go
git commit -m "personalshiny: rolls, eligibility and webhook adapter (Golbat#423)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Migration, `monsters.personal`, account loading into state

**Files:**
- Create: `processor/internal/db/migrations/000010_personal_shiny.up.sql`, `.down.sql`
- Modify: `processor/internal/db/monsters.go`, `processor/internal/db/tracking_queries.go`, `processor/internal/db/player_accounts.go`, `processor/internal/db/human_queries.go`, `processor/internal/store/human_sql.go`, `processor/internal/state/loader.go`
- Test: `processor/internal/db/player_accounts_test.go`, `processor/internal/state/loader_test.go` (append)

**Interfaces:**
- Consumes: `db.PlayerAccount`, `db.Human.Accounts` (Task 3).
- Produces:
  - `db.MonsterTracking.Personal int` (`db:"personal"`), `db.MonsterTrackingAPI.Personal int` (`db:"personal" json:"personal"`, no diff tag ⇒ identity field).
  - `db.LoadPlayerAccounts(dbx *sqlx.DB) (map[string][]PlayerAccount, error)` and `db.GroupPlayerAccounts(rows []PlayerAccount) map[string][]PlayerAccount`.
  - `state.attachHumanExtras(database *sqlx.DB, humans map[string]*db.Human) error` (unexported) used by `Load` and `LoadWithGeofences`.

- [ ] **Step 1: Write the migration**

`processor/internal/db/migrations/000010_personal_shiny.up.sql`:

```sql
CREATE TABLE IF NOT EXISTS `player_accounts` (
  `uid`        INT PRIMARY KEY AUTO_INCREMENT,
  `id`         VARCHAR(50)    NOT NULL,
  `player_id`  VARBINARY(256) NOT NULL,
  `label`      VARCHAR(64)    NULL,
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY `uniq_id_player` (`id`, `player_id`),
  UNIQUE KEY `uniq_id_label`  (`id`, `label`)
) ENGINE=InnoDB;

ALTER TABLE `monsters`
  ADD COLUMN `personal` tinyint NOT NULL DEFAULT 0;
```

`processor/internal/db/migrations/000010_personal_shiny.down.sql`:

```sql
ALTER TABLE `monsters` DROP COLUMN `personal`;
DROP TABLE IF EXISTS `player_accounts`;
```

- [ ] **Step 2: Write the failing tests**

Create `processor/internal/db/player_accounts_test.go`:

```go
package db

import (
	"slices"
	"testing"
	"time"
)

func TestGroupPlayerAccounts(t *testing.T) {
	t0 := time.Unix(1000, 0)
	rows := []PlayerAccount{
		{UID: 3, ID: "u1", PlayerID: "b", CreatedAt: t0},
		{UID: 1, ID: "u1", PlayerID: "a", CreatedAt: t0},
		{UID: 2, ID: "u2", PlayerID: "c", CreatedAt: t0},
	}
	got := GroupPlayerAccounts(rows)
	if len(got["u1"]) != 2 || got["u1"][0].PlayerID != "a" || got["u1"][1].PlayerID != "b" {
		t.Fatalf("u1 = %+v (want oldest first, uid tiebreak)", got["u1"])
	}
	if len(got["u2"]) != 1 {
		t.Fatalf("u2 = %+v", got["u2"])
	}
}

func TestPlayerAccountsCascadeOnDelete(t *testing.T) {
	if !slices.Contains(humanOwnedTables, "player_accounts") {
		t.Fatal("player_accounts not deleted with the human")
	}
}

func TestPersonalHelpers(t *testing.T) {
	if PackPersonal(true, true) != 3 || PackPersonal(false, true) != 2 || PackPersonal(false, false) != 0 {
		t.Fatal("PackPersonal")
	}
	if !IsPersonalShiny(3) || IsPersonalShiny(2) || !IsPersonalBackground(2) || IsPersonalBackground(1) {
		t.Fatal("Is* helpers")
	}
}
```

Append to `processor/internal/state/loader_test.go` (if the file's existing tests use a DSN-gated DB, put this test in a new `processor/internal/state/extras_test.go` instead):

```go
func TestApplyAccountsToHumans(t *testing.T) {
	humans := map[string]*db.Human{"u1": {ID: "u1"}, "u2": {ID: "u2"}}
	applyAccounts(humans, map[string][]db.PlayerAccount{
		"u1":      {{ID: "u1", PlayerID: "abc"}},
		"missing": {{ID: "missing", PlayerID: "zzz"}},
	})
	if len(humans["u1"].Accounts) != 1 || humans["u2"].Accounts != nil {
		t.Fatalf("u1=%v u2=%v", humans["u1"].Accounts, humans["u2"].Accounts)
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `cd processor && go test -count=1 ./internal/db/ ./internal/state/`
Expected: `undefined: GroupPlayerAccounts`, `undefined: applyAccounts`.

- [ ] **Step 4: Implement the loader**

Append to `processor/internal/db/player_accounts.go` (adding imports `sort` and `github.com/jmoiron/sqlx`):

```go
// LoadPlayerAccounts loads every linked account, grouped by human id,
// oldest first.
func LoadPlayerAccounts(dbx *sqlx.DB) (map[string][]PlayerAccount, error) {
	var rows []PlayerAccount
	if err := dbx.Select(&rows,
		`SELECT uid, id, player_id, COALESCE(label, '') AS label, created_at FROM player_accounts`); err != nil {
		return nil, err
	}
	return GroupPlayerAccounts(rows), nil
}

// GroupPlayerAccounts groups rows by human id, each group ordered by
// created_at then uid.
func GroupPlayerAccounts(rows []PlayerAccount) map[string][]PlayerAccount {
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		}
		return rows[i].UID < rows[j].UID
	})
	out := make(map[string][]PlayerAccount)
	for _, r := range rows {
		out[r.ID] = append(out[r.ID], r)
	}
	return out
}
```

- [ ] **Step 5: Thread `personal` through the monsters queries**

In `processor/internal/db/monsters.go`:
- Add to `MonsterTracking` after `PVPRankingEvolution`: ``	Personal              int      `db:"personal"` ``
- In `LoadMonsters`' SELECT, change `pvp_ranking_min_cp, pvp_ranking_cap, pvp_ranking_evolution,` to `pvp_ranking_min_cp, pvp_ranking_cap, pvp_ranking_evolution, personal,`.

In `processor/internal/db/tracking_queries.go`:
- Add to `MonsterTrackingAPI` after `PVPRankingEvolution`: ``	Personal              int      `db:"personal"                json:"personal"` `` (no `diff` tag: a personal rule is a different rule from the same filters without it).
- In **both** `SelectMonstersByIDProfile` and `SelectMonstersByID`, change `pvp_ranking_min_cp, pvp_ranking_cap, pvp_ranking_evolution,` to `pvp_ranking_min_cp, pvp_ranking_cap, pvp_ranking_evolution, personal,`.
- In `InsertMonster`: add `personal,` after `pvp_ranking_evolution,` in the column list, add one more `?` to `VALUES` (38 total), and add `m.Personal,` after `m.PVPRankingEvolution,` in the arguments.
- In `UpdateMonsterByUID`: change `pvp_ranking_evolution=?,` to `pvp_ranking_evolution=?, personal=?,` and add `m.Personal,` after `m.PVPRankingEvolution,`.

Check: `grep -c "pvp_ranking_evolution" processor/internal/db/tracking_queries.go` matches `grep -c "personal" …` for those five SQL sites. Profile copy (`CopyProfile`) copies columns generically, so it needs no change.

- [ ] **Step 6: Cascade deletes**

In `processor/internal/db/human_queries.go` `humanOwnedTables`, and in `processor/internal/store/human_sql.go` `humanOwnedTables`, change `"user_locations",` to `"user_locations", "player_accounts",`. Do **not** add it to `trackingTables` (it has no `profile_no`). `TestPlayerAccountsCascadeOnDelete` covers the `db` copy. Add the same test to `processor/internal/store/` (in a new `human_tables_test.go`, package `store`) for the store's copy. In the `store` copy, also assert `!slices.Contains(trackingTables, "player_accounts")` (it has no `profile_no`).

- [ ] **Step 7: Attach accounts in the state loader**

In `processor/internal/state/loader.go`, add:

```go
// attachHumanExtras loads per-human side tables (saved locations, linked
// game accounts) onto the freshly loaded humans.
func attachHumanExtras(database *sqlx.DB, humans map[string]*db.Human) error {
	locs, err := db.LoadUserLocations(database)
	if err != nil {
		return fmt.Errorf("load user locations: %w", err)
	}
	for id, h := range humans {
		if m, ok := locs[id]; ok {
			h.Locations = m
		}
	}
	accounts, err := db.LoadPlayerAccounts(database)
	if err != nil {
		return fmt.Errorf("load player accounts: %w", err)
	}
	applyAccounts(humans, accounts)
	return nil
}

// applyAccounts sets each human's linked accounts. Accounts of humans not in
// the map (disabled or deleted) are ignored.
func applyAccounts(humans map[string]*db.Human, accounts map[string][]db.PlayerAccount) {
	for id, list := range accounts {
		if h := humans[id]; h != nil {
			h.Accounts = list
		}
	}
}
```

Then in **both** `Load` and `LoadWithGeofences`, replace the existing block

```go
	locs, err := db.LoadUserLocations(database)
	if err != nil {
		return fmt.Errorf("load user locations: %w", err)
	}
	for id, h := range data.Humans {
		if m, ok := locs[id]; ok {
			h.Locations = m
		}
	}
```

with

```go
	if err := attachHumanExtras(database, data.Humans); err != nil {
		return err
	}
```

- [ ] **Step 8: Run tests, then the migration integration test if a DB is available**

Run: `cd processor && go build ./... && go test -count=1 ./internal/db/ ./internal/state/ ./internal/store/ ./internal/api/ ./internal/bot/...`
Expected: PASS. Tests that build `MonsterTrackingAPI` literals compile unchanged (the new field defaults to 0).

If MySQL is available locally (`internal/db/migrate_integration_test.go` uses `dragonite:dragonite@127.0.0.1:3306`), run `go test -count=1 -run Migrat ./internal/db/` and confirm `000010` applies and rolls back.

- [ ] **Step 9: Commit**

```bash
git add processor/internal/db processor/internal/store/human_sql.go processor/internal/state
git commit -m "db: player_accounts table, monsters.personal, accounts on state humans

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: `PlayerAccountStore` (SQL + mock)

**Files:**
- Create: `processor/internal/store/player_accounts.go`, `player_accounts_sql.go`, `mock_player_accounts.go`
- Test: `processor/internal/store/player_accounts_test.go` (mock, always runs), `processor/internal/store/player_accounts_sql_test.go` (DSN-gated)

**Interfaces:**
- Consumes: `db.PlayerAccount`, `personalshiny.MaxPlayers`, `personalshiny.MaxPlayerIDBytes`.
- Produces:

```go
type PlayerAccountStore interface {
	List(humanID string) ([]db.PlayerAccount, error)            // oldest first
	Add(humanID, playerID, label string) error                  // ErrInvalidPlayerID, ErrInvalidAccountLabel, ErrDuplicateAccount, ErrDuplicateAccountLabel, ErrTooManyAccounts
	Remove(humanID, playerID string) error                      // ErrAccountNotFound
	SetLabel(humanID, playerID, label string) error             // "" clears; ErrAccountNotFound, ErrDuplicateAccountLabel, ErrInvalidAccountLabel
	Resolve(humanID, ref string) (*db.PlayerAccount, error)     // exact player ID first, then label (case-insensitive); nil,nil if none
}
func NewPlayerAccountStore(dbx *sqlx.DB) PlayerAccountStore
func NewMockPlayerAccountStore() *MockPlayerAccountStore
func ValidatePlayerID(id string) error
func ValidateAccountLabel(label string) error
const MaxAccountLabelLen = 64
```

- [ ] **Step 1: Write the failing mock-backed tests**

Create `processor/internal/store/player_accounts_test.go`:

```go
package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestMockPlayerAccounts(t *testing.T) {
	s := NewMockPlayerAccountStore()
	if err := s.Add("u1", "User_ABC", "main"); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("u1", "user_abc", ""); err != nil {
		t.Fatalf("player IDs are case-sensitive: %v", err)
	}
	if err := s.Add("u1", "User_ABC", "other"); !errors.Is(err, ErrDuplicateAccount) {
		t.Fatalf("duplicate id: %v", err)
	}
	if err := s.Add("u1", "x", "MAIN"); !errors.Is(err, ErrDuplicateAccountLabel) {
		t.Fatalf("labels are case-insensitive: %v", err)
	}
	if err := s.Add("u2", "User_ABC", "main"); err != nil {
		t.Fatalf("same ID on another human: %v", err)
	}
	if err := s.Add("u1", "", ""); !errors.Is(err, ErrInvalidPlayerID) {
		t.Fatalf("empty id: %v", err)
	}
	if err := s.Add("u1", strings.Repeat("x", 257), ""); !errors.Is(err, ErrInvalidPlayerID) {
		t.Fatalf("long id: %v", err)
	}
	if err := s.Add("u1", "y", strings.Repeat("l", 65)); !errors.Is(err, ErrInvalidAccountLabel) {
		t.Fatalf("long label: %v", err)
	}

	a, err := s.Resolve("u1", "Main")
	if err != nil || a == nil || a.PlayerID != "User_ABC" {
		t.Fatalf("resolve by label: %+v %v", a, err)
	}
	a, _ = s.Resolve("u1", "user_abc")
	if a == nil || a.PlayerID != "user_abc" {
		t.Fatalf("resolve exact id before label: %+v", a)
	}
	if err := s.SetLabel("u1", "user_abc", "alt"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLabel("u1", "user_abc", "main"); !errors.Is(err, ErrDuplicateAccountLabel) {
		t.Fatalf("relabel clash: %v", err)
	}
	if err := s.SetLabel("u1", "nope", "x"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("relabel missing: %v", err)
	}
	if err := s.Remove("u1", "User_ABC"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("u1", "User_ABC"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
	list, _ := s.List("u1")
	if len(list) != 1 || list[0].PlayerID != "user_abc" || list[0].Label != "alt" {
		t.Fatalf("list = %+v", list)
	}
}

func TestMockPlayerAccountsLimit(t *testing.T) {
	s := NewMockPlayerAccountStore()
	for i := 0; i < 50; i++ {
		if err := s.Add("u1", fmt.Sprint("p", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Add("u1", "p50", ""); !errors.Is(err, ErrTooManyAccounts) {
		t.Fatalf("51st account: %v", err)
	}
}
```

Create `processor/internal/store/player_accounts_sql_test.go`, gated like `user_locations_sql_test.go` (reuse its `openTestDB` helper if it lives in the same package and creates tables; otherwise create the table in this test's setup with the exact SQL from Task 4 Step 1 and drop it in cleanup). It runs the same sequence as `TestMockPlayerAccounts` against `NewPlayerAccountStore(db)`. Write it as a shared function `runPlayerAccountStoreContract(t, s PlayerAccountStore)` called by both tests, so mock and SQL are held to one contract.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd processor && go test -count=1 -run PlayerAccount ./internal/store/`
Expected: compile errors.

- [ ] **Step 3: Implement the interface, validation and mock**

`processor/internal/store/player_accounts.go`:

```go
package store

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
)

var (
	ErrInvalidPlayerID       = errors.New("player id must be 1-256 bytes")
	ErrInvalidAccountLabel   = errors.New("account label must be at most 64 characters")
	ErrDuplicateAccount      = errors.New("player account already linked")
	ErrDuplicateAccountLabel = errors.New("account label already in use")
	ErrTooManyAccounts       = fmt.Errorf("at most %d linked accounts", personalshiny.MaxPlayers)
	ErrAccountNotFound       = errors.New("player account not found")
)

// MaxAccountLabelLen matches player_accounts.label VARCHAR(64).
const MaxAccountLabelLen = 64

// PlayerAccountStore manages a human's linked game accounts
// (player_accounts). Player IDs are case-sensitive; labels are optional,
// unique per human and compared case-insensitively.
type PlayerAccountStore interface {
	List(humanID string) ([]db.PlayerAccount, error)
	Add(humanID, playerID, label string) error
	Remove(humanID, playerID string) error
	SetLabel(humanID, playerID, label string) error
	// Resolve finds an account by exact player ID, else by label
	// (case-insensitive). Returns nil, nil when neither matches.
	Resolve(humanID, ref string) (*db.PlayerAccount, error)
}

// ValidatePlayerID enforces Golbat's limits: nonempty, at most 256 bytes.
func ValidatePlayerID(id string) error {
	if id == "" || len(id) > personalshiny.MaxPlayerIDBytes {
		return ErrInvalidPlayerID
	}
	return nil
}

// ValidateAccountLabel allows "" (no label) up to 64 characters.
func ValidateAccountLabel(label string) error {
	if utf8.RuneCountInString(label) > MaxAccountLabelLen {
		return ErrInvalidAccountLabel
	}
	return nil
}
```

`processor/internal/store/mock_player_accounts.go`:

```go
package store

import (
	"strings"
	"sync"
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
)

// MockPlayerAccountStore is an in-memory PlayerAccountStore for tests.
type MockPlayerAccountStore struct {
	mu      sync.Mutex
	nextUID int64
	rows    map[string][]db.PlayerAccount
}

func NewMockPlayerAccountStore() *MockPlayerAccountStore {
	return &MockPlayerAccountStore{rows: make(map[string][]db.PlayerAccount)}
}

func (m *MockPlayerAccountStore) List(humanID string) ([]db.PlayerAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]db.PlayerAccount(nil), m.rows[humanID]...), nil
}

func (m *MockPlayerAccountStore) Add(humanID, playerID, label string) error {
	if err := ValidatePlayerID(playerID); err != nil {
		return err
	}
	if err := ValidateAccountLabel(label); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.rows[humanID] {
		if a.PlayerID == playerID {
			return ErrDuplicateAccount
		}
		if label != "" && strings.EqualFold(a.Label, label) {
			return ErrDuplicateAccountLabel
		}
	}
	if len(m.rows[humanID]) >= personalshiny.MaxPlayers {
		return ErrTooManyAccounts
	}
	m.nextUID++
	m.rows[humanID] = append(m.rows[humanID], db.PlayerAccount{
		UID: m.nextUID, ID: humanID, PlayerID: playerID, Label: label, CreatedAt: time.Now(),
	})
	return nil
}

func (m *MockPlayerAccountStore) Remove(humanID, playerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.rows[humanID]
	for i, a := range list {
		if a.PlayerID == playerID {
			m.rows[humanID] = append(list[:i:i], list[i+1:]...)
			return nil
		}
	}
	return ErrAccountNotFound
}

func (m *MockPlayerAccountStore) SetLabel(humanID, playerID, label string) error {
	if err := ValidateAccountLabel(label); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := -1
	for i, a := range m.rows[humanID] {
		if a.PlayerID == playerID {
			idx = i
		} else if label != "" && strings.EqualFold(a.Label, label) {
			return ErrDuplicateAccountLabel
		}
	}
	if idx < 0 {
		return ErrAccountNotFound
	}
	m.rows[humanID][idx].Label = label
	return nil
}

func (m *MockPlayerAccountStore) Resolve(humanID, ref string) (*db.PlayerAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.rows[humanID] {
		if a.PlayerID == ref {
			return &a, nil
		}
	}
	for _, a := range m.rows[humanID] {
		if a.Label != "" && strings.EqualFold(a.Label, ref) {
			return &a, nil
		}
	}
	return nil, nil
}
```

- [ ] **Step 4: Implement the SQL store**

`processor/internal/store/player_accounts_sql.go`:

```go
package store

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
)

type sqlPlayerAccountStore struct{ db *sqlx.DB }

// NewPlayerAccountStore returns the MySQL-backed store.
func NewPlayerAccountStore(dbx *sqlx.DB) PlayerAccountStore { return &sqlPlayerAccountStore{db: dbx} }

const playerAccountCols = `uid, id, player_id, COALESCE(label, '') AS label, created_at`

func (s *sqlPlayerAccountStore) List(humanID string) ([]db.PlayerAccount, error) {
	var rows []db.PlayerAccount
	err := s.db.Select(&rows,
		`SELECT `+playerAccountCols+` FROM player_accounts WHERE id = ? ORDER BY created_at, uid`, humanID)
	return rows, err
}

// duplicateKind maps a MySQL 1062 to the store error for the violated key.
func duplicateKind(err error) error {
	var me *mysql.MySQLError
	if !errors.As(err, &me) || me.Number != mysqlErrDuplicate {
		return nil
	}
	if strings.Contains(me.Message, "uniq_id_label") {
		return ErrDuplicateAccountLabel
	}
	return ErrDuplicateAccount
}

func nullLabel(label string) any {
	if label == "" {
		return nil
	}
	return label
}

func (s *sqlPlayerAccountStore) Add(humanID, playerID, label string) error {
	if err := ValidatePlayerID(playerID); err != nil {
		return err
	}
	if err := ValidateAccountLabel(label); err != nil {
		return err
	}
	var n int
	if err := s.db.Get(&n, `SELECT COUNT(*) FROM player_accounts WHERE id = ?`, humanID); err != nil {
		return err
	}
	if n >= personalshiny.MaxPlayers {
		return ErrTooManyAccounts
	}
	if _, err := s.db.Exec(`INSERT INTO player_accounts (id, player_id, label) VALUES (?, ?, ?)`,
		humanID, playerID, nullLabel(label)); err != nil {
		if dup := duplicateKind(err); dup != nil {
			return dup
		}
		return err
	}
	return nil
}

func (s *sqlPlayerAccountStore) Remove(humanID, playerID string) error {
	res, err := s.db.Exec(`DELETE FROM player_accounts WHERE id = ? AND player_id = ?`, humanID, playerID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (s *sqlPlayerAccountStore) SetLabel(humanID, playerID, label string) error {
	if err := ValidateAccountLabel(label); err != nil {
		return err
	}
	var exists int
	if err := s.db.Get(&exists, `SELECT COUNT(*) FROM player_accounts WHERE id = ? AND player_id = ?`, humanID, playerID); err != nil {
		return err
	}
	if exists == 0 {
		return ErrAccountNotFound
	}
	if _, err := s.db.Exec(`UPDATE player_accounts SET label = ? WHERE id = ? AND player_id = ?`,
		nullLabel(label), humanID, playerID); err != nil {
		if dup := duplicateKind(err); dup != nil {
			return dup
		}
		return err
	}
	return nil
}

func (s *sqlPlayerAccountStore) Resolve(humanID, ref string) (*db.PlayerAccount, error) {
	var row db.PlayerAccount
	err := s.db.Get(&row, `SELECT `+playerAccountCols+` FROM player_accounts WHERE id = ? AND player_id = ? LIMIT 1`, humanID, ref)
	if err == nil {
		return &row, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	err = s.db.Get(&row, `SELECT `+playerAccountCols+` FROM player_accounts WHERE id = ? AND LOWER(label) = LOWER(?) LIMIT 1`, humanID, ref)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
```

- [ ] **Step 5: Run tests**

Run: `cd processor && go test -count=1 ./internal/store/`
Expected: PASS (SQL test skips without `PORACLENG_TEST_DSN`). If a test DB is available, run with the DSN set as well, and confirm `User_ABC` / `user_abc` both insert (the VARBINARY case sensitivity).

- [ ] **Step 6: Commit**

```bash
git add processor/internal/store/player_accounts*.go processor/internal/store/mock_player_accounts.go
git commit -m "store: PlayerAccountStore (SQL + mock)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Matcher hook

**Files:**
- Modify: `processor/internal/matching/pokemon.go`
- Test: `processor/internal/matching/pokemon_personal_test.go`

**Interfaces:**
- Consumes: `personalshiny.Checker` (`Allows`), `db.MonsterTracking.Personal`.
- Produces: `matching.ProcessedPokemon.Personal *personalshiny.Checker` (set by the handler, Task 7).

- [ ] **Step 1: Write the failing test**

Create `processor/internal/matching/pokemon_personal_test.go`:

```go
package matching

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Feed and spawn as in personalshiny's Golbat parity test: at encounter 1,
// " Trainer-é " rolls shiny and "0123456789abcdef" rolls the background.
const personalFeed = `{"rates":[{"pokemon":25,"form":0,"costume":0,"prediction":{"probability":0.1,"supported":true},"background":{"probability":0.2,"supported":true}}]}`

func personalChecker(t *testing.T, humans map[string]*db.Human, loaded bool) *personalshiny.Checker {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(personalFeed)) }))
	t.Cleanup(server.Close)
	r := personalshiny.NewRates(server.URL, "")
	if loaded {
		r.Start(context.Background())
		t.Cleanup(r.Stop)
		for i := 0; !r.Loaded(); i++ {
			if i > 2000 {
				t.Fatal("feed never loaded")
			}
			time.Sleep(time.Millisecond)
		}
	}
	ev := r.Evaluate(&webhook.PokemonWebhook{EncounterID: "1", PokemonID: 25, Latitude: 51.5, SeenType: "wild"})
	return personalshiny.NewChecker(ev, humans)
}

func personalPikachu(c *personalshiny.Checker, encountered bool) *ProcessedPokemon {
	p := &ProcessedPokemon{
		PokemonID: 25, IV: -1, RarityGroup: 1, TTHSeconds: 600, Latitude: 51.5, Longitude: 0,
		Encountered: encountered,
		PVPBestRank: map[int][]pvp.LeagueRank{}, PVPEvoData: map[int]map[int][]pvp.LeagueRank{},
		Personal:    c,
	}
	if encountered {
		p.IV, p.CP, p.Level = 50, 500, 20
	}
	return p
}

func TestPersonalRulesMatch(t *testing.T) {
	shinyHuman := makeHuman("shiny")
	shinyHuman.Accounts = []db.PlayerAccount{{PlayerID: " Trainer-é "}}
	bgHuman := makeHuman("bg")
	bgHuman.Accounts = []db.PlayerAccount{{PlayerID: "0123456789abcdef"}}
	plain := makeHuman("plain")
	humans := map[string]*db.Human{"shiny": shinyHuman, "bg": bgHuman, "plain": plain}

	rule := func(id string, bits int) *db.MonsterTracking {
		m := makeMonster(id, 25)
		m.Personal = bits
		return m
	}
	rules := []*db.MonsterTracking{
		rule("shiny", db.PersonalShiny),                         // match
		rule("shiny", db.PersonalBackground),                    // no
		rule("bg", db.PersonalBackground),                       // match
		rule("bg", db.PersonalShiny|db.PersonalBackground),      // no
		rule("plain", db.PersonalShiny),                         // no accounts: no
		rule("plain", 0),                                        // ordinary: match
	}
	st := makeTestState(rules, humans)
	m := &PokemonMatcher{PVPQueryMaxRank: 100}

	matched, _ := m.Match(personalPikachu(personalChecker(t, humans, true), false), st)
	got := map[string]int{}
	for _, u := range matched {
		got[u.ID]++
	}
	if got["shiny"] != 1 || got["bg"] != 1 || got["plain"] != 1 || len(matched) != 3 {
		t.Fatalf("unencountered matches = %v (personal is not an encounter-only filter)", got)
	}

	// Feed not loaded: personal rules match nothing, ordinary rules still do.
	matched, _ = m.Match(personalPikachu(personalChecker(t, humans, false), true), st)
	if len(matched) != 1 || matched[0].ID != "plain" {
		t.Fatalf("unloaded feed matches = %+v", matched)
	}

	// No checker at all (feature off): same.
	matched, _ = m.Match(personalPikachu(nil, true), st)
	if len(matched) != 1 || matched[0].ID != "plain" {
		t.Fatalf("nil checker matches = %+v", matched)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd processor && go test -count=1 -run TestPersonalRulesMatch ./internal/matching/`
Expected: compile error `unknown field Personal in struct literal of type ProcessedPokemon`.

- [ ] **Step 3: Implement**

In `processor/internal/matching/pokemon.go`:
- Import `"github.com/pokemon/poracleng/processor/internal/personalshiny"`.
- Add to `ProcessedPokemon` after `PVPEvoData`:

```go
	// Personal answers rules' personal bits (per-player shiny/background).
	// Set by the webhook handler after ProcessPokemonWebhook; nil when the
	// feature is off, in which case every personal rule fails.
	Personal *personalshiny.Checker
```

- In `matchMonsters`, immediately before `results = append(results, monster)`:

```go
		// Personal last: it is the only check that hashes.
		if monster.Personal != 0 && !data.Personal.Allows(monster.ID, monster.Personal) {
			continue
		}
```

- [ ] **Step 4: Run tests**

Run: `cd processor && go test -count=1 ./internal/matching/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/matching
git commit -m "matching: personal shiny/background bits on pokemon rules

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Config, processor wiring and per-user DTS fields

**Files:**
- Modify: `processor/internal/config/config.go`, `config/config.example.toml`, `processor/internal/api/config_schema.go`
- Create: `processor/internal/enrichment/personal.go`
- Modify: `processor/cmd/processor/main.go`, `processor/cmd/processor/pokemon.go`, `processor/internal/api/dts_fields.go`
- Test: `processor/internal/enrichment/personal_test.go`, `processor/internal/config/config_test.go` (append)

**Interfaces:**
- Consumes: `personalshiny.Rates`, `Checker`, `DisplayName`; `ProcessedPokemon.Personal`.
- Produces:
  - `config.ShinyRatesConfig{URL, Token string}` at `cfg.ShinyRates` (`toml:"shiny_rates"`), `config.PlayerAccountsConfig{SelfService bool}` at `cfg.PlayerAccounts` (`toml:"player_accounts"`).
  - `ProcessorService.shinyRates *personalshiny.Rates` (always non-nil; empty URL ⇒ never loads).
  - `enrichment.AnnotatePersonal(perUser map[string]map[string]any, matched []webhook.MatchedUser, c *personalshiny.Checker) map[string]map[string]any`.
  - Per-user DTS fields: `personalShiny` (bool), `personalShinyAccounts` ([]string), `personalBackground` (bool), `personalBackgroundAccounts` ([]string), `hasLinkedAccounts` (bool).

- [ ] **Step 1: Write the failing tests**

Create `processor/internal/enrichment/personal_test.go`:

```go
package enrichment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func testChecker(t *testing.T, humans map[string]*db.Human) *personalshiny.Checker {
	t.Helper()
	feed := `{"rates":[{"pokemon":25,"form":0,"costume":0,"prediction":{"probability":0.1,"supported":true},"background":{"probability":0.2,"supported":true}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(feed)) }))
	t.Cleanup(server.Close)
	r := personalshiny.NewRates(server.URL, "")
	r.Start(context.Background())
	t.Cleanup(r.Stop)
	for i := 0; !r.Loaded(); i++ {
		if i > 2000 {
			t.Fatal("feed never loaded")
		}
		waitMillis(1)
	}
	ev := r.Evaluate(&webhook.PokemonWebhook{EncounterID: "1", PokemonID: 25, Latitude: 51.5, SeenType: "wild"})
	return personalshiny.NewChecker(ev, humans)
}

func TestAnnotatePersonal(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": {ID: "u1", Accounts: []db.PlayerAccount{{PlayerID: " Trainer-é ", Label: "main"}, {PlayerID: "0123456789abcdef"}}},
		"ch": {ID: "ch", Accounts: []db.PlayerAccount{{PlayerID: " Trainer-é ", Label: "group"}}},
		"u2": {ID: "u2"},
	}
	matched := []webhook.MatchedUser{{ID: "u1"}, {ID: "u1"}, {ID: "ch"}, {ID: "u2"}}
	// perUser is nil when PVP display is not configured; annotation must
	// still produce the fields.
	out := AnnotatePersonal(nil, matched, testChecker(t, humans))

	u1 := out["u1"]
	if u1["personalShiny"] != true || !slices.Equal(u1["personalShinyAccounts"].([]string), []string{"main"}) {
		t.Fatalf("u1 shiny fields = %v", u1)
	}
	if u1["personalBackground"] != true || !slices.Equal(u1["personalBackgroundAccounts"].([]string), []string{"…cdef"}) {
		t.Fatalf("u1 background fields = %v", u1)
	}
	if u1["hasLinkedAccounts"] != true {
		t.Fatal("u1 hasLinkedAccounts")
	}
	if !slices.Equal(out["ch"]["personalShinyAccounts"].([]string), []string{"group"}) {
		t.Fatalf("channel label leaked: %v", out["ch"])
	}
	if _, ok := out["u2"]; ok {
		t.Fatalf("user without accounts got personal fields: %v", out["u2"])
	}

	// Existing per-user maps are added to, not replaced.
	existing := map[string]map[string]any{"u1": {"pvpUserRanking": 5}}
	out = AnnotatePersonal(existing, matched, testChecker(t, humans))
	if out["u1"]["pvpUserRanking"] != 5 || out["u1"]["personalShiny"] != true {
		t.Fatalf("merge = %v", out["u1"])
	}

	// Nil checker (feature off): untouched.
	if got := AnnotatePersonal(nil, matched, nil); got != nil {
		t.Fatalf("nil checker = %v", got)
	}
}
```

Add a tiny helper at the bottom of the same test file (keeps `time` out of the table above):

```go
func waitMillis(n int) { time.Sleep(time.Duration(n) * time.Millisecond) }
```
and add `"time"` to its imports.

Append to `processor/internal/config/config_test.go` (follow the file's existing helper for loading a TOML string; if it has none, use `toml.Decode` into `Config` directly):

```go
func TestShinyRatesAndPlayerAccountsConfig(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(`
[shiny_rates]
url = "https://rates.example"
token = "t"
[player_accounts]
self_service = true
`, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ShinyRates.URL != "https://rates.example" || cfg.ShinyRates.Token != "t" || !cfg.PlayerAccounts.SelfService {
		t.Fatalf("decoded = %+v %+v", cfg.ShinyRates, cfg.PlayerAccounts)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd processor && go test -count=1 -run 'TestAnnotatePersonal|TestShinyRatesAndPlayerAccountsConfig' ./internal/enrichment/ ./internal/config/`
Expected: compile errors.

- [ ] **Step 3: Config**

In `processor/internal/config/config.go`, add to `type Config struct` after `Snapshots`:

```go
	ShinyRates     ShinyRatesConfig     `toml:"shiny_rates"`
	PlayerAccounts PlayerAccountsConfig `toml:"player_accounts"`
```

and near `SnapshotsConfig`:

```go
// ShinyRatesConfig is the shiny-rate feed shared with Golbat's
// [shiny_rates] (GET <url>/v1/rates?compact=1). Empty URL turns personal
// shiny alerts and feed-backed shiny fields off.
type ShinyRatesConfig struct {
	URL   string `toml:"url"`
	Token string `toml:"token"`
}

// PlayerAccountsConfig controls who may link game accounts.
type PlayerAccountsConfig struct {
	// SelfService lets users add/remove/label their own accounts. When
	// false only admins (and the API) can. Ownership of a player ID cannot
	// be verified, so the default is false.
	SelfService bool `toml:"self_service"`
}
```

In `config/config.example.toml`, after the `[snapshots]` block:

```toml
# ---- Personal shiny / backgrounds ----
# Shiny-rate feed in the same format Golbat's [shiny_rates] uses
# (GET <url>/v1/rates?compact=1). When set, users with linked game accounts
# can track pokemon that are shiny (or have the event background) for their
# own accounts (`!track … personalshiny` / `personalbackground`), and the
# feed drives shinyPossible / shinyRate. Empty = off; shiny fields use
# Poracle's observed stats as before.
[shiny_rates]
url = ""
token = ""                               # optional bearer token

# Who may link game accounts (!account / API). Player IDs cannot be
# verified, so by default only admins (or a trusted API client) can link.
[player_accounts]
self_service = false
```

In `processor/internal/api/config_schema.go`, after the `snapshots` section entry, add:

```go
	// ---- shiny_rates ----
	{
		Name:  "shiny_rates",
		Title: "Shiny rates (personal shiny)",
		Fields: []ConfigFieldDef{
			{Name: "url", Type: "string", Default: "", Description: "Shiny-rate feed base URL, same format as Golbat [shiny_rates] (GET <url>/v1/rates?compact=1). Enables personal shiny/background tracking and feed-backed shinyPossible/shinyRate. Empty = off.", HotReload: false},
			{Name: "token", Type: "string", Default: "", Description: "Optional bearer token for the shiny-rate feed", Sensitive: true, HotReload: false, DependsOn: &ConfigDependency{Field: "url", Value: ""}},
		},
	},

	// ---- player_accounts ----
	{
		Name:  "player_accounts",
		Title: "Player accounts",
		Fields: []ConfigFieldDef{
			{Name: "self_service", Type: "bool", Default: false, Description: "Let users link/unlink/label their own game accounts with !account. When false only admins (or the API) can. Player IDs cannot be verified, so anyone allowed to link can link any ID."},
		},
	},
```

Check how `DependsOn` expresses "non-empty" in this file (`grep -n "DependsOn" internal/api/config_schema.go | head`). If there is no "non-empty" form, drop the `DependsOn` on `token`. If the package has a test that checks every config struct field has a schema entry (`grep -n "func Test" internal/api/config_schema_test.go`), run it now.

- [ ] **Step 4: Per-user annotation**

Create `processor/internal/enrichment/personal.go`:

```go
package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// AnnotatePersonal adds the per-user personal shiny/background fields for
// every matched user with linked accounts, whichever rule matched them.
// perUser may be nil (no PVP display configured); it is created as needed
// and returned. A nil checker (feature off) leaves perUser untouched.
func AnnotatePersonal(perUser map[string]map[string]any, matched []webhook.MatchedUser, c *personalshiny.Checker) map[string]map[string]any {
	if c == nil {
		return perUser
	}
	for _, u := range matched {
		res, linked := c.Result(u.ID)
		if !linked {
			continue
		}
		if perUser == nil {
			perUser = make(map[string]map[string]any)
		}
		m := perUser[u.ID]
		if m == nil {
			m = make(map[string]any, 5)
			perUser[u.ID] = m
		}
		m["hasLinkedAccounts"] = true
		m["personalShiny"] = len(res.Shiny) > 0
		m["personalShinyAccounts"] = displayNames(res.Shiny)
		m["personalBackground"] = len(res.Background) > 0
		m["personalBackgroundAccounts"] = displayNames(res.Background)
	}
	return perUser
}

func displayNames(as []db.PlayerAccount) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = personalshiny.DisplayName(a)
	}
	return out
}
```

Add `"github.com/pokemon/poracleng/processor/internal/db"` to its imports. `PokemonPerUser` writes `_lang` into every per-user map it builds; a map created here has no `_lang`. Check `dts`'s LayeredView lookup for `_lang` (`grep -rn '"_lang"' internal/dts internal/cmd 2>/dev/null; grep -rn '"_lang"' internal/ cmd/`). If anything reads `_lang` from the per-user layer to choose the language, set it here too, using the same fallback as `PokemonPerUser` (`u.Language`, else `e.DefaultLocale`, else `"en"`). That means making this an `(e *Enricher)` method and passing the default locale in; update the test to call `e.AnnotatePersonal` on `&Enricher{DefaultLocale: "en"}`.

- [ ] **Step 5: Wire the processor**

In `processor/cmd/processor/main.go`:
- Add field to `ProcessorService` (near `muteStore`):

```go
	// shinyRates is the shiny-rate feed (Golbat [shiny_rates] format). Always
	// non-nil; with no URL configured it never loads, so personal rules never
	// match and enrichment keeps the observed-stats shiny fields.
	shinyRates *personalshiny.Rates
```

- In `NewProcessorService`, after `ps` is constructed (before `return ps`):

```go
	ps.shinyRates = personalshiny.NewRates(cfg.ShinyRates.URL, cfg.ShinyRates.Token)
	if cfg.ShinyRates.URL != "" {
		ps.shinyRates.Start(ps.ctx)
		log.Infof("Shiny rates: refreshing from %s every 5m", cfg.ShinyRates.URL)
	}
```

- In `Close()`, right after `log.Info("Webhook workers stopped")`:

```go
	ps.shinyRates.Stop()
```

- Import `"github.com/pokemon/poracleng/processor/internal/personalshiny"`.

In `processor/cmd/processor/pokemon.go`, replace

```go
		matched, matchedAreas := ps.pokemonMatcher.Match(processed, ps.stateMgr.Get())
```

with

```go
		st := ps.stateMgr.Get()
		if st != nil {
			processed.Personal = personalshiny.NewChecker(ps.shinyRates.Evaluate(&pokemon), st.Humans)
		}
		matched, matchedAreas := ps.pokemonMatcher.Match(processed, st)
```

and after

```go
		if ps.enricher.PVPDisplay != nil && perLang != nil && len(matched) > 0 {
			perUser = ps.enricher.PokemonPerUser(perLang, matched)
		}
```

add

```go
		perUser = enrichment.AnnotatePersonal(perUser, matched, processed.Personal)
```

(import `personalshiny` and `enrichment` in `pokemon.go` if not already imported). Check that no other variable named `st` is in scope at that point (`grep -n "\bst\b" cmd/processor/pokemon.go`); rename to `snap` if it clashes.

- [ ] **Step 6: Register the DTS fields**

In `processor/internal/api/dts_fields.go`, append to `monsterFields` (next to `pvpUserRanking` or at the end of the per-user group):

```go
	{Name: "personalShiny", Type: "bool", Description: "Shiny for at least one of the recipient's linked game accounts (per-user; needs [shiny_rates])", Category: "other"},
	{Name: "personalShinyAccounts", Type: "array", Description: "Labels of the recipient's accounts the pokemon is shiny for; unlabelled accounts appear as …<last 4 chars of the player ID>", Category: "other"},
	{Name: "personalBackground", Type: "bool", Description: "Has the special (event) background for at least one of the recipient's linked accounts", Category: "other"},
	{Name: "personalBackgroundAccounts", Type: "array", Description: "Labels of the recipient's accounts the pokemon has the special background for", Category: "other"},
	{Name: "hasLinkedAccounts", Type: "bool", Description: "The recipient has at least one linked game account", Category: "other"},
```

and to the pokemon snippets list (near the "Shiny possible" snippet):

```go
	{Label: "Shiny for you", Insert: "{{#if personalShiny}}✨ Shiny for {{join personalShinyAccounts \", \"}}{{/if}}", Description: "Names the recipient's accounts the pokemon is shiny for", Category: "pokemon"},
```

Check that `join` takes a separator argument in `dts/helpers.go` (`grep -n '"join"' -A10 internal/dts/helpers.go`); adjust the snippet to the helper's real signature.

- [ ] **Step 7: Run tests**

Run: `cd processor && go build ./... && go test -count=1 ./internal/enrichment/ ./internal/config/ ./internal/api/ ./cmd/processor/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add processor/internal/config config/config.example.toml processor/internal/api/config_schema.go processor/internal/enrichment/personal*.go processor/cmd/processor processor/internal/api/dts_fields.go
git commit -m "processor: shiny-rate feed lifecycle, personal checker and per-user fields

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Feed-backed `shinyPossible`, `shinyRate`, `shinyRateBoosted`

**Files:**
- Create: `processor/internal/enrichment/shinyinfo.go`
- Modify: `processor/internal/enrichment/{enrichment,pokemon,raid,quest,nest,maxbattle}.go`, `processor/cmd/processor/main.go`, `processor/internal/api/dts_fields.go`
- Test: `processor/internal/enrichment/shinyinfo_test.go`

**Interfaces:**
- Consumes: `(*personalshiny.Rates).ShinyVariant`.
- Produces:
  - `enrichment.ShinyFeed` interface `{ ShinyVariant(pokemonID, form, costume int, lat, lon float64) (supported bool, probability float64, known bool) }` and `Enricher.ShinyFeed ShinyFeed`.
  - `(*Enricher).shinyFacts(pokemonID, form, costume int, lat, lon float64) shinyFacts`, with `shinyFacts{answered, possible, boosted bool; observed float64; oneIn int}`.

- [ ] **Step 1: Write the failing tests**

Create `processor/internal/enrichment/shinyinfo_test.go`:

```go
package enrichment

import "testing"

type fakeObserved map[int]float64

func (f fakeObserved) GetShinyRate(id int) float64 { return f[id] }

type fakeFeed map[[3]int]struct {
	supported bool
	p         float64
}

func (f fakeFeed) ShinyVariant(id, form, costume int, _, _ float64) (bool, float64, bool) {
	v, ok := f[[3]int{id, form, costume}]
	return v.supported, v.p, ok
}

func TestShinyFactsNoFeedIsObservedOnly(t *testing.T) {
	e := &Enricher{ShinyProvider: fakeObserved{25: 480}}
	f := e.shinyFacts(25, 0, 0, 0, 0)
	if !f.answered || !f.possible || f.observed != 480 || f.oneIn != 0 {
		t.Fatalf("observed only = %+v", f)
	}
	if f := e.shinyFacts(1, 0, 0, 0, 0); !f.answered || f.possible {
		t.Fatalf("unseen species = %+v", f)
	}
	if f := (&Enricher{}).shinyFacts(25, 0, 0, 0, 0); f.answered {
		t.Fatalf("no providers = %+v", f)
	}
}

func TestShinyFactsFeedWinsWhenKnown(t *testing.T) {
	e := &Enricher{
		ShinyProvider: fakeObserved{25: 480, 26: 300},
		ShinyFeed: fakeFeed{
			{25, 0, 0}: {true, 0.04},
			{27, 0, 0}: {false, 0},
		},
	}
	if f := e.shinyFacts(25, 0, 0, 0, 0); !f.possible || f.oneIn != 25 || !f.boosted || f.observed != 480 {
		t.Fatalf("listed boosted = %+v", f)
	}
	if f := e.shinyFacts(27, 0, 0, 0, 0); !f.answered || f.possible || f.oneIn != 0 {
		t.Fatalf("listed unreleased = %+v", f)
	}
	// Unlisted variant: fall back to observed stats.
	if f := e.shinyFacts(26, 0, 0, 0, 0); !f.possible || f.oneIn != 0 {
		t.Fatalf("unlisted falls back = %+v", f)
	}
	// Form-specific: (25, 5) is not listed, so observed answers.
	if f := e.shinyFacts(25, 5, 0, 0, 0); f.oneIn != 0 || !f.possible {
		t.Fatalf("other form falls back = %+v", f)
	}
}

func TestShinyFactsBaseRateNotBoosted(t *testing.T) {
	e := &Enricher{ShinyFeed: fakeFeed{{25, 0, 0}: {true, 1.0 / 512}}}
	if f := e.shinyFacts(25, 0, 0, 0, 0); f.boosted || f.oneIn != 512 {
		t.Fatalf("1/512 = %+v", f)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd processor && go test -count=1 -run TestShinyFacts ./internal/enrichment/`
Expected: compile errors.

- [ ] **Step 3: Implement `shinyinfo.go`**

```go
package enrichment

import "math"

// ShinyFeed is the optional shiny-rate feed ([shiny_rates], implemented by
// personalshiny.Rates). known=false when the feed has not loaded or does not
// list the variant.
type ShinyFeed interface {
	ShinyVariant(pokemonID, form, costume int, lat, lon float64) (supported bool, probability float64, known bool)
}

// shinyFacts is what the shiny template fields are built from.
type shinyFacts struct {
	answered bool    // some source answered: set shinyPossible
	possible bool    // shinyPossible
	observed float64 // observed 1:N from Poracle's stats (shinyStats), 0 = unknown
	oneIn    int     // expected 1:N from the feed (shinyRate), 0 = unknown
	boosted  bool    // feed probability above 1/512 (shinyRateBoosted)
}

// shinyFacts resolves shiny information for a variant at a location. The
// feed decides when it lists the variant; otherwise the observed stats
// answer, exactly as before the feed existed.
func (e *Enricher) shinyFacts(pokemonID, form, costume int, lat, lon float64) shinyFacts {
	var f shinyFacts
	if e.ShinyProvider != nil {
		f.observed = e.ShinyProvider.GetShinyRate(pokemonID)
		f.answered = true
		f.possible = f.observed > 0
	}
	if e.ShinyFeed != nil {
		if supported, p, known := e.ShinyFeed.ShinyVariant(pokemonID, form, costume, lat, lon); known {
			f.answered = true
			f.possible = supported
			if supported && p > 0 && p <= 1 {
				f.oneIn = int(math.Round(1 / p))
				f.boosted = p > 1.0/512
			}
		}
	}
	return f
}
```

In `processor/internal/enrichment/enrichment.go`, add to `Enricher` after `ShinyProvider`:

```go
	ShinyFeed          ShinyFeed         // optional; [shiny_rates] feed, preferred over ShinyProvider when it lists the variant
```

- [ ] **Step 4: Replace the five shiny blocks**

Each replacement keeps today's output exactly when `ShinyFeed` is nil.

`pokemon.go` — replace the block starting `// Shiny rate and shiny possible` with:

```go
	// Shiny rate and shiny possible
	if sf := e.shinyFacts(pokemon.PokemonID, pokemon.Form, pokemon.Costume, pokemon.Latitude, pokemon.Longitude); sf.answered {
		if sf.observed > 0 {
			m["shinyStats"] = int(math.Round(sf.observed))
		}
		m["shinyPossible"] = sf.possible
		if sf.possible {
			m["shinyPossibleEmojiKey"] = "shiny"
		}
		if sf.oneIn > 0 {
			m["shinyRate"] = sf.oneIn
			m["shinyRateBoosted"] = sf.boosted
		}
	}
```

`raid.go` — replace `if e.ShinyProvider != nil { rate := … }` with:

```go
				// Shiny possible
				if sf := e.shinyFacts(raid.PokemonID, raid.Form, raid.Costume, raid.Latitude, raid.Longitude); sf.answered {
					m["shinyPossible"] = sf.possible
					if sf.possible {
						m["shinyPossibleEmojiKey"] = "shiny"
					}
				}
```

`nest.go`:

```go
	// Shiny possible
	if sf := e.shinyFacts(nest.PokemonID, nest.Form, 0, nest.Lat, nest.Lon); sf.answered {
		m["shinyPossible"] = sf.possible
		if sf.possible {
			m["shinyPossibleEmojiKey"] = "shiny"
		}
		if sf.oneIn > 0 {
			m["shinyRate"] = sf.oneIn
			m["shinyRateBoosted"] = sf.boosted
		}
	}
```

`maxbattle.go` (the function has `lat, lon` parameters):

```go
			// Shiny possibility (mirrors raid).
			if sf := e.shinyFacts(mb.BattlePokemonID, mb.BattlePokemonForm, mb.BattlePokemonCostume, lat, lon); sf.answered {
				m["shinyPossible"] = sf.possible
				if sf.possible {
					m["shinyPossibleEmojiKey"] = "shiny"
				}
			}
```

`quest.go` (function has `lat, lon`; quest rewards carry no costume):

```go
		if sf := e.shinyFacts(rewardData.Monsters[0].PokemonID, rewardData.Monsters[0].FormID, 0, lat, lon); sf.answered {
			m["shinyPossible"] = sf.possible
			if sf.observed > 0 {
				m["shinyStats"] = int(math.Round(sf.observed))
			}
			if sf.oneIn > 0 {
				m["shinyRate"] = sf.oneIn
				m["shinyRateBoosted"] = sf.boosted
			}
		}
```

Before writing these, confirm the field names exist: `grep -n "Form\|Costume\|Latitude" internal/webhook/types.go` around `RaidWebhook` and `NestWebhook`. The raid block sits inside a `monster` lookup branch; keep it in the same place.

**Behaviour note for pokemon:** today `shinyStats` is only set when `rate > 0`, and so is `shinyPossible=true`; both stay that way with no feed. With the feed listing the variant, `shinyStats` still comes from the observed rate, while `shinyPossible` follows the feed.

- [ ] **Step 5: Wire the feed into the enricher**

In `processor/cmd/processor/main.go`, the enricher is configured before `ps` exists (`enricher.ShinyProvider = statsTracker`). After `ps.shinyRates` is created in `NewProcessorService` (Task 7 Step 5), add:

```go
	if cfg.ShinyRates.URL != "" {
		enricher.ShinyFeed = ps.shinyRates
	}
```

Assigning only when configured keeps `ShinyFeed` a true nil interface otherwise.

- [ ] **Step 6: Register DTS fields**

In `processor/internal/api/dts_fields.go`, add after each existing `shinyPossible` entry in `monsterFields`, `questFields` and `nestFields`:

```go
	{Name: "shinyRate", Type: "int", Description: "Expected shiny rate as 1-in-N from the [shiny_rates] feed at this location (absent without the feed or for unlisted variants)", Category: "other"},
	{Name: "shinyRateBoosted", Type: "bool", Description: "The feed's shiny rate here is above the 1/512 base (an event)", Category: "other"},
```

If `questFields`/`nestFields` have no `shinyPossible` entry, add the two fields at the end of those lists.

- [ ] **Step 7: Run tests (existing enrichment tests must pass unchanged)**

Run: `cd processor && go test -count=1 ./internal/enrichment/ ./internal/api/ ./cmd/processor/`
Expected: PASS. Any change to an existing expectation means the no-feed path changed behaviour; fix the code, not the test.

- [ ] **Step 8: Commit**

```bash
git add processor/internal/enrichment processor/cmd/processor/main.go processor/internal/api/dts_fields.go
git commit -m "enrichment: feed-backed shinyPossible, shinyRate and shinyRateBoosted

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: `!track` keywords, rule text, script export

**Files:**
- Modify: `processor/internal/bot/commands/track.go`, `processor/internal/bot/argmatch.go`, `processor/internal/bot/commands/conversions.go`, `processor/internal/bot/commands/script.go`, `processor/internal/rowtext/monster.go`, `processor/internal/discordbot/slash/autocomplete/listers/tracking.go`, `processor/internal/api/trackingMonster.go` (`toMonsterTracking` only), `processor/internal/i18n/locale/en.json`
- Test: `processor/internal/bot/commands/track_personal_test.go`, `processor/internal/rowtext/rowtext_test.go` (append)

**Interfaces:**
- Consumes: `db.PersonalShiny`, `db.PersonalBackground`, `MonsterTrackingAPI.Personal`.
- Produces: keywords `arg.personalshiny` / `arg.personalbackground`; i18n `tracking.personal_shiny`, `tracking.personal_background`, `msg.track.personal_no_accounts`.

- [ ] **Step 1: Write the failing tests**

Look at an existing track test for the context helper (`grep -n "func newTrackCtx\|func testTrackContext\|func newTestContext" internal/bot/commands/*_test.go | head`) and use it. Create `processor/internal/bot/commands/track_personal_test.go`:

```go
package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func TestTrackPersonalKeywords(t *testing.T) {
	ctx, stores := newTrackTestContext(t) // existing helper: DM user ctx + mock tracking stores
	cmd := &TrackCommand{}
	replies := cmd.Run(ctx, []string{"pikachu", "personalshiny", "personalbackground"})
	if len(replies) == 0 || replies[0].React == "🙅" {
		t.Fatalf("replies = %+v", replies)
	}
	rows, _ := stores.Monsters.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if len(rows) != 1 || rows[0].Personal != db.PersonalShiny|db.PersonalBackground {
		t.Fatalf("rows = %+v", rows)
	}
	// No linked accounts: created, with a warning.
	var all strings.Builder
	for _, r := range replies {
		all.WriteString(r.Text)
	}
	if !strings.Contains(all.String(), "!account") {
		t.Fatalf("missing no-accounts warning: %q", all.String())
	}
}

func TestTrackEverythingPersonalShinyIsNarrowing(t *testing.T) {
	ctx, _ := newTrackTestContext(t)
	ctx.IsAdmin = false
	ctx.Config.Tracking.EverythingFlagPermissions = "allow-any"
	replies := (&TrackCommand{}).Run(ctx, []string{"everything", "personalshiny"})
	if len(replies) > 0 && replies[0].React == "🙅" {
		t.Fatalf("everything personalshiny rejected: %+v", replies)
	}
}
```

Replace `newTrackTestContext` with the real helper name and its return shape. If no shared helper exists, build the context the way the nearest existing `TrackCommand` test does. Read the config field for the everything flag from `config.TrackingConfig` (`grep -n -i everything internal/config/config.go`).

Append to `processor/internal/rowtext/rowtext_test.go` (use its existing generator/translator setup):

```go
func TestMonsterRowTextPersonal(t *testing.T) {
	g, tr := newTestGenerator(t) // existing helper in this file
	m := &db.MonsterTracking{PokemonID: 25, MinIV: -1, MaxIV: 100, MaxCP: 9000, MaxLevel: 55, MaxATK: 15, MaxDEF: 15, MaxSTA: 15, MaxSize: 5, MaxRarity: 6, Costume: 9000, Personal: db.PersonalShiny | db.PersonalBackground}
	s := g.MonsterRowText(tr, m)
	if !strings.Contains(s, "personal shiny") || !strings.Contains(s, "personal background") {
		t.Fatalf("row text = %q", s)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd processor && go test -count=1 -run 'Personal' ./internal/bot/commands/ ./internal/rowtext/`
Expected: FAIL (keywords unrecognised / no text).

- [ ] **Step 3: i18n keys**

Add to `processor/internal/i18n/locale/en.json` (keep the file's key ordering convention — check whether it is sorted with `grep -n '"arg.shiny"' -B2 -A2`):

```json
  "arg.personalshiny": "personalshiny",
  "arg.personalbackground": "personalbackground",
  "tracking.personal_shiny": "personal shiny",
  "tracking.personal_background": "personal background",
  "msg.track.personal_no_accounts": "⚠️ You have no linked game accounts, so personal shiny/background rules won't match until you link one with `{0}account add <player-id>`.",
```

`{0}` is the command prefix. Find how other messages pass it (`grep -n "CommandPrefixForType\|ctx.Prefix" internal/bot/commands/*.go | head -3`) and use the same.

- [ ] **Step 4: Parse the keywords**

In `processor/internal/bot/argmatch.go` `knownKeywordKeys`, change `"arg.clean", "arg.edit", "arg.summary", "arg.shiny", "arg.ex",` to `"arg.clean", "arg.edit", "arg.summary", "arg.shiny", "arg.personalshiny", "arg.personalbackground", "arg.ex",`.

In `processor/internal/bot/commands/track.go`:
- In the track param list, after `{Type: bot.ParamKeyword, Key: "arg.shiny"},` add:

```go
		{Type: bot.ParamKeyword, Key: "arg.personalshiny"},
		{Type: bot.ParamKeyword, Key: "arg.personalbackground"},
```

- After `filters := c.parseFilters(ctx, parsed)`:

```go
	personal := db.PackPersonal(parsed.HasKeyword("arg.personalshiny"), parsed.HasKeyword("arg.personalbackground"))
```

  Then move the "everything" guard below that line if it currently runs earlier, and add `personal != 0 ||` as the first term of `hasFilters` (a personal rule matches roughly 1 in 512 spawns per account, so it narrows).
- In the `insert = append(insert, db.MonsterTrackingAPI{ … })` literal add `Personal: personal,`.
- After the diff is applied and the response is built, before returning:

```go
	if personal != 0 {
		if h := humanFromState(ctx, ctx.TargetID); h == nil || len(h.Accounts) == 0 {
			message.WriteString("\n" + tr.Tf("msg.track.personal_no_accounts", prefix))
		}
	}
```

  where `humanFromState` is:

```go
// humanFromState returns the human as loaded into the current state (with
// linked accounts), or nil.
func humanFromState(ctx *bot.CommandContext, id string) *db.Human {
	if ctx.StateMgr == nil {
		return nil
	}
	st := ctx.StateMgr.Get()
	if st == nil {
		return nil
	}
	return st.Humans[id]
}
```

  Use the prefix variable the surrounding code already has (or derive it as other commands do). If the response is assembled differently (`message` is a `strings.Builder` in the excerpt; check), append the warning in the same way. Accounts linked within the last debounce window (500 ms) may not be in state yet; that only causes a spurious warning, which is acceptable.

- [ ] **Step 5: Carry `Personal` through converters and exports**

- `processor/internal/bot/commands/conversions.go` (monster API→tracking converter): add `Personal: a.Personal,` after `PVPRankingEvolution`.
- `processor/internal/api/trackingMonster.go` `toMonsterTracking`: add `Personal: api.Personal,`.
- `processor/internal/discordbot/slash/autocomplete/listers/tracking.go` (monster converter around line 247): add `Personal: api.Personal,` (and `PVPRankingEvolution: api.PVPRankingEvolution,` is not your concern; leave it).
- `processor/internal/bot/commands/script.go` monster export, after the size line:

```go
	if db.IsPersonalShiny(m.Personal) {
		parts = append(parts, "personalshiny")
	}
	if db.IsPersonalBackground(m.Personal) {
		parts = append(parts, "personalbackground")
	}
```

- `processor/internal/rowtext/monster.go`, before `s += " " + standardText(...)`:

```go
	if db.IsPersonalShiny(monster.Personal) {
		s += " | " + tr.T("tracking.personal_shiny")
	}
	if db.IsPersonalBackground(monster.Personal) {
		s += " | " + tr.T("tracking.personal_background")
	}
```

Search for any other `MonsterTracking{` / `MonsterTrackingAPI{` converters that copy `PVPRankingEvolution`: `grep -rn "PVPRankingEvolution:" processor --include='*.go' | grep -v _test`. Every one should also copy `Personal`.

- [ ] **Step 6: Run tests**

Run: `cd processor && go test -count=1 ./internal/bot/... ./internal/rowtext/ ./internal/discordbot/... ./internal/api/`
Expected: PASS. If `internal/bot/testdata/parity.yaml` pins the keyword list, update it to include the two new keywords.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/bot processor/internal/rowtext processor/internal/discordbot/slash/autocomplete processor/internal/api/trackingMonster.go processor/internal/i18n/locale/en.json
git commit -m "track: personalshiny / personalbackground keywords

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Verbatim arguments and strict targets

**Files:**
- Modify: `processor/internal/bot/parser.go`, `processor/internal/bot/command.go`, `processor/internal/bot/target.go`, `processor/internal/discordbot/bot.go`, `processor/internal/telegrambot/bot.go`, `processor/internal/api/huma_features.go`, `processor/internal/discordbot/slash/dispatcher.go`
- Test: `processor/internal/bot/parser_test.go` (append), `processor/internal/bot/target_test.go` (append)

**Interfaces:**
- Produces:
  - `ParsedCommand.VerbatimArgs []string` — original case, no underscore substitution, pipe-split in step with `Args`.
  - `CommandContext.VerbatimArgs []string` — set by every host before `Run`.
  - `CommandContext.PlayerAccounts store.PlayerAccountStore`, `CommandContext.ShinyRates *personalshiny.Rates` (and the same two fields on `BotDeps`, copied in `NewCommandContext`).
  - `bot.BuildTargetFor(ctx *CommandContext, cmdKey string, args []string) (*Target, []string, error)`: `cmd.account` uses strict parsing, everything else `BuildTarget`.
  - `bot.StripTargetTokens(verbatim []string) []string` — drops `user:<x>` / `name:<x>` tokens, case-insensitively.

- [ ] **Step 1: Write the failing tests**

Append to `processor/internal/bot/parser_test.go` (reuse the file's parser constructor):

```go
func TestParseVerbatimArgs(t *testing.T) {
	p := newTestParser(t) // existing helper; must know "account" → cmd.account, or use any registered command word
	cmds := p.Parse(`!track User_ABC "Quoted_Value" x | Second_One`)
	if len(cmds) != 2 {
		t.Fatalf("got %d commands", len(cmds))
	}
	if got := cmds[0].VerbatimArgs; len(got) != 3 || got[0] != "User_ABC" || got[1] != "Quoted_Value" || got[2] != "x" {
		t.Fatalf("verbatim[0] = %q", got)
	}
	if got := cmds[1].VerbatimArgs; len(got) != 1 || got[0] != "Second_One" {
		t.Fatalf("verbatim[1] = %q", got)
	}
	if cmds[0].Args[0] != "user abc" {
		t.Fatalf("Args unchanged: %q", cmds[0].Args)
	}
}
```

Append to `processor/internal/bot/target_test.go`:

```go
func TestBuildTargetStrictKeepsUserPrefixedArgs(t *testing.T) {
	ctx := newTargetTestContext(t) // existing helper: DM ctx for a registered user
	_, rest, err := BuildTargetFor(ctx, "cmd.account", []string{"add", "username123", "namesake"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 3 || rest[1] != "username123" || rest[2] != "namesake" {
		t.Fatalf("strict target ate args: %q", rest)
	}
	// Non-account commands keep the legacy behaviour.
	if _, rest, _ := BuildTargetFor(ctx, "cmd.track", []string{"pikachu"}); len(rest) != 1 {
		t.Fatalf("track args = %q", rest)
	}
}

func TestStripTargetTokens(t *testing.T) {
	got := StripTargetTokens([]string{"add", "User:123", "Abc_Def", "NAME:hook", "username1"})
	if len(got) != 3 || got[0] != "add" || got[1] != "Abc_Def" || got[2] != "username1" {
		t.Fatalf("got %q", got)
	}
}
```

Use the real helper names (`grep -n "^func " internal/bot/parser_test.go internal/bot/target_test.go | head`).

- [ ] **Step 2: Run them to verify they fail**

Run: `cd processor && go test -count=1 -run 'Verbatim|Strict|StripTarget' ./internal/bot/`
Expected: compile errors.

- [ ] **Step 3: Parser**

In `processor/internal/bot/parser.go`:
- Add to `ParsedCommand` after `RawArgs`:

```go
	VerbatimArgs []string // remaining arguments exactly as typed: original case, underscores kept. For opaque values (player IDs) that must survive byte-for-byte.
```

- In `Parse`, build `verbatim` alongside `rawArgs` (`verbatim = append(verbatim, tok.Raw)` with no underscore replacement), then after `groups, rawGroups := splitByPipePaired(args, rawArgs)` add `_, verbatimGroups := splitByPipePaired(args, verbatim)` and set `VerbatimArgs: verbatimGroups[i]` in the per-group `ParsedCommand`.

- [ ] **Step 4: Strict target and context fields**

In `processor/internal/bot/target.go`:

```go
// strictTargetCommands parse only the explicit `user:<id>` / `name:<name>`
// override forms. Their other arguments are opaque values (player IDs,
// labels) that may legitimately begin with "user" or "name".
var strictTargetCommands = map[string]bool{"cmd.account": true}

// BuildTargetFor is BuildTarget with per-command override parsing.
func BuildTargetFor(ctx *CommandContext, cmdKey string, args []string) (*Target, []string, error) {
	if !strictTargetCommands[cmdKey] {
		return BuildTarget(ctx, args)
	}
	var overrides, rest []string
	for _, a := range args {
		if strings.HasPrefix(a, "user:") || strings.HasPrefix(a, "name:") {
			overrides = append(overrides, a)
		} else {
			rest = append(rest, a)
		}
	}
	target, _, err := BuildTarget(ctx, overrides)
	return target, rest, err
}

// StripTargetTokens removes `user:<x>` / `name:<x>` override tokens from
// verbatim args (matched case-insensitively, as the parser lowercases Args).
func StripTargetTokens(verbatim []string) []string {
	out := make([]string, 0, len(verbatim))
	for _, v := range verbatim {
		l := strings.ToLower(v)
		if strings.HasPrefix(l, "user:") || strings.HasPrefix(l, "name:") {
			continue
		}
		out = append(out, v)
	}
	return out
}
```

`BuildTarget(ctx, nil)` must behave as "no override" (it does: both overrides empty → default target). Confirm by reading the default branch.

In `processor/internal/bot/command.go`:
- `BotDeps`: add

```go
	// PlayerAccounts manages linked game accounts (!account). nil disables
	// the command.
	PlayerAccounts store.PlayerAccountStore
	// ShinyRates is the [shiny_rates] feed (never nil in production; may not
	// be loaded). Used by !info and !poracle-admin status.
	ShinyRates *personalshiny.Rates
```

- `CommandContext`: add the same two fields, plus

```go
	// VerbatimArgs are the command's arguments exactly as typed (original
	// case, underscores kept), set by the host before Run. Commands that take
	// opaque values (player IDs) read them from here; Args are normalised.
	VerbatimArgs []string
```

- `NewCommandContext`: add `PlayerAccounts: deps.PlayerAccounts,` and `ShinyRates: deps.ShinyRates,`.

- [ ] **Step 5: Hosts**

In each of `internal/discordbot/bot.go` (~line 696), `internal/telegrambot/bot.go` (~line 415) and `internal/api/huma_features.go` (~line 435), replace `bot.BuildTarget(ctx, cmd.Args)` with `bot.BuildTargetFor(ctx, cmd.CommandKey, cmd.Args)` and set `ctx.VerbatimArgs = cmd.VerbatimArgs` just before `handler.Run`/`Run`. In `internal/discordbot/slash/dispatcher.go`, set `ctx.VerbatimArgs = tokens` just before `cmd.Run(ctx, tokens)` (slash mappers emit tokens verbatim).

In `processor/cmd/processor/main.go` `sharedBotDeps`, add:

```go
		PlayerAccounts:     store.NewPlayerAccountStore(database),
		ShinyRates:         proc.shinyRates,
```

(`store` is already imported for `humanStore`; check the alias.)

- [ ] **Step 6: Run tests**

Run: `cd processor && go build ./... && go test -count=1 ./internal/bot/... ./internal/discordbot/... ./internal/telegrambot/... ./internal/api/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/bot processor/internal/discordbot processor/internal/telegrambot processor/internal/api/huma_features.go processor/cmd/processor/main.go
git commit -m "bot: verbatim args and strict target parsing for opaque values

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: `!account` command

**Files:**
- Create: `processor/internal/bot/commands/account.go`
- Modify: `processor/cmd/processor/main.go` (register), `processor/internal/i18n/locale/en.json`
- Test: `processor/internal/bot/commands/account_test.go`

**Interfaces:**
- Consumes: `ctx.PlayerAccounts`, `ctx.VerbatimArgs`, `bot.StripTargetTokens`, `store.Err*`, `personalshiny.DisplayName`, `ctx.Config.PlayerAccounts.SelfService`, `ctx.ReloadFunc`.
- Produces: `commands.AccountCommand` (`Name() == "cmd.account"`), i18n keys below.

Permission rule: **writes** (`add`, `remove`, `label`) are allowed when the caller is an admin for the target (`ctx.IsAdmin`, or a delegated admin for this channel — `ctx.Permissions.ChannelTracking` when the target is not a user), **or** `self_service` is on **and** the target is the caller's own user (`ctx.TargetID == ctx.UserID` and `ctx.TargetType` ends in `:user`). `list` is always allowed.

- [ ] **Step 1: i18n keys**

Add to `en.json`:

```json
  "cmd.account": "account",
  "msg.account.usage": "Usage:\n`{0}account` — list your linked game accounts\n`{0}account add <player-id> [label]`\n`{0}account remove <player-id|label>`\n`{0}account label <player-id> <label>` (omit the label to clear it)",
  "msg.account.none": "No linked game accounts.",
  "msg.account.list_header": "Linked game accounts:",
  "msg.account.list_row": "• {0} — added {1}",
  "msg.account.added": "✅ Linked {0}.",
  "msg.account.removed": "✅ Unlinked {0}.",
  "msg.account.labelled": "✅ {0} is now labelled \"{1}\".",
  "msg.account.label_cleared": "✅ Removed the label from {0}.",
  "msg.account.not_found": "No linked account matches \"{0}\".",
  "msg.account.duplicate": "That player ID is already linked.",
  "msg.account.duplicate_label": "Label \"{0}\" is already in use.",
  "msg.account.too_many": "You can link at most {0} accounts.",
  "msg.account.invalid_id": "Player IDs must be 1–256 bytes.",
  "msg.account.invalid_label": "Labels can be at most 64 characters.",
  "msg.account.admin_only": "Only admins can change linked accounts on this server.",
  "msg.account.unavailable": "Account linking is not available.",
  "arg.label": "label",
```

Before adding `arg.label`, check whether it already exists (`grep -n '"arg.label"\|"arg.add"\|"arg.remove"\|"arg.list"' internal/i18n/locale/en.json`); reuse existing keys.

- [ ] **Step 2: Write the failing tests**

Create `processor/internal/bot/commands/account_test.go`. Build a context the way `location_test.go` does, then set `PlayerAccounts`, `VerbatimArgs`, `Config.PlayerAccounts.SelfService`, `IsAdmin`, `UserID`, `TargetID`, `TargetType`. Use this helper and these cases:

```go
package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func accountCtx(t *testing.T, selfService, admin bool) (*bot.CommandContext, *store.MockPlayerAccountStore) {
	t.Helper()
	ctx := newLocationTestContext(t) // reuse the helper location_test.go uses; rename to the real one
	accts := store.NewMockPlayerAccountStore()
	ctx.PlayerAccounts = accts
	ctx.Config.PlayerAccounts.SelfService = selfService
	ctx.IsAdmin = admin
	ctx.UserID, ctx.TargetID, ctx.TargetType = "u1", "u1", "discord:user"
	ctx.ReloadFunc = func() {}
	return ctx, accts
}

func runAccount(ctx *bot.CommandContext, verbatim ...string) string {
	ctx.VerbatimArgs = verbatim
	lower := make([]string, len(verbatim))
	for i, v := range verbatim {
		lower[i] = strings.ReplaceAll(strings.ToLower(v), "_", " ")
	}
	var sb strings.Builder
	for _, r := range (&AccountCommand{}).Run(ctx, lower) {
		sb.WriteString(r.React + r.Text + "\n")
	}
	return sb.String()
}

func TestAccountSelfServiceAddKeepsIDVerbatim(t *testing.T) {
	ctx, accts := accountCtx(t, true, false)
	out := runAccount(ctx, "add", "User_ABC", "Main")
	if !strings.Contains(out, "✅") {
		t.Fatalf("add: %s", out)
	}
	list, _ := accts.List("u1")
	if len(list) != 1 || list[0].PlayerID != "User_ABC" || list[0].Label != "Main" {
		t.Fatalf("stored = %+v", list)
	}
	// IDs beginning with "user"/"name" survive (strict target parsing is the
	// host's job; here the command must not drop them either).
	runAccount(ctx, "add", "username_9")
	list, _ = accts.List("u1")
	if len(list) != 2 || list[1].PlayerID != "username_9" {
		t.Fatalf("user-prefixed id = %+v", list)
	}
}

func TestAccountWritesNeedAdminWithoutSelfService(t *testing.T) {
	ctx, accts := accountCtx(t, false, false)
	if out := runAccount(ctx, "add", "abc"); !strings.Contains(out, "🙅") {
		t.Fatalf("non-admin add allowed: %s", out)
	}
	if list, _ := accts.List("u1"); len(list) != 0 {
		t.Fatal("stored despite refusal")
	}
	// Listing is always allowed.
	if out := runAccount(ctx); strings.Contains(out, "🙅") {
		t.Fatalf("list refused: %s", out)
	}
	ctx.IsAdmin = true
	if out := runAccount(ctx, "add", "abc"); !strings.Contains(out, "✅") {
		t.Fatalf("admin add refused: %s", out)
	}
}

func TestAccountChannelTargetsAdminOnly(t *testing.T) {
	ctx, _ := accountCtx(t, true, false)
	ctx.TargetID, ctx.TargetType = "chan1", "discord:channel"
	if out := runAccount(ctx, "add", "abc"); !strings.Contains(out, "🙅") {
		t.Fatalf("self-service user linked a channel account: %s", out)
	}
}

func TestAccountRemoveAndLabel(t *testing.T) {
	ctx, accts := accountCtx(t, true, false)
	runAccount(ctx, "add", "Abc_1", "main")
	runAccount(ctx, "add", "def")
	if out := runAccount(ctx, "label", "def", "alt"); !strings.Contains(out, "✅") {
		t.Fatalf("label: %s", out)
	}
	if out := runAccount(ctx, "label", "def", "MAIN"); !strings.Contains(out, "🙅") {
		t.Fatalf("duplicate label accepted: %s", out)
	}
	if out := runAccount(ctx, "remove", "Main"); !strings.Contains(out, "✅") {
		t.Fatalf("remove by label: %s", out)
	}
	if out := runAccount(ctx, "remove", "nope"); !strings.Contains(out, "🙅") {
		t.Fatalf("remove missing: %s", out)
	}
	list, _ := accts.List("u1")
	if len(list) != 1 || list[0].PlayerID != "def" || list[0].Label != "alt" {
		t.Fatalf("final = %+v", list)
	}
	out := runAccount(ctx, "list")
	if !strings.Contains(out, "alt") {
		t.Fatalf("list output: %s", out)
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `cd processor && go test -count=1 -run TestAccount ./internal/bot/commands/`
Expected: compile errors.

- [ ] **Step 4: Implement**

Create `processor/internal/bot/commands/account.go`:

```go
package commands

import (
	"errors"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// AccountCommand manages the target's linked game accounts (player IDs) for
// personal shiny/background tracking. Player IDs and labels are read from
// ctx.VerbatimArgs so case and underscores survive.
type AccountCommand struct{}

func (c *AccountCommand) Name() string      { return "cmd.account" }
func (c *AccountCommand) Aliases() []string { return nil }

func (c *AccountCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if ctx.PlayerAccounts == nil {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.account.unavailable")}}
	}
	if help := helpArgReply(ctx, args, "msg.account.usage"); help != nil {
		return []bot.Reply{*help}
	}
	verbatim := bot.StripTargetTokens(ctx.VerbatimArgs)
	if len(verbatim) == 0 {
		return c.list(ctx)
	}

	enTr := ctx.Translations.For("en")
	sub := strings.ToLower(verbatim[0])
	is := func(key string) bool {
		return sub == strings.ToLower(tr.T(key)) || sub == strings.ToLower(enTr.T(key))
	}
	rest := verbatim[1:]
	switch {
	case is("arg.list"):
		return c.list(ctx)
	case is("arg.add"):
		if !c.canWrite(ctx) {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.account.admin_only")}}
		}
		if len(rest) < 1 {
			return []bot.Reply{{Text: inlineUsage(ctx, "msg.account.usage")}}
		}
		label := strings.Join(rest[1:], " ")
		return c.result(ctx, ctx.PlayerAccounts.Add(ctx.TargetID, rest[0], label), label,
			tr.Tf("msg.account.added", displayRef(rest[0], label)))
	case is("arg.remove"):
		if !c.canWrite(ctx) {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.account.admin_only")}}
		}
		if len(rest) < 1 {
			return []bot.Reply{{Text: inlineUsage(ctx, "msg.account.usage")}}
		}
		ref := strings.Join(rest, " ")
		a, err := ctx.PlayerAccounts.Resolve(ctx.TargetID, ref)
		if err != nil {
			log.Errorf("account: resolve %s/%q: %v", ctx.TargetID, ref, err)
			return []bot.Reply{{React: "🙅"}}
		}
		if a == nil {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.account.not_found", ref)}}
		}
		return c.result(ctx, ctx.PlayerAccounts.Remove(ctx.TargetID, a.PlayerID), "",
			tr.Tf("msg.account.removed", personalshiny.DisplayName(*a)))
	case is("arg.label"):
		if !c.canWrite(ctx) {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.account.admin_only")}}
		}
		if len(rest) < 1 {
			return []bot.Reply{{Text: inlineUsage(ctx, "msg.account.usage")}}
		}
		a, err := ctx.PlayerAccounts.Resolve(ctx.TargetID, rest[0])
		if err != nil {
			log.Errorf("account: resolve %s/%q: %v", ctx.TargetID, rest[0], err)
			return []bot.Reply{{React: "🙅"}}
		}
		if a == nil {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.account.not_found", rest[0])}}
		}
		label := strings.Join(rest[1:], " ")
		ok := tr.Tf("msg.account.labelled", personalshiny.DisplayName(*a), label)
		if label == "" {
			ok = tr.Tf("msg.account.label_cleared", personalshiny.DisplayName(*a))
		}
		return c.result(ctx, ctx.PlayerAccounts.SetLabel(ctx.TargetID, a.PlayerID, label), label, ok)
	}
	return []bot.Reply{{Text: inlineUsage(ctx, "msg.account.usage")}}
}

// canWrite: admins (or delegated channel admins for a non-user target);
// otherwise only self-service on the caller's own user.
func (c *AccountCommand) canWrite(ctx *bot.CommandContext) bool {
	isUserTarget := strings.HasSuffix(ctx.TargetType, ":user")
	if ctx.IsAdmin || (!isUserTarget && ctx.Permissions.ChannelTracking) {
		return true
	}
	return isUserTarget && ctx.TargetID == ctx.UserID &&
		ctx.Config != nil && ctx.Config.PlayerAccounts.SelfService
}

func (c *AccountCommand) result(ctx *bot.CommandContext, err error, label, ok string) []bot.Reply {
	tr := ctx.Tr()
	switch {
	case err == nil:
		if ctx.ReloadFunc != nil {
			ctx.ReloadFunc()
		}
		return []bot.Reply{{React: "✅", Text: ok}}
	case errors.Is(err, store.ErrDuplicateAccount):
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.account.duplicate")}}
	case errors.Is(err, store.ErrDuplicateAccountLabel):
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.account.duplicate_label", label)}}
	case errors.Is(err, store.ErrTooManyAccounts):
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.account.too_many", personalshiny.MaxPlayers)}}
	case errors.Is(err, store.ErrInvalidPlayerID):
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.account.invalid_id")}}
	case errors.Is(err, store.ErrInvalidAccountLabel):
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.account.invalid_label")}}
	case errors.Is(err, store.ErrAccountNotFound):
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.account.not_found", "")}}
	}
	log.Errorf("account: %s: %v", ctx.TargetID, err)
	return []bot.Reply{{React: "🙅"}}
}

func (c *AccountCommand) list(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	list, err := ctx.PlayerAccounts.List(ctx.TargetID)
	if err != nil {
		log.Errorf("account: list %s: %v", ctx.TargetID, err)
		return []bot.Reply{{React: "🙅"}}
	}
	if len(list) == 0 {
		return []bot.Reply{{Text: tr.T("msg.account.none")}}
	}
	var sb strings.Builder
	sb.WriteString(tr.T("msg.account.list_header"))
	for _, a := range list {
		sb.WriteString("\n" + tr.Tf("msg.account.list_row", personalshiny.DisplayName(a), a.CreatedAt.Format("2006-01-02")))
	}
	return bot.SplitTextReply(sb.String())
}

func displayRef(playerID, label string) string {
	if label != "" {
		return label
	}
	return personalshiny.DisplayName(dbAccount(playerID))
}
```

and add the small helper (same file):

```go
func dbAccount(playerID string) db.PlayerAccount { return db.PlayerAccount{PlayerID: playerID} }
```

with `"github.com/pokemon/poracleng/processor/internal/db"` imported. Check `helpArgReply`, `inlineUsage` and `bot.SplitTextReply` signatures against `location.go` (they are used there) and match their prefix handling for `{0}` in `msg.account.usage`. Also check the `Permissions.ChannelTracking` field name (`grep -n "ChannelTracking" internal/bot/command.go`).

Register in `processor/cmd/processor/main.go` next to `cmdRegistry.Register(&commands.LocationCommand{})`:

```go
	cmdRegistry.Register(&commands.AccountCommand{})
```

If `!help` lists commands from a static table, or `command_security` needs to know the command, follow how `cmd.location` is wired (`grep -rn '"cmd.location"' processor/internal --include='*.go' | grep -v _test`) and do the same for `cmd.account`.

- [ ] **Step 5: Run tests**

Run: `cd processor && go test -count=1 ./internal/bot/... ./cmd/processor/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/bot/commands/account*.go processor/cmd/processor/main.go processor/internal/i18n/locale/en.json
git commit -m "commands: !account to link game accounts

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Slash `/account` and `/track` personal options

**Files:**
- Create: `processor/internal/discordbot/slash/mappers/account.go`, `processor/internal/discordbot/slash/mappers/account_test.go`
- Modify: `processor/internal/discordbot/slash/definitions.go`, `processor/internal/discordbot/slash/mappers/track.go`, `processor/internal/discordbot/slash/snapshot_dump_test.go` (overrides list, if needed), `processor/internal/i18n/locale/en.json`
- Regenerate: `processor/internal/discordbot/slash/testdata/{track,account}.json`

**Interfaces:**
- Consumes: `yesOpt`, `subCommand`, `stringOpt`, `flattenOptions`, `getString`, `emitFlag`, `MapperError` (existing slash helpers).
- Produces: slash `/account` (`list|add|remove|label`) mapping to `!account` tokens; `/track` options `personal_shiny`, `personal_background` (single "Yes" choice, like `clean`).

- [ ] **Step 1: Write the failing mapper tests**

Create `processor/internal/discordbot/slash/mappers/account_test.go`, modelled on `location_test.go` (`grep -n "func Test" internal/discordbot/slash/mappers/location_test.go | head -3` for the option-building helper):

```go
func TestAccountMapper(t *testing.T) {
	cases := []struct {
		name string
		opts []*discordgo.ApplicationCommandInteractionDataOption
		want []string
	}{
		{"list", subOpt("list"), []string{"list"}},
		{"add", subOpt("add", strOpt("player_id", "User_ABC"), strOpt("label", "Main")), []string{"add", "User_ABC", "Main"}},
		{"add no label", subOpt("add", strOpt("player_id", "User_ABC")), []string{"add", "User_ABC"}},
		{"remove", subOpt("remove", strOpt("account", "Main")), []string{"remove", "Main"}},
		{"label", subOpt("label", strOpt("account", "User_ABC"), strOpt("label", "alt")), []string{"label", "User_ABC", "alt"}},
	}
	for _, tc := range cases {
		got, err := Account(tc.opts)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
	if _, err := Account(subOpt("add")); err == nil {
		t.Error("add without player_id accepted")
	}
}
```

Write `subOpt`/`strOpt` helpers in the test file if `location_test.go` has none. Add to the existing track mapper test:

```go
	// personal options
	got, _ := Track(opts(strOpt("pokemon", "everything"), strOpt("personal_shiny", "yes"), strOpt("personal_background", "yes")))
	if !slices.Contains(got, "personalshiny") || !slices.Contains(got, "personalbackground") {
		t.Fatalf("personal tokens missing: %q", got)
	}
```

using whatever value the `clean` yes-option sends (check `emitFlag` in `common.go`).

- [ ] **Step 2: Run them to verify they fail**

Run: `cd processor && go test -count=1 ./internal/discordbot/slash/...`
Expected: compile errors.

- [ ] **Step 3: Implement the mapper and definitions**

`processor/internal/discordbot/slash/mappers/account.go`:

```go
package mappers

import "github.com/bwmarrin/discordgo"

// Account maps /account sub-commands to !account tokens. Values are passed
// through verbatim: player IDs are case-sensitive.
//
//	list                      → ["list"]
//	add <player_id> [label]   → ["add", id, label?]
//	remove <account>          → ["remove", account]
//	label <account> [label]   → ["label", account, label?]
func Account(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	if len(opts) == 0 || opts[0] == nil || opts[0].Type != discordgo.ApplicationCommandOptionSubCommand {
		return nil, &MapperError{Key: "error.slash.account.no_subcommand"}
	}
	sub := opts[0]
	o := flattenOptions(sub.Options)
	switch sub.Name {
	case "list":
		return []string{"list"}, nil
	case "add":
		id := getString(o["player_id"])
		if id == "" {
			return nil, &MapperError{Key: "error.slash.account.no_player_id"}
		}
		tokens := []string{"add", id}
		if l := getString(o["label"]); l != "" {
			tokens = append(tokens, l)
		}
		return tokens, nil
	case "remove":
		ref := getString(o["account"])
		if ref == "" {
			return nil, &MapperError{Key: "error.slash.account.no_account"}
		}
		return []string{"remove", ref}, nil
	case "label":
		ref := getString(o["account"])
		if ref == "" {
			return nil, &MapperError{Key: "error.slash.account.no_account"}
		}
		tokens := []string{"label", ref}
		if l := getString(o["label"]); l != "" {
			tokens = append(tokens, l)
		}
		return tokens, nil
	}
	return nil, &MapperError{Key: "error.slash.account.no_subcommand"}
}

func init() { registry["account"] = Account }
```

In `processor/internal/discordbot/slash/mappers/track.go`, after `appendCommonTail(&tokens, o)`:

```go
	if tok := emitFlag(o["personal_shiny"], "personalshiny"); tok != "" {
		tokens = append(tokens, tok)
	}
	if tok := emitFlag(o["personal_background"], "personalbackground"); tok != "" {
		tokens = append(tokens, tok)
	}
```

and document the two options in the function comment.

In `processor/internal/discordbot/slash/definitions.go`:
- `allCommandKeys`: add `"cmd.account"` after `"cmd.location"`.
- `buildCommandDef`: add

```go
	case "cmd.account":
		return buildDefinition(bundle, key, canon, accountOptions(bundle))
```

- Add:

```go
// accountOptions exposes /account with sub-commands mirroring !account.
func accountOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		subCommand(bundle, "account.list", "list", "List your linked game accounts"),
		subCommand(bundle, "account.add", "add", "Link a game account (player ID)",
			stringOpt(bundle, "account.add.player_id", "player_id", "Your in-game player ID (exactly as shown)", true, false),
			stringOpt(bundle, "account.add.label", "label", "Optional short name, e.g. main or alt", false, false)),
		subCommand(bundle, "account.remove", "remove", "Unlink a game account",
			stringOpt(bundle, "account.remove.account", "account", "Player ID or label", true, false)),
		subCommand(bundle, "account.label", "label", "Set or clear an account's label",
			stringOpt(bundle, "account.label.account", "account", "Player ID or label", true, false),
			stringOpt(bundle, "account.label.label", "label", "New label (leave empty to clear)", false, false)),
	}
}
```

- In `trackOptions`, after `editOpt(...)`:

```go
		yesOpt(bundle, "track", "personal_shiny", "Only when shiny for one of your linked game accounts"),
		yesOpt(bundle, "track", "personal_background", "Only when it has the event background for one of your linked accounts"),
```

Add the matching `slash.cmd.account`, `slash.desc.account`, `slash.opt.account.*` / `.desc`, `slash.opt.track.personal_shiny(.desc)`, `slash.choice.track.personal_shiny.yes`, the background equivalents, and the `error.slash.account.*` keys to `en.json`, following the existing `slash.opt.location.*` naming exactly (`grep -n '"slash\.[a-z]*\.location' internal/i18n/locale/en.json | head`). Discord allows at most 25 options per command; count `/track`'s after adding (`len(trackOptions(...))` in a quick test) and stay under it.

- [ ] **Step 4: Regenerate the slash snapshots and run tests**

```bash
cd processor
POROCLE_DUMP_SNAPSHOTS=1 go test -count=1 -run TestDumpSnapshots ./internal/discordbot/slash/
git diff --stat internal/discordbot/slash/testdata
go test -count=1 ./internal/discordbot/...
```

If `TestDumpSnapshots` builds its own bundle with explicit overrides, add `withOverride("en", "slash.cmd.account", "account")` and `withOverride("en", "slash.desc.account", "Manage your linked game accounts")` to its list, plus `{"cmd.account", "account"}` to its key table, so `testdata/account.json` is produced. Review the diff: `track.json` should only gain the two options.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash processor/internal/i18n/locale/en.json
git commit -m "slash: /account and /track personal options

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: `!info` feed lines and admin status

**Files:**
- Modify: `processor/internal/bot/commands/info.go`, `processor/internal/bot/commands/poracle_admin_status.go`, `processor/internal/i18n/locale/en.json`
- Test: `processor/internal/bot/commands/info_shiny_feed_test.go`, `processor/internal/bot/commands/poracle_admin_status_test.go` (append)

**Interfaces:**
- Consumes: `ctx.ShinyRates` (`ShinyVariant`, `Boosted`, `Status`, `Loaded`), `personalshiny.OneIn`, `ctx.Humans.Get`.

- [ ] **Step 1: i18n keys**

```json
  "msg.info.shiny_expected": "Expected shiny rate: 1:{0}",
  "msg.info.shiny_expected_local": "Expected shiny rate: 1:{0} (1:{1} at your location right now)",
  "msg.info.shiny_not_released": "Shiny not released",
  "msg.info.shiny_boosted_header": "Boosted right now",
  "cmd.poracle_admin.status.section.shiny_rates": "✨ Shiny rates",
  "cmd.poracle_admin.status.label.shiny_rates_off": "not configured",
  "cmd.poracle_admin.status.label.shiny_rates_loaded": "loaded: {0} variants, refreshed {1} ago",
  "cmd.poracle_admin.status.label.shiny_rates_waiting": "not loaded yet (personal shiny rules match nothing)",
  "cmd.poracle_admin.status.label.shiny_rates_error": "last error: {0}",
```

- [ ] **Step 2: Write the failing tests**

Create `processor/internal/bot/commands/info_shiny_feed_test.go`. Build the context as the existing `info_test.go` does, attach a loaded feed (`personalshiny.NewRates(server.URL, "")` + `Start`, waiting for `Loaded()` as in Task 7's test), and assert:
- `!info pikachu` output contains `Expected shiny rate: 1:10` (base 0.1) for a user at (51.5, 0), and `1:20` when the feed has `"overrides":{"Europe/London":{"probability":0.05,"supported":true}}`, using `msg.info.shiny_expected_local` ("1:10 (1:20 at your location right now)").
- `!info shiny` contains `Boosted right now` followed by a Pikachu line with `1:10`.
- With `ctx.ShinyRates = personalshiny.NewRates("", "")` (unconfigured) the outputs equal today's (no "Expected" line, no "Boosted" header).

Append to `poracle_admin_status_test.go`: with an unconfigured feed the shiny-rates section says `not configured`; with an unloaded configured feed (`NewRates("http://127.0.0.1:1", "")`, not started) it says `not loaded yet`.

- [ ] **Step 3: Run them to verify they fail**

Run: `cd processor && go test -count=1 -run 'ShinyFeed|ShinyRates' ./internal/bot/commands/`
Expected: FAIL.

- [ ] **Step 4: Implement `!info <pokemon>`**

In `info.go`, immediately **before** the `// Shiny rate (from stats tracker)` block:

```go
	// Expected shiny rate from the [shiny_rates] feed (base, and at the
	// user's location when an override applies there).
	if ctx.ShinyRates.Loaded() {
		if line := c.expectedShinyLine(ctx, pokemonID); line != "" {
			sb.WriteByte('\n')
			sb.WriteString(line + "\n")
		}
	}
```

and add:

```go
func (c *InfoCommand) expectedShinyLine(ctx *bot.CommandContext, pokemonID int) string {
	tr := ctx.Tr()
	supported, base, known := ctx.ShinyRates.ShinyVariant(pokemonID, 0, 0, 0, 0)
	if !known {
		return ""
	}
	if !supported || base <= 0 {
		return ctx.Bold(tr.T("msg.info.shiny_not_released"))
	}
	if lat, lon, ok := c.userLocation(ctx); ok {
		if s, local, _ := ctx.ShinyRates.ShinyVariant(pokemonID, 0, 0, lat, lon); s && local > 0 && local != base {
			return ctx.Bold(tr.Tf("msg.info.shiny_expected_local", personalshiny.OneIn(base), personalshiny.OneIn(local)))
		}
	}
	return ctx.Bold(tr.Tf("msg.info.shiny_expected", personalshiny.OneIn(base)))
}

// userLocation is the target's default location, if set.
func (c *InfoCommand) userLocation(ctx *bot.CommandContext) (float64, float64, bool) {
	if !ctx.HasLocation || ctx.Humans == nil {
		return 0, 0, false
	}
	h, err := ctx.Humans.Get(ctx.TargetID)
	if err != nil || h == nil || (h.Latitude == 0 && h.Longitude == 0) {
		return 0, 0, false
	}
	return h.Latitude, h.Longitude, true
}
```

Base rate uses (0, 0), where no zone exists, so overrides never apply (`zoneAt` returns `""`, which matches no override key). Confirm with the Task 2 ocean assertion's outcome; if tzf assigns a zone at (0, 0), add an unexported `(*Rates).BaseShinyVariant` that skips overrides instead, and use it here.

- [ ] **Step 5: Implement `!info shiny`**

At the top of `shinyStats`, before the existing `ctx.Stats == nil` check, build the boosted section and prepend it to whatever the existing function returns:

```go
	var boosted strings.Builder
	if ctx.ShinyRates.Loaded() {
		lat, lon, _ := c.userLocation(ctx)
		if list := ctx.ShinyRates.Boosted(lat, lon); len(list) > 0 {
			tr := ctx.Tr()
			boosted.WriteString(ctx.Bold(tr.T("msg.info.shiny_boosted_header")) + "\n")
			for _, v := range list {
				name := tr.T(gamedata.PokemonTranslationKey(v.PokemonID))
				if v.Form != 0 {
					name += " (" + tr.T(gamedata.FormTranslationKey(v.Form)) + ")"
				}
				fmt.Fprintf(&boosted, "%s: 1:%d\n", ctx.EscapeForReply(name), v.OneIn)
			}
			boosted.WriteString("\n")
		}
	}
```

Restructure `shinyStats` so: if both the boosted section and the observed table are empty, return `msg.info.shiny_unavailable` (today's behaviour); otherwise return `bot.SplitTextReply(boosted.String() + observedTable)`. Check `gamedata.FormTranslationKey` exists (`grep -n "func FormTranslationKey\|func PokemonTranslationKey" internal/gamedata/*.go`); use the real form-name helper.

- [ ] **Step 6: Admin status section**

In `poracle_admin_status.go`, add after the summary-buffer section in the main render:

```go
	// Section: Shiny-rate feed.
	sb.WriteString("\n\n")
	sb.WriteString(renderShinyRatesSection(ctx, tr))
```

and:

```go
// renderShinyRatesSection reports the [shiny_rates] feed state.
func renderShinyRatesSection(ctx *bot.CommandContext, tr translator) string {
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.status.section.shiny_rates"))
	st := ctx.ShinyRates.Status()
	sb.WriteString("\n  ")
	switch {
	case !st.Configured:
		sb.WriteString(tr.T("cmd.poracle_admin.status.label.shiny_rates_off"))
		return sb.String()
	case st.Loaded:
		sb.WriteString(indicatorGreen + " " + tr.Tf("cmd.poracle_admin.status.label.shiny_rates_loaded",
			st.Variants, formatDuration(time.Since(st.LastSuccess).Round(time.Second))))
	default:
		sb.WriteString(indicatorYellow + " " + tr.T("cmd.poracle_admin.status.label.shiny_rates_waiting"))
	}
	if st.LastError != "" {
		sb.WriteString("\n  " + tr.Tf("cmd.poracle_admin.status.label.shiny_rates_error", st.LastError))
	}
	return sb.String()
}
```

(`indicatorGreen`, `indicatorYellow`, `formatDuration` and `translator` already exist in this file.)

- [ ] **Step 7: Run tests and commit**

Run: `cd processor && go test -count=1 ./internal/bot/commands/`
Expected: PASS.

```bash
git add processor/internal/bot/commands processor/internal/i18n/locale/en.json
git commit -m "info, admin status: shiny-rate feed lines

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 14: v1 `personal` field and v2 pokemon booleans

**Files:**
- Modify: `processor/internal/api/trackingMonster.go`, `processor/internal/api/v2_pokemon.go`, `processor/internal/api/testdata/openapi.golden.json`
- Test: `processor/internal/api/trackingmonster_personal_test.go`, `processor/internal/api/v2_pokemon_personal_test.go`

**Interfaces:**
- Consumes: `MonsterTrackingAPI.Personal`, `db.PackPersonal`, `db.PersonalKnownMask`.
- Produces: v1 request field `personal` (flexInt, masked to known bits); v2 rule fields `personal_shiny`, `personal_background` (`*bool`).

- [ ] **Step 1: Write the failing tests**

Model both files on the closest existing tests (`grep -ln "HandleCreateMonster" internal/api/*_test.go`; `v2_pokemon_test.go` for the v2 harness). Cases:

v1 (`trackingmonster_personal_test.go`):
1. POST a new rule with `"personal": 1` → stored `Personal == 1`.
2. POST a by-uid update of that rule with `"min_iv": 90` and **no** `personal` key → stored `Personal` still `1` and `MinIV == 90`.
3. POST a by-uid update with `"personal": 0` → stored `0`.
4. POST a new rule with `"personal": 7` → stored `3` (unknown bits masked).
5. GET returns `"personal":1` in the row JSON.

v2 (`v2_pokemon_personal_test.go`):
1. POST `{"pokemon_id":25,"personal_shiny":true}` → stored `Personal == 1`; GET returns `"personal_shiny":true` and `"personal_background":null`.
2. PUT the rule without either field → stored `0` (full-replace semantics).
3. POST with `"personal_background":true,"personal_shiny":true` → `3`.
4. Unknown field `"personal":1` → 422 (strict schema).

- [ ] **Step 2: Run them to verify they fail**

Run: `cd processor && go test -count=1 -run Personal ./internal/api/`
Expected: FAIL.

- [ ] **Step 3: v1**

In `trackingMonster.go`:
- Add to the request struct after `PVPRankingEvolution`: ``	Personal              flexInt  `json:"personal"` ``
- In `cleanRow`, add `Personal: req.Personal.intValue(0) & db.PersonalKnownMask,`.
- In the handler loop that builds `updates`/`insert`, record explicit-uid rows that omitted `personal`:

```go
		keepPersonal := map[int64]bool{}
		for _, req := range insertReqs {
			...
			if req.UID.isSet() {
				if !req.Personal.isSet() {
					keepPersonal[row.UID] = true
				}
				updates = append(updates, row)
			} else {
				insert = append(insert, row)
			}
		}
```

  then, right after `tracked` is fetched:

```go
		// v1 clients that predate personal shiny re-send rules without
		// "personal"; keep the stored bits rather than silently clearing them.
		if len(keepPersonal) > 0 {
			stored := make(map[int64]int, len(tracked))
			for _, t := range tracked {
				stored[t.UID] = t.Personal
			}
			for i := range updates {
				if keepPersonal[updates[i].UID] {
					updates[i].Personal = stored[updates[i].UID]
				}
			}
		}
```

  The explicit-uid `updates` slice is extended with diff-classified updates afterwards; do the keep step **before** `updates = append(updates, diff.Updates...)`. Confirm `flexInt` has `isSet()` (it is used as `req.UID.isSet()`).

- [ ] **Step 4: v2**

In `v2_pokemon.go`:
- Add to `v2PokemonRule` after `Summary`:

```go
	PersonalShiny      *bool `json:"personal_shiny,omitempty" nullable:"true" doc:"Only alert when the pokemon is shiny for one of the human's linked game accounts (personal bitmask bit 1). Needs the server's [shiny_rates] feed; matches nothing until it loads or while the human has no linked accounts. Omit for false. Returned as null when false."`
	PersonalBackground *bool `json:"personal_background,omitempty" nullable:"true" doc:"Only alert when the pokemon has the special (event) background for one of the human's linked accounts (personal bitmask bit 2). Same requirements as personal_shiny; when both are set both must hold. Omit for false. Returned as null when false."`
```

- In `translateV2Pokemon`: `Personal: db.PackPersonal(valueOr(req.PersonalShiny, false), valueOr(req.PersonalBackground, false)),`
- In `pokemonRowToRule`:

```go
		PersonalShiny:      ptrUnless(db.IsPersonalShiny(row.Personal), false),
		PersonalBackground: ptrUnless(db.IsPersonalBackground(row.Personal), false),
```

- [ ] **Step 5: Regenerate the OpenAPI golden file**

```bash
cd processor && UPDATE_GOLDEN=1 go test -count=1 -run Golden ./internal/api/ && git diff --stat internal/api/testdata/openapi.golden.json
go test -count=1 ./internal/api/
```

Expected: golden diff only adds the two pokemon rule properties; tests PASS.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/api
git commit -m "api: personal field on v1 monsters (kept when omitted) and v2 booleans

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 15: v2 accounts endpoints, snapshot, health capability

**Files:**
- Create: `processor/internal/api/v2_accounts.go`, `processor/internal/api/v2_accounts_test.go`
- Modify: `processor/internal/api/tracking.go` (`TrackingDeps.Accounts`), `processor/internal/api/v2_snapshot.go`, `processor/internal/api/api.go` (`Capabilities.PersonalShiny`), `processor/cmd/processor/main.go`, `processor/internal/api/testdata/openapi.golden.json`

**Interfaces:**
- Consumes: `store.PlayerAccountStore` + errors, `reloadState(deps)`, `okStatus()`, `v2HumanIDInput`, `muteResolveHuman`-style 404 lookup.
- Produces:
  - `TrackingDeps.Accounts store.PlayerAccountStore`
  - `api.RegisterV2Accounts(humaAPI huma.API, deps *TrackingDeps)`
  - `GET|POST /v2/humans/{id}/accounts`, `PUT|DELETE /v2/humans/{id}/accounts/{player_id}`
  - v2 snapshot field `accounts`, capability `personalShiny`.

- [ ] **Step 1: Write the failing tests**

Create `processor/internal/api/v2_accounts_test.go` using the v2 test harness that `v2_locations_test.go` uses (`grep -n "^func " internal/api/v2_locations_test.go | head`). Wire `deps.Accounts = store.NewMockPlayerAccountStore()`. Cases:
1. `GET /api/v2/humans/u1/accounts` on a known human with none → `200 {"accounts":[]}`; unknown human → 404.
2. `POST {"player_id":"User_ABC","label":"main"}` → 201, then GET lists `{"player_id":"User_ABC","label":"main","created_at":…}`.
3. Same POST again → 409; POST `{"player_id":"x","label":"MAIN"}` → 409; POST `{"player_id":""}` → 422; POST with `player_id` of 257 bytes → 422; 51st account → 422.
4. `PUT /accounts/User_ABC {"label":"alt"}` → 200; PUT on `/accounts/nope` → 404; PUT `{"label":""}` clears the label (GET shows `"label":null`).
5. `DELETE /accounts/User%2FABC` for an ID containing `/` (add it first) → 200, gone afterwards.
6. `DELETE` missing → 404.
7. `self_service=false` in config does not block the API.
8. The full snapshot `GET /api/v2/humans/u1/tracking` includes `"accounts":[…]`.
9. `/health` capabilities include `"personalShiny":true` (extend the existing capabilities test instead if one exists).

- [ ] **Step 2: Run them to verify they fail**

Run: `cd processor && go test -count=1 -run 'Accounts|Capabilit' ./internal/api/`
Expected: FAIL.

- [ ] **Step 3: Implement the endpoints**

Add to `TrackingDeps` in `tracking.go`:

```go
	Accounts     store.PlayerAccountStore   // linked game accounts (v2 accounts endpoints + snapshot); nil-safe in snapshot
```

Create `processor/internal/api/v2_accounts.go`:

```go
package api

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2_accounts.go is the strict /api/v2 surface for a human's linked game
// accounts (player IDs) used by personal shiny/background tracking. The API
// is trusted (it requires the API secret), so [player_accounts]
// self_service does not apply here.

type v2AccountItem struct {
	PlayerID  string    `json:"player_id" doc:"Game player ID, opaque and case-sensitive (1-256 bytes)."`
	Label     *string   `json:"label" nullable:"true" doc:"Optional label, unique per human (case-insensitive). null when unset."`
	CreatedAt time.Time `json:"created_at" doc:"When the account was linked."`
}

type v2AccountsOutput struct {
	Body struct {
		Accounts []v2AccountItem `json:"accounts"`
	}
}

type v2AddAccountBody struct {
	PlayerID string `json:"player_id" required:"true" minLength:"1" maxLength:"256" doc:"Game player ID (case-sensitive)."`
	Label    string `json:"label,omitempty" maxLength:"64" doc:"Optional label, unique per human."`
}

type v2AddAccountInput struct {
	ID   string `path:"id" doc:"Human id"`
	Body v2AddAccountBody
}

type v2AccountPathInput struct {
	ID       string `path:"id" doc:"Human id"`
	PlayerID string `path:"player_id" doc:"Game player ID (URL-escaped)"`
}

type v2LabelAccountInput struct {
	ID       string `path:"id" doc:"Human id"`
	PlayerID string `path:"player_id" doc:"Game player ID (URL-escaped)"`
	Body     struct {
		Label string `json:"label" maxLength:"64" doc:"New label; empty clears it."`
	}
}

type v2AccountCreatedOutput struct {
	Status int
	Body   struct {
		Account v2AccountItem `json:"account"`
	}
}

func accountToItem(a db.PlayerAccount) v2AccountItem {
	it := v2AccountItem{PlayerID: a.PlayerID, CreatedAt: a.CreatedAt}
	if a.Label != "" {
		l := a.Label
		it.Label = &l
	}
	return it
}

func accountStoreError(op, id string, err error) error {
	switch {
	case errors.Is(err, store.ErrDuplicateAccount):
		return huma.Error409Conflict("player id already linked")
	case errors.Is(err, store.ErrDuplicateAccountLabel):
		return huma.Error409Conflict("label already in use")
	case errors.Is(err, store.ErrTooManyAccounts), errors.Is(err, store.ErrInvalidPlayerID), errors.Is(err, store.ErrInvalidAccountLabel):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, store.ErrAccountNotFound):
		return huma.Error404NotFound("account not found")
	}
	log.Errorf("v2 accounts: %s %s: %s", op, id, err)
	return huma.Error500InternalServerError("database error")
}

// RegisterV2Accounts registers /api/v2/humans/{id}/accounts.
func RegisterV2Accounts(humaAPI huma.API, deps *TrackingDeps) {
	tag := []string{"v2-accounts"}
	sec := []map[string][]string{{"poracleSecret": {}}}

	huma.Register(humaAPI, huma.Operation{
		OperationID: "v2-list-human-accounts", Method: "GET", Path: "/v2/humans/{id}/accounts",
		Summary: "List a human's linked game accounts", Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*v2AccountsOutput, error) {
		if _, err := muteResolveHuman(deps, in.ID); err != nil {
			return nil, err
		}
		list, err := deps.Accounts.List(in.ID)
		if err != nil {
			return nil, accountStoreError("list", in.ID, err)
		}
		out := &v2AccountsOutput{}
		out.Body.Accounts = make([]v2AccountItem, 0, len(list))
		for _, a := range list {
			out.Body.Accounts = append(out.Body.Accounts, accountToItem(a))
		}
		return out, nil
	})

	huma.Register(humaAPI, huma.Operation{
		OperationID: "v2-add-human-account", Method: "POST", Path: "/v2/humans/{id}/accounts", DefaultStatus: 201,
		Summary:     "Link a game account",
		Description: "Links a player ID to the human. 409 if the player ID or label is already linked to this human; 422 when invalid or over the 50-account limit. Triggers a state reload.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2AddAccountInput) (*v2AccountCreatedOutput, error) {
		if _, err := muteResolveHuman(deps, in.ID); err != nil {
			return nil, err
		}
		if err := deps.Accounts.Add(in.ID, in.Body.PlayerID, in.Body.Label); err != nil {
			return nil, accountStoreError("add", in.ID, err)
		}
		reloadState(deps)
		a, err := deps.Accounts.Resolve(in.ID, in.Body.PlayerID)
		if err != nil || a == nil {
			return nil, accountStoreError("add", in.ID, errors.Join(err, store.ErrAccountNotFound))
		}
		out := &v2AccountCreatedOutput{Status: 201}
		out.Body.Account = accountToItem(*a)
		return out, nil
	})

	huma.Register(humaAPI, huma.Operation{
		OperationID: "v2-label-human-account", Method: "PUT", Path: "/v2/humans/{id}/accounts/{player_id}",
		Summary: "Set or clear a linked account's label", Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2LabelAccountInput) (*statusOKOutput, error) {
		if err := deps.Accounts.SetLabel(in.ID, in.PlayerID, in.Body.Label); err != nil {
			return nil, accountStoreError("label", in.ID, err)
		}
		reloadState(deps)
		return okStatus(), nil
	})

	huma.Register(humaAPI, huma.Operation{
		OperationID: "v2-delete-human-account", Method: "DELETE", Path: "/v2/humans/{id}/accounts/{player_id}",
		Summary: "Unlink a game account", Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2AccountPathInput) (*statusOKOutput, error) {
		if err := deps.Accounts.Remove(in.ID, in.PlayerID); err != nil {
			return nil, accountStoreError("remove", in.ID, err)
		}
		reloadState(deps)
		return okStatus(), nil
	})
}
```

Before writing, check how existing v2 endpoints return 201 (`grep -rn "DefaultStatus\|Status int" internal/api/v2_*.go | head`) and follow that exactly. Rename `muteResolveHuman` to `v2ResolveHuman` only if you also update its callers; otherwise reuse it as is. Confirm huma unescapes `%2F` in `{player_id}`; if the gin router rejects encoded slashes, add a test-proven note to the doc string ("player IDs containing `/` must be URL-escaped") and check gin's `UseRawPath`/`UnescapePathValues` settings in `main.go`.

In `processor/cmd/processor/main.go`, set `Accounts: store.NewPlayerAccountStore(database),` in `trackingDeps` (reuse the same instance for `sharedBotDeps.PlayerAccounts` from Task 10 by creating it once as `playerAccounts := store.NewPlayerAccountStore(database)`), and call `api.RegisterV2Accounts(humaAPI, trackingDeps)` next to `api.RegisterV2Mutes(humaAPI, trackingDeps)`.

- [ ] **Step 4: Snapshot and capability**

In `v2_snapshot.go`, add to the snapshot body struct after `Mutes`:

```go
	Accounts  []v2AccountItem              `json:"accounts" doc:"Linked game accounts — same items as GET …/accounts."`
```

and where `out.Body.Mutes = snapshotMutes(deps, human.ID)` is set:

```go
		out.Body.Accounts = snapshotAccounts(deps, human.ID)
```

with

```go
// snapshotAccounts lists the human's linked accounts; nil store ⇒ empty.
func snapshotAccounts(deps *TrackingDeps, id string) []v2AccountItem {
	out := make([]v2AccountItem, 0)
	if deps.Accounts == nil {
		return out
	}
	list, err := deps.Accounts.List(id)
	if err != nil {
		log.Errorf("v2 snapshot: accounts %s: %s", id, err)
		return out
	}
	for _, a := range list {
		out = append(out, accountToItem(a))
	}
	return out
}
```

In `api.go` `Capabilities`, add:

```go
	// PersonalShiny reports per-player shiny/background tracking: the
	// monsters personal bits (v1 `personal`, v2 personal_shiny /
	// personal_background), /api/v2/humans/{id}/accounts, and the
	// personal* DTS fields. Static: whether the feed is configured is a
	// runtime matter (rules simply never match without it).
	PersonalShiny bool `json:"personalShiny"`
```

and set `PersonalShiny: true` in `BuildCapabilities`.

- [ ] **Step 5: Golden file and tests**

```bash
cd processor && UPDATE_GOLDEN=1 go test -count=1 -run Golden ./internal/api/ && go test -count=1 ./internal/api/ ./cmd/processor/
```

Expected: PASS; golden diff adds the four account operations, the snapshot `accounts` field and nothing else.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/api processor/cmd/processor/main.go
git commit -m "api: v2 linked-account endpoints, snapshot accounts, personalShiny capability

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 16: Documentation and full gate

**Files:**
- Modify: `API.md`, `DTS.md`, `CLAUDE.md`, `CHANGELOG.md`, `docs/superpowers/specs/2026-10-12-personal-shiny-design.md` (status line)

- [ ] **Step 1: API.md**

Add:
- Under the v1 monsters tracking fields: `personal` (int bitmask: 1 = personal shiny, 2 = personal background; unknown bits are dropped; **omitted on a by-uid update ⇒ the stored value is kept**).
- Under v2 pokemon: `personal_shiny`, `personal_background` (booleans, null when false; full-replace on PUT).
- A new "Linked accounts" subsection under the v2 humans section documenting the four endpoints, status codes (201/404/409/422), the 50-account and 256-byte limits, case sensitivity, URL-escaping of `player_id`, and that `self_service` does not apply to the API.
- The v2 snapshot's `accounts` array.
- The `/health` capability `personalShiny`.

- [ ] **Step 2: DTS.md**

Add the per-user fields `personalShiny`, `personalShinyAccounts`, `personalBackground`, `personalBackgroundAccounts`, `hasLinkedAccounts` (pokemon only; empty in `!poracle-test`/editor previews; set for every matched user with linked accounts, whichever rule matched), and the base fields `shinyRate`, `shinyRateBoosted` (pokemon, quest, nest; only with the feed), with the example `{{#if personalShiny}}✨ Shiny for {{join personalShinyAccounts ", "}}{{/if}}`. Note that `shinyPossible` is feed-backed (variant-aware) when `[shiny_rates]` is configured and `shinyStats` keeps its observed meaning.

- [ ] **Step 3: CLAUDE.md**

Add a short "Personal Shiny" section after "Mute Infrastructure":

```markdown
## Personal Shiny

`internal/personalshiny/` copies Golbat#423's per-player shiny/background roll (SHA-1 of LE encounter ID ‖ player ID seeding `java.util.Random`, draws against the variant's rate) and its rate feed (`[shiny_rates] url` → `GET <url>/v1/rates?compact=1`). Keep it diffable with Golbat: see `internal/personalshiny/SYNC.md` (file mapping, pinned commit, deviations, update recipe). Users link game accounts (`player_accounts` table, `!account`, `/api/v2/humans/{id}/accounts`; `[player_accounts] self_service` gates non-admin linking). Accounts load onto `db.Human.Accounts` in state. `monsters.personal` is a bitmask (1 shiny, 2 background, 4/8 reserved for per-player sizes); the pokemon handler builds a per-webhook `personalshiny.Checker` that the matcher consults last and `enrichment.AnnotatePersonal` uses for the per-user `personal*` DTS fields. Fail closed: before the feed's first load personal rules match nothing. No Ditto/disguised/nearby-only predictions (Golbat's eligibility). The same feed backs `shinyPossible`/`shinyRate` (`enrichment/shinyinfo.go`), falling back to observed stats when the feed is absent or does not list the variant. Player IDs are case-sensitive: `!account` reads `CommandContext.VerbatimArgs`, and hosts resolve targets via `bot.BuildTargetFor`, which for `cmd.account` only recognises `user:`/`name:` overrides, so IDs starting with `user`/`name` survive.
```

Also: add `player_accounts` to the "Database" key-tables list, add the four account endpoints and `personalShiny` to the API table/capabilities text, and mention `!account` under "Key Commands". Mention `BuildTargetFor` in the target-resolution section ("`cmd.account` only recognises `user:`/`name:` with a colon").

- [ ] **Step 4: CHANGELOG.md**

Add an "Unreleased" entry following the file's existing format: personal shiny/background tracking (mirroring Golbat#423), `!account` / `/account`, `[shiny_rates]` and `[player_accounts]` config, feed-backed `shinyPossible` plus `shinyRate`/`shinyRateBoosted`, migration `000010`.

- [ ] **Step 5: Spec status**

In the spec, change `**Status:** proposed` to `**Status:** implemented (see plans/2026-10-12-personal-shiny.md)`.

- [ ] **Step 6: Full gate**

Run from `processor/`:

```bash
go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...
```

Expected: all four pass. Then check by hand that no repo file names any other community port (Global Constraints).

- [ ] **Step 7: Commit**

```bash
git add API.md DTS.md CLAUDE.md CHANGELOG.md docs/superpowers/specs/2026-10-12-personal-shiny-design.md
git commit -m "docs: personal shiny and linked accounts

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
