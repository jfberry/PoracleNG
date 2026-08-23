# Derived DTS Test Data Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every DTS template type — including the derived ones (`monsterChanged`, `incident`, `questSummary`, `weatherchange`, `rsvpChanges`) — previewable via the editor's `/api/dts/enrich` and testable via live `!poracle-test`, by unifying the two enrichment paths, adding structured test "partials", and addressing test data by DTS type name.

**Architecture:** One shared per-type dispatch (`enrichForType`) that both `/api/dts/enrich` (flatten → variables) and `/api/test` (wrap → RenderJob → deliver) consume; derived types carry a structured payload in the existing `webhook` field and reuse production render construction; a canonical DTS↔source alias table drives enrich, testdata, and `!poracle-test`.

**Tech Stack:** Go, huma, the existing `enricher.*` methods, `dts.NewLayeredView`, `RenderJob`.

Design spec: `docs/superpowers/specs/2026-07-18-derived-dts-test-data-design.md`.

## Global Constraints

- **One enrichment implementation.** After Task 1, the per-type "webhook/partial → `enrichResult`" logic lives ONLY in `enrich.go`'s `enrich*` functions. `test.go` and `EnrichWebhook` both call them. Do not leave a parallel copy in `test.go`.
- **`enrichResult` is the shared unit:** `{ templateType, base, perLang, perUser, webhookFields, tilePending }` (+ new `extras map[string]any` for derived state: `original`, `affected`, quest group, rsvp). perUser is caller-supplied context (editor = synthetic `_editor`; live = real target) — the shared `enrich*` returns base/perLang; each surface adds perUser.
- **Derived types reuse production render construction** — `dts.BuildOriginalView` (monsterChanged), `DispatchQuestSummary` grouping (questSummary), the `weatherchange`/`rsvpChanges` `RenderJob` shapes (`weather.go:223`, `raid.go:266`). Do not re-derive rendering.
- **Canonical alias table is the single source of truth** for DTS↔source resolution, used by `EnrichWebhook`, `/api/dts/testdata`, and `!poracle-test`.
- **Back-compat:** existing `/api/dts/enrich` by webhook type, `/api/dts/testdata?type=<webhookType>`, and every current `!poracle-test <webhookType>,<id>` keep working unchanged.
- **Pre-commit gate (from `processor/`):** `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`.

---

### Task 1: Unify enrichment — `test.go` calls the shared `enrich*` core

**Files:**
- Modify: `processor/cmd/processor/enrich.go` (add `extras` to `enrichResult`; expose a shared builder)
- Modify: `processor/cmd/processor/test.go` (replace per-type enrichment with calls to `enrich*`)
- Test: `processor/cmd/processor/enrich_test.go` (assert parity), existing `test.go` tests must stay green

**Interfaces:**
- Produces: `enrichResult.extras map[string]any`; a helper `func (ps *ProcessorService) renderJobFromEnrich(r *enrichResult, target webhook.MatchedUser, alertType string, raw json.RawMessage) RenderJob` used by test.go.

- [ ] **Step 1: Add `extras` to `enrichResult`** (`enrich.go`):
```go
type enrichResult struct {
	templateType  string
	base          map[string]any
	perLang       map[string]any
	perUser       map[string]any
	webhookFields map[string]any
	tilePending   *staticmap.TilePending
	extras        map[string]any // derived-type state: original, affected, rsvp, group. Nil for plain types.
}
```

- [ ] **Step 2: Move the synthetic-user PVP perUser out of `enrichPokemon`** so `enrich*` returns only base/perLang/templateType/tilePending. In `EnrichWebhook`, after obtaining the result for pokemon, compute the synthetic `_editor` perUser (the block currently at `enrich.go:119-132`) and set `result.perUser`. `enrichPokemon` no longer sets perUser.

