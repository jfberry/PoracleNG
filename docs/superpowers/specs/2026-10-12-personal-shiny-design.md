# Personal shiny and background alerting — design

**Status:** implemented (see plans/2026-10-12-personal-shiny.md)
**Date:** 2026-10-12
**Upstream reference:** UnownHash/Golbat#423 (personal shiny/background
filtering on the v3 scan), pinned at `06c3aba` until it merges.

## Problem

In Pokémon GO, whether a wild spawn is shiny (and whether it carries an
event background) is decided per player: the game seeds a
`java.util.Random` from SHA-1 of the encounter ID and the player ID, and
draws against the variant's current rate. The same spawn can be shiny for
one account and not another.

Golbat#423 reproduces that roll so maps can show "shiny for *my* accounts"
using rates from an external shiny-rate feed. Poracle has no equivalent:
users cannot ask to be alerted when a spawn is shiny for them, and
templates cannot say so.

Poracle's existing shiny information is also weak. `shinyPossible`,
`shinyStats` and `!info` shiny output are derived from Poracle's own
rolling-window sightings (`tracker/rarity.go`): species-level only, blind
to forms and costumes, unaware of a newly released shiny until it has been
seen, and unable to show event rates.

## Goals

1. Let users link game accounts (player IDs, optionally labelled) to their
   Poracle human.
2. Let pokemon tracking rules require "shiny for one of my accounts" and/or
   "has the special background for one of my accounts".
3. Expose per-user template fields naming which linked accounts the spawn is
   shiny / has a background for.
4. Give the **same answer as Golbat#423** for the same spawn, rates and
   player ID — by copying Golbat's implementation as exactly as possible.
5. Consume the **same rate feed format** as Golbat (`/v1/rates?compact=1`)
   so one rate source serves both.
6. Use that feed, when configured, for `shinyPossible`, a new expected
   `shinyRate`, and `!info`; with no feed configured, behave exactly as
   today.

## Non-goals

