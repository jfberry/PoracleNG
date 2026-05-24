# Admin Commands — Design

> **Status:** Implemented. Branch: `slash-commands-design`.

## Implementation status

Implemented in branch `slash-commands-design` alongside the Discord slash-command surface. All 11 subgroups (`slash`, `reload`, `emoji`, `reconcile`, `cache`, `ratelimit`, `summary`, `status`, `maintenance`, `config`, `warnings`) are live.

**Smoke verification:** `docs/admin-commands/SMOKE.md` — work through that checklist against a dev deployment before merging to main.

**Deferred items** (explicitly not implemented; tracked here for follow-up):

- `[processor.status]` config section for customisable status thresholds (webhook floor, render-queue warn %, delivery-queue warn depth, summary-buffer warn count). Currently hardcoded to the constants in `poracle_admin_status.go`.
- Persistent maintenance pause across restarts. A restart implicitly resumes, which is the safer default.
- Render-queue depth accessor in `BotDeps`. The `status` command reports "n/a" for the render-queue section; a `RenderQueueDepth`/`RenderQueueCapacity` closure would need to be added to `BotDeps` and wired in `main.go`.
- Summary buffer size in `BotDeps`. The `status` command reports "n/a" for the summary-buffer section; `SummaryBuffer.EnumerateUsers()` is accessible via `paSummary` but not surfaced in `statusReport` yet.
- Per-subgroup `command_security` ACLs (`poracle_admin.reload`, `poracle_admin.maintenance`, etc.). Currently only the top-level `poracle_admin` key is checked.
- Slash-command surface for admin operations (intentionally out of scope — admin actions are text-only by design).
- Webhook log replay (`!poracle-admin webhook replay <id>`).
- Gym state reset (`!poracle-admin gym reset`).
- Per-route Discord rate-limit tuning (inspection only is implemented).
- Unified `!stats` view rolling up `/api/stats/rarity` and `/api/stats/shiny`.

**Goal:** Provide a live administrative surface in chat so operators can manage slash-command registration, reload runtime state, inspect rate-limit and webhook health, manage emoji, trigger reconciliation, and pause delivery — all without editing config and restarting the processor.

## Scope

In scope:

- A new `!untrack <type>` text-command form for consistency with the slash surface (`/untrack raid id:12` works in both).
- A new `!poracle-admin` umbrella command with subcommand groups: `slash`, `reload`, `emoji`, `reconcile`, `cache`, `ratelimit`, `summary`, `status`, `config`, `warnings`, `maintenance`.
- New read-only introspection APIs on internal subsystems (webhook rate, ratelimit blocked-targets, Discord per-route rate state, geocoder cache stats, log capture buffer) where today's state is private.
- Deprecation of `!info poracle` and `!info config` admin subcommands — both print a one-line "→ moved to …" pointer. Single canonical implementation lives under `!poracle-admin`.

Explicitly out of scope:

- New top-level operator commands outside the `!poracle-admin` namespace. (Existing `!broadcast`, `!apply`, `!userlist` stay where they are.)
- Slash-command equivalents for `!poracle-admin`. Admin commands are text-only by design — same reasoning as `!poracle-test`: ops actions belong in a deliberate text invocation, not a Discord-managed picker.
- Per-target alert mutation (the existing rate-limit auto-disable path stays untouched; admin commands only inspect and reset).
- Webhook log replay (mentioned in brainstorming, dropped as low-value-for-risk).

## Why an umbrella

Three reasons:

1. **One permission gate.** Every subcommand is admin-only; one `IsAdmin` check at the top of `cmd.poracle_admin.Run` covers the whole surface. Without an umbrella each subcommand re-implements the check (or worse, forgets to).
2. **Discoverability.** `!poracle-admin help` enumerates every operator action in one place. Today operators learn about `-clear-guild-slash-commands` from the README and `/api/dts/reload` from API.md; with the umbrella, `!poracle-admin help` and `!poracle-admin <group> help` are the canonical references.
3. **Top-level command sprawl.** Adding nine new top-level commands (`!reload`, `!status`, `!maintenance`, …) crowds the namespace and creates collision risk with future feature commands.

