# Go-Native Message Delivery Design

## Goal

Replace the Node.js alerter's Discord and Telegram delivery workers with Go-native REST-based delivery in the processor. Pure HTTP — no Discord.js, no Telegraf. The alerter process is eliminated entirely for message delivery.

## Architecture

```
Render worker produces DeliveryJob
    → Dispatcher.Dispatch(job)
    → Fair queue (per-destination, concurrency-limited)
    → Platform sender (DiscordSender or TelegramSender)
    → REST API call (with rate limit awareness)
    → MessageTracker (if clean or edit key set)
```

## Package Structure

```
processor/internal/delivery/
  delivery.go      — Dispatcher, Job, Sender interface, NewDispatcher()
  queue.go         — Per-destination fair queue with semaphore concurrency control
  discord.go       — Discord REST sender (DM, channel, thread, webhook)
  telegram.go      — Telegram REST sender (text, photo, sticker, venue, location)
  ratelimit.go     — Discord per-destination rate limit tracker with global limit
  tracker.go       — MessageTracker: TTL cache for clean + edit, disk persistence
  image.go         — Image download + Discord multipart form builder
```

## Sender Interface

```go
type Sender interface {
    Send(ctx context.Context, job *Job) (*SentMessage, error)
    Delete(ctx context.Context, sentID string) error
    Edit(ctx context.Context, sentID string, message json.RawMessage) error
    Platform() string
}

type SentMessage struct {
    ID string // opaque platform-specific ID (sender knows how to parse for delete/edit)
}
```

The sent ID is opaque to the dispatcher and tracker. Each sender encodes whatever it needs:
- Discord bot: `"channelID:messageID"`
- Discord webhook: `"webhookURL:messageID"`
- Telegram: `"chatID:messageID"`

## Job

```go
type Job struct {
    Target       string          // user/channel/thread/webhook ID or URL
    Type         string          // "discord:user", "discord:channel", "discord:thread", "webhook",
                                 // "telegram:user", "telegram:group", "telegram:channel"
    Message      json.RawMessage // pre-rendered message JSON
    TTH          TTH             // time to hide (for clean/edit expiry)
    Clean        bool            // track for deletion on TTH expiry
    EditKey      string          // non-empty = track for future edits (also expires at TTH)
    Name         string          // human-readable destination name (for logging)
    LogReference string          // encounter/gym ID (for tracing)
    Lat, Lon     float64         // coordinates (for Telegram venue/location)
}
```

Tracking behavior:
- `Clean=false, EditKey=""` → fire and forget (no tracking, no memory)
- `Clean=true, EditKey=""` → track for deletion only
- `Clean=false, EditKey="X"` → track for edit only
- `Clean=true, EditKey="X"` → track for both

## Dispatcher

```go
type Dispatcher struct {
    discordSender  *DiscordSender
    telegramSender *TelegramSender
    queue          *FairQueue
    tracker        *MessageTracker
}

func (d *Dispatcher) Dispatch(job *Job) {
    d.queue.Enqueue(job)
}
```

The dispatcher owns everything. The render worker calls `Dispatch()` which enqueues the job. The fair queue drains jobs to the appropriate sender, then tracks if needed.

## Fair Queue

Per-destination serialization with configurable concurrency:

```go
type FairQueue struct {
    ch         chan *Job
    senders    map[string]Sender  // platform → sender
    tracker    *MessageTracker
    busy       map[string]bool    // target → currently sending
    mu         sync.Mutex
    sem        chan struct{}       // concurrency limiter
}
```

- Max 1 concurrent send per destination (prevents flooding one user)
- Global concurrency limit via semaphore (configurable per platform)
- Jobs for busy destinations wait until the current send completes

Config:
```toml
[tuning]
concurrent_discord_destinations = 10
concurrent_telegram_destinations = 5
```

## Discord Sender

Pure REST, no libraries.

