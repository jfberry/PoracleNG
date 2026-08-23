# Editor Handoff — Derived DTS Types & DTS-Name-Addressed Test Data

**For:** the DTS template editor (`~/dev/poracle-embed-visualizer`).
**From:** PoracleNG processor, branch `feature/derived-dts-test-data`.
**Date:** 2026-07-18.

This describes the **server-side** changes now available so the editor can (a) preview every DTS template type — including the *derived* ones that aren't a single raw webhook — and (b) stop hardcoding the DTS-type→webhook-type mapping and stop filtering scenarios client-side. Nothing in the editor is required to change for existing behaviour to keep working (all old calls are back-compatible); this doc lists what you *can now delete/simplify* and what's *newly available*.

## TL;DR — what you can delete
In `scripts/capture-test-data.mjs` (and wherever the editor mirrors this logic — `src/lib/api-client.js`, `src/components/TestDataPanel.jsx`):
1. **Delete the hardcoded `dtsToWebhookType` map** — the server now returns it (see §3).
2. **Delete the client-side pokestop invasion/lure filter** (the `grunt_type`/`lure_id` sniffing) — the server now splits it (see §2).
3. **Delete the `monsterNoIv→pokemon` / `egg→raid` special-casing** in the enrich call — pass the DTS type name directly (see §1).
4. **Add the newly-previewable types** to the editor's type list: the derived types `monsterChanged`, `incident`, `questSummary`, `weatherchange`, `rsvpChanges`, plus `fort-update` and `maxbattle` (previously omitted from `capture-test-data.mjs`'s `dtsTypes` array).

## 1. `POST /api/dts/enrich` now accepts DTS type names (incl. derived)
- Request unchanged: `{ type, webhook, language, platform }`.
- `type` now accepts **any DTS template type name** OR a raw webhook type — resolved server-side via the shared alias table. So you can send `type: "monster"`, `"monsterNoIv"`, `"egg"`, `"invasion"`, `"lure"`, `"incident"`, `"monsterChanged"`, `"questSummary"`, `"weatherchange"`, `"rsvpChanges"`, `"maxbattle"`, `"fort-update"`, etc. — **no client-side remap needed**. (`monsterNoIv` now correctly yields the monsterNoIv alias set; `egg` correctly yields the egg template — the two cases you special-cased.)
- Response `variables` now includes the **derived extras** the template reads:
  - `monsterChanged` → the `original.*` field bag (`{{original.name}}`, `{{original.iv}}`, …) plus `changeType`/`changeTypeText`.
  - `weatherchange` → `enrichedActivePokemons` (the affected-pokemon list) + weather names.
  - `questSummary` → the group fields (`rewardType`, `reward`, `count`, `quests`, per-row `withAR`, chunk/chunks).
  - `rsvpChanges` → the raid + RSVP fields (reuses the `raid` field set).
- For a derived type, send the **partial** (see §4) as `webhook` — the server enriches it. The two-stage flow (fetch testdata → user edits → enrich → preview) works unchanged; you just fetch the derived partials from the testdata endpoint like any other scenario.

## 2. `GET /api/dts/testdata?dtsType=<name>` — server filters + tags
- **New query param `?dtsType=<name>`**: resolves via the alias table and returns **only the entries that preview that DTS type**, each tagged with a `dtsType` field on the entry. The server performs the splits you did client-side:
  - `?dtsType=invasion` → only grunt invasions; `?dtsType=lure` → only lures (the pokestop invasion/lure split — payload-shape based, server-side).
  - `?dtsType=egg` → raid entries with `pokemon_id == 0`; `?dtsType=raid` → the rest (raid/egg split).
  - `?dtsType=incident` → the pokestop-event samples (kecleon/gold-stop/pokemon-contest — moved to `type:"incident"`).
  - `?dtsType=monsterChanged` / `questSummary` / `weatherchange` / `rsvpChanges` → the derived partials.
- **Legacy `?type=<webhookType>` is unchanged** (returns every entry of that raw type, untagged; takes precedence if both are set). Keep using it if you prefer, but `?dtsType=` removes the need for client-side filtering.
- Response shape: `{ status, testdata, types }` (see §3 for `types`).
- `nest` currently has no bundled sample (returns empty) — expected.

## 3. The DTS-type→source map (`types`) — drop your hardcoded copy
- Every `GET /api/dts/testdata` response now includes a **`types` object**: the full DTS-type→source map. Each key is a DTS type name; each value is `{ webhookType, templateType, derived }`.
  ```jsonc
  "types": {
    "monster":        { "webhookType": "pokemon",         "templateType": "monster",        "derived": false },
    "monsterNoIv":    { "webhookType": "pokemon",         "templateType": "monsterNoIv",    "derived": false },
    "egg":            { "webhookType": "raid",            "templateType": "egg",            "derived": false },
    "invasion":       { "webhookType": "pokestop",        "templateType": "invasion",       "derived": false },
    "incident":       { "webhookType": "incident",        "templateType": "incident",       "derived": true  },
    "monsterChanged": { "webhookType": "monster_changed", "templateType": "monsterChanged", "derived": true  },
    "questSummary":   { "webhookType": "quest_summary",   "templateType": "questSummary",   "derived": true  },
    "weatherchange":  { "webhookType": "weatherchange",   "templateType": "weatherchange",  "derived": true  },
    "rsvpChanges":    { "webhookType": "rsvp_changes",    "templateType": "rsvpChanges",    "derived": true  }
    // …plus gym, nest, lure, quest, raid, maxbattle, fort-update, showcase, greeting-less types
  }
  ```
- **Replace the editor's hardcoded `dtsToWebhookType` map with this.** Prefer `?dtsType=` (server does the resolution + filtering); use `types` only if you still want the client to know the source webhook type per DTS type.

## 4. Derived-type test "partials" (informational)
The derived testdata entries carry a structured payload in the existing `webhook` field — you generally don't construct these (fetch them via `?dtsType=`), but for reference:
- `monster_changed`: `{ old: <pokemon webhook>, new: <pokemon webhook> }`.
- `weatherchange`: a weather-change webhook with an `affected` (active pokemon) list.
- `quest_summary`: a raid/quest group `{ reward, quests:[…] }` (several quests under one reward).
- `rsvp_changes`: a raid webhook with an `rsvps` list.
The editor can let a user edit these and POST them to `/api/dts/enrich` exactly like a normal scenario.

## Caveats
- **`rsvpChanges` has no bundled default template** — it's an opt-in feature. Previewing it requires an operator-authored `rsvpChanges` template; without one there's nothing to render. (The enrich/testdata support is present, so the *editor* can still enrich + edit it.)
- **`greeting`** has no webhook source and no test data — omit it from the previewable list.
- `!poracle-test` (the live Discord/Telegram command) now also accepts DTS type names (`!poracle-test monsterChanged,ditto-reveal`) — not editor-relevant, but the same alias table backs both.

## Quick migration checklist
- [ ] Fetch `types` from `GET /api/dts/testdata`; delete the hardcoded `dtsToWebhookType`.
- [ ] Use `GET /api/dts/testdata?dtsType=<name>`; delete the client-side pokestop/lure filter.
- [ ] Pass the DTS type name straight to `POST /api/dts/enrich`; delete the `monsterNoIv`/`egg` special-cases.
- [ ] Add `monsterChanged`, `incident`, `questSummary`, `weatherchange`, `rsvpChanges`, `fort-update`, `maxbattle` to the previewable type list.
- [ ] Render the derived extras (`original.*`, `enrichedActivePokemons`, quest group, RSVP) in the preview.