- Per-player height/weight (Golbat#424). Bitmask bits are reserved for it.
- Ditto prediction. Golbat treats Ditto and disguised pokemon as never
  eligible; so does Poracle.
- Verifying that a player ID belongs to the human linking it (see
  "Account linking policy" for the mitigation).
- A shared Go module with Golbat. Golbat's implementation is still
  evolving; we copy and re-sync instead.
- Personal fields on alert types other than pokemon.

## Design overview

```
shiny-rate feed ──(5 min poll)──► personalshiny.Rates (atomic snapshot)
                                        │
player_accounts (MySQL) ──► state.State.Accounts (per human)
                                        │
pokemon webhook ─► ProcessPokemonWebhook ─► Rates.Evaluate(webhook) ─► Evaluation
                                        │
                     human validation: rule.personal bits ─► Evaluation.For(accounts)
                                        │
                     per-user enrichment: personalShiny[Accounts], personalBackground[Accounts]
```

Evaluation runs in-process. Calling Golbat's scan API with `annotate_for`
per webhook was rejected: it adds a network round-trip to every pokemon
alert and makes Golbat a hard dependency of the processor.

## Data model

### `player_accounts` (migration `000010`)

```sql
CREATE TABLE IF NOT EXISTS `player_accounts` (
  `uid`        INT PRIMARY KEY AUTO_INCREMENT,
  `id`         VARCHAR(255) NOT NULL,
  `player_id`  VARBINARY(256) NOT NULL,
  `label`      VARCHAR(64)  NULL,
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY `uniq_id_player` (`id`, `player_id`),
  UNIQUE KEY `uniq_id_label`  (`id`, `label`)
) ENGINE=InnoDB;

ALTER TABLE `monsters` ADD COLUMN `personal` TINYINT NOT NULL DEFAULT 0;
```

- `id` is the human ID. Accounts are per human, **not** per profile. It is
  `VARCHAR(255)` like `humans.id`, because webhook humans are keyed by their
  webhook URL (well over 50 characters). The unique keys therefore need large
  index prefixes, which `humans.id` already requires of every install.
- `player_id` is `VARBINARY` so uniqueness and lookups are case-sensitive and byte-exact under any table collation.
- `player_id` is a case-sensitive string of 1–256 ASCII letters and digits
  (enforced on input only; the roll itself stays byte-for-byte Golbat).
  Uniqueness is per human: the same player ID may be linked to several
  humans (shared household account, a channel tracking a group's
  accounts).
- `label` is optional, unique per human, compared case-insensitively
  (as `user_locations` labels are). NULL labels do not collide.
- At most **50 accounts per human** (Golbat's per-request player cap).
- `player_accounts` is added to `humanOwnedTables` in both
  `db/human_queries.go` and `store/human_sql.go`, so deleting a human
  (including reconciliation `role_check_mode = "delete"`) removes its
  accounts. `disable-user` leaves them, as it leaves tracking rules.

The migration number assumes nothing else claims `000010` first; renumber
at implementation time if needed.

### `monsters.personal` bitmask

`db/personal.go`, mirroring `db/clean.go`:

| Bit | Constant | Meaning |
|---|---|---|
| 1 | `PersonalShiny` | shiny for at least one linked account |
| 2 | `PersonalBackground` | special background for at least one linked account |
| 4, 8 | reserved | per-player XXL / XXS (Golbat#424) |

All set bits must hold (AND). "Shiny OR background" is two rules.
Helpers: `IsPersonalShiny`, `IsPersonalBackground`, `PackPersonal`.

### Store and state

- New `store.PlayerAccountStore` interface (List, Get, Add, Remove,
  SetLabel, Count) with a SQL implementation and a mock. It is deliberately
  separate from `HumanStore`.
- `db.LoadAll` loads all accounts into the state snapshot as
  `map[humanID][]Account` (as `user_locations` are loaded). Matching reads
  them from the immutable state; no DB access on the hot path.
- Account mutations (command or API) call the debounced `triggerReload`.

## The `personalshiny` package

`processor/internal/personalshiny/` is a copy of Golbat's implementation
with a thin adapter. A diff against Golbat should show only the adapter
and the documented deviations.

| Local file | Golbat source | Contents |
|---|---|---|
| javarandom/ (subpackage) | derive/javarandom/ | verbatim copy, both files (package name unchanged), so it diffs clean |
| `rates.go` | `decoder/api_pokemon_shiny.go` | `shinyPrediction`/`cutoff`, `shinyRate`, `shinyRates`, species maxima, refresh loop |
| `roll.go` | `decoder/api_pokemon_shiny.go` | `shinyPlayer` (`rolls`, `generator`), `shinyCutoff`, `backgroundCutoff`, `backgroundRoll`, `personalLookup`, annotate logic |
| `adapter.go` | — | webhook → Golbat inputs; public API |
| `SYNC.md` | — | mapping, pinned commit, update recipe, deviations |
| `*_test.go` | Golbat tests | copied vectors + adapter tests |

**Copy rules.** Golbat identifiers, comments and control flow are kept.
Every intentional deviation carries a `// Poracle:` comment and is listed in
`SYNC.md`. Package-level globals (`shinyRatesSnapshot`, `config.Config`)
become fields on a struct — the only structural change.

### Public API

```go
type Account struct{ PlayerID, Label string }

type Rates struct{ /* atomic snapshot + refresher */ }
func NewRates(url, token string) *Rates
func (r *Rates) Start(ctx context.Context)
func (r *Rates) Stop()
func (r *Rates) Loaded() bool

// Evaluate resolves variant, cutoffs and eligibility once per webhook.
// Returns nil when rates are not loaded or the pokemon is ineligible.
func (r *Rates) Evaluate(p *webhook.PokemonWebhook) *Evaluation

func (e *Evaluation) For(accounts []db.PlayerAccount) Result
type Result struct{ Shiny, Background []db.PlayerAccount }

type Checker struct{ … }   // per webhook: Evaluation + state humans, memoised per human
func NewChecker(ev *Evaluation, humans map[string]*db.Human) *Checker
func (c *Checker) Allows(humanID string, bits int) bool
func (c *Checker) Result(humanID string) (Result, bool)

// Variant-level lookups for shinyPossible / shinyRate (see below).
func (r *Rates) Variant(pokemonID, form, costume int, lat, lon float64) (v VariantInfo, known bool)
```

### Behaviour (all from Golbat#423)

- **Feed:** `GET <url>/v1/rates?compact=1`, optional
  `Authorization: Bearer <token>`, 10 s timeout, redirects not followed,
  4 MB body limit. Refresh every 5 minutes, or 30 s after a failure. A
  failed refresh keeps the last good snapshot. A response without a `rates`
  list (`{}`, `null`, an error object) is rejected.
- **Fail closed:** until the first successful load, `Evaluate` returns nil,
  so personal rules match nothing. Logged with Golbat's wording.
- **Roll:** seed = first 8 bytes (LE) of SHA-1(LE64(encounter ID) ‖ player
  ID); `java.util.Random`; 24-bit draws compared against
  `ceil(probability × 2²⁴)`.
- **Draw order:** the shiny draw is taken only if the variant can be shiny
  at that location (`supported`, or unknown variant); the background draw
  follows only if a background rate is on offer. So the background is the
  second draw for shiny-capable variants and the first otherwise.
- **Eligibility** (Golbat's `personalLookup`): never Ditto, never
  disguised, never costume 255, only `wild` and `encounter` seen types.
  Poracle's webhook has no `is_ditto` field; Golbat's webhook reports a
  detected Ditto as `pokemon_id` 132 with the disguise in
  `display_pokemon_id`, so the adapter maps Golbat's checks onto
  `pokemon_id == 132` and `display_pokemon_id` set and different from
  `pokemon_id`. Non-numeric encounter IDs are ineligible.
- **Unknown variant:** 1/512 shiny, no background, unless their species is listed in the feed's `unsupportedPokemon` (never shiny: known + unsupported, no shiny draw, so the background draw is the first).
- **Timezone overrides** are resolved per spawn location.
- **Rates are read at processing time**, as Golbat reads them at scan time.

### Known deviation: timezone lookup

Golbat uses `tzf.NewFullFinder()`. Poracle's `geo` package uses
`NewDefaultFinder()` (simplified polygons, ~141 MB less heap) and falls back
to `"UTC"` where Golbat returns `""`. The adapter uses Poracle's finder
**without** the UTC fallback. Within ~111 m of a timezone border the
finders can disagree, so results can differ from Golbat only when the feed
has per-timezone overrides active there. Accepted; documented in `SYNC.md`.

## Matching

- `matching.ProcessedPokemon` gains `Personal *personalshiny.Evaluation`,
  set in `ProcessPokemon` after `ProcessPokemonWebhook` when `[shiny_rates]`
  is configured.
- The personal check is the **last** filter (the only one that hashes). It
  runs inside human validation, after the enabled/profile/area/distance/
  strict-location checks, so only humans the sighting can reach have their
  accounts hashed. Rules with `personal == 0` skip it. A rule with
  `personal != 0` fails if the evaluation is nil, the human has no linked
  accounts, or any required bit has no hitting account.
- `Evaluation.For` is memoised per human per webhook, so several personal
  rules for one human (IV + PVP leagues, etc.) hash once.
- **Not an encounter-only filter.** The roll needs only the encounter ID, so
  personal rules match unencountered wild spawns.
- **Re-sends and reveals.** Each webhook is evaluated afresh; nothing is
  cached per encounter. With the species in the duplicate key (#245), a
  Ditto reveal reaches change detection; it is ineligible, so no personal
  match follows. Re-sends for users who already have a message take the
  existing prior-message (reply/edit) path.

## Enrichment and template fields

`PokemonPerUser` currently returns nil when PVP display is not configured.
The per-user map must exist whenever personal evaluation is active and a
matched user has linked accounts, independent of PVP config.

Personal fields are set for **every matched user with linked accounts**,
whichever rule matched — so an ordinary IV alert can say "shiny for main".

| Field | Type | Meaning |
|---|---|---|
| `personalShiny` | bool | shiny for ≥1 of the recipient's accounts |
| `personalShinyAccounts` | []string | those accounts' labels; unlabelled accounts as `…` + last 4 characters of the player ID |
| `personalBackground` | bool | special background for ≥1 account |
| `personalBackgroundAccounts` | []string | as above |
| `hasLinkedAccounts` | bool | recipient has ≥1 linked account |

Example: `{{#if personalShiny}}✨ Shiny for {{join personalShinyAccounts ", "}}{{/if}}`

Registered in `api/dts_fields.go` and documented in DTS.md. Like other
per-user fields they are empty in `!poracle-test` and editor previews.
`monsterChanged`'s `{{original.*}}` does not carry them (the value cannot
change while the encounter remains eligible).

## Feed-backed `shinyPossible`, `shinyRate` and `!info`

The existing `enrichment.ShinyRateProvider` (species-only, observed) is
replaced by a `ShinyInfo` provider:

```go
ShinyPossible(pokemonID, form, costume int, lat, lon float64) bool
ShinyRate(pokemonID, form, costume int, lat, lon float64) (oneIn int, known bool)
ObservedRate(pokemonID int) float64   // existing shinyStats source
```

**Resolution:**

1. **No `[shiny_rates] url` configured:** exactly today's behaviour — the
   observed-stats provider answers `shinyPossible`; `shinyRate` and
   `shinyRateBoosted` are not set.
2. **URL configured, feed loaded, variant listed:** `shinyPossible` =
   `prediction.supported` (timezone override applied); `shinyRate` =
   `round(1 / probability)`.
3. **URL configured but feed not yet loaded, or variant not listed:** fall
   back to observed stats for `shinyPossible`; `shinyRate` not set.
   (Golbat's "unknown ⇒ 1/512", unless the species is in `unsupportedPokemon`, applies to the personal roll only — using it
   for a "possible" badge would mark unreleased shinies as possible.)

`supported` means the shiny is released, wherever the pokemon is
encountered, so the feed drives `shinyPossible` for **pokemon, raid, max
battle, quest and nest** alike. `shinyPossibleEmoji` and the
`request_shiny_images` icon choice follow it.

**Template fields:**

- `shinyPossible` — as above, now variant-aware.
- `shinyStats` — **unchanged meaning**: observed 1:N ratio.
- `shinyRate` (new) — expected 1:N from the feed at that location. Pokemon,
  quest and nest only: the feed's probability is the wild rate, which is
  wrong for raid and max-battle encounters.
- `shinyRateBoosted` (new) — probability above 1/512 (an event), same
  types as `shinyRate`.

**`!info <pokemon>`:** adds an "Expected shiny rate" line from the feed
(base rate, plus the override active for the user's location timezone)
above the existing observed line. Omitted with no feed.

**`!info shiny`:** adds a "Boosted right now" section listing variants above
1/512 at the user's location, above the existing observed table. Omitted
with no feed.

`/api/stats/shiny` and `/api/stats/shiny-possible` stay observed-only.

## Account linking policy

Ownership of a player ID cannot be verified. With open linking, anyone can
link another player's ID and learn which spawns are shiny for that account.
Operators choose:

```toml
[player_accounts]
self_service = false   # true: users manage their own accounts
                       # false: admins (or the API) only
```

- `self_service = false` (default): add/remove/label are admin-only, or
  delegated channel admins for their channels. Users may always list their
  own accounts.
- Channel, webhook and group humans: account changes are admin-only
  regardless of `self_service`.
- The v2 API requires the API secret and so is trusted; it ignores
  `self_service` (a frontend that verified ownership can link directly).

## Configuration

```toml
[shiny_rates]
url = ""      # shiny-rate feed base URL (same format as Golbat [shiny_rates]); empty = off
token = ""    # optional bearer token

[player_accounts]
self_service = false
```

Both sections go into `config.example.toml` and the config-editor schema.

## Commands

### `!account`

| Form | Who |
|---|---|
| `!account` / `!account list` | anyone, own accounts: name, masked ID, date (bare `!account` also prints the usage line, like `!location`) |
| `!account add <name> <player-id>` / `!account add <player-id>` | self-service or admin; name first like `!location add`; one arg = unnamed account; more than two args = usage error |
| `!account remove <name\|player-id>` | self-service or admin; exact player ID first, then name |
| `!account rename <name\|player-id> [new-name]` | self-service or admin; omit the new name to clear it (replaces the earlier `label` subcommand) |

- **Player IDs must survive parsing verbatim.** `ParsedCommand` gains `VerbatimArgs` (original case, no underscore substitution), copied to `CommandContext.VerbatimArgs`; `!account` reads IDs and names from it. `BuildTarget` strips any argument beginning `user`/`name`, so hosts call `BuildTargetFor`, which for `cmd.account` only recognises `user:<id>` / `name:<name>`.
- Translatable command and message keys in `en.json` and the locale files.
- Slash command `/account` with `list|add|remove|rename` subcommands (`add`: required `player_id`, optional `name`). Renaming for `!location`/`!profile` is tracked in #256.

### `!track`

- New keywords `personalshiny` / `personalbackground`
  (`arg.personalshiny`, `arg.personalbackground`) set the bits.
- The existing bare `shiny` keyword (a no-op on `!track`, used by `!quest`)
  is unchanged.
- Either keyword counts as a narrowing filter for the non-admin
  `!track everything` check, so `!track everything personalshiny` is
  allowed.
- Creating a personal rule with no linked accounts succeeds with a warning
  pointing at `!account add`.
- `/track` gains `personal-shiny` and `personal-background` options; the
  slash mapper forwards them.
- `rowtext/monster.go` (and so `!tracked` and API descriptions) appends
  "personal shiny" / "personal background".

## API

### v2 tracking

`v2_pokemon.go` gains `personal_shiny` and `personal_background`
(`*bool`, omitempty, returned as null when false), packed via
`packPersonal` alongside `packClean`. Full-replace semantics like other
filters. OpenAPI golden regenerated.

### v1 tracking (frozen; additive field)

- `GET` returns the raw `personal` column.
- `POST` accepts optional `personal` (`flexInt`). On an update of an
  existing rule (by uid) where `personal` is **omitted, the stored value is
  kept**, so older clients editing other fields do not strip it. New rules
  default to 0.

### v2 accounts (`api/v2_accounts.go`)

```
GET    /api/v2/humans/{id}/accounts                → [{player_id, label, created_at}]
POST   /api/v2/humans/{id}/accounts                {player_id, label?} → 201
PUT    /api/v2/humans/{id}/accounts/{player_id}    {label}
DELETE /api/v2/humans/{id}/accounts/{player_id}
```

- 409 on duplicate player ID or label; 422 on invalid ID/label or over 50
  accounts; 404 on unknown human or account. `player_id` is 1–256 ASCII
  letters/digits, so it needs no URL escaping.
- The v2 full tracking snapshot gains an `accounts` array.

### Other surfaces

- `!poracle-admin status`: "shiny rates" line — loaded, variant count, age
  of last refresh, last error.
- `/health` capabilities: `personalShiny: true`.
- Docs: API.md, DTS.md, CLAUDE.md (short section), CHANGELOG.

## Error handling

| Situation | Behaviour |
|---|---|
| Feed never loads | personal rules match nothing; `shinyPossible` uses observed stats; warning logged; shown in admin status |
| Bad feed response | refresh rejected, last snapshot kept; invalid per-variant probabilities use Golbat's fallbacks |
| Invalid account input | command error / v2 409 or 422 |
| Personal rule, no accounts | rule created with a warning; never matches until an account is linked |
| Non-numeric encounter ID | ineligible |

## Testing

- **Copied Golbat tests, unchanged:** roll vectors, draw order, missing
  rates list rejection; per-spawn zone test adapted to Poracle's finder.
- **Parity:** a table of (webhook, rates, accounts) built from Golbat's
  fixtures; `Evaluation.For` must return the accounts Golbat's `annotate`
  would, including a background-only variant where the background is the
  first draw.
- **Eligibility:** Ditto, disguised, nearby-only and unknown-variant cases;
  a disguise → Ditto-reveal sequence yields no personal match after the
  reveal.
- **Matcher:** each bit alone and combined; no accounts; feed not loaded;
  one hash per human across several rules; unencountered spawns match.
- **Enrichment:** personal fields present without PVP display config;
  labels and masked IDs; users without accounts get none.
- **ShinyInfo:** no URL ⇒ output identical to today's for existing
  fixtures; listed vs unlisted variant; timezone override changing
  `shinyRate` and `shinyRateBoosted`; raid/maxbattle get no `shinyRate`.
- **API/commands:** v1 keeps `personal` when omitted; v2 accounts CRUD and
  limits; `self_service` on/off; admin `user:` override; player IDs keep
  case and underscores; `!track everything personalshiny` passes the
  narrowing check; rowtext.
- **Delete cascade:** deleting a human removes its `player_accounts` rows.
- Pre-commit gate: `go build`, `go vet`, `go test -count=1`,
  `golangci-lint run`.

## Keeping in sync with Golbat

`personalshiny/SYNC.md` records:

- upstream PR and pinned commit (re-pin to the merge commit once
  Golbat#423 merges);
- the file mapping above;
- deviations: struct instead of globals, Poracle's timezone finder, the
  adapter, and the feed-backed `shinyPossible`/`shinyRate` (Poracle only);
- the update recipe:
  `git diff <pinned>..main -- decoder/api_pokemon_shiny.go derive/javarandom docs/personal-shinies.md`,
  port changes, re-copy test vectors, bump the pin.

## Sequencing

Build on `develop`, after #254 (exempt-from-area-security) merges. #254
rewrites `api/v2_humans.go`, `store/human*.go`, the reconcilers, config,
the config schema and the OpenAPI golden file. This design keeps clear of
those where it can (separate account store, `v2_accounts.go`, cascade via
`humanOwnedTables`); the remaining overlaps are the one-line snapshot
addition in `v2_humans.go`, config/schema additions, and a regenerated
golden file. The `personalshiny` package, rates feed, migration and matcher
hook can be built before #254 lands.