## `!untrack <type>` reroute

Today `!untrack <args>` only handles pokemon; for other types, operators use `!raid remove id:12`, `!egg remove id:34`, etc. The slash dispatcher already reroutes `/untrack <type>` to `cmd.<type>` with `["remove", ...]` tokens (see `processor/internal/discordbot/slash/dispatcher.go`). This work mirrors that reroute on the text path so both surfaces accept the same shape.

**Design choice:** the reroute lives inside `cmd.untrack.Run`, not in the parser. Reasons:

- Parser-level rewriting would obscure what command the user invoked (logs, error messages would lie).
- Disambiguation against pokemon names is clean: the recognised type list (`raid`, `egg`, `quest`, `invasion`, `incident`, `lure`, `nest`, `gym`, `fort`, `maxbattle`) is disjoint from pokemon names and pokemon aliases. First-token-is-type can be checked without false positives.
- Keeps the parser stateless; subcommand-style dispatch already exists for other commands (`!info <sub>`, `!profile <sub>`).

Behaviour: `!untrack raid id:12` → re-dispatches to `cmd.raid.Run(ctx, ["remove", "id:12"])`. `!untrack pikachu iv90` falls through to existing monster-untrack logic. Type tokens must be the canonical English form OR translated to it by the parser before `cmd.untrack` sees them — same convention every other command follows.

## `!poracle-admin` command shape

Top-level: `!poracle-admin <group> <subcommand> [args...]`, with `!pa` registered as a typing alias (same command, shorter to type — discoverable via the long name, ergonomic via the short).

With no args: prints group-level help. With `<group>` only: prints that group's subcommand help.

### Admin-only surface

This command is reserved for administrators. The fact is communicated through four overlapping signals so it cannot be mistaken:

1. **The name itself.** "admin" is in the canonical command name; the alias `!pa` is short but operators will discover it via help where the long name leads.
2. **Help filtering.** `!poracle-admin` does not appear in `!help` output for non-admin users (same treatment as `!broadcast`, `!apply`, `!userlist`).
3. **Help intro text.** The first line of `!poracle-admin help` is i18n key `cmd.poracle_admin.help.admin_only`: "This command is reserved for server administrators. Each subgroup performs a live operations task."
4. **Refusal is a text message, not a react.** When a non-admin invokes `!poracle-admin` or `!pa`, the bot replies with `cmd.poracle_admin.not_admin`: "This command is reserved for administrators." A text message (not a silent 🙅 react) so operators don't waste time wondering if the bot is broken. The 🙅 react is reserved for `command_security` role-gated denials, which is a different mechanism.

Permission: admin-only via existing `bot.IsAdmin(cfg, platform, userID)`. `command_security` integration is top-level only for v1 — a delegated-admin role can be granted access to the entire `!poracle-admin` surface but not to individual subgroups. Per-subgroup ACLs (`poracle_admin.reload`, `poracle_admin.maintenance`, etc.) are non-breaking to add later if any operator asks.

i18n: every subcommand name and help string is identifier-keyed (`cmd.poracle_admin`, `cmd.poracle_admin.group.slash`, `cmd.poracle_admin.slash.sync`, etc.). English bundle is canonical; non-English bundles fall back per-key as elsewhere.

### Group reference

#### `slash` — slash-command lifecycle

| Subcommand | What it does |
|---|---|
| `sync` | Re-run slash sync against Discord, respecting current `register_globally` + `guilds` config. Equivalent to today's `-sync-slash-commands` CLI flag. |
| `force-resync` | Clear the local fingerprint cache, then sync. Use when commands look wrong in clients and you want a guaranteed push. |
| `clear-global` | Remove globally-registered commands from Discord (used when migrating from global → per-guild). Equivalent to today's `-clear-global-slash-commands` CLI flag. |
| `clear-guild <guild_id>` | Remove guild-scoped commands from one guild. With no arg: prompts for the guild ID. Equivalent to today's `-clear-guild-slash-commands` CLI flag, but per-guild instead of all-configured-guilds. |
| `status` | Show last sync timestamp + fingerprint per scope (global + each configured guild). |

