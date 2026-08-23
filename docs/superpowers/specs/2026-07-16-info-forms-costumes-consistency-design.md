# `!info` Forms & Costumes Consistency — Design

Status: draft / for review
Date: 2026-07-16
Builds on: pokemon costume, recency, and raid-costume features (raid costume recency + `!info` raid-costume section already shipped).

## Summary

Make the `!info <pokemon>` forms/costumes surface consistent and less cluttered:
1. **Symmetric recency** — add a separate raid-**forms** recency tracker + section, so `!info` shows four parallel recency sections: recently-seen forms, raid forms, costumes, raid costumes.
2. **Copy-pasteable costumes** — render the costume recency sections as `pokemon costume:<name>` (like the form sections), so they paste straight into `!track`/`!raid`.
3. **Truncate the long roster + reveal subcommands** — the big "available forms" roster truncates at 10 inline with a hint; `!info <pokemon> forms` shows the full roster and `!info <pokemon> costumes` shows the full recently-seen costumes.

## Background

Current `!info <pokemon>` (`bot/commands/info.go` `pokemonInfo`) renders, in order:
- Recently-seen forms (spawn) — copy-pasteable `pokemon form:<name>` (`availableRecentForms`)
- Recently-seen costumes (spawn) — `id — name` (`availableCostumes`)
- Recently-seen raid costumes — `id — name` (`availableRaidCostumes`)
- Available forms (full roster, untruncated) — copy-pasteable `pokemon form:<name>` (`availableForms`)

Gaps vs. the desired consistency:
- No raid-**forms** recency (raid webhooks carry `form`; only spawn forms are tracked).
- Costume sections use `id — name`, not the copy-pasteable format forms use.
- The available-forms roster is very long for some species (e.g. Pikachu) and is shown in full.

`!info` subcommands (`!info costumes`, `!info moves`, …) dispatch at the top level by `args[0]`. `!info pikachu forms` currently falls to `pokemonInfo(["pikachu","forms"])`, which ignores `args[1:]`.

## Decisions (confirmed)

