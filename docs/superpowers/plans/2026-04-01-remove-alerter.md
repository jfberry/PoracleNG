# Remove Alerter — Final Migration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove the Node.js alerter entirely. All functionality served by the Go processor. Single process, single port.

**Architecture:** Processor serves all API endpoints, runs Discord+Telegram bots, handles all delivery. No proxy, no inter-process HTTP calls.

**Tech Stack:** Go (existing processor), discordgo (existing), go-telegram-bot-api (existing)

---

## What Remains in the Alerter

7 API endpoints still served by the alerter via the processor's catch-all proxy:

| Endpoint | Complexity | Consumer |
|----------|-----------|----------|
| `GET /api/config/poracleWeb` | Easy | PoracleWeb frontend |
| `GET /api/masterdata/monsters` | Easy | PoracleWeb frontend |
| `GET /api/masterdata/grunts` | Easy | PoracleWeb frontend |
| `POST /api/postMessage` | Trivial | Legacy — already proxies to processor |
| `GET /api/humans/{id}/roles` | Medium | PoracleWeb role management |
| `POST /api/humans/{id}/roles/add/{roleId}` | Medium | PoracleWeb role management |
| `POST /api/humans/{id}/roles/remove/{roleId}` | Medium | PoracleWeb role management |
| `GET /api/humans/{id}/getAdministrationRoles` | Medium | PoracleWeb delegated admin |

## What Must Be Removed from the Processor

Several processor components exist only to communicate with the alerter:

| Component | Location | Purpose |
|-----------|----------|---------|
| `sendConfirmation()` | `api/tracking.go:72-128` | POSTs to alerter `/api/postMessage` |
| `postMessageToAlerter()` | `cmd/processor/helpers.go:270-296` | POSTs to alerter `/api/postMessage` |
| `NewAlerterProxy()` | `api/proxy.go` | Reverse proxy for unhandled `/api/*` |
| `webhook.Sender` | `internal/webhook/sender.go` | Dead code — forwards matched results to alerter |
| `AlerterURL` config | `config/config.go:121` | Required alerter URL |
| `alerter_url` validation | `config/config.go:541-542` | Fails startup if not set |
| Alerter-only downloads | `resources/download.go:39-42,54-78,128-159` | poracle-v2 monsters.json, grunts.json, enRefMerged locales |

## Tasks

### Task 1: Port `GET /api/config/poracleWeb`

**Files:**
- Create: `processor/internal/api/config.go`

Returns aggregated config for the PoracleWeb frontend. Read values from `*config.Config`:

```go
func HandleConfigPoracleWeb(cfg *config.Config) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        resp := map[string]any{
            "status":  "ok",
            "version": version,
            "locale":  cfg.General.Locale,
            "prefix":  cfg.Discord.Prefix,
            "pvpFilterMaxRank":      cfg.PVP.FilterMaxRank,
            "pvpFilterGreatMinCP":   cfg.PVP.FilterGreatMinCP,
            "pvpFilterUltraMinCP":   cfg.PVP.FilterUltraMinCP,
            "pvpFilterLittleMinCP":  cfg.PVP.FilterLittleMinCP,
            "pvpLittleLeagueAllowed": true,
            "pvpCaps":               cfg.PVP.LevelCaps,
            "pvpRequiresMinCp":      cfg.PVP.ForceMinCP && cfg.PVP.DataSource == "webhook",
            "defaultPvpCap":         cfg.Tracking.DefaultUserTrackingLevelCap,
            "defaultTemplateName":   cfg.General.DefaultTemplateName,
            "admins": map[string]any{
                "discord":  cfg.Discord.Admins,
                "telegram": cfg.Telegram.Admins,
            },
            "maxDistance":     cfg.Tracking.MaxDistance,
            "defaultDistance": cfg.Tracking.DefaultDistance,
            "everythingFlagPermissions": cfg.Tracking.EverythingFlagPermissions,
            "gymBattles": cfg.Tracking.EnableGymBattle,
            // disabledHooks computed from config disable flags
        }
        json.NewEncoder(w).Encode(resp)
    }
}
```

