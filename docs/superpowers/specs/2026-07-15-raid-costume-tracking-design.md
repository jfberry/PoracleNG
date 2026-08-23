# Raid Costume Tracking — Design

Status: draft / for review
Date: 2026-07-15
Mirrors: `docs/superpowers/specs/2026-07-15-costume-tracking-design.md` (pokemon costume), applied to the **raid** tracking type.

## Summary

Add **costume** as a filter dimension of **raid** tracking (costumed raid bosses,
e.g. costumed Pikachu raids): filter raid rules by costume, resolve costume
names in `!raid`/`/raid`, surface a boss species' recently-seen **raid**
costumes in `!info`, and expose costume on the v1 and v2 raid APIs.

**Display is already done.** Raid enrichment already weaves the boss costume into
`fullName`/`megaName`/`costumeName` (shipped in `fix(enrichment): raid & maxbattle
boss names now include costume`). This design is the **tracking / filter /
recency / !info / API** half only.

## Background

Raid webhooks already carry `costume` (`webhook.RaidWebhook.Costume`,
`webhook/types.go`). Costumed raid bosses spawn during events, so operators want
to (1) track a specific costumed raid, or (2) exclude costumes. The raid matcher
currently filters on pokemon_id / level / form / evolution / team but not
costume, and the raid table has no costume column.

## Decisions (mirror pokemon costume, confirmed for raids)

| # | Decision |
|---|----------|
| D1 | **Filter sentinel:** `raid.costume` uses **9000 = any** (`bot.WildcardID`, the same sentinel raid `evolution` already uses), **0 = explicitly no costume**, **N = that costume**. Default 9000; absent in a payload ⇒ 9000. (Raid `form` uses 0=any, but costume needs 9000=any so `0` stays meaningful as "no costume" — identical to pokemon costume.) |
| D2 | **Costume is independent from form/evolution/level** — all apply in the matcher. |
| D3 | **Command syntax:** `!raid <pokemon> costume:<name-or-id>`, resolved via the existing `ArgMatcher.ResolveCostume` (default 9000). `costume:0` = no costume. `/raid` gains a `costume` option with name autocomplete. Costume applies to every rule the command generates (single-form and multi-form `pokemon_form` paths). |
| D4 | **Recency — SEPARATE raid tracker.** New `RecordRaidCostume(pokemonID, costume)` / `RecentRaidCostumes(pokemonID)` bucket, fed only by raid webhooks (distinct from the spawn-fed `RecordCostume`). `/raid costume` autocomplete boosts raid-seen costumes; `/track costume` keeps boosting spawn-seen costumes. |
| D5 | **`!info <pokemon>`** gains a distinct **"Recently-seen raid costumes"** section (parallel to the existing spawn "Recently-seen costumes" section), sourced from `RecentRaidCostumes`. Each guarded — shown only when non-empty. |
| D6 | **rowtext / `!tracked`:** the raid rule description shows the costume — translated name for N>0, "no costume" for 0, nothing at 9000 — reusing the monster rowtext costume logic (translate → masterfile-name fallback). |
| D7 | **v2 raid API** gains a nullable `Costume *int` (create + update writable; omit/null ⇒ wildcard 9000; 0 ⇒ no costume; N ⇒ that costume; read returns null at 9000). Mirrors v2 pokemon costume, **not** raid form's 0-semantic. |
| D8 | **v1 raid API compatibility:** an absent `costume` defaults to **9000**, not Go-zero 0 — otherwise v1 clients (ReactMap/PoracleWeb) that don't send it would create no-costume raid rules and, because costume is rule-identity (no `diff` tag), duplicate rows on re-submit. The `raidInsertRequest.Costume` is `flexInt`; both build paths default via `req.Costume.intValue(9000)`. |

## Components

### 1. DB storage
- Migration `0000NN_add_raid_costume.{up,down}.sql`:
  `ALTER TABLE raid ADD COLUMN costume INT NOT NULL DEFAULT 9000;`
- `db.RaidTracking` (`db/raids.go`) gains `Costume int \`db:"costume"\``.
- `db.RaidTrackingAPI` (`db/tracking_queries.go`) gains `Costume int \`db:"costume" json:"costume"\`` — **no `diff` tag** (rule identity, like `Form`).
- SQL sites updated in lock-step: `LoadRaids` SELECT (`db/raids.go`), `SelectRaidsByIDProfile` + `SelectRaidsByID` SELECT, `InsertRaid` INSERT (cols+binds), raid UPDATE (cols+binds). Column/placeholder counts must balance.

### 2. Matcher (`matching/raid.go`)
- `RaidData` gains `Costume int`, populated from `raid.Costume` in the raid handler (`cmd/processor/raid.go`).
- In `MatchRaid`, alongside the form (`r.Form != raid.Form && r.Form != 0`) and evolution (`r.Evolution != 9000 && ...`) checks:
  ```go
  if r.Costume != 9000 && r.Costume != raid.Costume {
      continue
  }
  ```
- Egg matching is unaffected (eggs have no boss/costume).

