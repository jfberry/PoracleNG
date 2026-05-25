# Per-rule location & area overrides — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users save named locations and apply per-rule location / area overrides on any of the 10 tracking types, with bot commands, slash commands, and REST API surfaces.

**Architecture:** A new `user_locations` table holds per-user named locations. Every tracking table gains two nullable columns (`override_location_label`, `override_areas`). The matcher's `ValidateHumansGeneric` reads these at match time, falling through to the human's defaults when NULL. No DB FKs — referential integrity is enforced by the existing manual cleanup routine.

**Tech Stack:** Go, sqlx + MySQL, gin for HTTP, discordgo for slash commands, jq + jsoniter for tests.

**Spec:** `docs/superpowers/specs/2026-05-25-per-rule-location-area-overrides-design.md`

---

## Phase 1 — Schema + state foundation

### Task 1: Migration

**Files:**
- Create: `processor/internal/db/migrations/000004_per_rule_overrides.up.sql`
- Create: `processor/internal/db/migrations/000004_per_rule_overrides.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- 000004_per_rule_overrides.up.sql
CREATE TABLE user_locations (
  uid        INT PRIMARY KEY AUTO_INCREMENT,
  id         VARCHAR(50) NOT NULL,
  label      VARCHAR(64) NOT NULL,
  latitude   DOUBLE NOT NULL,
  longitude  DOUBLE NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_id_label (id, label),
  INDEX idx_id (id)
);

ALTER TABLE monsters
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;
ALTER TABLE raid
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;
ALTER TABLE egg
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;
ALTER TABLE quest
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;
ALTER TABLE invasion
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;
ALTER TABLE lures
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;
ALTER TABLE nests
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;
ALTER TABLE gym
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;
ALTER TABLE forts
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;
ALTER TABLE maxbattle
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;
```

- [ ] **Step 2: Write the down migration**

```sql
-- 000004_per_rule_overrides.down.sql
ALTER TABLE maxbattle DROP COLUMN override_areas, DROP COLUMN override_location_label;
ALTER TABLE forts     DROP COLUMN override_areas, DROP COLUMN override_location_label;
ALTER TABLE gym       DROP COLUMN override_areas, DROP COLUMN override_location_label;
ALTER TABLE nests     DROP COLUMN override_areas, DROP COLUMN override_location_label;
ALTER TABLE lures     DROP COLUMN override_areas, DROP COLUMN override_location_label;
ALTER TABLE invasion  DROP COLUMN override_areas, DROP COLUMN override_location_label;
ALTER TABLE quest     DROP COLUMN override_areas, DROP COLUMN override_location_label;
ALTER TABLE egg       DROP COLUMN override_areas, DROP COLUMN override_location_label;
ALTER TABLE raid      DROP COLUMN override_areas, DROP COLUMN override_location_label;
ALTER TABLE monsters  DROP COLUMN override_areas, DROP COLUMN override_location_label;
DROP TABLE user_locations;
```

- [ ] **Step 3: Boot the processor against an existing DB to apply the migration**

Run: `cd processor && go run ./cmd/processor -basedir ..`
Expected: log line `Applied migration 000004_per_rule_overrides`, then the processor starts cleanly. Stop with Ctrl-C.

- [ ] **Step 4: Verify schema**

Run: `mysql -u <user> -p<pass> <db> -e "SHOW CREATE TABLE user_locations; SHOW COLUMNS FROM monsters LIKE 'override_%';"`
Expected: table exists with the columns above; `monsters` shows both override columns as nullable.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/db/migrations/000004_per_rule_overrides.up.sql processor/internal/db/migrations/000004_per_rule_overrides.down.sql
git commit -m "db(migration): per-rule location/area overrides + user_locations table"
```

---

### Task 2: DB struct + loader for user_locations

**Files:**
- Create: `processor/internal/db/user_locations.go`
- Test: `processor/internal/db/user_locations_test.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/db/user_locations_test.go
package db

