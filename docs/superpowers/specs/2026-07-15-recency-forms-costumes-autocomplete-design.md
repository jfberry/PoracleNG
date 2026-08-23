# Recency-Aware Form & Costume Surfaces — Design

Status: draft / for review
Date: 2026-07-15
Depends on: `feature/costume-tracking` (PR #162) — reuses `RecentActivity.RecentCostumes` and the costume autocomplete added there.

## Summary

Make the `/track` form and costume pickers, and `!info <pokemon>`, surface what
is **actually spawning now** instead of a static alphabetical list:

1. `/track costume` autocomplete boosts the chosen pokemon's **recently-seen
   costumes** to the top.
2. `RecentActivity` gains a **per-species recent-forms** dimension (mirror of the
   existing recent-costumes one), fed from the pokemon webhook handler.
3. `/track form` autocomplete boosts the chosen pokemon's **recently-seen forms**
   to the top.
4. `!info <pokemon>` gains a **"recently seen forms"** section, parallel to the
   existing "recently seen costumes" section.

## Background

The project already has a mature recency-boost pattern for autocomplete:
`tracker.RecentActivity` keeps 6-hour rolling buckets (`ActiveRaidBosses`,
`ActiveQuestItems`, `ActiveInvasionGrunts`, …), and the dispatcher prepends them
via `PrependActivePokemon` / `PrependActiveItems` / `PrependActiveGrunts`
(`internal/discordbot/slash/autocomplete/recent_activity_boost.go`). Each boost:
prepends the recent entries (cap 10), dedups against the base list, hard-caps at
Discord's 25 choices, and only fires when the focused option is empty.

The costume work added a **two-level, per-species** recency bucket —
`costumesByPokemon map[int]map[int]time.Time` with `RecordCostume(pokemonID,
costume)` / `RecentCostumes(pokemonID)` — plus `availableCostumes` in `!info`.
This design extends that shape to forms and wires both into the two pickers.

Established facts (verified):
- Autocomplete providers receive `deps *bot.BotDeps` (has `GameData`,
  `Translations`, `RecentActivity`) and the raw `*discordgo.InteractionCreate`.
- The dispatcher already reads the sibling `pokemon` option for the form case via
  `siblingOptionString(ic, "pokemon")` and passes it to `autocomplete.Form`.
- `autocomplete.Form` is species-scoped and **skips form 0** (the "any form"
  placeholder). `autocomplete.Costume` is a flat global list and **keeps id 0**
  ("no costume") as a real choice.
- `RecordCostume` skips costume ≤ 0; `RecentCostumes` returns id list for a
  species. Producer is wired in `cmd/processor/pokemon.go` beside stats.

## Decisions

| # | Decision |
|---|----------|
| D1 | **Reuse the 6h boost pattern verbatim** — boost cap 10, total cap 25, dedup, only on empty focused. No new tunables. |
| D2 | **Costume recency is per-species** — `/track costume` boosts `RecentCostumes(pokemonID)` for the sibling-selected pokemon. If no pokemon is selected yet, the picker stays the current flat global list (recency needs a species key). The base list is unchanged; recency only reorders/prepends. |
| D3 | **New per-species recent-forms bucket** — `formsByPokemon map[int]map[int]time.Time`, `RecordForm(pokemonID, form)`, `RecentForms(pokemonID)`, mirroring the costume methods 1:1 (same mutex, same `active()` window, same lazy inner-map init). |
| D4 | **`RecordForm` skips form ≤ 0.** Form 0 is the "any form" placeholder — the form picker already omits it and it isn't a trackable value, so recording it would surface a non-choice. (Mirrors `RecordCostume`'s skip of ≤ 0.) |
| D5 | **Producer** — `RecordForm(pokemon.PokemonID, pokemon.Form)` called in `cmd/processor/pokemon.go` immediately beside the existing `RecordCostume`, on every processed pokemon webhook. |
| D6 | **`/track form` boost** — when a pokemon is selected and the focused text is empty, prepend `RecentForms(pokemonID)` above `autocomplete.Form`'s alphabetical list. Recent forms are always a subset of that species' forms, so dedup keeps them from repeating. |
| D7 | **`!info` recent forms — its own section.** Add a "recently seen forms" section listing `id — name` from `RecentForms(pokemonID)`, directly parallel to the existing "recently seen costumes" section, and **leave the full `availableForms` list intact**. Forms and costumes stay structurally identical in `!info`. |
| D8 | **Value/label contracts unchanged.** Boosted costume choices use the existing `Costume` provider's contract (label = translated costume name, value = id string); boosted form choices use `Form`'s contract (label = translated form name, value = lowercased name). So the text parser downstream sees exactly what it does today. |

