# Admin Commands Smoke Test

Run each item against a dev deployment with both Discord and Telegram bots active.
Tick each box when verified. Target time: 30–60 minutes.

**Prerequisites:**
- A registered admin user on both platforms.
- A registered non-admin user (or a second account) for refusal tests.
- At least one active tracking rule (for `!untrack` tests).
- Slash commands enabled in config (`[discord.slash_commands] enabled = true`) for the `slash` group.
- Discord-only subgroups (`slash`, `reconcile`, `emoji`) note the expected refusal text when tested from Telegram.

---

## Foundation

- [ ] `!poracle-admin` (no args, admin caller on Discord) — reply contains "This command is reserved for server administrators" banner and lists all 11 group names (`slash`, `reload`, `emoji`, `reconcile`, `cache`, `ratelimit`, `summary`, `status`, `maintenance`, `config`, `warnings`).
- [ ] `!poracle-admin` (non-admin caller on Discord) — reply: "This command is reserved for administrators." — a text message, NOT a 🙅 react.
- [ ] `!pa` (admin caller) — identical reply to `!poracle-admin` above; alias works.
- [ ] `!pa` (non-admin caller) — same refusal as above.
- [ ] `!poracle-admin unknown_group` (admin caller) — reply contains the "unknown group" message (not an error trace).
- [ ] `!poracle-admin` (no args, admin caller on Telegram) — same help listing as Discord.
- [ ] `!poracle-admin` (non-admin caller on Telegram) — same text refusal.

---

## !untrack type reroute

- [ ] `!untrack raid id:<N>` (where N is a real raid tracking UID) — removes the raid rule and confirms; same result as `!raid remove id:<N>`.
- [ ] `!untrack egg id:<N>` — removes an egg rule; same as `!egg remove id:<N>`.
- [ ] `!untrack invasion grunt:bug` — removes matching invasion rules; same as `!invasion remove grunt:bug`.
- [ ] `!untrack quest id:<N>` — removes a quest rule by UID.
- [ ] `!untrack pikachu iv90` — falls through to existing pokemon-untrack path (NOT treated as a type reroute).
- [ ] `!untrack id:45` — removes by bare UID across all tracking types (existing behaviour unchanged).

---

## !info redirect

- [ ] `!info poracle` (any user) — reply: "→ This view has moved to `!poracle-admin status`" (or equivalent redirect text). Does NOT print status data.
- [ ] `!info config` (admin caller) — reply: "→ This view has moved to `!poracle-admin config`" (or equivalent redirect text). Does NOT print config data.

---

## reload

Run from Discord first, then repeat from Telegram to confirm platform parity.

- [ ] `!poracle-admin reload` (no sub, admin) — shows the `reload` subgroup help listing all three subcommands (`dts`, `geofence`, `state`).
- [ ] `!poracle-admin reload help` — same help listing.
- [ ] `!poracle-admin reload dts` — reply confirms success, includes elapsed-ms and template count (e.g. "Reloaded 47 DTS templates in 23ms").
- [ ] `!poracle-admin reload dts` (Telegram) — same success reply.
- [ ] `!poracle-admin reload geofence` — reply confirms success, includes elapsed-ms, geofence count, and tracking rule count.
- [ ] `!poracle-admin reload geofence` (Telegram) — same success reply.
- [ ] `!poracle-admin reload state` — reply confirms success, includes elapsed-ms, tracking rule count, and active human count.
- [ ] `!poracle-admin reload state` (Telegram) — same success reply.
- [ ] `!poracle-admin reload bogus` — reply: "unknown subcommand" for the `reload` group.

---

## status

