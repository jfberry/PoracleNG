# Admin Commands — Implementation Plan

> **For agentic workers:** Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement task-by-task. Steps use `- [ ]` checkboxes.

**Companion document:** `DESIGN.md` holds the rationale and command reference. Read it first.

**Branch:** `admin-commands` (planned; not yet created).

**Scope:** `!untrack <type>` reroute + `!poracle-admin` umbrella with subgroups `slash`, `reload`, `emoji`, `reconcile`, `cache`, `ratelimit`, `summary`, `status`, `config`, `warnings`, `maintenance`.

---

## File structure

### New files

- `processor/internal/bot/commands/poracle_admin.go` — top-level dispatcher + help.
- `processor/internal/bot/commands/poracle_admin_slash.go` — slash group.
- `processor/internal/bot/commands/poracle_admin_reload.go` — reload group.
- `processor/internal/bot/commands/poracle_admin_emoji.go` — emoji group.
- `processor/internal/bot/commands/poracle_admin_reconcile.go` — reconcile group.
- `processor/internal/bot/commands/poracle_admin_cache.go` — cache group.
- `processor/internal/bot/commands/poracle_admin_ratelimit.go` — ratelimit group.
- `processor/internal/bot/commands/poracle_admin_summary.go` — summary group.
- `processor/internal/bot/commands/poracle_admin_status.go` — status group + shared status helper.
- `processor/internal/bot/commands/poracle_admin_maintenance.go` — maintenance group.
- `processor/internal/bot/commands/poracle_admin_test.go` — unit tests for the dispatcher + each group.
- `processor/internal/webhook/rate.go` — webhook rate counter.
- `processor/internal/webhook/rate_test.go` — rate counter tests.

### Modified files

- `processor/internal/bot/commands/untrack.go` — first-token type reroute.
- `processor/internal/ratelimit/ratelimit.go` — new `ListBlocked`, `StateFor`, `Reset`.
- `processor/internal/delivery/ratelimit.go` — new `Snapshot()`.
- `processor/internal/delivery/dispatcher.go` — new `Pause`, `Resume`, `PauseState`.
- `processor/internal/delivery/queue.go` — pause-aware send loop.
- `processor/internal/geocoding/cache.go` — new `Stats`, `ClearMemory`, hit/miss instrumentation.
- `processor/internal/discordbot/reconciliation.go` — extract single-user reconcile if not already public.
- `processor/internal/webhook/receiver.go` — wire rate counter.
- `processor/internal/bot/commands/info.go` — `!info poracle` calls into the shared status helper.
- `processor/cmd/processor/main.go` — register `PoracleAdminCommand`, wire receiver rate counter into command deps, expose dispatcher/limiter snapshots through `BotDeps`.
- `processor/internal/bot/command.go` — add new fields to `BotDeps` for the introspection surfaces the admin commands need.
- `processor/internal/i18n/locale/en.json` — new `cmd.poracle_admin.*` keys.
- `processor/internal/i18n/locale/de.json` — German equivalents.

---

## Phase 0: Foundation

### Task 0.1: `!untrack <type>` reroute

- [ ] Edit `processor/internal/bot/commands/untrack.go`. At the top of `Run`, if `len(args) >= 1` and `args[0]` is a recognised tracking type (the canonical English short name — `raid`, `egg`, `quest`, `invasion`, `incident`, `lure`, `nest`, `gym`, `fort`, `maxbattle`), look up the target command via `ctx.Registry.Get("cmd." + args[0])` and call its `Run(ctx, append([]string{"remove"}, args[1:]...))`. Return its replies directly.
- [ ] Add a small `validUntrackType(s string) bool` helper next to the reroute or in a shared place. Don't try to be clever about translations — the parser already reverse-translated the token before `cmd.untrack` sees it (text path) or the slash mapper already emitted the canonical token (slash path).
- [ ] Test: existing pokemon-untrack tests must still pass (`!untrack pikachu` falls through unchanged). New tests:
  - `!untrack raid id:12` → calls `cmd.raid` with `["remove", "id:12"]`.
  - `!untrack egg level:5` → calls `cmd.egg` with `["remove", "level:5"]`.
  - `!untrack invasion grunt:bug` → calls `cmd.invasion` with `["remove", "grunt:bug"]`.