```go
type DiscordSender struct {
    token        string
    client       *http.Client
    rateLimiter  *DiscordRateLimiter
    uploadImages bool
    dmChannels   sync.Map  // userID → DM channelID (cached for session)
}
```

### Destination Routing

| Type | Action |
|------|--------|
| `discord:user` | Create DM channel (cached) → POST `/channels/{dmChannelID}/messages` |
| `discord:channel` | POST `/channels/{channelID}/messages` |
| `discord:thread` | POST `/channels/{threadID}/messages` (threads are channels in Discord API) |
| `webhook` | POST `{webhookURL}?wait=true` |

### Message Normalization
- `embed` (singular) → `embeds[]` (array)
- Color string → integer: `#`-prefixed or 6-char → hex parse; otherwise decimal parse

### Image Upload (if `uploadEmbedImages` enabled)
1. Extract `embeds[0].image.url`
2. HTTP GET the image
3. Replace URL with `"attachment://map.png"`
4. Build `multipart/form-data`: `payload_json` field + `files[0]` field (bot) or `file` field (webhook)
5. Fallback: if download fails, send with URL in embed

### Rate Limiting

Per-destination tracking (aligned with per-destination queue serialization):

```go
type DiscordRateLimiter struct {
    mu       sync.Mutex
    targets  map[string]*targetLimit
    global   tokenBucket  // 50 req/sec global
}
```

On each response, update from headers:
- `X-RateLimit-Remaining` → remaining count
- `X-RateLimit-Reset-After` → seconds until reset

Before sending, check remaining > 0 or wait until reset. On 429: parse `Retry-After` using Dexter's heuristic (`> 1000 → milliseconds, else → seconds`), add jitter, retry up to 10 times.

### Sent ID Format
- Bot messages: `"channelID:messageID"`
- Webhook messages: `"webhookURL:messageID"`

### Delete
- Bot: `DELETE /channels/{channelID}/messages/{messageID}` with `Authorization: Bot {token}`
- Webhook: `DELETE {webhookURL}/messages/{messageID}`

### Edit
- Bot: `PATCH /channels/{channelID}/messages/{messageID}`
- Webhook: `PATCH {webhookURL}/messages/{messageID}`

### Error Handling
- 50007 (cannot send to user), 10003 (unknown channel), 10013 (unknown user) → permanent failure, disable user
- 429 → retry with backoff
- 5xx → retry with backoff (up to 5 retries)

## Telegram Sender

```go
type TelegramSender struct {
    token  string
    client *http.Client
}
```

All endpoints: `POST https://api.telegram.org/bot{token}/{method}` with JSON body.

### Send Order

The rendered message contains Telegram-specific fields: `content`, `sticker`, `photo`, `location`, `venue`, `send_order`, `parse_mode`, `webpage_preview`.

The sender parses these and sends multiple API calls in order:
1. Parse `send_order` (default: `["sticker", "photo", "text", "location", "venue"]`)
2. For each item in order, send if present:
   - `sticker` → `sendSticker`
   - `photo` → `sendPhoto`
   - `text` → `sendMessage` (with `parse_mode`, `disable_web_page_preview`)
   - `location` → `sendLocation`
   - `venue` → `sendVenue`
3. Track the **text message ID** as the sent ID (for clean/edit)

### Parse Mode
Normalize case-insensitively to `"Markdown"`, `"MarkdownV2"`, or `"HTML"`. Default `"Markdown"`.

### Rate Limiting
Simple: on 429, parse `parameters.retry_after` (seconds) from response body. Sleep and retry up to 5 times.

### Sent ID Format
`"chatID:messageID"`

### Delete
`POST /bot{token}/deleteMessage` with `{"chat_id": ..., "message_id": ...}`

### Edit
`POST /bot{token}/editMessageText` with `{"chat_id": ..., "message_id": ..., "text": ..., "parse_mode": ...}`

