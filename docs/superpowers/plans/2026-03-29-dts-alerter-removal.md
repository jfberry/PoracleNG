# DTS Alerter Removal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move all remaining DTS features to the Go processor and remove DTS/handlebars entirely from the alerter.

**Architecture:** Two new processor API endpoints (`GET /api/config/templates` and `POST /api/dts/render`) serve template metadata and on-demand rendering. The alerter commands (`help`, `poracle`, `info dts`) and reconciliation greeting code call these APIs instead of loading DTS locally. After migration, DTS loading, handlebars, configChecker, and dtsloader are removed from the alerter.

**Tech Stack:** Go (existing dts package), Node.js (HTTP client calls to processor API).

**Spec:** `docs/superpowers/specs/2026-03-29-dts-alerter-removal-design.md`

---

## File Structure

### Processor (new/modified)
```
processor/internal/api/dts.go              — HandleTemplateConfig, HandleDTSRender endpoints
processor/internal/dts/templates.go        — Add TemplateMetadata(), DTSEntry name/description fields
processor/cmd/processor/main.go            — Register new endpoints, add startup validation logging
```

### Alerter (modified)
```
alerter/src/app.js                         — Remove DTS loading, chokidar, fastify.dts, dts params
alerter/src/lib/configFetcher.js           — Remove dtsloader, configChecker, dts from return
alerter/src/routes/apiConfig.js            — Remove /api/config/templates endpoint
alerter/src/lib/poracleMessage/commands/help.js    — Call processor API
alerter/src/lib/poracleMessage/commands/info.js    — !info dts calls processor API
alerter/src/lib/discord/commando/index.js          — Remove dts, mustache
alerter/src/lib/discord/commando/commands/poracle.js — Call processor API for greeting
alerter/src/lib/discord/commando/events/guildMemberRemove.js — Remove client.dts param
alerter/src/lib/discord/commando/events/guildMemberUpdate.js — Remove client.dts param
alerter/src/lib/discord/discordReconciliation.js   — Call processor API for greeting
alerter/src/lib/discord/poracleDiscordState.js     — Remove dts, mustache
alerter/src/lib/telegram/Telegram.js               — Remove handlebars, dts
alerter/src/lib/telegram/commands/poracle.js       — Call processor API for greeting
alerter/src/lib/telegram/commands/start.js         — Remove controller.dts param
alerter/src/lib/telegram/middleware/controller.js   — Remove dts, mustache from ctx.state
alerter/src/lib/telegram/poracleTelegramState.js   — Remove handlebars, dts, mustache
alerter/src/lib/telegram/telegramReconciliation.js — Call processor API for greeting
```

### Alerter (deleted)
```
alerter/src/lib/dtsloader.js
alerter/src/lib/configChecker.js
alerter/src/util/upgradeDts.js
```

---

### Task 1: Processor API — Template Config Endpoint

**Files:**
- Create: `processor/internal/api/dts.go`
- Modify: `processor/internal/dts/templates.go`
- Modify: `processor/cmd/processor/main.go`

- [ ] **Step 1: Add name/description fields to DTSEntry**

In `processor/internal/dts/templates.go`, add `Name` and `Description` to `DTSEntry`:

```go
type DTSEntry struct {
    Type         string `json:"type"`
    ID           jsonID `json:"id"`
    Platform     string `json:"platform"`
    Language     string `json:"language"`
    Default      bool   `json:"default"`
    Hidden       bool   `json:"hidden"`
    Name         string `json:"name,omitempty"`
    Description  string `json:"description,omitempty"`
    Template     any    `json:"template"`
    TemplateFile string `json:"templateFile"`
}
```

Add a `TemplateMetadata` method to `TemplateStore` that returns the grouped metadata:

```go
type TemplateInfo struct {
    ID          string `json:"id"`
    Name        string `json:"name,omitempty"`
    Description string `json:"description,omitempty"`
}

// TemplateMetadata returns template metadata grouped by platform → type → language.
// When includeDescriptions is true, returns TemplateInfo objects; otherwise just ID strings.
func (ts *TemplateStore) TemplateMetadata(includeDescriptions bool) map[string]map[string]map[string]any
```

The method iterates `ts.entries`, skips hidden entries, and groups by platform → type → language (using `"%"` for entries with no language).

- [ ] **Step 2: Create HandleTemplateConfig**

Create `processor/internal/api/dts.go`:

```go
func HandleTemplateConfig(ts *dts.TemplateStore) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        includeDesc := r.URL.Query().Get("includeDescriptions") == "true"
        result := ts.TemplateMetadata(includeDesc)

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]any{
            "status":   "ok",
            "discord":  result["discord"],
            "telegram": result["telegram"],
        })
    }
}
```

- [ ] **Step 3: Create HandleDTSRender**

In the same file, add the on-demand render endpoint:

```go
type dtsRenderRequest struct {
    Type     string         `json:"type"`
    ID       string         `json:"id"`
    Platform string         `json:"platform"`
    Language string         `json:"language"`
    View     map[string]any `json:"view"`
}

func HandleDTSRender(ts *dts.TemplateStore) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req dtsRenderRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            // error response
            return
        }

        tmpl := ts.Get(req.Type, req.Platform, req.ID, req.Language)
        if tmpl == nil {
            // error: no template found
            return
        }

        result, err := tmpl.Exec(req.View)
        if err != nil {
            // error: render failed
            return
        }

        var message any
        json.Unmarshal([]byte(result), &message)

        json.NewEncoder(w).Encode(map[string]any{
            "status":  "ok",
            "message": message,
        })
    }
}
```

- [ ] **Step 4: Register endpoints in main.go**

Add to the HTTP mux setup:

```go
mux.HandleFunc("GET /api/config/templates", auth(api.HandleTemplateConfig(dtsRenderer.Templates())))
mux.HandleFunc("POST /api/dts/render", auth(api.HandleDTSRender(dtsRenderer.Templates())))
```

This requires exposing `TemplateStore` from `Renderer`. Add a `Templates() *TemplateStore` method to `Renderer`.

- [ ] **Step 5: Add startup validation logging**

In `dts.NewRenderer()` or `LoadTemplates()`, after loading entries, log a summary:

```go
log.Infof("DTS loaded: %d templates (%d discord, %d telegram)", total, discordCount, telegramCount)
```

And warn for any type that has no default template:

```go
for _, wantType := range []string{"monster", "raid", "egg", "quest", "invasion", "lure", "nest", "gym", "fort-update", "maxbattle", "weatherchange"} {
    // check if default exists for discord and telegram
}
```

- [ ] **Step 6: Build and test**

```bash
cd processor && go build ./cmd/processor && go test ./internal/dts/ -count=1
```

Test the endpoints with curl:
```bash
curl -s http://localhost:4200/api/config/templates -H "X-Poracle-Secret: hello" | python3 -m json.tool | head -20
curl -s http://localhost:4200/api/config/templates?includeDescriptions=true -H "X-Poracle-Secret: hello" | python3 -m json.tool | head -20
curl -s -X POST http://localhost:4200/api/dts/render -H "X-Poracle-Secret: hello" -H "Content-Type: application/json" -d '{"type":"greeting","platform":"discord","language":"en","view":{"prefix":"!"}}'
```

- [ ] **Step 7: Commit**

```bash
git add processor/internal/api/dts.go processor/internal/dts/templates.go processor/cmd/processor/main.go processor/internal/dts/renderer.go
git commit -m "feat: add /api/config/templates and /api/dts/render processor endpoints"
```

---

### Task 2: Update Alerter Commands — help, info dts, poracle greeting

**Files:**
- Modify: `alerter/src/lib/poracleMessage/commands/help.js`
- Modify: `alerter/src/lib/poracleMessage/commands/info.js`

These are the shared command logic files called from both Discord and Telegram wrappers. They use `client.config.processor.url` and `client.config.processor.headers` for API calls (same pattern as `ask.js`, `poracle-test.js`, `location.js`).

- [ ] **Step 1: Rewrite help.js**

Replace the local DTS lookup + handlebars compile with a processor API call:

```javascript
const axios = require('axios')

function getDtsFromProcessor(config, type, id, platform, language, prefix) {
    return axios.post(`${config.processor.url}/api/dts/render`, {
        type, id, platform, language,
        view: { prefix }
    }, {
        headers: { 'Content-Type': 'application/json', ...config.processor.headers },
        timeout: 5000
    })
}
```

The `getDts()` function becomes `getDtsFromProcessor()`. The `isHelpAvailable()` function can either call the API or be removed (simplify: always attempt the render, handle "not found" response).

The Telegram-specific formatting (extract `embed.fields`, send as multiple text messages) stays — it works on the rendered message object returned by the API.

- [ ] **Step 2: Update info.js `!info dts` case**

Replace the `client.dts` iteration with a processor API call:

```javascript
case 'dts': {
    if (msg.isFromAdmin) {
        try {
            const resp = await axios.get(`${client.config.processor.url}/api/config/templates`, {
                headers: client.config.processor.headers, timeout: 5000
            })
            const { data } = resp
            let s = 'Loaded DTS templates:\n'
            for (const [platform, types] of Object.entries(data)) {
                if (platform === 'status') continue
                for (const [type, langs] of Object.entries(types)) {
                    for (const [lang, ids] of Object.entries(langs)) {
                        for (const id of ids) {
                            s += `type: ${type} platform: ${platform} id: ${id} language: ${lang}\n`
                        }
                    }
                }
            }
            await msg.reply(s)
        } catch (err) {
            await msg.reply('Failed to fetch DTS info from processor')
        }
    }
    break
}
```

- [ ] **Step 3: Commit**

```bash
git add alerter/src/lib/poracleMessage/commands/help.js alerter/src/lib/poracleMessage/commands/info.js
git commit -m "feat: help and info dts commands call processor API instead of local DTS"
```

---

### Task 3: Update Greeting Code — Discord and Telegram

**Files:**
- Modify: `alerter/src/lib/discord/commando/commands/poracle.js`
- Modify: `alerter/src/lib/discord/discordReconciliation.js`
- Modify: `alerter/src/lib/telegram/commands/poracle.js`
- Modify: `alerter/src/lib/telegram/telegramReconciliation.js`

All four files do the same thing: find a greeting DTS template, compile with `{prefix}`, send the result. Replace with a processor API call.

- [ ] **Step 1: Update Discord poracle.js greeting**

Replace the DTS lookup + `client.mustache.compile(JSON.stringify(greetingDts.template))` block with:

```javascript
try {
    const resp = await axios.post(`${client.config.processor.url}/api/dts/render`, {
        type: 'greeting', platform: 'discord', language,
        view: { prefix: util.prefix }
    }, { headers: { 'Content-Type': 'application/json', ...client.config.processor.headers }, timeout: 5000 })
    if (resp.data.status === 'ok' && resp.data.message) {
        await msg.reply(resp.data.message)
    }
} catch (err) {
    client.log.error('Failed to render greeting:', err.message)
}
```

- [ ] **Step 2: Update Discord reconciliation greeting**

Same pattern in `discordReconciliation.js` — replace the `this.dts.find(...)` + `this.mustache.compile(...)` with an API call. The reconciliation has access to config via `this.config`.

- [ ] **Step 3: Update Telegram poracle.js greeting**

Same pattern. Replace `controller.dts.find(...)` + `client.mustache.compile(...)`.

- [ ] **Step 4: Update Telegram reconciliation greeting**

Same pattern in `telegramReconciliation.js`.

- [ ] **Step 5: Commit**

```bash
git add alerter/src/lib/discord/commando/commands/poracle.js \
       alerter/src/lib/discord/discordReconciliation.js \
       alerter/src/lib/telegram/commands/poracle.js \
       alerter/src/lib/telegram/telegramReconciliation.js
git commit -m "feat: greeting commands and reconciliation call processor API for DTS rendering"
```

---

### Task 4: Remove DTS/Handlebars from Alerter Infrastructure

This is the big cleanup task. Remove all DTS loading, handlebars imports, and dts parameters from constructor chains.

**Files to modify:**
- `alerter/src/app.js`
- `alerter/src/lib/configFetcher.js`
- `alerter/src/routes/apiConfig.js`
- `alerter/src/lib/discord/commando/index.js`
- `alerter/src/lib/discord/commando/events/guildMemberRemove.js`
- `alerter/src/lib/discord/commando/events/guildMemberUpdate.js`
- `alerter/src/lib/discord/poracleDiscordState.js`
- `alerter/src/lib/telegram/Telegram.js`
- `alerter/src/lib/telegram/commands/start.js`
- `alerter/src/lib/telegram/middleware/controller.js`
- `alerter/src/lib/telegram/poracleTelegramState.js`

**Files to delete:**
- `alerter/src/lib/dtsloader.js`
- `alerter/src/lib/configChecker.js`
- `alerter/src/util/upgradeDts.js`

- [ ] **Step 1: Clean app.js**

Read the file carefully. Remove:
- The `dts` variable from the destructured import of `configFetcher`
- `fastify.decorate('dts', dts)`
- The `dts` parameter passed to `DiscordCommando`, `TelegramWorker` (both instances), `TelegramReconciliation`, `DiscordReconciliation` constructors
- The chokidar DTS file watcher block (watching `dts.json` and `dts/` directory)
- The `dts.splice(0, dts.length, ...newDts)` reload logic