- [ ] **Step 3: Add the RenderJob wrapper** (`test.go`):
```go
// renderJobFromEnrich wraps a shared enrichResult into a delivery RenderJob for
// a single test target. perUser is computed here with the REAL target user
// (unlike the editor's synthetic user).
func (ps *ProcessorService) renderJobFromEnrich(r *enrichResult, target webhook.MatchedUser, alertType string, raw json.RawMessage, isPokemon, isEncountered bool) RenderJob {
	matched := []webhook.MatchedUser{target}
	perLang := map[string]map[string]any{}
	if r.perLang != nil {
		perLang[target.Language] = r.perLang
	}
	var perUser map[string]map[string]any
	if isPokemon && ps.enricher.PVPDisplay != nil && r.perLang != nil {
		perUser = ps.enricher.PokemonPerUser(perLang, matched)
	}
	return RenderJob{
		AlertType:         alertType,
		TemplateType:      r.templateType,
		IsPokemon:         isPokemon,
		IsEncountered:     isEncountered,
		Enrichment:        r.base,
		PerLangEnrichment: perLang,
		PerUserEnrichment: perUser,
		WebhookFields:     r.webhookFields,
		MatchedUsers:      matched,
		MatchedAreas:      []webhook.MatchedArea{},
		TileGate:          ps.newTileGate(r.tilePending),
		LogReference:      "test",
	}
}
```

- [ ] **Step 4: Rewrite each `test.go` per-type handler** to call the matching `enrich*` and `renderJobFromEnrich`. Example (`processTestPokemon`):
```go
func (ps *ProcessorService) processTestPokemon(raw json.RawMessage, target webhook.MatchedUser) error {
	r, err := ps.enrichPokemon(raw, target.Language)
	if err != nil {
		return err
	}
	if ps.renderCh == nil {
		return fmt.Errorf("render queue not available")
	}
	isEnc := false
	if v, ok := r.extras["encountered"].(bool); ok { isEnc = v }
	ps.renderCh <- ps.renderJobFromEnrich(r, target, "pokemon", raw, true, isEnc)
	return nil
}
```
(Add `extras["encountered"] = processed.Encountered` in `enrichPokemon`.) Do the same for raid/egg/quest/invasion/lure/nest/gym/fort/maxbattle handlers. The showcase handler (test.go:~215) keeps its `TemplateType:"showcase"` but sources enrichment from `enrichInvasion`-style logic; leave showcase behaviour identical.

- [ ] **Step 5: Parity test** (`enrich_test.go`): for each webhook type with a bundled sample, assert `enrich<Type>` produces the same `templateType` and non-empty `base` that the old path did (golden-ish: assert key fields like `name`/`fullName` present). Run existing `test.go` command tests — all must stay green (no behaviour change).

- [ ] **Step 6: Gate + commit**
```bash
go build ./... && go vet ./... && go test -count=1 ./cmd/... && golangci-lint run ./cmd/...
git commit -am "refactor(test): unify poracle-test enrichment onto the shared enrich* core"
```

---

### Task 2: Canonical DTS↔source alias table + DTS-name resolution in `EnrichWebhook`

**Files:**
- Create: `processor/cmd/processor/dts_alias.go`
- Modify: `processor/cmd/processor/enrich.go` (`EnrichWebhook` resolves DTS names)
- Test: `processor/cmd/processor/dts_alias_test.go`

**Interfaces:**
- Produces: `type dtsSource struct { WebhookType string; TemplateType string; Derived bool }`; `func dtsAlias(name string) (dtsSource, bool)`; `func dtsTypeMap() map[string]dtsSource` (for the API to expose).

- [ ] **Step 1: Write the failing test** — `dtsAlias("monster")` → `{WebhookType:"pokemon", TemplateType:"monster"}`; `dtsAlias("egg")` → `{WebhookType:"raid", TemplateType:"egg"}`; `dtsAlias("monsterChanged")` → `{WebhookType:"monster-changed", Derived:true}`; `dtsAlias("pokemon")` (a webhook type) resolves too; unknown → `false`.

- [ ] **Step 2: Implement the table** (`dts_alias.go`) covering: monster→pokemon, monsterNoIv→pokemon, monsterChanged→monster-changed(derived), raid→raid, egg→raid, rsvpChanges→rsvp-changes(derived), quest→quest, questSummary→quest-summary(derived), invasion→pokestop/invasion, incident→incident(derived-ish: moved samples), showcase→showcase, lure→pokestop/lure, weatherchange→weather-change(derived), gym→gym, nest→nest, maxbattle→max_battle. Include identity entries for raw webhook types so both `monster` and `pokemon` resolve.

- [ ] **Step 3: Resolve in `EnrichWebhook`** — before the switch, `if src, ok := dtsAlias(webhookType); ok { webhookType = src.WebhookType or route derived }`. Keep the existing switch for non-derived; add derived cases in Tasks 4–7.

- [ ] **Step 4: Gate + commit.**

---

### Task 3: incident — move samples + render the incident template

