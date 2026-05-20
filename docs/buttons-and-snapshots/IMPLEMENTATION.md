# Buttons & Snapshots & TOML DTS — Implementation

> **For agentic workers:** Use `superpowers:subagent-driven-development`. Fresh subagent per task, two-stage review per task, continuous execution.

**Companion documents:** `DESIGN.md` (decisions locked), `README.md` (scope), `SMOKE.md` (manual verification per phase). Read `DESIGN.md` first.

**Branch:** `buttons-and-snapshots` off `raid-rsvp`. The dev chain stacks: `main` → `slash-commands-design` → `raid-rsvp` → `buttons-and-snapshots`. `slash-commands-design` and `raid-rsvp` are not pre-merged to `main`; we develop on top of them.

**Scope:** Opt-in snapshot store, Discord buttons + `!mute` commands, TOML DTS format with config-editor round-trip. Five phases; each independently demoable.

---

## File structure

### New packages

- `processor/internal/snapshots/` — pogreb-backed `Snapshot` store. `Store` interface + impl, background sweep, metrics.
- `processor/internal/mute/` — in-memory `MuteEntry` map, matcher integration, expiry sweep, metrics.
- `processor/internal/buttonactions/` — action handler registry, `Handler` interface, `mute` / `unsubscribe` / `redeliver` / `render` handlers.

### Modified

- `processor/cmd/processor/main.go` — wire snapshot store + mute store + button-action registry into `ProcessorService`; honour `[snapshots] enabled`.
- `processor/cmd/processor/render.go` — emit `components` block when buttons are configured; thread `RenderJob` through to dispatcher with snapshot-write metadata.
- `processor/cmd/processor/{pokemon,raid,maxbattle,invasion,quest,gym,nest,lure,fort}.go` — add `filterMuted` step after `filterValidation`.
- `processor/internal/config/config.go` — `[snapshots]` config section with `enabled` (default false), `path`, `max_age`, `sweep_interval`.
- `processor/internal/delivery/discord.go` — write snapshot on send success; re-include `components` on edits.
- `processor/internal/delivery/telegram.go` — write snapshot on send success (parity, even though no buttons in v1).
- `processor/internal/delivery/tracker.go` — drop snapshot when message is clean-deleted.
- `processor/internal/dts/templates.go` — TOML loader, `buttons[]` parsing, `buttonResponse` template type, duplicate-conflict WARN.
- `processor/internal/dts/renderer.go` — filter `buttons` by `applies_to` + `show_if` at render time; build `custom_id`.
- `processor/internal/discordbot/bot.go` — `InteractionCreate` handler.
- `processor/internal/discordbot/interactions.go` (new) — click handler: snapshot lookup → button resolve → dispatch.
- `processor/internal/bot/commands/mute.go` (new) — `!mute` / `!unmute` command implementations.
- `processor/internal/bot/commands/<type>.go` — extend per-type handlers with `mute|unmute` subcommands (mirrors `remove`).
- `processor/internal/bot/commands/tracked.go` — augment `!tracked` output with active mutes.
- `processor/internal/api/snapshots.go` (new) — `GET /api/snapshots/<messageID>`.
- `processor/internal/api/dts.go` — TOML round-trip in editor save path; `source_format` field; pre-write backup.
- `processor/internal/i18n/locale/*.json` — new keys: `cmd.mute`, `cmd.unmute`, `msg.button.*` (6 keys), `msg.mute.*`.

### New examples

- `examples/dts/buttons/raid-with-mute.json` — minimal JSON example: one raid template with one mute button.
- `examples/dts/buttons/raid-with-pvp.toml` — TOML example: raid template with conditional PVP-details button.
- `examples/dts/buttons/README.md` — operator-facing notes.

### Documentation

- `CLAUDE.md` — snapshot store, mute infrastructure, button actions, TOML DTS sections.
- `DTS.md` — new TOML section + buttons section.
- `README.md` — operator-facing summary; how to enable, how to add a button.

---

## Phase 0: Familiarisation

### Task 0.1: Verify branch base

- [ ] Confirm `raid-rsvp` branch exists locally and is current with origin.
- [ ] Branch `buttons-and-snapshots` from `raid-rsvp`.
- [ ] Pull existing test suite green on the branch (whatever was green on `raid-rsvp`).

