# `!tappable` tracking type — plan

- **Date:** 2026-10-28
- **Branch:** `feat/tappable` (worktree `.worktrees/tappable`, off `origin/develop`)
- **Skill:** `.claude/skills/new-tracking/SKILL.md` (develop version). Section numbers (§N) below refer to it.
- **Golbat source:** `feat/tappable-webhook` branch (`decoder/tappable_webhook.go`, spec `docs/superpowers/specs/2026-10-28-tappable-webhook-design.md`).

## Golbat payload

Envelope `{"type":"tappable","message":{…}}`. Sent once per **new or changed reward** (not per tap). Encounter tappables
are never sent as `tappable`; they arrive as `pokemon` webhooks with `seen_type` `tappable_encounter` /
`tappable_lure_encounter`, so this type has no pokemon-encounter rewards.

```json
{"encounter_id":"16082073574809335024","spawnpoint_id":"476daa23b5b","pokestop_id":"None","pokestop_name":null,
 "latitude":48.16015080292662,"longitude":16.416816693472395,"type":"TAPPABLE_TYPE_BOX",
 "reward_type":"pokemon_candy","item_id":null,"reward_pokemon_id":41,"count":6,
 "rewards":[{"pokemon_candy":41,"count":6}],
 "disappear_time":1793220544,"disappear_time_verified":true,"updated":1793219372}
```

| Field | Notes |
|-------|-------|
| `encounter_id` | decimal string (exceeds 2^53) — keep as string |
| `spawnpoint_id` | hex string, `"None"` when absent |
| `pokestop_id` | fort id string, `"None"` when absent (fort/lure tappables) |
| `pokestop_name` | `null` without a stop, `"Unknown"` if not found |
| `type` | free-form string (`TAPPABLE_TYPE_BOX` is not in the proto enum; also `BREAKFAST`, `MAPLE`, `ROUTE_PIN`) |
| `reward_type`/`item_id`/`reward_pokemon_id`/`count` | describe `rewards[0]` only; nullable |
| `rewards` | array of protojson `LootItemProto` with proto field names: one oneof key + `count`. Keys: `item`, `stardust`, `pokecoin`, `pokemon_candy`, `experience`, `pokemon_egg`, `avatar_template_id`, `sticker_id`, `mega_energy_pokemon_id`, `xl_candy`, `follower_pokemon`, `neutral_avatar_*`, `temp_evo_pokemon_branch` (object, `pokedex_id`). May be null. |
| `disappear_time` | unix; `0` when unknown. Spawn tappables: spawnpoint despawn; fort tappables: tap + 120s |
| `disappear_time_verified`, `updated` | |

Sample (`webhooks.log`, 81 s): 1,971 hooks, all `TAPPABLE_TYPE_BOX` on spawnpoints, rewards 6× candy (Zubat 41, Pikachu 25,
Sinistea 854) or 1× item 1301 (Rare Candy). ~24/s — high volume, so matcher cost matters (§Cross-cutting hot path).

## Decisions

| # | Question | Answer |
|---|----------|--------|
| names | type / table / wire / DTS / maptype | all `tappable`. Config flag `disable_tappable`. `metrics.TypeTappable = "tappable"`. |
| 1 | Webhook | New Golbat webhook, new table. |
| 2 | Table | New `tappable` table (migration `000010_add_tappable`). |
| 3 | v1 API | **No.** v2 only. |
| 4 | Pokemon filter | Via the reward (candy / XL candy / mega energy pokemon), quest-style `reward` column; 0 = any. |
| 5 | Form/costume | No. |
| 6 | Numeric ranges | Min amount only (quest `amount` / stardust+pokecoin min in `reward`). |
| 7 | List filters | No. |
| 8 | Expiry | Yes — `disappear_time`. Drop the event if it's already in the past. `0` = unknown (no TTH fields; clean uses the default TTH path other unknown-expiry types use). |
| 9 | Edit | **No.** Add to `v2EditUnsupported`, `rejectEdit` in Translate. Not in `default_edit`. |
| 10 | Clean | **Yes** (bit 1). |
| 11 | Multi-rule metadata | No — dedup by user as quest does. |
| 12 | POI | No POI filter column. `mute.Event.PokestopID` set when `pokestop_id != "None"`. |
| 13 | Tile | **Yes** — `poracle-tappable.json` for rampardos (+ `.testdata.json`), swifttileserver, swifttileserver-night; `case "tappable":` in the enrichment static-map key list (lat/lon + reward `imgUrl`). No new fallback image (use `Fallbacks.ImgURL`). |
| 14 | Repeat firing | Golbat already sends only on new/changed reward. Dedup key `encounter_id + reward signature (type,item,pokemon,count of rewards[0])`, TTL to disappear time (or a short default when 0). |
| 15 | Translation | Yes — `TappableTranslate`: reward names (item / candy / XL candy / mega energy / stardust / pokecoin), tappable type name. |
| 16 | Command security | Yes — `tappable` in `BlockedAlerts` / `commandSecurityName`. |
| 17 | Recent-activity boost | No. |
| 18 | NLP | No. |
| slash | `/tappable` + `/untrack tappable` | **Yes.** |
| summary | Digest delivery | No. |
| capability | `/health` capabilities | Add `tappable: true` so clients can feature-detect. |

