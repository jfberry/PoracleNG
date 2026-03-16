---
name: new-webhook
description: Generate a task checklist for adding a new webhook type to PoracleNG. Use when planning implementation of a new webhook type across processor, alerter, database, API, and commands.
user-invocable: true
argument-hint: [webhook-type-name] [optional-reference-pr-url]
allowed-tools: Read, Grep, Glob, Bash(gh *), WebFetch, TaskCreate
---

# Add New Webhook Type to PoracleNG

Generate a comprehensive implementation checklist for adding a new webhook type to PoracleNG.

**Webhook type name:** $0
**Reference PR (optional):** $1

## Instructions

1. If a reference PR URL is provided ($1), fetch the PR diff and description to understand what fields and logic are involved. Use this to fill in type-specific details in the checklist.

2. Examine the existing codebase for current patterns. Look at a similar existing type (raid, invasion, quest) to confirm the checklist items are still accurate.

3. Create tasks for each item below, grouped by component. Substitute the actual webhook type name ($0) throughout. If the reference PR reveals additional steps, add them.

4. Present the full checklist to the user for review before creating tasks.

## Checklist Template

### Database Migration
- [ ] Create `processor/internal/db/migrations/000XXX_add_$0.up.sql` — table with `uid` (auto-increment PK), `id` (FK→humans), `profile_no`, `ping`, `clean`, `distance`, `template`, plus type-specific columns. Include FK constraint with CASCADE delete
- [ ] Create matching `.down.sql` with DROP TABLE

### Processor — Webhook Receiving
- [ ] Add webhook struct to `processor/internal/webhook/types.go` with JSON tags matching Golbat schema
- [ ] Add `Process$0(raw json.RawMessage) error` to `Processor` interface in `processor/internal/webhook/receiver.go`
- [ ] Add case `"$0"` in `ServeHTTP()` switch in `processor/internal/webhook/receiver.go`

### Processor — DB & State
- [ ] Create `processor/internal/db/$0.go` with tracking struct (db tags) and `Load$0()` function
- [ ] Add `$0` field to `AllData` struct in `processor/internal/db/loader.go`
- [ ] Add `Load$0()` call in `LoadAll()` in `processor/internal/db/loader.go`
- [ ] Add `$0` field to `State` struct in `processor/internal/state/state.go`
- [ ] Update `processor/internal/state/loader.go` to assign loaded data to state and log count

### Processor — Matching
- [ ] Create `processor/internal/matching/$0.go` with data struct and matcher
- [ ] Implement `Match()` method: check geofence areas, iterate subscriptions, filter by fields, call `ValidateHumansGeneric()`

### Processor — Handler & Enrichment
- [ ] Create `processor/cmd/processor/$0.go` with `Process$0()` method on `ProcessorService` — worker pool, parse JSON, duplicate check, match, send
- [ ] Create `processor/internal/enrichment/$0.go` with enrichment method returning template variables (times, location, weather, etc.)

### Processor — Wiring
- [ ] Add `$0Matcher` field to `ProcessorService` struct in `processor/cmd/processor/main.go`
- [ ] Initialize matcher in `NewProcessorService()` in `processor/cmd/processor/main.go`

### Processor — Config (if needed)
- [ ] Add config struct/fields to `processor/internal/config/config.go`
- [ ] Add section to `config/config.example.toml`

### Alerter — Controller
- [ ] Create `alerter/src/controllers/$0.js` extending Controller base class
- [ ] Implement `handleMatched()` — build DTS data, map URLs, user filtering
- [ ] Instantiate controller in `alerter/src/app.js`
- [ ] Add to `allControllers` array in `alerter/src/app.js`
- [ ] Add `case '$0'` in postMatched handler in `alerter/src/app.js`