| # | Decision |
|---|----------|
| D1 | **Raid-forms recency (separate).** New `RecordRaidForm(pokemonID, form)` / `RecentRaidForms(pokemonID)` bucket (mirror `RecordRaidCostume`/`RecentRaidCostumes`, skip form ≤ 0), fed from the raid handler. A "Recently-seen raid forms" section in `!info`, copy-pasteable `pokemon form:<name>`, placed right after the spawn "Recently-seen forms" section. |
| D2 | **Costume sections copy-pasteable.** Both `availableCostumes` (spawn) and `availableRaidCostumes` render `ctx.Code("<pokemon> costume:<name>")` — the costume name lowercased with spaces→underscores (mirrors `availableRecentForms`'s form format), so a user pastes it into `!track`/`!raid`. Names resolve via the existing `costumeName(ctx, tr, id)` (translate → masterfile fallback); unresolved ids are skipped. |
| D3 | **Section order** in `!info <pokemon>`: recently-seen forms → recently-seen raid forms → recently-seen costumes → recently-seen raid costumes → available forms (roster). Forms grouped, costumes grouped. |
| D4 | **Recency sections shown in full** (bounded by the 6h window). Only the **available-forms roster** truncates. |
| D5 | **Roster truncation.** When `availableForms` has more than **10** entries, `!info <pokemon>` shows the first 10 followed by a hint line: "More than 10 forms — do `!info <pokemon> forms`" (localized). ≤ 10 → shown in full, no hint. |
| D6 | **`!info <pokemon> forms`** — new sub-route (detected in `pokemonInfo` when `args[1]` matches the "forms" subword): shows the recently-seen forms (spawn + raid) **and** the full available-forms roster (untruncated) — a complete "forms for this species" view, no other sections. |
| D7 | **`!info <pokemon> costumes`** — new sub-route (`args[1]` matches the existing "costumes" subword): shows pikachu's **recently-seen costumes, spawn + raid combined and deduped**, copy-pasteable, untruncated. (Costumes have no per-species roster in the data; recency is the only per-species costume data.) |
| D8 | **`/raid form` slash option** — add a `form` option to `/raid` (autocomplete=true), mirroring the just-added `/raid costume`. `mappers/raid.go` emits `form:<val>`; the dispatcher routes `(cmd="raid", opt="form")` to `autocomplete.Form` cascading from the sibling `boss` option, and boosts `RecentRaidForms(pid)` when the field is empty. The text command (`!raid pikachu form:alolan`) already supports form via `applyFormFilter`; this is the slash surface only. |

## Components

### 1. RecentActivity — raid forms (`tracker/recent_activity.go`)
Add, mirroring the raid-costume trio:
- field `raidFormsByPokemon map[int]map[int]time.Time` (+ constructor init)
- `RecordRaidForm(pokemonID, form int)` — no-op when `pokemonID <= 0 || form <= 0`
- `RecentRaidForms(pokemonID int) []int` — via the existing `active()` window
Producer: `RecordRaidForm(raid.PokemonID, raid.Form)` in `cmd/processor/raid.go`, beside the existing `RecordRaidCostume`/`RecordRaidBoss` calls (guarded `form > 0` and `recentActivity != nil`).
Consumed by both the `!info` raid-forms section AND the `/raid form` autocomplete boost (§5).

### 2. `!info` sections (`bot/commands/info.go`)
- **New helper** `availableRecentRaidForms(ctx, pokemonID) []string` — copy-pasteable `pokemon form:<name>` from `RecentRaidForms` (mirror `availableRecentForms`).
- **Rewrite** `availableCostumes` and `availableRaidCostumes` to emit `ctx.Code("<pokemon> costume:<name>")` (copy-pasteable) instead of `id — name`. Extract the shared costume-line formatting so both use it (name resolution + lowercase/underscore).
- **Render order** (D3): spawn forms, raid forms, spawn costumes, raid costumes, then available forms.
- **Roster truncation** (D5): if `len(forms) > 10`, print the first 10 and a hint line; else print all.

### 3. Sub-routing (`bot/commands/info.go` `pokemonInfo`)
After resolving the pokemon from `args[0]`, if `len(args) > 1` and `args[1]` matches the "forms" or "costumes" subword (via the same `tr.T`/`enTr.T` match used at the top level), branch:
- "forms" → render only the full available-forms roster (untruncated) for that species.
- "costumes" → render only the combined recently-seen costumes (spawn + raid, deduped) for that species.
- "forms" → render the recently-seen forms (spawn + raid) followed by the full available-forms roster (untruncated) for that species.
- "costumes" → render the combined recently-seen costumes (spawn + raid, deduped) for that species.
Otherwise render the normal `!info <pokemon>` view.

### 5. `/raid form` slash option (`definitions.go`, `mappers/raid.go`, `dispatcher.go`)
Mirror the `/raid costume` slash work (just shipped):
- `definitions.go` `raidOptions` — add `stringOpt(bundle, "raid.form", "form", "Raid boss form", false, true)` (autocomplete=true).
- `mappers/raid.go` — emit a `form:<val>` token when the form option is set.
- `dispatcher.go` `routeAutocomplete` — add `(cmd="raid", opt="form")`: `base := autocomplete.Form(ctx, deps, siblingOptionString(ic,"boss"), focused, userLang)`, then when `focused == ""` and the boss resolves, `base = autocomplete.PrependRecentForms(base, deps, RecentRaidForms(pid), userLang)`. (Reuses the existing `autocomplete.Form` + `PrependRecentForms` + `ResolvePokemonID`.)
The text command already handles `!raid pikachu form:alolan` via `applyFormFilter` — no command/DB change needed.

### 4. i18n (`i18n/locale/en.json`)
New keys:
- `msg.info.recent_raid_forms` — "**Recently-seen raid forms:**" (header)
- `msg.info.sub.forms` — the "forms" subword (matches `!info <pokemon> forms`)
- `msg.info.more_forms` — the truncation hint, `Tf`-formatted: `"More than {0} forms — do {1} for the full list"` where `{0}` = the cap (10) and `{1}` = the localized `!info <pokemon> forms` command string (inline-code wrapped)
- (reuse `msg.info.sub.costumes` for the "costumes" subword; reuse `msg.info.recent_forms` / `msg.info.available_costumes` / `msg.info.recent_raid_costumes` / `msg.info.available_forms` headers)

## Testing
- RecentActivity: `RecordRaidForm`/`RecentRaidForms` — record + retrieve, skip form ≤ 0, per-species, window; separate from spawn `RecentForms` and from raid costumes.
- `!info <pokemon>`: raid-forms section appears after `RecordRaidForm`, absent when empty; costume sections render `pokemon costume:<name>` (copy-pasteable), not `id — name`; section order is forms → raid forms → costumes → raid costumes → available forms.
- Truncation: species with > 10 available forms shows 10 + the hint; ≤ 10 shows all, no hint.
- Sub-routes: `!info <pokemon> forms` shows recent forms (spawn + raid) + the full roster (more than the truncated inline view); `!info <pokemon> costumes` shows the combined recently-seen costumes; neither collides with the global `!info costumes`/`!info forms` top-level dispatch.
- `/raid form`: mapper emits `form:<val>`; the dispatcher boosts recent raid forms first for the selected boss (proves boost, not alphabetical); no boss → base list.

## Out of scope / deferred
- A global `!info forms` (all forms across all species) — only the per-species `!info <pokemon> forms`.
- Truncating the recency sections (D4: shown in full).
- Egg form/costume (eggs have no boss).

## Affected files (reference)
`tracker/recent_activity.go` (+ test), `cmd/processor/raid.go`, `bot/commands/info.go` (+ tests), `i18n/locale/en.json`, `discordbot/slash/definitions.go` + `mappers/raid.go` + `dispatcher.go` (+ tests) + slash testdata fixtures (`testdata/raid.json`, `parity.yaml`).