Keep everything else (Discord/Telegram workers, delivery queues, routes, geofence watcher).

- [ ] **Step 2: Clean configFetcher.js**

Remove:
- `require('./dtsloader')`
- `require('./configChecker')`
- `dts` variable and `dtsLoader.readDtsFiles()` call
- `configChecker.checkDts(dts, config)` call
- `dts` from the return tuple

- [ ] **Step 3: Clean apiConfig.js**

Remove the entire `/api/config/templates` endpoint (the `fastify.get('/api/config/templates', ...)` block). Keep `/api/config/poracleWeb`.

- [ ] **Step 4: Clean Discord commando/index.js**

Remove:
- `const mustache = require('handlebars')`
- `dts` from constructor parameter
- `this.dts = dts`
- `this.client.dts = this.dts`
- `this.client.mustache = mustache`

- [ ] **Step 5: Clean Discord events (guildMemberRemove.js, guildMemberUpdate.js)**

Remove `client.dts` from the arguments passed to reconciliation constructors.

- [ ] **Step 6: Clean Discord poracleDiscordState.js**

Remove `this.dts = client.dts` and `this.mustache = client.mustache`.

- [ ] **Step 7: Clean Discord discordReconciliation.js**

Remove:
- `const mustache = require('handlebars')`
- `dts` from constructor
- `this.dts = dts`
- `this.mustache = mustache`

(The greeting API call was added in Task 3.)

- [ ] **Step 8: Clean Telegram Telegram.js**

Remove:
- `const mustache = require('handlebars')`
- `dts` from constructor parameter
- `dts` from the controller middleware call: `.use(controller(query, scannerQuery, dts, ...))`  →  `.use(controller(query, scannerQuery, ...))`

- [ ] **Step 9: Clean Telegram middleware/controller.js**

Remove `dts` and `mustache` from the function parameters and `ctx.state.controller` assignment.

- [ ] **Step 10: Clean Telegram poracleTelegramState.js**

Remove:
- `const mustache = require('handlebars')`
- `this.dts = ctx.state.controller.dts`
- `this.mustache = mustache`

- [ ] **Step 11: Clean Telegram telegramReconciliation.js**

Remove:
- `const mustache = require('handlebars')`
- `dts` from constructor
- `this.dts = dts`
- `this.mustache = mustache`

(The greeting API call was added in Task 3.)

- [ ] **Step 12: Clean Telegram commands/start.js**

Remove `controller.dts` from the arguments passed to reconciliation.

- [ ] **Step 13: Delete obsolete files**

```bash
rm alerter/src/lib/dtsloader.js
rm alerter/src/lib/configChecker.js
rm alerter/src/util/upgradeDts.js
```

- [ ] **Step 14: Verify alerter starts**

Start the alerter and check for import errors or missing references. Fix any missed references.

- [ ] **Step 15: Commit**

```bash
git add -A
git commit -m "feat: remove all DTS/handlebars from alerter — processor handles all rendering"
```

---

### Task 5: Remove npm Dependencies

- [ ] **Step 1: Remove handlebars packages**

```bash
cd alerter && npm uninstall handlebars @budibase/handlebars-helpers
```

- [ ] **Step 2: Verify alerter still starts**

```bash
cd alerter && node src/app.js
```

Check no `require('handlebars')` remains anywhere.

- [ ] **Step 3: Commit**

```bash
git add alerter/package.json alerter/package-lock.json
git commit -m "chore: remove handlebars and @budibase/handlebars-helpers npm dependencies"
```

---

### Task 6: Integration Testing

- [ ] **Step 1: Test template config API**

```bash
curl -s http://localhost:4200/api/config/templates -H "X-Poracle-Secret: hello"
curl -s "http://localhost:4200/api/config/templates?includeDescriptions=true" -H "X-Poracle-Secret: hello"
```

- [ ] **Step 2: Test DTS render API**

```bash
curl -s -X POST http://localhost:4200/api/dts/render \
  -H "X-Poracle-Secret: hello" -H "Content-Type: application/json" \
  -d '{"type":"greeting","platform":"discord","language":"en","view":{"prefix":"!"}}'
```

- [ ] **Step 3: Test Discord commands**

- `!help` — should show greeting
- `!help track` — should show track help
- `!info dts` — should list all templates
- `!poracle` (new registration) — should show greeting

- [ ] **Step 4: Test alert rendering still works**

- Verify pokemon, raid, invasion alerts render correctly
- Verify static maps, TTH, emoji all present

- [ ] **Step 5: Commit any fixes**