### Task 0.2: Map the touchpoints

- [ ] Read `processor/cmd/processor/render.go` — the `RenderJob` and `processRenderJob` flow.
- [ ] Read `processor/internal/delivery/discord.go` — `Send`, `Edit`, and the components handling (look for any existing `components` field; expected: none).
- [ ] Read `processor/internal/delivery/tracker.go` — `MessageTracker` lifecycle, clean-deletion callback.
- [ ] Read `processor/internal/dts/templates.go` — current JSON loader, selection chain.
- [ ] Read `processor/cmd/processor/{pokemon,raid}.go` — `filterBlocked` and `filterValidation` placement (mute filter goes between `filterValidation` and render).

No code yet. Output: brief notes on each touchpoint for the implementer's context.

---

## Phase 1: Snapshot store

**Goal:** Opt-in snapshot store. Alert fires → snapshot in pogreb. Edits overwrite. Clean-deletion drops. Background sweep cleans orphans.

**Demoable:** `enabled=true` in config; fire an alert; `GET /api/snapshots/<messageID>` returns the snapshot JSON.

### Task 1.1: Package skeleton + struct

- [ ] Create `processor/internal/snapshots/`.
- [ ] Define `Snapshot` struct per #108 (all fields including `TemplateRequested` / `TemplateSelected`).
- [ ] Define `Store` interface: `Write(key string, s *Snapshot) error`, `Read(key string) (*Snapshot, error)`, `Delete(key string) error`, `Sweep(maxAge time.Duration) (deleted int, err error)`.
- [ ] Stub all methods.

### Task 1.2: pogreb-backed implementation

- [ ] Add pogreb impl backing `Store`.
- [ ] Key format: `target:messageID`.
- [ ] Value: JSON-serialised `Snapshot` with `version: 1` field at the top.
- [ ] On read: if version > supported, treat as miss + log warn.
- [ ] On write error: log warn, return nil (never block delivery).

### Task 1.3: Background sweep

- [ ] Goroutine started in `Open()`, stopped in `Close()`.
- [ ] Walks pogreb keys, deletes entries past `ExpiresAt + max_age` grace period.
- [ ] Runs every `sweep_interval` (default 1h).
- [ ] Emits metric per sweep run.

### Task 1.4: Config wiring

- [ ] Add `[snapshots]` section to `internal/config/config.go`: `Enabled bool` (default false), `Path string` (default `config/.cache/snapshots/`), `MaxAge time.Duration` (default 7 days), `SweepInterval time.Duration` (default 1h).
- [ ] Loader does NOT create the directory if `Enabled = false`.
- [ ] Add to `config.example.toml` with documentation comments explaining the opt-in nature.

### Task 1.5: Wire into ProcessorService

- [ ] `cmd/processor/main.go`: construct `snapshots.Store` if enabled; pass to delivery dispatcher and to `InteractionCreate` handler.
- [ ] If disabled, pass a nil store; senders + render path check for nil and skip.
- [ ] Shutdown order: dispatcher → tracker → snapshots → ...

### Task 1.6: Sender integration — Discord

- [ ] After `Send` success in `delivery/discord.go`: build `Snapshot` from the `Job` + the resolved message ID; write via `Store.Write`.
- [ ] On `Edit` success: build a fresh `Snapshot` from the new state; write (overwrite same key).
- [ ] **Components must be re-included on every `Edit` call** — gather components from the current render job and pass to the edit API. Without this, button-bearing edited messages lose their buttons.
- [ ] Write errors: log warn, do not fail the job.

### Task 1.7: Sender integration — Telegram

- [ ] Parity write on Telegram send success (no buttons in v1, but snapshot still useful for command-time inspection and future Telegram button work).
- [ ] No edit-side changes needed — Telegram doesn't have the components-replace issue.

### Task 1.8: MessageTracker clean-deletion hook

- [ ] When `MessageTracker` triggers a clean-deletion callback, also call `Store.Delete(target:messageID)` so the snapshot drops with the message.

### Task 1.9: Snapshot inspection API

- [ ] `GET /api/snapshots/<messageID>` in `internal/api/snapshots.go`.
- [ ] Protected by `x-poracle-secret` (existing middleware).
- [ ] 200 with `Snapshot` JSON on hit.
- [ ] 404 on miss.
- [ ] 503 if `[snapshots] enabled = false` (no store to query).