- [ ] `!poracle-admin status` (admin, Discord) — reply contains all major sections: Build/uptime, Webhooks, Render queue, Delivery, Discord rate, Telegram rate, Alert limits, Summary buffer, Tracking counts, MySQL.
- [ ] Status shows 🟢 / 🟡 / 🔴 indicators next to relevant sections.
- [ ] Status includes the processor version string.
- [ ] `!poracle-admin status -v` — reply includes additional per-route Discord detail and per-type webhook breakdown.
- [ ] `!poracle-admin status` (Telegram) — same sections; Discord-specific rate fields show "n/a" or equivalent.
- [ ] MySQL ping line shows 🟢 and "ok" when DB is reachable.
- [ ] Tracking section lists per-type counts (pokemon, raid, egg, quest, invasion, lure, nest, gym, fort, maxbattle) and active human count.

---

## maintenance

- [ ] `!poracle-admin maintenance` (no sub, admin) — shows current running/paused state (most useful default).
- [ ] `!poracle-admin maintenance status` — same as no-sub: shows "delivery running" when unpaused.
- [ ] `!poracle-admin maintenance help` — shows the `maintenance` subgroup help text listing `pause`, `resume`, `status`.
- [ ] `!poracle-admin maintenance pause` — confirms pause with "(no reason given)" and note that webhooks still ingest.
- [ ] `!poracle-admin maintenance status` after pause — shows 🔴 paused state with reason and elapsed time.
- [ ] `!poracle-admin maintenance pause DB maintenance` — confirms pause with reason "DB maintenance".
- [ ] `!poracle-admin maintenance pause` when already paused — reply notes already-paused with timestamp and existing reason.
- [ ] During pause: run any command (e.g. `!version` or `!track pikachu`) — bot reply includes the maintenance-active suffix line ("🔧 Maintenance mode is active — alerts are not being delivered.").
- [ ] During pause: confirm no new alerts are delivered to users (send a test webhook; alert should not appear in Discord/Telegram).
- [ ] `!poracle-admin maintenance resume` — confirms resume with how long it was paused and the reason.
- [ ] After resume: run same command (e.g. `!version`) — maintenance suffix is gone from the reply.
- [ ] `!poracle-admin maintenance resume` when not paused — reply: "not currently paused" (or equivalent).
- [ ] After resume: queued messages drain and alerts resume delivery.
- [ ] `!poracle-admin maintenance` (Telegram) — same behaviour as Discord.

---

## slash

This group manages Discord slash command registration. All subcommands must refuse gracefully on Telegram.

- [ ] `!poracle-admin slash` (no sub, Discord admin) — shows the `slash` subgroup help listing all subcommands (`sync`, `force-resync`, `clear-global`, `clear-guild`, `status`).
- [ ] `!poracle-admin slash` (Telegram admin) — reply: "Discord slash commands not available — this command must be run from a deployment with the Discord side enabled." (or equivalent i18n text).
- [ ] `!poracle-admin slash status` (Discord, slash enabled) — shows last sync timestamp + short fingerprint per scope (global + each configured guild). "never synced" for any scope not yet synced.
- [ ] `!poracle-admin slash sync` (Discord, slash enabled) — syncs commands; reply includes elapsed-ms.
- [ ] `!poracle-admin slash force-resync` (Discord) — clears fingerprint cache and re-syncs; reply notes forced resync + elapsed-ms.
- [ ] `!poracle-admin slash clear-global` (Discord) — clears globally-registered commands; reply confirms success.
- [ ] `!poracle-admin slash clear-guild` (Discord, no arg) — reply: argument required (guild ID missing).
- [ ] `!poracle-admin slash clear-guild <guild_id>` (Discord) — clears guild-scoped commands for the specified guild; reply confirms success.
- [ ] After `!poracle-admin slash sync`, open Discord in a test guild — slash commands appear in the command picker within a few seconds (guild-scoped) or up to 1 hour (global).

---

## emoji

Manages the processor's loaded emoji configuration. Discord-specific emojis may show differently on Telegram.

