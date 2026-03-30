# Go-Native Message Delivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Node.js alerter's Discord and Telegram delivery workers with Go-native REST-based delivery in the processor.

**Architecture:** A `delivery` package with a `Dispatcher` that routes `Job`s through a per-destination fair queue to platform-specific `Sender` implementations (Discord REST, Telegram REST). A `MessageTracker` backed by `ttlcache/v3` handles clean deletion on TTH expiry and edit lookups, with disk persistence across restarts.

**Tech Stack:** Go, `net/http` (no Discord/Telegram libraries), `ttlcache/v3`, `encoding/json`, `mime/multipart`.

**Spec:** `docs/superpowers/specs/2026-03-30-processor-delivery-design.md`

---

## File Structure

```
processor/internal/delivery/
  delivery.go          — Job, SentMessage, Sender interface, Dispatcher
  delivery_test.go
  queue.go             — FairQueue: per-destination serialization + concurrency control
  queue_test.go
  discord.go           — DiscordSender: DM, channel, thread, webhook REST delivery
  discord_test.go
  telegram.go          — TelegramSender: text, photo, sticker, venue, location
  telegram_test.go
  ratelimit.go         — DiscordRateLimiter: per-destination + global rate tracking
  ratelimit_test.go
  tracker.go           — MessageTracker: TTL cache for clean/edit + disk persistence
  tracker_test.go
  image.go             — Image download + Discord multipart form builder
  image_test.go

processor/internal/config/config.go    — Add delivery config fields
processor/internal/metrics/metrics.go  — Add delivery metrics
processor/cmd/processor/main.go        — Initialize dispatcher, shutdown
processor/cmd/processor/render.go      — Dispatch to delivery instead of HTTP POST
config/config.example.toml             — Document delivery config
```

---

### Task 1: Types, Interface, and Config

**Files:**
- Create: `processor/internal/delivery/delivery.go`
- Modify: `processor/internal/config/config.go`
- Modify: `processor/internal/metrics/metrics.go`
- Modify: `config/config.example.toml`

- [ ] **Step 1: Create delivery.go with core types**

```go
package delivery

type TTH struct {
    Days    int
    Hours   int
    Minutes int
    Seconds int
}

func (t TTH) Duration() time.Duration {
    return time.Duration(t.Days)*24*time.Hour +
        time.Duration(t.Hours)*time.Hour +
        time.Duration(t.Minutes)*time.Minute +
        time.Duration(t.Seconds)*time.Second
}

type Job struct {
    Target       string
    Type         string          // "discord:user", "discord:channel", "discord:thread", "webhook",
                                 // "telegram:user", "telegram:group", "telegram:channel"
    Message      json.RawMessage
    TTH          TTH
    Clean        bool
    EditKey      string
    Name         string
    LogReference string
    Lat, Lon     float64
}

type SentMessage struct {
    ID string // opaque platform-specific ID
}

type Sender interface {
    Send(ctx context.Context, job *Job) (*SentMessage, error)
    Delete(ctx context.Context, sentID string) error
    Edit(ctx context.Context, sentID string, message json.RawMessage) error
    Platform() string
}

// PlatformFromType extracts "discord" or "telegram" from a type string.
func PlatformFromType(typ string) string {
    if typ == "webhook" { return "discord" }
    parts := strings.SplitN(typ, ":", 2)
    return parts[0]
}
```

- [ ] **Step 2: Add config fields**

In `config.go`, add to `DiscordConfig`:
```go
UploadEmbedImages  bool `toml:"upload_embed_images"`
MessageDeleteDelay int  `toml:"message_delete_delay"` // extra ms for clean TTH on channels
```

Add to `TelegramConfig` if not already present:
```go
Token []string `toml:"token"` // (may already exist)
```

Add to `TuningConfig`:
```go
ConcurrentDiscordDestinations int `toml:"concurrent_discord_destinations"`
ConcurrentTelegramDestinations int `toml:"concurrent_telegram_destinations"`
ConcurrentDiscordWebhooks     int `toml:"concurrent_discord_webhooks"`
DeliveryQueueSize              int `toml:"delivery_queue_size"`
```

Add defaults:
```go
ConcurrentDiscordDestinations: 10,
ConcurrentTelegramDestinations: 10,
ConcurrentDiscordWebhooks: 10,
DeliveryQueueSize: 200,
```

- [ ] **Step 3: Add delivery metrics**