### Error Handling
- 403 (forbidden/blocked) → permanent failure, disable user
- 429 → retry with `retry_after`
- Other → retry with backoff (up to 5 retries)

## Message Tracker

Single TTL cache for both clean and edit tracking, backed by `ttlcache/v3`.

```go
type MessageTracker struct {
    cache   *ttlcache.Cache[string, *TrackedMessage]
    senders map[string]Sender
    mu      sync.Mutex
}

type TrackedMessage struct {
    SentID string
    Target string
    Type   string
    Clean  bool
}
```

### Cache Keys
- Edit-trackable messages: key = `editKey`
- Clean-only messages: key = `"clean:{type}:{target}:{sentID}"`

### Expiry Callback
On TTL expiry (`cache.OnEviction`):
- If `Clean=true`: resolve sender from `Type`, call `sender.Delete(sentID)`
- Remove from cache (automatic)

### Edit Lookup
When a new job has `EditKey`:
1. `cache.Get(editKey)` → find previous message
2. If found: call `sender.Edit(sentID, newMessage)`
3. On success: update `SentID` in cache entry (message ID may change on some platforms)
4. On failure: send as new message, update cache with new sent ID

### Disk Persistence

**On shutdown**: iterate cache, save to `.cache/delivery-tracker.json`:
```json
[
    {"key": "enc_123_user_456", "sentID": "123:789", "target": "456", "type": "discord:user", "clean": true, "expiresAt": "2026-03-30T12:00:00Z"},
    {"key": "clean:telegram:user:789:42", "sentID": "789:42", "target": "789", "type": "telegram:user", "clean": true, "expiresAt": "2026-03-30T12:05:00Z"}
]
```

**On startup**: load file, for each entry:
- If `expiresAt` is past and `Clean`: delete immediately (async)
- If `expiresAt` is past and not clean: discard
- If `expiresAt` is future: re-insert with remaining TTL

## Config

```toml
[discord]
token = ["bot-token-1"]           # existing
upload_embed_images = true         # download + reupload static map tiles

[telegram]
token = ["bot-token-1"]           # existing

[tuning]
concurrent_discord_destinations = 10
concurrent_telegram_destinations = 5
delivery_queue_size = 200          # buffered channel capacity
```

## Integration

The render worker currently calls `sender.DeliverMessages(jobs)` which POSTs to the alerter. This changes to:

```go
for _, job := range jobs {
    dispatcher.Dispatch(&delivery.Job{
        Target:  job.Target,
        Type:    job.Type,
        Message: job.Message,
        TTH:     job.TTH,
        Clean:   job.Clean,
        // EditKey from matched user tracking rule (future)
    })
}
```

The `/api/deliverMessages` alerter endpoint and the alerter's Discord/Telegram workers are no longer needed. The alerter process can be removed entirely once commands are also migrated (future work).

## Metrics

- `poracle_delivery_total` — counter with `{platform, status}` labels
- `poracle_delivery_duration_seconds` — histogram per platform
- `poracle_delivery_queue_depth` — gauge
- `poracle_delivery_tracker_size` — gauge (tracked messages for clean/edit)
- `poracle_delivery_clean_total` — counter (successful deletions)
- `poracle_delivery_rate_limited_total` — counter with `{platform}`

## File Changes

### New
- `processor/internal/delivery/` — entire package (7 files)

### Modified
- `processor/cmd/processor/render.go` — dispatch to delivery instead of HTTP POST
- `processor/cmd/processor/main.go` — initialize dispatcher, shutdown
- `processor/internal/config/config.go` — delivery config fields

### Removed (after validation)
- `alerter/src/lib/discord/discordWorker.js`
- `alerter/src/lib/discord/discordWebhookWorker.js`
- `alerter/src/lib/telegram/Telegram.js` (delivery parts)
- `alerter/src/lib/FairPromiseQueue.js` (already removed)
- `alerter/src/routes/deliverMessages.js`
