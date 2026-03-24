# PoracleNG

PoracleNG splits Poracle into two components: a high-performance **Processor** (Go) that receives webhooks and performs all user matching in memory, and an **Alerter** (Node.js) that handles template rendering and message delivery to Discord and Telegram.

## Architecture

```
Golbat ──webhook──▶ Processor (Go :3030) ──matched──▶ Alerter (Node.js :3031) ──▶ Discord / Telegram
                         │                                │
                         │◀──── POST /api/reload ──────────│
                         │                                │
                         ▼                                ▼
                    MySQL (read)                     MySQL (read/write)
```

- The **Processor** receives raw webhooks from Golbat, matches them against all user tracking rules in memory, and forwards only the matched results to the alerter.
- The **Alerter** receives pre-matched results, renders templates (DTS), performs geocoding, generates static maps, and delivers messages to Discord and Telegram. It also handles user commands and notifies the processor to reload when tracking data changes.

## Quick Start

### Prerequisites

- Go 1.21+ (for the processor)
- Node.js 18+ (for the alerter)
- MySQL 8.0+
- A Golbat instance sending webhooks

### 1. Configure

Copy the example config and edit it:

```sh
cp config/config.example.toml config/config.toml
```

At minimum you need to set:

```toml
[database]
host = "127.0.0.1"
user = "poracleuser"
password = "poraclepassword"
database = "poracle"

[discord]
enabled = true
token = ["your-bot-token"]
guilds = ["your-guild-id"]
channels = ["registration-channel-id"]
admins = ["your-discord-user-id"]
```

See `config/config.example.toml` for the full list of settings with documentation.

### 2. Build

```sh
make build
```

This builds the Go processor binary and runs `npm install` for the alerter. You can also build each component separately with `make build-processor` or `make install-alerter`.

### 3. Start

**With pm2 (recommended for production):**

```sh
pm2 start ecosystem.config.js
```

The included `ecosystem.config.js` sets `kill_timeout: 10000` (10 seconds) to allow graceful shutdown of Discord/Telegram queues. The default pm2 timeout of 1.6 seconds is too short and can leave orphaned processes.

**Without pm2:**

```sh
./start.sh
```

The start script:
- Builds the processor and installs alerter dependencies if needed
- Starts the processor and waits for it to pass a health check
- Starts the alerter
- Monitors both processes and shuts them down together on Ctrl-C

You can also use `make start` which builds first then runs `start.sh`.

On first startup, the processor downloads game data (pokemon names, moves, locales) into `resources/`, loads all tracking data from the database, and begins accepting webhooks. Rarity and shiny stats will be empty until enough pokemon sightings accumulate.

### 5. Point Golbat at the processor

Configure Golbat to send webhooks to the **processor** (not the alerter):

```
http://<your-host>:3030/
```

## Configuration

PoracleNG uses a single shared TOML config file at `config/config.toml` for both the processor and alerter. Both components read from the same file, eliminating the need to keep settings in sync.

```
config/
  config.toml              # Your configuration (only overrides needed)
  config.example.toml      # Full reference with all defaults and documentation
  geofences/               # Geofence files (GeoJSON or Poracle format)
```

The only settings specific to each component are their networking:

```toml
[processor]
host = "0.0.0.0"                        # Processor listen address
port = 3030                              # Processor takes the original Poracle port
alerter_url = "http://localhost:3031"    # Where the alerter is listening

[alerter]
host = "127.0.0.1"                      # Alerter listen address
port = 3031                              # Alerter runs on processor port + 1
processor_url = "http://localhost:3030"  # Where the processor is listening
```

Everything else (database, PVP, weather, geofence, discord, telegram, geocoding, tuning, etc.) is shared.

### Config Data Files

Some features use JSON data files (DTS templates, pokemon aliases, etc.). These are loaded from `config/` with automatic fallback to bundled defaults:

| File | Purpose | Fallback |
|------|---------|----------|
| `dts.json` | Discord/Telegram message templates | Yes (`fallbacks/dts.json`) |
| `pokemonAlias.json` | Pokemon name aliases for commands | Yes (`fallbacks/pokemonAlias.json`) |
| `partials.json` | Handlebars template partials | Yes (`fallbacks/partials.json`) |
| `testdata.json` | Test webhook data for `!test` command | Yes (`fallbacks/testdata.json`) |
| `geofences/*.json` | Geofence definitions | Yes (`fallbacks/geofence.json`) |
| `dts/` | Additional DTS files (merged with dts.json) | No |
| `broadcast.json` | Broadcast message templates | No |
| `channelTemplate.json` | Discord channel auto-creation templates | No |
| `customMaps/` | Custom static map definitions | No |
| `emoji.json` | Custom emoji mappings | No |
| `custom.<lang>.json` | Custom locale translations | No |

Files with a fallback will use the bundled version from `fallbacks/` if you haven't placed a custom version in `config/`. Files without a fallback are optional features that are simply disabled when absent.

The `examples/` directory contains reference copies of all data files plus community-contributed DTS templates to help you get started. To customize a file, copy it from `examples/` into `config/` and edit it there.

### Resource Downloads

On startup, the processor automatically downloads game data from public GitHub repositories:

- **Game Master** (pokemon, moves, items, types) from WatWowMap/Masterfile-Generator
- **Invasion lineups** from WatWowMap/event-info
- **Locale translations** from WatWowMap/pogo-translations

These are cached in `resources/` and reused if the download fails. No manual setup is needed.

## Directory Layout

```
PoracleNG/
  config/                  # Your configuration
    config.toml            # Shared TOML config (both components)
    config.example.toml    # Full reference
    geofences/             # Your geofence files
    dts.json               # Your DTS templates (optional, falls back to default)
    ...                    # Other data files (optional)
  fallbacks/               # Bundled defaults used when config/ files are missing
  examples/                # Reference files and community-contributed templates
    dts/                   # Example DTS templates from the community
    customMaps/            # Example custom map definitions
  processor/               # Go processor source
  alerter/                 # Node.js alerter source
  resources/               # Downloaded game data (auto-managed)
  logs/                    # Log files from both components
  scripts/                 # Migration and utility scripts
```

## Logging

Both components write logs to the shared `logs/` directory at the project root:

**Processor logs:**
- `logs/processor.log` — main processor log (rotated by size)

**Alerter logs:**
- `logs/general-<date>.log` — main alerter log (rotated daily)
- `logs/errors-<date>.log` — warnings and errors
- `logs/discord-<date>.log` — Discord message delivery
- `logs/telegram-<date>.log` — Telegram message delivery
- `logs/commands-<date>.log` — user commands
- `logs/controller-<date>.log` — controller activity
- `logs/matched_webhooks-<date>-<hour>.log` — matched results from the processor (if enabled)

Log level and retention are configured in the `[logging]` section of `config.toml`:

```toml
[logging]
level = "verbose"        # silly, debug, verbose, info, warn
max_age = 7              # days to keep log files
```

## Migrating from PoracleJS

An automated migration script converts your existing PoracleJS configuration:

```sh
node scripts/migrate-from-poracle.js
```

The script will:

1. **Ask** for the path to your existing PoracleJS installation
2. **Copy customized data files** (DTS, aliases, geofences, etc.) into `config/`, skipping any that are unchanged from Poracle's defaults
3. **Convert your `local.json`** overrides into the unified `config/config.toml`, stripping obsolete settings
4. **Print instructions** for the manual steps below

### What changes

**Webhook destination changes.** Golbat must now send webhooks to the **processor** (default port 3030), not the old alerter port. The processor matches and forwards results to the alerter internally.

**Two components to run.** You now start both the processor and the alerter. The processor must be running before the alerter can receive matched results.

**New URL settings.** Both components need to know where the other is listening:

```toml
[processor]
alerter_url = "http://localhost:3031"

[alerter]
processor_url = "http://localhost:3030"
```

