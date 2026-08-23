# Showcase Support — Design & Implementation Plan

Status: Phase 1 + Phase 2 implemented (this PR); Phase 3 pending
Date: 2026-07-14

## Summary

Pokémon GO **Showcases** (pokéstop contests) currently cannot produce a correct
alert on a modern Golbat. This spec adds a showcase ingestion path driven by the
Golbat **`pokestop`** webhook, gated on `showcase_expiry > now`, that reuses the
existing **incident** downstream (the `incident` template type, `translateShowcaseRankings`,
and v2 `/incident` tracking) by synthesising the `display_type = 9` classification.
It is **not** a new tracking type — no new table, store, matcher, command, or
tracking API.

## Background — why showcases are broken today

Golbat emits showcase signals on **two** webhooks, and each carries only half of
what an alert needs:

- **`pokestop` webhook** (`decoder/pokestop_state.go` `createPokestopWebhooks`) —
  carries the showcase **content**: `showcase_focus`, `showcase_expiry`,
  `showcase_rankings`, `showcase_pokemon_*`, `showcase_ranking_standard`. **No
  `display_type`.** It is a *snapshot* multiplexing lure + power-up + showcase;
  every fire includes all three classes' fields regardless of which changed.
- **`invasion` webhook** (`decoder/incident_state.go` `createIncidentWebhooks`,
  fed from GMO fort `PokestopDisplays` at `gmo_decode.go`) — *does* fire for
  contests with `display_type = 9` (`INCIDENT_CONTEST`), but is a **bare
  envelope**: expiration + `display_type=9` + `character=0` + empty lineup. **No
  showcase content.**

PoracleNG's entire showcase feature keys on the `invasion` / `display_type >= 7`
path (the one with no content):

- `isIncident := gruntTypeID == 0 && displayType >= 7` — `cmd/processor/invasion.go:166`.
- `ResolveGruntTypeName(0, 9, gd)` → `"showcase"` (via `gd.Util.PokestopEvent[9]`) —
  `internal/matching/invasion.go:92-99`. Only then can a v2 `/incident` rule
  (`grunt_type="showcase"`) match.
- `translateShowcaseRankings` reads `showcase_rankings` and sets `showcasePresent`,
  `showcase[]`, `showcaseFirst` — `internal/enrichment/invasion.go:306-350`.
- Showcase DTS fields live only on the `incident` template type —
  `internal/api/dts_fields.go:279-308`.

Consequences of the mismatch:

- The content-rich `pokestop` webhook has no `display_type`, so `routePokestop`
  (`internal/webhook/receiver.go:144-170`, which only peeks `lure_expiration`,
  `incident_expiration`, `incident_grunt_type`) drops it into `ProcessInvasion`
  as a degenerate `grunt_type=0` invasion that resolves to the name `"0"`,
  matches nobody, and whose parsed `showcase_rankings` never reach the
  (unselected) incident template.
- The `display_type=9` invasion webhook classifies correctly but has no rankings
  → `showcasePresent=false` → an empty showcase card.

**Neither webhook alone renders a real showcase.** The downstream is already
correct; it just never receives `display_type=9` *together with* the content.

## Data model (verified from Golbat source + production logs)

### Showcase fields on the `pokestop` webhook

| Field | Meaning |
|-------|---------|
| `showcase_focus` | JSON object; `type` key names the focus class, remaining keys depend on it. The authoritative "what is featured". |
| `showcase_expiry` | **Unix seconds when the contest ends.** The *only* active/ended signal — there is no boolean flag. |
| `showcase_rankings` | `{total_entries, last_update, contest_entries[≤3]}` leaderboard snapshot. |
| `showcase_pokemon_id` / `_form_id` / `_type_id` | Deprecated flat mirrors, populated **only** for `pokemon` / `type` focus; **null for all other focus types**. |
| `showcase_ranking_standard` | Ranking metric enum: `1`=MIN (smallest wins), `2`=MAX (largest wins). |

### `showcase_focus` types (10) — `decoder/pokestop_showcase.go:42-123`

