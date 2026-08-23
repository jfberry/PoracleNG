# Tracking API — Per-field Canonical-type Audit

**Purpose**: drive the huma migration for all 10 tracking-rule POST endpoints.
Every field was verified against the actual Go source (`trackingXxx.go` handler,
`commands/xxx.go` bot keywords, `db/migrations/*.sql` schema). No field was guessed.

**Principle**: model the caller's mental model, stay lenient about legacy forms,
keep the stored DB value/semantics unchanged.

---

## Common fields (present on every type)

| field | json | Go type | DB type | semantics class | server default | canonical wire form | lenient-accepts | required? | notes |
|---|---|---|---|---|---|---|---|---|---|
| `uid` | `uid` | `flexInt` | auto-increment, omit on insert | id | — | integer | string, bool | no | present means update; absent means insert |
| `profile_no` | `profile_no` | `flexInt` | `int(11) NOT NULL DEFAULT 1` | genuine-int | query param profileNo | integer | string, bool | no | falls back to active profile if omitted |
| `distance` | `distance` | `flexInt` | `int(11) NOT NULL` | genuine-int (metres, 0=area-based) | 0 | integer | string, bool | no | capped at 40 000 000 |
| `template` | `template` | `any` | `text DEFAULT NULL` | string/id | server default (config `default_template_name`) | string | numeric (coerced to string), omit | no | empty string or omitted → server default |
| `clean` | `clean` | `flexBool` | `tinyint(1) NOT NULL DEFAULT 0` | bitmask (bit1=clean, bit2=edit, bit4=summary) | 0 | boolean (bit1 only) | integer bitmask 0–7, string | no | see Special representations; huma adds `edit` and `summary` boolean siblings |
| `ping` | `ping` | not in insert struct (always set to `""`) | `text NOT NULL` | string | `""` | — | — | no | server-managed; callers do not send this |
| `override_location_label` | `override_location_label` | `string` | `VARCHAR(64) NULL` (migration 4) | string/id (saved-location label) | `""` (null) | string | — | no | mutually exclusive with `override_areas`; requires `distance > 0` |
| `override_areas` | `override_areas` | `[]string` | `TEXT NULL` (migration 4, stored as JSON array) | list | nil (null) | array of strings | — | no | mutually exclusive with `override_location_label` and `distance > 0` |

---

## Pokemon (`monsters` table)

Request struct: `monsterInsertRequest` (gin) / `monsterRuleRequest` (huma, already migrated).