In `metrics.go`:
```go
var DeliveryTotal = promauto.NewCounterVec(...)       // {platform, status}
var DeliveryDuration = promauto.NewHistogramVec(...)   // {platform}
var DeliveryQueueDepth = promauto.NewGauge(...)
var DeliveryTrackerSize = promauto.NewGauge(...)
var DeliveryCleanTotal = promauto.NewCounter(...)
var DeliveryRateLimited = promauto.NewCounterVec(...)  // {platform}
```

- [ ] **Step 4: Update config.example.toml**

Add `delivery_queue_size = 200` to `[tuning]` section.

- [ ] **Step 5: Build and commit**

```bash
cd processor && go build ./cmd/processor
git commit -m "feat(delivery): add core types, Sender interface, config, and metrics"
```

---

### Task 2: Discord Rate Limiter

**Files:**
- Create: `processor/internal/delivery/ratelimit.go`
- Create: `processor/internal/delivery/ratelimit_test.go`

- [ ] **Step 1: Implement DiscordRateLimiter**

Per-destination rate limit tracking plus global 50 req/sec token bucket.

```go
type DiscordRateLimiter struct {
    mu       sync.Mutex
    targets  map[string]*targetLimit
    global   tokenBucket
}

type targetLimit struct {
    remaining int
    resetAt   time.Time
}

type tokenBucket struct {
    tokens    int
    maxTokens int
    lastRefill time.Time
    rate       float64 // tokens per second
}

func NewDiscordRateLimiter() *DiscordRateLimiter

// Wait blocks until we can send to the given target.
func (rl *DiscordRateLimiter) Wait(target string)

// Update records rate limit info from Discord response headers.
func (rl *DiscordRateLimiter) Update(target string, headers http.Header)

// HandleRateLimit sleeps for the retry-after duration on 429.
// Uses Dexter's heuristic: value > 1000 → milliseconds, else → seconds.
func (rl *DiscordRateLimiter) HandleRateLimit(retryAfter float64) time.Duration
```

- [ ] **Step 2: Write tests**

Test: Wait returns immediately when no limit, Wait blocks when remaining=0, Update parses headers, HandleRateLimit heuristic (seconds vs milliseconds), global token bucket refill.

- [ ] **Step 3: Build, test, commit**

```bash
cd processor && go test -v ./internal/delivery/ -run TestRateLimit
git commit -m "feat(delivery): add Discord per-destination rate limiter with global token bucket"
```

---

### Task 3: Image Download and Multipart Builder

**Files:**
- Create: `processor/internal/delivery/image.go`
- Create: `processor/internal/delivery/image_test.go`

- [ ] **Step 1: Implement image download and multipart builder**

```go
// DownloadImage fetches an image URL with a short timeout.
func DownloadImage(client *http.Client, url string) ([]byte, error)

// BuildMultipartMessage creates a multipart/form-data body for Discord image upload.
// fieldName is "files[0]" for bot messages or "file" for webhooks.
func BuildMultipartMessage(payload json.RawMessage, imageData []byte, fieldName string) (body *bytes.Buffer, contentType string, err error)

// NormalizeDiscordMessage normalizes embed→embeds[], coerces color strings to ints.
func NormalizeDiscordMessage(raw json.RawMessage) (json.RawMessage, error)

// ExtractEmbedImageURL extracts embeds[0].image.url from a message, if present.
func ExtractEmbedImageURL(raw json.RawMessage) string

// ReplaceEmbedImageURL replaces embeds[0].image.url with "attachment://map.png".
func ReplaceEmbedImageURL(raw json.RawMessage) json.RawMessage
```

- [ ] **Step 2: Write tests**

Test: NormalizeDiscordMessage (embed→embeds, color coercion), multipart builder produces valid form, ExtractEmbedImageURL from nested JSON, mock HTTP server for DownloadImage.

- [ ] **Step 3: Build, test, commit**

```bash
git commit -m "feat(delivery): add image download and Discord multipart builder"
```

---

### Task 4: Discord Sender

**Files:**
- Create: `processor/internal/delivery/discord.go`
- Create: `processor/internal/delivery/discord_test.go`

- [ ] **Step 1: Implement DiscordSender**

```go
type DiscordSender struct {
    token        string
    client       *http.Client
    rateLimiter  *DiscordRateLimiter
    uploadImages bool
    deleteDelay  time.Duration      // extra delay for channel clean (message_delete_delay)
    dmChannels   sync.Map           // userID → DM channelID
}

func NewDiscordSender(token string, uploadImages bool, deleteDelayMs int) *DiscordSender
```