**Config format.** The alerter's `default.json` / `local.json` system is replaced by a single `config/config.toml`. All settings use `snake_case`. See `config/config.example.toml` for the full reference.

**Data files moved.** DTS templates, aliases, and other data files are now loaded from `config/` (at the project root) instead of `alerter/config/`. Files you haven't customized don't need to be copied — bundled fallbacks in `fallbacks/` are used automatically.

**Scanner type.** MAD scanner support has been removed. The default scanner type is now `golbat`. If you use RDM, add `scanner_type = "rdm"` under `[database]`.

**Logs directory.** Both components now write to `logs/` at the project root instead of `alerter/logs/`.

## API Endpoints

All API endpoints are available through the processor (default port 3030). External tools like PoracleWeb only need to know the processor's address. See [API.md](API.md) for the full reference with request/response examples.

### Processor (Go)

| Category | Endpoints | Description |
|----------|-----------|-------------|
| Webhooks | `POST /` | Receive Golbat webhooks |
| State | `GET/POST /api/reload`, `/api/geofence/reload` | Reload tracking rules / geofences |
| Tracking | `/api/tracking/{type}/{id}` | CRUD for all 10 tracking types (pokemon, raid, egg, quest, invasion, lure, nest, gym, fort, maxbattle) |
| Humans | `/api/humans/*` | User management (areas, location, start/stop, create) |
| Profiles | `/api/profiles/*` | Profile CRUD (create, delete, copy, update hours) |
| Geofence | `/api/geofence/*` | Geofence data (all, hash, geojson) and tile generation |
| Stats | `/api/stats/*` | Rarity, shiny rate, shiny possible |
| Weather | `GET /api/weather` | Weather cell data |
| Geocoding | `GET /api/geocode/forward` | Forward geocode lookup |
| Test | `POST /api/test` | Test webhook simulation |
| Health | `GET /health`, `GET /metrics` | Health check and Prometheus metrics |

### Alerter (Node.js, proxied through processor)

| Category | Endpoints | Description |
|----------|-----------|-------------|
| Delivery | `POST /api/matched` | Receive pre-matched results from processor |
| Messages | `POST /api/postMessage` | Direct message delivery to Discord/Telegram |
| Discord | `/api/humans/{id}/roles/*` | Discord role management (requires Discord.js client) |
| Config | `GET /api/config/*` | Server config and template metadata |
| Game data | `GET /api/masterdata/*` | Pokemon and grunt master data |

The processor proxies unhandled `/api/*` requests to the alerter transparently. Both components expose `/health` and `/metrics` on their respective ports for Prometheus monitoring.

## Monitoring

Two importable Grafana dashboards for the Prometheus metrics exposed by both services are included:

- `monitoring/grafana/poracle-operations-lite-dashboard.json` for a concise day-to-day operations view
- `monitoring/grafana/poracle-observability-dashboard.json` for the complete observability view

There is also an example Prometheus scrape config at `monitoring/prometheus.yml.example`. If you are using [Zapdos](https://github.com/UnownHash/Zapdos), add `poracle_processor` and `poracle_alerter` jobs to `Zapdos/vmagnet/prometheus.yml`.

Then:

1. Point Prometheus at the processor and alerter `/metrics` endpoints.
2. Add Prometheus as a Grafana data source.
3. Import `monitoring/grafana/poracle-operations-lite-dashboard.json` for a compact operational view, or `monitoring/grafana/poracle-observability-dashboard.json` for the full dashboard.

## Connections Summary

| From | To | Endpoint | Purpose |
|------|----|----------|---------|
| Golbat | Processor | `POST /` | Raw webhooks (all types) |
| Processor | Alerter | `POST /api/matched` | Pre-matched results with user lists |
| Alerter | Processor | `POST /api/reload` | Trigger in-memory state reload |

## Building

```sh
make build              # Build everything
make build-processor    # Build Go processor only
make install-alerter    # Install alerter Node.js dependencies only
make clean              # Remove processor binary
make test               # Run processor tests
make start              # Build + start both components
```
