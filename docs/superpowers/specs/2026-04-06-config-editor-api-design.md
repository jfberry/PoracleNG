# Config Editor API — Design Spec

**Date:** 2026-04-06
**Goal:** Add API endpoints to the PoracleNG processor so the DTS Editor web app can read, modify, and save configuration settings, and resolve Discord/Telegram IDs to human-readable names.

## Context

Configuration lives in `config/config.toml` (TOML). The web-based config editor will be a feature inside the existing DTS Editor app, which already has API secret and CORS support. Server admins only — no delegated access control needed.

Sensitive settings (database credentials, Discord/Telegram tokens, bind addresses) are **excluded from the web editor entirely** and remain TOML-only.

## Override File

**File:** `config/overrides.json`

Changes made via the editor are saved to a JSON override file, never touching the original `config.toml`. This mirrors the DTS editor pattern (original is safe, overrides layer on top).

**Structure** mirrors TOML sections:
```json
{
  "general": {
    "locale": "de",
    "max_pokemon": 50
  },
  "discord": {
    "admins": ["123456789", "987654321"]
  },
  "alert_limits": {
    "dm_limit": 30
  }
}
```

**Load order:** TOML first, then JSON overrides applied on top.

**Merge rules:**
- Scalar fields: override replaces
- Arrays (`string[]`): override replaces entirely (not append)
- Array-of-tables (`table[]`): override replaces the entire array

**Revert:** Admin deletes `config/overrides.json` to restore all original TOML values.

**Future migration:** The API contract stays the same if storage moves to a database later — only the backend changes.

## API Endpoints

All endpoints require `X-Poracle-Secret` header. CORS is enabled globally.

### GET /api/config/schema

Returns field metadata grouped by section. Each field includes name, type, default value, description, and optional metadata (constrained options, resolution hints, dependency visibility).

Sensitive/TOML-only fields are excluded entirely — they don't appear in the schema.

**Response:**
```json
{
  "status": "ok",
  "sections": [
    {
      "name": "general",
      "title": "General Settings",
      "fields": [
        {
          "name": "locale",
          "type": "string",
          "default": "en",
          "description": "Default language for new users",
          "hotReload": true
        },
        {
          "name": "role_check_mode",
          "type": "select",
          "default": "ignore",
          "description": "Action when a user loses their Discord role",
          "hotReload": true,
          "options": [
            {"value": "ignore", "label": "Ignore", "description": "Log but take no action"},
            {"value": "disable-user", "label": "Disable User", "description": "Set admin_disable flag, remove subscription roles, send goodbye message"},
            {"value": "delete", "label": "Delete", "description": "Remove all tracking data and human record permanently"}
          ]
        },
        {
          "name": "disabled_commands",
          "type": "string[]",
          "default": [],
          "description": "Commands to disable globally",
          "hotReload": true
        }
      ]
    },
    {
      "name": "discord",
      "title": "Discord Bot",
      "fields": [
        {
          "name": "admins",
          "type": "string[]",
          "description": "Admin user IDs",
          "hotReload": true,
          "resolve": "discord:user"
        },
        {
          "name": "check_role",
          "type": "bool",
          "default": false,
          "description": "Enable periodic role membership checks",
          "hotReload": false
        },
        {
          "name": "check_role_interval",
          "type": "int",
          "default": 6,
          "description": "Hours between periodic role checks",
          "hotReload": false,
          "dependsOn": {"field": "check_role", "value": true}
        }
      ],
      "tables": [
        {
          "name": "delegated_admins",
          "title": "Delegated Channel Admins",
          "description": "Users/roles that can manage tracking for specific channels, categories, or guilds",
          "fields": [
            {"name": "target", "type": "string", "description": "Channel, category, or guild ID", "resolve": "discord:target"},
            {"name": "admins", "type": "string[]", "description": "User or role IDs", "resolve": "discord:user|role"}
          ]
        }
      ]
    }
  ]
}
```

### GET /api/config/values

Returns current merged config values (TOML + overrides applied). Only web-editable sections are included.

**Query parameters:**
- `section` (optional) — return only one section

**Response:**
```json
{
  "status": "ok",
  "values": {
    "general": {
      "locale": "en",
      "max_pokemon": 0,
      "disabled_commands": []
    },
    "discord": {
      "admins": ["344179542874914817"],
      "channels": ["123456789"],
      "check_role": true,
      "check_role_interval": 6,
      "delegated_admins": [
        {"target": "998877665", "admins": ["111222333"]}
      ]
    }
  }
}
```

### POST /api/config/values

Save config changes. Accepts partial updates — only the fields being changed. Merges into `config/overrides.json`.

For hot-reloadable fields, applies changes to the in-memory config immediately. If any changed field requires a restart, flags it in the response.

**Request:**
```json
{
  "discord": {
    "admins": ["344179542874914817", "999888777"]
  },
  "alert_limits": {
    "dm_limit": 30
  }
}
```

**Response:**
```json
{
  "status": "ok",
  "saved": 2,
  "restart_required": false
}
```

If any field requires restart:
```json
{
  "status": "ok",
  "saved": 3,
  "restart_required": true,
  "restart_fields": ["discord.check_role"]
}
```

### POST /api/resolve

Batch resolve Discord/Telegram IDs to human-readable names. Results are cached for 10 minutes (using existing jellydator/ttlcache).

**Request:**
```json
{
  "discord": {
    "users": ["344179542874914817"],
    "roles": ["987654321"],
    "channels": ["111222333"],
    "guilds": ["444555666"]
  },
  "telegram": {
    "chats": ["789012345", "-100123456"]
  }
}
```

