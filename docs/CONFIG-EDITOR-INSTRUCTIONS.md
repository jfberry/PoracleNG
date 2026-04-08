# Config Editor — Implementation Instructions

A comprehensive brief for implementing the PoracleNG config editor (the "Config" tab inside the DTS Editor app).

## Endpoints

All endpoints require the `X-Poracle-Secret` header. All return JSON. CORS is enabled globally.

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/config/schema` | Field metadata (types, defaults, descriptions, options, dependencies) |
| `GET` | `/api/config/values?section=<name>` | Current merged values + list of overridden field paths |
| `POST` | `/api/config/values` | Save partial updates to overrides.json |
| `POST` | `/api/config/validate` | Dry-run validation, returns issues without saving |
| `POST` | `/api/config/migrate` | Slim config.toml by moving editable values to overrides.json |
| `POST` | `/api/resolve` | Batch resolve Discord/Telegram IDs to names |
| `GET` | `/api/geofence/all` | Used to autocomplete `geofence:area` fields |

Full request/response shapes are in `API.md` under sections "Configuration", "Config Editor", and "DTS Editor".

## Schema field properties

Each field in `GET /api/config/schema` has these properties (all optional except `name`, `type`, `description`):

```json
{
  "name": "iv_colors",
  "type": "color[]",
  "default": ["#9D9D9D", "#FFFFFF", "#1EFF00", "#0070DD", "#A335EE", "#FF8000"],
  "description": "Six hex color codes for pokemon IV ranking tiers (0-5 stars). Must contain exactly 6 entries.",
  "hotReload": true,
  "sensitive": false,
  "deprecated": false,
  "advanced": false,
  "hideDefault": false,
  "minLength": 6,
  "maxLength": 6,
  "resolve": "",
  "options": null,
  "dependsOn": null
}
```

### Types

| Type | Render as |
|------|-----------|
| `string` | Text input |
| `int`, `float` | Number input |
| `bool` | Toggle / checkbox |
| `string[]` | List of text inputs with add/remove buttons |
| `int[]` | List of number inputs |
| `color[]` | List of colour pickers (hex strings) |
| `select` | Dropdown using `options[]` |
| `map` | Key/value editor (for `dts_dictionary`, `command_security`) |

### Flags

- **`hotReload: true`** — change applies immediately on save. The user doesn't need to restart.
- **`hotReload: false`** — change saved but only takes effect after restart. The save response will include `restart_required: true` and `restart_fields: [...]`.
- **`sensitive: true`** — render as a password input. The value comes back as `"****"` from `GET /values`. **Send `"****"` back unchanged** if the user didn't touch it — the processor strips masked sensitive values before saving so secrets aren't wiped. Send a real value to update.
- **`deprecated: true`** — the field is no longer recommended. Hide it unless it's already set; if set, show with a warning badge ("deprecated, will be removed in a future version"). Same flag exists at the option level inside `select.options[]` — for those, hide the deprecated option from new selections but display it with a warning if the current value matches it.
- **`advanced: true`** — hide behind a "Show advanced settings" toggle. Used for performance tuning, rarity statistics, fallback URLs, and other settings most users don't need to touch.
- **`hideDefault: true`** — don't pre-fill the default value in an empty input. Used for fallback URLs (long GitHub URLs that would clutter the form). Show the placeholder/example as helper text instead.
- **`minLength`, `maxLength`** — for array types. Enforce client-side; the server also enforces server-side and rejects saves that violate them (see Validation below).
- **`resolve: "<hint>"`** — the field contains an ID that should be resolved to a name. Use `POST /api/resolve` to look it up. Hints listed below.
- **`options: [...]`** — for `select` type. Each option has `value`, `label`, `description`, optional `deprecated`.
- **`dependsOn: {field, value}`** — hide or grey out this field unless the named field has the named value. E.g., `shortlink_provider_url` has `dependsOn: {field: "shortlink_provider", value: "shlink"}` and should be hidden when `shortlink_provider` isn't `"shlink"`.

### Resolve hints

| Hint | Resolve via |
|------|-------------|
| `discord:user` | `POST /api/resolve` → `discord.users[]` |
| `discord:role` | `POST /api/resolve` → `discord.roles[]` |
| `discord:channel` | `POST /api/resolve` → `discord.channels[]` |
| `discord:guild` | `POST /api/resolve` → `discord.guilds[]` |
| `discord:target` | `POST /api/resolve` → try guild, category, channel |
| `discord:user\|role` | could be either; try both |
| `telegram:chat` | `POST /api/resolve` → `telegram.chats[]` |
| `destination` | `POST /api/resolve` → `destinations[]` (any kind of ID, processor figures it out) |
| `geofence:area` | `GET /api/geofence/all` returns the area list — autocomplete from there |

When the editor receives a `resolve` hint on a field:
1. Collect all IDs in that field (and any other fields with the same hint on the same screen)
2. Send a single `POST /api/resolve` request with the appropriate category arrays
3. Cache results client-side; the server already caches for 10 minutes
4. Display the resolved name next to (or in place of) the raw ID
5. **For `destination` hint, watch for `stale: true`** — show a warning that the destination is registered but no longer reachable on the platform

## Sections and tables

The schema groups fields into sections. Some sections also have **tables** (array-of-tables in TOML, e.g., `[[discord.delegated_admins]]`):

```json
{
  "name": "discord",
  "title": "Discord",
  "fields": [...],
  "tables": [
    {
      "name": "delegated_admins",
      "title": "Delegated Channel Admins",
      "description": "Users/roles that can manage tracking for specific channels...",
      "fields": [
        {"name": "target", "type": "string", "resolve": "discord:target"},
        {"name": "admins", "type": "string[]", "resolve": "discord:user|role"}
      ]
    }
  ]
}
```

Render tables as repeatable row groups — each row has its own copy of the table's fields, plus add/remove row buttons. The `target` field uses the same `resolve` hints as regular fields.

**Nested section names** like `geofence.koji` and `reconciliation.discord` are intentional — they map to TOML's nested section syntax (`[geofence.koji]`). Treat them as opaque section identifiers; the section name happens to contain a dot.

## Loading and editing flow

### 1. Initial load

```
GET /api/config/schema    → render the form structure
GET /api/config/values    → fill in current values + mark overridden fields
```

The values response includes an `overridden` array of dotted field paths (e.g., `["discord.admins", "alert_limits.dm_limit"]`). Mark these fields with a visible badge indicating "set via web editor" so users editing `config.toml` directly understand why their TOML edits aren't taking effect.

### 2. Validate as the user types

For non-trivial validation (path existence, colour format, array length), call `POST /api/config/validate` with the current form state. Display issues inline:

- `error` severity: red border, error message under the field, disable submit button until fixed
- `warning` severity: yellow border, advisory message, allow submit

You don't need to call validate on every keystroke — debounce (~500ms) or call on blur. Validation is cheap server-side but every roundtrip adds latency.

### 3. Save

`POST /api/config/values` with only the fields that have changed. The processor merges them into `overrides.json` (deep-merging with existing overrides) and applies them in-memory.

Response:
- `saved`: number of fields written
- `restart_required`: bool
- `restart_fields`: which fields require a restart (only present if `restart_required: true`)

If `restart_required: true`, show a banner: "These changes are saved but won't take effect until the processor is restarted: ..."

### 4. Migrate (one-time, optional)

After the user has been using the web editor for a while, offer a "Clean up config.toml" button somewhere in the settings. Confirm with a modal explaining:

- Your current `config.toml` will be backed up to `config.toml.bak.<timestamp>`
- All editable settings will be moved into the web editor's storage (`overrides.json`)
- `config.toml` will be rewritten to contain only database, tokens, and connection settings
- This is reversible: delete `overrides.json` and restore from the backup

`POST /api/config/migrate` → returns `{backup, fields_moved, fields_kept}`. Show the user what was moved and where the backup is, then refresh the values display.

The migration is **idempotent** — running it twice produces the same result.

## Sensitive field handling

Sensitive fields (`sensitive: true`) are returned as `"****"` by `GET /values`. Behaviour:

- **Render as password inputs** (or password input lists for `string[]` arrays of secrets)
- **Show the masked value as the placeholder** so the user knows a value is set
- **On submit, send back `"****"` unchanged** if the user didn't touch it — the processor strips these and preserves the existing secret
- **On submit, send the new plaintext value** if the user typed something new
- For `string[]` sensitive fields (`accuweather_api_keys`, `geocoding_key`, `static_key`), each array entry is masked individually as `"****"`. The user can add new entries (real values), remove entries, or replace existing masked entries with new ones.

## Validation rules currently enforced server-side

The processor rejects saves that violate any of these:

1. **Unknown sections or fields** — reject with HTTP 400, message identifies the bad field
2. **`MinLength`/`MaxLength`** on array fields (currently only `iv_colors` requires exactly 6)
3. **`color[]` format** — each entry must match `#RGB` or `#RRGGBB`
4. **Geofence paths** — must be either an http(s):// URL or a relative path under the config directory; absolute paths and `..` escapes rejected as errors; missing files reported as warnings (don't block save)