| field | json | Go type | DB type | semantics class | server default | canonical wire form | lenient-accepts | required? | notes |
|---|---|---|---|---|---|---|---|---|---|
| `pokemon_id` | `pokemon_id` | `flexInt` | `int(11) NOT NULL` | genuine-int (Pokédex ID) | — | integer | string, bool | **yes** | handler returns 400 if absent |
| `form` | `form` | `flexInt` | `int(11) NOT NULL` | genuine-int (form ID, 0=any) | 0 | integer | string, bool | no | |
| `min_iv` | `min_iv` | `flexInt` | `int(11) NOT NULL` | genuine-int (−1–100, −1=no lower bound) | −1 | integer | string, bool | no | |
| `max_iv` | `max_iv` | `flexInt` | `int(11) NOT NULL` | genuine-int (0–100) | 100 | integer | string, bool | no | |
| `min_cp` | `min_cp` | `flexInt` | `int(11) NOT NULL` | genuine-int | 0 | integer | string, bool | no | |
| `max_cp` | `max_cp` | `flexInt` | `int(11) NOT NULL` | genuine-int | 9000 | integer | string, bool | no | |
| `min_level` | `min_level` | `flexInt` | `int(11) NOT NULL` | genuine-int (0–55) | 0 | integer | string, bool | no | |
| `max_level` | `max_level` | `flexInt` | `int(11) NOT NULL` | genuine-int (0–55) | 55 | integer | string, bool | no | |
| `atk` | `atk` | `flexInt` | `int(11) NOT NULL` | genuine-int (0–15) | 0 | integer | string, bool | no | minimum ATK IV |
| `def` | `def` | `flexInt` | `int(11) NOT NULL` | genuine-int (0–15) | 0 | integer | string, bool | no | minimum DEF IV |
| `sta` | `sta` | `flexInt` | `int(11) NOT NULL` | genuine-int (0–15) | 0 | integer | string, bool | no | minimum STA IV |
| `max_atk` | `max_atk` | `flexInt` | `int(11) NOT NULL` | genuine-int (0–15) | 15 | integer | string, bool | no | |
| `max_def` | `max_def` | `flexInt` | `int(11) NOT NULL` | genuine-int (0–15) | 15 | integer | string, bool | no | |
| `max_sta` | `max_sta` | `flexInt` | `int(11) NOT NULL` | genuine-int (0–15) | 15 | integer | string, bool | no | |
| `gender` | `gender` | `flexInt` | `int(11) NOT NULL` | enum 0=any / 1=male / 2=female / 3=genderless | 0 | integer (or string "any"\|"male"\|"female"\|"genderless") | bool | no | see Special representations |
| `min_weight` | `min_weight` | `flexInt` | `int(11) NOT NULL` | genuine-int (grams) | 0 | integer | string, bool | no | |
| `max_weight` | `max_weight` | `flexInt` | `int(11) NOT NULL` | genuine-int (grams) | 9 000 000 | integer | string, bool | no | |
| `min_time` | `min_time` | `flexInt` | `int(11) NOT NULL DEFAULT 0` | genuine-int (seconds remaining) | 0 | integer | string, bool | no | |
| `rarity` | `rarity` | `flexInt` | `int(11) NOT NULL DEFAULT −1` | genuine-int (−1=any, 1–6) | −1 | integer | string, bool | no | |
| `max_rarity` | `max_rarity` | `flexInt` | `int(11) NOT NULL DEFAULT 6` | genuine-int (1–6) | 6 | integer | string, bool | no | |
| `size` | `size` | `flexInt` | `int(11) NOT NULL DEFAULT −1` | genuine-int (−1=any, 1–5) | −1 | integer | string, bool | no | |
| `max_size` | `max_size` | `flexInt` | `int(11) NOT NULL DEFAULT 5` | genuine-int (1–5) | 5 | integer | string, bool | no | |
| `pvp_ranking_league` | `pvp_ranking_league` | `flexInt` | `int(11) NOT NULL DEFAULT 0` | enum 0=none / 500=little / 1500=great / 2500=ultra | 0 | integer (or string "none"\|"little"\|"great"\|"ultra") | bool | no | see Special representations; 0 means IV-mode (no PVP) |
| `pvp_ranking_best` | `pvp_ranking_best` | `flexInt` | `int(11) NOT NULL DEFAULT 1` | genuine-int (best/lowest rank to alert on) | 1 | integer | string, bool | no | |
| `pvp_ranking_worst` | `pvp_ranking_worst` | `flexInt` | `int(11) NOT NULL DEFAULT 4096` | genuine-int (worst/highest rank to alert on) | 4096 | integer | string, bool | no | |
| `pvp_ranking_min_cp` | `pvp_ranking_min_cp` | `flexInt` | `int(11) NOT NULL DEFAULT 1` | genuine-int (CP floor) | 0 (handler uses `intValue(0)`) | integer | string, bool | no | DB DEFAULT is 1; handler writes 0 when field omitted — NEEDS DECISION on whether to align |
| `pvp_ranking_cap` | `pvp_ranking_cap` | `flexInt` | `int(11) NOT NULL DEFAULT 0` | genuine-int (level cap; 0=league default) | 0 | integer | string, bool | no | |

---

## Raid (`raid` table)

Request struct: `raidInsertRequest`.