Implementation reuses `internal/discordbot/slash/registration.go` Sync/Clear methods. The CLI flags can remain for emergency boot-time recovery but the live commands become the canonical path.

#### `reload` — runtime state reload

| Subcommand | What it does |
|---|---|
| `dts` | Reload DTS templates and partials from disk. Wraps `/api/dts/reload`. |
| `geofence` | Reload geofence files + Koji fetch. Wraps `/api/geofence/reload`. |
| `state` | Reload tracking rules and human records from MySQL. Wraps `/api/reload`. |

No `reload all` — explicit per-category invocation is clearer and (with `geofence`) avoids unintentionally hammering Koji.

No `reload masterfile` or `reload icons` — neither has a runtime reload path today. Adding one is its own piece of work; not blocking this plan.

#### `emoji` — emoji management

| Subcommand | What it does |
|---|---|
| `list` | Show all configured emoji keys + per-platform resolutions. Lets an operator see what `emojiWeather=fog` actually renders to on Discord vs Telegram. |
| `reload` | Reload `config/emoji.json` from disk. Useful after uploading new guild emoji and updating the file. |
| `test <key>` | Render one emoji for the current platform (uses the calling channel's platform) — handy sanity check after uploads. |

The existing `!poracle-emoji upload` / `!poracle-id` / `!poracle-clean` commands stay where they are — they're Discord-side gateway operations with different mechanics (manipulating guild assets, not just reading config). The `emoji` group is for config-side inspection.

#### `reconcile` — Discord role sync

| Subcommand | What it does |
|---|---|
| `run` | Trigger the full `SyncDiscordRole` immediately instead of waiting for the periodic timer. |
| `user <id>` | Re-evaluate a single user's roles + community membership. Useful when an operator has just changed someone's roles and wants to verify the bot's view. |

`user <id>` depends on a single-user reconcile entrypoint. CLAUDE.md describes `reconcileSingleUser()` as wired to `GuildMemberUpdate`/`GuildMemberRemove` events; if that function doesn't yet exist (the exploration agent couldn't find it but the docs assume it), Phase 5 extracts one from `SyncDiscordRole`'s per-user loop.

#### `cache` — cache stats and clear

| Subcommand | What it does |
|---|---|
| `stats` | Report geocoder cache sizes: memory layer count + disk layer count + hit/miss counters (if instrumented). |
| `clear geocoder` | Drop the geocoder memory cache. Disk layer untouched unless explicitly `--disk`. |

Shlink cache dropped from scope — not enough operational value, and Shlink is an external service so clearing local state doesn't affect what users see.

#### `ratelimit` — alert limit inspection

| Subcommand | What it does |
|---|---|
| `list` | Show all targets currently in a breached/banned state across both buckets (alert + summary). Columns: target, platform, type, bucket, count, breach time, ban-until time. |
| `show <target>` | Detailed view of one target: current counter values for alert + summary buckets, window-start, violation count in the 24h window, last breach. |
| `reset <target>` | Zero all counters for the target. Does NOT touch `admin_disable` — if the user was auto-disabled, they still need re-registration. Use for false-positive testing or one-off forgiveness. |
| `userlist` | Shortcut: routes to `!userlist disabled` for the operator's convenience (acknowledges the discoverability hole — "where do I find all auto-disabled users?"). |

Requires new introspection API on `internal/ratelimit/ratelimit.go` — see "New introspection APIs" below.

#### `summary` — summary buffer inspection

| Subcommand | What it does |
|---|---|
| `list` | All users with anything buffered: rows of `user, alert_type, entry_count, next_fire_at`, plus a total. |
| `show <user>` | Per alert type for that user: count + sample entries. |
| `fire <user> [alert_type]` | Force-dispatch the user's buffered entries now. With `alert_type`: just that bucket. Without: all buckets. Equivalent to `!summary quest now` but admin-targeted. |

The generic `user:<id>` admin-override (resolved by `BuildTarget` on every command) already lets an operator run `!summary quest now user:<id>` against another user — but operators don't always know the override exists, so `!poracle-admin summary fire <user>` is the discoverable surface for the same capability.