**Send** routes by job.Type:
- `discord:user` → `ensureDMChannel(target)` → `postMessage(dmChannelID, message)`
- `discord:channel` → `postMessage(target, message)`
- `discord:thread` → `postMessage(target, message)` (threads are channels)
- `webhook` → `postWebhook(target, message)`

**postMessage**: POST to `/api/v10/channels/{id}/messages` with `Authorization: Bot {token}`. If `uploadImages` and message has embed image, download + multipart. Parse response for message ID. Rate limit update from response headers.

**postWebhook**: POST to `{webhookURL}?wait=true`. Different multipart field name (`file` not `files[0]`). No auth header.

**Delete**: Parse sentID (`channelID:messageID` or `webhookURL:messageID`). DELETE to appropriate endpoint.

**Edit**: Parse sentID. PATCH to appropriate endpoint.

**Error handling**: Retry up to 5 times on 429/5xx. Permanent errors (50007, 10003, 10013) return immediately with typed error for user disable.

- [ ] **Step 2: Write tests**

Use `httptest.NewServer` to mock Discord API. Test: DM channel creation + caching, message send, webhook send, 429 retry, image upload multipart, delete, edit, permanent error detection.

- [ ] **Step 3: Build, test, commit**

```bash
git commit -m "feat(delivery): add Discord REST sender with DM, channel, thread, webhook support"
```

---

### Task 5: Telegram Sender

**Files:**
- Create: `processor/internal/delivery/telegram.go`
- Create: `processor/internal/delivery/telegram_test.go`

- [ ] **Step 1: Implement TelegramSender**

```go
type TelegramSender struct {
    token  string
    client *http.Client
}

func NewTelegramSender(token string) *TelegramSender
```

**Send**: Parse message JSON for Telegram-specific fields (`content`, `sticker`, `photo`, `send_order`, `parse_mode`, `webpage_preview`, `location`, `venue`). Execute send order:
- `sendSticker(chatID, stickerID)`
- `sendPhoto(chatID, photoURL)`
- `sendMessage(chatID, text, parseMode, disableWebPreview)`
- `sendLocation(chatID, lat, lon)`
- `sendVenue(chatID, lat, lon, title, address)`

Return the text message's ID as the sent ID (`"chatID:messageID"`).

**Delete**: Parse sentID. POST to `/bot{token}/deleteMessage`.

**Edit**: Parse sentID. POST to `/bot{token}/editMessageText`.

**Rate limiting**: On 429, parse `parameters.retry_after` from response body. Sleep and retry up to 5 times.

**Error handling**: 403 (blocked/forbidden) → permanent error for user disable. Others → retry.

- [ ] **Step 2: Write tests**

Mock Telegram API. Test: send order execution, parse mode normalization, 429 retry, delete, edit, permanent error detection, multi-item send (sticker + text).

- [ ] **Step 3: Build, test, commit**

```bash
git commit -m "feat(delivery): add Telegram REST sender with send order support"
```

---

### Task 6: Message Tracker

**Files:**
- Create: `processor/internal/delivery/tracker.go`
- Create: `processor/internal/delivery/tracker_test.go`

- [ ] **Step 1: Implement MessageTracker**

```go
type TrackedMessage struct {
    SentID string
    Target string
    Type   string
    Clean  bool
}

type MessageTracker struct {
    cache    *ttlcache.Cache[string, *TrackedMessage]
    senders  map[string]Sender
    cacheDir string
}

func NewMessageTracker(cacheDir string, senders map[string]Sender) *MessageTracker
```

**Track**: Insert a message into the TTL cache with TTH duration.
```go
func (mt *MessageTracker) Track(key string, msg *TrackedMessage, ttl time.Duration)
```

Key = `editKey` if editable, or `"clean:{type}:{target}:{sentID}"` if clean-only.

**LookupEdit**: Find a previous message by edit key.
```go
func (mt *MessageTracker) LookupEdit(editKey string) *TrackedMessage
```

**UpdateEdit**: Update the sent ID after a successful edit (message ID may change).
```go
func (mt *MessageTracker) UpdateEdit(editKey string, newSentID string)
```

**Expiry callback**: On eviction (TTL expired), if `Clean=true`, resolve sender from `Type` and call `Delete(sentID)` asynchronously.