| field | json | Go type | DB type | semantics class | server default | canonical wire form | lenient-accepts | required? | notes |
|---|---|---|---|---|---|---|---|---|---|
| `pokemon_id` | `pokemon_id` | `flexInt` | `int(11) NOT NULL` | genuine-int (Pokédex ID; 9000=any) | 9000 | integer | string, bool | no | 9000 means "track by level, not specific pokemon" |
| `pokemon_form` | `pokemon_form` | `[]pokemonFormPair` | not a DB column; expansion input | list of {pokemon_id, form} objects | — | array of objects | — | no | mutual with pokemon_id + form; produces one row per pair |
| `level` | `level` | `json.RawMessage` | `int(11) NOT NULL` | genuine-int (raid tier; 9000=any) | `[0]` (→ parsed as 9000 when pokemon_id≠9000) | integer or array of integers | — | no | accepts int or `[int,…]` for multi-level expansion |
| `form` | `form` | `flexInt` | `int(11) NOT NULL` | genuine-int (0=any) | 0 | integer | string, bool | no | |
| `team` | `team` | `flexInt` | `int(11) NOT NULL` | enum 0=Harmony / 1=Mystic / 2=Valor / 3=Instinct / 4=any | 4 (clamped to 0–4 else 4) | integer (or string "harmony"\|"mystic"\|"valor"\|"instinct"\|"any") | bool | no | see Special representations |
| `exclusive` | `exclusive` | `flexBool` | `tinyint(1) DEFAULT 0` | genuine-bool (EX-eligible only) | false/0 | boolean | integer (0/1), string | no | stored as IntBool |
| `move` | `move` | `flexInt` | `int(11) NOT NULL DEFAULT 9000` | genuine-int (move ID; 9000=any) | 9000 | integer | string, bool | no | |
| `evolution` | `evolution` | `flexInt` | `int(11) NOT NULL DEFAULT 9000` | genuine-int (evolution ID; 9000=any) | 9000 | integer | string, bool | no | |
| `gym_id` | `gym_id` | `*string` | `varchar(255) DEFAULT NULL` | string/id (gym identifier) | null | string | — | no | null/empty means any gym |
| `rsvp_changes` | `rsvp_changes` | `flexInt` | `tinyint(8) NOT NULL DEFAULT 0` | enum 0=none / 1=rsvp / 2=rsvp_only | 0 (clamped; out-of-range → 0) | string "none"\|"rsvp"\|"rsvp_only" | integer 0–2 | no | see Special representations; bot keywords: `arg.no_rsvp`(0) `arg.rsvp`(1) `arg.rsvp_only`(2) |

---

## Egg (`egg` table)

Request struct: `eggInsertRequest`. Shares all fields except `pokemon_id`, `form`, `move`, `evolution`.

| field | json | Go type | DB type | semantics class | server default | canonical wire form | lenient-accepts | required? | notes |
|---|---|---|---|---|---|---|---|---|---|
| `level` | `level` | `json.RawMessage` | `int(11) NOT NULL` | genuine-int (egg tier; ≥1) | `[0]` → 400 if lvl<1 | integer or array of integers | — | **yes** (must be ≥1) | same multi-level expansion as raid; handler returns 400 if level < 1 |
| `team` | `team` | `flexInt` | `int(11) NOT NULL` | enum 0=Harmony / 1=Mystic / 2=Valor / 3=Instinct / 4=any | 4 | integer (or string) | bool | no | same enum as raid |
| `exclusive` | `exclusive` | `flexBool` | `tinyint(1) DEFAULT 0` | genuine-bool (EX egg) | false/0 | boolean | integer, string | no | |
| `gym_id` | `gym_id` | `*string` | `varchar(255) DEFAULT NULL` | string/id | null | string | — | no | |
| `rsvp_changes` | `rsvp_changes` | `flexInt` | `tinyint(8) NOT NULL DEFAULT 0` | enum 0=none / 1=rsvp / 2=rsvp_only | 0 | string "none"\|"rsvp"\|"rsvp_only" | integer 0–2 | no | same enum + clamping as raid |

---

## Quest (`quest` table)

Request struct: `questInsertRequest`.