Check the alerter's response carefully for all fields that PoracleWeb uses. Some config fields may not yet exist in the Go config struct — add them.

### Task 2: Port `GET /api/masterdata/monsters` and `/grunts`

**Files:**
- Create: `processor/internal/api/masterdata.go`

**Monsters:** PoracleWeb expects the poracle-v2 format (array of objects with `name`, `id`, `types`, `forms`). The processor has the raw masterfile in `gamedata.GameData`. Build the poracle-v2 format from raw data + translations:

```go
func HandleMasterdataMonsters(gd *gamedata.GameData, tr *i18n.Bundle) http.HandlerFunc
```

For each pokemon in `gd.Monsters`: emit `{id, name, types: [{name, id}], forms: [{name, id}]}` using English translations from the bundle (`poke_{id}`, `form_{formId}`, `poke_type_{typeId}`).

**Grunts:** Return grunt data from `gd.Grunts` in the format PoracleWeb expects. Check what `fastify.GameData.grunts` returns — it's the `resources/data/grunts.json` content (poracle-v2 format). Build equivalent from `gd.Grunts` (classic.json format).

### Task 3: Port Discord Role Management (3 endpoints)

**Files:**
- Create: `processor/internal/api/roles.go`

These endpoints require the Discord bot session (discordgo) to query guild members and manage roles. The processor's discordgo session is in `discordbot.Bot`.

**`GET /api/humans/{id}/roles`** — Lists available roles across all configured guilds:
1. Look up human by ID, verify `type == "discord:user"`
2. For each guild in `cfg.Discord.Guilds`:
   - Fetch guild roles via `session.GuildRoles(guildID)`
   - Fetch member via `session.GuildMember(guildID, userID)`
   - Return roles with `has: true/false` for the member
3. Filter to only `role_subscriptions` roles if configured

**`POST /api/humans/{id}/roles/add/{roleId}`** and **`/remove/{roleId}`**:
1. Look up human, verify discord:user
2. Call `session.GuildMemberRoleAdd(guildID, userID, roleID)` or `...RoleRemove(...)`
3. Return success/failure

**`GET /api/humans/{id}/getAdministrationRoles`**:
1. Look up human
2. Check `delegated_admin` config for channel tracking, webhook tracking, user tracking permissions
3. Return permission flags based on user's guild membership and roles

**Dependency:** These handlers need access to the discordgo session. Add `DiscordSession *discordgo.Session` to a deps struct, or pass the `discordbot.Bot` reference.

### Task 4: Replace `sendConfirmation()` — Use Dispatcher

**Files:**
- Modify: `processor/internal/api/tracking.go` — replace `sendConfirmation` with dispatcher-based delivery
- Modify: `processor/internal/api/tracking*.go` (10 files) — update call sites

Currently `sendConfirmation` POSTs JSON to the alerter's `/api/postMessage`. Replace with direct `delivery.Dispatcher.Dispatch()`:

```go
func sendConfirmation(deps *TrackingDeps, human *db.HumanAPI, message, language string) {
    if message == "" || deps.Dispatcher == nil {
        return
    }
    job := delivery.Job{
        Target:   human.ID,
        Type:     human.Type,
        Name:     human.Name,
        Platform: platformFromType(human.Type),
        Message:  map[string]any{"content": message},
        TTH:      3600, // 1 hour
    }
    deps.Dispatcher.Dispatch(job)
}
```

Add `Dispatcher *delivery.Dispatcher` to `TrackingDeps`.

### Task 5: Replace `postMessageToAlerter()` — Use Dispatcher

**Files:**
- Modify: `processor/cmd/processor/helpers.go` — replace `postMessageToAlerter` with dispatcher call
- Modify: `processor/cmd/processor/profiles.go` — update call site

Same pattern as Task 4. The rate-limit notifications and profile scheduler messages use `postMessageToAlerter` to send user-facing messages. Replace with `Dispatcher.Dispatch()`.

### Task 6: Remove `POST /api/postMessage` Compatibility

**Files:**
- Modify: `processor/cmd/processor/main.go` — add route alias