### Task 1.10: Metrics

- [ ] Register Prometheus counters per #108's metrics section: writes, reads (hit/miss/stale), bytes, sweep deletions, age histogram.
- [ ] Emit at the right points.

### Task 1.11: Unit tests

- [ ] `snapshots_test.go`: write → read round-trip; version-mismatch handling; sweep deletion; concurrent writes.
- [ ] Config loading test: `enabled = false` → no store dir; `enabled = true` → dir created on open.

### Task 1.12: Integration test

- [ ] End-to-end: enable snapshots, fire a synthetic pokemon webhook via the test endpoint, assert the snapshot is readable via the GET API with the expected fields populated.

**Done when:** all phase 1 tasks check off, `SMOKE.md` Phase 1 checklist passes manually, and unit + integration tests are green.

---

## Phase 2: Mute infrastructure + commands

**Goal:** `!mute` / `!unmute` commands working. Alerts filtered by active mutes. `!tracked` displays mute state.

**Demoable:** `!mute gym "Victoria Park Entrance" duration:30m` (or `!mute gym 1a15c33709c147fd85eeb9e6bb1e1c14.16`) → confirmation message. Next raid alert for that gym is dropped. `!tracked` lists the mute under "Property mutes." After 30 minutes (or `!unmute gym …`) the mute disappears and alerts resume.

### Task 2.1: Mute package skeleton

- [ ] Create `processor/internal/mute/`.
- [ ] `MuteEntry` struct per #109.
- [ ] `Store` interface: `Add(entry MuteEntry) error`, `RemoveByID(humanID, scopeType, scopeValue string) (bool, error)`, `RemoveAll(humanID string) (int, error)`, `List(humanID string) []MuteEntry`, `Match(humanID string, event MatchableEvent) bool`, `Sweep() int`.
- [ ] In-memory implementation using `map[humanID][]MuteEntry` with `sync.RWMutex`.

### Task 2.2: Background expiry sweep

- [ ] Goroutine that runs every minute, prunes expired entries.
- [ ] Emit metric per sweep.

### Task 2.3: Matcher integration

- [ ] Define `MatchableEvent` interface that exposes the fields mutes care about: `GymID() string`, `PokestopID() string`, `StationID() string`, `SpeciesID() int`, `AreaNames() []string`.
- [ ] Add `filterMuted(matchedUsers []MatchedUser, event MatchableEvent) []MatchedUser` helper in `processor/cmd/processor/` (or inside `mute/` if the matched-user shape stays in scope).
- [ ] Insert `filterMuted` call after `filterValidation` in each webhook handler: pokemon, raid, egg, maxbattle, invasion, quest, gym, nest, lure, fort_update.
- [ ] For UID-scope mutes (`tracking`): the filter checks against the matched user's tracking UID(s), not the event entity.

### Task 2.4: Unified `!mute` command parser

- [ ] New command in `internal/bot/commands/mute.go`. **All mute parsing lives here.**
- [ ] Argument shape: scope nouns are **positional** (they select which operation to perform). Parameter flags use the existing `key:value` style (matches the rest of the command vocabulary — `duration:`, `id:`, etc.).
- [ ] Forms accepted:
  - `!mute gym <name|id> [duration:<dur>]` — entity mute on gym. Multi-word names need quotes: `!mute gym "Victoria Park Entrance"`.
  - `!mute pokemon <name|id> [duration:<dur>]` — entity mute on species.
  - `!mute area <name> [duration:<dur>]` — entity mute on area. Quotes for multi-word names.
  - `!mute pokestop <id> [duration:<dur>]` — entity mute on pokestop.
  - `!mute station <id> [duration:<dur>]` — entity mute on max battle station.
  - `!mute everything [duration:<dur>]` — self-mute all alerts. Special positional token (no value).
  - `!mute id:<uid> [duration:<dur>]` — tracking-rule mute by UID (type-agnostic; `id:` is a parameter).
  - `!mute <type> id:<uid> [duration:<dur>]` — tracking-rule mute by UID within a type (`raid`, `egg`, etc.). Type is positional; `id:` is a parameter.
  - `!mute <pokemon-name>` — positional shorthand for `!mute pokemon <name>` (mirrors how bare `!untrack pikachu` defaults to the pokemon type).