| field | json | Go type | DB type | semantics class | server default | canonical wire form | lenient-accepts | required? | notes |
|---|---|---|---|---|---|---|---|---|---|
| `reward_type` | `reward_type` | `flexInt` | `int(11) NOT NULL` | enum 2=item / 3=stardust / 4=candy / 7=pokemon / 12=mega_energy | — | integer (or string "item"\|"stardust"\|"candy"\|"pokemon"\|"mega_energy") | bool | **yes** | handler returns 400 on any value not in {2,3,4,7,12}; see Special representations |
| `reward` | `reward` | `flexInt` | `int(11) NOT NULL` | genuine-int (item ID, pokemon ID, stardust amount; 0=any) | 0 | integer | string, bool | no | semantics depend on reward_type |
| `form` | `form` | `flexInt` | `int(11) NOT NULL DEFAULT 0` | genuine-int (form ID; 0=any) | 0 | integer | string, bool | no | only meaningful when reward_type=7 (pokemon) |
| `shiny` | `shiny` | `flexBool` | `tinyint(1) DEFAULT 0` | genuine-bool | false/0 | boolean | integer, string | no | stored as IntBool |
| `amount` | `amount` | `flexInt` | `int(11) NOT NULL DEFAULT 0` | genuine-int (min amount; 0=any) | 0 | integer | string, bool | no | meaningful for reward_type 2 (item), 4 (candy), 12 (mega_energy); stardust uses `reward` not `amount` |
| `clean` (summary bit) | via `clean` bitmask bit4 | — | same `clean` column | bitmask bit 4 | — | — | — | no | `!quest summary` sets bit4 on `clean`; the huma layer exposes this as a dedicated `summary` boolean sibling (same pattern as pokemon) |

---

## Invasion (`invasion` table)

Request struct: `invasionInsertRequest`.

| field | json | Go type | DB type | semantics class | server default | canonical wire form | lenient-accepts | required? | notes |
|---|---|---|---|---|---|---|---|---|---|
| `grunt_type` | `grunt_type` | `*string` | `varchar(255) NOT NULL` | string/id (canonical grunt-type name: "dragon", "giovanni", "everything", etc.) | — | string | — | **yes** | handler returns 400 if nil or empty; values are lowercased canonical names derived from grunt template strings; "everything" matches all |
| `gender` | `gender` | `flexInt` | `int(11) NOT NULL` | enum 0=any / 1=male / 2=female | 0 | integer (or string "any"\|"male"\|"female") | bool | no | see Special representations; `ParamGender` in bot |

---

## Lure (`lures` table)

Request struct: `lureInsertRequest`.

| field | json | Go type | DB type | semantics class | server default | canonical wire form | lenient-accepts | required? | notes |
|---|---|---|---|---|---|---|---|---|---|
| `lure_id` | `lure_id` | `flexInt` | `int(11) NOT NULL` | enum 0=any / 501–506=specific lure types | — | integer (or string "any"\|"glacial"\|"mossy"\|"rainy"\|"magnetic"\|"golden"\|"sparkly") | bool | **yes** (must be in valid set) | handler returns 400 for unknown IDs; valid: {0, 501, 502, 503, 504, 505, 506}; see Special representations for name mapping |

---

## Nest (`nests` table)

Request struct: `nestInsertRequest`.

| field | json | Go type | DB type | semantics class | server default | canonical wire form | lenient-accepts | required? | notes |
|---|---|---|---|---|---|---|---|---|---|
| `pokemon_id` | `pokemon_id` | `flexInt` | `int(11) NOT NULL` | genuine-int (Pokédex ID; 0=any) | 0 | integer | string, bool | no | 0 means any pokemon |
| `form` | `form` | `flexInt` | `int(11) NOT NULL` | genuine-int (0=any) | 0 | integer | string, bool | no | |
| `min_spawn_avg` | `min_spawn_avg` | `flexInt` | `int(11) NOT NULL` | genuine-int (min hourly spawn rate, 0=any) | 0 | integer | string, bool | no | |

---

## Gym (`gym` table)

Request struct: `gymInsertRequest`.

| field | json | Go type | DB type | semantics class | server default | canonical wire form | lenient-accepts | required? | notes |
|---|---|---|---|---|---|---|---|---|---|
| `team` | `team` | `flexInt` | `int(11) NOT NULL` | enum 0=Harmony / 1=Mystic / 2=Valor / 3=Instinct / 4=any | **no default — required** | integer (or string "harmony"\|"mystic"\|"valor"\|"instinct"\|"any") | bool | **yes** | handler returns 400 if absent (`!req.Team.isSet()`) or out-of-range (0–4) |
| `slot_changes` | `slot_changes` | `flexBool` | `tinyint(1) NOT NULL` (no DB default) | genuine-bool (alert on slot/defender changes) | false/0 | boolean | integer (0/1), string | no | bot keyword `arg.slot_changes` |
| `battle_changes` | `battle_changes` | `flexBool` | `tinyint(1) NOT NULL DEFAULT 0` | genuine-bool (alert on battle start/end) | false/0 | boolean | integer (0/1), string | no | bot keyword `arg.battle_changes`; gated by `Config.Tracking.EnableGymBattle` |
| `gym_id` | `gym_id` | `*string` | `varchar(255) DEFAULT NULL` | string/id (gym identifier) | null | string | — | no | null/empty means any gym; permission-gated via `specificgym` feature |