Uses the existing `SummaryBuffer.List(humanID, alertType)` API; adds an `EnumerateUsers()` method for `list`.

#### `status` — health snapshot

This is the single-screen "is anything wrong" view. **`!info poracle` is deprecated** — it now prints a one-line redirect ("→ moved to `!poracle-admin status`") and nothing else. Single canonical implementation lives here; no aliasing, no shared helpers, no two-render-paths drift.

Sections:

- **Build/uptime:** version, uptime, last config reload, last state reload, last DTS reload.
- **Webhooks:** received per minute over the last 5/15/60 minutes, broken out by type. Warning indicator if total over the last 5 min is below a configurable floor (default: 0 over 5 min on a previously-active install = red). Addresses "warn if webhooks not arriving."
- **Match/render queue:** depth + capacity for the render channel; warning if >80% full (matches the existing tile-skip threshold).
- **Delivery queue:** per-platform queue depth + active workers. Recent permanent-failure count (last hour).
- **Discord rate state:** number of routes with active `X-RateLimit-Remaining < limit`, number of 429s in the last 5 min, global token-bucket tokens remaining. Addresses "visibility on rate-limited routes."
- **Telegram rate state:** count of 429 retries in the last 5 min, any active backoff timers.
- **Alert limits:** count of targets currently in a breached/banned state for each bucket (alert + summary).
- **Summary buffer:** total entries buffered, distinct user/type buckets.
- **Tracking counts:** per-type tracking row count, active human count, registered human count.
- **MySQL:** ping + connection pool stats.

`!info poracle` keeps its existing trigger as a shortcut but renders through the new helper. Operators with muscle memory don't have to relearn anything.

The status output uses sections separated by blank lines and emoji indicators (🟢 / 🟡 / 🔴) for at-a-glance scanning. Verbose mode (`!poracle-admin status -v`) adds per-route Discord detail, per-handler webhook breakdown, and full queue contents.

#### `config` — config inspection

Effective merged config with sensitive fields redacted. Replaces `!info config` (which now just prints "→ moved to `!poracle-admin config`"). Single rendering function backs the new subgroup; the deprecated `!info config` path doesn't render anything itself.

| Subcommand | What it does |
|---|---|
| (no arg) | Print effective config with redactions, sectioned. |
| `<section>` | Print just one section (e.g. `discord`, `geofence`, `processor`). |
| `keys` | List section names + key counts. |

Same redaction list as the existing `!info config` (tokens, secrets, DSN passwords) — extract that list into a shared helper so adding a new sensitive key automatically protects both surfaces.

#### `warnings` — captured WARN/ERROR log buffer

Operators routinely need to see "what's gone wrong recently" without SSH'ing to read the log file. This subgroup keeps two ring buffers in memory and renders them on demand.

**Startup buffer** — captures every WARN and ERROR from process start until `log.MarkStartupComplete()` is called. The call sits in `cmd/processor/main.go` right before the HTTP server starts listening (the natural "the bot is now running" point). Bounded to 200 entries so a misconfigured deployment can't OOM the buffer.

**Rolling buffer** — last 50 WARN/ERROR entries after startup, FIFO. Drops the oldest when full.

Each entry records: timestamp, level (WARN/ERROR), message, source `file:line` if the logging library provides it. If startup never completes (the call never fires because main.go panicked) the rolling buffer stays at zero entries and the startup buffer holds everything — useful diagnostic state in itself.

| Subcommand | What it does |
|---|---|
| (no arg) | Print "Startup section" (all captured warnings since process start, up to 200) + blank line + "Recent section" (last 50 rolling buffer entries, newest first). |
| `startup` | Just the startup section. |
| `recent` | Just the rolling buffer. |
| `clear` | Empty the rolling buffer. Startup buffer is immutable post-startup. |

Implementation: a new `internal/logbuffer/` package with a thread-safe ring + hook into the logging library. The hook fires synchronously inside the log call, so the cost per log line is one mutex acquire + one slot write — negligible against the existing log-formatting cost. Levels below WARN are not captured (no INFO or DEBUG noise).

#### `maintenance` — pause/resume delivery