**Save/Load**: Persist cache to `{cacheDir}/delivery-tracker.json` on `Save()`, restore on `Load()`. Entries with expired TTL that are clean get immediate async deletion on load. Non-expired entries are re-inserted with remaining TTL.

- [ ] **Step 2: Write tests**

Test: Track + expiry triggers delete (mock sender), LookupEdit finds previous message, UpdateEdit changes sentID, Save/Load round-trip, expired clean entries deleted on load, non-clean expired entries discarded.

- [ ] **Step 3: Build, test, commit**

```bash
git commit -m "feat(delivery): add MessageTracker with TTL cache, clean deletion, and disk persistence"
```

---

### Task 7: Fair Queue

**Files:**
- Create: `processor/internal/delivery/queue.go`
- Create: `processor/internal/delivery/queue_test.go`

- [ ] **Step 1: Implement FairQueue**

```go
type FairQueue struct {
    ch       chan *Job
    senders  map[string]Sender
    tracker  *MessageTracker
    wg       sync.WaitGroup

    // Per-platform concurrency semaphores
    discordSem chan struct{}
    webhookSem chan struct{}
    telegramSem chan struct{}
}

func NewFairQueue(ch chan *Job, senders map[string]Sender, tracker *MessageTracker, cfg QueueConfig) *FairQueue

type QueueConfig struct {
    ConcurrentDiscord  int
    ConcurrentWebhook  int
    ConcurrentTelegram int
}
```

**Start**: Launch worker goroutines that drain the channel.

```go
func (fq *FairQueue) Start()
```

Workers:
1. Pull job from channel
2. Acquire platform-appropriate semaphore (blocks if at concurrency limit)
3. Determine sender from `PlatformFromType(job.Type)`
4. If job has `EditKey`, check tracker for previous message → attempt edit
5. If no edit (or edit failed), send as new message
6. If send succeeds and (`Clean || EditKey != ""`), track in MessageTracker
7. Release semaphore
8. Record metrics

**Stop**: Close channel, wait for workers to drain.

```go
func (fq *FairQueue) Stop() {
    close(fq.ch)
    fq.wg.Wait()
}
```

- [ ] **Step 2: Write tests**

Test: jobs routed to correct sender, concurrency limit enforced, edit lookup + fallback to new send, clean tracking after send, stop drains remaining jobs.

- [ ] **Step 3: Build, test, commit**

```bash
git commit -m "feat(delivery): add FairQueue with per-platform concurrency and edit/clean tracking"
```

---

### Task 8: Dispatcher

**Files:**
- Modify: `processor/internal/delivery/delivery.go`
- Create: `processor/internal/delivery/delivery_test.go`

- [ ] **Step 1: Add Dispatcher to delivery.go**

```go
type DispatcherConfig struct {
    DiscordToken      string
    TelegramToken     string
    UploadImages      bool
    DeleteDelayMs     int
    QueueSize         int
    CacheDir          string
    QueueConfig       QueueConfig
}

type Dispatcher struct {
    queue   *FairQueue
    tracker *MessageTracker
    ch      chan *Job
}

func NewDispatcher(cfg DispatcherConfig) (*Dispatcher, error)

func (d *Dispatcher) Dispatch(job *Job) {
    d.ch <- job
}

func (d *Dispatcher) Start()

func (d *Dispatcher) Stop() {
    d.queue.Stop()
    d.tracker.Save()
}

func (d *Dispatcher) QueueDepth() int {
    return len(d.ch)
}

func (d *Dispatcher) TrackerSize() int {
    return d.tracker.Size()
}
```

`NewDispatcher` creates senders, tracker, queue, loads tracker cache.

- [ ] **Step 2: Write integration test**

Test full flow: create dispatcher with mock senders, dispatch job, verify sender.Send called, verify tracker populated for clean job.

- [ ] **Step 3: Build, test, commit**

```bash
git commit -m "feat(delivery): add Dispatcher orchestrating queue, senders, and tracker"
```

---

### Task 9: Wire into Processor

**Files:**
- Modify: `processor/cmd/processor/main.go`
- Modify: `processor/cmd/processor/render.go`

- [ ] **Step 1: Initialize Dispatcher in main.go**

After render pool setup, create and start the dispatcher:

```go
dispatcher, err := delivery.NewDispatcher(delivery.DispatcherConfig{
    DiscordToken:  cfg.Discord.Token[0], // first token
    TelegramToken: cfg.Telegram.Token[0],
    UploadImages:  cfg.Discord.UploadEmbedImages,
    DeleteDelayMs: cfg.Discord.MessageDeleteDelay,
    QueueSize:     cfg.Tuning.DeliveryQueueSize,
    CacheDir:      filepath.Join(cfg.BaseDir, "config", ".cache"),
    QueueConfig: delivery.QueueConfig{
        ConcurrentDiscord:  cfg.Tuning.ConcurrentDiscordDestinations,
        ConcurrentWebhook:  cfg.Tuning.ConcurrentDiscordWebhooks,
        ConcurrentTelegram: cfg.Tuning.ConcurrentTelegramDestinations,
    },
})
dispatcher.Start()
```

Add to ProcessorService struct. Add to graceful shutdown:
```go
dispatcher.Stop()
```

Add to periodic status log:
```go
statusParts = append(statusParts, fmt.Sprintf("DeliveryQ: %d", dispatcher.QueueDepth()))
metrics.DeliveryQueueDepth.Set(float64(dispatcher.QueueDepth()))
metrics.DeliveryTrackerSize.Set(float64(dispatcher.TrackerSize()))
```

- [ ] **Step 2: Update render.go to dispatch instead of HTTP POST**

In `processRenderJob`, replace `ps.sender.DeliverMessages(jobs)` with:

```go
for _, job := range deliveryJobs {
    ps.dispatcher.Dispatch(&delivery.Job{
        Target:       job.Target,
        Type:         job.Type,
        Message:      job.Message,
        TTH:          delivery.TTH{...},  // convert from webhook.DeliveryJob TTH
        Clean:        job.Clean,
        Name:         job.Name,
        LogReference: job.LogReference,
        Lat:          parseLat(job.Lat),
        Lon:          parseLon(job.Lon),
    })
}
```

Remove the `sender.DeliverMessages` call and the HTTP POST to alerter.

- [ ] **Step 3: Build and verify**

```bash
cd processor && go build ./cmd/processor
```

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: wire delivery dispatcher into processor, replace alerter HTTP delivery"
```

---

### Task 10: Remove Alerter Delivery Infrastructure

**Files to remove** (after delivery is validated):
- `alerter/src/lib/discord/discordWorker.js`
- `alerter/src/lib/discord/discordWebhookWorker.js`
- `alerter/src/routes/deliverMessages.js`

**Files to modify:**
- `alerter/src/app.js` — remove Discord/Telegram worker setup, queue declarations, deliverMessages route

**Note**: Telegram delivery parts of `Telegram.js` stay for now (the bot command handling lives there). Only the delivery queue drain loop and send methods are removed. This task should be done carefully after full validation.

- [ ] **Step 1: Remove Discord worker files**
- [ ] **Step 2: Remove deliverMessages route**
- [ ] **Step 3: Clean up app.js**
- [ ] **Step 4: Verify alerter still starts** (for command handling)
- [ ] **Step 5: Commit**

```bash
git commit -m "feat: remove alerter delivery workers — processor handles all delivery"
```

---

### Task 11: Integration Testing

- [ ] **Step 1: Start processor with delivery enabled**

Verify log:
```
Delivery dispatcher started: discord=10 webhook=10 telegram=10 queue=200
Delivery tracker loaded: 0 entries from cache
```

- [ ] **Step 2: Test Discord DM delivery**

Trigger a pokemon alert for a Discord user. Verify message arrives in DM.

- [ ] **Step 3: Test Discord channel delivery**

Trigger alert for a registered channel. Verify message in channel.

- [ ] **Step 4: Test Discord webhook delivery**

Trigger alert for a registered webhook. Verify message delivered.

- [ ] **Step 5: Test Telegram delivery**

Trigger alert for a Telegram user. Verify message arrives with correct send order.

- [ ] **Step 6: Test clean deletion**

Enable `clean` on a tracking rule. Trigger alert. Verify message appears then is deleted after TTH.

- [ ] **Step 7: Test image upload**

Enable `upload_embed_images`. Verify static map appears as attachment, not URL.

- [ ] **Step 8: Check Prometheus metrics**

```bash
curl -s http://localhost:4200/metrics | grep poracle_delivery
```

- [ ] **Step 9: Test cache persistence**

Trigger clean alert. Restart processor before TTH expires. Verify message is still deleted after restart.

- [ ] **Step 10: Commit any fixes**