- [ ] Update CLAUDE.md's "Key Commands" entry for `!untrack` to mention the new form.

### Task 0.2: `!poracle-admin` skeleton

- [ ] Create `processor/internal/bot/commands/poracle_admin.go` with `PoracleAdminCommand` struct, `Name() = "cmd.poracle_admin"`, `Aliases() = ["cmd.pa"]` (so `!pa` works as a typing shortcut). `Run` checks `bot.IsAdmin`; if not admin → text reply using `cmd.poracle_admin.not_admin` ("This command is reserved for administrators."), NOT a silent 🙅 react. The 🙅 react is reserved for `command_security` role-gated denials.
- [ ] Add `cmd.pa` to the i18n bundle (`slash.cmd.pa` style) and ensure the parser's reverse-translation handles both `poracle-admin` and `pa` as routing to the same command.
- [ ] In `processor/internal/bot/commands/help.go` (or wherever `!help` enumerates commands for users), confirm `cmd.poracle_admin` is filtered out for non-admins — same treatment as `!broadcast`/`!apply`/`!userlist`. If the filter logic is generic, no change needed; if it's an explicit allowlist, add the new command to the admin-only set.
- [ ] Implement subgroup dispatch via a `map[string]subgroup` where each subgroup has its own `Run(ctx, args)` and `Help(ctx)`. Subgroups: `slash`, `reload`, `emoji`, `reconcile`, `cache`, `ratelimit`, `summary`, `status`, `maintenance`. For Phase 0, register all nine groups but leave each one's `Run` as a "not yet implemented" stub returning a `tr.T("cmd.poracle_admin.stub")` reply. Subsequent phases swap in real bodies.
- [ ] `!poracle-admin` with no args → `Help(ctx)` listing groups + one-line descriptions, all i18n-keyed.
- [ ] `!poracle-admin <group>` with no further args → that group's `Help(ctx)`.
- [ ] Register in `processor/cmd/processor/main.go` next to the other `cmdRegistry.Register(&commands.X{})` calls.
- [ ] Add `command_security` mapping in `processor/internal/bot/permissions.go`'s `commandSecurityName()` for `cmd.poracle_admin` → `poracle_admin` (and per-group keys: `poracle_admin.slash`, etc., if we want per-group ACLs — start with just the top-level).
- [ ] Tests: dispatcher routes args correctly; non-admin gets refusal; help renders.

### Task 0.3: i18n keys for skeleton

- [ ] Add to `en.json`: `cmd.poracle_admin`, `cmd.pa`, `cmd.poracle_admin.desc`, `cmd.poracle_admin.help.admin_only` ("This command is reserved for server administrators. Each subgroup performs a live operations task."), `cmd.poracle_admin.help.intro`, `cmd.poracle_admin.help.groups`, `cmd.poracle_admin.stub`, `cmd.poracle_admin.unknown_group`, `cmd.poracle_admin.unknown_sub`, `cmd.poracle_admin.not_admin` ("This command is reserved for administrators."), plus `cmd.poracle_admin.group.<name>.desc` for each of the nine groups.
- [ ] Add German equivalents to `de.json`.

---

## Phase 1: Reload group (cheapest first — wraps existing APIs)

### Task 1.1: `reload` subgroup

- [ ] Implement `poracle_admin_reload.go`. Subcommands: `dts`, `geofence`, `state`, `help`.
- [ ] `dts` calls the same path `/api/dts/reload` uses (factor out the inner function from the HTTP handler so both call sites share it).
- [ ] `geofence` calls `state.LoadWithGeofences()`.
- [ ] `state` calls `state.Load()` — bypassing the 500ms debouncer (call `state.Load()` directly, not `triggerReload()`).
- [ ] Each subcommand returns success/failure reply with elapsed-ms and "what was reloaded" detail (template count, geofence count, tracking row count).
- [ ] Wire required deps through `BotDeps` (likely already has `StateMgr` and a `TemplatesProvider`; add a `ReloadDTS func() error` field rather than passing the templates pointer).
- [ ] Tests: each subcommand exercises happy path against a stub state manager + stub reload functions; verify the reply text mentions the elapsed-ms and what was reloaded.

---

## Phase 2: Introspection APIs (no commands yet — unblocks status + ratelimit + summary + cache + maintenance)

### Task 2.1: Webhook rate counter