**Files:**
- Modify: `fallbacks/testdata.json` (retype the pokestop-incident samples to `incident`)
- Modify: `processor/cmd/processor/enrich.go` + `test.go` (an `incident` path rendering `TemplateType:"incident"`)
- Test: `processor/cmd/processor/*_test.go`

- [ ] **Step 1:** Add an `enrichIncident(raw, lang)` that reuses the invasion enrichment (it's a PokestopEvent) but sets `templateType:"incident"`. Wire `case "incident"` into `EnrichWebhook` and a `processTestIncident` into test.go (via `renderJobFromEnrich`, `alertType:"incident"`).
- [ ] **Step 2:** In `fallbacks/testdata.json`, move the incident-flavoured pokestop samples (`kecleon`, `goldstop`, `pokemoncontest`, …) to `type:"incident"` entries (keep the webhook payloads). Leave true invasions and lures as pokestop.
- [ ] **Step 3:** Test: `EnrichWebhook("incident", <kecleon webhook>, "en", "discord")` returns `templateType:"incident"` and incident-only fields (`incidentTypeName`); `!poracle-test incident,kecleon` enqueues a RenderJob with `TemplateType:"incident"`. Gate + commit.

---

### Task 4: weatherchange partial + builder + testdata

**Files:** `enrich.go` (`enrichWeatherChange`), `test.go` (`processTestWeatherChange`), `fallbacks/testdata.json`, tests.

**Reuse:** the `weatherchange` `RenderJob` construction at `cmd/processor/weather.go:223` and its enrichment (the `consumeWeatherChanges` path builds `[]webhook.ActivePokemonEntry`).

- [ ] **Step 1:** Define the partial shape and add a `weather-change` sample to `fallbacks/testdata.json`: the cell/old→new weather fields + `affected: [ {pokemon_id, form, ...}, ... ]` (a short list, per the requirement).
- [ ] **Step 2:** `enrichWeatherChange(raw, lang)` parses the partial, runs the same weather enrichment `consumeWeatherChanges` uses to produce the base/perLang + the affected-pokemon list into `extras["affected"]`; `templateType:"weatherchange"`. Read `weather.go:38-230` and reuse its enrichment calls rather than re-deriving.
- [ ] **Step 3:** `processTestWeatherChange` wraps via `renderJobFromEnrich` with `alertType:"weather"`, `TemplateType:"weatherchange"`. Wire `case "weather-change"`/`"weatherchange"` into `EnrichWebhook` + test.go + the alias table.
- [ ] **Step 4:** Test: enrich returns `weatherchange` template + the affected list in variables; live path enqueues it. Gate + commit.

---

### Task 5: questSummary partial + builder + testdata

**Files:** `enrich.go`, `test.go`, `fallbacks/testdata.json`, tests.

**Reuse:** `cmd/processor/quest_summary_dispatch.go:27 DispatchQuestSummary(humanID, alertType)` and its grouping/render (`TemplateType:"questSummary"`).

- [ ] **Step 1:** Add a `quest-summary` sample: `{ reward:{type,amount,...}, quests:[<quest webhook>, ...] }`.
- [ ] **Step 2:** Factor the group→RenderJob construction out of `DispatchQuestSummary` into a reusable builder (e.g. `buildQuestSummaryRenderJob(group, target) RenderJob`) if it's currently inline, so both the scheduler and the test path call it. `enrichQuestSummary`/`processTestQuestSummary` synthesise a group from the partial and call it.
- [ ] **Step 3:** Test: `!poracle-test quest-summary,stardust` renders one `questSummary` job grouping the sample quests; enrich returns the group variables. Gate + commit.

---

### Task 6: monsterChanged partial + builder + testdata

**Files:** `enrich.go`, `test.go`, `fallbacks/testdata.json`, tests.

**Reuse:** `internal/dts/original_view.go:16 BuildOriginalView(prior tracker.EncounterState, gd, tr) map[string]any` and the change RenderJob shape at `cmd/processor/pokemon.go:392` (`IsChange:true`, `OriginalView`, `ChangeType`).

- [ ] **Step 1:** Add a `monster-changed` sample: `{ old:<pokemon webhook>, new:<pokemon webhook> }` (e.g. species/form/encountered shift).
- [ ] **Step 2:** `enrichMonsterChanged`: enrich `new` via `enrichPokemon`; build `OriginalView` from `old` (map the old webhook to a `tracker.EncounterState`, then `dts.BuildOriginalView`); set `extras["original"]`, `templateType:"monsterChanged"`.
- [ ] **Step 3:** `processTestMonsterChanged`: `renderJobFromEnrich(..., isPokemon=true)` then set `job.IsChange=true`, `job.OriginalView=extras["original"]`, `job.ChangeType="test"`, `job.TemplateType="monsterChanged"`, `job.ReplyKey=<encounterID>`. Wire the alias + cases.
- [ ] **Step 4:** Test: enrich returns `monsterChanged` template with `original.*` fields; live path enqueues an `IsChange` job with a populated `OriginalView`. Gate + commit.

---

### Task 7: rsvpChanges partial + builder + testdata

**Files:** `enrich.go`, `test.go`, `fallbacks/testdata.json`, tests.

**Reuse:** the `rsvpChanges` RenderJob at `cmd/processor/raid.go:266` (`TemplateType:"rsvpChanges"`, `OverrideCleanTTH`).

- [ ] **Step 1:** Add an `rsvp-changes` sample: `{ raid:<raid webhook>, rsvps:[{timeslot, going, maybe}, ...] }`.
- [ ] **Step 2:** `enrichRsvpChanges`: enrich the raid via `enrichRaid`; attach the rsvp fields; `templateType:"rsvpChanges"`; `extras["overrideCleanTTH"]` = latest timeslot.
- [ ] **Step 3:** `processTestRsvpChanges`: wrap via `renderJobFromEnrich(alertType:"raid")`, set `job.TemplateType="rsvpChanges"`, `job.OverrideCleanTTH=extras[...]`, `job.EditKey`/`ReplyKey` per the raid convention (`raidlife:{gymID}:{raidEnd}`). Wire alias + cases.
- [ ] **Step 4:** Test + gate + commit.

---

### Task 8: `GET /api/dts/testdata?dtsType=` — server-side filter + tags + map endpoint

**Files:**
- Modify: `processor/internal/api/dts_testdata.go`, `processor/internal/api/huma_dts_reads.go`
- Test: `processor/internal/api/*_test.go`

- [ ] **Step 1:** Extend the testdata read to accept `?dtsType=<name>`: resolve via the alias table (exposed from the processor to the API — pass `dtsTypeMap()` in, or replicate the table in `internal/api`), return only the entries that preview that DTS type. Do the pokestop→invasion/lure split server-side (the logic the editor currently does in `capture-test-data.mjs`: invasion = has `grunt_type`/`character`/`display_type`; lure = has `lure_id`/`lure_expiration`). Tag each returned entry with `dtsType`.
- [ ] **Step 2:** Add a discoverable map to the response (or a sibling `GET /api/dts/testdata/types`) returning the full DTS-type→source table so the editor drops its hardcoded copy.
- [ ] **Step 3:** Keep `?type=<webhookType>` working unchanged (back-compat). Update the OpenAPI golden (`UPDATE_GOLDEN=1 go test ./internal/api/ -run TestOpenAPIGolden`).
- [ ] **Step 4:** Test: `?dtsType=invasion` returns only invasion scenarios; `?dtsType=incident` returns the moved samples; `?dtsType=monsterChanged` returns the partial; tags present; `?type=pokestop` unchanged. Gate + commit.

---

### Task 9: `!poracle-test` accepts DTS type names

**Files:** the `!poracle-test` bot command (`internal/bot/commands/*` — the poracle-test handler) + `POST /api/test` handler.
- [ ] Resolve the leading `type` token via the alias table before dispatch, so `!poracle-test monsterChanged,species-shift` and `!poracle-test weatherchange,clear-to-rain` work alongside the existing webhook-type forms. Test both forms. Gate + commit.

---

### Task 10: Editor handoff doc

**Files:** Create `docs/superpowers/handoffs/2026-07-18-dts-editor-derived-types.md`.
- [ ] Write the complete server contract for the editor agent (see the spec's "Editor handoff doc — required contents"): DTS-name-addressable `/api/dts/enrich`; `GET /api/dts/testdata?dtsType=` + tags + the map endpoint; the derived-type entries + partial shapes; and the exact editor **delete list** (`dtsToWebhookType` map, pokestop invasion/lure filter, `monsterNoIv`/`egg` special-casing in `capture-test-data.mjs`; address by DTS type in `api-client.js`/`TestDataPanel.jsx`). Note `fort-update`/`maxbattle` + derived types are now selectable. Commit.

---

### Final: full gate
- [ ] From `processor/`: `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...` — all green, `0 issues`.
