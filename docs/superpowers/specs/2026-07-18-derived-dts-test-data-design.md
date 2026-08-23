# Testing Derived DTS Types (+ unified enrichment + DTS-name addressing) — Design

Status: draft / for review
Date: 2026-07-18
Editor under review: `~/dev/poracle-embed-visualizer` (the DTS template editor).

## Summary

Make every DTS template type previewable and testable — including the **derived**
types that aren't a single raw webhook (`monsterChanged`, `incident`,
`questSummary`, `weatherchange`, `rsvpChanges`) — through **both** consumer
surfaces:
1. the DTS editor's two-stage flow (`GET /api/dts/testdata` → user edits →
   `POST /api/dts/enrich` → enriched variables → preview), and
2. live `!poracle-test` / `POST /api/test` (render + deliver).

Along the way: **unify** the two duplicated per-type enrichment implementations
into one shared dispatch, and make test data **addressable by DTS template
type name** (server-provided mapping) so the editor stops hardcoding
`monster→pokemon` and stops filtering scenarios client-side.

## Background

### Two duplicated enrichment paths (today)
- `cmd/processor/enrich.go` `EnrichWebhook(type, raw, lang, platform) → variables`
  — a per-type switch (pokemon/raid/egg/quest/invasion/lure/nest/gym/
  fort_update/max_battle) calling `enricher.Pokemon/Raid/Quest/…`. Powers the
  editor's `POST /api/dts/enrich`.
- `cmd/processor/test.go` — its **own** parallel per-type handlers, also calling
  `enricher.Pokemon/Raid/Invasion/…`, building `RenderJob`s. Powers live
  `POST /api/test` (`!poracle-test`).

The two must be kept in lock-step by hand; a derived type would otherwise have
to be implemented twice.

### Test data shape
`fallbacks/testdata.json` (+ `config/testdata.json` override) is
`[]TestDataEntry{ type, test, location, webhook }` where `webhook` is
`json.RawMessage` (any JSON — already flexible enough for a "partial").

### Derived DTS types have no test path (verified)
`fieldsByType` registers `monsterChanged`, `incident`, `questSummary`,
`weatherchange`, `rsvpChanges` as real editable types, but nothing renders them:
the reachable `TemplateType`s are only monster/raid/egg/invasion/showcase/quest/
gym/nest/fort-update/maxbattle/lure. Each derived type needs extra state:
`monsterChanged` = old+new sighting (`OriginalView`); `weatherchange` = a
weather-change event + affected-pokemon list; `questSummary` = a grouped digest;
`rsvpChanges` = a raid + RSVP update; `incident` = pokestop-event samples routed
to the incident template.

### Editor's current coupling (`poracle-embed-visualizer`)
`scripts/capture-test-data.mjs` hardcodes `dtsToWebhookType`
(`monster→pokemon`, `monsterNoIv→pokemon`, `egg→raid`, `invasion/lure→pokestop`,
`fort-update→fort_update`, `maxbattle→max_battle`, …); it **filters** pokestop
scenarios client-side (sniffing `grunt_type`/`lure_id`) to split invasion vs
lure; and it special-cases `monsterNoIv→pokemon`/`egg→raid` when calling enrich.
`src/lib/api-client.js` exposes `enrichWebhook(type, webhook, lang)`,
`getTestData(type)`, `getFields(type)`. All of this is server-derivable.

## Decisions

| # | Decision |
|---|----------|
| D1 | **Unify enrichment.** One shared dispatch `enrichForType(name, partial, lang, platform) → EnrichedResult{ Layers, TemplateType, Extras }` (Extras carries `OriginalView`, the quest group, the affected list, the RSVP context). `EnrichWebhook` becomes a thin wrapper returning the flattened `variables`. `test.go`'s live path calls the **same** dispatch and wraps the result in a `RenderJob`. A derived type is added exactly once. |
| D2 | **Derived types as test "partials".** The `webhook` field holds a structured payload per derived type: `monster-changed: {old, new}`; `weather-change: {…cell old/new weather…, affected:[pokemon…]}`; `quest-summary: {reward, quests:[quest…]}`; `rsvp-changes: {raid, rsvps:[…]}`. `incident`: **move** the existing kecleon/gold-stop/etc. pokestop-incident samples into `incident`-typed entries that render the `incident` template (no runtime routing). |
| D3 | **Canonical DTS-name addressing.** One server-side alias table maps every DTS template type ↔ its test source, resolving `monster↔pokemon`, `monsterNoIv←pokemon`, `egg←raid`, plus the derived names. The shared dispatch accepts a DTS type name OR a webhook type name via this table. Single source of truth, used by the enrich endpoint, testdata endpoint, and `!poracle-test`. |
| D4 | **`GET /api/dts/testdata` becomes DTS-type-aware.** It accepts a DTS type (e.g. `?dtsType=invasion`) and returns exactly the entries that preview that type — the **server** does the pokestop invasion/lure split and any filtering, and tags each returned entry with the DTS type(s) it can preview. The editor drops its hardcoded map + client-side filtering. (The legacy `?type=<webhookType>` query stays for back-compat.) |
| D5 | **`POST /api/dts/enrich` accepts DTS type names for all types**, including the derived ones — it runs the unified dispatch, so it returns the enriched variables (incl. `original.*`, affected list, group) the editor needs to preview a derived template. |
| D6 | **`!poracle-test` / `POST /api/test` accept DTS type names too** (`!poracle-test monsterChanged,species-shift`), resolved via the same alias table; the derived partials render+deliver live. |
| D7 | **Editor handoff doc is a deliverable.** A standalone instruction doc for the editor agent describing every new/changed server contract (the DTS-name-addressable enrich, the DTS-type-aware testdata endpoint + its tags, the derived-type entries, and exactly which editor code to delete: the hardcoded map, the filtering, the special-casing). |