`pokemon` (`pokemon_id`, optional `pokemon_form`), `type` (`pokemon_type_1`,
optional `pokemon_type_2`), `alignment` (`pokemon_alignment`), `class`
(`pokemon_class`), `family` (`pokemon_family`), `buddy` (`min_level`),
`generation` (`generation`), `hatched` (`hatched` bool), `mega`
(`temp_evolution`, `restriction`), `shiny` (`shiny` bool).

### Critical properties

1. **`showcase_expiry` is the only active/ended signal.** Active iff
   `showcase_expiry != null && showcase_expiry > now`.
2. **Fields linger stale.** Golbat has no `ExpireShowcase` and no cron that nulls
   showcase fields on end. After a contest ends the stored record keeps the last
   `showcase_focus` / `showcase_expiry` (now past) / `showcase_rankings` until a
   new contest overwrites them — and those stale fields ride along on any
   unrelated `pokestop` webhook fire (e.g. a lure change). **Same class of bug as
   the stale-lure issue already fixed (PR #160).**
3. **The flat `showcase_pokemon_*` fields are insufficient.** Production data
   includes a `buddy` focus (`{"min_level":3,"type":"buddy"}`) with both flat
   fields null. The enrichment **must parse `showcase_focus` JSON**, not the flat
   columns.

### Production log findings (operator scanner, 2026-07-14)

- Focus types seen: `type` and `buddy`. (`type` populates `flat_type_id`; `buddy`
  has all flat fields null.)
- `showcase_ranking_standard = 2` (MAX) throughout.
- **Staleness is the norm:** all sampled `showcase_focus`-bearing webhooks except
  one carried a *past* `showcase_expiry` — stale remnants on lure webhooks. The
  `showcase_expiry > now` gate is the majority case, not an edge case.
- Fire frequency ≤ 2 per stop in the sampled window (small sample) → fire-once
  dedup likely adequate for MVP; edit-mode is a nice-to-have.
- **Open:** whether the scanner also emits `display_type:9` invasion webhooks was
  not measured (grep #1 not run). Suppression (below) is designed to be safe
  either way.

## Design decisions

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **Drive showcases from the `pokestop` webhook.** | It is the only source with the content (focus, expiry, rankings). |
| D2 | **Reuse the incident downstream by classifying showcases as `display_type=9`.** No new tracking type. | `ResolveGruntTypeName→"showcase"`, v2 `/incident` tracking, and `translateShowcaseRankings` already handle `display_type=9`. |
| D2b | **Render via a dedicated `showcase` template type** (display-only; `AlertType` stays `incident`). | A showcase is a specialised display model (leaderboard + focus), distinct from the plain Gold-Stop/Kecleon incident card. Tracking/rate-limit/blocked-alerts semantics stay tied to `incident`; only the selected template differs (same pattern as `rsvpChanges` vs `raid`). A bundled default `showcase` template ships in `fallbacks/dts.json`. |
| D3 | **Gate on `showcase_expiry > now`.** | Fields linger stale (see property 2); without this every lure/power-up snapshot carrying a dead showcase fires. Direct analogue of the lure gate. |
| D4 | **Parse `showcase_focus` for all 10 focus types.** | Flat fields are null for `buddy` and 7 other classes (confirmed in prod). A small switch produces a translated "featured" descriptor. |
| D5 | **Dedicated `ProcessShowcase` handler, not raw-reinjection into `ProcessInvasion`.** | The `pokestop` and `invasion` webhooks use different field names (`name` vs `pokestop_name`, `showcase_expiry` vs `incident_expire_timestamp`). A dedicated handler parses the pokéstop shape correctly, then builds an invasion-style `matching.InvasionData` with `GruntType="showcase"`, `DisplayType=9`, and reuses the invasion matcher + incident enrichment + incident template. |
| D6 | **Suppress the content-less `display_type=9` invasion webhook** (if the scanner sends it). | Otherwise showcase trackers get an empty incident card from the invasion webhook alongside the real one from the pokéstop webhook. Gate in `ProcessInvasion`: if `isIncident && displayType==9 && ShowcaseRankings empty` → drop. **Kept as a safety net** — operator observed no `display_type:9` invasion webhooks, but was outside the showcase window so it can't be relied on (O1). |
| D7 | **Edit-mode in scope (Phase 1), reusing the raid-RSVP path.** EditKey `showcase:<pokestop_id>:<showcase_expiry>`; `OverrideCleanTTH = showcase_expiry`; dedup on `(pokestop_id, showcase_expiry, rank-1 fingerprint)`. | Showcases first fire with an **empty** leaderboard (contest start), then re-fire on rank-1 movement. Edit collapses all fires of one contest into a single message that fills in and updates in place. See "Edit-mode tracking" below. |

## Structure decision — reuse incident, no dedicated showcase structure (yet)

The load-bearing choice: **showcases piggyback entirely on the incident/invasion
*tracking* structure; we create no dedicated showcase table, command, matcher,
API, or rowtext.** This is deliberate — defer a dedicated structure until there's
user feedback justifying it.

Distinguish three layers so the tradeoff is clear:

- **Tracking / subscription (REUSED, not extended).** A showcase rule *is* a v2
  `/incident` rule (`grunt_type="showcase"`) in the `invasion` table. The only
  filter it can express is "showcase or not" → **match-all**. Anything finer
  (filter by focus — "Water-type showcases", "rare pokemon in top 3") is **not
  possible** without a dedicated tracking structure. This is the refinement we're
  consciously deferring.
- **Ingestion (NEW, but thin and not a "structure").** Parsing the showcase
  fields and a `ProcessShowcase` handler are unavoidable — Golbat only sends the
  content on the `pokestop` webhook. This is a webhook reader, not a tracking
  structure; it does not lock anything in.
- **Display / enrichment & template (REUSED + extendable).** The `incident`
  template and its enrichment already render arbitrary showcase content, so the
  *alert itself* can still be made as rich as we like (focus line, leaderboard,
  ranking standard — Phase 2). Display refinement is **not** blocked by this
  choice; only tracking-granularity is.

**Why this is a safe, low-commitment first step:** because ingestion goes through
a dedicated `ProcessShowcase` handler, adding a real showcase tracking structure
later means pointing that handler at a new matcher + migrating existing
`grunt_type="showcase"` incident rules — the webhook parsing, expiry gate, dedup,
edit path, and enrichment all carry over. We are not painting ourselves into a
corner; we're choosing match-all-via-incident now and keeping the door open.

## Characteristics (new-tracking interview, resolved)

- **Webhook source:** new *ingestion* path on an existing envelope (`pokestop`);
  reuses an existing *tracking* type (incident). → new handler + routing; **no**
  new table/store/matcher/command/API/rowtext.
- **Pokemon-by-ID / form / numeric ranges / list filters:** N/A for MVP (match
  all showcases). Focus-based filtering is a Phase-3 option (D4 enables it).
- **Time-bound expiry (#6):** **yes** — `showcase_expiry`. Enrichment computes
  `tth`/`disappearTime` from it.
- **Edit support (#7):** **in scope (Phase 1)** — EditKey shape decided (D7); mechanism below.
- **Auto-delete on TTH (#8):** inherit whatever the incident template/rule sets;
  no special work.
- **Bound to a POI (#10):** pokéstop (already the incident identity).
- **Repeat-firing webhook (#13):** **yes** — dedup key includes `showcase_expiry`
  (D7).
- **Translation (#15):** **yes** — focus descriptor needs translated pokémon /
  type / class names (D4).

## Edit-mode tracking (how it works in the existing structure)

Showcases need edit because a contest **first fires with an empty leaderboard**
(the pokéstop webhook fires on `showcase_expiry` change = contest start, before
any entries), then re-fires only on **rank-1 movement**. Without edit, a user
gets an empty card plus one new card per rank-1 change. **No new tracking
structure is required** — this reuses the raid-RSVP edit path verbatim:

1. **Rule storage.** Showcases are tracked as v2 `/incident` rules
   (`grunt_type="showcase"`) in the existing `invasion` table, which already has a
   `clean` column. The **edit bit** is `clean & 2` (`db/clean.go`; `3` = edit+clean).
   The user opts into edit the same way raid RSVP does — no schema change.
2. **Stable EditKey.** `ProcessShowcase` sets
   `RenderJob.EditKey = "showcase:<pokestop_id>:<showcase_expiry>"`. It is constant
   across every fire of the same contest (same stop, same end time), so the empty
   → filling → rank-shuffle fires all resolve to the same message.
3. **Generic render/delivery path — already wired.** `RenderAlert` (the render
   call the incident/showcase type uses) already threads `EditKey`
   (`cmd/processor/render.go:173`). `delivery.FairQueue` looks the key up in
   `delivery.MessageTracker`; if a prior message exists for that (EditKey, target)
   it **edits in place**, else sends new and tracks it (tracking happens only when
   `clean/edit` bit is set). Invasion simply never populated `EditKey` — that's the
   only gap.
4. **TTL = contest end.** `RenderJob.OverrideCleanTTH = showcase_expiry`
   (`render.go:186`) keeps the message editable, and clean-deletes it at contest
   end if the clean bit is also set.
5. **Dedup must let updates through.** `CheckShowcase` keys on
   `(pokestop_id, showcase_expiry, rank-1 fingerprint)` — where the fingerprint is
   the top entry's `pokemon_id` + `score` + `total_entries`, mirroring Golbat's own
   rank-1 fire trigger. A key on `(pokestop, expiry)` alone would collapse every
   re-fire into the first empty card; including the fingerprint lets each
   meaningful leaderboard change reach the edit path.

Non-edit rules still work — they just send a fresh message per rank-1 change
(few, since Golbat only fires on rank-1 movement). Edit is **recommended** for
showcase rules and worth documenting as such.

## Implementation plan

### Phase 1 — ingestion, correctness & edit (MVP: real, self-updating showcase alerts)

- [ ] **T1. Webhook parsing.** Add showcase fields to the pokéstop-shape parse.
  Add a `ShowcaseWebhook` struct (or extend the lure parse) in
  `internal/webhook/types.go` with `pokestop_id`, `name`, `url`, `latitude`,
  `longitude`, `updated`, `showcase_expiry` (int64), `showcase_focus`
  (`json.RawMessage`), `showcase_rankings` (`json.RawMessage`),
  `showcase_pokemon_id/_form_id/_type_id` (nullable int), `showcase_ranking_standard`.
- [ ] **T2. Routing.** In `routePokestop` (`internal/webhook/receiver.go:144-170`)
  add a showcase branch **independent** of the lure/invasion branches (a stop can
  have a lure *and* a showcase): if `showcase_expiry` present (or
  `showcase_rankings` present), call `ProcessShowcase(raw)`. Do **not** fold it
  into the `lure_expiration <= 0` fallback.
- [ ] **T3. Handler + gate + dedup + edit.** New `cmd/processor/showcase.go`
  `ProcessShowcase`: parse `ShowcaseWebhook`; **drop if `showcase_expiry <= now`**
  (the core gate — mirrors `hasActiveLure`); dedup via new
  `tracker.DuplicateCache.CheckShowcase(pokestopID, showcaseExpiry, rank1Fingerprint)`;
  build `matching.InvasionData` with `GruntType="showcase"`, `DisplayType=9`,
  `Expiration=showcase_expiry`, `ShowcaseRankings=<raw>`; call the existing
  `InvasionMatcher.Match`; enqueue a `RenderJob` with `AlertType="incident"`,
  `TemplateType="incident"`, **`EditKey="showcase:<pokestop_id>:<showcase_expiry>"`**,
  and **`OverrideCleanTTH=showcase_expiry`** (see "Edit-mode tracking"). Reuse
  `filterBlocked` / `filterValidation` / `filterMuted`.
- [ ] **T4. Suppress content-less invasion showcases** (D6). In
  `cmd/processor/invasion.go`, after `isIncident` is computed: if
  `displayType==9 && len(inv.ShowcaseRankings)==0` → debug-log and return.
- [ ] **T5. Config toggle.** `DisableShowcase bool` in `[general]`
  (`internal/config/config.go`), `disable_showcase` in `config.example.toml`, and
  a schema `Field` in `internal/api/config_schema.go`. (Alternatively reuse
  `DisableInvasion` — **open question O4**.) `ProcessShowcase` returns early when
  disabled.
- [ ] **T6. Test data.** Add `showcase` scenarios (active `type` focus, active
  `buddy` focus, and an already-expired one) to `fallbacks/testdata.json`; wire
  `case "showcase"` in `cmd/processor/test.go` and `validHooks` in
  `bot/commands/poracletest.go`. Confirm `!poracle-test showcase,type` delivers.
- [ ] **T7. Tests.** Pure-predicate + handler tests: expiry gate (active vs
  stale), dedup, the D6 suppression, and a `ResolveGruntTypeName(0,9)→"showcase"`
  regression. Lock a real production webhook (the Sephardic Temple sample) like
  `TestHasActiveLure_RealShowcaseStopWithStaleLure`.

### Phase 2 — focus enrichment & display content — DONE (this PR)

- [x] **T8. Parse `showcase_focus`** (`internal/enrichment/showcase.go`
  `ShowcaseFocusTranslate`). All 10 focus classes enumerated in util.json
  `showcaseFocus`; category labels via i18n `showcase_focus_{type}`; specific
  value resolved per class.
- [x] **T9. DTS field metadata.** `showcaseFocusPresent/Type/Category/Name/Emoji`
  added to `incidentFields` (`internal/api/dts_fields.go`).

**Enum caveat — the values do NOT map by index (verified against Golbat proto).**
Value resolution deliberately avoids assuming the game-proto enum equals a
gamelocale key number:
- **alignment** — `ContestPokemonAlignmentFocusProto`: `0 unset, 1 PURIFIED, 2
  SHADOW` — **reversed** from gamelocale `alignment_1`=Shadow / `alignment_2`=Purified.
  Mapped explicitly.
- **generation** — proto `PokedexGenerationId` (`GEN1=1..GEN8=8, GEN8A=9 Hisui,
  GEN9=10 Paldea, MELTAN=1002`) doesn't line up with the 1..9 gen numbering.
  Mapped explicitly (proto 1-8 → gen 1-8, proto 10 → gen 9; Hisui/Meltan → no label).
- **class** — `HoloPokemonClass` `0 normal, 1 legendary, 2 mythic, 3 ultra beast`.
- **type / pokemon / family** — proto ids match existing `poke_type_{id}` /
  `poke_{id}` keys, reused directly.

### Phase 3 — pending
- [ ] Focus line in the default `incident` template + a `showcaseRankingStandard`
  (MIN/MAX) label are minor polish left for a follow-up.

### Phase 3 — optional follow-ups (defer)

- [ ] **T11. Focus-based tracking filters.** Let users track showcases by featured
  `type`/`pokemon`/`class` (extends v2 incident tracking with a focus filter).
  **Decided out for now (O3)** — match-all only, since there is no new showcase
  command or table. Revisit only if operators ask.

## Open questions — RESOLVED (operator, 2026-07-14)

- **O1 — `display_type:9` invasion webhooks?** None observed, but operator was
  outside the showcase window, so it can't be relied on. → **Keep D6/T4 as a
  safety net** (drop content-less `display_type=9` invasions if they arrive).
- **O2 — fire-once vs edit-mode?** → **Edit-mode, Phase 1.** Showcases first
  appear empty and re-fire; edit collapses them into one self-updating message
  (see "Edit-mode tracking"). Reuses the raid-RSVP path; no new structure.
- **O3 — focus-based tracking filters?** → **No.** Match-all only (no new showcase
  command/table). T11 deferred indefinitely.
- **O4 — dedicated `disable_showcase` toggle?** → **Yes**, add it (in preparation),
  separate from `disable_invasion`.

## What is explicitly NOT needed (reused from incident)

New DB migration/table, new store + UID accessors, new matcher, new tracking API
endpoints, new bot command, new rowtext, `trackingTables`/`backup`/`human_queries`
additions. Showcases are stored and tracked as incident rules
(`grunt_type="showcase"`) and rendered by the `incident` template — all of which
already exist.
