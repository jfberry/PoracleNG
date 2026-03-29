# Render Queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Separate CPU-bound webhook processing from I/O-bound rendering via a buffered channel + render goroutine pool, preventing tile generation latency from blocking webhook workers.

**Architecture:** Webhook handlers enrich and queue a `RenderJob` to a buffered channel, returning immediately. A configurable pool of render goroutines consumes jobs, resolves tiles (with queue-pressure degradation), renders templates, shortens URLs, and delivers to the alerter. Prometheus metrics expose queue depth, render duration, and tile skip counts.

**Tech Stack:** Go channels, sync.WaitGroup, existing dts/metrics packages.

**Spec:** `docs/superpowers/specs/2026-03-29-render-queue-design.md`

---

## File Structure

```
processor/cmd/processor/
  render.go         — NEW: RenderJob struct, renderWorker(), queue pressure logic
  main.go           — Modified: start render pool, add to status log, graceful shutdown
  pokemon.go        — Modified: queue RenderJob instead of inline render
  raid.go           — Modified: same
  invasion.go       — Modified: same
  quest.go          — Modified: same
  lure.go           — Modified: same
  nest.go           — Modified: same
  gym.go            — Modified: same
  fort.go           — Modified: same
  maxbattle.go      — Modified: same
  weather.go        — Modified: same

processor/internal/config/config.go    — Modified: add RenderPoolSize, RenderQueueSize
processor/internal/metrics/metrics.go  — Modified: add render queue metrics
config/config.example.toml             — Modified: document new tuning options
```

---

### Task 1: Config and Metrics

**Files:**
- Modify: `processor/internal/config/config.go`
- Modify: `processor/internal/metrics/metrics.go`
- Modify: `config/config.example.toml`

- [ ] **Step 1: Add config fields**

In `config.go`, add to `TuningConfig`:
```go
RenderPoolSize  int `toml:"render_pool_size"`   // concurrent render goroutines (default 8)
RenderQueueSize int `toml:"render_queue_size"`  // max buffered render jobs (default 100)
```

Add defaults in the config initialization:
```go
RenderPoolSize:  8,
RenderQueueSize: 100,
```

- [ ] **Step 2: Add Prometheus metrics**

In `metrics.go`, add:
```go
var RenderQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
    Name: "poracle_render_queue_depth",
    Help: "Current number of items in the render queue",
})

var RenderQueueCapacity = promauto.NewGauge(prometheus.GaugeOpts{
    Name: "poracle_render_queue_capacity",
    Help: "Configured render queue capacity",
})

var RenderDuration = promauto.NewHistogram(prometheus.HistogramOpts{
    Name:    "poracle_render_duration_seconds",
    Help:    "Time to process a render job (tile + render + deliver)",
    Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
})

var RenderTotal = promauto.NewCounterVec(prometheus.CounterOpts{
    Name: "poracle_render_total",
    Help: "Total render jobs processed",
}, []string{"status"})

var RenderTileSkipped = promauto.NewCounter(prometheus.CounterOpts{
    Name: "poracle_render_tile_skipped_total",
    Help: "Tiles skipped due to render queue pressure",
})
```

- [ ] **Step 3: Update config example**

Add to `[tuning]` section in `config/config.example.toml`:
```toml
render_pool_size = 8                             # concurrent render goroutines
render_queue_size = 100                          # max buffered render jobs
```

- [ ] **Step 4: Build**

```bash
cd processor && go build ./cmd/processor
```

- [ ] **Step 5: Commit**

```bash
git add processor/internal/config/config.go processor/internal/metrics/metrics.go config/config.example.toml
git commit -m "feat: add render queue config and Prometheus metrics"
```

---

### Task 2: RenderJob and Render Worker

**Files:**
- Create: `processor/cmd/processor/render.go`

- [ ] **Step 1: Create render.go**

This file contains:

**RenderJob struct:**
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
    IsEncountered     bool   // pokemon-only: selects monster vs monsterNoIv template
    IsPokemon         bool   // true = RenderPokemon, false = RenderAlert
    LogReference      string
}
```

**renderWorker goroutine:**
```go
func (ps *ProcessorService) renderWorker() {
    defer ps.renderWg.Done()
    for job := range ps.renderCh {
        start := time.Now()
        ps.processRenderJob(job)
        metrics.RenderDuration.Observe(time.Since(start).Seconds())
    }
}
```

**processRenderJob:**
```go
func (ps *ProcessorService) processRenderJob(job RenderJob) {
    l := log.WithField("ref", job.LogReference)

    // 1. Resolve tile with queue pressure awareness
    if job.TilePending != nil {
        pressure := float64(len(ps.renderCh)) / float64(cap(ps.renderCh))
        if pressure > 0.8 {
            job.TilePending.Apply(job.TilePending.Fallback)
            metrics.RenderTileSkipped.Inc()
            l.Debugf("Tile skipped (queue pressure %.0f%%)", pressure*100)
        } else {
            wait := time.Until(job.TilePending.Deadline)
            if wait <= 0 {
                wait = time.Millisecond
            }
            select {
            case url := <-job.TilePending.Result:
                job.TilePending.Apply(url)
            case <-time.After(wait):
                job.TilePending.Apply(job.TilePending.Fallback)
            }
        }
    }

    // 2. Render
    var jobs []webhook.DeliveryJob
    if job.IsPokemon {
        jobs = ps.dtsRenderer.RenderPokemon(
            job.Enrichment,
            job.PerLangEnrichment,
            job.PerUserEnrichment,
            job.WebhookFields,
            job.MatchedUsers,
            job.MatchedAreas,
            job.IsEncountered,
            job.LogReference,
        )
    } else {
        jobs = ps.dtsRenderer.RenderAlert(
            job.TemplateType,
            job.Enrichment,
            job.PerLangEnrichment,
            job.WebhookFields,
            job.MatchedUsers,
            job.MatchedAreas,
            job.LogReference,
        )
    }

    // 3. Deliver
    if len(jobs) > 0 {
        if err := ps.sender.DeliverMessages(jobs); err != nil {
            l.Errorf("Failed to deliver rendered messages: %s", err)
            metrics.RenderTotal.WithLabelValues("error").Inc()
            return
        }
    }
    metrics.RenderTotal.WithLabelValues("ok").Inc()
}
```

Note: import `staticmap` package for `TilePending` type.

- [ ] **Step 2: Build**

```bash
cd processor && go build ./cmd/processor
```

- [ ] **Step 3: Commit**

```bash
git add processor/cmd/processor/render.go
git commit -m "feat: add RenderJob struct and render worker goroutine"
```

---

### Task 3: Wire Render Pool into ProcessorService

**Files:**
- Modify: `processor/cmd/processor/main.go`

- [ ] **Step 1: Add render queue fields to ProcessorService**

Find the `ProcessorService` struct (in main.go or wherever it's defined). Add:
```go
renderCh chan RenderJob
renderWg sync.WaitGroup
```

- [ ] **Step 2: Start render pool after DTS renderer initialization**

After `dtsRenderer` is created, add:
```go
// Start render pool
ps.renderCh = make(chan RenderJob, cfg.Tuning.RenderQueueSize)
metrics.RenderQueueCapacity.Set(float64(cfg.Tuning.RenderQueueSize))
renderPoolSize := cfg.Tuning.RenderPoolSize
if renderPoolSize < 1 {
    renderPoolSize = 8
}
for i := 0; i < renderPoolSize; i++ {
    ps.renderWg.Add(1)
    go ps.renderWorker()
}
log.Infof("Render pool started: %d workers, queue size %d", renderPoolSize, cfg.Tuning.RenderQueueSize)
```

- [ ] **Step 3: Add render queue depth to periodic status log**

In the periodic status summary block (around line 316), add render queue depth:
```go
if ps.renderCh != nil {
    statusParts = append(statusParts, fmt.Sprintf("RenderQ: %d/%d",
        len(ps.renderCh), cap(ps.renderCh)))
    metrics.RenderQueueDepth.Set(float64(len(ps.renderCh)))
}
```

- [ ] **Step 4: Add graceful shutdown**

In the shutdown section (signal handling or deferred cleanup), add:
```go
if ps.renderCh != nil {
    close(ps.renderCh)
    ps.renderWg.Wait()
    log.Info("Render pool stopped")
}
```

- [ ] **Step 5: Build and verify**

```bash
cd processor && go build ./cmd/processor
```

- [ ] **Step 6: Commit**

```bash
git add processor/cmd/processor/main.go
git commit -m "feat: wire render pool into ProcessorService with status logging"
```

---

### Task 4: Update All Webhook Handlers

**Files:**
- Modify: `processor/cmd/processor/pokemon.go`
- Modify: `processor/cmd/processor/raid.go`
- Modify: `processor/cmd/processor/invasion.go`
- Modify: `processor/cmd/processor/quest.go`
- Modify: `processor/cmd/processor/lure.go`
- Modify: `processor/cmd/processor/nest.go`
- Modify: `processor/cmd/processor/gym.go`
- Modify: `processor/cmd/processor/fort.go`
- Modify: `processor/cmd/processor/maxbattle.go`
- Modify: `processor/cmd/processor/weather.go`

Each handler changes from:
```go
// OLD: inline tile wait + render + deliver
webhookFields := parseWebhookFields(raw)
if tilePending != nil {
    select { ... }  // blocks 200-500ms
}
jobs := ps.dtsRenderer.RenderAlert(...)
ps.sender.DeliverMessages(jobs)
```

To:
```go
// NEW: queue render job, return immediately
webhookFields := parseWebhookFields(raw)
ps.renderCh <- RenderJob{
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

- [ ] **Step 1: Update pokemon.go**

Pokemon is special — uses `RenderPokemon` not `RenderAlert`. Set `IsPokemon: true` and `IsEncountered`. Remove the inline tile resolution block and the `RenderPokemon` + `DeliverMessages` calls.

- [ ] **Step 2: Update raid.go**

Template type is the `msgType` variable (either `"raid"` or `"egg"`). Remove tile resolution + `RenderAlert` + `DeliverMessages`.

- [ ] **Step 3: Update invasion.go**

Template type `"invasion"`. Same pattern.

- [ ] **Step 4: Update quest.go**

Template type `"quest"`.

- [ ] **Step 5: Update lure.go**

Template type `"lure"`.

- [ ] **Step 6: Update nest.go**

Template type `"nest"`.

- [ ] **Step 7: Update gym.go**

Template type `"gym"`.

- [ ] **Step 8: Update fort.go**

Template type `"fort-update"`.

- [ ] **Step 9: Update maxbattle.go**

Template type `"maxbattle"`.

- [ ] **Step 10: Update weather.go**

Weather is special — it loops per-user with per-user tile pending. Each user becomes a separate `RenderJob`:
```go
for _, user := range matched {
    // ... compute perLang with per-user tile ...
    ps.renderCh <- RenderJob{
        TemplateType:      "weatherchange",
        Enrichment:        baseEnrichment,
        PerLangEnrichment: perLang,
        WebhookFields:     webhookFields,
        MatchedUsers:      []webhook.MatchedUser{user},
        MatchedAreas:      matchedAreas,
        TilePending:       tilePending,  // per-user tile
        LogReference:      change.S2CellID,
    }
}
```

Remove the `allJobs` accumulation and `DeliverMessages` call. Each per-user job is delivered independently by the render worker.

- [ ] **Step 11: Build and test**

```bash
cd processor && go build ./cmd/processor
```

- [ ] **Step 12: Commit**

```bash
git add processor/cmd/processor/pokemon.go processor/cmd/processor/raid.go \
       processor/cmd/processor/invasion.go processor/cmd/processor/quest.go \
       processor/cmd/processor/lure.go processor/cmd/processor/nest.go \
       processor/cmd/processor/gym.go processor/cmd/processor/fort.go \
       processor/cmd/processor/maxbattle.go processor/cmd/processor/weather.go
git commit -m "feat: all webhook handlers queue to render pool instead of inline render"
```

---

### Task 5: Integration Testing

- [ ] **Step 1: Build and start processor**

```bash
cd processor && go build ./cmd/processor
```

Start processor. Verify log shows:
```
Render pool started: 8 workers, queue size 100
```

- [ ] **Step 2: Verify status log includes RenderQ**

Wait for periodic status log:
```
[Status] Webhooks: X/min | Matched: Y/min | RenderQ: 0/100 | Tiles: ...
```

- [ ] **Step 3: Verify alerts still render correctly**

Trigger pokemon, raid, invasion, quest alerts via live webhooks or poracle-test. Verify messages arrive in Discord/Telegram with correct data, static maps, emoji.

- [ ] **Step 4: Check Prometheus metrics**

```bash
curl -s http://localhost:4200/metrics | grep poracle_render
```

Verify `poracle_render_queue_depth`, `poracle_render_queue_capacity`, `poracle_render_total`, `poracle_render_duration_seconds` are present.

- [ ] **Step 5: Commit any fixes**