### Alerter — API Tracking Endpoints
- [ ] Create `alerter/src/routes/apiTracking$0.js` with CRUD:
  - `GET /api/tracking/$0/:id` — list user's trackings
  - `POST /api/tracking/$0/:id` — add/update trackings (validate fields, detect duplicates, batch insert/update, trigger reload)
  - `DELETE /api/tracking/$0/:id/byUid/:uid` — delete single tracking
  - `POST /api/tracking/$0/:id/delete` — batch delete by UIDs
- [ ] Register route in `alerter/src/app.js`

### Alerter — Command Logic (poracleMessage)
- [ ] Create `alerter/src/lib/poracleMessage/commands/$0.js` with `exports.run(client, msg, args, options)` — parse args, validate, DB insert/update/delete, reply to user. This is the platform-agnostic command implementation
- [ ] Add `$0RowText()` helper to `alerter/src/lib/poracleMessage/commands/tracked.js` for display formatting
- [ ] Add `$0` as a recognised command in `alerter/src/lib/poracleMessage/commands/script.js`
- [ ] Add `$0` to backup/restore in `alerter/src/lib/poracleMessage/commands/backup.js` (selectAllQuery + id/uid strip)
- [ ] Add `$0` delete to `alerter/src/lib/poracleMessage/commands/unregister.js`
- [ ] Add `$0` delete to profile delete in `alerter/src/lib/poracleMessage/commands/profile.js`
- [ ] Add `$0` to profile `copyto` categories list in `alerter/src/lib/poracleMessage/commands/profile.js`
- [ ] Add `$0` test data and case to `alerter/src/lib/poracleMessage/commands/poracle-test.js`

### Alerter — Platform Command Wrappers
- [ ] Create Discord wrapper `alerter/src/lib/discord/commando/commands/$0.js` — requires poracleMessage command, wraps with PoracleDiscordState/Message, calls `commandLogic.run()`. Auto-registered by filename
- [ ] Create Telegram wrapper `alerter/src/lib/telegram/commands/$0.js` — requires poracleMessage command, wraps with PoracleTelegramState/Message, calls `commandLogic.run()`. Auto-registered by filename

### Alerter — Cleanup & Reconciliation
- [ ] Add `$0` deleteQuery to `alerter/src/lib/discord/commando/events/channelDelete.js` (cleanup when Discord channel is deleted)
- [ ] Add `$0` deleteQuery to `alerter/src/lib/discord/discordReconciliation.js` (user removal cleanup)
- [ ] Add `$0` to commandSecurity list in `alerter/src/lib/discord/discordReconciliation.js`
- [ ] Add `$0` deleteQuery to `alerter/src/lib/telegram/telegramReconciliation.js` (user removal cleanup)

### Alerter — API & Config Integration
- [ ] Add `$0` to `disabledHooks` list in `alerter/src/routes/apiConfig.js`
- [ ] Add `$0` to the combined tracking GET endpoint in `alerter/src/routes/apiTracking.js` (both single-profile and all-profiles queries)

### Alerter — DTS Templates & Test Data
- [ ] Add default DTS entries for `$0` (Discord + Telegram) in `config/defaults/dts.json`
- [ ] Add test data for `$0` in `config/defaults/testdata.json`
- [ ] Create tileserver template `tileservercache_templates/poracle-$0.json` (and night variant if applicable)

### Documentation & Config
- [ ] Add `disable_$0` option to `config/config.example.toml` under `[general]`
- [ ] Update README or docs if applicable

## Data Flow Reference

```
Golbat webhook → receiver.go (parse + route)
  → cmd/processor/$0.go (handler: dedup, match, enrich, send)
    → matching/$0.go (filter subscriptions against webhook data)
    → enrichment/$0.go (compute template variables)
  → webhook/sender.go → POST /api/matched to alerter
    → app.js postMatched → controllers/$0.js (format for delivery)
    → Discord/Telegram workers (send to users)

User commands → Discord/Telegram wrapper → poracleMessage/commands/$0.js → DB insert/update
API clients → /api/tracking/$0/:id → apiTracking$0.js → DB insert/update
Both → trigger state reload → processor picks up new subscriptions
```
