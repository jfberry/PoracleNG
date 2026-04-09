# Render Queue Design

## Goal

Separate CPU-bound webhook processing (parse, match, enrich) from I/O-bound rendering (tile resolution, template rendering, Shlink URL shortening, HTTP delivery) to prevent tile generation latency (200-500ms) from blocking webhook worker goroutines.

## Architecture

```
HTTP goroutine: accept webhook → worker pool queue
Worker goroutine (4 default): pull → parse → match → enrich → send to renderCh → return
Render goroutine (8 default): pull from renderCh → resolve tile → render → shlink → deliver
```

Three independent stages with buffered handoffs:
1. **HTTP → Worker**: existing worker pool semaphore
2. **Worker → Render**: new buffered `renderCh` channel (configurable size, default 100)
3. **Render → Alerter**: synchronous HTTP POST per render goroutine

Workers produce `RenderJob` structs and immediately return to process the next webhook. Render goroutines consume jobs, handle all I/O (tile wait, Shlink, HTTP delivery), and tolerate latency.

## RenderJob

```go
type RenderJob struct {
    TemplateType      string
    Enrichment        map[string]any
    PerLangEnrichment map[string]map[string]any
    PerUserEnrichment map[string]map[string]any
    WebhookFields     map[string]any
    MatchedUsers      []webhook.MatchedUser
    MatchedAreas      []webhook.MatchedArea
    TilePending       *staticmap.TilePending
    IsEncountered     bool   // pokemon only
    LogReference      string
}
```

## Tile Resolution with Queue Pressure

The render goroutine resolves tiles with pressure-aware degradation:

```go
pressure := float64(len(renderCh)) / float64(cap(renderCh))

if pressure > 0.8 || job.TilePending == nil {
    // Skip tile wait — use fallback URL
    if job.TilePending != nil {
        job.TilePending.Apply(job.TilePending.Fallback)
    }
} else {
    // Normal tile resolution with deadline
    wait := time.Until(job.TilePending.Deadline)
    if wait <= 0 { wait = time.Millisecond }
    select {
    case url := <-job.TilePending.Result:
        job.TilePending.Apply(url)
    case <-time.After(wait):
        job.TilePending.Apply(job.TilePending.Fallback)
    }
}
```

At >80% queue depth, tiles use fallback URLs immediately. Alerts still deliver (with generic map tile URL). When pressure drops, tiles resolve normally.

## Config

```toml
[tuning]
render_pool_size = 8       # concurrent render goroutines (default 8)
render_queue_size = 100    # max buffered render jobs (default 100)
```

## Observability

**Prometheus metrics:**
- `poracle_render_queue_depth` — gauge, current items in renderCh
- `poracle_render_queue_capacity` — gauge, configured max
- `poracle_render_duration_seconds` — histogram, total render time (tile + template + shlink + deliver)
- `poracle_render_tile_skipped_total` — counter, tiles skipped due to queue pressure
- `poracle_render_total` — counter with `{status="ok|error"}`

**Periodic status log** (existing `[Status]` line in main.go):
```
[Status] Webhooks: 120/min | Matched: 45/min | RenderQ: 3/100 | Tiles: 12 avg:350ms | Geo: 8 avg:2ms
```

**Warning log** when queue exceeds 80%:
```
WARN Render queue pressure: 85/100 (85%) — tile resolution skipped
```

## Handler Changes

Each webhook handler changes from:
```go
// Current: inline tile wait + render
webhookFields := parseWebhookFields(raw)
if tilePending != nil {
    select { ... } // blocks worker 200-500ms
}
jobs := ps.dtsRenderer.RenderAlert(...)
ps.sender.DeliverMessages(jobs)
```

To:
```go
// New: queue render job, return immediately
webhookFields := parseWebhookFields(raw)
ps.renderQueue <- RenderJob{
    TemplateType:      "raid",
    Enrichment:        baseEnrichment,
    PerLangEnrichment: perLang,
    WebhookFields:     webhookFields,
    MatchedUsers:      matched,
    MatchedAreas:      matchedAreas,
    TilePending:       tilePending,
    LogReference:      raid.GymID,
}
```

The render goroutine pool handles tile resolution, rendering, and delivery.

## ProcessorService Changes

```go
type ProcessorService struct {
    // ... existing fields ...
    renderCh   chan RenderJob
    renderWg   sync.WaitGroup
}
```

Started in `main.go` after renderer initialization:
```go
ps.renderCh = make(chan RenderJob, cfg.Tuning.RenderQueueSize)
for i := 0; i < cfg.Tuning.RenderPoolSize; i++ {
    ps.renderWg.Add(1)
    go ps.renderWorker()
}
```

Graceful shutdown: close `renderCh`, wait on `renderWg`.

## Weather Special Case

Weather already loops per-user with per-user tile resolution. The handler queues one `RenderJob` per user (each with their own `TilePending`). The render goroutine resolves each independently.

## File Changes

- `processor/cmd/processor/main.go` — render pool startup, shutdown, status logging
- `processor/cmd/processor/render.go` — new file: `RenderJob` struct, `renderWorker()` goroutine
- `processor/cmd/processor/pokemon.go` — queue RenderJob instead of inline render
- `processor/cmd/processor/raid.go` (and all other handlers) — same
- `processor/internal/config/config.go` — add `RenderPoolSize`, `RenderQueueSize` to TuningConfig
- `processor/internal/metrics/metrics.go` — add render queue metrics
- `config/config.example.toml` — document new tuning options