Register `POST /api/postMessage` as an alias for `POST /api/deliverMessages` so any external callers (legacy scripts, etc.) continue to work:

```go
mux.HandleFunc("POST /api/postMessage", auth(api.HandleDeliverMessages(proc.dispatcher)))
```

### Task 7: Remove Alerter Proxy and Dead Code

**Files:**
- Delete: `processor/internal/api/proxy.go`
- Delete: `processor/internal/webhook/sender.go` (dead code — never sends data)
- Modify: `processor/cmd/processor/main.go` — remove `mux.Handle("/api/", ...)` proxy, remove `webhook.NewSender(...)`, remove `alerterClient`
- Modify: `processor/internal/api/tracking.go` — remove old `sendConfirmation` HTTP POST code, remove `AlerterURL` and `APISecret` from `TrackingDeps`
- Modify: `processor/internal/config/config.go` — make `alerter_url` optional (no longer required), keep for backward compat but don't fail if missing
- Modify: `processor/cmd/processor/helpers.go` — remove `postMessageToAlerter`, remove `postMessage` struct

### Task 8: Remove Alerter-Only Resource Downloads

**Files:**
- Modify: `processor/internal/resources/download.go` — remove:
  - `downloadGameMaster()` — fetches poracle-v2 `monsters.json`, `grunts.json`, `items.json` etc. to `resources/data/`
  - `downloadLocales()` — fetches enRefMerged English-as-key locale files to `resources/locale/`
  - Keep: `downloadRawMaster()` (processor's raw masterfile), `downloadGrunts()` (classic.json), `downloadGameLocales()` (identifier-key locales)

**Note:** `resources/data/util.json` is used by BOTH processor and alerter. The processor loads it via `gamedata.LoadUtilData`. Verify the download of `util.json` is NOT removed — it comes from the raw masterfile download, not from `downloadGameMaster`.

### Task 9: Remove Alerter from start.sh and Dockerfile

**Files:**
- Modify: `start.sh` — remove alerter startup, health check wait, graceful kill. Single process only.
- Modify: `Dockerfile` — remove Node.js builder stage, remove `npm install`, remove alerter source copy, remove port 3031 expose. Single-stage Go build + Alpine runtime.
- Modify: `CLAUDE.md` — update architecture section to reflect single-process model

### Task 10: Delete Alerter Directory

**Files:**
- Delete: `alerter/` — entire directory (src/, package.json, node_modules, etc.)
- Delete: `fallbacks/` — bundled defaults that were alerter-specific (check if processor still needs any)
- Modify: `.gitignore` — remove alerter-specific entries

**Before deleting:** Verify that no processor code imports or reads from `alerter/` directory. The i18n load chain was already updated to not load `alerter/locale/*.json`.

---

## Implementation Order

1. **Task 1** (config endpoint) — quick win, no dependencies
2. **Task 2** (masterdata endpoints) — quick win, uses existing gamedata
3. **Task 4** (replace sendConfirmation) — critical, enables Task 7
4. **Task 5** (replace postMessageToAlerter) — critical, enables Task 7
5. **Task 6** (postMessage alias) — trivial
6. **Task 3** (Discord role endpoints) — medium complexity, needs discordgo session wiring
7. **Task 7** (remove proxy + dead code) — depends on Tasks 1-6
8. **Task 8** (remove alerter downloads) — independent
9. **Task 9** (start.sh + Dockerfile) — depends on Task 7
10. **Task 10** (delete alerter) — final step after E2E validation

Tasks 1-2 and 4-5 can be done in parallel. Task 3 is the most complex and can be done independently.

---

## Verification

After all tasks:
- `go build ./...` — clean build
- `go test ./...` — all tests pass
- PoracleWeb connects to processor only (port 3030)
- All PoracleWeb features work: tracking CRUD, role management, config loading, masterdata
- Discord bot commands work (no alerter needed)
- Telegram bot commands work
- Rate limit messages delivered directly
- Confirmation messages delivered directly
- `docker build` produces single-process image
- `start.sh` starts only the processor