### 3. Command (`bot/commands/raid.go`, slash)
- Parse the `costume:` arg via `ArgMatcher.ResolveCostume` (numeric fast-path; else `costume_{id}` user-lang + English match). Default 9000; unresolved name ⇒ 🙅 (mirrors `!track`).
- Apply the resolved costume to every generated raid rule (the single-form path and each entry of the `pokemon_form` multi-form path).
- `!raid remove … costume:N` — selective removal by costume (mirror `!untrack costume:N`).
- Slash `/raid`: add a `costume` option (autocomplete=true) to `raidOptions` (`definitions.go`); `mappers/raid.go` emits `costume:<id>`; the dispatcher routes `(cmd="raid", opt="costume")` to a costume autocomplete that boosts `RecentRaidCostumes(pid)` where `pid` is resolved from the sibling **`boss`** option (`/raid` has no separate `pokemon`/`form` option — the boss name carries the species/form), reusing `PrependRecentCostumes` + `ResolvePokemonID`.
- Store empty/default template behavior unchanged.

### 4. rowtext (`rowtext/raid.go`)
- Add a costume clause mirroring `rowtext/monster.go`: omit at 9000, `msg.no_costume` at 0, translated `costume_{id}` name (masterfile-name fallback) for N>0. Requires `RaidTracking.Costume`.

### 5. Recency (`tracker/recent_activity.go`, `cmd/processor/raid.go`, `!info`, slash)
- New `raidCostumesByPokemon map[int]map[int]time.Time` + `RecordRaidCostume(pokemonID, costume int)` (skips ≤0) + `RecentRaidCostumes(pokemonID int) []int`, mirroring the costume trio 1:1.
- Producer: `RecordRaidCostume(raid.PokemonID, raid.Costume)` in the raid webhook handler (`cmd/processor/raid.go`), guarded `costume > 0` and `recentActivity != nil`.
- `!info <pokemon>`: `availableRaidCostumes(ctx, pokemonID)` helper (mirrors `availableCostumes`, `id — name`), rendered as a "Recently-seen raid costumes" section under a new `msg.info.recent_raid_costumes` key, placed after the spawn "Recently-seen costumes" section.
- `/raid costume` autocomplete boost: dispatcher case for `(cmd="raid", opt="costume")` prepends `RecentRaidCostumes(pid)` when the field is empty and the sibling `boss` option resolves to a pokemon id.

### 6. v1 raid API (`api/trackingRaid.go`)
- `raidInsertRequest` gains `Costume flexInt \`json:"costume"\``.
- Both build paths set `Costume: req.Costume.intValue(9000)` — the single-form path and each `pokemon_form` entry.
- `toRaidTracking` copies `Costume`.
- Regression test: v1 raid payload with no `costume` → stored 9000; `costume:0` → 0; `costume:5` → 5; re-POST of an existing rule → no duplicate row (idempotency).

### 7. v2 raid API (`api/v2_raid.go`)
- `v2RaidRule` gains `Costume *int` (nullable, doc'd 9000/0/null semantics).
- Write: `valueOr(req.Costume, 9000)`; read: `ptrUnless(row.Costume, 9000)`.
- OpenAPI golden regenerated (additive).

### 8. DTS field metadata
- `costumeName` is already a raid DTS field (added in the display fix). No change needed here beyond confirming it's present.

### 9. i18n
- New key `msg.info.recent_raid_costumes` (header, e.g. "Recently-seen raid costumes:"). Reuses existing `costume_{id}`, `arg.prefix.costume`, `msg.no_costume`.

## Testing
- Matcher: costume 9000 matches all; `costume:1` only costume 1; `costume:0` only non-costumed; independent of form/evolution/level.
- Command: `!raid pikachu costume:holiday_2016` stores 1 (single + multi-form paths); `costume:0` → 0; remove by costume; rowtext shows it.
- v1 API: absent → 9000; 0 → 0; 5 → 5; idempotent re-POST (no dup row).
- v2 API: round-trip of Costume (present / null=wildcard / 0).
- Recency: `RecordRaidCostume`/`RecentRaidCostumes` record + retrieve, skip ≤0, per-species, window; separate from spawn `RecentCostumes`.
- `/raid costume` autocomplete: boosts recent raid costumes first (proves boost, not alphabetical); no pokemon → flat list.
- `!info <pokemon>`: raid-costume section appears after `RecordRaidCostume`, absent when empty, distinct from the spawn section.

## Out of scope / deferred
- Costume on egg tracking (eggs have no boss).
- Costume filtering on other types beyond pokemon (done) and raid (this).
- Negative/exclusion syntax beyond `costume:0`.
- Display changes (already shipped).

## Affected files (reference)
`db/migrations/` + `db/raids.go` + `db/tracking_queries.go`, `matching/raid.go`
+ `cmd/processor/raid.go`, `bot/commands/raid.go` + `bot/commands/*` (remove path),
`rowtext/raid.go`, `tracker/recent_activity.go`, `bot/commands/info.go`,
`discordbot/slash/{definitions,mappers/track or raid,dispatcher,autocomplete}`,
`api/trackingRaid.go`, `api/v2_raid.go` (+ OpenAPI golden), `i18n/locale/en.json`.