- [ ] `duration:1h` / `30m` / `2d` flag parser; default to a configurable default (start at 1h).
- [ ] Confirmation reply via `msg.mute.added` naming what was muted (gym name if resolved, hex if not).

### Task 2.5: Per-type mute as a routing alias

- [ ] Add a `mute` subcommand to each per-type command file (`raid.go`, `egg.go`, `quest.go`, etc. under `internal/bot/commands/`).
- [ ] **Subcommand is a thin alias only** — its job is to prepend the type to the remaining args and delegate to the unified `!mute` handler. No per-type filter parser; no extra parameter logic.
- [ ] Example: `!raid mute id:12 duration:30m` → unified handler receives `["raid", "id:12", "duration:30m"]` → same flow as `!mute raid id:12 duration:30m`.
- [ ] Add the matching `unmute` subcommand the same way.

This is much simpler than the original sketch — the previous "reuse the remove filter parser" plan is dropped because tracking-rule mutes are UID-targeted, and `id:X` is what `!tracked` already exposes for that purpose.

### Task 2.6: Property-based mute handlers (in the unified parser)

Each positional scope noun in Task 2.4 maps to a small handler that resolves the value and writes the MuteEntry. Reuse existing resolver helpers from `internal/bot/commands/` so behaviour stays consistent with `!raid`, `!quest`, etc.:

- [ ] `gym` → `resolveGymRef` (existing — handles both hex IDs and names) → MuteEntry{ScopeType=gym, ScopeValue=<32-hex>.16}.
- [ ] `pokemon` → pokemon resolver (existing — used by `!track`) → MuteEntry{ScopeType=pokemon, ScopeValue=<dex-id>}.
- [ ] `area` → user-permitted-area validator (community-aware) → MuteEntry{ScopeType=area, ScopeValue=<area-name>}.
- [ ] `pokestop` → direct → MuteEntry{ScopeType=pokestop, ScopeValue=<id>}.
- [ ] `station` → direct → MuteEntry{ScopeType=station, ScopeValue=<id>}.
- [ ] `everything` (special positional token, no value) → MuteEntry{ScopeType=everything, ScopeValue=""}. Allowed only on DM target (per the existing target model).

### Task 2.7: Unmute commands

- [ ] `!unmute <same arg shape as !mute>` — symmetric removal.
- [ ] `!unmute all` — clear all mutes for the caller.
- [ ] `!unmute everything` — accepted as an alias for `!unmute all` (consistency with `!mute everything`).

### Task 2.7.1: Button-side scope name parity

- [ ] In the button scope dispatch (`buttonactions` package), update scope names: `species` → `pokemon`, `user` → `everything`. The underlying snapshot fields (`pokemon_id`, `Target`) read the same data — only the user-facing label changes.
- [ ] Update DTS schema validation to reject the old `species`/`user` strings with a warn pointing operators at the new names.

### Task 2.8: `!tracked` augmentation

- [ ] In `internal/bot/commands/tracked.go`: alongside each tracking rule, query the mute store for matching UID-scope mutes and append `🔇 muted (XXm left)` indicator.
- [ ] At the bottom of the output, add a "Property mutes" section listing entity-scope mutes.
- [ ] Format per #109's sketch.

### Task 2.9: i18n keys

- [ ] Add to `processor/internal/i18n/locale/en.json`:
  - `cmd.mute` (the command name itself for translation lookups)
  - `cmd.unmute`
  - `msg.mute.added` — "Muted {0} for {1}."
  - `msg.mute.removed` — "Unmuted {0}."
  - `msg.mute.none_found` — "No active mute for {0}."
  - `msg.mute.cleared_all` — "Cleared {0} active mutes."
- [ ] Add stub entries to all other locale files (translators will fill in).

### Task 2.10: Slash command equivalents

- [ ] If `slash-commands-design` provides a registration pattern, register `/mute` and `/unmute` with subcommand structure matching the chat forms.
- [ ] If the slash infrastructure isn't ready, skip this task — it can land as a follow-up.

### Task 2.11: Metrics

- [ ] `poracle_mute_entries_active` gauge.
- [ ] `poracle_mute_hits_total{scope}` counter (incremented in `filterMuted`).