**Response:**
```json
{
  "status": "ok",
  "discord": {
    "users": {
      "344179542874914817": {"name": "JamesBerry", "globalName": "James Berry"}
    },
    "roles": {
      "987654321": {"name": "Moderator", "guild": "My Server", "guildId": "444555666"}
    },
    "channels": {
      "111222333": {"name": "raid-alerts", "type": "text", "guild": "My Server", "guildId": "444555666", "categoryName": "Pokemon"}
    },
    "guilds": {
      "444555666": {"name": "My Server"}
    }
  },
  "telegram": {
    "chats": {
      "789012345": {"name": "James Berry", "type": "private"},
      "-100123456": {"name": "Pokemon Group", "type": "supergroup"}
    }
  }
}
```

IDs that cannot be resolved are omitted from the response (not an error).

Discord resolution is unavailable when the Discord bot is not configured. Same for Telegram. The response simply omits the platform section.

### POST /api/config/validate (optional, v2)

Dry-run validation. Same request body as POST /api/config/values but only checks for problems without writing. Returns warnings/errors. Not required for v1.

## Schema Field Types

| Type | Editor widget | Example fields |
|------|--------------|----------------|
| `string` | Text input | `locale`, `api_secret`, `default_template_name` |
| `int` | Number input | `dm_limit`, `max_pokemon`, `reload_interval_secs` |
| `float` | Number input (decimal) | `pvp_filter_great_min_cp` |
| `bool` | Toggle/switch | `check_role`, `enabled`, `area_security.enabled` |
| `string[]` | Tag list / multi-input | `admins`, `channels`, `guilds`, `disabled_commands` |
| `select` | Dropdown | `role_check_mode`, `everything_flag_permissions` |
| `table[]` | Repeatable row group | `delegated_admins`, `role_subscriptions`, `communities` |

### Select Options

Select fields provide options as objects with value, label, and description:
```json
{
  "options": [
    {"value": "ignore", "label": "Ignore", "description": "Log but take no action"},
    {"value": "disable-user", "label": "Disable User", "description": "Set admin_disable, send goodbye"}
  ]
}
```

### Dependency Visibility

Fields can declare a dependency so the editor hides/greys them out when irrelevant:
```json
{
  "name": "check_role_interval",
  "dependsOn": {"field": "check_role", "value": true}
}
```

### Resolution Hints

Fields containing platform IDs declare a `resolve` hint for the editor:
- `discord:user` — Discord user ID
- `discord:role` — Discord role ID
- `discord:channel` — Discord channel ID
- `discord:guild` — Discord guild ID
- `discord:target` — could be guild, category, or channel (try all)
- `discord:user|role` — could be either user or role
- `telegram:chat` — Telegram user or group ID
- `geofence:area` — geofence area name (editor fetches list from `GET /api/geofence/all` for autocomplete)

The editor collects all IDs on the page and sends a single batch `POST /api/resolve` request. For `geofence:area` fields, the editor calls `GET /api/geofence/all` to populate autocomplete options.

## Hot Reload vs Restart

Each field is tagged `"hotReload": true/false` in the schema.

**Hot-reloadable** (applied immediately):
- Admin lists, delegated admins, user tracking admins
- Alert limits (dm_limit, channel_limit, timing_period, max_limits_before_stop, overrides)
- Disabled commands, command security
- DTS dictionary
- General tracking limits (max_pokemon, max_raid, etc.)
- Locale, available languages

**Restart required:**
- Discord settings (guilds, channels, check_role, register_on_start, role subscriptions)
- Telegram settings (channels, check_role, register_on_start)
- Processor tuning (worker counts, render pool, reload interval)
- Geofence source (Koji URL, geofence paths)
- PVP config (level caps, filter settings)
- Static map, geocoding, icon provider settings
- Reconciliation settings
- Webhook logging

On save:
1. Write to `config/overrides.json`
2. If all changed fields are hot-reloadable: refresh in-memory config, return `restart_required: false`
3. If any changed field requires restart: save but return `restart_required: true` with the list of fields needing restart

## Excluded Settings (TOML-only)

These are never exposed via the API:
- `[database]` — all fields (host, port, user, password, database name)
- `[discord] token`
- `[telegram] token`
- `[processor] port`, `bind_address`
- `[processor] api_secret` (the secret that protects the API itself)
- `[webhook_logging]` file paths

## File Structure

### New files
- `processor/internal/api/config_schema.go` — hand-maintained schema definitions (initially generated from config.example.toml and config struct)
- `processor/internal/api/config_values.go` — GET/POST values handlers, override file read/write, hot-reload trigger
- `processor/internal/api/config_resolve.go` — batch ID resolution handler with ttlcache

### Modified files
- `processor/internal/config/config.go` — add `LoadOverrides` and `ApplyOverrides` functions
- `processor/cmd/processor/main.go` — register new routes, pass bot sessions to resolve handler

### Routes
```go
apiGroup.GET("/config/schema", api.HandleConfigSchema())
apiGroup.GET("/config/values", api.HandleConfigValues(cfg))
apiGroup.POST("/config/values", api.HandleConfigSave(cfg, configDir, reloadFunc))
apiGroup.POST("/resolve", api.HandleResolve(discordSession, telegramAPI))
```

## Override Load & Apply

**LoadOverrides(configDir string) (map[string]any, error)**
- Reads `config/overrides.json` if it exists
- Returns nil map if file doesn't exist (not an error)

**ApplyOverrides(cfg *config.Config, overrides map[string]any) error**
- Walks the override map section by section
- For each section, matches fields by TOML tag name against the config struct
- Sets values using reflection
- For `table[]` fields, replaces the entire slice

**SaveOverrides(configDir string, updates map[string]any) error**
- Reads existing overrides.json (or empty map)
- Deep-merges updates into existing overrides
- Writes back to overrides.json

Called during config load (after TOML parse) and on hot-reload.