The editor should mirror these client-side for instant feedback, but always trust `POST /api/config/validate` as the source of truth.

## UI suggestions

### Section grouping

There are roughly 20 sections. Group them into a sidebar/tab navigation. Suggested groups:

- **General** — `general`, `locale`
- **Discord** — `discord` (includes its tables)
- **Telegram** — `telegram` (includes its tables)
- **Tracking** — `tracking`, `pvp`, `area_security`
- **Maps & Geofences** — `geofence`, `geofence.koji`, `geocoding`
- **Weather** — `weather`
- **Alerts & Limits** — `alert_limits`
- **Reconciliation** — `reconciliation.discord`, `reconciliation.telegram`
- **Advanced** — `tuning`, `stats`, `fallbacks`, `logging`, `webhookLogging`, `ai`

Within each section, fields with `advanced: true` should be hidden behind a "Show advanced settings" toggle at the bottom of the section.

### Per-field UI patterns

- **Sensitive `string`** (e.g., `bearer_token`) — password input with show/hide toggle
- **Sensitive `string[]`** (e.g., `accuweather_api_keys`) — list of password inputs, add/remove rows
- **`color[]`** (e.g., `iv_colors`) — fixed-size grid of colour pickers, no add/remove (when MinLength = MaxLength)
- **`string[]` with resolve hint** (e.g., `discord.admins`) — list of inputs with autocomplete from resolved names
- **`select` with deprecated options** — current value visible even if deprecated, but new selections from dropdown only show non-deprecated options
- **`dependsOn` field** — when the parent doesn't match, render greyed out with a tooltip "requires `parent_field = value`" rather than hiding entirely (so users know it exists)