### Task 2.12: Tests

- [ ] Unit tests for `mute.Store`: Add/Remove/List/Match/Sweep, including profile-scope semantics.
- [ ] `filterMuted` tests per webhook type: event matches mute → user dropped; event doesn't match → user retained.
- [ ] Command parser tests: each form (`!mute pikachu`, `!mute raid id:12`, `!raid mute id:12`, `!mute gym X 1h`, etc.).
- [ ] `!tracked` rendering test: tracked rules + entity mutes + UID mutes all displayed correctly.

**Done when:** SMOKE.md Phase 2 passes, tests green, `!tracked` integration looks right.

---

## Phase 3: Buttons end-to-end

**Goal:** Buttons declared in DTS → attached to Discord alerts → clicks dispatch actions or render responses.

**Demoable:** Add the Quick Start example from #109 (one mute button on raid). Fire a raid alert in a DM. Click the button. Mute applied; ephemeral confirmation. `!tracked` shows the mute.

### Task 3.1: DTS schema extension — buttons array

- [ ] In `internal/dts/templates.go`: add `Buttons []ButtonDef` to the entry struct (JSON loader picks it up).
- [ ] `ButtonDef` struct with all fields from #109 (id, label, style, response_template_id / response_template_inline / response_text / action, scope, params, applies_to, show_if, visible_to).
- [ ] Validation at load time: exactly one of response-field / action; scope/action/applies_to/visible_to are known values; show_if parses as Handlebars.
- [ ] Validation errors → warn + skip the entry (or skip just the button, leaving the rest of the entry intact — prefer the latter so an operator's broken button doesn't kill their alerts).

### Task 3.2: `buttonResponse` template type

- [ ] Register `buttonResponse` as a recognised type in any type-list constant.
- [ ] Selection chain unchanged — the existing 6-priority chain handles the new type identically.
- [ ] DTS editor field discovery (`/api/dts/fields/buttonResponse`) returns the snapshot view shape.

### Task 3.3: Render path — emit components

- [ ] In `cmd/processor/render.go` and/or `internal/dts/renderer.go`: when the resolved entry has buttons AND `[snapshots] enabled = true`, build a Discord `components` array.
- [ ] Filter by `applies_to` against the destination's `TargetType`.
- [ ] Evaluate `show_if` against the resolved view; skip the button if falsy.
- [ ] Apply action-level defaults to `applies_to` if the button doesn't specify (mute/unsubscribe → `["dm"]`; render-style → `["any"]`).
- [ ] Build `custom_id = "poracle:btn:<messageID-placeholder>:<actionID>"` — the `<messageID-placeholder>` gets filled in by the sender after the API returns the actual message ID. (Alternative: pre-generate a stable interaction token; deferred unless the placeholder-substitution proves awkward.)

### Task 3.4: Components on edits

