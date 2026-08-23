# Per-Destination Delivery Lanes — Design

Status: draft / for review
Date: 2026-08-09
Supersedes: PR #182 (interim per-channel serialization + 429 backoff — the redesign carries its fix)

## Summary

Replace the delivery `FairQueue`'s **single shared job channel + shared worker
pool + per-destination `sync.Mutex`** with **one lightweight queue ("lane") and
one drainer goroutine per destination**. Lanes are spawned on demand and reaped
when idle. A global per-platform semaphore still caps total concurrent API
calls. This isolates routes: a rate-limited or hot destination drains at its own
pace on its own lane without tying up shared workers or the shared buffer, so
every other route keeps flowing.

## Background: why the shared model degrades globally

Today (`internal/delivery/queue.go`):
- **One** channel `ch` (buffer `delivery_queue_size`, default **200**) carries
  every job for every destination and platform.
- Workers = sum of per-platform concurrency
  (`concurrent_discord_destinations`=10 + `concurrent_discord_webhooks`=10 +
  `concurrent_telegram_destinations`=10 = **30** default) all pull from `ch`.
- `processJob` acquires the **per-destination lock first**, then
  `WaitForRateLimit`, then the platform semaphore, then Send/Edit/Delete.

Failure mode: a worker that pulls a job for a rate-limited destination grabs that
destination's lock and **blocks in `WaitForRateLimit`/429-backoff while holding a
worker**. Other workers that pull the same destination's jobs block on that lock
too. A hot destination with a run of jobs at the channel front can park **all 30
workers** on its lock, and its backlog fills the shared 200 buffer. Result: the
pain is felt **globally** — free routes starve of workers, `Dispatch` (all sends)
blocks when the buffer fills, and clean-deletes drop. This is the
head-of-line/worker-starvation problem the interim PR #182 does not solve (it
serializes per-channel but still runs on the shared channel + workers).

## Decisions (agreed)

| # | Decision |
|---|----------|
| D1 | **Supersede #182.** One redesign PR carries the clean-delete fix; #182 closes. #182's building blocks are reused: `doWithRetry`/`doPostWithRetry` (429 Retry-After backoff), `Job.DeleteSentID`, the `MessageTracker` clean-delete hook, and the removal of `Delete`'s self-`Wait`. |
| D2 | **Per-destination lanes, spawn-on-demand + idle-reap.** A lane = a bounded buffered channel + one drainer goroutine, keyed by `job.Target`. Created on first job for a target; the drainer exits after an idle timeout with an empty lane and is re-created on the next job. Bounds goroutines/memory to *active* destinations. |
| D3 | **Global per-platform concurrency cap retained, but held only during the wire call.** The platform semaphore (`discordSem`/`webhookSem`/`telegramSem`, existing sizes) wraps **only the in-flight HTTP request** — acquired/released *per attempt inside* `doWithRetry`/`doPostWithRetry`, NOT across the proactive `WaitForRateLimit` nor the 429 `Retry-After` backoff. A waiting or backing-off drainer therefore holds **no** slot, so a few rate-limited routes can't exhaust the semaphore and stall healthy routes. (This fixes a pre-existing bug: today the semaphore is held across the whole `Send`, backoff included.) |
| D4 | **`delivery_queue_size` becomes the PER-ROUTE buffer** (default stays 200 — now "200 queued for a single route", not shared). |
| D5 | **Overflow is per job kind, and bounded.** Sends (`Dispatch`) **block** when a route's lane is full — per-route backpressure to the render pool, only that route's dispatch blocks, not all. Bounded-and-block is deliberate: PoracleJS used an unbounded queue and ultimately failed under load; a bounded lane applies backpressure instead of growing without limit. A full-lane block is **logged** (rate-limited, naming the target) and counted (metric). Clean-deletes enqueue **non-blocking** (drop-on-full → logged + counted). A drop during normal operation is **unrecoverable at runtime**: the message reached the hook because it was just evicted from the tracker's ttlcache, so `Save()` (which persists only still-live entries) never writes it, and the next `Load()` has nothing left to re-clean — the message lingers until manually cleared. Only messages still tracked at shutdown are persisted and re-cleaned on next load. This is accepted rather than guarded against: per-route lane buffering makes drops far less likely than the old shared-buffer design, and blocking here would stall the tracker's single eviction goroutine. |