### Override badge

For every field whose dotted path is in the `overridden` array from `GET /values`:

- Small badge next to the field label: "via web editor" or a coloured dot
- Tooltip: "This value is in `config/overrides.json`, taking precedence over `config.toml`"

This is critical for users who edit `config.toml` directly and wonder why their changes aren't being applied.

### Migration UI

A button at the top of the form (or in a settings menu) labelled something like "Clean up config.toml". On click, show a confirmation modal explaining the operation, then call `POST /api/config/migrate` and display the response.

## Currently-deprecated items

The following fields/options are marked `deprecated: true` in the schema. The editor should warn users about them:

- `general.rdm_url` — RDM map integration
- `general.rocket_mad_url` — RocketMAD integration
- `general.shortlink_provider` options `hideuri` and `yourls` — only `shlink` is supported
- `pvp.data_source` — only the `webhook` value works; `ohbem` is no longer supported

## Resolve API quick reference

### Request

```json
POST /api/resolve
{
  "discord": {
    "users": ["344179542874914817"],
    "roles": ["987654321"],
    "channels": ["111222333"],
    "guilds": ["444555666"]
  },
  "telegram": {
    "chats": ["789012345"]
  },
  "destinations": ["111222333", "raid-feed"]
}
```

### Response

```json
{
  "status": "ok",
  "discord": {
    "users": {"344179542874914817": {"name": "JamesBerry", "globalName": "James Berry"}},
    "roles": {"987654321": {"name": "Moderator", "guild": "My Server", "guildId": "444555666"}},
    "channels": {"111222333": {"name": "raid-alerts", "type": "text", "guild": "My Server", "guildId": "444555666"}},
    "guilds": {"444555666": {"name": "My Server"}}
  },
  "telegram": {
    "chats": {"789012345": {"name": "James", "type": "private"}}
  },
  "destinations": {
    "111222333": {"kind": "discord:channel", "name": "raid-alerts", "enabled": true, "notes": "EU South alerts"},
    "raid-feed": {"kind": "webhook", "name": "raid-feed", "enabled": true}
  }
}
```

IDs that can't be resolved are omitted (not an error). Discord/Telegram sections are omitted entirely when the respective bot isn't configured.

`destinations[id].stale: true` means the ID exists in the humans table but the platform API can't find it — show a warning, the destination is registered but no longer reachable.

`destinations[id].kind` tells you what type was matched: `webhook`, `discord:channel`, `discord:user`, `discord:role`, `discord:guild`, `telegram:user`, `telegram:channel`, `telegram:group`. Use this to render type-appropriate icons.

## Common gotchas

1. **Don't send unchanged sensitive fields as empty strings** — `""` is a real value that wipes the secret. Send `"****"` back unchanged, or omit the field entirely.

2. **Don't send the entire `GET /values` response back to `POST /values`** — that would write every default into overrides.json and clobber the user's `config.toml`. Only POST fields that the user actually changed.

3. **Watch out for nested section names** — `geofence.koji` and `reconciliation.discord` look like dot notation but they're literal section identifiers in the schema. Don't try to split them as paths.

4. **Tables (`array-of-tables`) replace entirely on save** — if the user is editing `discord.delegated_admins` and you POST a partial update, the entire array gets replaced. Always send the full table contents when any row changes.

5. **`hideDefault` doesn't mean "no default exists"** — the schema still has `default` populated, but the editor should leave the input blank (or show the default as placeholder text) when the field hasn't been customised. Used for fallback URLs.

6. **`dependsOn` is single-valued** — you can only depend on one field having one specific value. Multi-condition dependencies aren't supported. If a field appears to need multiple conditions, it's only declaring the most specific one.

7. **Path validation is opt-in** — the geofence paths field is the only one that gets server-side path validation. Other path-like fields are not validated server-side.
