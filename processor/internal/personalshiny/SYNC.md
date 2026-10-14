# Keeping personalshiny in sync with Golbat

This package copies Golbat's personal shiny implementation so Poracle's
alerts agree with a Golbat-backed map for the same spawn, rates and player.

- **Upstream:** UnownHash/Golbat#423 — "Add personal shiny/background filtering and annotation to the pokemon v3 scan"
- **Pinned commit:** `bec2c23327a205730c8d27cfbfcffb3ca5f82fd1` (PR head "Predict personal shinies for known wild Dittos"; re-pin to the merge commit once #423 merges)

## File mapping

| Poracle | Golbat |
|---|---|
| `javarandom/javarandom.go`, `javarandom/javarandom_test.go` | `derive/javarandom/` — verbatim |
| `rates.go` | `decoder/api_pokemon_shiny.go`: `shinyScale`, `defaultShinyCutoff`, `defaultShinyRatesMaxBytes`, `errShinyRatesTooLarge`, `shinyPrediction`/`cutoff`, `shinyVariant`, `shinyRate`, `shinyCutoffs`, `shinyRates` (incl. `unsupported`, `fallback`, `zoned`), `StartShinyRates`, `refreshShinyRates` (incl. `max_bytes` and the feed `fallback`), `newShinyRates`, `effectiveOverrides` |
| `roll.go` | `decoder/api_pokemon_shiny.go`: `dittoPokemonId`, `dittoShinyCutoff`, `maxPersonalPlayers`, `maxPersonalPlayerIDBytes`, `personalLookup`, `isKnownDitto`, `shinyPlayer` (`rolls`, `dittoRoll`, `generator`), `shinyScan` (`rate`, `zoneAt`, `shinyCutoff`, `backgroundCutoff`), `shinyCutoffs.capable`, `backgroundRoll` |
| `adapter.go` | Poracle only; `Evaluate` + `Evaluation.player` are the two halves of `shinyScan.annotate` (incl. its known-Ditto branch) |
| `roll_test.go`, `adapter_test.go`, `rates_test.go` | `decoder/api_pokemon_shiny_test.go` vectors: `TestPersonalShinyScan` (rolls + annotation), `TestPersonalDrawOrder`, `TestShinyZonePerSpawn`, `TestPersonalDittoShiny` (with `thirdDraw`), `TestShinyCapabilityIgnoresTimezone`, `TestShinyRatesRejectMissingList`, `TestShinyRatesUnsupportedSpecies`, `TestShinyRatesNormalize`, `TestShinyRatesFallback`, `TestShinyRatesMaxBytes`. Driven through `Rates.Evaluate` where Golbat drives `annotate`/`match`; `shinyMaximum` assertions are dropped |

Not copied: the scan request plumbing (`ScanPokemonWithShiny`, `hasPersonal`,
`preparePersonalScan` and its `add` closure, `annotate`'s result wrapper,
`matchClause`, `match`, `matchDitto`, `first`, `shinyMaximum`,
`ErrPersonalRequest`, `ErrShinyRatesNotConfigured`, `ErrShinyRatesNotLoaded`)
and the gRPC surface — Poracle evaluates webhooks, not scan requests.
`newShinyRates` still builds the per-species `maximum` / `backgroundMaximum`
maps (kept for diffing), but nothing in Poracle reads them.

## Intentional differences (each marked `// Poracle:` in the code)

1. Package globals (`shinyRatesSnapshot`, `config.Config.ShinyRates`) are fields on `Rates`; `StartShinyRates` is `(*Rates).Start` with a matching `Stop`; refresh outcomes are recorded for `Status`.
2. Timezone lookup uses `geo.TimezoneName` (tzf **default** finder, no UTC fallback) instead of `tz.SearchTimezone` (tzf **full** finder). Within ~111 m of a zone border the two can disagree, so results can differ only where the feed has per-timezone overrides active there. Accepted.
3. `personalLookup` / `isKnownDitto` read the webhook, not Golbat's Pokemon record. The webhook has no `is_ditto`; Golbat's `setDittoAttributes` sets `IsDitto`, `pokemon_id` 132 and `display_pokemon_id` (the disguise) together and `resetDittoAttributes` clears them together, so a known Ditto is `pokemon_id == 132 && display_pokemon_id != 0`. An undisguised 132 (display 0) stays ineligible, as upstream; Golbat's separate `IsDitto` exclusion is covered by the 132 check. Out-of-range costumes are rejected like costume 255.
4. `maxPersonalPlayers` / `maxPersonalPlayerIDBytes` are exported as `MaxPlayers` / `MaxPlayerIDBytes` and enforced per human by the account store.
5. `[shiny_rates] max_bytes` reaches `refreshShinyRates` as `NewRates`' `maxBytes`, not via `config.Config`; the config loader also defaults it to 16 MiB.
6. The "not loaded yet" warning says "personal shiny alerts" instead of "personal scans".
7. Poracle-only uses of the feed: `ShinyVariant` / `BaseShinyVariant` (override-free variant lookup for `!info`'s base rate) / `ShinyVariantForm` / `BaseShinyVariantForm` (the same lookups, retrying a form-0 miss under the species' masterfile default/NORMAL form, since the game keys many species' normal variant under its own form id) / `Boosted` (shinyPossible, shinyRate, `!info`). `Evaluate` (the personal roll) never uses the default-form fallback: it keeps Golbat's behaviour. These read the normalised cutoffs: `supported` is the base capability (`capable()`, never changed by an override), probability is the cutoff at the location / `shinyScale` (enrichment's `shinyPossible` is `supported && probability > 0`, so the badge follows the location while the roll still takes its draw), and `Boosted` lists capable variants whose cutoff there exceeds the 1/512 cutoff (32768).
8. Unlisted variants are **unknown** to the variant lookups (callers fall back to observed stats for `shinyPossible`); only listed variants and `unsupportedPokemon` species are known. Golbat's `fallback` rate is a roll assumption and is used only by `Evaluate`; as a "shiny possible" badge it would flag unreleased shinies.
9. Ditto in the variant lookups: species 132 reports known, supported, probability 1/64 for every form and costume whatever the feed says (it usually lists 132 in `unsupportedPokemon`), so a Ditto alert shows `shinyPossible` and `shinyRate` 64. Requires a loaded feed like every other lookup. 1/64 is Ditto's normal rate, not an event: `Boosted` never lists 132 (even if the feed lists it above 1/512) and `enrichment.shinyFacts` never sets `shinyRateBoosted` for it (via the exported `DittoPokemonID`, so the two cannot drift).

## Updating

```bash
cd /path/to/Golbat && git fetch origin
git diff bec2c23327a205730c8d27cfbfcffb3ca5f82fd1..origin/main -- \
  decoder/api_pokemon_shiny.go decoder/api_pokemon_shiny_test.go \
  derive/javarandom docs/personal-shinies.md
```

Port each change into the mapped file, re-copy any changed test vectors,
run `go test ./internal/personalshiny/...`, then bump the pinned commit above.
If Golbat changes draw order, eligibility or the feed format, its updated
vectors will fail here until ported.