- [ ] Create `processor/internal/webhook/rate.go` with `RateCounter` (60-slot ring buffer keyed on `unixMinute%60`, plus a `map[string]int` for per-type 60-min totals, all under one `sync.Mutex`).
- [ ] Public API: `Record(webhookType string)`, `Snapshot() RateSnapshot` (returns Per5/15/60 totals + per-type breakdown for last 60 min).
- [ ] On each `Record`, if the slot's minute differs from now, zero the slot's count and update its minute. Per-type map gets the same minute-rollover treatment (or a separate per-type ring; simpler: just decay per-type counts every minute via a background goroutine, but a passive approach is fine for this scale).
- [ ] Hook into `processor/internal/webhook/receiver.go` — call `Record(item.Type)` in the per-item loop after the type switch.
- [ ] Expose the counter as a method on `*Receiver` or as a standalone field on `ProcessorService` so commands can reach it.
- [ ] Tests: record a known sequence; assert snapshot values for 5/15/60 windows.

### Task 2.2: Ratelimit introspection

- [ ] In `processor/internal/ratelimit/ratelimit.go`, add `TargetState` struct, `ListBlocked()`, `StateFor(id, dtype)`, `Reset(id, dtype) bool`.
- [ ] `ListBlocked` walks both `counters` and `violations` maps under the existing lock, collecting any target whose current count exceeds limit OR whose ban-until is in the future. Return a value-typed slice (no internal map references escape).
- [ ] `Reset` removes the target from both maps and returns whether anything was removed.
- [ ] Tests: seed counters and bans, assert `ListBlocked` finds them; assert `Reset` clears.

### Task 2.3: Discord per-route rate snapshot

- [ ] In `processor/internal/delivery/ratelimit.go`, add `RouteState`, `DiscordRateSnapshot`, and `Snapshot()` method.
- [ ] Snapshot includes: routes where `remaining < limit`, global token-bucket tokens remaining, count of 429s in the last 5 min (requires adding a small ring counter for 429 timestamps).
- [ ] Tests: simulate 429s and partial-quota state; assert snapshot.

### Task 2.4: Telegram rate snapshot

- [ ] In `processor/internal/delivery/telegram.go` (or wherever the Telegram client lives), add a 429-counter equivalent + `TelegramRateSnapshot{ Recent429Count int; CurrentBackoffUntil time.Time }`. If the existing client doesn't track backoff state in a queryable way, add it.
- [ ] Tests: similar to Task 2.3.

### Task 2.5: Geocoder cache stats

- [ ] In `processor/internal/geocoding/cache.go`, add `Stats` struct + `Stats()` + `ClearMemory()`.
- [ ] Add `atomic.Uint64` fields for `hitsMemory`, `hitsDisk`, `misses`; increment inside `Get`.
- [ ] `Stats()` reads counters + `ttlcache.Len()` for memory layer + pogreb's count for disk (whatever the pogreb API exposes — if it doesn't expose count, skip disk count and document why).
- [ ] `ClearMemory()` returns count cleared, deletes all entries from the memory layer only.
- [ ] Tests: instrument a cache, exercise hits/misses, assert stats.

### Task 2.6: Delivery pause primitive

- [ ] In `processor/internal/delivery/dispatcher.go`, add `paused atomic.Bool`, `pauseReason atomic.Pointer[string]`, `pausedSince atomic.Pointer[time.Time]`, `pauseCond *sync.Cond` (with its own mutex).
- [ ] `Pause(reason string)`: sets paused true, records reason+timestamp, broadcasts cond.
- [ ] `Resume()`: sets paused false, broadcasts cond so any waiting sends proceed.
- [ ] `PauseState() (bool, string, time.Time)`.
- [ ] In `processor/internal/delivery/queue.go`'s `processJob`: insert the pause check **before** the rate-limit `Limiter.Check` call. If paused AND `job.BypassRateLimit == false` AND `job.EditKey == ""` (edits to already-tracked messages still go through), wait on `pauseCond` until unpaused. This ordering matters: it ensures no rate-limit counters increment during pause, so no breach notifications are generated by the limiter while ops work is in progress. Bypass jobs (rate-limit notifications, ban farewells) skip the pause check entirely.
- [ ] Tests: pause, dispatch a job, assert it doesn't send until resume; bypass job sends even when paused.