---

## Fort (`forts` table)

Request struct: `fortInsertRequest`. Note: no `clean` column in `forts` table (schema confirms it is absent).

| field | json | Go type | DB type | semantics class | server default | canonical wire form | lenient-accepts | required? | notes |
|---|---|---|---|---|---|---|---|---|---|
| `fort_type` | `fort_type` | `*string` | `varchar(255) NOT NULL DEFAULT 'everything'` | enum "pokestop" / "gym" / "everything" | "everything" | string "pokestop"\|"gym"\|"everything" | — | no | handler returns 400 for unrecognised values; note: bot command also accepts "station" but the API `validFortTypes` does NOT include "station" — see NEEDS DECISION below |
| `include_empty` | `include_empty` | `flexBool` | `tinyint(1) NOT NULL DEFAULT 1` | genuine-bool (include forts with no edit detail) | DB defaults 1 but handler uses `intValue(0)` → **false** | boolean | integer, string | no | NEEDS DECISION: DB DEFAULT is 1 (true) but handler default is 0 (false); see notes |
| `change_types` | `change_types` | `any` | `varchar(255) NOT NULL DEFAULT '[]'` | list (JSON-encoded array of strings) | `[]` | array of strings | string (passed through), omit | no | stored as JSON string in DB; values: "location", "new", "removal", "image_url", "name", "description"; empty array matches any change type |

---

## Maxbattle (`maxbattle` table)

Request struct: `maxbattleInsertRequest`.

| field | json | Go type | DB type | semantics class | server default | canonical wire form | lenient-accepts | required? | notes |
|---|---|---|---|---|---|---|---|---|---|
| `pokemon_id` | `pokemon_id` | `flexInt` | `int(11) NOT NULL DEFAULT 9000` | genuine-int (Pokédex ID; 9000=by level) | 9000 | integer | string, bool | no | 9000 means "track by level" |
| `level` | `level` | `flexInt` | `int(11) NOT NULL DEFAULT 9000` | genuine-int (max battle tier; 9000=any; 90=all for specific pokemon) | 9000 (or required if pokemon_id=9000) | integer | string, bool | no | handler requires level ≥1 when pokemon_id=9000; 90 used by bot for specific-pokemon "all levels" |
| `form` | `form` | `flexInt` | `int(11) NOT NULL DEFAULT 0` | genuine-int (0=any) | 0 | integer | string, bool | no | |
| `move` | `move` | `flexInt` | `int(11) NOT NULL DEFAULT 9000` | genuine-int (move ID; 9000=any) | 9000 | integer | string, bool | no | |
| `gmax` | `gmax` | `flexBool` | `tinyint(1) NOT NULL DEFAULT 0` | genuine-bool (Gigantamax only) | false/0 | boolean | integer (0/1), string | no | bot keyword `arg.gmax`; stored as int 0/1 |
| `evolution` | `evolution` | `flexInt` | `int(11) NOT NULL DEFAULT 9000` | genuine-int (evolution ID; 9000=any) | 9000 | integer | string, bool | no | |
| `station_id` | `station_id` | `*string` | `varchar(255) DEFAULT NULL` | string/id (power spot station identifier) | null | string | — | no | null/empty means any station |

---

## Special representations

### `clean` — bitmask (all 10 types)

DB column: `tinyint(1) NOT NULL DEFAULT 0` (despite the name "tinyint(1)", the stored range is 0–7).

| bit | integer value | boolean field | meaning |
|-----|---------------|---------------|---------|
| 1 | 1 | `clean` | auto-delete message on TTH expiry |
| 2 | 2 | `edit` | track message for in-place editing (RSVP, etc.) |
| 4 | 4 | `summary` | buffer and group delivery (quest summary scheduler) |

