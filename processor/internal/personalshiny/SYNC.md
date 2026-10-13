# Keeping personalshiny in sync with Golbat

This package copies Golbat's personal shiny implementation so Poracle's
alerts agree with a Golbat-backed map for the same spawn, rates and player.

- **Upstream:** UnownHash/Golbat#423 — "Add personal shiny/background filtering and annotation to the pokemon v3 scan"
- **Pinned commit:** `06c3aba26b75553ce413e10f7229931ce4f4b81f` (PR head; re-pin to the merge commit once #423 merges)

## File mapping

| Poracle | Golbat |
|---|---|
| `javarandom/javarandom.go`, `javarandom/javarandom_test.go` | `derive/javarandom/` — verbatim |
| `rates.go` | `decoder/api_pokemon_shiny.go`: `shinyScale`, `defaultShinyCutoff`, `shinyPrediction`/`cutoff`, `shinyVariant`, `shinyRate`, `shinyRates` (incl. `unsupported`), `StartShinyRates`, `refreshShinyRates` |
| `roll.go` | `decoder/api_pokemon_shiny.go`: `maxPersonalPlayers`, `maxPersonalPlayerIDBytes`, `personalLookup`, `shinyPlayer` (`rolls`, `generator`), `shinyScan` (`rate`, incl. the `unsupported` species branch, `zoneAt`, `shinyCutoff`, `backgroundCutoff`), `backgroundRoll` |
| `adapter.go` | Poracle only; `Evaluate` + `Evaluation.player` are the two halves of `shinyScan.annotate` |
| `roll_test.go`, `adapter_test.go`, `rates_test.go` | `decoder/api_pokemon_shiny_test.go` vectors: `TestPersonalShinyScan` (rolls + annotation), `TestPersonalDrawOrder`, `TestShinyZonePerSpawn`, `TestShinyRatesRejectMissingList`, `TestShinyRatesUnsupportedSpecies` |

Not copied: the scan request plumbing (`ScanPokemonWithShiny`, `matchClause`,
`match`, `first`, the `add` closure, `ErrPersonalRequest`) — Poracle evaluates
webhooks, not scan requests.

## Intentional differences (each marked `// Poracle:` in the code)

1. Package globals (`shinyRatesSnapshot`, `config.Config.ShinyRates`) are fields on `Rates`; `StartShinyRates` is `(*Rates).Start` with a matching `Stop`; refresh outcomes are recorded for `Status`.
2. `refreshShinyRates`' loop variable `r` is `rt` (the receiver is `r`).
3. Timezone lookup uses `geo.TimezoneName` (tzf **default** finder, no UTC fallback) instead of `tz.SearchTimezone` (tzf **full** finder). Within ~111 m of a zone border the two can disagree, so results can differ only where the feed has per-timezone overrides active there. Accepted.
4. `personalLookup` reads the webhook, not Golbat's Pokemon record. The webhook has no `is_ditto`; a detected Ditto arrives as `pokemon_id` 132 with the disguise in `display_pokemon_id`, which the existing checks cover. Out-of-range costumes are rejected like costume 255.
5. `maxPersonalPlayers` / `maxPersonalPlayerIDBytes` are exported as `MaxPlayers` / `MaxPlayerIDBytes` and enforced per human by the account store.
6. The "not loaded yet" warning says "personal shiny alerts" instead of "personal scans".
7. Poracle-only uses of the feed: `ShinyVariant` / `BaseShinyVariant` (override-free variant lookup for `!info`'s base rate) / `ShinyVariantForm` / `BaseShinyVariantForm` (the same lookups, retrying a form-0 miss under the species' masterfile default/NORMAL form, since the game keys many species' normal variant under its own form id) / `Boosted` (shinyPossible, shinyRate, `!info`). `Evaluate` (the personal roll) never uses the default-form fallback: it keeps Golbat's behaviour.

## Updating

```bash
cd /path/to/Golbat && git fetch origin
git diff 06c3aba26b75553ce413e10f7229931ce4f4b81f..origin/main -- \
  decoder/api_pokemon_shiny.go decoder/api_pokemon_shiny_test.go \
  derive/javarandom docs/personal-shinies.md
```

Port each change into the mapped file, re-copy any changed test vectors,
run `go test ./internal/personalshiny/...`, then bump the pinned commit above.
If Golbat changes draw order, eligibility or the feed format, its updated
vectors will fail here until ported.