## Components

### 1. `RecentActivity` — recent forms (`internal/tracker/recent_activity.go`)
Add, mirroring the costume trio:
- field `formsByPokemon map[int]map[int]time.Time` (init in the constructor)
- `RecordForm(pokemonID, form int)` — no-op when `pokemonID <= 0 || form <= 0`;
  lazily inits the inner map; records under `r.mu`.
- `RecentForms(pokemonID int) []int` — returns the windowed form ids via the
  existing `active()` helper (same 6h TTL, sorted for determinism as
  `RecentCostumes` is).

### 2. Producer (`cmd/processor/pokemon.go`)
Beside the existing `RecordCostume` call, add
`ps.RecentActivity.RecordForm(pokemon.PokemonID, pokemon.Form)` (guarded the same
way — only when RecentActivity is wired). Records the spawn's actual form.

### 3. Costume autocomplete boost
- **Dispatcher** (`dispatcher.go`, `case opt == "costume" && cmd == "track"`):
  read `siblingOptionString(ic, "pokemon")`, resolve to id, and when focused is
  empty prepend `RecentCostumes(pokemonID)`.
- **Boost helper** (`recent_activity_boost.go`): add
  `PrependRecentCostumes(base, deps, costumeIDs []int, userLang)` — resolves each
  id via `costumeLabel` (user lang → English fallback), value = id string, cap
  10 / 25 / dedup, mirroring `PrependActiveItems`.
- The base `autocomplete.Costume` stays flat/global and unchanged.

### 4. Form autocomplete boost
- **Dispatcher** (`case opt == "form" && cmd == "track"`): after building
  `autocomplete.Form(...)`, when focused is empty and a pokemon is resolved,
  prepend `RecentForms(pokemonID)`.
- **Boost helper**: add `PrependRecentForms(base, deps, formIDs []int, userLang)`
  — resolves each id via `formLabel`, value = lowercased name, same cap/dedup
  rules. Skips ids that don't resolve to a named form (defensive).

### 5. `!info` recent forms section (`internal/bot/commands/info.go`)
- `availableForms`-style helper `availableRecentForms(ctx, pokemonID) []string`
  returning `"id — name"` (name via the existing `formName`/`formLabel`
  resolution with masterfile fallback), sourced from `RecentForms(pokemonID)`.
- Render it as a "recently seen forms" section under the new i18n key
  `msg.info.recent_forms`, placed **immediately before** the recently-seen-costumes
  section (matching the approved output order: recent forms → recent costumes →
  available forms). Guard: omit the section when the list is empty or
  `RecentActivity` is nil.

## i18n
One new key in `internal/i18n/locale/en.json`: `msg.info.recent_forms` (header
for the recent-forms section, parallel to the costume section's
`msg.info.available_costumes`). No new keys needed for autocomplete (labels come
from existing form/costume translations).

## Testing
- `RecentActivity`: `RecordForm`/`RecentForms` — record + retrieve, skip form ≤ 0,
  per-species isolation, recency window (mirror the costume test).
- `PrependRecentCostumes` / `PrependRecentForms`: recent-first ordering, dedup
  against base, 10/25 caps, empty-input passthrough, nil-deps safety (mirror the
  existing boost tests).
- Dispatcher routing: costume case now reads the sibling pokemon; form case still
  cascades; both only boost on empty focused (unit-test the boost decision, or
  assert via the provider given a stubbed RecentActivity).
- `!info`: recent-forms section appears after `RecordForm`, absent when empty.

## Out of scope / deferred
- Recency for other pickers (raid boss forms, etc.) — pokemon `/track` only.
- Persisting recency across restart (all RecentActivity buckets are in-memory by
  design; resets on restart, self-heals within 6h).
- Weighting/ordering recent entries by frequency — recency (last-seen) only,
  matching every existing boost.

## Affected files (reference)
`internal/tracker/recent_activity.go` (+ test), `cmd/processor/pokemon.go`,
`internal/discordbot/slash/autocomplete/recent_activity_boost.go` (+ test),
`internal/discordbot/slash/dispatcher.go`, `internal/bot/commands/info.go`,
`internal/i18n/locale/en.json`.
