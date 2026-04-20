<p align="center">
  <img width="200" src="https://raw.githubusercontent.com/jfberry/PoracleNG/images/starchy.svg?sanitize=true">
</p>

# PoracleNG

PoracleNG is a high-performance Pokemon GO webhook alerting system written in Go. It receives webhooks from a scanner (Golbat), matches them against user-defined tracking rules, and delivers personalised alerts to Discord and Telegram — including bot commands, reconciliation, and all APIs in a single process.

## Architecture

```
Golbat ──webhook──▶ Processor (Go :3030) ──REST API──▶ Discord / Telegram
                         │
                         ▼
                    MySQL (read/write)
```

The processor receives raw webhooks from Golbat, matches them against all user tracking rules in memory, enriches with game data and translations, renders DTS Handlebars templates, and delivers messages directly via the Discord REST API and Telegram Bot API. It also runs the Discord and Telegram bots (commands, reconciliation) and exposes the full `/api/*` surface consumed by external tools like PoracleWeb.

**Migrating from PoracleJS?** See [Migrating from PoracleJS](#migrating-from-poraclejs) for an automated migration script and what has changed.

## Quick Start

### Prerequisites

- Go 1.21+
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

This builds the Go processor binary.

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

The start script builds the processor binary if needed and runs it.

You can also use `make start` which builds first then runs `start.sh`.

On first startup, the processor downloads game data (pokemon names, moves, locales) into `resources/`, loads all tracking data from the database, and begins accepting webhooks. Rarity and shiny stats will be empty until enough pokemon sightings accumulate.

### 4. Point Golbat at the processor

Configure Golbat to send webhooks to the processor:

```
http://<your-host>:3030/
```

### Docker

A pre-built Docker image is available:

```yaml
# docker-compose.yml
services:
  poracle:
    image: ghcr.io/jfberry/poracleng:main
    ports:
      - "3030:3030"
    volumes:
      - ./config:/app/config
      - ./logs:/app/logs
    restart: unless-stopped
```

Create `config/config.toml` (see step 1 above), then:

```sh
docker compose up -d
```

The processor downloads game data on first startup. Logs are written to the `logs/` volume.

## Configuration

PoracleNG uses a single TOML config file at `config/config.toml`.

```
config/
  config.toml              # Your configuration (only overrides needed)
  config.example.toml      # Full reference with all defaults and documentation
  geofences/               # Geofence files (GeoJSON or Poracle format)
```

The processor listens on port 3030 by default:

```toml
[processor]
host = "0.0.0.0"
port = 3030
```

See `config/config.example.toml` for the full list of settings (database, PVP, weather, geofence, discord, telegram, geocoding, tuning, etc.).

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
    config.toml            # Your TOML config
    config.example.toml    # Full reference
    geofences/             # Your geofence files
    dts.json               # Your DTS templates (optional, falls back to default)
    ...                    # Other data files (optional)
  fallbacks/               # Bundled defaults used when config/ files are missing
  examples/                # Reference files and community-contributed templates
    dts/                   # Example DTS templates from the community
    customMaps/            # Example custom map definitions
  processor/               # Go processor source
  resources/               # Downloaded game data (auto-managed)
  logs/                    # Log files
  scripts/                 # Migration and utility scripts
  tileserver_templates/    # Tileservercache JSON templates (deployed to your tileserver)
```

## Logging

The processor writes logs to the `logs/` directory at the project root:

- `logs/processor.log` — main processor log (rotated by size)

Log level and retention are configured in the `[logging]` section of `config.toml`:

```toml
[logging]
level = "verbose"        # silly, debug, verbose, info, warn
max_age = 7              # days to keep log files
```

## Migrating from PoracleJS

> **Prerequisite:** Make sure you are on the latest PoracleJS so that your database schema is up to date before migrating.

An automated migration script converts your existing PoracleJS configuration. Node.js is needed for this one-time script only — the PoracleNG runtime itself is pure Go.

### Bare metal (with Node.js installed)

Clone this repo and run:

```sh
node scripts/migrate-from-poracle.js
```

When prompted, enter the path to your existing PoracleJS installation.

### Docker (no Node.js installed)

Run a throwaway `node:alpine` container that fetches the migration tooling and writes into your mounted config dir:

```sh
docker run --rm -it \
  -v /path/to/your/poraclejs:/oldporacle \
  -v /path/to/your/poracleng-config:/workspace/config \
  -w /workspace \
  node:20-alpine \
  sh -c 'apk add --no-cache git >/dev/null && \
    git clone --depth=1 https://github.com/jfberry/PoracleNG /tmp/ng && \
    cd /tmp/ng && rm -rf config && ln -s /workspace/config config && \
    node scripts/migrate-from-poracle.js'
```

When prompted for `Path to your existing PoracleJS installation:` enter `/oldporacle`.

> **Note:** The migrated config files may be owned by `root` depending on your Docker setup — you may need to `chown` them before editing.

### What the script does

1. **Copies customized data files** (DTS, aliases, geofences, etc.) into `config/`, skipping any that are unchanged from Poracle's defaults
2. **Converts your `local.json`** overrides into the unified `config/config.toml`, stripping obsolete settings
3. **Prints instructions** for the manual steps below

After migrating, review `config/config.toml` for any incorrect ports or values, ensure your volumes are mounted correctly, and start PoracleNG.

### What changes

**Single process.** PoracleNG is a single Go binary — webhook handling, matching, rendering, delivery, bots, and all `/api/*` endpoints run in one process. External tools like PoracleWeb only need the processor's address.

**Webhook destination changes.** Golbat must now send webhooks to the processor (default port 3030), not the old alerter port.

**Config format.** The JS alerter's `default.json` / `local.json` system is replaced by a single `config/config.toml`. All settings use `snake_case`. See `config/config.example.toml` for the full reference.

**Data files moved.** DTS templates, aliases, and other data files are now loaded from `config/` (at the project root). Files you haven't customised don't need to be copied — bundled fallbacks in `fallbacks/` are used automatically.

**Scanner type.** MAD scanner support has been removed. The default scanner type is now `golbat`. If you use RDM, add `scanner_type = "rdm"` under `[database]`.

**Logs directory.** The processor writes to `logs/` at the project root.

**pm2 users.** Use the included `ecosystem.config.js` instead of `pm2 start start.sh` directly — it sets `kill_timeout: 10000` (10s) to allow graceful shutdown of message queues. The default pm2 timeout of 1.6s can leave orphaned processes.

**Tileserver templates.** The processor generates static map tiles via tileservercache. The templates are in `tileserver_templates/` at the repo root — deploy them to your tileserver. Available fields differ slightly from the JS alerter; see [TILESERVER.md](TILESERVER.md) for the full field reference.

**DTS templates.** Templates have access to both pre-translated Poracle fields (recommended) and raw webhook fields. See [DTS.md](DTS.md) for the full field reference.

## API Endpoints

All API endpoints are served by the processor (default port 3030). See [API.md](API.md) for the full reference with request/response examples.

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
| Messages | `POST /api/deliverMessages` | Send confirmation messages to Discord/Telegram |
| Game data | `GET /api/masterdata/*` | Pokemon and grunt master data |
| Config | `GET /api/config/poracleWeb` | Server config for web UI |
| Test | `POST /api/test` | Test webhook simulation |
| Health | `GET /health`, `GET /metrics` | Health check and Prometheus metrics |

## Monitoring

Two importable Grafana dashboards for the Prometheus metrics exposed by the processor are included:

- `monitoring/grafana/poracle-operations-lite-dashboard.json` for a concise day-to-day operations view
- `monitoring/grafana/poracle-observability-dashboard.json` for the complete observability view

There is also an example Prometheus scrape config at `monitoring/prometheus.yml.example`. If you are using [Zapdos](https://github.com/UnownHash/Zapdos), add a `poracle_processor` job to `Zapdos/vmagnet/prometheus.yml`.

Then:

1. Point Prometheus at the processor's `/metrics` endpoint.
2. Add Prometheus as a Grafana data source.
3. Import `monitoring/grafana/poracle-operations-lite-dashboard.json` for a compact operational view, or `monitoring/grafana/poracle-observability-dashboard.json` for the full dashboard.

## Connections Summary

| From | To | Endpoint | Purpose |
|------|----|----------|---------|
| Golbat | Processor | `POST /` | Raw webhooks (all types) |
| Processor | Discord | Discord REST API v10 + gateway | Alert delivery and bot commands |
| Processor | Telegram | Telegram Bot API + polling | Alert delivery and bot commands |
| PoracleWeb / other | Processor | `/api/tracking/*`, `/api/humans/*`, etc. | External tool integration |

## Building

```sh
make build              # Build the Go processor
make clean              # Remove processor binary
make test               # Run processor tests
make start              # Build + start the processor
```
