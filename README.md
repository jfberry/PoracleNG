# PoracleNG

PoracleNG splits Poracle into two components: a high-performance **Processor** (Go) that receives webhooks and performs all user matching in memory, and an **Alerter** (Node.js) that handles template rendering and message delivery to Discord and Telegram.

## Architecture

```
Golbat ──webhook──▶ Processor (Go) ──matched──▶ Alerter (Node.js) ──▶ Discord / Telegram
                        │                           │
                        │◀──── POST /api/reload ─────│
                        │                           │
                        ▼                           ▼
                   MySQL (read)                MySQL (read/write)
```

### Processor (Go)

The processor receives raw webhooks from Golbat, performs all matching against user tracking rules in memory, and forwards only the matched results to the alerter.

**Responsibilities:**

- Receives Golbat webhooks via `POST /` (pokemon, raid, weather, invasion, quest, lure, gym, nest, fort update)
- Loads all tracking data from MySQL into memory (humans, monsters, raids, eggs, invasions, quests, lures, gyms, nests, forts, profiles, geofences)
- Matches all incoming webhook types against user tracking rules entirely in memory
- Computes PVP rankings from Golbat-provided PVP data
- Tracks weather state per S2 cell (direct webhooks + inference from pokemon boost)
- Detects pokemon encounter changes (pokemon_id/form changes on the same encounter)
- Deduplicates pokemon and raid webhooks
- Tracks pokemon rarity from observed sighting frequency
- Enriches pokemon messages with computed fields (IV, CP, level, TTH, rarity, PVP ranks, cell weather)
- Batches and sends matched results to the alerter via `POST /api/matched`
- Reloads in-memory state periodically and on demand via `POST /api/reload`

**Key design decisions:**

- All matching is done in memory with no per-webhook database queries, enabling much higher throughput than the original SQL-based matching
- State is loaded atomically and swapped via read-write mutex, so matching is never blocked by reloads
- Pokemon processing uses a bounded worker pool for concurrent matching

### Alerter (Node.js)

The alerter receives pre-matched results from the processor and handles everything after matching: data enrichment with game data, template rendering (DTS), geocoding, static map generation, and message delivery.

**Responsibilities:**

- Receives matched payloads from the processor via `POST /api/matched`
- Looks up game data (pokemon names, move names, types, etc.)
- Renders message templates (DTS) per user language and template preference
- Generates static map images
- Delivers messages to Discord (users, channels, webhooks) and Telegram (users, channels)
- Processes user commands (!track, !raid, !egg, !untrack, etc.)
- Manages user registration and tracking rules in MySQL
- Notifies the processor to reload when tracking data changes

## Data Flow

1. **Golbat** sends webhooks to the processor (pokemon, raid, weather, invasion, quest, lure, gym, nest, fort update)
2. **Processor** matches each webhook against all user tracking rules in memory
3. For each match, the processor sends a payload to the alerter containing:
   - The original webhook message (enriched with computed fields for pokemon)
   - The list of matched users with their delivery preferences
   - The list of matched geofence areas
4. **Alerter** renders templates and delivers messages for each matched user
5. When a user changes their tracking via Discord/Telegram commands or the web API, the alerter calls `POST /api/reload` on the processor to refresh its in-memory state

## Configuration

### Processor

Copy `processor/config.toml.example` to `processor/config.toml` and edit:

```toml
[server]
listen_addr = ":4200"

[database]
# Same MySQL database as the alerter
dsn = "poracle:password@tcp(127.0.0.1:3306)/poracle?parseTime=true"

[alerter]
# URL where the alerter is listening
url = "http://localhost:4201"

[geofence]
# Same geofence files as the alerter
paths = ["geofence/geofence.json"]

[pvp]
# Must match the alerter's PVP settings
pvp_query_max_rank = 100
pvp_filter_max_rank = 100
pvp_evolution_direct_tracking = true
level_caps = [50]
pvp_filter_great_min_cp = 1400
pvp_filter_ultra_min_cp = 2400
pvp_filter_little_min_cp = 400
include_mega_evolution = false

[weather]
enable_inference = true

[tuning]
reload_interval_secs = 60    # Safety-net periodic reload interval
encounter_cache_ttl = 3600   # Seconds to keep encounter state for change detection
worker_pool_size = 4         # Concurrent pokemon processing goroutines
batch_size = 50              # Max items per batch sent to alerter
flush_interval_millis = 100  # How often to flush partial batches

[areaSecurity]
enabled = false
strict_locations = false

[logging]
level = "info"               # trace, debug, info, warn, error
file_logging_enabled = true
filename = "logs/processor.log"
max_size = 50                # MB per log file before rotation
max_age = 30                 # Days to retain old log files
max_backups = 5
compress = false

[webhookLogging]
enabled = false              # Log raw incoming webhooks to file (one JSON line per webhook)
filename = "logs/webhooks.log"
max_size = 100
max_age = 1
max_backups = 3
compress = false
```

### Alerter

In the alerter's config (e.g. `alerter/config/local.json`), add the processor URL:

```json
{
  "processor": {
    "url": "http://localhost:4200"
  }
}
```

This tells the alerter where the processor is listening, so it can send `POST /api/reload` when users change their tracking rules.

### Golbat

Configure Golbat to send webhooks to the **processor** (not the alerter):

```
http://<processor-host>:4200/
```

## Connections Summary

| From | To | Endpoint | Purpose |
|------|----|----------|---------|
| Golbat | Processor | `POST /` | Raw webhooks (all types) |
| Processor | Alerter | `POST /api/matched` | Pre-matched results with user lists |
| Alerter | Processor | `POST /api/reload` | Trigger in-memory state reload |

## Building

### Processor

```sh
cd processor
go build -o processor ./cmd/processor
```

### Running

```sh
# Start the processor (receives webhooks, performs matching)
cd processor
./processor -config config.toml

# Start the alerter (receives matched results, delivers messages)
cd alerter
node src/app.js
```

The processor should be started before the alerter. On startup, the processor loads all tracking data from the database and begins accepting webhooks. Rarity data will be empty until enough pokemon sightings accumulate (typically a few hours).

## API Endpoints

### Processor

| Method | Path | Description |
|--------|------|-------------|
| POST | `/` | Receive Golbat webhooks |
| POST | `/api/reload` | Trigger in-memory data reload |
| GET | `/health` | Health check |

### Alerter

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/matched` | Receive pre-matched results from processor |
| Various | `/api/tracking/*` | User tracking management (existing Poracle API) |

## Logging

### Processor

The processor uses logrus with a format matching Golbat's log output:

```
INFO 2026-03-11 15:25:10 Processor starting on :4200
INFO 2026-03-11 15:25:10 State loaded: 150 humans, 45 raids, 12 eggs, 8 fences
INFO 2026-03-11 15:25:15 [abc123] Pokemon 25 appeared at [51.500,-0.100] and 3 humans cared
```

Log entries for pokemon and raid processing include a reference ID in brackets (`[encounter_id]` or `[gym_id]`) for traceability.

When `webhookLogging` is enabled, every incoming webhook is written as one JSON object per line to the configured file, useful for debugging and replay.

### Alerter

The alerter writes matched webhook payloads to `logs/matched_webhooks-*.log` when webhook logging is enabled (`config.logger.enableLogs.webhooks = true`).
