# DTS Alerter Removal Design

## Goal

Move all remaining DTS-dependent features from the alerter to the Go processor, allowing complete removal of DTS loading, Handlebars, and template rendering from the alerter.

## Changes

### 1. Template Metadata API (`GET /api/config/templates`)

Move from alerter to processor. The processor already has the `TemplateStore` with all loaded entries.

**Default response** (backward-compatible):
```json
{
  "status": "ok",
  "discord": {
    "monster": { "en": ["1", "2"], "%": ["3"] },
    "raid": { "en": ["1"] }
  },
  "telegram": { ... }
}
```

**With `?includeDescriptions=true`:**
```json
{
  "status": "ok",
  "discord": {
    "monster": {
      "en": [
        {"id": "1", "name": "Standard", "description": "Full alert with PVP and maps"},
        {"id": "2"}
      ]
    }
  }
}
```

- `name` and `description` are optional fields on DTS entries, omitted from response when absent
- Hidden templates (`hidden: true`) excluded
- Supersedes PR #39 — no raw template body exposure needed
- Registered on the processor HTTP mux, takes precedence over the alerter proxy

### 2. Help & Greeting Template Rendering API (`POST /api/dts/render`)

New processor endpoint for on-demand template rendering.

**Request:**
```json
{
  "type": "help",
  "id": "track",
  "platform": "discord",
  "language": "en",
  "view": { "prefix": "!" }
}
```

**Response (success):**
```json
{
  "status": "ok",
  "message": { "embed": { "fields": [...] } }
}
```

**Response (no template found):**
```json
{
  "status": "error",
  "error": "no template found for help/discord/track/en"
}
```

Uses existing `TemplateStore.Get()` selection chain and raymond rendering. The `view` context is whatever the caller passes — for help/greeting it's just `{prefix}`.

### 3. Config Checker at Go Startup

Integrated into `dts.NewRenderer()` / template loading:
- Log warnings for types that have no default template per platform
- Log template count summary: "DTS loaded: 25 templates (12 discord, 13 telegram)"
- Template compilation errors already logged by `TemplateStore.Get()`
- No separate checker binary or package — just startup validation

### 4. Alerter Changes

**Commands updated to use processor API:**
- `help.js` — calls `POST /api/dts/render` with type="help" (or type="greeting" for `!help` with no args)
- `poracle.js` — calls `POST /api/dts/render` with type="greeting" for welcome message
- Telegram `poracle.js` — same
- `info.js` — `!info dts` calls `GET /api/config/templates` and formats the response (replaces direct `client.dts` iteration)

**Reconciliation updated:**
- `discordReconciliation.js` — greeting on user gain calls processor API
- `telegramReconciliation.js` — greeting calls processor API

**Telegram help formatting stays in alerter** — the platform-specific logic that extracts `embed.fields` and sends as multiple text messages remains in the alerter's help command. It receives the rendered message object from the processor and formats it for Telegram.

**Removed from alerter:**

Files deleted:
- `lib/dtsloader.js` — DTS file loading
- `lib/configChecker.js` — template validation
- `util/upgradeDts.js` — obsolete migration utility

Files modified (remove dts/mustache/handlebars references):
- `app.js` — remove dts loading, `fastify.dts`, chokidar DTS watcher, dts parameter to constructors
- `lib/configFetcher.js` — remove dtsloader require, configChecker, dts from return tuple
- `routes/apiConfig.js` — remove `/api/config/templates` endpoint (keep `/api/config/poracleWeb`)
- `lib/discord/commando/index.js` — remove `this.dts`, `this.mustache`, `client.dts`, `client.mustache`, handlebars require, dts constructor param
- `lib/discord/commando/commands/poracle.js` — replace DTS greeting with processor API call
- `lib/discord/commando/events/guildMemberRemove.js` — remove `client.dts` parameter
- `lib/discord/commando/events/guildMemberUpdate.js` — remove `client.dts` parameter
- `lib/discord/discordReconciliation.js` — remove handlebars require, `this.dts`, `this.mustache`, dts constructor param; replace greeting with processor API call
- `lib/discord/poracleDiscordState.js` — remove `this.dts`, `this.mustache`
- `lib/poracleMessage/commands/help.js` — replace DTS lookup + compile with processor API call
- `lib/poracleMessage/commands/info.js` — `!info dts` calls processor `/api/config/templates`
- `lib/telegram/Telegram.js` — remove handlebars require, dts constructor param, dts in controller middleware
- `lib/telegram/commands/poracle.js` — replace DTS greeting with processor API call
- `lib/telegram/commands/start.js` — remove `controller.dts` parameter
- `lib/telegram/middleware/controller.js` — remove dts and mustache from ctx.state
- `lib/telegram/poracleTelegramState.js` — remove handlebars require, `this.dts`, `this.mustache`
- `lib/telegram/telegramReconciliation.js` — remove handlebars require, `this.dts`, `this.mustache`, dts constructor param; replace greeting with processor API call

npm dependencies to remove:
- `handlebars`
- `@budibase/handlebars-helpers` (already unused after controller removal)

## File Structure

### Processor (new/modified)
```
processor/internal/api/dts.go           — HandleTemplateConfig, HandleDTSRender endpoints
processor/internal/dts/templates.go     — Add TemplateMetadata() method to TemplateStore
processor/cmd/processor/main.go         — Register new endpoints
```

### Alerter (modified/removed)
```
alerter/src/routes/apiConfig.js         — Remove /api/config/templates, keep /api/config/poracleWeb
alerter/src/lib/poracleMessage/commands/help.js     — Call processor API
alerter/src/lib/poracleMessage/commands/info.js     — !info dts calls /api/config/templates
alerter/src/lib/discord/commando/commands/poracle.js — Call processor API
alerter/src/lib/telegram/commands/poracle.js         — Call processor API
alerter/src/lib/discord/discordReconciliation.js     — Call processor API for greeting
alerter/src/lib/telegram/telegramReconciliation.js   — Call processor API for greeting
alerter/src/lib/discord/commando/index.js            — Remove dts parameter
alerter/src/lib/telegram/Telegram.js                 — Remove dts parameter
alerter/src/app.js                                   — Remove DTS loading, chokidar, configChecker

alerter/src/lib/dtsloader.js            — Deleted
alerter/src/lib/configChecker.js        — Deleted
```

## DTS Entry Fields (updated)

```json
{
  "id": "1",
  "type": "monster",
  "platform": "discord",
  "language": "en",
  "default": true,
  "hidden": false,
  "name": "Standard Alert",
  "description": "Full Pokemon alert with PVP, maps, and weather",
  "template": { ... }
}
```

`name` and `description` are optional, for display in PoracleWeb template selection UI.