- [ ] `!poracle-admin emoji` (no sub, Discord admin) — shows the `emoji` subgroup help listing `list`, `reload`, `test`.
- [ ] `!poracle-admin emoji list` — shows all configured emoji keys with per-platform resolutions; output chunked if more than Discord's 2000-char limit.
- [ ] `!poracle-admin emoji test emojiWeather` — resolves the `emojiWeather` key for the current platform (Discord or Telegram) and shows the result.
- [ ] `!poracle-admin emoji test nonExistentKey` — reply: "key not found" (or equivalent).
- [ ] `!poracle-admin emoji test` (no key arg) — reply: usage hint.
- [ ] `!poracle-admin emoji reload` — reloads `config/emoji.json`; reply confirms count of keys loaded and elapsed-ms.
- [ ] `!poracle-admin emoji` subcommands (Telegram admin) — all three subcommands work; resolutions show unicode/Telegram-appropriate values.

---

## reconcile

Discord-specific. Must refuse gracefully on Telegram.

- [ ] `!poracle-admin reconcile` (no sub, Discord admin) — shows the `reconcile` subgroup help listing `run` and `user`.
- [ ] `!poracle-admin reconcile` (Telegram admin) — reply: "Discord reconciliation not available on this platform" (or equivalent).
- [ ] `!poracle-admin reconcile run` (Discord, reconciliation enabled) — triggers full `SyncDiscordRole`; reply includes elapsed-ms. Verify in logs that reconciliation ran.
- [ ] `!poracle-admin reconcile run` (Discord, reconciliation not configured) — reply: "reconciliation not configured" (or equivalent).
- [ ] `!poracle-admin reconcile user <valid_discord_id>` (Discord) — reconciles a single user; reply includes user ID and elapsed-ms.
- [ ] `!poracle-admin reconcile user` (no ID arg) — reply: usage hint (user ID required).
- [ ] `!poracle-admin reconcile user <id>` (Telegram) — reply: "not available on this platform" refusal.

---

## cache

- [ ] `!poracle-admin cache` (no sub, admin) — shows the `cache` subgroup help listing `stats` and `clear geocoder`.
- [ ] `!poracle-admin cache stats` (Discord) — shows geocoder cache stats: memory entries, disk entries, hits (memory), hits (disk), misses, hit rate %.
- [ ] `!poracle-admin cache stats` (Telegram) — same stats; numeric values present.
- [ ] `!poracle-admin cache clear geocoder` (Discord) — drops in-memory geocoder cache; reply reports how many entries were cleared.
- [ ] `!poracle-admin cache clear geocoder` (Telegram) — same behaviour.
- [ ] `!poracle-admin cache clear` (no specifier) — reply: usage hint ("did you mean `clear geocoder`?").
- [ ] `!poracle-admin cache bogus` — reply: unknown subcommand for `cache` group.

---

## ratelimit

- [ ] `!poracle-admin ratelimit` (no sub, admin) — shows the `ratelimit` subgroup help listing `list`, `show`, `reset`, `userlist`.
- [ ] `!poracle-admin ratelimit list` when no targets are rate-limited — reply: "no targets currently rate-limited" (positive news, stated clearly).
- [ ] `!poracle-admin ratelimit list` when one or more targets are breached — shows rows with target, bucket (alert/summary), count/limit, and ban-until if applicable.
- [ ] `!poracle-admin ratelimit show <target_id>` — shows both alert and summary bucket detail for the target: count, limit, window timing, violation count, ban-until if applicable.
- [ ] `!poracle-admin ratelimit show` (no target arg) — reply: usage hint.
- [ ] `!poracle-admin ratelimit show <unknown_id>` — reply: "no rate-limit state found for that target".
- [ ] `!poracle-admin ratelimit reset <target_id>` — clears counters; reply confirms + reminds that `admin_disable` is not touched.
- [ ] `!poracle-admin ratelimit reset <unknown_id>` — reply: "no state to reset".
- [ ] `!poracle-admin ratelimit userlist` — re-dispatches to `!userlist disabled`; output is the disabled-user list.
- [ ] All `ratelimit` subcommands (Telegram) — same behaviour; platform parity.

---

## summary