### Task 2.6b: Log capture buffer

- [ ] Create `processor/internal/logbuffer/` package: `Entry`, `Buffer`, `New(startupCap, rollingCap int)`, `Capture(level, message, source)`, `MarkStartupComplete()`, `Startup()`, `Recent()`, `ClearRecent()`.
- [ ] One mutex, two ring buffers internally (startup capped at 200 by convention, rolling capped at 50). Startup buffer freezes after `MarkStartupComplete` — subsequent captures only go to rolling.
- [ ] Read the existing logger setup in `processor/cmd/processor/main.go` to find what logging library is in use (logrus, zerolog, std log, etc.). Add a hook/sink/writer that intercepts WARN and ERROR records and calls `buffer.Capture`. INFO/DEBUG ignored.
- [ ] Place `buffer.MarkStartupComplete()` in `main.go` immediately before the HTTP server starts listening (after all subsystem init, just before `r.Run(...)`).
- [ ] Tests: capture before/after MarkStartupComplete and assert routing; ring buffer wraparound for both layers; concurrent capture race-clean.

### Task 2.7: Single-user reconcile entrypoint

- [ ] Check whether `reconcileSingleUser(userID string)` already exists in `processor/internal/discordbot/reconciliation.go` (CLAUDE.md says it's wired to `GuildMemberUpdate`; if so, just confirm it's exported or expose a wrapper).
- [ ] If absent, extract the per-user body of `SyncDiscordRole` into `ReconcileSingleUser(userID string) error`. Periodic sync becomes a loop over `ReconcileSingleUser`.
- [ ] Tests: synthetic user + role state, assert the right action (added/removed/updated).

### Task 2.8: Wire all introspection into `BotDeps`

- [ ] Edit `processor/internal/bot/command.go`. Add to `BotDeps`:
  ```go
  WebhookRate    func() webhook.RateSnapshot
  AlertLimiter   *ratelimit.Limiter
  DiscordRate    func() delivery.DiscordRateSnapshot
  TelegramRate   func() delivery.TelegramRateSnapshot
  GeocoderStats  func() geocoding.CacheStats
  GeocoderClear  func() int
  Dispatcher     *delivery.Dispatcher
  Reconciler     func(userID string) error   // single-user; nil-safe (skip on Telegram-only deploys)
  RunReconcile   func() error                // full sync
  LogBuffer      *logbuffer.Buffer           // startup + rolling WARN/ERROR captures
  ```
- [ ] Wire these in `processor/cmd/processor/main.go` where `BotDeps` is constructed.

---

## Phase 3: Status command (the consumer of Phase 2)

### Task 3.1: Shared `statusReport` helper

- [ ] Create `poracle_admin_status.go` with a `statusReport(ctx) []Reply` function (or `renderStatus(ctx, verbose bool) string`).
- [ ] Sections (from DESIGN.md): Build/uptime, Webhooks (5/15/60 min totals + per-type), Render queue depth, Delivery queue depth per platform, Discord rate state, Telegram rate state, Alert limits (count breached/banned per bucket), Summary buffer total, Tracking counts, MySQL ping + pool stats.
- [ ] Use 🟢/🟡/🔴 indicators where thresholds make sense (webhook rate dropping to 0; render queue >80%; >0 banned users; MySQL ping fail).
- [ ] Configurable thresholds via `[processor.status]` config section? Defer to Phase 9 if time; for now hardcode reasonable values.
- [ ] Tests: synthetic state through BotDeps stubs; assert the rendered output mentions each section and the indicator changes with the underlying state.

### Task 3.2: `status` subcommand

- [ ] `poracle_admin_status.go` `Run` calls `statusReport`. `status -v` calls verbose variant (per-route Discord detail + per-handler webhook breakdown).
- [ ] Tests: integration through the dispatcher.

### Task 3.3: Deprecate `!info poracle` and `!info config`