| Subcommand | What it does |
|---|---|
| `pause [reason]` (alias `start`) | Halt outbound delivery (Discord + Telegram). Webhooks still ingest, match, and render — but their delivery jobs are **dropped on the floor**. Optionally records a reason for `status` to display. |
| `resume` (alias `stop`) | Resume delivery. Nothing to drain — jobs dropped during the pause window are gone, not buffered. |
| `status` | Show current state (paused/running) and queue depth. |

Use for DB maintenance, scanner upgrades, or panic-mode "stop sending while we debug". A paused state survives restarts only if persisted — design choice: **don't persist**. A restart implicitly resumes, which matches the safer "I rebooted to fix this and want alerts back" expectation.

**Drop, don't buffer.** When paused, normal delivery jobs are dropped rather than queued. Buffering would balloon memory on long pauses and produce a flood of stale alerts on resume — neither matches what an operator wants from "maintenance mode." Bypass jobs (rate-limit notifications, ban farewells) still send; they're administrative messages and tend to be rare. Edit jobs (RSVP updates etc.) are also dropped — the operator's mental model is "throw everything on the floor."

**Pause check ordering.** The pause check sits *before* the rate-limit check in `FairQueue.processJob`, so during pause no counter increments and no breach-notification jobs are produced — the upstream cause won't fire.

**Universal "maintenance active" suffix.** Every command reply (not just `!poracle-admin` — every command on every surface) appends `cmd.maintenance.active_suffix`: "🔧 Maintenance mode is active — alerts are not being delivered." while pause is on. This stops users from filing "the bot is broken" tickets when their `!track` succeeds but no alerts arrive. Implementation lives in the reply-emission layer (Discord and Telegram bots), so every command benefits without modification — and the suffix vanishes the moment `resume` is called.

**Status banner.** When paused, `statusReport` from the `status` group prepends a 🔴 PAUSED banner so anyone running `!info poracle` or `!poracle-admin status` notices immediately, with the operator-supplied reason and time-since-pause.

Requires a pause/resume primitive on `delivery.Dispatcher` — see "New introspection APIs" below.

## New introspection APIs

These are the only non-trivial new abstractions this work adds. Everything else wraps existing internal calls.

### Webhook rate counter

`internal/webhook/receiver.go` gains a sliding-window minute counter. Implementation: a 60-slot ring buffer of `(timestamp, count)`, indexed by `unixMinute % 60`, with a per-type breakdown map under a single mutex. Public read API:

```go
type RateSnapshot struct {
    Per5Min, Per15Min, Per60Min  int
    PerType                       map[string]int  // last 60 min, per webhook type
}
func (r *Receiver) RateSnapshot() RateSnapshot
```

Cost per webhook: one mutex acquire, one map update — negligible against the existing handler cost.

### Ratelimit blocked-targets API

`internal/ratelimit/ratelimit.go` adds:

```go
type TargetState struct {
    ID, Type, Bucket  string
    Count             int
    WindowStart       time.Time
    BannedUntil       time.Time
    ViolationCount24h int
}
func (l *Limiter) ListBlocked() []TargetState        // anything in breach or banned
func (l *Limiter) StateFor(id, dtype string) []TargetState  // both buckets for one target
func (l *Limiter) Reset(id, dtype string) bool        // returns true if anything was reset
```

Implementation: snapshot under the existing lock, copy out by value. Hot-path cost unaffected.

### Discord per-route rate snapshot

`internal/delivery/ratelimit.go` adds:

```go
type RouteState struct {
    Route          string
    Remaining      int
    Limit          int
    ResetAt        time.Time
}
type DiscordRateSnapshot struct {
    Routes          []RouteState  // only routes with remaining < limit
    GlobalTokens    int
    Recent429Count  int  // last 5 min
}
func (l *DiscordRateLimiter) Snapshot() DiscordRateSnapshot
```

Telegram gets a smaller equivalent — it doesn't have per-route limits, just global 429 backoff.

### Log capture buffer

New package `internal/logbuffer/`:

```go
type Entry struct {
    Time    time.Time
    Level   string  // "WARN" or "ERROR"
    Message string
    Source  string  // "file.go:42", empty if not available
}

type Buffer struct { ... }  // thread-safe; one mutex; two ring buffers internally
func New(startupCap, rollingCap int) *Buffer
func (b *Buffer) Capture(level, message, source string)  // called by log hook
func (b *Buffer) MarkStartupComplete()                   // freezes startup buffer
func (b *Buffer) Startup() []Entry                       // snapshot (copy)
func (b *Buffer) Recent() []Entry                        // snapshot (copy)
func (b *Buffer) ClearRecent()
```

The log hook lives where the logger is initialised (probably `cmd/processor/main.go` or wherever the logrus/zerolog/etc. instance is set up — implementer to discover). It wraps the existing log writer; for every WARN or ERROR record, it calls `buffer.Capture(...)`. INFO and DEBUG are ignored.

`MarkStartupComplete` is called from `main.go` immediately before `r.Run(":3030")` or equivalent. From that point forward, captures go into the rolling buffer and the startup buffer is sealed.

### Geocoder cache stats

`internal/geocoding/cache.go` adds:

```go
type CacheStats struct {
    MemoryEntries  int
    DiskEntries    int
    HitsMemory     uint64
    HitsDisk       uint64
    Misses         uint64
}
func (c *Cache) Stats() CacheStats
func (c *Cache) ClearMemory() int  // returns entries cleared
```

Hit/miss counters require adding `atomic.AddUint64` calls inside `Get` and `Set`. Trivial; no contention.

### Delivery pause primitive

`internal/delivery/dispatcher.go` adds:

```go
func (d *Dispatcher) Pause(reason string)
func (d *Dispatcher) Resume()
func (d *Dispatcher) PauseState() (paused bool, reason string, since time.Time)
```

Implementation: a `paused atomic.Bool` checked in `FairQueue.processJob` before each send call; when set, the queue holds the job and waits on a `sync.Cond` until resumed. Edits, summary bypass, and ban-farewell jobs still respect pause (the whole point) — though the rate-limit notification path may want a configurable override (debate point below).

## Debate points

1. **`!poracle-admin` vs `!admin`.** Shorter is nicer but `!admin` is generic enough to collide with future things (mod tools, etc.). Sticking with the `!poracle-*` namespace convention used by `!poracle-test`, `!poracle-emoji`, `!poracle-clean`.

2. ~~Should `maintenance pause` block rate-limit notifications too?~~ Resolved by the pause-before-limiter ordering: during pause, no rate-limit counters increment, so no breach notifications are produced. The universal "maintenance active" suffix on every command reply tells users why their alerts have stopped, which removes the only reason the rate-limit notification would have been useful here.

3. **Should `status` be on the public side?** Right now it's admin-only. Some operators might want a `!poracle-health` or similar that anyone can run to confirm "the bot is alive." Out of scope here, but mention as a follow-up.

4. **Slash-command access.** Admin commands stay text-only. Debate-point only because we've established a slash surface for everything else — does the omission feel inconsistent? Position: no. `!poracle-test` is text-only for the same reason and nobody's complained; admin actions deserve typed deliberation, not picker autocomplete.

5. **Telegram parity.** All `!poracle-admin` subcommands must work on Telegram too, since Telegram-only operators exist. The Discord-specific groups (`slash`, `reconcile`, `emoji`) gracefully no-op or report "not supported on this platform" rather than erroring.

6. **`reload state` debounce interaction.** The existing 500ms debouncer coalesces tracking-API mutations. Should `!poracle-admin reload state` bypass the debounce (force immediate) or feed into it? Default proposal: **bypass**. The operator typed the command for a reason and shouldn't wait 500ms.

## Out of scope (for follow-up)

- Slash-command surface for admin operations.
- Webhook log replay (`!poracle-admin webhook replay <id>`) — useful for debugging but the safety story (preventing real alert fanout from replayed data) needs its own design.
- Gym state reset (`!poracle-admin gym reset`) — niche enough to skip until requested.
- Persistent maintenance pause across restarts.
- Per-route Discord rate-limit *tuning* (not just inspection).
- A unified `!stats` view rolling up `/api/stats/rarity` and `/api/stats/shiny` into the admin namespace.