**Canonical wire** (huma new callers): send `"clean": true` (bit1), `"edit": true` (bit2), `"summary": true` (bit4) as separate booleans. Any combination is valid.
**Lenient-accepts** (legacy clients): send an integer `0–7` in `"clean"` (e.g. `"clean": 3` = clean+edit). `collapseClean()` ORs the booleans over the integer.
**Note**: `forts` table has no `clean` column at all; the fort request struct has no `clean`/`edit`/`summary` fields.
**Note**: `quest summary` keyword maps exclusively to bit 4 on the quest `clean` column; it does not use a separate DB field.

---

### `rsvp_changes` — enum (raid and egg)

DB column: `tinyint(8) NOT NULL DEFAULT 0`.

| integer | string name | bot keyword |
|---------|-------------|-------------|
| 0 | `none` | `arg.no_rsvp` (default) |
| 1 | `rsvp` | `arg.rsvp` |
| 2 | `rsvp_only` | `arg.rsvp_only` |

Handler clamps: any value outside 0–2 is silently reset to 0.
**Canonical wire**: string `"none"` \| `"rsvp"` \| `"rsvp_only"`.
**Lenient-accepts**: integer 0, 1, or 2.

---

### `pvp_ranking_league` — enum (pokemon only)

DB column: `int(11) NOT NULL DEFAULT 0`.

| integer | string name | league CP cap |
|---------|-------------|---------------|
| 0 | `none` | n/a (IV mode) |
| 500 | `little` | 500 CP |
| 1500 | `great` | 1500 CP |
| 2500 | `ultra` | 2500 CP |

Note: the stored integer IS the CP cap value, not a sequential index.
**Canonical wire**: string `"none"` \| `"little"` \| `"great"` \| `"ultra"`.
**Lenient-accepts**: integer 0, 500, 1500, or 2500.

---

### `team` — enum (raid, egg, gym)

DB column: `int(11) NOT NULL`.

| integer | string name |
|---------|-------------|
| 0 | `harmony` (grey / no team) |
| 1 | `mystic` (blue) |
| 2 | `valor` (red) |
| 3 | `instinct` (yellow) |
| 4 | `any` |

Raid/egg handler default: 4 (clamped: out-of-range → 4).
Gym handler: **required** (no default; returns 400 if absent or out of range 0–4).
**Canonical wire**: string.
**Lenient-accepts**: integer 0–4.

---

### `gender` — enum (pokemon and invasion)

| integer | string name |
|---------|-------------|
| 0 | `any` |
| 1 | `male` |
| 2 | `female` |
| 3 | `genderless` (pokemon only; invasion uses 0–2) |

**Canonical wire**: string.
**Lenient-accepts**: integer.

---

### `reward_type` — enum (quest only)

| integer | string name |
|---------|-------------|
| 2 | `item` |
| 3 | `stardust` |
| 4 | `candy` |
| 7 | `pokemon` |
| 12 | `mega_energy` |

Handler returns 400 for any value not in this set.
**Canonical wire**: string.
**Lenient-accepts**: integer from the set above.

---

### `lure_id` — enum (lure only)

| integer | string name | note |
|---------|-------------|------|
| 0 | `any` | any lure type |
| 501 | `normal` | ordinary lure |
| 502 | `glacial` | |
| 503 | `mossy` | |
| 504 | `magnetic` | |
| 505 | `rainy` | |
| 506 | `golden` | |

Note: string name for 501 is "normal" based on the item ID. If lure names differ in util.json, the string enum values should be derived from there — **NEEDS DECISION** on exact string names for 501–506. Integer IDs are definitive.
**Canonical wire**: string (or integer).
**Lenient-accepts**: integer from the set above.

---

### `fort_type` — enum (fort only)

Valid values as enforced by the API handler (`validFortTypes`): `"pokestop"`, `"gym"`, `"everything"`.
The bot command also parses a `"station"` keyword (maps to `fortType = "station"`) but the API
handler does NOT include it in `validFortTypes` and will return 400 if sent.
**NEEDS DECISION**: should `"station"` be added to `validFortTypes` to align bot and API behaviour?

---

### `change_types` — JSON-string list (fort only)