## Components (PoracleNG)

### 1. Alias / mapping table (`cmd/processor/enrich.go` or a small new file)
A single canonical structure: for each DTS type — its render `TemplateType`, its
test-source category, and whether it's a "derived" (partial) type. Drives D3/D4/
D5/D6. Exposed to the editor (see §3). Rough shape:
`monster→pokemon(encountered)`, `monsterNoIv→pokemon(unencountered)`,
`monsterChanged→monster-changed(partial)`, `raid→raid`, `egg→raid(egg)`,
`rsvpChanges→rsvp-changes(partial)`, `quest→quest`,
`questSummary→quest-summary(partial)`, `invasion→pokestop(invasion)`,
`incident→incident(moved samples)`, `showcase→showcase`, `lure→pokestop(lure)`,
`weatherchange→weather-change(partial)`, `gym→gym`, `nest→nest`,
`maxbattle→max_battle`. (`greeting` has no webhook source — omit.)

### 2. Unified dispatch (`enrich.go`)
`enrichForType(name, partial, lang, platform) (EnrichedResult, error)` resolves
`name` via the alias table, parses the partial, runs the real `enricher.*`
methods, and returns `{ Layers (base+perLang+perUser), TemplateType, Extras }`.
- `EnrichWebhook` → calls it, flattens `Layers` into the `variables` map (as
  today), and additionally merges `Extras` so the editor sees `original.*` etc.
- `test.go` → calls it, then builds the `RenderJob` (setting `IsChange`/
  `OriginalView`/`OverrideCleanTTH`/`TemplateType` from `Extras`) and delivers.
Derived-type builders live here, reusing production render construction where it
is already factored (`dts.BuildOriginalView`, the quest-summary grouping, the
weather-change enrichment) rather than re-implementing.

### 3. `GET /api/dts/testdata` (huma_dts_reads.go / dts_testdata.go)
- Accept `?dtsType=<name>`; resolve via the alias table; return the entries for
  that DTS type, server-side filtered (e.g. pokestop→invasion vs lure) and each
  tagged with `dtsType`(s). Keep `?type=<webhookType>` working.
- Optionally add a `GET /api/dts/testdata/types` (or include the map in the
  existing response) so the editor can discover the full DTS-type→source map and
  drop its hardcoded copy.

### 4. `POST /api/dts/enrich` (huma_dts_writes.go)
No signature change — it already takes `{type, webhook, language, platform}`.
`type` now accepts any DTS name (resolved by the unified dispatch), and the
response `variables` includes derived extras. Remove the need for the editor's
client-side special-casing.

### 5. `POST /api/test` + `!poracle-test` (cmd/processor/test.go, bot command)
Route through the unified dispatch; accept DTS type names; support the derived
partials end-to-end (render + deliver).

### 6. Test data (`fallbacks/testdata.json`)
- Move the pokestop-incident samples (kecleon, gold-stop, pokemon-contest, …)
  to `incident`-typed entries.
- Add `monster-changed`, `weather-change`, `quest-summary`, `rsvp-changes`
  sample entries (partials) — `weather-change` includes a short affected-pokemon
  list, per the requirement.

### 7. Editor handoff doc (D7)
`docs/superpowers/handoffs/2026-07-18-dts-editor-derived-types.md` — lives **in
this (PoracleNG) repo**; the operator points the editor agent at it to read.
The complete server contract the editor must consume.

## Editor handoff doc — required contents
- The DTS-name-addressable `POST /api/dts/enrich` (accepts every DTS type incl.
  derived; `variables` now carries `original.*` / affected / group).
- The DTS-type-aware `GET /api/dts/testdata?dtsType=` (server filters + tags);
  and the discoverable DTS-type→source map endpoint.
- The new derived-type test entries + their partial shapes.
- **Delete list** for the editor: the hardcoded `dtsToWebhookType` map, the
  client-side pokestop invasion/lure filter, and the `monsterNoIv→pokemon`/
  `egg→raid` special-casing in `capture-test-data.mjs`; adjust `api-client.js`/
  `TestDataPanel.jsx` to address by DTS type.
- Note that `fort-update`/`maxbattle` (and the derived types) are now selectable.

## Testing
- Unified dispatch: `enrichForType` returns identical `Layers`/`TemplateType`
  for a webhook type and its DTS alias (`pokemon` == `monster`); the derived
  builders populate `Extras` correctly (OriginalView for monster-changed;
  affected list for weather-change; group for quest-summary; RSVP for rsvp).
- `/api/dts/enrich` by DTS name (incl. derived) returns the expected variables.
- `/api/dts/testdata?dtsType=invasion` returns only invasion scenarios (not
  lure); `?dtsType=incident` returns the moved samples; tags present.
- `/api/test` + `!poracle-test` render+deliver each derived type from its
  partial (fake dispatcher assertion, mirroring existing test-command tests).
- Back-compat: existing `?type=<webhookType>` and webhook-type enrich unchanged.

## Out of scope / deferred
- `greeting` test data (no webhook source).
- Changing the on-disk testdata *file* format (stays `[]TestDataEntry`; partials
  ride in the existing `webhook` field).
- Editor-side implementation (covered by the handoff doc; a separate effort in
  the editor repo).

## Affected files (PoracleNG)
`cmd/processor/enrich.go` (+ alias table, unified dispatch, derived builders),
`cmd/processor/test.go` (route through dispatch), `internal/api/dts_testdata.go`
+ `huma_dts_reads.go` (dtsType query + tags + map endpoint),
`internal/api/huma_dts_writes.go` (enrich by DTS name), the `!poracle-test`
command, `fallbacks/testdata.json` (move incident + add 4 partials), and the
editor handoff doc.