- [ ] In `delivery/discord.go`: the Edit path must include the new components array (from the edit job's render), not omit it. Without this, edited messages lose their buttons because Discord's edit semantics replace components by default.

### Task 3.5: Components gated by `[snapshots] enabled`

- [ ] If snapshots are disabled, the renderer logs `debug` and skips the components block entirely. DTS still loads without error — the operator can keep button definitions and turn them on later.

### Task 3.6: `InteractionCreate` handler

- [ ] In `internal/discordbot/interactions.go` (new file): register an `InteractionCreate` handler on the existing discordgo session.
- [ ] Filter for `MessageComponent` interaction type with `custom_id` prefix `poracle:btn:`.
- [ ] Parse `(messageID, actionID)`.
- [ ] Load snapshot from `snapshots.Store`. On miss → respond ephemeral `msg.button.expired`.
- [ ] Resolve button definition from currently-loaded DTS by `(actionID, snapshot.TemplateType, snapshot.Platform, snapshot.Language)`. On miss → ephemeral `msg.button.unavailable`.
- [ ] Check `applies_to` against `snapshot.TargetType`. On fail → ephemeral `msg.button.wrong_target`.
- [ ] Check `visible_to` against the clicker. On fail → ephemeral `msg.button.unauthorized`.
- [ ] Per-user click cooldown: in-memory map `(clicker, messageID, buttonID) → lastClick`. If within 5s → ephemeral `msg.button.cooldown`.
- [ ] Dispatch.

### Task 3.7: Action handler registry

- [ ] `internal/buttonactions/`: `Handler` interface with `Handle(ctx, snapshot, button, clicker) (ResponseMessage, error)`.
- [ ] Registry with `Register(name string, h Handler)` and `Get(name string)`.
- [ ] Init function called from `cmd/processor/main.go` registers the built-ins.

### Task 3.8: `mute` action handler

- [ ] Reads `button.Scope`, looks up corresponding field from `snapshot.View` (or `TrackingUIDs` for scope=tracking, or `MatchedAreas` for scope=area).
- [ ] Reads `button.Params["duration_min"]`.
- [ ] Reads `snapshot.Target` for the human ID.
- [ ] Calls `mute.Store.Add(...)`.
- [ ] Returns ephemeral confirmation message listing what was muted and for how long.

### Task 3.9: `unsubscribe` action handler

- [ ] Reads `button.Scope` (must be `tracking` — error if not).
- [ ] Reads `snapshot.TrackingUIDs`.
- [ ] Deletes the tracking rule(s) via the store layer.
- [ ] Triggers state reload.
- [ ] Returns ephemeral confirmation listing the deleted rules.

### Task 3.10: `redeliver` action handler

- [ ] Uses `snapshot.TemplateSelected` (or falls back to chain if entry is gone).
- [ ] Renders against `snapshot.View`.
- [ ] DM-targets the clicker. If clicker is not registered as a Poracle user (no DM channel) → ephemeral `msg.button.action_failed` with "register first."

### Task 3.11: `render` action handler

- [ ] Reads `button.Params["template_id"]` (special value `"$same"` uses `snapshot.TemplateSelected`).
- [ ] Looks up `buttonResponse` template by that id, platform, language.
- [ ] Renders against `snapshot.View`; returns ephemeral.

### Task 3.12: Response template rendering — three shapes

- [ ] `response_template_id` → look up `buttonResponse` entry; render against snapshot view; respond ephemeral.
- [ ] `response_template_inline` → use the inline template body string directly; render; respond ephemeral.
- [ ] `response_text` → render Handlebars against snapshot view; respond ephemeral as plain text (no JSON parse).

### Task 3.13: Per-clicker merge for direction-style fields

- [ ] If the clicker has their own location set, compute their `distance` / `bearing` from the event location and merge into the response view at render time.
- [ ] For channel snapshots (no per-user data in the saved view), this is the path that gives the clicker meaningful per-user info.

### Task 3.14: i18n for canonical errors

- [ ] Add the 6 `msg.button.*` keys from #109 to `processor/internal/i18n/locale/en.json` and stub entries to other locale files.

### Task 3.15: Metrics

- [ ] `poracle_button_clicks_total{template_type, template_id, button_id, result}` — the operator-visibility metric.
- [ ] `poracle_button_actions_total{action, result}`.

### Task 3.16: Tests

- [ ] DTS schema extension: parse buttons, validate, reject malformed.
- [ ] Render path: `applies_to` filtering, `show_if` evaluation, components gated on `[snapshots] enabled`.
- [ ] `InteractionCreate` handler: each error path (expired, unavailable, wrong_target, unauthorized, cooldown).
- [ ] Each action handler: success path + at least one error path.
- [ ] End-to-end synthetic test: button declared in DTS → fire test webhook → simulate interaction → action executes → ephemeral response.

**Done when:** SMOKE.md Phase 3 passes; the Quick Start example from #109 works end-to-end.

---

## Phase 4: TOML loader + editor round-trip

**Goal:** Operators can author DTS in TOML. The config editor round-trips JSON internally and preserves on-disk format with pre-write backup.

**Demoable:** Drop a TOML DTS file in `config/dts/` with a button. Reload DTS. Alert fires with the button. Click works. Open the same entry in the config editor — see it in the JSON form. Edit a field. Save. File is still TOML; backup `.bak` file holds the original.

### Task 4.1: TOML parser/loader extension

- [ ] In `internal/dts/templates.go`: extend the directory scan to pick up `*.toml`.
- [ ] Dispatch on file extension to either the existing JSON parser or the new TOML parser.
- [ ] TOML parser produces the same in-memory `Entry` struct.
- [ ] Use `github.com/BurntSushi/toml` (already a dep).

### Task 4.2: Error handling

- [ ] TOML syntax errors → warn with file path + parser error message; skip that file's entries; continue loading.
- [ ] Template body Handlebars compile errors → warn with entry id; skip the entry.
- [ ] `response_template_inline` compile errors → warn with `(entry id, button id)`; skip that button only.
- [ ] Match the existing JSON loader's error-message style.

### Task 4.3: Duplicate-conflict WARN

- [ ] After loading all user files (JSON + TOML in `config/dts/`), check for `(type, id, platform, language)` collisions within the user-files tier.
- [ ] WARN for each collision: which files, which key, which entry wins.
- [ ] Do NOT warn on `config/dts/*` overriding `config/dts.json` — that's intentional.
- [ ] Do NOT warn on user files overriding `fallbacks/dts.json` — intentional.

### Task 4.4: `source_format` field on entries

- [ ] `Entry` struct gains a `SourceFormat string` field (`"json"` or `"toml"`).
- [ ] `SourceFile string` field captures the originating filename.
- [ ] `/api/dts/templates` GET surfaces these fields for the editor's use.

### Task 4.5: JSON → TOML serialiser

- [ ] In `internal/dts/templates.go` (or a sibling file): function that takes an in-memory `Entry` and produces TOML bytes.
- [ ] Multi-line strings rendered as `"""..."""` for `template` and `response_template_inline`.
- [ ] `buttons` array rendered as `[[entry.buttons]]` blocks with `params` as inline tables.
- [ ] Comments NOT preserved (acknowledged tradeoff — backup file is the recovery path).

### Task 4.6: Editor save path with format dispatch

- [ ] `POST /api/dts/templates` (existing): read incoming JSON; check `source_format` field; if `"toml"`, serialise back to TOML; if `"json"`, current behaviour.
- [ ] Write to the original `SourceFile` (preserving file location).

### Task 4.7: Pre-write backup

- [ ] Before writing the rewritten file: copy the existing file to a backup using the existing rewrite-backup mechanism.
- [ ] Backup filename per the convention used elsewhere in the codebase (check `internal/api/config.go` or wherever the existing model lives — match it).

### Task 4.8: Editor extension for buttons

- [ ] The editor needs to render the new `buttons[]` vocabulary regardless of source format.
- [ ] Action: any backend API changes needed? The editor frontend lives elsewhere (PoracleWeb); within this branch, ensure the API contract surfaces buttons in a discoverable way (via the existing `/api/dts/fields/{type}` enrichment if that's where field schemas come from).
- [ ] Add a `/api/dts/actions` endpoint listing registered action handlers and their required `params` so the editor can render appropriate forms.

### Task 4.9: Examples

- [ ] `examples/dts/buttons/raid-with-mute.json` — minimal JSON: one button, one action.
- [ ] `examples/dts/buttons/raid-with-pvp.toml` — TOML: conditional PVP button with `show_if`.
- [ ] `examples/dts/buttons/README.md` — operator-facing notes (when to use which format, how to enable).

### Task 4.10: Tests

- [ ] TOML loader: valid file loads → correct in-memory entries; broken syntax → warn + continue; partial corruption (one bad button) → skip that button only.
- [ ] Duplicate-conflict WARN: simulate two TOML files with same key → WARN logged; entry from last-loaded file wins.
- [ ] JSON → TOML round-trip: load TOML → serialise back → reload → equivalent in-memory shape (comments lost is expected, content identical).
- [ ] Pre-write backup: save a TOML entry → backup file exists with original content.

**Done when:** SMOKE.md Phase 4 passes; TOML examples work end-to-end; editor save preserves TOML format.

---

## Phase 5: Documentation + polish

**Goal:** External-facing docs updated. Internal docs reflect the new infrastructure.

### Task 5.1: CLAUDE.md updates

- [ ] New section: snapshot store (path, opt-in, lifecycle).
- [ ] New section: mute infrastructure (in-memory, `filterMuted` placement, profile semantics).
- [ ] New section: button actions (registry, scope dispatch, action handlers).
- [ ] New section: TOML DTS (loader, format preservation).
- [ ] Update the existing DTS Template System section with `buttonResponse` and `buttons[]`.

### Task 5.2: DTS.md updates

- [ ] New top-level "Buttons" section: vocabulary, response shapes, action types, `applies_to` defaults, `show_if`, `visible_to`.
- [ ] New top-level "TOML format" section: file convention, body-as-string model, side-by-side examples.
- [ ] Update the "Template selection chain" section to mention `buttonResponse`.

### Task 5.3: README.md updates (project root)

- [ ] Brief operator-facing summary: "buttons are available; opt in via `[snapshots] enabled = true`; see DTS.md for the vocabulary."
- [ ] One sentence on mute commands: "`!mute` and `!unmute` work alongside the existing tracking commands."

### Task 5.4: Examples polish

- [ ] Each `examples/dts/buttons/` example file: header comment, link back to DTS.md, working JSON or TOML.
- [ ] Verify each example loads cleanly via DTS reload.

### Task 5.5: Manual smoke run

- [ ] Walk through SMOKE.md end-to-end on a clean dev environment.
- [ ] Fix anything that surfaces.

### Task 5.6: Code review pass

- [ ] Self-review all phase commits.
- [ ] Run the full test suite.
- [ ] Run linters / type checks (`go vet`, `staticcheck` if used).

**Done when:** docs cross-link cleanly, examples work, SMOKE.md passes top-to-bottom.

---

## Cross-cutting concerns

### i18n keys touched in this branch

- `cmd.mute`, `cmd.unmute` (command names for translation).
- `msg.mute.added`, `msg.mute.removed`, `msg.mute.none_found`, `msg.mute.cleared_all`.
- `msg.button.expired`, `msg.button.unavailable`, `msg.button.wrong_target`, `msg.button.unauthorized`, `msg.button.action_failed`, `msg.button.cooldown`.

All land in `processor/internal/i18n/locale/en.json` with stub keys in other locale files. Translator pickup is a separate process.

### Metrics touched

- `poracle_snapshot_writes_total{result}`, `poracle_snapshot_reads_total{result}`, `poracle_snapshot_store_bytes`, `poracle_snapshot_sweep_deletions_total`, `poracle_snapshot_age_seconds`.
- `poracle_mute_entries_active`, `poracle_mute_hits_total{scope}`.
- `poracle_button_clicks_total{template_type, template_id, button_id, result}`, `poracle_button_actions_total{action, result}`.

### Tests

Each phase has its own unit + integration test set. Branch-level:

- Run `go test ./...` after each phase.
- The integration test from Phase 1 verifies snapshot persistence end-to-end.
- The end-to-end button test in Phase 3 exercises the full interaction loop synthetically.

### Risks and watch items

- **Edit-path components.** The trickiest part of the refactor. If components aren't included on `Edit` calls, edited messages silently lose their buttons. Test this explicitly with the raid-rsvp flow (egg → raid → rsvpChanges).
- **MessageTracker callback timing.** Snapshot deletion piggybacks on clean-deletion; make sure the callback is invoked even for non-clean messages at TTL expiry, or rely on the safety sweep.
- **Action handler error contracts.** If a handler panics, the interaction must still respond within 3 seconds. Wrap dispatch in a panic recover with an ephemeral fallback.
- **DTS reload races.** Reload while a button click is in flight: the click handler looks up against current DTS, so a freshly-reloaded missing entry returns `msg.button.unavailable`. Test this race.
- **Pogreb instance separation.** The snapshot store should be a separate pogreb instance from geocache. Don't share — they have different working sets and sweep cadences.

---

## Sequencing

Build vertically per phase. Each phase ends in a demoable state:

1. **Phase 1 ships:** snapshots opt-in working; nothing user-visible yet beyond the GET API.
2. **Phase 2 ships:** mute commands work; alerts get filtered; `!tracked` shows mutes. Buttons not yet attached.
3. **Phase 3 ships:** buttons attached to alerts; clicks dispatch.
4. **Phase 4 ships:** TOML files work; editor round-trips preserve format.
5. **Phase 5 ships:** docs current; examples polished.

Within a phase, tasks are mostly serial — Task N typically depends on N-1 within the phase, with parallelism only at the leaves (tests can be written alongside code).

If timeboxing, Phase 1 + 2 + 3 is the minimum viable launch (buttons work; TOML can come later). Phase 4 can split off into a separate PR within the same branch if reviewers want it sequenced separately.
