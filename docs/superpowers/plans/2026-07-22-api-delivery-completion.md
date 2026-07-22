# API Delivery — Completion Plan (full pack + envelope fields)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the api-delivery feature so a partner (Diadem) reviews a complete, accurate contract: the full 15-type canonical payload pack, the remaining envelope fields, and docs re-synced to what the code actually emits.

**Architecture:** Builds on the shipped api-delivery core. Adds three envelope fields sourced from data already computed at render time (`tracking_uids`, `areas` from the snapshot-builder inputs; `in_reply_to` from the queue-stamped `ReplyToID`), replaces the 2-type starter `fallbacks/dts/api.toml` with all 15 alert types, adds a conformance test that pins every entry's canonical keys, and re-syncs the receiver spec + design doc.

**Tech Stack:** Go, BurntSushi/toml, `jfberry/raymond` Handlebars.

## Global Constraints

- Pre-commit gate (from `processor/`): `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`
- No new dependencies.
- **Decisions locked in brainstorming:**
  - `revision` stays **reserved (always 0)** — the "apply every edit" idempotency model is already correct; do NOT implement monotonic revision.
  - The static map URL lives in the **payload** (`static_map` in the common block), NOT an envelope `media` field. There is no envelope `media` field; remove it from the spec.
  - Envelope gains `tracking_uids`, `areas`, `in_reply_to` (all `omitempty`, never `null`).
- **Partner-named packs from the start.** The reference partner pack is `fallbacks/dts/diadem.toml`, entries `platform = "api"`, `id = "diadem"`, selected via `[api_delivery] template = "diadem"` — which becomes the shipped config default (changed from `"default"`). A second partner ships `fallbacks/dts/<partner>.toml` with `id = "<partner>"` and the operator sets `template = "<partner>"`; DTS selection already keys on `id`, so no other mechanism is needed. Packs are readonly fallbacks; operators copy one into `config/dts/` to customise. This replaces the 2-type starter `fallbacks/dts/api.toml`/`id="default"` shipped by the core plan (delete it).
- Numeric interpolations MUST be guarded so a sparse webhook yields valid JSON. For the unencountered `iv = -1` sentinel, use `{{#if (gte iv 0)}}{{iv}}{{else}}null{{/if}}` (the `-1` is truthy under a bare `{{#if iv}}`).
- Canonical payload schema is design doc `docs/superpowers/specs/2026-07-19-api-delivery-destination-design.md` §1.7 (common block §1.7.2, pokemon §1.7.3, all types §1.7.4). That table is the authoritative field list the pack transcribes.

## File Structure

- Modify: `processor/internal/delivery/delivery.go` — add `Job.TrackingUIDs`, `Job.Areas`.
- Modify: `processor/internal/delivery/api.go` — emit `tracking_uids`/`areas`/`in_reply_to`.
- Modify: `processor/cmd/processor/render.go` — populate the two new Job fields.
- Modify: `processor/internal/delivery/api_test.go` — envelope-field tests.
- Rename+rewrite: `fallbacks/dts/api.toml` → `fallbacks/dts/diadem.toml` — all 15 alert-type entries, `id="diadem"`.
- Modify: `processor/internal/config/config.go` — `[api_delivery] template` default `"default"` → `"diadem"`.
- Modify: `processor/internal/config/config_test.go` — `TestAPIDeliveryDefaults` expects `"diadem"`.
- Modify: `processor/internal/dts/api_render_test.go` — `TestAPIStarterPackSparsePokemonValidJSON` loads `diadem.toml`, template `"diadem"`.
- Modify: `config/config.example.toml` — `template` comment shows `"diadem"`.
- Create: `processor/internal/dts/api_pack_conformance_test.go` — per-type render + canonical-key conformance.
- Modify: `docs/api-delivery-receiver-spec.md`, `docs/superpowers/specs/2026-07-19-api-delivery-destination-design.md` — re-sync.
- Modify: `processor/internal/delivery/dispatcher.go` — remove dead `APIConcurrency` field.

---

### Task 1: Envelope fields — `tracking_uids`, `areas`, `in_reply_to`