import (
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestLoadUserLocations_GroupsByID(t *testing.T) {
	// Use an in-memory sqlite + this package's helpers if available;
	// otherwise spin up a test MySQL via the existing testdb harness in
	// processor/internal/db/*_test.go. Follow the pattern used in
	// summary_sql_test.go.
	dbx := openTestDB(t) // helper from existing tests
	defer dbx.Close()

	mustExec(t, dbx, `INSERT INTO user_locations (id, label, latitude, longitude) VALUES
		('u1', 'Home', 51.5, -0.1),
		('u1', 'Work', 51.6, -0.2),
		('u2', 'Home', 40.7, -74.0)`)

	got, err := LoadUserLocations(dbx)
	if err != nil {
		t.Fatalf("LoadUserLocations: %v", err)
	}
	if len(got["u1"]) != 2 || len(got["u2"]) != 1 {
		t.Fatalf("grouping wrong: %+v", got)
	}
	if got["u1"]["home"].Latitude != 51.5 {
		t.Fatalf("expected lowercased-key lookup, got %+v", got["u1"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/db -run TestLoadUserLocations -v`
Expected: FAIL — `LoadUserLocations undefined`.

- [ ] **Step 3: Implement**

```go
// processor/internal/db/user_locations.go
package db

import (
	"strings"

	"github.com/jmoiron/sqlx"
)

// UserLocation is a row from the user_locations table.
type UserLocation struct {
	UID       int64   `db:"uid"`
	ID        string  `db:"id"`
	Label     string  `db:"label"`
	Latitude  float64 `db:"latitude"`
	Longitude float64 `db:"longitude"`
}

// LoadUserLocations loads every saved user location and indexes them by
// (human id) → (lowercased label) → location. Lookups in the matcher are
// case-insensitive, so we lowercase here once at load rather than at each
// match.
func LoadUserLocations(dbx *sqlx.DB) (map[string]map[string]*UserLocation, error) {
	var rows []UserLocation
	if err := dbx.Select(&rows, `SELECT uid, id, label, latitude, longitude FROM user_locations`); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]*UserLocation)
	for i := range rows {
		r := &rows[i]
		m, ok := out[r.ID]
		if !ok {
			m = make(map[string]*UserLocation)
			out[r.ID] = m
		}
		m[strings.ToLower(r.Label)] = r
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd processor && go test ./internal/db -run TestLoadUserLocations -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/db/user_locations.go processor/internal/db/user_locations_test.go
git commit -m "db: LoadUserLocations groups saved locations by human id, lowercased key"
```

---

### Task 3: Store interface + SQL implementation for user locations

**Files:**
- Modify: `processor/internal/store/human.go` (add UserLocation type + extend HumanStore interface)
- Create: `processor/internal/store/user_locations_sql.go`
- Test: `processor/internal/store/user_locations_sql_test.go`

- [ ] **Step 1: Add UserLocation type + extend interface**

In `processor/internal/store/human.go`, add to the type list near `Profile`:

```go
// UserLocation is a per-user saved named location.
type UserLocation struct {
	UID       int64
	ID        string
	Label     string
	Latitude  float64
	Longitude float64
}

// ReferencingRule identifies one tracking rule that references a saved
// location label. Returned by CountLocationReferences so the !location
// remove command can list them.
type ReferencingRule struct {
	Type string // "pokemon", "raid", ..., matches the URL path on /api/tracking/<type>/
	UID  int64
}
```

Extend `HumanStore` interface with:

```go
	// --- Saved locations ---

	// ListLocations returns every saved location for the given human id,
	// ordered by label.
	ListLocations(id string) ([]UserLocation, error)

	// GetLocation returns one saved location by case-insensitive label,
	// or nil if not found.
	GetLocation(id, label string) (*UserLocation, error)

	// AddLocation inserts a new saved location. Returns an error
	// containing "duplicate" in its text if the label already exists
	// for this human (callers test with errors.Is or strings.Contains).
	AddLocation(loc UserLocation) (int64, error)

	// DeleteLocation removes the named saved location by case-insensitive
	// label match. Returns nil if the location did not exist
	// (idempotent — callers should validate existence before calling if
	// they want a "not found" response).
	DeleteLocation(id, label string) error

	// CountLocationReferences returns every tracking rule (across all 10
	// tracking tables) whose override_location_label matches the given
	// label for this human. Used by !location remove to refuse delete
	// when references exist.
	CountLocationReferences(id, label string) ([]ReferencingRule, error)
```

- [ ] **Step 2: Write the failing test**

```go
// processor/internal/store/user_locations_sql_test.go
package store

import (
	"strings"
	"testing"
)

func TestUserLocationsSQL_RoundTrip(t *testing.T) {
	dbx := openTestDB(t)
	defer dbx.Close()
	s := &sqlHumanStore{db: dbx}

	if _, err := s.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1}); err != nil {
		t.Fatalf("AddLocation: %v", err)
	}
	if _, err := s.AddLocation(UserLocation{ID: "u1", Label: "Work", Latitude: 51.6, Longitude: -0.2}); err != nil {
		t.Fatalf("AddLocation: %v", err)
	}

	list, err := s.ListLocations("u1")
	if err != nil || len(list) != 2 {
		t.Fatalf("ListLocations: got %d rows err=%v", len(list), err)
	}

	got, err := s.GetLocation("u1", "home") // lowercase lookup
	if err != nil || got == nil || got.Label != "Home" {
		t.Fatalf("GetLocation case-insensitive: got=%+v err=%v", got, err)
	}

	if _, err := s.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 0, Longitude: 0}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}

	if err := s.DeleteLocation("u1", "WORK"); err != nil {
		t.Fatalf("DeleteLocation: %v", err)
	}
	list, _ = s.ListLocations("u1")
	if len(list) != 1 || list[0].Label != "Home" {
		t.Fatalf("after delete, got %+v", list)
	}
}

func TestCountLocationReferences(t *testing.T) {
	dbx := openTestDB(t)
	defer dbx.Close()
	s := &sqlHumanStore{db: dbx}

	mustExec(t, dbx, `INSERT INTO monsters (id, profile_no, pokemon_id, override_location_label) VALUES ('u1', 0, 25, 'Home'), ('u1', 0, 26, 'Home')`)
	mustExec(t, dbx, `INSERT INTO raid     (id, profile_no, pokemon_id, level, override_location_label) VALUES ('u1', 0, 0, 5, 'home')`)

	refs, err := s.CountLocationReferences("u1", "HOME")
	if err != nil {
		t.Fatalf("CountLocationReferences: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs (2 pokemon + 1 raid), got %d: %+v", len(refs), refs)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd processor && go test ./internal/store -run "TestUserLocationsSQL_RoundTrip|TestCountLocationReferences" -v`
Expected: FAIL — methods undefined.

- [ ] **Step 4: Implement**

```go
// processor/internal/store/user_locations_sql.go
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
)

const mysqlErrDuplicate = 1062

func (s *sqlHumanStore) ListLocations(id string) ([]UserLocation, error) {
	var rows []UserLocation
	err := s.db.Select(&rows,
		`SELECT uid, id, label, latitude, longitude FROM user_locations WHERE id = ? ORDER BY label`, id)
	return rows, err
}

func (s *sqlHumanStore) GetLocation(id, label string) (*UserLocation, error) {
	var row UserLocation
	err := s.db.Get(&row,
		`SELECT uid, id, label, latitude, longitude FROM user_locations WHERE id = ? AND LOWER(label) = LOWER(?) LIMIT 1`,
		id, label)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *sqlHumanStore) AddLocation(loc UserLocation) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO user_locations (id, label, latitude, longitude) VALUES (?, ?, ?, ?)`,
		loc.ID, loc.Label, loc.Latitude, loc.Longitude)
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == mysqlErrDuplicate {
			return 0, fmt.Errorf("duplicate label %q for user %q", loc.Label, loc.ID)
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (s *sqlHumanStore) DeleteLocation(id, label string) error {
	_, err := s.db.Exec(
		`DELETE FROM user_locations WHERE id = ? AND LOWER(label) = LOWER(?)`, id, label)
	return err
}

// trackingTypePathMap maps tracking-table names → URL path segment
// used by the tracking API (and by !tracked output) so referencing-rule
// listings are stable across surfaces.
var trackingTypePathMap = map[string]string{
	"monsters": "pokemon",
	"raid":     "raid",
	"egg":      "egg",
	"quest":    "quest",
	"invasion": "invasion",
	"lures":    "lure",
	"nests":    "nest",
	"gym":      "gym",
	"forts":    "fort",
	"maxbattle": "maxbattle",
}

func (s *sqlHumanStore) CountLocationReferences(id, label string) ([]ReferencingRule, error) {
	var out []ReferencingRule
	for table, typePath := range trackingTypePathMap {
		var uids []int64
		q := fmt.Sprintf(
			`SELECT uid FROM %s WHERE id = ? AND LOWER(override_location_label) = LOWER(?)`,
			table)
		if err := s.db.Select(&uids, q, id, label); err != nil {
			return nil, fmt.Errorf("count refs in %s: %w", table, err)
		}
		for _, u := range uids {
			out = append(out, ReferencingRule{Type: typePath, UID: u})
		}
	}
	// Deterministic order for tests + replies
	sortReferences(out)
	return out, nil
}

func sortReferences(refs []ReferencingRule) {
	// inline sort to avoid pulling in sort just for this helper; few entries.
	for i := 1; i < len(refs); i++ {
		j := i
		for j > 0 && (refs[j-1].Type > refs[j].Type ||
			(refs[j-1].Type == refs[j].Type && refs[j-1].UID > refs[j].UID)) {
			refs[j-1], refs[j] = refs[j], refs[j-1]
			j--
		}
	}
}

// Suppress unused import warning if strings isn't otherwise used.
var _ = strings.ToLower
```

- [ ] **Step 5: Add interface methods to the mock**

In `processor/internal/store/mock_human.go` (existing mock), add:

```go
func (m *MockHumanStore) ListLocations(id string) ([]UserLocation, error) {
	return append([]UserLocation(nil), m.Locations[id]...), nil
}
func (m *MockHumanStore) GetLocation(id, label string) (*UserLocation, error) {
	low := strings.ToLower(label)
	for i := range m.Locations[id] {
		if strings.ToLower(m.Locations[id][i].Label) == low {
			r := m.Locations[id][i]
			return &r, nil
		}
	}
	return nil, nil
}
func (m *MockHumanStore) AddLocation(loc UserLocation) (int64, error) {
	if _, err := m.GetLocation(loc.ID, loc.Label); err == nil {
		// existing — caller would have hit duplicate
	}
	if m.Locations == nil {
		m.Locations = map[string][]UserLocation{}
	}
	loc.UID = int64(len(m.Locations[loc.ID]) + 1)
	m.Locations[loc.ID] = append(m.Locations[loc.ID], loc)
	return loc.UID, nil
}
func (m *MockHumanStore) DeleteLocation(id, label string) error {
	low := strings.ToLower(label)
	in := m.Locations[id]
	out := in[:0]
	for _, l := range in {
		if strings.ToLower(l.Label) != low {
			out = append(out, l)
		}
	}
	m.Locations[id] = out
	return nil
}
func (m *MockHumanStore) CountLocationReferences(id, label string) ([]ReferencingRule, error) {
	return append([]ReferencingRule(nil), m.LocationRefs[id+"|"+strings.ToLower(label)]...), nil
}
```

And add the supporting fields to the `MockHumanStore` struct:

```go
	Locations    map[string][]UserLocation
	LocationRefs map[string][]ReferencingRule // keyed by id|lowercased-label
```

- [ ] **Step 6: Run tests**

Run: `cd processor && go test ./internal/store -run "TestUserLocationsSQL_RoundTrip|TestCountLocationReferences" -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/store/human.go processor/internal/store/user_locations_sql.go processor/internal/store/user_locations_sql_test.go processor/internal/store/mock_human.go
git commit -m "store: HumanStore gains user-locations CRUD + CountLocationReferences"
```

---

### Task 4: Wire user_locations into state load + rename humanOwnedTables

**Files:**
- Modify: `processor/internal/db/humans.go` (add `Locations` field to `Human`)
- Modify: `processor/internal/db/human_queries.go` (rename + add)
- Modify: `processor/internal/store/human_sql.go` (rename + add)
- Modify: `processor/internal/state/loader.go` (load + attach)

- [ ] **Step 1: Add `Locations` field to db.Human**

In `processor/internal/db/humans.go`, in the parsed `Human` struct (the one with `Area []string`), add:

```go
	Locations map[string]*UserLocation // lowercased label → location; nil if user has none
```

- [ ] **Step 2: Rename trackingTables → humanOwnedTables in both files; add user_locations**

In `processor/internal/db/human_queries.go`:

```go
// humanOwnedTables lists all tables holding rows keyed by human id that
// must be cleaned up when a human is deleted (since the DB has no FK
// cascades). Includes per-profile tracking tables AND per-user-owned
// helpers like user_locations.
var humanOwnedTables = []string{
	"monsters", "raid", "egg", "quest", "invasion", "weather",
	"lures", "gym", "nests", "maxbattle", "forts",
	"user_locations",
}
```

Replace all uses of `trackingTables` with `humanOwnedTables` in this file.

In `processor/internal/store/human_sql.go`, do the same rename and additions. Both files must agree.

- [ ] **Step 3: Load user locations + attach to humans during state build**

In `processor/internal/state/loader.go`, find where `db.LoadHumans` (or equivalent) is called during state assembly. Right after the humans map is built and before it's frozen into State, add:

```go
	locs, err := db.LoadUserLocations(s.db)
	if err != nil {
		return nil, fmt.Errorf("load user locations: %w", err)
	}
	for id, h := range humans {
		if m, ok := locs[id]; ok {
			h.Locations = m
		}
	}
```

(Adjust variable names to match the surrounding code.)

- [ ] **Step 4: Smoke test by re-running the processor**

Run: `cd processor && go build ./... && go test ./internal/db ./internal/store -count=1`
Expected: clean build + tests pass.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/db/humans.go processor/internal/db/human_queries.go processor/internal/store/human_sql.go processor/internal/state/loader.go
git commit -m "state: load user_locations and attach to humans; rename humanOwnedTables"
```

---

### Task 5: Override columns on all 10 tracking row structs + loaders

**Files:**
- Modify: `processor/internal/db/monsters.go`
- Modify: `processor/internal/db/raid.go`
- Modify: `processor/internal/db/egg.go`
- Modify: `processor/internal/db/quest.go`
- Modify: `processor/internal/db/invasion.go`
- Modify: `processor/internal/db/lures.go`
- Modify: `processor/internal/db/nests.go`
- Modify: `processor/internal/db/gym.go`
- Modify: `processor/internal/db/forts.go`
- Modify: `processor/internal/db/maxbattle.go`
- Modify: `processor/internal/db/tracking_queries.go` (the `*TrackingAPI` JSON DTO structs)
- Modify: `processor/internal/store/tracking_sql.go` (INSERT/UPDATE column lists)

- [ ] **Step 1: Add columns to every `*Tracking` struct**

For each of the 10 `*Tracking` structs in `processor/internal/db/*.go`, add two fields:

```go
	OverrideLocationLabel string   `db:"override_location_label"`
	OverrideAreas         []string `db:"-"` // populated by post-scan; JSON column read separately
	OverrideAreasRaw      string   `db:"override_areas"` // raw JSON column, parsed into OverrideAreas at load
```

In each `Load<Type>s` function, after the `Select`, parse `OverrideAreasRaw` into `OverrideAreas`:

```go
	for i := range rows {
		if rows[i].OverrideAreasRaw != "" {
			_ = json.Unmarshal([]byte(rows[i].OverrideAreasRaw), &rows[i].OverrideAreas)
		}
	}
```

And expand each SQL SELECT to include `override_location_label, COALESCE(override_areas, '') AS override_areas`.

- [ ] **Step 2: Add the same fields to every `*TrackingAPI` struct**

In `processor/internal/db/tracking_queries.go`, every `*TrackingAPI` struct gains:

```go
	OverrideLocationLabel string   `db:"override_location_label" json:"override_location_label" diff:"override_location_label"`
	OverrideAreas         []string `db:"-"                       json:"override_areas"          diff:"override_areas"`
```

(Note: API DTOs hold the parsed slice directly. The `diff:` tag ensures `tracking.ApplyDiff`'s struct-walker treats both as content-affecting columns when deciding insert-vs-update.)

- [ ] **Step 3: Update INSERT/UPDATE SQL in every per-type store**

In `processor/internal/store/tracking_sql.go`, every `Insert<Type>` and `Update<Type>` SQL must include the two new columns. The `override_areas` value comes from json-marshalling the slice (NULL when empty):

```go
func marshalOverrideAreas(areas []string) any {
	if len(areas) == 0 {
		return nil
	}
	b, _ := json.Marshal(areas)
	return string(b)
}
```

And in each per-type Insert/Update, append `override_location_label, override_areas` to the column list with placeholders, passing `nullIfEmpty(row.OverrideLocationLabel)` and `marshalOverrideAreas(row.OverrideAreas)`.

`nullIfEmpty` helper if not already present:

```go
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
```

- [ ] **Step 4: Round-trip test for monsters (other types follow same pattern)**

Add to `processor/internal/store/tracking_test.go`:

```go
func TestMonstersInsertWithOverride(t *testing.T) {
	dbx := openTestDB(t)
	defer dbx.Close()
	s := NewTrackingStores(dbx)
	uid, err := s.Monsters.Insert(&db.MonsterTrackingAPI{
		ID: "u1", ProfileNo: 0, PokemonID: 25,
		OverrideLocationLabel: "Home",
		OverrideAreas:         []string{"berlin", "munich"},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, _ := s.Monsters.SelectByIDProfile("u1", 0)
	if len(rows) != 1 || rows[0].UID != uid {
		t.Fatalf("expected 1 row uid=%d, got %+v", uid, rows)
	}
	if rows[0].OverrideLocationLabel != "Home" {
		t.Fatalf("label round-trip: got %q", rows[0].OverrideLocationLabel)
	}
	if len(rows[0].OverrideAreas) != 2 || rows[0].OverrideAreas[0] != "berlin" {
		t.Fatalf("areas round-trip: got %v", rows[0].OverrideAreas)
	}

	// Insert with no override — both fields nil/empty
	uid2, _ := s.Monsters.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 0, PokemonID: 26})
	rows, _ = s.Monsters.SelectByIDProfile("u1", 0)
	var got *db.MonsterTrackingAPI
	for i := range rows {
		if rows[i].UID == uid2 {
			got = &rows[i]
		}
	}
	if got == nil || got.OverrideLocationLabel != "" || len(got.OverrideAreas) != 0 {
		t.Fatalf("no-override row should have empty fields; got %+v", got)
	}
}
```

- [ ] **Step 5: Run tests + build**

Run: `cd processor && go test ./internal/store -run TestMonstersInsertWithOverride -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/db/ processor/internal/store/tracking_sql.go processor/internal/store/tracking_test.go
git commit -m "tracking: round-trip override_location_label + override_areas on all 10 types"
```

---

## Phase 2 — Matcher

### Task 6: Tests for override semantics in ValidateHumansGeneric

**Files:**
- Test: `processor/internal/matching/generic_override_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// processor/internal/matching/generic_override_test.go
package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func newRule(uid int64, distance int, label string, areas []string) trackingUserData {
	return trackingUserData{
		HumanID:               "u1",
		UID:                   uid,
		Distance:              distance,
		OverrideLocationLabel: label,
		OverrideAreas:         areas,
	}
}

func newHuman(lat, lon float64, areas []string, locs map[string]*db.UserLocation) *db.Human {
	return &db.Human{
		ID: "u1", Enabled: true,
		Latitude: lat, Longitude: lon,
		Area:      areas,
		Locations: locs,
	}
}

func TestOverride_LocationAnchorsDistance(t *testing.T) {
	humans := map[string]*db.Human{"u1": newHuman(0, 0, nil, map[string]*db.UserLocation{
		"home": {Label: "Home", Latitude: 51.5, Longitude: -0.1},
	})}
	rules := []trackingUserData{newRule(1, 500, "Home", nil)}
	// Event at (51.501, -0.1) is ~111m from Home — within 500m
	out := ValidateHumansGeneric(rules, 51.501, -0.1, nil, false, humans, "test")
	if len(out) != 1 {
		t.Fatalf("rule with location override should fire; got %d", len(out))
	}
}

func TestOverride_AreasReplaceHumanAreas(t *testing.T) {
	humans := map[string]*db.Human{"u1": newHuman(0, 0, []string{"london"}, nil)}
	rules := []trackingUserData{newRule(1, 0, "", []string{"berlin"})}
	// Event is in "berlin" only. Without override, human.Area=london wouldn't match.
	out := ValidateHumansGeneric(rules, 52.5, 13.4, map[string]bool{"berlin": true}, false, humans, "test")
	if len(out) != 1 {
		t.Fatalf("override_areas should replace human.Area; got %d", len(out))
	}
}

func TestOverride_OrphanedLabelFallsThrough(t *testing.T) {
	humans := map[string]*db.Human{"u1": newHuman(51.5, -0.1, nil, nil)}
	rules := []trackingUserData{newRule(1, 500, "Home", nil)} // Home not in user.Locations
	// Falls through to human (51.5, -0.1); event close by → still fires
	out := ValidateHumansGeneric(rules, 51.501, -0.1, nil, false, humans, "test")
	if len(out) != 1 {
		t.Fatalf("orphaned override should silently fall through; got %d", len(out))
	}
}

func TestOverride_AreasFireWithoutHumanAreas(t *testing.T) {
	humans := map[string]*db.Human{"u1": newHuman(0, 0, nil, nil)} // no human areas
	rules := []trackingUserData{newRule(1, 0, "", []string{"berlin"})}
	out := ValidateHumansGeneric(rules, 52.5, 13.4, map[string]bool{"berlin": true}, false, humans, "test")
	if len(out) != 1 {
		t.Fatalf("override_areas alone should be enough; got %d", len(out))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/matching -run TestOverride -v`
Expected: FAIL — `trackingUserData` has no `OverrideLocationLabel` / `OverrideAreas` fields yet.

- [ ] **Step 3: Commit the failing tests**

```bash
git add processor/internal/matching/generic_override_test.go
git commit -m "matching: failing tests for per-rule location/area override semantics"
```

---

### Task 7: Implement override in ValidateHumansGeneric

**Files:**
- Modify: `processor/internal/matching/generic.go`

- [ ] **Step 1: Extend trackingUserData**

Add to the struct in `processor/internal/matching/generic.go`:

```go
	OverrideLocationLabel string
	OverrideAreas         []string
```

- [ ] **Step 2: Update every per-type matcher to populate the override fields when building trackingUserData**

In each of `matching/{pokemon,raid,egg,quest,invasion,lure,nest,gym,fort,maxbattle}.go`, where `trackingUserData{...}` literals are constructed, add:

```go
		OverrideLocationLabel: rule.OverrideLocationLabel,
		OverrideAreas:         rule.OverrideAreas,
```

(Adjust `rule` to whatever the local variable is named — typically `mt`, `r`, `rt`, etc.)

- [ ] **Step 3: Update the area/distance branch in ValidateHumansGeneric**

Replace the existing distance/area check (currently using `human.Latitude/Longitude` and `human.Area` directly) with override-aware resolution:

```go
		// Resolve effective anchor location: rule override → human default
		anchorLat, anchorLon := human.Latitude, human.Longitude
		if td.OverrideLocationLabel != "" && human.Locations != nil {
			if loc, ok := human.Locations[strings.ToLower(td.OverrideLocationLabel)]; ok {
				anchorLat, anchorLon = loc.Latitude, loc.Longitude
			}
			// else: orphaned label — silently fall through to human defaults
		}
		// Resolve effective area set: rule override → human default
		effectiveAreas := human.Area
		if len(td.OverrideAreas) > 0 {
			effectiveAreas = td.OverrideAreas
		}

		if td.Distance > 0 {
			dist = HaversineDistance(anchorLat, anchorLon, lat, lon)
			if dist > float64(td.Distance) {
				continue
			}
		} else if !td.IsSpecificMatch {
			if !areaOverlap(effectiveAreas, matchedAreaNames) {
				continue
			}
		}
```

Make sure `import "strings"` is present.

Also: the `bearing` and `Distance` set in the returned `MatchedUser` should use the anchor coords (so `!tracked` and rendered messages show distance from the override, not the human's default). Update:

```go
		bearing := Bearing(anchorLat, anchorLon, lat, lon)
```

And in the returned `MatchedUser`:

```go
			Latitude:  anchorLat,
			Longitude: anchorLon,
```

- [ ] **Step 4: Run tests**

Run: `cd processor && go test ./internal/matching -run TestOverride -v && go test ./internal/matching -count=1`
Expected: all override tests PASS; other matcher tests still PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/matching/
git commit -m "matching: per-rule location/area override in ValidateHumansGeneric + all 10 per-type matchers"
```

---

## Phase 3 — !location command surface

### Task 8: i18n keys for !location subcommands + override errors

**Files:**
- Modify: `processor/internal/i18n/locale/en.json`

- [ ] **Step 1: Add the new keys**

Add these to `processor/internal/i18n/locale/en.json` (English only — Crowdin syncs the rest):

```json
  "msg.location.usage_subcommands": "Usage: `{0}location add <name> <coords-or-place>` | `{0}location list` | `{0}location show <name>` | `{0}location remove <name>` | `{0}location remove default`",
  "msg.location.added": "✅ Saved location `{0}` at {1}, {2}.",
  "msg.location.duplicate": "🙅 You already have a saved location called `{0}`. Remove it first with `{1}location remove {0}`.",
  "msg.location.list_header": "Your saved locations:",
  "msg.location.list_default": "  default: {0}, {1}",
  "msg.location.list_row": "  `{0}`: {1}, {2}",
  "msg.location.list_empty": "You have no saved locations. Add one with `{0}location add <name> <coords-or-place>`.",
  "msg.location.show_not_found": "🙅 No saved location called `{0}`.",
  "msg.location.show": "📍 `{0}` is at {1}, {2}.",
  "msg.location.removed": "✅ Removed saved location `{0}`.",
  "msg.location.default_removed": "✅ Cleared your default location.",
  "msg.location.remove_referenced": "🙅 Cannot remove `{0}` — {1} tracking rule(s) reference it: {2}. Edit or remove those rules first.",
  "msg.location.add_usage": "Usage: `{0}location add <name> <coords-or-place>`",
  "msg.location.show_usage": "Usage: `{0}location show <name>`",
  "msg.location.remove_usage": "Usage: `{0}location remove <name>` or `{0}location remove default`",
  "msg.location.geocode_failed": "🙅 Couldn't find a place matching `{0}`.",
  "msg.override.requires_distance": "🙅 `location:` needs a `d:` distance — e.g. `location:Home d:500`.",
  "msg.override.area_and_distance": "🙅 `area:` and `d:` are mutually exclusive (area-mode vs distance-mode). Pick one.",
  "msg.override.area_and_location": "🙅 `area:` and `location:` are mutually exclusive. Pick one.",
  "msg.override.unknown_location": "🙅 No saved location called `{0}`. Add it with `{1}location add {0} <coords-or-place>`.",
  "msg.override.area_not_permitted": "🙅 Area `{0}` is not in your allowed areas. Use `{1}area list` to see what you can pick.",
  "tracking.override_location_fmt": "@ {0}",
  "tracking.override_areas_fmt": "in {0}"
```

- [ ] **Step 2: Verify JSON is valid**

Run: `jq 'length' processor/internal/i18n/locale/en.json`
Expected: a number larger than before (no error).

- [ ] **Step 3: Commit**

```bash
git add processor/internal/i18n/locale/en.json
git commit -m "i18n: keys for !location subcommands + override validation errors"
```

---

### Task 9: !location subcommand dispatch + add/list

**Files:**
- Modify: `processor/internal/bot/commands/location.go`
- Test: `processor/internal/bot/commands/location_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// processor/internal/bot/commands/location_test.go
package commands

import (
	"strings"
	"testing"
)

func TestLocation_AddSavesNamedLocation(t *testing.T) {
	ctx, store := newTestLocationCtx(t)
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"add", "Home", "51.5,-0.1"})
	if len(replies) == 0 || !strings.Contains(replies[0].Text, "Saved location") {
		t.Fatalf("expected save confirmation, got %+v", replies)
	}
	got, _ := store.GetLocation("u1", "Home")
	if got == nil || got.Latitude != 51.5 {
		t.Fatalf("location not persisted: %+v", got)
	}
}

func TestLocation_AddRejectsDuplicate(t *testing.T) {
	ctx, store := newTestLocationCtx(t)
	store.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"add", "Home", "0,0"})
	if len(replies) == 0 || !strings.Contains(replies[0].Text, "already have") {
		t.Fatalf("expected duplicate error, got %+v", replies)
	}
}

func TestLocation_ListShowsDefaultAndNamed(t *testing.T) {
	ctx, store := newTestLocationCtx(t)
	store.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"list"})
	body := replies[0].Text
	if !strings.Contains(body, "Home") {
		t.Fatalf("list should show named locations, got %s", body)
	}
}
```

(`newTestLocationCtx` builds a CommandContext using the MockHumanStore — pattern matches existing `bot/commands/*_test.go` files.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/bot/commands -run TestLocation_Add -v`
Expected: FAIL — subcommands not implemented.

- [ ] **Step 3: Implement subcommand dispatch + add + list**

In `processor/internal/bot/commands/location.go`, before the existing bare-location/lat-lon handling, add subcommand routing:

```go
func (c *LocationCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")
	matchSub := func(key string) bool {
		if len(args) == 0 {
			return false
		}
		sub := strings.ToLower(args[0])
		return sub == strings.ToLower(tr.T(key)) || sub == strings.ToLower(enTr.T(key))
	}

	switch {
	case matchSub("arg.add"):
		return c.addLocation(ctx, args[1:])
	case matchSub("arg.list"):
		return c.listLocations(ctx)
	case matchSub("arg.show"):
		return c.showLocation(ctx, args[1:])
	case matchSub("arg.remove"):
		return c.removeLocation(ctx, args[1:])
	}

	// ... existing bare !location + lat/lon set-default flow continues below
}
```

Then add:

```go
func (c *LocationCommand) addLocation(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) < 2 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.add_usage", bot.CommandPrefix(ctx))}}
	}
	name := args[0]
	placeOrCoords := strings.Join(args[1:], " ")

	lat, lon, err := resolveLatLon(ctx, placeOrCoords)
	if err != nil {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.geocode_failed", placeOrCoords)}}
	}

	if _, err := ctx.Humans.AddLocation(store.UserLocation{
		ID: ctx.TargetID, Label: name, Latitude: lat, Longitude: lon,
	}); err != nil {
		if strings.Contains(err.Error(), "duplicate") {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.duplicate", name, bot.CommandPrefix(ctx))}}
		}
		log.Errorf("location add: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.location.added", name, lat, lon)}}
}

func (c *LocationCommand) listLocations(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	locs, _ := ctx.Humans.ListLocations(ctx.TargetID)
	human, _ := ctx.Humans.Get(ctx.TargetID)

	var sb strings.Builder
	sb.WriteString(tr.T("msg.location.list_header") + "\n")
	if human != nil && (human.Latitude != 0 || human.Longitude != 0) {
		sb.WriteString(tr.Tf("msg.location.list_default", human.Latitude, human.Longitude) + "\n")
	}
	if len(locs) == 0 && (human == nil || human.Latitude == 0 && human.Longitude == 0) {
		return []bot.Reply{{Text: tr.Tf("msg.location.list_empty", bot.CommandPrefix(ctx))}}
	}
	for _, l := range locs {
		sb.WriteString(tr.Tf("msg.location.list_row", l.Label, l.Latitude, l.Longitude) + "\n")
	}
	return []bot.Reply{{Text: sb.String()}}
}
```

`resolveLatLon` is a helper to reuse the existing lat/lon-or-place parsing path from the bare `!location <args>` flow. Extract it from the existing function if not already a helper.

- [ ] **Step 4: Run tests**

Run: `cd processor && go test ./internal/bot/commands -run TestLocation_ -v`
Expected: PASS for add/list tests; show/remove still fail (next tasks).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/bot/commands/location.go processor/internal/bot/commands/location_test.go
git commit -m "bot(!location): add + list subcommands"
```

---

### Task 10: !location show

**Files:**
- Modify: `processor/internal/bot/commands/location.go`
- Test: `processor/internal/bot/commands/location_test.go`

- [ ] **Step 1: Write the failing test**

Append to `location_test.go`:

```go
func TestLocation_ShowCaseInsensitive(t *testing.T) {
	ctx, store := newTestLocationCtx(t)
	store.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"show", "home"})
	if !strings.Contains(replies[0].Text, "51.5") {
		t.Fatalf("show should be case-insensitive, got %s", replies[0].Text)
	}
}

func TestLocation_ShowNotFound(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"show", "Nope"})
	if !strings.Contains(replies[0].Text, "No saved location") {
		t.Fatalf("expected not-found message, got %s", replies[0].Text)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/bot/commands -run TestLocation_Show -v`
Expected: FAIL — `showLocation` undefined.

- [ ] **Step 3: Implement**

In `processor/internal/bot/commands/location.go`:

```go
func (c *LocationCommand) showLocation(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.show_usage", bot.CommandPrefix(ctx))}}
	}
	loc, _ := ctx.Humans.GetLocation(ctx.TargetID, args[0])
	if loc == nil {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.show_not_found", args[0])}}
	}
	return []bot.Reply{{Text: tr.Tf("msg.location.show", loc.Label, loc.Latitude, loc.Longitude)}}
}
```

- [ ] **Step 4: Run tests**

Run: `cd processor && go test ./internal/bot/commands -run TestLocation_Show -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/bot/commands/location.go processor/internal/bot/commands/location_test.go
git commit -m "bot(!location): show subcommand with case-insensitive lookup"
```

---

### Task 11: !location remove (with refuse-when-referenced) + remove default

**Files:**
- Modify: `processor/internal/bot/commands/location.go`
- Test: `processor/internal/bot/commands/location_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestLocation_RemoveRefusesWhenReferenced(t *testing.T) {
	ctx, store := newTestLocationCtx(t)
	store.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})
	store.LocationRefs = map[string][]ReferencingRule{
		"u1|home": {
			{Type: "pokemon", UID: 42},
			{Type: "raid", UID: 17},
		},
	}
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"remove", "Home"})
	if !strings.Contains(replies[0].Text, "Cannot remove") || !strings.Contains(replies[0].Text, "2 tracking") {
		t.Fatalf("expected refuse-with-count, got %s", replies[0].Text)
	}
	if loc, _ := store.GetLocation("u1", "Home"); loc == nil {
		t.Fatalf("location should NOT have been deleted")
	}
}

func TestLocation_RemoveNamedSucceeds(t *testing.T) {
	ctx, store := newTestLocationCtx(t)
	store.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"remove", "Home"})
	if !strings.Contains(replies[0].Text, "Removed") {
		t.Fatalf("expected success, got %s", replies[0].Text)
	}
	if loc, _ := store.GetLocation("u1", "Home"); loc != nil {
		t.Fatalf("location should be deleted")
	}
}

func TestLocation_RemoveDefault(t *testing.T) {
	ctx, store := newTestLocationCtx(t)
	store.SetLocation("u1", 0, 51.5, -0.1)
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"remove", "default"})
	if !strings.Contains(replies[0].Text, "Cleared") {
		t.Fatalf("expected default-cleared, got %s", replies[0].Text)
	}
}

func TestLocation_RemoveBareErrors(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"remove"})
	if !strings.Contains(replies[0].Text, "Usage:") {
		t.Fatalf("bare remove should show usage, got %s", replies[0].Text)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/bot/commands -run TestLocation_Remove -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
func (c *LocationCommand) removeLocation(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.remove_usage", bot.CommandPrefix(ctx))}}
	}

	target := args[0]
	enTr := ctx.Translations.For("en")
	defaultKW := strings.ToLower(tr.T("arg.default"))
	defaultKWEn := strings.ToLower(enTr.T("arg.default"))
	if strings.ToLower(target) == defaultKW || strings.ToLower(target) == defaultKWEn || strings.ToLower(target) == "default" {
		if err := ctx.Humans.SetLocation(ctx.TargetID, ctx.ProfileNo, 0, 0); err != nil {
			return []bot.Reply{{React: "🙅"}}
		}
		ctx.TriggerReload()
		return []bot.Reply{{React: "✅", Text: tr.T("msg.location.default_removed")}}
	}

	refs, err := ctx.Humans.CountLocationReferences(ctx.TargetID, target)
	if err != nil {
		log.Errorf("location remove count refs: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	if len(refs) > 0 {
		var parts []string
		for _, r := range refs {
			parts = append(parts, fmt.Sprintf("%s id:%d", r.Type, r.UID))
		}
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.location.remove_referenced", target, len(refs), strings.Join(parts, ", "))}}
	}

	if err := ctx.Humans.DeleteLocation(ctx.TargetID, target); err != nil {
		log.Errorf("location remove delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.location.removed", target)}}
}
```

Add the `arg.default` key to `en.json` if not already present:

```json
  "arg.default": "default",
```

- [ ] **Step 4: Run tests**

Run: `cd processor && go test ./internal/bot/commands -run TestLocation_ -v`
Expected: PASS for all !location tests.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/bot/commands/location.go processor/internal/bot/commands/location_test.go processor/internal/i18n/locale/en.json
git commit -m "bot(!location): remove with refuse-when-referenced + remove default"
```

---

## Phase 4 — Tracking commands

### Task 12: New prefix param types for area: (multi) and location:

**Files:**
- Modify: `processor/internal/bot/argmatch.go`
- Test: `processor/internal/bot/argmatch_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestArgMatch_AreaMultiple(t *testing.T) {
	args := []string{"area:berlin", "area:munich,hamburg", "area:Frankfurt"}
	parsed := newTestMatcher(t).Match(args, []ParamDef{
		{Type: ParamPrefixStringList, Key: "arg.prefix.area"},
	}, "en")
	got := parsed.StringLists["area"]
	want := []string{"berlin", "munich", "hamburg", "frankfurt"} // lowercased + comma-split
	if !slicesEqual(got, want) {
		t.Fatalf("area list: got %v, want %v", got, want)
	}
}

func TestArgMatch_LocationSingle(t *testing.T) {
	args := []string{"location:Home"}
	parsed := newTestMatcher(t).Match(args, []ParamDef{
		{Type: ParamPrefixString, Key: "arg.prefix.location"},
	}, "en")
	if parsed.Strings["location"] != "Home" {
		t.Fatalf("location: got %q", parsed.Strings["location"])
	}
}
```

(Test helper `slicesEqual` is a basic `len + each` comparison; add inline if not present.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/bot -run "TestArgMatch_AreaMultiple|TestArgMatch_LocationSingle" -v`
Expected: FAIL — `ParamPrefixStringList` undefined / `arg.prefix.area` not handled.

- [ ] **Step 3: Implement**

In `processor/internal/bot/argmatch.go`:

1. Add the new param type constant:

```go
const (
	// ... existing
	ParamPrefixStringList
)
```

2. Add `StringLists` map to `ParsedArgs`:

```go
type ParsedArgs struct {
	// ... existing fields
	StringLists map[string][]string // for ParamPrefixStringList; comma-split, lowercased
}
```

3. Initialize the map and handle the new param type in `Match()`. For each token matching `<prefix>:<value>`:

```go
		case ParamPrefixStringList:
			for _, v := range strings.Split(value, ",") {
				v = strings.TrimSpace(strings.ToLower(v))
				if v == "" {
					continue
				}
				parsed.StringLists[key] = append(parsed.StringLists[key], v)
			}
			consumed[i] = true
```

4. Register the `arg.prefix.area` and `arg.prefix.location` keys in i18n:

In `processor/internal/i18n/locale/en.json`:

```json
  "arg.prefix.area": "area",
  "arg.prefix.location": "location",
```

- [ ] **Step 4: Run tests**

Run: `cd processor && go test ./internal/bot -run "TestArgMatch_" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/bot/argmatch.go processor/internal/bot/argmatch_test.go processor/internal/i18n/locale/en.json
git commit -m "bot(argmatch): ParamPrefixStringList + arg.prefix.{area,location} param keys"
```

---

### Task 13: Shared override-validation helper for tracking commands

**Files:**
- Create: `processor/internal/bot/commands/override.go`
- Test: `processor/internal/bot/commands/override_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// processor/internal/bot/commands/override_test.go
package commands

import (
	"strings"
	"testing"
)

func TestParseOverride_LocationRequiresDistance(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	got, reply := parseOverride(ctx, map[string]string{"location": "Home"}, nil, 0)
	if reply == nil || !strings.Contains(reply.Text, "needs a `d:`") {
		t.Fatalf("expected requires-distance error, got %+v / %+v", got, reply)
	}
}

func TestParseOverride_AreaAndDistanceRejected(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	_, reply := parseOverride(ctx, nil, []string{"berlin"}, 500)
	if reply == nil || !strings.Contains(reply.Text, "mutually exclusive") {
		t.Fatalf("expected a+d rejection, got %+v", reply)
	}
}

func TestParseOverride_LocationAndAreaRejected(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	_, reply := parseOverride(ctx, map[string]string{"location": "Home"}, []string{"berlin"}, 500)
	if reply == nil || !strings.Contains(reply.Text, "mutually exclusive") {
		t.Fatalf("expected location+area rejection, got %+v", reply)
	}
}

func TestParseOverride_UnknownLocation(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	_, reply := parseOverride(ctx, map[string]string{"location": "Nope"}, nil, 500)
	if reply == nil || !strings.Contains(reply.Text, "No saved location") {
		t.Fatalf("expected unknown-location error, got %+v", reply)
	}
}

func TestParseOverride_AreaNotPermitted(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	// AreaLogic mock should allow only "london"
	_, reply := parseOverride(ctx, nil, []string{"berlin"}, 0)
	if reply == nil || !strings.Contains(reply.Text, "not in your allowed") {
		t.Fatalf("expected permission error, got %+v", reply)
	}
}

func TestParseOverride_ValidLocation(t *testing.T) {
	ctx, store := newTestLocationCtx(t)
	store.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})
	got, reply := parseOverride(ctx, map[string]string{"location": "home"}, nil, 500)
	if reply != nil {
		t.Fatalf("valid case rejected: %+v", reply)
	}
	if got.LocationLabel != "Home" {
		t.Fatalf("label not normalised to stored case: %+v", got)
	}
}

func TestParseOverride_ValidAreas(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	got, reply := parseOverride(ctx, nil, []string{"london"}, 0)
	if reply != nil {
		t.Fatalf("valid case rejected: %+v", reply)
	}
	if len(got.Areas) != 1 || got.Areas[0] != "london" {
		t.Fatalf("areas not stored: %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/bot/commands -run TestParseOverride -v`
Expected: FAIL — `parseOverride` undefined.

- [ ] **Step 3: Implement**

```go
// processor/internal/bot/commands/override.go
package commands

import (
	"github.com/pokemon/poracleng/processor/internal/bot"
)

// Override holds the resolved per-rule override fields ready to drop
// onto a *TrackingAPI insert struct. Empty fields mean "no override".
type Override struct {
	LocationLabel string
	Areas         []string
}

// parseOverride takes the parsed `location:` and `area:` tokens plus
// the rule's own `distance:` value and resolves them into an Override
// (or returns a Reply explaining the rejection). All four mutually-
// exclusive combinations from the spec are enforced here so each
// tracking command stays one helper call away from the rule.
func parseOverride(ctx *bot.CommandContext, strings_ map[string]string, areas []string, distance int) (Override, *bot.Reply) {
	tr := ctx.Tr()
	var locLabel string
	if strings_ != nil {
		locLabel = strings_["location"]
	}

	hasLocation := locLabel != ""
	hasAreas := len(areas) > 0

	if hasLocation && hasAreas {
		return Override{}, &bot.Reply{React: "🙅", Text: tr.T("msg.override.area_and_location")}
	}
	if hasAreas && distance > 0 {
		return Override{}, &bot.Reply{React: "🙅", Text: tr.T("msg.override.area_and_distance")}
	}
	if hasLocation && distance == 0 {
		return Override{}, &bot.Reply{React: "🙅", Text: tr.T("msg.override.requires_distance")}
	}

	out := Override{}

	if hasLocation {
		loc, _ := ctx.Humans.GetLocation(ctx.TargetID, locLabel)
		if loc == nil {
			return Override{}, &bot.Reply{React: "🙅", Text: tr.Tf("msg.override.unknown_location", locLabel, bot.CommandPrefix(ctx))}
		}
		out.LocationLabel = loc.Label // normalize to stored case
	}

	if hasAreas {
		human := getUserHuman(ctx) // existing helper
		permitted := ctx.AreaLogic.PermittedAreaSet(human) // returns map[string]bool of lowercased names
		for _, a := range areas {
			if !permitted[a] {
				return Override{}, &bot.Reply{React: "🙅", Text: tr.Tf("msg.override.area_not_permitted", a, bot.CommandPrefix(ctx))}
			}
		}
		out.Areas = append(out.Areas, areas...)
	}

	return out, nil
}
```

If `AreaLogic.PermittedAreaSet` doesn't exist, add it (delegates to existing `GetAvailableAreasMarked` / `ResolveDisplayNames` logic, returning a lowercased-name set).

- [ ] **Step 4: Run tests**

Run: `cd processor && go test ./internal/bot/commands -run TestParseOverride -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/bot/commands/override.go processor/internal/bot/commands/override_test.go processor/internal/bot/
git commit -m "bot: parseOverride helper enforces 4 mutually-exclusive override rules"
```

---

### Task 14: Wire override params into !track (pokemon)

**Files:**
- Modify: `processor/internal/bot/commands/track.go`
- Test: `processor/internal/bot/commands/track_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestTrack_AcceptsLocationOverride(t *testing.T) {
	ctx, store := newTestTrackCtx(t)
	store.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})

	cmd := &TrackCommand{}
	replies := cmd.Run(ctx, []string{"pikachu", "iv100", "d:500", "location:Home"})
	if anyReact(replies, "🙅") {
		t.Fatalf("valid command rejected: %+v", replies)
	}
	rules, _ := ctx.Tracking.Monsters.SelectByIDProfile("u1", 0)
	if len(rules) == 0 || rules[0].OverrideLocationLabel != "Home" {
		t.Fatalf("override not stored: %+v", rules)
	}
}

func TestTrack_RejectsLocationWithoutDistance(t *testing.T) {
	ctx, store := newTestTrackCtx(t)
	store.AddLocation(UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})
	cmd := &TrackCommand{}
	replies := cmd.Run(ctx, []string{"pikachu", "iv100", "location:Home"})
	if !anyContains(replies, "needs a `d:`") {
		t.Fatalf("expected requires-distance rejection, got %+v", replies)
	}
}

func TestTrack_RejectsAreaWithDistance(t *testing.T) {
	ctx, _ := newTestTrackCtx(t)
	cmd := &TrackCommand{}
	replies := cmd.Run(ctx, []string{"pikachu", "iv100", "d:500", "area:london"})
	if !anyContains(replies, "mutually exclusive") {
		t.Fatalf("expected a+d rejection, got %+v", replies)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/bot/commands -run TestTrack_ -v | head -50`
Expected: FAIL — params not wired into !track yet.

- [ ] **Step 3: Add the params + wire override**

In `processor/internal/bot/commands/track.go`, append to the existing `parameterDefinition` map (or the equivalent prefix-param list — match the file's pattern):

```go
		{Type: bot.ParamPrefixString,     Key: "arg.prefix.location"},
		{Type: bot.ParamPrefixStringList, Key: "arg.prefix.area"},
```

After arg parsing, before building the insert struct, add:

```go
	override, reply := parseOverride(ctx, parsed.Strings, parsed.StringLists["area"], common.Distance)
	if reply != nil {
		return []bot.Reply{*reply}
	}
```

Then on each `MonsterTrackingAPI` literal built for insert:

```go
		OverrideLocationLabel: override.LocationLabel,
		OverrideAreas:         override.Areas,
```

- [ ] **Step 4: Run tests**

Run: `cd processor && go test ./internal/bot/commands -run TestTrack_ -v`
Expected: PASS for the three new tests; existing !track tests still pass.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/bot/commands/track.go processor/internal/bot/commands/track_test.go
git commit -m "bot(!track): accept location: + area: override params with shared validation"
```

---

### Task 15: Replicate override wiring to the other 9 tracking commands

**Files:**
- Modify: `processor/internal/bot/commands/raid.go`
- Modify: `processor/internal/bot/commands/egg.go`
- Modify: `processor/internal/bot/commands/quest.go`
- Modify: `processor/internal/bot/commands/invasion.go`
- Modify: `processor/internal/bot/commands/lure.go`
- Modify: `processor/internal/bot/commands/nest.go`
- Modify: `processor/internal/bot/commands/gym.go`
- Modify: `processor/internal/bot/commands/fort.go`
- Modify: `processor/internal/bot/commands/maxbattle.go`
- Test: append cases to each command's existing `*_test.go`

For each command, apply the same three changes from Task 14:

- [ ] **Step 1: Add prefix params to the per-command param list**

```go
		{Type: bot.ParamPrefixString,     Key: "arg.prefix.location"},
		{Type: bot.ParamPrefixStringList, Key: "arg.prefix.area"},
```

- [ ] **Step 2: Add `parseOverride` call before building insert structs**

```go
	override, reply := parseOverride(ctx, parsed.Strings, parsed.StringLists["area"], common.Distance)
	if reply != nil {
		return []bot.Reply{*reply}
	}
```

- [ ] **Step 3: Populate the override fields on every per-type `*TrackingAPI` insert literal in that command**

```go
		OverrideLocationLabel: override.LocationLabel,
		OverrideAreas:         override.Areas,
```

- [ ] **Step 4: Smoke test — extend each command's test file with one happy-path override case**

E.g. in `raid_test.go`:

```go
func TestRaid_AcceptsAreaOverride(t *testing.T) {
	ctx, _ := newTestRaidCtx(t)
	cmd := &RaidCommand{}
	replies := cmd.Run(ctx, []string{"level:5", "area:london"})
	if anyReact(replies, "🙅") {
		t.Fatalf("rejected: %+v", replies)
	}
	rules, _ := ctx.Tracking.Raid.SelectByIDProfile("u1", 0)
	if len(rules) == 0 || len(rules[0].OverrideAreas) != 1 {
		t.Fatalf("override not stored: %+v", rules)
	}
}
```

(Repeat for each command type, adjusting the rule type + sample args.)

- [ ] **Step 5: Run tests + build**

Run: `cd processor && go test ./internal/bot/commands -count=1 && go build ./...`
Expected: all PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/bot/commands/{raid,egg,quest,invasion,lure,nest,gym,fort,maxbattle}*.go
git commit -m "bot: wire location:/area: override into raid/egg/quest/invasion/lure/nest/gym/fort/maxbattle"
```

---

### Task 16: Show overrides in `!tracked` rowtext

**Files:**
- Modify: each of `processor/internal/rowtext/{monster,raid,egg,quest,invasion,lure,nest,gym,fort,maxbattle}.go`
- Test: extend `processor/internal/rowtext/rowtext_test.go`

- [ ] **Step 1: Write the failing test**

In `rowtext_test.go`:

```go
func TestRowText_MonsterShowsOverrides(t *testing.T) {
	tr := testTranslator(t)
	g := testGameData(t)
	rt := New(g, "1")
	rule := &db.MonsterTracking{
		PokemonID:             25,
		Distance:              500,
		OverrideLocationLabel: "Home",
	}
	got := rt.MonsterRowText(tr, rule)
	if !strings.Contains(got, "@ Home") {
		t.Fatalf("expected '@ Home' in rowtext, got %s", got)
	}

	rule.OverrideLocationLabel = ""
	rule.Distance = 0
	rule.OverrideAreas = []string{"berlin", "munich"}
	got = rt.MonsterRowText(tr, rule)
	if !strings.Contains(got, "in berlin, munich") {
		t.Fatalf("expected 'in berlin, munich' in rowtext, got %s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/rowtext -run TestRowText_MonsterShowsOverrides -v`
Expected: FAIL — rowtext doesn't include overrides.

- [ ] **Step 3: Add a shared helper**

In `processor/internal/rowtext/rowtext.go` (or wherever shared helpers live):

```go
func appendOverride(tr Translator, s, label string, areas []string) string {
	if label != "" {
		s += " | " + tr.Tf("tracking.override_location_fmt", label)
	}
	if len(areas) > 0 {
		s += " | " + tr.Tf("tracking.override_areas_fmt", strings.Join(areas, ", "))
	}
	return s
}
```

In every `*RowText` function across the 10 rowtext files, append:

```go
	s = appendOverride(tr, s, t.OverrideLocationLabel, t.OverrideAreas)
```

just before the existing `return s`.

- [ ] **Step 4: Run tests + build**

Run: `cd processor && go test ./internal/rowtext -count=1 && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/rowtext/
git commit -m "rowtext: show '@ <label>' and 'in <areas>' on rules with overrides"
```

---

## Phase 5 — REST API

### Task 17: GET /api/humans/{id}/locations + GET /api/humans/{id}/locations/{label}

**Files:**
- Create: `processor/internal/api/locations.go`
- Test: `processor/internal/api/locations_test.go`
- Modify: `processor/cmd/processor/main.go` (route wiring)

- [ ] **Step 1: Write the failing tests**

```go
// processor/internal/api/locations_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLocations_List(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	mock.SetLocation("u1", 0, 51.5, -0.1)
	mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})

	r := gin.New()
	g := r.Group("/api/humans")
	g.GET("/:id/locations", HandleListLocations(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/humans/u1/locations", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Locations struct {
			Default *struct{ Latitude, Longitude float64 } `json:"default"`
			Named   []struct{ Label string }               `json:"named"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if resp.Locations.Default == nil || resp.Locations.Default.Latitude != 51.5 {
		t.Fatalf("default missing: %+v", resp.Locations.Default)
	}
	if len(resp.Locations.Named) != 1 || resp.Locations.Named[0].Label != "Home" {
		t.Fatalf("named missing: %+v", resp.Locations.Named)
	}
}

func TestLocations_GetOne_CaseInsensitive(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})

	r := gin.New()
	r.GET("/api/humans/:id/locations/:label", HandleGetLocation(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/humans/u1/locations/home", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Home") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestLocations_GetOne_NotFound(t *testing.T) {
	deps, _ := newTestLocationDeps(t)

	r := gin.New()
	r.GET("/api/humans/:id/locations/:label", HandleGetLocation(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/humans/u1/locations/nope", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/api -run TestLocations_ -v`
Expected: FAIL — handlers undefined.

- [ ] **Step 3: Implement**

```go
// processor/internal/api/locations.go
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/store"
)

type locationRow struct {
	Label     string  `json:"label"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type defaultLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type locationsPayload struct {
	Default *defaultLocation `json:"default,omitempty"`
	Named   []locationRow    `json:"named"`
}

func HandleListLocations(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		locs, err := deps.Humans.ListLocations(id)
		if err != nil {
			log.Errorf("api locations list: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		out := locationsPayload{Named: make([]locationRow, 0, len(locs))}
		for _, l := range locs {
			out.Named = append(out.Named, locationRow{Label: l.Label, Latitude: l.Latitude, Longitude: l.Longitude})
		}
		human, _ := deps.Humans.Get(id)
		if human != nil && (human.Latitude != 0 || human.Longitude != 0) {
			out.Default = &defaultLocation{Latitude: human.Latitude, Longitude: human.Longitude}
		}
		trackingJSONOK(c, map[string]any{"locations": out})
	}
}

func HandleGetLocation(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		label := c.Param("label")
		loc, err := deps.Humans.GetLocation(id, label)
		if err != nil {
			log.Errorf("api locations get: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if loc == nil {
			trackingJSONError(c, http.StatusNotFound, "location not found")
			return
		}
		trackingJSONOK(c, locationRow{Label: loc.Label, Latitude: loc.Latitude, Longitude: loc.Longitude})
	}
}

var _ = store.UserLocation{}
```

- [ ] **Step 4: Wire routes in cmd/processor/main.go**

In `cmd/processor/main.go`, near where other `/api/humans/...` routes are registered, add:

```go
	humans := apiGroup.Group("/humans")
	humans.GET("/:id/locations", api.HandleListLocations(trackingDeps))
	humans.GET("/:id/locations/:label", api.HandleGetLocation(trackingDeps))
```

(If a `humans` route group already exists, append to it instead.)

- [ ] **Step 5: Run tests**

Run: `cd processor && go test ./internal/api -run TestLocations_ -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/api/locations.go processor/internal/api/locations_test.go processor/cmd/processor/main.go
git commit -m "api: GET /api/humans/{id}/locations and /:label"
```

---

### Task 18: POST /api/humans/{id}/locations/add (single + array)

**Files:**
- Modify: `processor/internal/api/locations.go`
- Modify: `processor/internal/api/locations_test.go`
- Modify: `processor/cmd/processor/main.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestLocations_Add_Single(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	r := gin.New()
	r.POST("/api/humans/:id/locations/add", HandleAddLocation(deps))

	body := `{"label":"Home","latitude":51.5,"longitude":-0.1}`
	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/locations/add", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got, _ := mock.GetLocation("u1", "Home")
	if got == nil {
		t.Fatalf("location not persisted")
	}
}

func TestLocations_Add_Array(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	r := gin.New()
	r.POST("/api/humans/:id/locations/add", HandleAddLocation(deps))

	body := `[{"label":"Home","latitude":51.5,"longitude":-0.1},{"label":"Work","latitude":51.6,"longitude":-0.2}]`
	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/locations/add", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	list, _ := mock.ListLocations("u1")
	if len(list) != 2 {
		t.Fatalf("expected 2 locations, got %d", len(list))
	}
}

func TestLocations_Add_DuplicateInResults(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})

	r := gin.New()
	r.POST("/api/humans/:id/locations/add", HandleAddLocation(deps))

	body := `{"label":"Home","latitude":0,"longitude":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/locations/add", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK { // batch endpoint reports per-row results
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "duplicate") {
		t.Fatalf("expected duplicate report in body, got %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/api -run TestLocations_Add -v`
Expected: FAIL — handler undefined.

- [ ] **Step 3: Implement**

```go
type addLocationRequest struct {
	Label     string  `json:"label"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Place     string  `json:"place,omitempty"`
}

type addLocationResult struct {
	Label string `json:"label"`
	Error string `json:"error,omitempty"`
}

func HandleAddLocation(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		rawBody, err := readBody(c)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, err.Error())
			return
		}

		var reqs []addLocationRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &reqs); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single addLocationRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
			reqs = []addLocationRequest{single}
		}

		results := make([]addLocationResult, 0, len(reqs))
		for _, r := range reqs {
			if r.Label == "" {
				results = append(results, addLocationResult{Label: r.Label, Error: "label required"})
				continue
			}
			lat, lon := r.Latitude, r.Longitude
			if r.Place != "" {
				// Server-side geocode via deps.Geocoder; for v1 require lat/lon
				// directly and treat place as a forward-geocode placeholder.
				results = append(results, addLocationResult{Label: r.Label, Error: "place geocoding not yet supported via API; send latitude+longitude"})
				continue
			}
			if _, err := deps.Humans.AddLocation(store.UserLocation{ID: id, Label: r.Label, Latitude: lat, Longitude: lon}); err != nil {
				results = append(results, addLocationResult{Label: r.Label, Error: err.Error()})
				continue
			}
			results = append(results, addLocationResult{Label: r.Label})
		}
		reloadState(deps)
		trackingJSONOK(c, map[string]any{"results": results})
	}
}
```

Place-geocoding is deferred; v1 requires explicit lat/lon. (Note this in the spec's "Out of scope" section in a follow-up commit if needed.)

- [ ] **Step 4: Wire route**

In `cmd/processor/main.go`, add to the humans group:

```go
	humans.POST("/:id/locations/add", api.HandleAddLocation(trackingDeps))
```

- [ ] **Step 5: Run tests**

Run: `cd processor && go test ./internal/api -run TestLocations_Add -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/api/locations.go processor/internal/api/locations_test.go processor/cmd/processor/main.go
git commit -m "api: POST /api/humans/{id}/locations/add (single + array, per-row results)"
```

---

### Task 19: POST /api/humans/{id}/locations/{label}/delete (refuse-when-referenced)

**Files:**
- Modify: `processor/internal/api/locations.go`
- Modify: `processor/internal/api/locations_test.go`
- Modify: `processor/cmd/processor/main.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestLocations_Delete_Success(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})

	r := gin.New()
	r.POST("/api/humans/:id/locations/:label/delete", HandleDeleteLocation(deps))

	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/locations/Home/delete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got, _ := mock.GetLocation("u1", "Home")
	if got != nil {
		t.Fatalf("location should be deleted")
	}
}

func TestLocations_Delete_Conflict(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})
	mock.LocationRefs = map[string][]store.ReferencingRule{
		"u1|home": {{Type: "pokemon", UID: 42}},
	}

	r := gin.New()
	r.POST("/api/humans/:id/locations/:label/delete", HandleDeleteLocation(deps))

	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/locations/Home/delete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "referencing_rules") || !strings.Contains(w.Body.String(), "pokemon") {
		t.Fatalf("response should include referencing rules: %s", w.Body.String())
	}
	got, _ := mock.GetLocation("u1", "Home")
	if got == nil {
		t.Fatalf("location should NOT have been deleted")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/api -run TestLocations_Delete -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
func HandleDeleteLocation(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		label := c.Param("label")

		refs, err := deps.Humans.CountLocationReferences(id, label)
		if err != nil {
			log.Errorf("api locations delete count refs: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if len(refs) > 0 {
			c.JSON(http.StatusConflict, map[string]any{
				"status":             "error",
				"error":              "location is referenced by tracking rules",
				"referencing_rules":  refs,
			})
			return
		}
		if err := deps.Humans.DeleteLocation(id, label); err != nil {
			log.Errorf("api locations delete: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		reloadState(deps)
		trackingJSONOK(c, nil)
	}
}
```

- [ ] **Step 4: Wire route**

```go
	humans.POST("/:id/locations/:label/delete", api.HandleDeleteLocation(trackingDeps))
```

- [ ] **Step 5: Run tests**

Run: `cd processor && go test ./internal/api -run TestLocations_Delete -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/api/locations.go processor/internal/api/locations_test.go processor/cmd/processor/main.go
git commit -m "api: POST /api/humans/{id}/locations/{label}/delete with 409 + referencing_rules"
```

---

### Task 20: Server-side override validation on POST /api/tracking/{type}/{id}

**Files:**
- Modify: `processor/internal/api/tracking.go` (or per-type API handlers)
- Test: `processor/internal/api/tracking_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestTrackingAPI_RejectsLocationWithoutDistance(t *testing.T) {
	deps, mock := newTestTrackingDeps(t)
	mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})

	r := gin.New()
	r.POST("/api/tracking/pokemon/:id", HandleCreateMonster(deps))

	body := `[{"pokemon_id":25,"distance":0,"override_location_label":"Home"}]`
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/u1", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "requires distance") {
		t.Fatalf("expected 400/requires-distance, got %d / %s", w.Code, w.Body.String())
	}
}

func TestTrackingAPI_RejectsUnknownLocation(t *testing.T) {
	deps, _ := newTestTrackingDeps(t)

	r := gin.New()
	r.POST("/api/tracking/pokemon/:id", HandleCreateMonster(deps))

	body := `[{"pokemon_id":25,"distance":500,"override_location_label":"Nope"}]`
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/u1", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unknown location label") {
		t.Fatalf("expected 400/unknown-location, got %d / %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd processor && go test ./internal/api -run TestTrackingAPI_ -v`
Expected: FAIL — validation not yet present.

- [ ] **Step 3: Add shared validation function**

In `processor/internal/api/tracking.go`:

```go
// validateOverrideFields checks the four mutually-exclusive override
// rules from the spec (matching the bot-side parseOverride logic). 
// Returns ("", nil) when valid; returns error msg + http status otherwise.
func validateOverrideFields(
	deps *TrackingDeps,
	humanID string,
	overrideLabel string,
	overrideAreas []string,
	distance int,
) (string, int) {
	hasLocation := overrideLabel != ""
	hasAreas := len(overrideAreas) > 0

	if hasLocation && hasAreas {
		return "override_location_label and override_areas are mutually exclusive", http.StatusBadRequest
	}
	if hasAreas && distance > 0 {
		return "override_areas and distance are mutually exclusive", http.StatusBadRequest
	}
	if hasLocation && distance == 0 {
		return "override_location_label requires distance > 0", http.StatusBadRequest
	}

	if hasLocation {
		loc, err := deps.Humans.GetLocation(humanID, overrideLabel)
		if err != nil {
			return "database error", http.StatusInternalServerError
		}
		if loc == nil {
			return "unknown location label: " + overrideLabel, http.StatusBadRequest
		}
	}

	if hasAreas {
		human, _ := deps.Humans.Get(humanID)
		permitted := deps.AreaLogic.PermittedAreaSet(human)
		for _, a := range overrideAreas {
			if !permitted[strings.ToLower(a)] {
				return "area not permitted: " + a, http.StatusBadRequest
			}
		}
	}

	return "", 0
}
```

- [ ] **Step 4: Call it in every per-type Create handler**

In each per-type handler (`HandleCreateMonster`, `HandleCreateRaid`, etc. in `trackingMonster.go` through `trackingMaxBattle.go`), after parsing the request rows and before applying defaults, loop over each row and:

```go
		if msg, code := validateOverrideFields(deps, human.ID, row.OverrideLocationLabel, row.OverrideAreas, row.Distance.intValue(0)); msg != "" {
			trackingJSONError(c, code, msg)
			return
		}
```

- [ ] **Step 5: Run tests + build**

Run: `cd processor && go test ./internal/api -count=1 && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/api/
git commit -m "api(tracking): server-side override validation mirroring bot rules"
```

---

## Phase 6 — Slash commands

### Task 21: /location subcommands

**Files:**
- Modify: `processor/internal/discordbot/slash/definitions.go`
- Modify: `processor/internal/discordbot/slash/dispatcher.go`
- Create: `processor/internal/discordbot/slash/mappers/location.go`
- Create: `processor/internal/discordbot/slash/autocomplete/listers/locations.go`
- Test: `processor/internal/discordbot/slash/mappers/location_test.go`

- [ ] **Step 1: Add /location to slash definitions**

In `definitions.go`, add a `/location` command with subcommands `add`, `list`, `show`, `remove`, `set-default`, `remove-default`:

```go
	{
		Name:        "location",
		Description: tr.T("slash.desc.location"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type: discordgo.ApplicationCommandOptionSubCommand,
				Name: "add", Description: tr.T("slash.opt.location.add.desc"),
				Options: []*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: tr.T("slash.opt.location.add.name.desc"), Required: true},
					{Type: discordgo.ApplicationCommandOptionString, Name: "place", Description: tr.T("slash.opt.location.add.place.desc"), Required: true},
				},
			},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "list", Description: tr.T("slash.opt.location.list.desc")},
			{
				Type: discordgo.ApplicationCommandOptionSubCommand,
				Name: "show", Description: tr.T("slash.opt.location.show.desc"),
				Options: []*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: tr.T("slash.opt.location.show.name.desc"), Required: true, Autocomplete: true},
				},
			},
			{
				Type: discordgo.ApplicationCommandOptionSubCommand,
				Name: "remove", Description: tr.T("slash.opt.location.remove.desc"),
				Options: []*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: tr.T("slash.opt.location.remove.name.desc"), Required: true, Autocomplete: true},
				},
			},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "set-default", Description: tr.T("slash.opt.location.setdefault.desc")},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "remove-default", Description: tr.T("slash.opt.location.removedefault.desc")},
		},
	},
```

Add the corresponding i18n keys in `en.json`.

- [ ] **Step 2: Wire dispatcher**

In `dispatcher.go`, route `/location` to the new mapper which translates the slash invocation into the same text-command args used by `!location`.

```go
	case "location":
		args := mappers.MapLocation(ic, d)
		return d.runTextCommand(ctx, "cmd.location", args)
```

- [ ] **Step 3: Implement mapper**

```go
// processor/internal/discordbot/slash/mappers/location.go
package mappers

import "github.com/bwmarrin/discordgo"

// MapLocation translates a /location slash invocation into the equivalent
// !location text-command args.
func MapLocation(ic *discordgo.InteractionCreate, _ any) []string {
	opts := ic.ApplicationCommandData().Options
	if len(opts) == 0 {
		return nil
	}
	sub := opts[0]
	switch sub.Name {
	case "add":
		var name, place string
		for _, o := range sub.Options {
			switch o.Name {
			case "name":
				name = o.StringValue()
			case "place":
				place = o.StringValue()
			}
		}
		return []string{"add", name, place}
	case "list":
		return []string{"list"}
	case "show":
		return []string{"show", sub.Options[0].StringValue()}
	case "remove":
		return []string{"remove", sub.Options[0].StringValue()}
	case "set-default":
		// presence-only — actual coords come from the existing user-location
		// /location flow; treat set-default as an alias for the bare
		// "!location" usage hint (or no-op + reply with usage)
		return nil
	case "remove-default":
		return []string{"remove", "default"}
	}
	return nil
}
```

- [ ] **Step 4: Add autocomplete lister for user locations**

```go
// processor/internal/discordbot/slash/autocomplete/listers/locations.go
package listers

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// ListUserLocations returns the user's saved-location labels matching
// the partial input. Used by /location show, /location remove, and the
// `location` option on /track / /raid / ...
func ListUserLocations(humans store.HumanStore, humanID, partial string) []string {
	locs, _ := humans.ListLocations(humanID)
	low := strings.ToLower(partial)
	out := make([]string, 0, len(locs))
	for _, l := range locs {
		if low == "" || strings.Contains(strings.ToLower(l.Label), low) {
			out = append(out, l.Label)
		}
	}
	return out
}
```

Wire the autocomplete dispatch for `/location` `name` options to this lister.

- [ ] **Step 5: Test the mapper**

```go
// processor/internal/discordbot/slash/mappers/location_test.go
package mappers

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestMapLocation_Add(t *testing.T) {
	ic := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "location",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{{
					Name: "add",
					Options: []*discordgo.ApplicationCommandInteractionDataOption{
						{Name: "name", Value: "Home"},
						{Name: "place", Value: "51.5,-0.1"},
					},
				}},
			},
		},
	}
	got := MapLocation(ic, nil)
	want := []string{"add", "Home", "51.5,-0.1"}
	if !slicesEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
```

- [ ] **Step 6: Run tests + build**

Run: `cd processor && go test ./internal/discordbot/slash/... -count=1 && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/discordbot/slash/ processor/internal/i18n/locale/en.json
git commit -m "slash: /location command with add/list/show/remove/{set,remove}-default + autocomplete"
```

---

### Task 22: location + areas options on /track and the other 9 tracker slash commands

**Files:**
- Modify: `processor/internal/discordbot/slash/definitions.go` (10 commands)
- Modify: `processor/internal/discordbot/slash/mappers/{monster,raid,egg,quest,invasion,lure,nest,gym,fort,maxbattle}.go`
- Modify: `processor/internal/discordbot/slash/autocomplete/dispatcher.go`
- Test: extend `processor/internal/discordbot/slash/mappers/*_test.go`

- [ ] **Step 1: Add two new options on each tracker slash command**

For each `/track`, `/raid`, `/egg`, etc. in `definitions.go`, append:

```go
		{Type: discordgo.ApplicationCommandOptionString, Name: "location", Description: tr.T("slash.opt.tracker.location.desc"), Autocomplete: true},
		{Type: discordgo.ApplicationCommandOptionString, Name: "areas",    Description: tr.T("slash.opt.tracker.areas.desc"),    Autocomplete: true},
```

- [ ] **Step 2: Map these options into text-command args in each tracker's mapper**

In each `mappers/<type>.go`, when building args:

```go
	if v := getOptionString(opts, "location"); v != "" {
		args = append(args, "location:"+v)
	}
	if v := getOptionString(opts, "areas"); v != "" {
		// Already comma-separated; the !track-side parser accepts both
		// `area:X area:Y` and `area:X,Y` so we pass through as-is.
		args = append(args, "area:"+v)
	}
```

- [ ] **Step 3: Autocomplete dispatch**

In autocomplete `dispatcher.go`, when the focused option is named `location`, call `ListUserLocations(deps.Humans, userID, partial)`; when named `areas`, call the existing area lister + filter to permitted areas.

- [ ] **Step 4: One test per tracker mapper**

E.g. in `mappers/monster_test.go`, add:

```go
func TestMapMonster_PassesOverrideOptions(t *testing.T) {
	ic := buildMonsterIC("pikachu", map[string]string{"location": "Home", "d": "500"})
	got := MapMonster(ic, nil)
	if !contains(got, "location:Home") || !contains(got, "d:500") {
		t.Fatalf("override options not mapped: %v", got)
	}
}
```

- [ ] **Step 5: Run tests + build**

Run: `cd processor && go test ./internal/discordbot/slash/... -count=1 && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/discordbot/slash/ processor/internal/i18n/locale/en.json
git commit -m "slash: location + areas options on all 10 tracker commands"
```

---

## Phase 7 — Reconciliation + smoke

### Task 23: Prune override_areas during area-permission reconciliation

**Files:**
- Modify: `processor/internal/bot/area_logic.go`
- Test: extend `processor/internal/bot/area_logic_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestValidateAndPrune_StripsDisallowedFromOverrides(t *testing.T) {
	dbx := openTestDB(t)
	defer dbx.Close()
	mustExec(t, dbx, `INSERT INTO monsters (id, profile_no, pokemon_id, override_areas) VALUES
		('u1', 0, 25, '["berlin","munich"]'),
		('u1', 0, 26, '["berlin"]')`)

	al := NewAreaLogic(...)
	permitted := map[string]bool{"munich": true} // berlin removed
	if err := al.PruneOverrideAreas(dbx, "u1", permitted); err != nil {
		t.Fatalf("PruneOverrideAreas: %v", err)
	}

	var rows []struct {
		PokemonID     int    `db:"pokemon_id"`
		OverrideAreas string `db:"override_areas"`
	}
	mustQuery(t, dbx, &rows, `SELECT pokemon_id, COALESCE(override_areas, '') AS override_areas FROM monsters WHERE id='u1' ORDER BY pokemon_id`)
	if rows[0].OverrideAreas != `["munich"]` {
		t.Fatalf("expected pruned to munich only, got %q", rows[0].OverrideAreas)
	}
	if rows[1].OverrideAreas != "" {
		t.Fatalf("expected NULL after empty pruning, got %q", rows[1].OverrideAreas)
	}
}
```

- [ ] **Step 2: Implement**

```go
// PruneOverrideAreas walks every tracking row for the given human and
// drops areas no longer in `permitted` from override_areas. If the
// override list becomes empty, the column is NULLed so the rule falls
// back to the human's areas.
func (a *AreaLogic) PruneOverrideAreas(dbx *sqlx.DB, humanID string, permitted map[string]bool) error {
	for _, table := range []string{"monsters", "raid", "egg", "quest", "invasion", "lures", "nests", "gym", "forts", "maxbattle"} {
		type row struct {
			UID           int64  `db:"uid"`
			OverrideAreas string `db:"override_areas"`
		}
		var rows []row
		q := fmt.Sprintf(`SELECT uid, COALESCE(override_areas, '') AS override_areas FROM %s WHERE id = ? AND override_areas IS NOT NULL AND override_areas != ''`, table)
		if err := dbx.Select(&rows, q, humanID); err != nil {
			return err
		}
		for _, r := range rows {
			var areas []string
			if err := json.Unmarshal([]byte(r.OverrideAreas), &areas); err != nil {
				continue
			}
			kept := areas[:0]
			for _, a := range areas {
				if permitted[strings.ToLower(a)] {
					kept = append(kept, a)
				}
			}
			if len(kept) == len(areas) {
				continue // no change
			}
			var newVal any
			if len(kept) == 0 {
				newVal = nil
			} else {
				b, _ := json.Marshal(kept)
				newVal = string(b)
			}
			updQ := fmt.Sprintf(`UPDATE %s SET override_areas = ? WHERE uid = ?`, table)
			if _, err := dbx.Exec(updQ, newVal, r.UID); err != nil {
				return err
			}
		}
	}
	return nil
}
```

Then call `PruneOverrideAreas` from the existing `ValidateAndPrune` path right after the human's `Area` column is updated.

- [ ] **Step 3: Run tests + build**

Run: `cd processor && go test ./internal/bot -run TestValidateAndPrune_Strips -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 4: Commit**

```bash
git add processor/internal/bot/area_logic.go processor/internal/bot/area_logic_test.go
git commit -m "reconciliation: prune disallowed areas from per-rule override_areas"
```

---

### Task 24: Smoke checklist doc

**Files:**
- Create: `docs/superpowers/specs/2026-05-25-per-rule-location-area-overrides-smoke.md`

- [ ] **Step 1: Write the smoke checklist**

```markdown
# Per-rule location & area overrides — Smoke checklist

Run through these in a real Discord/Telegram + processor + MySQL setup
to validate the feature end-to-end before merging.

## !location surface
- [ ] `!location add Home canterbury` → reply confirms save with coords
- [ ] `!location add "Holiday Home" santa monica` → quoted multi-word name works
- [ ] `!location add Home 0,0` → rejected as duplicate
- [ ] `!location list` → shows default + named, sorted by label
- [ ] `!location show home` → case-insensitive lookup; shows coords + map link
- [ ] `!location show Nope` → "No saved location" error
- [ ] `!location remove default` → clears the default location
- [ ] `!location remove` (bare) → usage error
- [ ] `!location remove Home` → succeeds when no rules reference it

## Tracking rules with override
- [ ] `!track pikachu iv100 d:500 location:Home` → rule stored with override
- [ ] `!tracked` shows the rule with "@ Home" appended
- [ ] `!track pikachu iv100 location:Home` → rejected (no d:)
- [ ] `!track pikachu iv100 d:500 area:london` → rejected (a+d mutually exclusive)
- [ ] `!track pikachu iv100 area:london area:berlin` → rule stored with both areas
- [ ] `!tracked` shows "in london, berlin"
- [ ] `!track pikachu iv100 area:berlin location:Home` → rejected (mutually exclusive)
- [ ] `!track pikachu iv100 area:NotPermitted` → rejected ("not in your allowed areas")
- [ ] `!track pikachu iv100 d:500 location:Nope` → rejected ("No saved location")
- [ ] Same checks for `!raid`, `!egg`, `!quest`, `!invasion`, `!lure`, `!nest`, `!gym`, `!fort`, `!maxbattle`

## Refuse-when-referenced
- [ ] After creating an override rule for "Home", `!location remove Home` is rejected with the rule listed

## Webhook matching
- [ ] Trigger a webhook within 500m of "Home" coords — alert fires (rule with `location:Home d:500`)
- [ ] Trigger a webhook outside that radius — alert does not fire even if within human default location
- [ ] Trigger a webhook in the "berlin" area — alert with `area:berlin` fires even if human's areas don't include berlin
- [ ] Webhook delivered message shows distance from override location, not human default

## REST API
- [ ] `GET /api/humans/u1/locations` → envelope `{locations: {default, named}}`
- [ ] `GET /api/humans/u1/locations/home` → case-insensitive 200
- [ ] `GET /api/humans/u1/locations/nope` → 404
- [ ] `POST /api/humans/u1/locations/add` with single body → 200 + persisted
- [ ] `POST /api/humans/u1/locations/add` with array body → 200 + multi-row results
- [ ] `POST /api/humans/u1/locations/add` with duplicate label → row reports `"duplicate"`
- [ ] `POST /api/humans/u1/locations/Home/delete` while referenced → 409 + `referencing_rules`
- [ ] `POST /api/tracking/pokemon/u1` body with `override_location_label` + `distance: 0` → 400
- [ ] Same body with valid override → 200 + persisted row carries override
- [ ] Same body referencing unknown label → 400

## Slash commands
- [ ] `/location add` works end-to-end (mirrors !location add)
- [ ] `/location show` autocomplete suggests user's saved labels
- [ ] `/track` with `location` + `d:500` works; autocomplete on `location` suggests labels
- [ ] `/track` with `areas: berlin,munich` parses correctly
- [ ] Mutually-exclusive checks fire server-side on slash too

## Reconciliation (area-security mode only)
- [ ] User loses access to area X. Run reconciliation. Rules with `override_areas: ["X"]` are NULLed; rules with `["X", "Y"]` become `["Y"]`
- [ ] User's tracking rules continue to function with their remaining permitted areas

## Cascade on user delete
- [ ] Delete a user via the existing delete-human routine. `user_locations` rows for that user are removed
- [ ] No orphaned `user_locations` rows remain

## Profile interaction
- [ ] Switching profile does NOT clear per-rule overrides
- [ ] Per-rule overrides apply consistently across all of the user's profiles
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/specs/2026-05-25-per-rule-location-area-overrides-smoke.md
git commit -m "docs(smoke): manual verification checklist for per-rule overrides"
```

---

## Self-review summary

**Spec coverage:**
- ✅ `user_locations` table + tracking columns — Task 1
- ✅ Matcher integration in `ValidateHumansGeneric` — Task 7
- ✅ `!location` surface with all 5 subcommand cases — Tasks 9, 10, 11
- ✅ Refuse-when-referenced — Task 11 (bot), Task 19 (API)
- ✅ Per-tracking-rule params with all 4 mutually-exclusive validations — Tasks 13, 14, 15, 20
- ✅ All 10 tracking types — Tasks 5, 15, 16, 20, 22
- ✅ REST API matching project verb-in-path style — Tasks 17, 18, 19
- ✅ Slash commands with autocomplete — Tasks 21, 22
- ✅ Reconciliation pruning — Task 23
- ✅ Rowtext display — Task 16
- ✅ i18n — Tasks 8, 11, 21, 22
- ✅ Smoke checklist — Task 24

**Placeholder scan:** No TBD / TODO / "implement later" in any step.

**Type consistency:** `OverrideLocationLabel` (string) and `OverrideAreas` ([]string) used uniformly across DB structs, API DTOs, matcher input, command parsing, and reconciliation.

**Spec deferral noted:** Server-side place-geocoding in `POST /api/humans/{id}/locations/add` requires `latitude+longitude`; `place` is accepted but returns a per-row error. Update the spec's "Out of scope" section with this v1 limitation.