Stored as a JSON-encoded string array in a `varchar(255)` column (default `'[]'`).
Valid string values (matching Golbat's `change_type` / `edit_types[]` field names):
`"location"`, `"new"`, `"removal"`, `"image_url"`, `"name"`, `"description"`.
Note: the bot keyword `photo` maps to `"image_url"` (the internal Golbat field name, not the user-facing keyword).
Empty array `[]` means match any change type.
**Canonical wire**: JSON array of strings.
**Lenient-accepts**: a raw JSON string (passed through as-is by the current handler).

---

### `slot_changes` / `battle_changes` — genuine-bool (gym only)

Both are stored as `tinyint(1)` (IntBool). They are independent flags, not a bitmask.
- `slot_changes`: true = alert when a defender is added/removed from a gym slot.
- `battle_changes`: true = alert when a battle starts/ends. Gated by `Config.Tracking.EnableGymBattle`.
**Canonical wire**: boolean.
**Lenient-accepts**: integer (0/1), string.

---

## NEEDS DECISION flags

1. **`fort_type` + `"station"`**: The bot command (`arg.station`) produces `fortType = "station"` and stores it in the DB. The API `validFortTypes` set is `{"pokestop","gym","everything"}` — it returns 400 for `"station"`. The fort matcher uses a simple string compare, so "station" rows in the DB would only match Golbat `fort_update` webhooks whose `fort_type` is literally `"station"`. Decision needed: should "station" be added to `validFortTypes`, or is it intentionally blocked at the API layer?

2. **`include_empty` handler default vs DB default**: The `forts` DB column has `DEFAULT 1` (true), but the handler calls `req.IncludeEmpty.intValue(0)` → default **false** when the field is omitted. New rows inserted without `include_empty` get 0 in the DB even though the schema default is 1. Decision needed: align handler to default true, or update DB schema default to 0?

3. **`pvp_ranking_min_cp` server default**: DB `DEFAULT 1`; handler writes `intValue(0)` → 0 when omitted. Decision needed: should the huma canonical default document 0 or 1?

4. **`lure_id` string names**: The integer→string mapping for lure IDs 501–506 should be confirmed against `resources/data/util.json` lure entries (the canonical UI display names). The table above uses common names but the exact English strings from util.json should be the canonical enum values.

---

## SIGNED-OFF DECISIONS (2026-05-31)

Global modeling: **string enums + lenient legacy int** for all enum fields, **booleans** for all genuine-bool fields, each still accepting the legacy integer/0-1 form via flex coercion. Stored DB values/semantics unchanged. Apply across all 10 types in the fan-out.

Per the NEEDS DECISION items above:

1. **`fort_type` "station" → PRESERVE (do NOT add to the API).** The huma fort endpoint keeps `validFortTypes = {pokestop, gym, everything}` and rejects `station` (422/400), exactly as the gin handler does today. Document the bot-accepts / API-rejects split in the fort endpoint description. **Follow-up:** file a separate issue about reconciling the bot/API/`station` support — NOT part of this migration.

2. **`include_empty` → HONOR DB INTENT (default true).** The huma fort handler must default `include_empty` to **true** when the field is omitted (the DB column is `DEFAULT 1`). This is a deliberate behavior change from the current gin handler (which defaults false). **Requires a changelog/CHANGELOG note** that API clients omitting `include_empty` now get `true`.

3. **`pvp_ranking_min_cp` → PRESERVE (default 0).** Document canonical default 0; no behavior change. (DB `DEFAULT 1` is dead because the handler always writes the column.)

4. **`lure_id` string names → derive from `resources/data/util.json`** during the lure migration; do not hardcode guessed names.

Enum string-value names are derived from the bot keywords / util.json:
- `team`: `harmony|mystic|valor|instinct|any` (0–4)
- `rsvp_changes`: `none|rsvp|rsvp_only` (0–2)
- `gender`: `any|male|female|genderless` (0–3; invasion omits genderless)
- `pvp_ranking_league`: `none|little|great|ultra` (0/500/1500/2500)
- `reward_type`: `item|stardust|candy|pokemon|mega_energy` (2/3/4/7/12)
- `fort_type`: `pokestop|gym|everything`
- `lure_id`: `any` + names-from-util.json (0/501–506)