## Design

### Lane

```
type lane struct {
    ch      chan *Job     // per-route buffer (delivery_queue_size)
    target  string
    // pending is queued + in-flight + about-to-be-enqueued jobs. Guarded by
    // FairQueue.lanesMu (incremented under it in enqueue; the reaper reads it
    // under it). Prevents reaping a lane that has work or an in-flight enqueue.
    pending int
}
```

### FairQueue changes

- Remove `ch` (shared), `destLocks`, the fixed `worker()` pool, and the
  `discordSem`/`webhookSem`/`telegramSem` fields (the semaphores move to the
  senders per D3).
- Add `lanesMu sync.Mutex` + `lanes map[string]*lane`, a `stopped bool`, and
  `perRouteBuf int` (from `delivery_queue_size`, the per-lane channel size).
- Keep `rateLimiter` wiring, `failCounts`, `tracker`, `dispatcher` — the whole
  `processJob` pipeline is reused, just moved into the drainer and keyed off a
  lane instead of the shared channel.
- `const laneIdleTimeout = 60 * time.Second` (not configurable — a const).

### Enqueue (race-safe lane get-or-create)

```
func (fq *FairQueue) enqueue(job *Job, block bool) (accepted bool) {
    fq.lanesMu.Lock()
    if fq.stopped {                 // shutting down
        fq.lanesMu.Unlock()
        return false
    }
    l, ok := fq.lanes[job.Target]
    if !ok {
        l = &lane{ch: make(chan *Job, fq.perRouteBuf), target: job.Target}
        fq.lanes[job.Target] = l
        go fq.runLane(l)
    }
    l.pending++                     // reserve BEFORE releasing the lock
    fq.lanesMu.Unlock()

    if block {
        l.ch <- job                 // send: per-route backpressure
        return true
    }
    select {                        // clean-delete: non-blocking
    case l.ch <- job:
        return true
    default:
        fq.lanesMu.Lock(); l.pending--; fq.lanesMu.Unlock()
        return false                // dropped (re-cleaned on next load)
    }
}
```

`pending` is incremented **under `lanesMu`** before the lock is released, so the
reaper (which reads `pending` under `lanesMu`) can never reap a lane between an
enqueue's lock-release and its channel send. If the reaper deleted the lane just
before this enqueue took the lock, `fq.lanes[target]` is absent and a **new**
lane+drainer is created — no job is ever sent to a dead lane. This is the
standard "counter-under-lock" reap-safety pattern.

### Drainer

```
func (fq *FairQueue) runLane(l *lane) {
    defer fq.wg.Done()             // registered on spawn
    idle := time.NewTimer(laneIdleTimeout)
    for {
        select {
        case job, ok := <-l.ch:
            if !ok { return }       // channel closed on shutdown
            if !idle.Stop() { <-idle.C }
            fq.processJob(job)      // full existing pipeline (below)
            fq.lanesMu.Lock(); l.pending--; fq.lanesMu.Unlock()
            idle.Reset(laneIdleTimeout)
        case <-idle.C:
            fq.lanesMu.Lock()
            if l.pending == 0 {
                delete(fq.lanes, l.target)
                fq.lanesMu.Unlock()
                return              // reap: no work, no in-flight enqueue
            }
            fq.lanesMu.Unlock()
            idle.Reset(laneIdleTimeout)
        }
    }
}
```

`processJob` keeps everything it does today **minus the destLock and the
semaphore** (lane serialization is inherent — one drainer per target; the
semaphore moves into the sender, see below): `WaitForRateLimit(target)` (no slot)
→ clean-delete branch (`DeleteSentID`) OR [edit-before-send → pause gate →
expired-squash → disabled check → alert-limit `Check` (Phase-2) → `Send`] →
tracking / snapshot write / reply-index / failure-disable.

### Concurrency: the semaphore is held only during the wire call (D3)