### Reward model (mirrors quest)

Columns `reward_type INT NOT NULL DEFAULT 0`, `reward INT NOT NULL DEFAULT 0`, `amount INT NOT NULL DEFAULT 0`. Reward type
ids are quest's (`QuestRewardProto.Type`) so rowtext, translations and icons are shared:

| Golbat `rewards[]` key | reward_type | `reward` column | amount |
|------------------------|-------------|-----------------|--------|
| `item` | 2 | item id (0 = any item) | min count (`amount`) |
| `stardust` | 3 | min amount (quest quirk) | — |
| `pokemon_candy` | 4 | pokemon id (0 = any) | min count |
| `pokecoin` | 8 | min amount (quest quirk) | — |
| `xl_candy` | 9 | pokemon id (0 = any) | min count |
| `mega_energy_pokemon_id`, `temp_evo_pokemon_branch` (`pokedex_id`) | 12 | pokemon id (0 = any) | min count |
| anything else | not trackable (still enriched/rendered when another reward matches) | | |

A rule matches if **any** entry of `rewards` matches (quest semantics). If `rewards` is null, synthesise one entry from the
top-level `reward_type`/`item_id`/`reward_pokemon_id`/`count`. Parse once per event in the handler, not per rule.

### Command grammar (`!tappable`)

Mirrors `!quest` minus pokemon-encounter, shiny, form, summary and edit:
`stardust[:N]`, `pokecoin[:N]`, `candy[:pokemon]`, `xlcandy[:pokemon]` (new `arg.prefix.xl_candy` / `arg.xl_candy`
keyword if none exists), `energy[:pokemon]`, item names (reuse `matchItemName`), `all items`, `everything`
(gated like `questBulkAllowed`), `amount:N`, plus the common `d:`/`template:`/`clean`/`location:`/`area:`/`remove`/`id:N`/
`mute`. A **bare pokemon name** (`!tappable zubat`) means candy for that pokemon. `remove` with no reward removes all.

## Task order (each step builds and passes the gate before the next)

1. **Core data** — §1 migration + every table list, §2 DB structs/loader/state/orphans/metrics/admin status, §3 store + `TablesWithUID` + mock stores, squash scenario test.
2. **Matcher** — §4 `matching/tappable.go` + tests (reward types, wildcards, amount mins, any-of-rewards, area/distance/blocked).
3. **Webhook + enrichment** — §5 types/receiver/dedup/handler/main wiring, §6 enrichment (base + translate, icons via `uicons.Reward*Icon`), `enrichment.go` static-map case, `dts/view.go` aliases, `cmd/processor/enrich.go`, §11 mute event.
4. **Templates + test alerts** — §12 `fallbacks/dts.json` (discord + telegram), help topic, `dts_fields.go`; §13 testdata (candy, item, stardust, fort tappable), dtsmap, `test.go`, poracletest, huma doc strings; tile templates (§13 #13).
5. **v2** — §7 `api/v2_tappable.go` + registration ordering, `v2EditUnsupported`, mutes enums, golden regen, tests.
6. **Bot command** — §8 command, conversions, tracked, script (+round-trip), untrack, mute token, info, permissions; §15 i18n; §16 rowtext.
7. **Slash** — §9 definitions, mapper, dispatcher, parity fixtures, listers, snapshots.
8. **Buttons / mutes** — §10 unsubscribe `alertTypeTable` + case (default branch deletes maxbattle!), §11 `ruleTables`.
9. **Config** — §14 `disable_tappable` (struct, example, schema, `hookTypes`, four gates), command_security comment, static_map_type/tileserver_settings maptype lists; capability flag.
10. **Docs** — §17 DTS.md, v2-api-design.md, API.md, README, TILESERVER.md, slash README, CHANGELOG, CLAUDE.md type lists.
11. **Done** — §18 gate, type-enumerating test grep, sweep diff vs maxbattle, cross-cutting checks (bot vs v2 identical row test, permissions, bad payloads: `"None"` ids, null rewards, unknown reward key, past expiry, `disappear_time` 0).