**Files:**
- Modify: `processor/internal/delivery/delivery.go` (Job fields)
- Modify: `processor/internal/delivery/api.go` (`apiEnvelope` + `buildSendEnvelope`)
- Modify: `processor/cmd/processor/render.go` (populate)
- Test: `processor/internal/delivery/api_test.go`

**Interfaces:**
- Produces: `Job.TrackingUIDs []int64`, `Job.Areas []string`; envelope keys `tracking_uids`, `areas`, `in_reply_to`.

- [ ] **Step 1: Write the failing test**

Add to `processor/internal/delivery/api_test.go`:

```go
func TestAPISendEnvelopeExtraFields(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := newAPISenderForTest(srv.URL)
	job := &Job{
		Target: "u-42", Type: "api:user", Message: json.RawMessage(`{}`),
		TrackingUIDs: []int64{45, 46},
		Areas:        []string{"london", "city"},
		ReplyToID:    "u-42:7c9e6a1f-0000-4000-8000-000000000000:abc123", // prior message SentID
	}
	if _, err := s.Send(context.Background(), job); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	uids, _ := body["tracking_uids"].([]any)
	if len(uids) != 2 || uids[0].(float64) != 45 {
		t.Errorf("tracking_uids = %v, want [45 46]", body["tracking_uids"])
	}
	areas, _ := body["areas"].([]any)
	if len(areas) != 2 || areas[0].(string) != "london" {
		t.Errorf("areas = %v, want [london city]", body["areas"])
	}
	// in_reply_to = the provider id half of the prior SentID
	if body["in_reply_to"] != "abc123" {
		t.Errorf("in_reply_to = %v, want abc123", body["in_reply_to"])
	}
}

func TestAPISendInReplyToFallsBackToMessageID(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := newAPISenderForTest(srv.URL)
	// prior message had no provider id (2-part SentID) → in_reply_to is the message id
	job := &Job{Target: "u-1", Type: "api:user", Message: json.RawMessage(`{}`),
		ReplyToID: "u-1:7c9e6a1f-0000-4000-8000-000000000000"}
	if _, err := s.Send(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if body["in_reply_to"] != "7c9e6a1f-0000-4000-8000-000000000000" {
		t.Errorf("in_reply_to = %v, want the message id", body["in_reply_to"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/delivery/ -run 'TestAPISendEnvelopeExtraFields|TestAPISendInReplyToFallsBackToMessageID' -v`
Expected: FAIL — `Job` has no `TrackingUIDs`/`Areas`; envelope omits the keys.

- [ ] **Step 3: Add the Job fields**

In `processor/internal/delivery/delivery.go`, add to the `Job` struct (near `Name`/`TemplateID`):

```go
	TrackingUIDs []int64  `json:"trackingUids,omitempty"` // matched tracking-rule UIDs (api envelope tracking_uids)
	Areas        []string `json:"areas,omitempty"`        // matched geofence area names (api envelope areas)
```

- [ ] **Step 4: Emit the fields in the envelope**

In `processor/internal/delivery/api.go`, add to `apiEnvelope` (after `TemplateID`):

```go
	TrackingUIDs      []int64         `json:"tracking_uids,omitempty"`
	Areas             []string        `json:"areas,omitempty"`
	InReplyTo         string          `json:"in_reply_to,omitempty"`
```

In `buildSendEnvelope`, after setting `Payload`:

```go
	env.TrackingUIDs = job.TrackingUIDs
	env.Areas = job.Areas
	if job.ReplyToID != "" {
		_, mid, pid := splitAPISentID(job.ReplyToID)
		if pid != "" {
			env.InReplyTo = pid
		} else {
			env.InReplyTo = mid
		}
	}
```

- [ ] **Step 5: Populate the Job fields in render.go**

In `processor/cmd/processor/render.go`, in the `delivery.Job{...}` literal inside `processRenderJob`'s delivery loop, add:

```go
			TrackingUIDs:  collectTrackingUIDs(job.MatchedUsers, j.Target),
			Areas:         areaNames(job.MatchedAreas),
```

`collectTrackingUIDs` already exists (used by `buildSnapshot`). Add the small `areaNames` helper near it in `render.go`:

```go
// areaNames extracts the geofence area names from matched areas (for the
// api envelope's areas field). Mirrors the inline slice buildSnapshot builds.
func areaNames(areas []webhook.MatchedArea) []string {
	if len(areas) == 0 {
		return nil
	}
	out := make([]string, 0, len(areas))
	for _, a := range areas {
		out = append(out, a.Name)
	}
	return out
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd processor && go test ./internal/delivery/ -run 'TestAPISend' -v`
Expected: PASS (all api send tests, including the two new ones).

- [ ] **Step 7: Full gate + commit**

```bash
cd processor && go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...
git add internal/delivery/delivery.go internal/delivery/api.go internal/delivery/api_test.go cmd/processor/render.go
git commit -m "feat(delivery): emit tracking_uids/areas/in_reply_to in api envelope"
```

---

### Task 2: Full 15-type partner pack (`diadem.toml`)

Replace the 2-type starter `fallbacks/dts/api.toml` with a partner-named pack `fallbacks/dts/diadem.toml` (`id="diadem"`) covering all 15 alert types, transcribing design doc §1.7. Each entry inlines the common block (§1.7.2) and adds the type-specific fields (§1.7.3/§1.7.4). No partials (self-contained pack). Every numeric interpolation guarded.

**Files:**
- Delete: `fallbacks/dts/api.toml`; Create: `fallbacks/dts/diadem.toml`
- Modify: `processor/internal/config/config.go`, `processor/internal/config/config_test.go`, `processor/internal/dts/api_render_test.go`, `config/config.example.toml`

**Interfaces:**
- Produces: `platform="api"`, `id="diadem"` entries for: `pokemon`, `monsterChanged`, `raid`, `egg`, `rsvpChanges`, `quest`, `questSummary`, `invasion`, `incident`, `lure`, `nest`, `gym`, `fort`, `maxbattle`, `weatherchange`. Consumed/validated by Task 3.

- [ ] **Step 0a: Change the shipped default template to `"diadem"`**

In `processor/internal/config/config.go`, `applyAPIDeliveryDefaults`, change the template default:

```go
	if cfg.APIDelivery.Template == "" {
		cfg.APIDelivery.Template = "diadem"
	}
```

In `processor/internal/config/config_test.go`, update `TestAPIDeliveryDefaults`'s assertion:

```go
	if cfg.APIDelivery.Template != "diadem" {
		t.Errorf("Template default = %q, want diadem", cfg.APIDelivery.Template)
	}
```

In `config/config.example.toml`, change the commented template line under `[api_delivery]` to show the partner id:

```toml
# template      = "diadem"                         # partner pack id (fallbacks/dts/<partner>.toml)
```

- [ ] **Step 0b: Rename the shipped starter and repoint its test**