**The problem this fixes.** Today `processJob` acquires the semaphore
(`sem <- struct{}{}`, `queue.go:167`) and holds it across the whole
`Send`/`Delete`. But `Send` → `doWithRetry` **sleeps through the 429
`Retry-After` backoff while still holding the slot**. So in exactly the
rate-limited scenario, a handful of backing-off destinations pin every slot and
stall all healthy routes — and with per-lane drainers that's N sleeping drainers
each holding a slot. The proactive `WaitForRateLimit` is already correctly
*outside* the semaphore (`queue.go:158-163`); only the reactive 429 backoff is
inside it.

**The rule.** A platform semaphore slot is occupied **only while a request is
genuinely on the wire** — never during a wait or a backoff sleep. Two waits, both
kept slot-free:

1. **Proactive `WaitForRateLimit(target)`** — stays in the drainer, before the
   alert-limit `Check` (preserving today's ordering), and acquires no slot. A
   lane waiting on its route's rate limit holds nothing.
2. **Reactive 429/5xx backoff** — moves *inside* the semaphore's grip today; the
   fix is to make the semaphore grip finer than the retry loop.

**The change.** Move the semaphore (and the `DeliveryInFlight{platform}` gauge)
off the `FairQueue` and into the senders, acquired **per HTTP attempt** inside
`doWithRetry` / `doPostWithRetry`:

```
for attempt := 0; attempt <= maxRetries; attempt++ {
    acquire(sem)                       // slot held...
    resp, status := ds.doRequest(...)  // ...only across the wire call
    release(sem)                        // ...released before we interpret
    if status == 429 { sleep(retryAfter); continue }  // backoff holds NO slot
    if status >= 500 { sleep(backoff);   continue }
    return resp, status
}
```

Net: N lanes stuck in rate-limit wait or 429 backoff consume **zero** slots
between attempts, and `DeliveryInFlight` now reads true wire concurrency. The
semaphores (`discordSem` for channel/DM/thread, `webhookSem` for webhooks,
`telegramSem`) are constructed on the senders from the same config sizes; the
DiscordSender selects channel-vs-webhook the same way it already resolves routing
(via `resolveMessageURL`'s target/sentID shape). The `FairQueue` no longer owns
the semaphore objects or the fixed worker pool; the drainer's `processJob` keeps
calling `sender.WaitForRateLimit(target)` before `Check` exactly as today.

### Clean-delete integration (from #182, retargeted)

Unchanged from #182 except the hook enqueues to a **lane** instead of the shared
channel: `MessageTracker.cleanDelete` → `cleanDeleteHook` → `Dispatcher.enqueueCleanDelete`
→ `fq.enqueue(&Job{DeleteSentID:…}, block=false)`. A hot channel's delete burst
fills **its own** lane (200) and drains serially at its rate limit; excess drops
are **not recovered at runtime** — the message was already evicted from the
tracker's ttlcache (that eviction is what triggered the clean-delete), so
`Save()` never persists it and the next `Load()` has nothing left to re-clean;
the message lingers until manually cleared. Only messages still tracked at
shutdown are persisted and re-cleaned on next load. It no longer competes with
sends to other channels for a shared buffer or shared workers.

### Dispatch / DispatchBypass

`Dispatcher.Dispatch(job)` → `fq.enqueue(job, block=true)`; `DispatchBypass`
likewise (bypass jobs skip the alert-limit `Check` inside `processJob`, as
today). Both route by `job.Target`.

### Shutdown

`Stop()` must drain all live lanes. Sequence:
1. Set `fq.stopped` under `lanesMu` (new enqueues rejected → return `false`;
   `Dispatch` callers already stopped upstream per the existing shutdown order).
2. Under `lanesMu`, `close(l.ch)` for every live lane; the drainers finish their
   buffered jobs and return on the closed channel.
3. `fq.wg.Wait()` (each drainer registers on spawn), then `fq.cancel()`.

This replaces "`close(ch)`; `wg.Wait()`" and preserves "queued jobs are still
delivered before shutdown". The tracker's clean-delete hook is guarded (D5 /
existing recover) so an eviction racing shutdown drops safely.

### Config

- `delivery_queue_size` (default 200) → **per-route** lane buffer (doc + example
  comment updated: "max buffered jobs per destination"). This is a **semantic
  change**, not just a doc update: on the old shared-channel model the buffer
  was allocated once, total; now every *active* lane eagerly allocates its own
  `make(chan *Job, delivery_queue_size)`, so buffered-job memory scales as
  `O(delivery_queue_size × active_lanes)`. An operator who raised this value
  under the old shared-queue model to survive bursts gets a multiplicative
  memory blow-up under the new one and should lower it back down.
- `concurrent_discord_destinations` / `_webhooks` / `concurrent_telegram_destinations`
  → **global** per-platform concurrent-API-call caps (semaphores). Sizes
  unchanged; they now live on the senders and gate only the wire call (D3).
- **No new config knob.** The lane idle timeout is a `const laneIdleTimeout =
  60 * time.Second` — deliberately not exposed (one less thing to tune wrong).

### Instrumentation

The goal: make a *developing* bad situation visible before it becomes a global
stall, without exploding metric cardinality (no per-target Prometheus labels).
Two surfaces:

**1. Aggregate gauges/counters** (extend `internal/metrics`, all bounded
cardinality — no target label):

| Metric | Kind | Meaning |
|--------|------|---------|
| `delivery_active_lanes` | gauge | live lanes right now (spawned − reaped) |
| `delivery_lane_queued_total` | gauge | sum of `len(l.ch)` across lanes — total buffered backlog |
| `delivery_lane_max_depth` | gauge | deepest single lane's `len(l.ch)` — the head-of-line signal |
| `delivery_lanes_near_capacity` | gauge | count of lanes with `len(l.ch) ≥ 80% × perRouteBuf` |
| `delivery_lane_backpressure_total` | counter | send-enqueue blocked on a full lane (D5) |
| `delivery_clean_delete_dropped_total` | counter | clean-delete dropped on a full lane (D5) |
| `delivery_lane_spawned_total` / `delivery_lane_reaped_total` | counters | lane lifecycle churn |
| `delivery_inflight{platform}` | gauge (existing) | semaphore occupancy — now = true wire concurrency (D3) |

The four lane gauges are computed by a cheap walk of the `lanes` map under
`lanesMu` when the periodic `[Status]` reporter samples (once per interval — not
per job), so no hot-path cost. A `FairQueue.LaneStats()` method returns
`(active, totalQueued, maxDepth, deepestTarget, nearCap)` in one lock pass.

**2. The periodic `[Status]` health log** (`cmd/processor/main.go`, the existing
`Delivery: Discord:%d+%d Telegram:%d Tracked:%d RateLimited:%d` line) gains a
lane summary and — critically — **names the worst offender** so an operator can
act:

```
Delivery: Discord:3+1 Telegram:0 Tracked:812 RateLimited:2 | Lanes:14 active, 190 queued, deepest=173 (channel:1377245284236660836), 2 near-cap
```

`deepestTarget` is logged as a *field value*, not a metric label, so cardinality
stays bounded. When `maxDepth ≥ 80% × perRouteBuf` OR `backpressure_total`
advanced since the last sample, the reporter escalates that line to `WARN` (the
"we're getting into a bad situation" signal). D5's per-event full-lane logs
(rate-limited, naming the target) remain the fine-grained trail.

## What carries over vs. changes

- **Reused as-is**: `Job.DeleteSentID`, `MessageTracker.cleanDelete` +
  `cleanDeleteHook`, `Delete` self-`Wait` removal, the whole per-job pipeline
  (rate-limit `Check`, tracking, snapshot, reply threading, edit-before-send,
  failure-disable), the `DiscordRateLimiter`, the platform semaphore *sizes*
  (from config).
- **Moved (per D3)**: the platform semaphores + the `DeliveryInFlight{platform}`
  gauge from `FairQueue` onto the senders, with `acquire`/`release` inside
  `doWithRetry` / `doPostWithRetry` so a slot wraps only the wire call, not the
  backoff. `WaitForRateLimit` stays a drainer call (before `Check`), unchanged.
- **Removed**: shared `ch`, `destLocks`, fixed `worker()` pool + `Start()`'s
  "spawn N workers", the `FairQueue`'s semaphore fields.
- **Added**: `lane`, `lanes` map + `lanesMu`, `runLane` drainer, spawn/reap
  lifecycle, `enqueue`, `LaneStats()`, the lane metrics + `[Status]` lane summary.

## Testing

- **Isolation** (the headline): a rate-limited/slow route (its sender's
  `WaitForRateLimit`/Send blocks) must NOT delay a free route — assert a job to
  a free target completes while a target-A job is parked. (Today this fails: the
  shared workers/buffer starve the free route.)
- **Per-route serialization**: N jobs to one target run one-at-a-time (max
  concurrency 1 for that target) while different targets run concurrently up to
  the platform semaphore.
- **Reap safety** (race): interleave rapid enqueue + idle-reap for one target
  under `-race`; no job lost, no send-on-closed panic, no double drainer. Assert
  a reaped-then-re-enqueued target still delivers.
- **Global cap**: with `concurrent_discord_destinations=2` and many active
  lanes, at most 2 concurrent Discord API calls.
- **Semaphore released during waits (D3)**: a sender parked in `WaitForRateLimit`
  or 429 `Retry-After` backoff must hold **zero** semaphore slots. Assert that
  with `concurrent_discord_destinations=1` and lane A's sender in backoff, a job
  to lane B still acquires the slot and completes (i.e. A's backoff didn't pin
  the only slot). This is the direct regression test for the pre-existing bug.
- **Overflow**: a full lane blocks a send (backpressure, `backpressure_total`
  increments) but drops a clean-delete (non-blocking, `clean_delete_dropped_total`
  increments); other lanes unaffected.
- **Instrumentation**: `LaneStats()` returns correct `(active, totalQueued,
  maxDepth, deepestTarget, nearCap)` for a hand-built set of lanes; the WARN
  escalation fires when `maxDepth ≥ 80% × perRouteBuf`.
- **Shutdown**: buffered jobs across many lanes all deliver before `Stop()`
  returns; `-race` clean.
- **Clean-delete end-to-end**: eviction burst on one channel serializes on its
  lane and succeeds under a 429-then-OK sender (carried from #182).
- Existing delivery tests (send/edit/tracker/rate-limit/snapshot) stay green.

## Risks / edge cases

- **Reap↔enqueue race** — the core risk; addressed by the counter-under-lock
  pattern (D2) and covered by a `-race` test.
- **Lane-map lock contention** — `lanesMu` is taken on enqueue, per-job
  `pending--`, and reap. Critical sections are tiny (map op + int). If profiling
  ever shows contention, shard the map by target hash. Not expected at current
  scale.
- **Goroutine count** — bounded by *active* destinations (idle-reaped). A pathological
  fan-out (thousands simultaneously active) = thousands of mostly-idle drainers;
  acceptable (cheap goroutines) and self-trimming.
- **Ordering across a reap** — a target reaped then re-created starts a fresh
  lane; since reap only happens with an empty lane + no pending, no reordering of
  live jobs occurs.
- **Route isolation is not absolute (D5 caveat)** — isolation is enforced at the
  delivery layer, not the dispatch layer. `Dispatch` is called by render
  workers, and a blocking send on a full lane (`enqueue(job, block=true)`) parks
  the calling render worker until that lane drains. A permanently-stuck (e.g.
  persistently rate-limited) lane can therefore back-pressure into the *shared*
  render pool via the render→deliver handoff — other routes don't stall
  directly, but the render pool that feeds all routes can. This is still
  strictly better than the old shared-channel design (which blocked *every*
  `Dispatch` globally once the single 200-job buffer filled), and in practice
  is bounded by the Phase-1 alert pre-filter (`internal/ratelimit`), which caps
  a destination's inflow well under the lane size before render/enqueue even
  happens. That pre-filter does not cover everything, though: clean-deletes use
  `block=false` (they drop, not block, so they don't contribute to this), but
  bypass and summary traffic are dispatched without going through the
  pre-filter — accepted because that traffic is low-volume. A deadline/shed on
  the blocking send was considered and explicitly deferred.

## Out of scope / deferred

- Priority between sends and clean-deletes within a lane (FIFO for now).
- Cross-platform fairness knobs beyond the existing per-platform caps.
- **Per-target** Prometheus time series (deliberately avoided — the deepest
  target is a log field, not a metric label, to keep cardinality bounded).