- [ ] `!poracle-admin summary` (no sub, admin) — shows the `summary` subgroup help listing `list`, `show`, `fire`.
- [ ] `!poracle-admin summary list` when no users have buffered quests — reply: "summary buffer is empty".
- [ ] `!poracle-admin summary list` when users have buffered entries — shows rows: user ID, alert type, entry count, next-fire time.
- [ ] `!poracle-admin summary show <user_id>` with buffered entries — shows per-alert-type breakdown with reward-group samples.
- [ ] `!poracle-admin summary show <user_id>` with no entries — reply: "no buffered entries for that user".
- [ ] `!poracle-admin summary show` (no user arg) — reply: usage hint.
- [ ] `!poracle-admin summary fire <user_id>` (quest buffer non-empty) — dispatches the buffer immediately; reply reports how many entries were dispatched. Verify the quest summary message arrives in the user's target channel.
- [ ] `!poracle-admin summary fire <user_id> quest` — same as above with explicit alert type.
- [ ] `!poracle-admin summary fire` (no user arg) — reply: usage hint.
- [ ] All `summary` subcommands (Telegram) — same behaviour.

---

## config

- [ ] `!poracle-admin config` (no sub, admin) — dumps the full effective config with sensitive fields redacted (`***`).
- [ ] Sensitive fields are redacted: `discord.token`, `database.password`, `telegram.token`, `processor.api_secret`, `geocoding.shlink_api_key`.
- [ ] Non-sensitive fields are visible: `discord.admins`, `general.locale`, `processor.port`.
- [ ] `!poracle-admin config discord` — shows only the `[discord]` section; token fields still redacted.
- [ ] `!poracle-admin config keys` — lists all section names with their key counts (e.g. `discord: 12 keys`).
- [ ] `!poracle-admin config bogusSection` — reply: "unknown section" (not a crash).
- [ ] `!poracle-admin config help` — shows the subgroup usage (keys, section, full-dump descriptions).
- [ ] `!poracle-admin config` (Telegram) — same full dump; same redactions.
- [ ] Long config output is chunked into multiple messages (does not hit Discord's 2000-char limit in a single message).

---

## warnings

- [ ] `!poracle-admin warnings` (no sub, admin) — shows "Startup" section (up to 200 entries) and "Recent" section (last 50 rolling entries).
- [ ] Startup section is non-empty immediately after a fresh restart (contains any WARN/ERROR from init).
- [ ] After a clean start with no errors, startup section shows "no startup warnings" (or equivalent empty message).
- [ ] `!poracle-admin warnings startup` — shows only the startup section.
- [ ] `!poracle-admin warnings recent` — shows only the rolling buffer (last 50 WARN/ERROR entries since startup completed).
- [ ] `!poracle-admin warnings clear` — empties the rolling buffer; reply reports how many entries were cleared. Startup buffer is unchanged.
- [ ] After `clear`: `!poracle-admin warnings recent` shows "no recent warnings".
- [ ] `!poracle-admin warnings help` — shows subcommand listing (`startup`, `recent`, `clear`).
- [ ] `!poracle-admin warnings` (Telegram) — same sections and behaviour.
- [ ] Trigger a recoverable WARN (e.g. geocoding with a bad address); confirm the entry appears in `!poracle-admin warnings recent` within seconds.

---

## Cross-cutting

### Non-admin access

- [ ] Non-admin runs `!poracle-admin reload dts` — reply is the "This command is reserved for administrators." text message (not a 🙅 react, not silent).
- [ ] Non-admin runs `!pa status` — same text refusal.

### Maintenance suffix on every command

- [ ] Pause delivery (`!poracle-admin maintenance pause`), then run `!help` — reply includes the maintenance suffix.
- [ ] Pause delivery, then run `!tracked` — reply includes the suffix.
- [ ] Pause delivery, then run `!poracle-admin status` — status header shows 🔴 PAUSED banner with reason and duration; reply also includes the maintenance suffix.
- [ ] Resume delivery (`!poracle-admin maintenance resume`), then run `!help` — suffix is gone.

### Restart behaviour

- [ ] Restart the processor while delivery is paused — after restart, delivery is running (pause does not persist). Confirm by running `!poracle-admin maintenance status`.