`git rm fallbacks/dts/api.toml` (the core plan's 2-type starter). The new `fallbacks/dts/diadem.toml` is created below.

In `processor/internal/dts/api_render_test.go`, `TestAPIStarterPackSparsePokemonValidJSON` loads the shipped file and renders with template `"default"`. Repoint it: load `fallbacks/dts/diadem.toml` (same `fallbacks` fallbackDir; the walker finds `diadem.toml`) and render the pokemon entry with `Template: "diadem"` via `ts.Get("pokemon", "api", "diadem", "en")` / a user with `Template: "diadem"`. Keep the assertion (valid JSON with fields absent). Rename the test to `TestDiademPackSparsePokemonValidJSON` if you like; either way it must load the real `diadem.toml`.

- [ ] **Step 1: Write the common-block-bearing pokemon and raid entries**

All entries use `id = "diadem"`. Use these two as the pattern (`id="diadem"`):

Use exactly these two entries as the pattern. The common block (address / static_map / icon_url / map_urls / time_remaining / sun) is inlined; `static_map` in the payload triggers tile generation via `UsesTile`.

```toml
# Diadem partner pack (id="diadem"). Self-contained: the common block is
# repeated inline in every entry (no partials). Numeric interpolations are
# guarded so a sparse webhook yields valid JSON.
# Payload schema: docs/superpowers/specs/2026-07-19-api-delivery-destination-design.md §1.7.

[[entry]]
type = "pokemon"
platform = "api"
id = "diadem"
template = """
{
  "pokemon_id": {{#if pokemonId}}{{pokemonId}}{{else}}0{{/if}},
  "form_id": {{#if formId}}{{formId}}{{else}}0{{/if}},
  "name": "{{name}}",
  "full_name": "{{fullName}}",
  "name_en": "{{nameEng}}",
  "form_name": "{{formName}}",
  "costume_name": "{{costumeName}}",
  "gender": "{{genderName}}",
  "generation": {{#if generation}}{{generation}}{{else}}0{{/if}},
  "rarity": "{{rarityName}}",
  "size": "{{sizeName}}",
  "shiny_possible": {{#if shinyPossible}}true{{else}}false{{/if}},
  "encountered": {{#if encountered}}true{{else}}false{{/if}},
  "iv": {{#if (gte iv 0)}}{{iv}}{{else}}null{{/if}},
  "atk": {{#if (gte atk 0)}}{{atk}}{{else}}null{{/if}},
  "def": {{#if (gte def 0)}}{{def}}{{else}}null{{/if}},
  "sta": {{#if (gte sta 0)}}{{sta}}{{else}}null{{/if}},
  "cp": {{#if cp}}{{cp}}{{else}}null{{/if}},
  "level": {{#if level}}{{level}}{{else}}null{{/if}},
  "types": [{{#each typeNameEng}}"{{this}}"{{#unless @last}},{{/unless}}{{/each}}],
  "color": "{{color}}",
  "quick_move": "{{quickMoveName}}",
  "charge_move": "{{chargeMoveName}}",
  "despawn_at": {{#if despawnTimestamp}}{{despawnTimestamp}}{{else}}0{{/if}},
  "despawn_display": "{{time}}",
  "despawn_verified": {{#if confirmedTime}}true{{else}}false{{/if}},
  "distance_m": {{#if distance}}{{distance}}{{else}}0{{/if}},
  "bearing_deg": {{#if bearing}}{{bearing}}{{else}}0{{/if}},
  "pokestop_name": "{{pokestopName}}",
  "static_map": "{{staticMap}}",
  "icon_url": "{{imgUrl}}",
  "map_urls": { "google": "{{googleMapUrl}}", "apple": "{{appleMapUrl}}" },
  "address": {
    "formatted": "{{addr}}", "street_number": "{{streetNumber}}", "street_name": "{{streetName}}",
    "neighbourhood": "{{neighbourhood}}", "suburb": "{{suburb}}", "city": "{{city}}",
    "state": "{{state}}", "postcode": "{{zipcode}}", "country": "{{country}}",
    "country_code": "{{countryCode}}", "intersection": "{{intersection}}"
  },
  "time_remaining": {
    "days": {{#if tthd}}{{tthd}}{{else}}0{{/if}}, "hours": {{#if tthh}}{{tthh}}{{else}}0{{/if}},
    "minutes": {{#if tthm}}{{tthm}}{{else}}0{{/if}}, "seconds": {{#if tths}}{{tths}}{{else}}0{{/if}}
  },
  "sun": {
    "sunrise_display": "{{sunriseTime}}", "sunset_display": "{{sunsetTime}}",
    "is_night": {{#if isNight}}true{{else}}false{{/if}},
    "is_dawn": {{#if isDawn}}true{{else}}false{{/if}},
    "is_dusk": {{#if isDusk}}true{{else}}false{{/if}}
  }
}
"""

[[entry]]
type = "raid"
platform = "api"
id = "diadem"
template = """
{
  "level": {{#if level}}{{level}}{{else}}0{{/if}},
  "level_name": "{{levelName}}",
  "boss": {
    "pokemon_id": {{#if pokemonId}}{{pokemonId}}{{else}}0{{/if}},
    "name": "{{name}}", "full_name": "{{fullName}}", "name_en": "{{nameEng}}",
    "form_name": "{{formName}}", "costume_name": "{{costumeName}}",
    "types": [{{#each typeNameEng}}"{{this}}"{{#unless @last}},{{/unless}}{{/each}}],
    "color": "{{color}}",
    "cp20": {{#if cp20}}{{cp20}}{{else}}0{{/if}}, "cp25": {{#if cp25}}{{cp25}}{{else}}0{{/if}},
    "quick_move": "{{quickMoveName}}", "charge_move": "{{chargeMoveName}}",
    "shiny_possible": {{#if shinyPossible}}true{{else}}false{{/if}}
  },
  "gym": {
    "name": "{{gymName}}", "image_url": "{{gymUrl}}", "team": "{{teamName}}",
    "ex": {{#if ex}}true{{else}}false{{/if}}
  },
  "hatch_at": {{#if hatchTimestamp}}{{hatchTimestamp}}{{else}}0{{/if}},
  "end_at": {{#if endTimestamp}}{{endTimestamp}}{{else}}0{{/if}},
  "hatch_display": "{{hatchTime}}", "end_display": "{{time}}",
  "static_map": "{{staticMap}}", "icon_url": "{{imgUrl}}",
  "map_urls": { "google": "{{googleMapUrl}}", "apple": "{{appleMapUrl}}" },
  "address": {
    "formatted": "{{addr}}", "city": "{{city}}", "country": "{{country}}",
    "country_code": "{{countryCode}}"
  },
  "time_remaining": {
    "hours": {{#if tthh}}{{tthh}}{{else}}0{{/if}}, "minutes": {{#if tthm}}{{tthm}}{{else}}0{{/if}},
    "seconds": {{#if tths}}{{tths}}{{else}}0{{/if}}
  }
}
"""
```

`monsterChanged` = the `pokemon` entry with an added `"previous": { "pokemon_id": {{#if original.pokemonId}}{{original.pokemonId}}{{else}}0{{/if}}, "full_name": "{{original.fullName}}", "iv": {{#if (gte original.iv 0)}}{{original.iv}}{{else}}null{{/if}}, "cp": {{#if original.cp}}{{original.cp}}{{else}}0{{/if}}, "level": {{#if original.level}}{{original.level}}{{else}}0{{/if}} }` object. `rsvpChanges` = the `raid` entry (same fields; the rsvp timeslots ride the same view). `egg` = the raid entry minus the `boss` object, keyed on `level`/`level_name`/`gym`/hatch/end.

- [ ] **Step 2: Add the remaining entries per §1.7.4**

Add one `[[entry]]` (`platform="api"`, `id="diadem"`) for each remaining type, transcribing its payload-key ← registry-field row from design doc **§1.7.4** and applying the guard rules (string fields `"{{x}}"`, bool `{{#if x}}true{{else}}false{{/if}}`, numeric `{{#if x}}{{x}}{{else}}0{{/if}}`, arrays via the `{{#each}}…{{#unless @last}},{{/unless}}` pattern). Include the common block (address/static_map/icon_url/map_urls/time_remaining/sun) where the type has a location:

- `egg`, `monsterChanged`, `rsvpChanges` — per Step 1 notes.
- `quest` — `pokestop_name`, `quest`←`questString`, `reward`←`rewardString`, `conditions`←`conditionString`, `condition_list`←`conditionList`, `reward_detail.{dust_amount,item_name,item_amount,monster_name,energy_amount,candy_amount,xl_candy_amount,shiny}`.
- `questSummary` — `reward_type`, `reward_id`←`reward`, `reward_form`, `reward_name`, `count`, `chunk`, `chunks`, `entries[]`←`quests` (each `{pokestop_name, latitude, longitude}` + the quest reward keys), `static_map`.
- `invasion` — `pokestop_name`, `grunt_name`, `grunt_type`, `grunt_type_name`, `grunt_type_id`, `display_type_id`, `gender`←`genderName`, `color`←`gruntTypeColor`, `expires_display`←`time`.
- `incident` — `pokestop_name`, `pokestop_id`, `pokestop_image_url`←`pokestopUrl`, `display_type`, `incident_type_name`, `color`, `expires_display`←`disappearTime`; plus showcase fields when present (`showcase.{present,total_entries,focus_category,focus_name,entries[]}`).
- `lure` — `pokestop_name`, `lure_type_id`, `lure_type_name`, `color`←`lureTypeColor`, `expires_display`←`time`.
- `nest` — `nest_name`, `pokemon_id`, `name`, `full_name`, `name_en`, `form_name`, `types`←`typeNameEng`, `color`, `spawn_avg`←`pokemonSpawnAvg`, `pokemon_count`, `shiny_possible`.
- `gym` — `gym_name`, `image_url`←`gymUrl`, `team`←`teamName`, `old_team`←`oldTeamName`, `slots_available`, `old_slots_available`, `in_battle`←`inBattle`, `ex`, `color`←`gymColor`.
- `fort` — `fort_id`←`id`, `fort_type`, `name`, `change_type`, `change_type_text`, `edit_types`←`editTypesList`, `is_edit_name`, `is_edit_location`, `is_edit_image`, `is_empty`, `new_name`, `old_name`, `new_description`, `old_description`, `new_image_url`, `old_image_url`.
- `maxbattle` — `pokemon_id`, `name`, `full_name`, `costume_name`, `level`, `gmax`, `pokestop_name`, `types`←`typeNameEng`, `color`, `quick_move`, `charge_move`, `end_at`←`endTimestamp`, `end_display`←`time`, `total_stationed`, `total_stationed_gmax`.
- `weatherchange` — `weather`←`weatherName`, `old_weather`←`oldWeatherName`, `change_in.{hours,minutes,seconds}`←`weatherTthh/m/s`, `affected_pokemon`←`enrichedActivePokemons`.

- [ ] **Step 3: Sanity-render each entry + run the config/renderer tests**

Eyeball each entry for balanced braces and trailing commas. Then run the tests touched by the rename + default change:

Run: `cd processor && go test -count=1 ./internal/config/ ./internal/dts/ -run 'TestAPIDelivery|TestFallbackTomlPackLoads|TestDiademPackSparsePokemonValidJSON|TestAPIStarterPackSparsePokemonValidJSON'`
Expected: PASS (config default is now `"diadem"`; the sparse-pokemon test loads `diadem.toml`).

- [ ] **Step 4: Commit**

```bash
cd processor && go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...
cd /Users/james/GolandProjects/PoracleNG/.claude/worktrees/api-delivery
git add fallbacks/dts/api.toml fallbacks/dts/diadem.toml processor/internal/config/config.go processor/internal/config/config_test.go processor/internal/dts/api_render_test.go config/config.example.toml
git commit -m "feat(dts): full 15-type diadem partner pack; default template=diadem"
```

---

### Task 3: Pack conformance test

Pin every pack entry: it must render to valid JSON, and emit the canonical required keys for its type, for both a fully-populated and a sparse (all-optional-absent) enrichment map.

**Files:**
- Create: `processor/internal/dts/api_pack_conformance_test.go`

- [ ] **Step 1: Write the conformance test**

For each type, a representative enrichment map and the set of required top-level payload keys. The test loads the real `fallbacks/dts/diadem.toml`, renders each type through `RenderAlert` (or `RenderPokemon` for pokemon) with a `Template: "diadem"` user, and asserts (a) valid JSON and (b) every required key present.

```go
package dts

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// apiPackConformanceCase is one alert type's render fixture + required keys.
type apiPackConformanceCase struct {
	alertType    string
	templateType string // DTS type (usually == alertType; "monster" for pokemon)
	enrichment   map[string]any
	requiredKeys []string
}

func renderAPIType(t *testing.T, r *Renderer, c apiPackConformanceCase, user webhook.MatchedUser) json.RawMessage {
	t.Helper()
	var jobs []webhook.DeliveryJob
	if c.templateType == "monster" {
		jobs = r.RenderPokemon(c.enrichment, nil, nil, nil, []webhook.MatchedUser{user}, nil, true, "ref", "")
	} else {
		jobs = r.RenderAlert(c.templateType, c.enrichment, nil, nil, []webhook.MatchedUser{user}, nil, "ref", "")
	}
	if len(jobs) != 1 {
		t.Fatalf("%s: want 1 job, got %d", c.alertType, len(jobs))
	}
	return jobs[0].Message
}

func newRealPackRenderer(t *testing.T) *Renderer {
	t.Helper()
	// fallbackDir points at the repo-root fallbacks/ so the real diadem.toml loads.
	fallbackDir, err := filepath.Abs(filepath.Join("..", "..", "..", "fallbacks"))
	if err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	writeTestDTS(t, configDir, []DTSEntry{}) // no user entries; fallbacks win
	r, err := NewRenderer(RendererConfig{ConfigDir: configDir, FallbackDir: fallbackDir, DefaultLocale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAPIPackConformance(t *testing.T) {
	r := newRealPackRenderer(t)
	user := webhook.MatchedUser{ID: "u-1", Type: "api:user", Template: "diadem", Language: "en"}

	cases := []apiPackConformanceCase{
		{
			alertType: "pokemon", templateType: "monster",
			enrichment: map[string]any{"pokemonId": 25, "name": "Pikachu", "iv": 100, "cp": 800, "level": 20,
				"typeNameEng": []string{"Electric"}, "latitude": 1.0, "longitude": 2.0,
				"tth": map[string]any{"totalSeconds": 600}},
			requiredKeys: []string{"pokemon_id", "name", "iv", "cp", "level", "types", "despawn_at", "static_map", "map_urls", "address", "time_remaining", "sun"},
		},
		{
			alertType: "raid", templateType: "raid",
			enrichment: map[string]any{"level": 5, "name": "Mewtwo", "gymName": "Gym A",
				"typeNameEng": []string{"Psychic"}, "latitude": 1.0, "longitude": 2.0,
				"tth": map[string]any{"totalSeconds": 600}},
			requiredKeys: []string{"level", "boss", "gym", "end_at", "static_map"},
		},
		// … one case per remaining type (egg, quest, invasion, incident, lure,
		// nest, gym, fort, maxbattle, weatherchange, questSummary, rsvpChanges,
		// monsterChanged). Each: a minimal enrichment map with the fields the
		// template reads, and the type's required top-level keys from §1.7.4.
	}

	for _, c := range cases {
		t.Run(c.alertType, func(t *testing.T) {
			// Populated render → valid JSON + required keys.
			msg := renderAPIType(t, r, c, user)
			var m map[string]any
			if err := json.Unmarshal(msg, &m); err != nil {
				t.Fatalf("%s: invalid JSON: %v\n%s", c.alertType, err, string(msg))
			}
			for _, k := range c.requiredKeys {
				if _, ok := m[k]; !ok {
					t.Errorf("%s: missing required key %q", c.alertType, k)
				}
			}
			// Sparse render → still valid JSON (guards hold with everything absent).
			sparse := map[string]any{"latitude": 1.0, "longitude": 2.0, "tth": map[string]any{"totalSeconds": 60}}
			sparseMsg := renderAPIType(t, r, apiPackConformanceCase{alertType: c.alertType, templateType: c.templateType, enrichment: sparse}, user)
			if !json.Valid(sparseMsg) {
				t.Errorf("%s: sparse render produced invalid JSON:\n%s", c.alertType, string(sparseMsg))
			}
		})
	}
}
```

- [ ] **Step 2: Fill in a case for every remaining type**

Add the 13 remaining cases (egg, monsterChanged, rsvpChanges, quest, questSummary, invasion, incident, lure, nest, gym, fort, maxbattle, weatherchange), each with a minimal populated enrichment map and the required top-level keys from §1.7.4. Where the alert type's DTS template type differs (e.g. incident/showcase), set `templateType` accordingly.

- [ ] **Step 3: Run — fix any pack entry the test flags**

Run: `cd processor && go test ./internal/dts/ -run TestAPIPackConformance -v`
Expected: every subtest PASS. If a subtest fails with invalid JSON or a missing key, the fault is in the Task-2 pack entry — fix the entry (this is the test doing its job). Do not weaken the required-key list to make a broken entry pass.

- [ ] **Step 4: Commit**

```bash
cd processor && go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...
git add internal/dts/api_pack_conformance_test.go
# plus any fallbacks/dts/diadem.toml fixes the test forced
git add ../fallbacks/dts/diadem.toml
git commit -m "test(dts): diadem pack conformance — valid JSON + canonical keys per type"
```

---

### Task 4: Re-sync the specs to what the code emits

**Files:**
- Modify: `docs/api-delivery-receiver-spec.md`
- Modify: `docs/superpowers/specs/2026-07-19-api-delivery-destination-design.md`

- [ ] **Step 1: Move `media.static_map` out of the envelope**

In BOTH docs, the envelope tables (`docs/api-delivery-receiver-spec.md` §3.1, design doc §1.3) currently list a `media.static_map` row and the send example shows `"media": {...}`. Remove the `media` row and the `"media"` line from the send-example JSON. In the receiver spec §8 (payload) and design doc §1.7.2 (common block), state that the static map URL is a payload common-block field `static_map` (present when the template requests a tile), alongside `icon_url` and `map_urls`.

- [ ] **Step 2: Remove the "not emitted in this version" caveats for the three envelope fields**

In `docs/api-delivery-receiver-spec.md`, the version note (added after the §3.1 table) currently says `in_reply_to`, `tracking_uids`, `areas`, and `media` are not emitted. Update it: these three (`in_reply_to`, `tracking_uids`, `areas`) ARE now emitted; drop `media` (moved to payload). Keep only `revision` described as reserved. Adjust the design doc's Part 2 "deferred" list (§2.9 / wherever it enumerates deferrals) to remove these three and `media` — leaving `revision` monotonicity as the sole envelope-level deferral.

- [ ] **Step 3: Confirm the payload schema reference matches the shipped partner pack**

In receiver spec §8 and design doc §1.7.5, ensure the wording matches the shipped reality: the reference partner pack is `fallbacks/dts/diadem.toml`, entries `id="diadem"`, selected via `[api_delivery] template="diadem"` (the shipped default), all 15 types, validated by the conformance test (Task 3). §1.7.5 already describes this diadem/`id="diadem"`/`template="diadem"` model — confirm it still reads correctly and that it presents per-partner packs (`<partner>.toml` / `id="<partner>"`) as the standard multi-partner pattern, not a future aside. Do NOT rename anything to `id="default"`.

- [ ] **Step 4: Commit**

```bash
git add docs/api-delivery-receiver-spec.md docs/superpowers/specs/2026-07-19-api-delivery-destination-design.md
git commit -m "docs: re-sync api spec — static_map in payload, tracking_uids/areas/in_reply_to emitted"
```

---

### Task 5: Remove the dead `DispatcherConfig.APIConcurrency` field

**Files:**
- Modify: `processor/internal/delivery/dispatcher.go`

- [ ] **Step 1: Delete the unused field**

`DispatcherConfig.APIConcurrency` is never read (concurrency flows through `QueueConfig.ConcurrentAPI`). Remove the field declaration. Grep first to confirm no reader exists:

Run: `cd processor && grep -rn "APIConcurrency" .`
Expected: only the struct field declaration (and no assignment in `main.go`'s `DispatcherConfig{...}` literal — confirm). Delete the field line.

- [ ] **Step 2: Build + commit**

```bash
cd processor && go build ./... && go vet ./... && golangci-lint run ./internal/delivery/
git add internal/delivery/dispatcher.go
git commit -m "chore(delivery): drop unused DispatcherConfig.APIConcurrency field"
```

---

## Full-suite verification

From `processor/`:

```bash
go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...
```

Manual smoke (optional): `[api_delivery] enabled=true`, `log_only=true`; create an `api:user`; add tracking rules across several types; replay webhooks or `!poracle-test`; confirm each logged envelope carries the right `alert_type`, a valid `payload`, and — where applicable — `tracking_uids`/`areas`/`in_reply_to`.

## Self-Review notes

- **Coverage:** envelope fields (§1.3) → Task 1; full pack (§1.7) → Task 2; conformance (§2.8) → Task 3; doc accuracy → Task 4; the one logged cleanup → Task 5.
- **Locked decisions honored:** revision stays reserved (no code); static_map in payload not envelope (Task 2 pack + Task 4 spec); no envelope `media` field.
- **Type consistency:** `Job.TrackingUIDs []int64` matches `collectTrackingUIDs`'s return; `apiEnvelope.TrackingUIDs`/`Areas`/`InReplyTo` are the JSON `tracking_uids`/`areas`/`in_reply_to`; `in_reply_to` derives from `ReplyToID` via the existing `splitAPISentID`.
- **Deferred beyond this plan (intentional):** monotonic `revision`; per-partner pack ids; the auth-drop metrics label; api raid-RSVP compact-template partition-path awareness. None affect the contract Diadem reviews.