- [ ] Edit `processor/internal/bot/commands/info.go`. The `poracle` subcommand path now returns a single text reply: `tr.T("cmd.info.poracle.moved")` → "→ This view has moved to `!poracle-admin status` — please use that instead." NO rendering of status data from this command. Delete any helper code that was used only for the old `!info poracle` output (if it's still used elsewhere, leave it alone).
- [ ] Same treatment for the `config` subcommand: returns `tr.T("cmd.info.config.moved")` → "→ This view has moved to `!poracle-admin config` — please use that instead." NO rendering of config from this command. The actual config rendering moves to the new `!poracle-admin config` subgroup (Task 5.5).
- [ ] Add i18n keys `cmd.info.poracle.moved` and `cmd.info.config.moved` to en.json + de.json.
- [ ] Update any existing `!info poracle` and `!info config` tests: they now assert the redirect message, not the data. Keep the test names; just update the assertions.

### Task 5.5: `config` subgroup

- [ ] Create `processor/internal/bot/commands/poracle_admin_config.go` with the `config` subgroup. Subcommands: (no arg) — full effective config with redactions; `<section>` — one section by name; `keys` — list section names + key counts.
- [ ] Extract the existing config rendering logic from `!info config` (likely in `info.go`) into a shared helper in the new file. The redaction list (tokens, secrets, DSN passwords) becomes a `var redactedFields = map[string]bool{...}` at package scope so it's a single source of truth.
- [ ] Register the new subgroup in `paSubgroups` in `poracle_admin.go`. Add `config` to `paSubgroupOrder` in a sensible position (between `status` and `maintenance` makes sense alphabetically and conceptually).
- [ ] Add i18n keys: `cmd.poracle_admin.group.config.desc`, `cmd.poracle_admin.config.help.intro`, plus per-subcommand descs and the redacted-section header.
- [ ] Tests: full config rendering, one-section path, keys path, unknown-section error, redactions actually redact.

### Task 5.6: `warnings` subgroup

- [ ] Create `processor/internal/bot/commands/poracle_admin_warnings.go` with the `warnings` subgroup. Subcommands: (no arg) — startup + recent; `startup`; `recent`; `clear`.
- [ ] Calls `ctx.LogBuffer.Startup()` / `.Recent()` / `.ClearRecent()`. Renders each entry as `<timestamp> <level> <message> [<source>]`. Chunk long output through the existing chunked-reply helper.
- [ ] Register the new subgroup in `paSubgroups`. Add `warnings` to `paSubgroupOrder` next to `status` (operators looking at status will frequently want warnings too).
- [ ] Add i18n keys: `cmd.poracle_admin.group.warnings.desc`, `cmd.poracle_admin.warnings.help.intro`, per-subcommand descs, plus `cmd.poracle_admin.warnings.section.startup`, `cmd.poracle_admin.warnings.section.recent`, `cmd.poracle_admin.warnings.empty`, `cmd.poracle_admin.warnings.cleared`.
- [ ] Tests: seeded buffer (startup + recent), assert sections render correctly; empty buffer → empty-message; `clear` empties recent only.

---

## Phase 4: Slash group

### Task 4.1: `slash` subcommands

- [ ] Implement `poracle_admin_slash.go`. Subcommands: `sync`, `force-resync`, `clear-global`, `clear-guild <id>`, `status`, `help`.
- [ ] `sync` calls the same `SyncCommands` path that's wired today — extract a callable `func() error` from the registration code in `processor/internal/discordbot/slash/registration.go` if it's not already shaped that way.
- [ ] `force-resync` clears the fingerprint cache file then calls sync.
- [ ] `clear-global` calls `ClearGlobalCommands`.
- [ ] `clear-guild <id>` calls `ClearGuildCommands` for the one ID (require the arg; reject if missing).
- [ ] `status` reads the fingerprint cache and renders one row per scope (`global`, plus each configured guild) with last-synced timestamp + first 8 chars of fingerprint.
- [ ] Wire dispatcher reference + cache reader through `BotDeps`.
- [ ] Telegram-side execution: report "Discord slash commands not available — this command must be run from a deployment with the Discord side enabled" rather than erroring or no-opping silently.
- [ ] Tests: each subcommand happy path + the no-discord-platform refusal.

---

## Phase 5: Cache, ratelimit, summary, reconcile groups

### Task 5.1: `cache` subcommands

- [ ] `poracle_admin_cache.go`. Subcommands: `stats`, `clear geocoder`, `help`.
- [ ] `stats` calls `BotDeps.GeocoderStats()` and renders a small table.
- [ ] `clear geocoder` calls `BotDeps.GeocoderClear()` and reports how many entries were dropped.
- [ ] Tests: stubbed stats/clear funcs.

### Task 5.2: `ratelimit` subcommands

- [ ] `poracle_admin_ratelimit.go`. Subcommands: `list`, `show <target>`, `reset <target>`, `userlist`, `help`.
- [ ] `list` calls `AlertLimiter.ListBlocked()`, renders rows. Empty list → "no targets currently rate-limited" — good news that should be loud.
- [ ] `show <target>` calls `AlertLimiter.StateFor(target, "")`; if `target` lacks a type qualifier, look it up via `HumanStore.Get` to determine the type. Renders both buckets.
- [ ] `reset <target>` calls `AlertLimiter.Reset(...)`. Confirm action with a 2-line reply: who was reset + reminder that `admin_disable` isn't touched.
- [ ] `userlist` delegates to `cmd.userlist` with `["disabled"]` args (route through the registry, same shape as the untrack reroute in Task 0.1).
- [ ] Tests: seeded limiter state, each subcommand path.

### Task 5.3: `summary` subcommands

- [ ] `poracle_admin_summary.go`. Subcommands: `list`, `show <user>`, `fire <user> [alert_type]`, `help`.
- [ ] `list` calls a new `SummaryBuffer.EnumerateUsers()` (add to `internal/tracker/summary_buffer.go` — returns `[]struct{ HumanID, AlertType string; Count int; NextFireAt time.Time }`).
- [ ] `show <user>` takes a positional user ID; calls `SummaryBuffer.List(user, alertType)` for each alert type and renders. (The generic `user:<id>` admin-override is unrelated — it's `BuildTarget`'s mechanism and applies to every command, not something this subcommand needs to handle specially.)
- [ ] `fire <user> [alert_type]` calls into `ProcessorService.DispatchQuestSummary` (and per-type equivalents as they're added — for now just quest).
- [ ] Tests: seeded buffer, each subcommand path.

### Task 5.4: `reconcile` subcommands

- [ ] `poracle_admin_reconcile.go`. Subcommands: `run`, `user <id>`, `help`.
- [ ] `run` calls `BotDeps.RunReconcile()`.
- [ ] `user <id>` calls `BotDeps.Reconciler(id)`.
- [ ] If `BotDeps.Reconciler == nil` (Telegram-only deploy or Discord side disabled): refusal reply.
- [ ] Tests: stubbed reconciler funcs.

---

## Phase 6: Emoji + maintenance

### Task 6.1: `emoji` subcommands

- [ ] `poracle_admin_emoji.go`. Subcommands: `list`, `reload`, `test <key>`, `help`.
- [ ] `list` walks the loaded emoji config map and renders rows of `key | discord_resolution | telegram_resolution`. Long; split across multiple replies if needed (use the existing chunked-text reply helper).
- [ ] `reload` re-reads `config/emoji.json` (factor out the inner loader from startup so both share it).
- [ ] `test <key>` resolves the key via the existing `getEmoji(key, platform)` helper using `ctx.Platform`, reports the result.
- [ ] Tests: synthetic emoji map, each path.

### Task 6.2: `maintenance` subcommands

- [ ] `poracle_admin_maintenance.go`. Subcommands: `pause [reason...]`, `resume`, `status`, `help`.
- [ ] `pause` calls `Dispatcher.Pause(reason)`; reason defaults to "(no reason given)". Reply with confirmation + reminder that webhooks still ingest and queue.
- [ ] `resume` calls `Dispatcher.Resume()`; reply with confirmation.
- [ ] `status` calls `Dispatcher.PauseState()`; reports paused/running + reason + how long.
- [ ] Status banner: when paused, `statusReport` from Phase 3 prepends a 🔴 PAUSED banner so anyone running `!info poracle` or `!poracle-admin status` notices immediately.
- [ ] Tests: pause/resume with a stub dispatcher, assert state transitions and banner inclusion.

### Task 6.3: Universal "maintenance active" reply suffix

- [ ] Add i18n key `cmd.maintenance.active_suffix` to `en.json` ("🔧 Maintenance mode is active — alerts are not being delivered.") and `de.json`.
- [ ] In the reply-emission path of `processor/internal/discordbot/bot.go` (the function that converts `[]bot.Reply` into outgoing Discord messages): after building the message content, check `BotDeps.Dispatcher.PauseState()`. If paused, append the suffix as a new line on the last text reply; if the last reply is an embed, send an additional plain-text follow-up message containing the suffix.
- [ ] Same in `processor/internal/telegrambot/bot.go`.
- [ ] Suffix appends to every command reply on both platforms — not just `!poracle-admin` — so any user interaction during maintenance gets the signal. The suffix vanishes the moment `resume` is called (it's a live check, not a cached state).
- [ ] Tests: simulate paused dispatcher, run a command (e.g. `!version`), assert the suffix is present in the rendered output. Resume, run the same command, assert it's absent.

---

## Phase 7: i18n + final review

### Task 7.1: Full i18n sweep

- [ ] Every user-visible string under `cmd.poracle_admin.*` is i18n-keyed. No string literals in reply construction.
- [ ] Add German translations to `de.json` for every new key.
- [ ] Run the existing translation completeness test (if one exists) or add one that asserts every `cmd.poracle_admin.*` key in `en.json` has a `de.json` counterpart.

### Task 7.2: End-to-end smoke

- [ ] Manual test plan: run each subgroup against a dev deployment. Document the steps in `docs/admin-commands/SMOKE.md` (or under a "Manual verification" section here).
- [ ] Verify behaviour on both Discord and Telegram. Confirm "Discord-only" subgroups (`slash`, `reconcile`, `emoji`) refuse cleanly on Telegram with i18n-keyed messages.

### Task 7.3: Documentation

- [ ] Update CLAUDE.md's "Key Commands" section to list `!poracle-admin` and the `!untrack <type>` reroute.
- [ ] Update `config.example.toml` if any new config section was added (Task 3.1 mentioned `[processor.status]` for thresholds — only if implemented).
- [ ] Update `processor/internal/api/config_schema.go` if config was added.
- [ ] Add a brief admin-commands paragraph to README.md.

### Task 7.4: Self-review

- [ ] Run `/simplify` over the branch.
- [ ] Run `go test ./...` and `go build ./...`.
- [ ] Dispatch a final code reviewer subagent against the full diff.

---

## Cross-cutting concerns

### Permissions

- Every subgroup `Run` re-asserts `bot.IsAdmin` defensively, even though `PoracleAdminCommand.Run` already gates. Belt-and-braces in case a subgroup is ever reached through a different code path.
- `command_security` integration: top-level only for v1 (`poracle_admin`). Per-subgroup ACLs (`poracle_admin.reload` etc.) are a follow-up if any operator asks.

### Platform parity

- All subgroups must work on Telegram, except: `slash` (no Discord registration to manage), `reconcile` (Discord-roles-specific), `emoji` (Discord-emoji-specific). These three return a clear i18n-keyed refusal when invoked from Telegram.

### Reply formatting

- Status output is text-only (no Discord embed). Reason: parity with Telegram and keeps the rendering helper platform-agnostic.
- Long output (emoji list, ratelimit list) uses the existing chunked-text helper that already handles Discord's 2000-char and Telegram's 4096-char limits.

### Hot-path cost

- Webhook rate counter: one mutex + one map update per webhook. Negligible.
- Ratelimit/Discord-rate counters: same — counter increments are already happening, this work adds read-side methods that copy out under existing locks.
- Geocoder cache hit/miss: three `atomic.Uint64` increments per `Get`. Negligible.
- Dispatcher pause: one `atomic.Bool` load per job's pre-send. No-op when not paused.

### Testing strategy

- Each command file has a `*_test.go` next to it. Use the existing test harness (the slash-commands work introduced parity fixtures; admin commands don't need parity because there's no slash equivalent, but the same `CommandContext` stubbing pattern applies).
- Introspection APIs (Phase 2) get their own unit tests independent of any command.
- The `statusReport` helper is the one that benefits most from a snapshot test — fix a synthetic state, assert the rendered output verbatim.

---

## Estimated effort

Rough sizing — for human planning, not contractual:

- Phase 0: 0.5 day
- Phase 1: 0.5 day
- Phase 2: 2 days (six introspection APIs)
- Phase 3: 1.5 days (status is the most rendering-heavy piece)
- Phase 4: 1 day
- Phase 5: 1.5 days
- Phase 6: 1 day
- Phase 7: 1 day

Total: ~9 dev-days for one engineer. Parallelisable across two by splitting Phase 2 + Phase 5 subgroups.
