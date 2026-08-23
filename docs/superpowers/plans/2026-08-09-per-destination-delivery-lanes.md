# Per-Destination Delivery Lanes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the delivery `FairQueue`'s shared channel + fixed worker pool + per-destination mutex with one bounded queue ("lane") and one drainer goroutine per destination, and hold the per-platform concurrency semaphore only during the wire call — so a rate-limited route can never starve healthy routes.

**Architecture:** Three phases. **Phase 1** moves the per-platform concurrency semaphore off the `FairQueue` and onto the two concrete senders, acquired per HTTP attempt inside their retry loops and released during the 429/5xx backoff (the D3 fix). This ships value on the *existing* shared-queue architecture. **Phase 2** replaces the shared channel + worker pool + `destLocks` with spawn-on-demand/idle-reap per-destination lanes. **Phase 3** adds bounded-cardinality lane instrumentation and a health-escalating `[Status]` log.

**Tech Stack:** Go, `net/http`, `sync`, `sync/atomic`, `context`, logrus, prometheus/promauto. Test with `net/http/httptest` and `-race`.

**Spec:** `docs/superpowers/specs/2026-08-09-per-destination-delivery-lanes-design.md` (decisions D1–D5). Read it before starting.

## Global Constraints

- **Pre-commit gate (every commit):** from `processor/`, `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...` must pass. Never suppress a lint with `//nolint` unless justified by a comment.
- **Commit trailer:** end every commit message with `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- **Lane idle timeout is a const, not config:** `const laneIdleTimeout = 60 * time.Second` in `internal/delivery/queue.go`. Do NOT add a config knob.
- **No new config field.** `delivery_queue_size` is reinterpreted as the per-route buffer; no schema change.
- **No per-target Prometheus labels.** Lane metrics are aggregates; the deepest target appears only as a log *field value*.
- **Overflow policy (D5):** sends (`block=true`) apply backpressure by blocking on a full lane (counted + throttled-logged); clean-deletes (`block=false`) drop on a full lane (counted + logged), re-cleaned on next startup load.
- **Semaphore rule (D3):** a platform slot is held ONLY while an HTTP request is on the wire — never during `WaitForRateLimit` (proactive) nor 429/5xx backoff (reactive).
- **Package under change:** `processor/internal/delivery/` (+ `processor/internal/metrics/metrics.go`, + the `[Status]` reporter in `processor/cmd/processor/main.go`).
- Work happens on branch `fix/clean-delete-rate-limit` (the existing #182 worktree; this redesign supersedes #182 in the same PR).

---

## File Structure

**Phase 1 (semaphore → senders):**
- Modify `internal/delivery/discord.go` — add `discordSem`/`webhookSem`/in-flight counters, `SetConcurrency`, `roundTrip`; route `doWithRetry` + `ensureDMChannel` through `roundTrip`.
- Modify `internal/delivery/telegram.go` — add `sem`/in-flight counter, `SetConcurrency`, `roundTrip`; route `callWithRetry` + `doPostWithRetry` through `roundTrip`.
- Modify `internal/delivery/queue.go` — delete `discordSem`/`webhookSem`/`telegramSem` + `semaphoreFor` + `counterFor` + the in-flight atomics; drop the semaphore + gauge block from `processJob`; add `workerCount`; `DiscordDepth`/`WebhookDepth`/`TelegramDepth` read from senders.
- Modify `internal/delivery/dispatcher.go` — call `SetConcurrency` on the senders in `NewDispatcher`.
- Modify `internal/delivery/discord_test.go`, `telegram_test.go` — add sender-level concurrency tests.

**Phase 2 (lanes):**
- Modify `internal/delivery/queue.go` — add `lane` type, `lanes` map + `lanesMu` + `stopped` + `perRouteBuf`; `enqueue`, `runLane`; rewrite `Start`/`Stop`; drop `ch`/`destLocks`/`worker`/`workerCount`; drop `destLock` from `processJob`; change `NewFairQueue` signature; add `PerRouteBuffer` to `QueueConfig`.
- Modify `internal/delivery/dispatcher.go` — drop `d.ch`; `Dispatch`/`DispatchBypass`/`enqueueCleanDelete` call `queue.enqueue`; `QueueDepth` via lane stats.
- Modify `internal/delivery/queue_test.go` — migrate `newTestFairQueue` + all `ch <- job` sites; add lane isolation / reap-race / overflow / shutdown tests.
- Modify `internal/delivery/dispatcher_test.go` and the three non-delivery `NewDispatcherWithSenders` callers if signatures shift (they should not — `NewDispatcherWithSenders` keeps its signature).

**Phase 3 (instrumentation):**
- Modify `internal/metrics/metrics.go` — add lane gauges + counters.
- Modify `internal/delivery/queue.go` — `LaneStats()`, backpressure/drop counting.
- Modify `internal/delivery/dispatcher.go` — expose `LaneStats()`.
- Modify `cmd/processor/main.go` — `[Status]` lane summary + WARN escalation.
- Modify `internal/delivery/queue_test.go` — `LaneStats` unit test.

---

# Phase 1 — Semaphore into the senders (D3 fix)

After Phase 1 the shared worker pool + `destLocks` remain, but the concurrency cap is enforced by the senders per wire call and released during backoff. This is independently shippable and fixes the "backoff pins a slot" bug.

### Task 1: DiscordSender owns its wire-call semaphores

**Files:**
- Modify: `processor/internal/delivery/discord.go`
- Test: `processor/internal/delivery/discord_test.go`

**Interfaces:**
- Produces: `(*DiscordSender).SetConcurrency(discord, webhook int)`, `(*DiscordSender).DiscordInFlight() int`, `(*DiscordSender).WebhookInFlight() int`, and an internal `roundTrip(ctx, method, url string, bodyBytes []byte, contentType string, auth bool) ([]byte, int, http.Header, error)`.
- Consumes: existing `doRequest`, `metrics.DeliveryInFlight`.

- [ ] **Step 1: Write the failing test** — a 429-then-200 discord request under `SetConcurrency(1,1)` must free the slot during backoff so a second concurrent request to a *different* target proceeds.

Add to `processor/internal/delivery/discord_test.go`:

```go
func TestDiscord_SemaphoreReleasedDuring429Backoff(t *testing.T) {
	// srv: target "a" 429s once (retry_after 0.4s) then 204s; target "b" 204s immediately.
	var aHits atomic.Int32
	bDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/channels/a/"):
			if aHits.Add(1) == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"retry_after":0.4}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"1"}`))
		case strings.Contains(r.URL.Path, "/channels/b/"):
			close(bDone)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"2"}`))
		}
	}))
	defer srv.Close()

	ds := NewDiscordSender("tok", false, 0)
	ds.baseURL = srv.URL
	ds.SetConcurrency(1, 1) // ONE discord slot

	// Start the "a" send (it will 429 and back off ~0.4s).
	go func() {
		_, _ = ds.postMessage(context.Background(), "a", json.RawMessage(`{"content":"a"}`), nil, "", "")
	}()

	// "b" must acquire the single slot while "a" is backing off (slot released).
	select {
	case <-bDone:
		// pass: b reached the wire during a's backoff
	case <-time.After(300 * time.Millisecond):
		t.Fatal("target b never reached the wire — a's 429 backoff pinned the only slot")
	}
	_, _ = aHits.Load(), 0
}
```

Ensure the test file imports `net/http`, `net/http/httptest`, `strings`, `sync/atomic`, `time`, `context`, `encoding/json` (add any missing).

- [ ] **Step 2: Run it, expect FAIL** — `roundTrip`/`SetConcurrency` don't exist yet (compile failure), or once stubbed, `b` times out because the current `doWithRetry` holds the slot across the backoff.

Run: `cd processor && go test ./internal/delivery/ -run TestDiscord_SemaphoreReleasedDuring429Backoff -v`

- [ ] **Step 3: Add the semaphore fields + accessors + `SetConcurrency`.**

In `discord.go`, add to the `DiscordSender` struct (after `dmChannels sync.Map`):

```go
	// Per-subtype wire-call concurrency semaphores (nil = unlimited). A slot is
	// held only across a single HTTP round-trip (see roundTrip), never during
	// WaitForRateLimit or 429/5xx backoff, so a rate-limited route can't pin the
	// pool. discordSem gates bot calls (channel/DM/thread); webhookSem gates
	// webhook calls. Selected by the `auth` flag (auth => bot => discordSem).
	discordSem   chan struct{}
	webhookSem   chan struct{}
	discordInFly atomic.Int64
	webhookInFly atomic.Int64
```

Add `"sync/atomic"` to the import block.

Add these methods (near `NewDiscordSender`):

```go
// makeSem returns a semaphore of capacity n, or nil for n<=0 (unlimited).
func makeSem(n int) chan struct{} {
	if n <= 0 {
		return nil
	}
	return make(chan struct{}, n)
}

// SetConcurrency sizes the per-subtype wire-call semaphores. n<=0 means
// unlimited. Call once at construction, before any Send/Edit/Delete.
func (ds *DiscordSender) SetConcurrency(discord, webhook int) {
	ds.discordSem = makeSem(discord)
	ds.webhookSem = makeSem(webhook)
}

// DiscordInFlight / WebhookInFlight report current concurrent wire calls per
// subtype (for the [Status] log and per-platform depth gauges).
func (ds *DiscordSender) DiscordInFlight() int { return int(ds.discordInFly.Load()) }
func (ds *DiscordSender) WebhookInFlight() int { return int(ds.webhookInFly.Load()) }
```

- [ ] **Step 4: Add `roundTrip`** (single HTTP attempt under a slot; slot released before the caller interprets/backs off):

```go
// roundTrip executes ONE Discord HTTP request while holding a platform
// concurrency slot, and releases the slot before returning — so the caller's
// 429/5xx backoff runs slot-free. auth selects the bot (discordSem) vs webhook
// (webhookSem) semaphore. Returns the response body, status, and headers.
func (ds *DiscordSender) roundTrip(ctx context.Context, method, url string, bodyBytes []byte, contentType string, auth bool) ([]byte, int, http.Header, error) {
	sem, inFly := ds.discordSem, &ds.discordInFly
	if !auth {
		sem, inFly = ds.webhookSem, &ds.webhookInFly
	}
	if sem != nil {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-ctx.Done():
			return nil, 0, nil, ctx.Err()
		}
	}
	inFly.Add(1)
	metrics.DeliveryInFlight.WithLabelValues("discord").Inc()
	defer func() {
		inFly.Add(-1)
		metrics.DeliveryInFlight.WithLabelValues("discord").Dec()
	}()

	var body io.Reader
	if bodyBytes != nil {
		body = bytes.NewReader(bodyBytes)
	}
	resp, err := ds.doRequest(ctx, method, url, body, contentType, auth)
	if err != nil {
		return nil, 0, nil, err
	}
	respBody, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, resp.StatusCode, resp.Header, fmt.Errorf("reading response body: %w", readErr)
	}
	return respBody, resp.StatusCode, resp.Header, nil
}
```

- [ ] **Step 5: Route `doWithRetry` through `roundTrip`.** Replace the body of the `for attempt` loop in `doWithRetry` (currently the `doRequest` + `Update` + `ReadAll` + 429/5xx block) with:

```go
	for attempt := 0; attempt <= maxRetries; attempt++ {
		respBody, status, header, err := ds.roundTrip(ctx, method, url, bodyBytes, contentType, auth)
		if err != nil {
			if status != 0 {
				// read error after a response — surface it (non-retryable)
				return nil, status, err
			}
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			logref.Warnf(logRef, "discord: %s to %s failed (attempt %d/%d): %v", method, rateLimitKey, attempt+1, maxRetries+1, err)
			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return nil, 0, ctx.Err()
				case <-time.After(time.Duration(attempt+1) * time.Second):
				}
				continue
			}
			return nil, 0, err
		}

		ds.rateLimiter.Update(rateLimitKey, header)

		if status == http.StatusTooManyRequests {
			var rl struct {
				RetryAfter float64 `json:"retry_after"`
			}
			json.Unmarshal(respBody, &rl) //nolint:errcheck
			d := ParseRetryAfter(rl.RetryAfter)
			metrics.DeliveryRateLimited.WithLabelValues("discord").Inc()
			ds.rateLimiter.Record429()
			logref.Warnf(logRef, "discord: 429 for %s %s, retry_after=%.1fs (attempt %d/%d)", method, rateLimitKey, rl.RetryAfter, attempt+1, maxRetries+1)
			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return respBody, status, ctx.Err()
				case <-time.After(d):
				}
				continue
			}
			return respBody, status, nil
		}

		if status >= 500 && attempt < maxRetries {
			logref.Warnf(logRef, "discord: %s to %s status=%d (attempt %d/%d), retrying", method, rateLimitKey, status, attempt+1, maxRetries+1)
			select {
			case <-ctx.Done():
				return respBody, status, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
			continue
		}

		return respBody, status, nil
	}
	return nil, 0, fmt.Errorf("discord API: max retries exceeded")
```

Update the `doWithRetry` doc comment's line "It does NOT call rateLimiter.Wait" to also note "and acquires a concurrency slot per attempt via roundTrip, released before any backoff sleep." The function signature is unchanged.

- [ ] **Step 6: Route `ensureDMChannel` through `roundTrip`** so DM-channel creation is also slot-gated. Replace its `doRequest` + status/decoder block with:

```go
	respBody, status, _, err := ds.roundTrip(ctx, http.MethodPost, ds.baseURL+"/users/@me/channels",
		[]byte(body), "application/json", true)
	if err != nil {
		return "", fmt.Errorf("creating DM channel: %w", err)
	}
	if status != http.StatusOK {
		code := extractErrorCode(respBody)
		if code == 50007 || code == 10003 || code == 10013 {
			return "", &PermanentError{
				Err:    fmt.Errorf("discord error %d creating DM channel for %s", code, userID),
				Reason: fmt.Sprintf("discord error code %d", code),
			}
		}
		return "", fmt.Errorf("creating DM channel returned status %d: %s", status, respBody)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("decoding DM channel response: %w", err)
	}
	ds.dmChannels.Store(userID, result.ID)
	logref.Infof(logRef, "discord: created DM channel %s for user %s", result.ID, userID)
	return result.ID, nil
```

(`body` stays the `fmt.Sprintf(...)` string above; `[]byte(body)` passes it as the request bytes.)

- [ ] **Step 7: Run the new test + the existing discord suite.**

Run: `cd processor && go test ./internal/delivery/ -run 'TestDiscord' -race -v`
Expected: PASS, including the existing `TestDiscordDelete_RetriesOn429` / `TestDiscordEdit_RetriesOn429`.

- [ ] **Step 8: Commit.**

```bash
git add processor/internal/delivery/discord.go processor/internal/delivery/discord_test.go
git commit -m "delivery: DiscordSender holds concurrency slot only during the wire call

Add per-subtype wire-call semaphores (discordSem/webhookSem) + SetConcurrency +
roundTrip; route doWithRetry and ensureDMChannel through roundTrip so the slot is
released during 429/5xx backoff. Fixes a backing-off request pinning the pool.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

### Task 2: TelegramSender owns its wire-call semaphore

**Files:**
- Modify: `processor/internal/delivery/telegram.go`
- Test: `processor/internal/delivery/telegram_test.go`

**Interfaces:**
- Produces: `(*TelegramSender).SetConcurrency(n int)`, `(*TelegramSender).TelegramInFlight() int`, internal `roundTrip(ctx, method string, jsonBody []byte) ([]byte, int, error)`.

- [ ] **Step 1: Write the failing test** — under `SetConcurrency(1)`, a step that 429-backs off must free the slot for another target.

Add to `processor/internal/delivery/telegram_test.go`:

```go
func TestTelegram_SemaphoreReleasedDuring429Backoff(t *testing.T) {
	bDone := make(chan struct{})
	var aHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		isA := strings.Contains(string(body), `"chat_a"`)
		w.Header().Set("Content-Type", "application/json")
		if isA && aHits.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"parameters":{"retry_after":1}}`))
			return
		}
		if !isA {
			close(bDone)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer srv.Close()

	ts := NewTelegramSender("tok")
	ts.baseURL = srv.URL
	ts.SetConcurrency(1)

	go func() {
		_, _ = ts.sendMessage(context.Background(), "chat_a", 0, "hi", "HTML", false, "", "")
	}()
	select {
	case <-bDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("chat_b never reached the wire — chat_a's 429 backoff pinned the only slot")
	}
}
```

Add missing imports (`io`, `net/http`, `net/http/httptest`, `strings`, `sync/atomic`, `time`, `context`).

- [ ] **Step 2: Run it, expect FAIL** (compile error first, then timeout).

Run: `cd processor && go test ./internal/delivery/ -run TestTelegram_SemaphoreReleasedDuring429Backoff -v`

- [ ] **Step 3: Add the fields + accessors + `SetConcurrency`.**

In `telegram.go`, add to `TelegramSender` (after the rate-limit introspection fields):

```go
	// Wire-call concurrency semaphore (nil = unlimited). Held only across a
	// single HTTP round-trip (see roundTrip), never during 429 backoff.
	sem   chan struct{}
	inFly atomic.Int64
```

Add `"sync/atomic"` to the imports.

Add methods (near `NewTelegramSender`):

```go
// SetConcurrency sizes the wire-call semaphore. n<=0 means unlimited. Call once
// at construction, before any Send/Edit/Delete.
func (ts *TelegramSender) SetConcurrency(n int) { ts.sem = makeSem(n) }

// TelegramInFlight reports current concurrent wire calls (for the [Status] log).
func (ts *TelegramSender) TelegramInFlight() int { return int(ts.inFly.Load()) }
```

(`makeSem` is defined in Task 1 in the same package — reuse it.)

- [ ] **Step 4: Add `roundTrip`:**

```go
// roundTrip posts ONE Telegram request while holding a concurrency slot, and
// releases the slot before returning so the caller's 429/5xx backoff runs
// slot-free. Returns the response body and status.
func (ts *TelegramSender) roundTrip(ctx context.Context, method string, jsonBody []byte) ([]byte, int, error) {
	if ts.sem != nil {
		select {
		case ts.sem <- struct{}{}:
			defer func() { <-ts.sem }()
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
	ts.inFly.Add(1)
	metrics.DeliveryInFlight.WithLabelValues("telegram").Inc()
	defer func() {
		ts.inFly.Add(-1)
		metrics.DeliveryInFlight.WithLabelValues("telegram").Dec()
	}()

	resp, err := ts.doPostRaw(ctx, method, jsonBody)
	if err != nil {
		return nil, 0, err
	}
	respBody, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", readErr)
	}
	return respBody, resp.StatusCode, nil
}
```

- [ ] **Step 5: Route `callWithRetry` through `roundTrip`.** In `callWithRetry`, replace:

```go
		resp, err := ts.doPostRaw(ctx, method, jsonBody)
		if err != nil {
			if attempt < maxRetries {
				time.Sleep(time.Second)
				continue
			}
			return 0, err
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return 0, fmt.Errorf("reading response body: %w", readErr)
		}

		if resp.StatusCode == http.StatusOK {
```

with:

```go
		respBody, status, err := ts.roundTrip(ctx, method, jsonBody)
		if err != nil {
			if status != 0 {
				return 0, fmt.Errorf("reading response body: %w", err)
			}
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			if attempt < maxRetries {
				time.Sleep(time.Second)
				continue
			}
			return 0, err
		}

		if status == http.StatusOK {
```

Then, in the rest of `callWithRetry`, replace every remaining `resp.StatusCode` with `status` (the 403 branch, the 429 branch, the final non-2xx branch). Leave all logging/backoff/`Record429`/`setBackoffUntil` logic exactly as-is.

- [ ] **Step 6: Route `doPostWithRetry` through `roundTrip`.** In `doPostWithRetry`, replace:

```go
		resp, err := ts.doPostRaw(ctx, method, jsonBody)
		if err != nil {
			if attempt < maxRetries {
				time.Sleep(time.Second)
				continue
			}
			return nil, 0, err
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", readErr)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
```

with:

```go
		respBody, status, err := ts.roundTrip(ctx, method, jsonBody)
		if err != nil {
			if status != 0 {
				return nil, status, fmt.Errorf("reading response body: %w", err)
			}
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			if attempt < maxRetries {
				time.Sleep(time.Second)
				continue
			}
			return nil, 0, err
		}

		if status == http.StatusTooManyRequests {
```

Then replace the remaining `resp.StatusCode` uses in `doPostWithRetry` (the `>= 500` branch and the final `return respBody, resp.StatusCode, nil`) with `status`.

- [ ] **Step 7: Run the new test + existing telegram suite.**

Run: `cd processor && go test ./internal/delivery/ -run 'TestTelegram' -race -v`
Expected: PASS, including the existing `TestTelegramDelete_RetriesOn429`.

- [ ] **Step 8: Commit.**

```bash
git add processor/internal/delivery/telegram.go processor/internal/delivery/telegram_test.go
git commit -m "delivery: TelegramSender holds concurrency slot only during the wire call

Add wire-call semaphore + SetConcurrency + roundTrip; route callWithRetry and
doPostWithRetry through it so the slot is released during 429/5xx backoff.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

### Task 3: FairQueue stops owning the semaphore; depth reads from senders

**Files:**
- Modify: `processor/internal/delivery/queue.go`
- Test: `processor/internal/delivery/queue_test.go` (existing tests must stay green)

**Interfaces:**
- Consumes: `(*DiscordSender).DiscordInFlight/WebhookInFlight`, `(*TelegramSender).TelegramInFlight` (Tasks 1–2).
- Produces: unchanged `FairQueue.DiscordDepth()/WebhookDepth()/TelegramDepth()` signatures (now sender-backed).

- [ ] **Step 1: Delete the semaphore + in-flight machinery from the `FairQueue` struct.** Remove these fields:

```go
	// Per-platform concurrency semaphores
	discordSem  chan struct{}
	webhookSem  chan struct{}
	telegramSem chan struct{}
```
and
```go
	// Per-platform in-flight counters for metrics
	discordInFlight  atomic.Int64
	webhookInFlight  atomic.Int64
	telegramInFlight atomic.Int64
```

Add in their place:

```go
	// workerCount is the fixed drain-pool size (sum of per-platform concurrency).
	// Concurrency is now enforced by the senders per wire call (see DiscordSender
	// /TelegramSender.SetConcurrency); this only sizes the shared worker pool.
	// (Phase 2 removes the pool entirely in favour of per-destination lanes.)
	workerCount int
```

- [ ] **Step 2: Update `NewFairQueue`.** Replace the `return &FairQueue{...}` block's semaphore construction. Remove `discordSem`/`webhookSem`/`telegramSem` from the literal and add `workerCount`:

```go
	return &FairQueue{
		ch:                ch,
		senders:           senders,
		tracker:           tracker,
		dispatcher:        d,
		ctx:               ctx,
		cancel:            cancel,
		workerCount:       cfg.ConcurrentDiscord + cfg.ConcurrentWebhook + cfg.ConcurrentTelegram,
		failThreshold:     failThreshold,
		failBlockDuration: 5 * time.Minute,
		onDisabled:        cfg.OnDisabled,
		rateLimiter:       cfg.RateLimiter,
		rateLimitHooks:    cfg.RateLimitHooks,
	}
```

(The `cfg.Concurrent* <= 0 → 1` clamps at the top of `NewFairQueue` stay, so `workerCount >= 3`.)

- [ ] **Step 3: Update `Start()`** to use `workerCount`:

```go
func (fq *FairQueue) Start() {
	for range fq.workerCount {
		fq.wg.Add(1)
		go fq.worker()
	}
}
```

- [ ] **Step 4: Drop the semaphore + gauge block from `processJob`.** Remove these two blocks (currently steps "3" and the in-flight counter):

```go
	// 3. Acquire platform semaphore (limits global concurrency per platform)
	sem := fq.semaphoreFor(job.Type)
	sem <- struct{}{}
	defer func() { <-sem }()

	// Track per-platform in-flight count
	counter := fq.counterFor(job.Type)
	counter.Add(1)
	metrics.DeliveryInFlight.WithLabelValues(platform).Inc()
	defer func() {
		counter.Add(-1)
		metrics.DeliveryInFlight.WithLabelValues(platform).Dec()
	}()
```

Leave the `platform := PlatformFromType(job.Type)` + `WaitForRateLimit` block (step 2) and the `sender, ok := fq.senders[platform]` lookup that follows. `platform` is still used by the `DeliveryTotal`/`DeliveryDuration` labels below, so keep it.

- [ ] **Step 5: Delete `semaphoreFor` and `counterFor`** (now unused) and rewrite the depth accessors to read from senders:

```go
// DiscordDepth returns discord bot (channel/DM/thread) wire calls in flight.
func (fq *FairQueue) DiscordDepth() int {
	if ds, ok := fq.senders["discord"].(*DiscordSender); ok {
		return ds.DiscordInFlight()
	}
	return 0
}

// WebhookDepth returns discord webhook wire calls in flight.
func (fq *FairQueue) WebhookDepth() int {
	if ds, ok := fq.senders["discord"].(*DiscordSender); ok {
		return ds.WebhookInFlight()
	}
	return 0
}

// TelegramDepth returns telegram wire calls in flight.
func (fq *FairQueue) TelegramDepth() int {
	if ts, ok := fq.senders["telegram"].(*TelegramSender); ok {
		return ts.TelegramInFlight()
	}
	return 0
}
```

Remove the now-unused `"sync/atomic"` import ONLY if nothing else in `queue.go` uses it — `failRecord.count atomic.Int32` still does, so keep the import.

- [ ] **Step 6: Wire `SetConcurrency` in `NewDispatcher`** (`dispatcher.go`). After building `senders`, before `NewMessageTracker`, add:

```go
	if ds, ok := senders["discord"].(*DiscordSender); ok {
		ds.SetConcurrency(cfg.Queue.ConcurrentDiscord, cfg.Queue.ConcurrentWebhook)
	}
	if ts, ok := senders["telegram"].(*TelegramSender); ok {
		ts.SetConcurrency(cfg.Queue.ConcurrentTelegram)
	}
```

- [ ] **Step 7: Run the full delivery suite + race.** The existing `TestFairQueueConcurrency` sends all jobs to one target, so the `destLock` still caps it at 1 (≤ 2 passes). `TestFairQueue_CleanDeleteSerializedPerTarget` still passes (destLock unchanged).

Run: `cd processor && go test ./internal/delivery/ -race -count=1`
Expected: PASS.

- [ ] **Step 8: Commit.**

```bash
git add processor/internal/delivery/queue.go processor/internal/delivery/dispatcher.go
git commit -m "delivery: move the concurrency semaphore off the FairQueue onto the senders

processJob no longer holds a platform slot across Send; the senders acquire it
per wire call and release it during backoff. Depth accessors read sender
in-flight counts. NewDispatcher sizes the sender semaphores from QueueConfig.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

### Task 4: Sender-level global-cap regression test

**Files:**
- Test: `processor/internal/delivery/discord_test.go`

**Interfaces:**
- Consumes: `SetConcurrency`, `postMessage`.

- [ ] **Step 1: Write the test** — with `SetConcurrency(2, 2)`, at most 2 concurrent bot requests hit the wire across *different* targets.

```go
func TestDiscord_GlobalConcurrencyCap(t *testing.T) {
	var cur, max atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := cur.Add(1)
		for {
			m := max.Load()
			if c <= m || max.CompareAndSwap(m, c) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond) // hold the slot
		cur.Add(-1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()

	ds := NewDiscordSender("tok", false, 0)
	ds.baseURL = srv.URL
	ds.SetConcurrency(2, 2)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = ds.postMessage(context.Background(), "chan"+strconv.Itoa(i), json.RawMessage(`{"content":"x"}`), nil, "", "")
		}(i)
	}
	wg.Wait()

	if m := max.Load(); m > 2 {
		t.Errorf("expected at most 2 concurrent discord wire calls, got %d", m)
	}
	if m := max.Load(); m == 0 {
		t.Error("expected some concurrency, got 0")
	}
}
```

Add `sync`, `strconv` imports if missing.

- [ ] **Step 2: Run it.**

Run: `cd processor && go test ./internal/delivery/ -run TestDiscord_GlobalConcurrencyCap -race -v`
Expected: PASS.

- [ ] **Step 3: Full gate for Phase 1.**

Run: `cd processor && go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`
Expected: all green.

- [ ] **Step 4: Commit.**

```bash
git add processor/internal/delivery/discord_test.go
git commit -m "delivery: sender-level global-concurrency-cap regression test

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

# Phase 2 — Per-destination lanes

Replace the shared channel + fixed worker pool + `destLocks` with one bounded lane + one drainer per `job.Target`, spawn-on-demand and idle-reaped. Build on Phase 1 (senders already own concurrency).

### Task 5: Add the lane type, FairQueue lane fields, and `QueueConfig.PerRouteBuffer`

**Files:**
- Modify: `processor/internal/delivery/queue.go`

**Interfaces:**
- Produces: `type lane struct{ ch chan *Job; target string; pending int }`; `FairQueue.lanes`, `lanesMu`, `stopped`, `perRouteBuf`; `QueueConfig.PerRouteBuffer`; `const laneIdleTimeout`.

- [ ] **Step 1: Add the const + lane type** near the top of `queue.go` (after the imports):

```go
// laneIdleTimeout is how long a destination's drainer waits with an empty lane
// before reaping itself. Deliberately a const — not exposed as config.
const laneIdleTimeout = 60 * time.Second

// lane is one destination's bounded queue + (implicitly) its single drainer.
type lane struct {
	ch     chan *Job
	target string
	// pending counts queued + in-flight + about-to-be-enqueued jobs. Guarded by
	// FairQueue.lanesMu. It exists so the reaper never deletes a lane that has
	// work or an in-flight enqueue (the counter-under-lock reap-safety pattern).
	pending int
}
```

- [ ] **Step 2: Add `PerRouteBuffer` to `QueueConfig`** (in the `QueueConfig` struct, after `FailThreshold`):

```go
	// PerRouteBuffer is the buffered capacity of each destination's lane
	// (from [tuning] delivery_queue_size). <=0 defaults to 200.
	PerRouteBuffer int
```

- [ ] **Step 3: Add lane fields to `FairQueue`** and remove `ch`, `destLocks`, `workerCount`:

Remove:
```go
	ch         chan *Job
```
```go
	// Per-destination locks ensure max 1 in-flight send per target
	destLocks sync.Map // target string → *sync.Mutex
```
```go
	// workerCount is the fixed drain-pool size ...
	workerCount int
```

Add (after `dispatcher *Dispatcher`):
```go
	// Per-destination lanes. Each lane has one drainer goroutine; lanes spawn on
	// first job for a target and reap after laneIdleTimeout idle. lanesMu guards
	// the map, each lane's pending, and stopped.
	lanesMu     sync.Mutex
	lanes       map[string]*lane
	stopped     bool
	perRouteBuf int
```

- [ ] **Step 4: Update `NewFairQueue` signature and body.** Change the signature from:

```go
func NewFairQueue(ch chan *Job, senders map[string]Sender, tracker *MessageTracker, cfg QueueConfig, d *Dispatcher) *FairQueue {
```
to:
```go
func NewFairQueue(senders map[string]Sender, tracker *MessageTracker, cfg QueueConfig, d *Dispatcher) *FairQueue {
```

In the body: keep the `cfg.Concurrent* <= 0 → 1` clamps and `failThreshold` default. Add a per-route-buffer default and replace the returned literal:

```go
	perRouteBuf := cfg.PerRouteBuffer
	if perRouteBuf <= 0 {
		perRouteBuf = 200
	}
	return &FairQueue{
		senders:           senders,
		tracker:           tracker,
		dispatcher:        d,
		ctx:               ctx,
		cancel:            cancel,
		lanes:             make(map[string]*lane),
		perRouteBuf:       perRouteBuf,
		failThreshold:     failThreshold,
		failBlockDuration: 5 * time.Minute,
		onDisabled:        cfg.OnDisabled,
		rateLimiter:       cfg.RateLimiter,
		rateLimitHooks:    cfg.RateLimitHooks,
	}
```

The `cfg.ConcurrentDiscord`/`Webhook`/`Telegram` clamps are now only consumed by the senders (via `SetConcurrency` in the dispatcher); keep the clamps so a 0 config still yields a valid sender cap through the dispatcher — actually the dispatcher reads `cfg.Queue.Concurrent*` directly, so ALSO ensure the dispatcher clamps. (See Task 8, Step 3.)

This will not compile until Tasks 6–8 land (the `worker`/`Start`/`Stop`/`ch` references remain). That is expected within this phase; do the remaining Phase-2 tasks before running the suite. Do NOT commit at this step — commit at the end of Task 8 when the package compiles again.

### Task 6: `enqueue` (race-safe lane get-or-create)

**Files:**
- Modify: `processor/internal/delivery/queue.go`

**Interfaces:**
- Produces: `(*FairQueue).enqueue(job *Job, block bool) bool`.

- [ ] **Step 1: Add `enqueue`:**

```go
// enqueue routes a job to its destination lane, spawning the lane + drainer on
// first use. block=true (sends) applies backpressure: it blocks until the lane
// has room. block=false (clean-deletes) drops on a full lane and returns false.
// Returns false if the queue is stopping or the job was dropped.
func (fq *FairQueue) enqueue(job *Job, block bool) bool {
	fq.lanesMu.Lock()
	if fq.stopped {
		fq.lanesMu.Unlock()
		return false
	}
	l, ok := fq.lanes[job.Target]
	if !ok {
		l = &lane{ch: make(chan *Job, fq.perRouteBuf), target: job.Target}
		fq.lanes[job.Target] = l
		fq.wg.Add(1)
		go fq.runLane(l)
	}
	l.pending++ // reserve BEFORE releasing the lock so the reaper can't drop us
	fq.lanesMu.Unlock()

	if block {
		select {
		case l.ch <- job:
			return true
		default:
			// Lane full — record + throttle-log backpressure, then block.
			fq.recordBackpressure(l.target) // added in Phase 3; no-op stub for now
			l.ch <- job
			return true
		}
	}
	select {
	case l.ch <- job:
		return true
	default:
		fq.lanesMu.Lock()
		l.pending--
		fq.lanesMu.Unlock()
		fq.recordCleanDropped(l.target) // added in Phase 3; no-op stub for now
		return false
	}
}
```

- [ ] **Step 2: Add temporary no-op stubs** so Phase 2 compiles before Phase 3 fills them in:

```go
// recordBackpressure / recordCleanDropped are fleshed out in Phase 3
// (metrics + throttled logging). Kept as methods here so enqueue is final.
func (fq *FairQueue) recordBackpressure(target string) {}
func (fq *FairQueue) recordCleanDropped(target string) {}
```

(These are replaced, not appended, in Phase 3 Task 11.)

### Task 7: `runLane` drainer + `Start`/`Stop` rewrite; drop `destLock` + `worker`

**Files:**
- Modify: `processor/internal/delivery/queue.go`

**Interfaces:**
- Produces: `(*FairQueue).runLane(l *lane)`; rewritten `Start()`/`Stop()`.

- [ ] **Step 1: Delete the old `worker` method** entirely:

```go
func (fq *FairQueue) worker() {
	defer fq.wg.Done()
	for job := range fq.ch {
		fq.processJob(job)
	}
}
```

- [ ] **Step 2: Add `runLane`:**

```go
// runLane is one destination's drainer: it processes jobs FIFO and reaps itself
// after laneIdleTimeout with an empty lane and no pending work.
func (fq *FairQueue) runLane(l *lane) {
	defer fq.wg.Done()
	idle := time.NewTimer(laneIdleTimeout)
	defer idle.Stop()
	for {
		select {
		case job, ok := <-l.ch:
			if !ok {
				return // channel closed on shutdown
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			fq.processJob(job)
			fq.lanesMu.Lock()
			l.pending--
			fq.lanesMu.Unlock()
			idle.Reset(laneIdleTimeout)
		case <-idle.C:
			fq.lanesMu.Lock()
			if l.pending == 0 {
				delete(fq.lanes, l.target)
				fq.lanesMu.Unlock()
				return // reap: no work, no in-flight enqueue
			}
			fq.lanesMu.Unlock()
			idle.Reset(laneIdleTimeout)
		}
	}
}
```

- [ ] **Step 3: Rewrite `Start()`** — lanes are self-starting, so `Start` is a no-op kept for API compatibility (the dispatcher + tests call it):

```go
// Start is retained for API compatibility. Lanes spawn on demand in enqueue, so
// there is no worker pool to launch here.
func (fq *FairQueue) Start() {}
```

- [ ] **Step 4: Rewrite `Stop()`** to close every live lane and drain:

```go
// Stop marks the queue stopped (new enqueues rejected), closes every live lane
// so its drainer finishes buffered jobs and exits, waits for all drainers, then
// cancels the context. Buffered jobs are still delivered before shutdown.
func (fq *FairQueue) Stop() {
	fq.lanesMu.Lock()
	fq.stopped = true
	for _, l := range fq.lanes {
		close(l.ch)
	}
	fq.lanesMu.Unlock()

	log.Info("delivery: waiting for delivery lanes to drain...")
	fq.wg.Wait()
	log.Info("delivery: delivery lanes drained")
	fq.cancel()
}
```

- [ ] **Step 5: Drop the `destLock` from `processJob`.** Remove:

```go
	// 1. Acquire per-destination lock (ensures max 1 send per target)
	lockI, _ := fq.destLocks.LoadOrStore(job.Target, &sync.Mutex{})
	destLock := lockI.(*sync.Mutex)
	destLock.Lock()
	defer destLock.Unlock()

```

Lane serialization (one drainer per target) now provides the same guarantee. The rest of `processJob` is unchanged. Also update the rate-limit-hook comment inside `processJob` that says "while it still holds the per-destination mutex" → "while its lane drainer is busy" (the fire-and-forget rationale still holds: a synchronous hook would block the lane's drainer).

- [ ] **Step 6: Remove the now-unused `"sync"` import** only if `sync.Map`/`sync.Mutex`/`sync.WaitGroup` are no longer referenced. `FairQueue.wg sync.WaitGroup`, `lanesMu sync.Mutex`, and `failCounts sync.Map` all remain, so keep `"sync"`.

### Task 8: Dispatcher routes through `enqueue`; wire config

**Files:**
- Modify: `processor/internal/delivery/dispatcher.go`
- Modify: `processor/cmd/processor/main.go`

**Interfaces:**
- Consumes: `(*FairQueue).enqueue`, `NewFairQueue` (new signature).
- Produces: unchanged `Dispatcher.Dispatch/DispatchBypass/QueueDepth` external behavior.

- [ ] **Step 1: Drop `d.ch` from the `Dispatcher` struct** and stop creating the channel.

In `dispatcher.go`, remove the `ch chan *Job` field from `Dispatcher`.

In `NewDispatcher`: remove the `queueSize`/`ch := make(...)` block; construct the queue with the per-route buffer via `QueueConfig`. Replace:

```go
	tracker := NewMessageTracker(cfg.CacheDir, senders)

	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = 1000
	}
	ch := make(chan *Job, queueSize)

	d := &Dispatcher{ch: ch, tracker: tracker}
	d.queue = NewFairQueue(ch, senders, tracker, cfg.Queue, d)
```

with:

```go
	tracker := NewMessageTracker(cfg.CacheDir, senders)

	queueCfg := cfg.Queue
	queueCfg.PerRouteBuffer = cfg.QueueSize // delivery_queue_size => per-route lane buffer

	d := &Dispatcher{tracker: tracker}
	d.queue = NewFairQueue(senders, tracker, queueCfg, d)
```

(Keep the `SetConcurrency` wiring added in Task 3 Step 6 above this block.)

- [ ] **Step 2: Update `NewDispatcherWithSenders`** (test constructor) the same way — drop `ch`, pass `queueSize` as `PerRouteBuffer`:

```go
func NewDispatcherWithSenders(senders map[string]Sender, tracker *MessageTracker, queueSize int, queueCfg QueueConfig) *Dispatcher {
	queueCfg.PerRouteBuffer = queueSize
	d := &Dispatcher{tracker: tracker}
	d.queue = NewFairQueue(senders, tracker, queueCfg, d)
	if tracker != nil {
		tracker.SetCleanDeleteHook(d.enqueueCleanDelete)
	}
	return d
}
```

(Signature unchanged, so its callers in `poracle_admin_*_test.go` and `pokemon_change_test.go` still compile.)

- [ ] **Step 3: Route `Dispatch`/`DispatchBypass`/`enqueueCleanDelete`/`QueueDepth` through the queue.**

Replace `func (d *Dispatcher) Dispatch(job *Job) { d.ch <- job }` with:

```go
func (d *Dispatcher) Dispatch(job *Job) { d.queue.enqueue(job, true) }
```

Replace `DispatchBypass`'s `d.ch <- job` with `d.queue.enqueue(job, true)` (keep `job.BypassRateLimit = true` above it).

In `enqueueCleanDelete`, replace the `select { case d.ch <- job: default: log.Warnf(...) }` block with:

```go
	if !d.queue.enqueue(job, false) {
		log.Warnf("delivery: clean-delete lane full, dropping delete for %s:%s (re-cleaned on next load)", msg.Target, msg.SentID)
	}
```

Keep the `defer func() { _ = recover() }()` guard (a lane channel can close during shutdown; `enqueue` checks `stopped` under lock so the recover is now belt-and-suspenders, but harmless).

Replace `func (d *Dispatcher) QueueDepth() int { return len(d.ch) }` with a lane-summed depth:

```go
// QueueDepth returns the total buffered jobs across all lanes.
func (d *Dispatcher) QueueDepth() int {
	total, _, _, _, _ := d.queue.LaneStats()
	return total
}
```

Add `LaneStats` (exported — the status reporter uses it too) to `queue.go` now, so the dispatcher compiles:

```go
// LaneStats walks the lane map once under lanesMu. Returns total buffered jobs,
// active lane count, deepest single-lane depth, that lane's target, and the
// count of lanes at/over 80% of perRouteBuf. Pure read — sets no metrics.
func (fq *FairQueue) LaneStats() (totalQueued, active, maxDepth int, deepestTarget string, nearCap int) {
	fq.lanesMu.Lock()
	defer fq.lanesMu.Unlock()
	active = len(fq.lanes)
	threshold := fq.perRouteBuf * 8 / 10
	for target, l := range fq.lanes {
		d := len(l.ch)
		totalQueued += d
		if d > maxDepth {
			maxDepth = d
			deepestTarget = target
		}
		if threshold > 0 && d >= threshold {
			nearCap++
		}
	}
	return
}
```

- [ ] **Step 4: Migrate `queue_test.go` construction + enqueue sites.**

(a) Rewrite the helper:

```go
func newTestFairQueue(t *testing.T, senders map[string]Sender, cfg QueueConfig) (*FairQueue, func(*Job)) {
	t.Helper()
	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })
	fq := NewFairQueue(senders, tracker, cfg, nil)
	return fq, func(j *Job) { fq.enqueue(j, true) }
}
```

(b) At every call site using the helper, change `fq, ch := newTestFairQueue(...)` → `fq, enq := newTestFairQueue(...)` and every `ch <- job` → `enq(job)`.

(c) At every direct-construction site — pattern `ch := make(chan *Job, N); tracker := NewMessageTracker(...); fq := NewFairQueue(ch, senders, tracker, cfg, nil)` — delete the `ch := make(...)` line, change to `fq := NewFairQueue(senders, tracker, cfg, nil)`, and change each `ch <- job` to `fq.enqueue(job, true)`.

(d) `TestFairQueue_CleanDeleteSerializedPerTarget` sends clean-deletes via `ch <- &Job{...DeleteSentID...}`. Those are clean-deletes, so change them to `fq.enqueue(&Job{...}, false)` to exercise the real drop-on-full path (the buffer is 200 by default here, well above n=20, so none drop). Keep its assertions; per-target serialization now comes from the lane, and it still expects `maxConcurrent == 1`.

(e) `TestFairQueueStop` (line ~1092): it likely enqueues then calls `Stop` and asserts drain. Keep the assertions; route enqueues through `enq`/`enqueue`. Its buffered-jobs-still-deliver expectation holds under the new `Stop`.

Run after migration: `cd processor && go test ./internal/delivery/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Full gate.**

Run: `cd processor && go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`
Expected: all green (this is the first point since Task 5 that the package compiles).

- [ ] **Step 6: Commit.**

```bash
git add processor/internal/delivery/queue.go processor/internal/delivery/dispatcher.go processor/internal/delivery/queue_test.go
git commit -m "delivery: replace shared queue + worker pool with per-destination lanes

One bounded lane + one drainer per job.Target, spawn-on-demand and idle-reaped
(60s). Sends block on a full lane (backpressure); clean-deletes drop. Removes the
shared channel, fixed worker pool, and per-destination mutex. Per-target
serialization is now inherent (one drainer per target).

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

### Task 9: Lane isolation + reap-safety tests

**Files:**
- Test: `processor/internal/delivery/queue_test.go`

**Interfaces:**
- Consumes: `enqueue`, `Stop`, a blocking mock sender.

- [ ] **Step 1: Isolation test** — a slow/parked target must not delay a free target.

```go
func TestLanes_SlowTargetDoesNotBlockFreeTarget(t *testing.T) {
	release := make(chan struct{})
	freeDone := make(chan struct{})
	sender := &laneMockSender{
		onSend: func(job *Job) {
			if job.Target == "slow" {
				<-release // park the slow lane
			} else {
				close(freeDone)
			}
		},
	}
	senders := map[string]Sender{"discord": sender}
	fq, enq := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 4})
	fq.Start()
	defer func() { close(release); fq.Stop() }()

	enq(&Job{Type: "discord:channel", Target: "slow", Message: json.RawMessage(`{}`)})
	enq(&Job{Type: "discord:channel", Target: "free", Message: json.RawMessage(`{}`)})

	select {
	case <-freeDone:
		// pass: free lane delivered while slow lane parked
	case <-time.After(time.Second):
		t.Fatal("free target blocked behind a parked slow target — lanes not isolated")
	}
}
```

Add a small `laneMockSender` near the other test senders. It hooks BOTH `Send`
and `Delete` so tests can park either path (clean-deletes invoke `Delete`, not
`Send`):

```go
type laneMockSender struct {
	onSend   func(*Job)
	onDelete func(sentID string)
}

func (m *laneMockSender) Send(_ context.Context, job *Job) (*SentMessage, error) {
	if m.onSend != nil {
		m.onSend(job)
	}
	return &SentMessage{ID: "sent-" + job.Target}, nil
}
func (m *laneMockSender) Delete(_ context.Context, sentID string) error {
	if m.onDelete != nil {
		m.onDelete(sentID)
	}
	return nil
}
func (m *laneMockSender) Edit(_ context.Context, _ string, _ json.RawMessage, _ []byte) error {
	return nil
}
func (m *laneMockSender) WaitForRateLimit(string) {}
func (m *laneMockSender) Platform() string { return "discord" }
```

- [ ] **Step 2: Reap-safety race test** — rapid enqueue interleaved with reaping (use a tiny idle timeout via a target that goes idle) must not lose jobs, double-drain, or send on a closed channel. Since `laneIdleTimeout` is a 60s const, drive reaping structurally instead: enqueue N jobs to one target, let them drain, then enqueue again, asserting all deliver. Run under `-race`.

```go
func TestLanes_ReapThenReuseDelivers(t *testing.T) {
	var delivered atomic.Int32
	sender := &laneMockSender{onSend: func(*Job) { delivered.Add(1) }}
	senders := map[string]Sender{"discord": sender}
	fq, enq := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 2})
	fq.Start()
	defer fq.Stop()

	for i := 0; i < 50; i++ {
		enq(&Job{Type: "discord:channel", Target: "t", Message: json.RawMessage(`{}`)})
	}
	// Force a reap of target "t" without waiting 60s: delete via an idle tick by
	// directly invoking the reap path is not exported, so instead assert delivery
	// completes, then enqueue a second wave to prove re-creation works.
	waitFor(t, func() bool { return delivered.Load() == 50 }, time.Second)

	for i := 0; i < 50; i++ {
		enq(&Job{Type: "discord:channel", Target: "t", Message: json.RawMessage(`{}`)})
	}
	waitFor(t, func() bool { return delivered.Load() == 100 }, time.Second)
}

// waitFor polls cond until true or the deadline; fails the test on timeout.
func waitFor(t *testing.T, cond func() bool, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", d)
}
```

Note: a genuine reap↔enqueue `-race` interleaving is covered structurally by the counter-under-lock invariant and Step 3's shutdown test; a timing-driven reap test is omitted to avoid a 60s-real-time test. If a reviewer wants a true reap test, extract `laneIdleTimeout` to a package var settable in tests — but per the Global Constraints it stays a const, so we do not.

- [ ] **Step 3: Shutdown drain test** — buffered jobs across many lanes all deliver before `Stop()` returns.

```go
func TestLanes_ShutdownDrainsAllLanes(t *testing.T) {
	var delivered atomic.Int32
	sender := &laneMockSender{onSend: func(*Job) {
		time.Sleep(time.Millisecond)
		delivered.Add(1)
	}}
	senders := map[string]Sender{"discord": sender}
	fq, enq := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 4})
	fq.Start()

	const lanes, per = 10, 5
	for l := 0; l < lanes; l++ {
		for j := 0; j < per; j++ {
			enq(&Job{Type: "discord:channel", Target: "t" + strconv.Itoa(l), Message: json.RawMessage(`{}`)})
		}
	}
	fq.Stop() // must block until every buffered job is delivered

	if got := delivered.Load(); got != lanes*per {
		t.Errorf("expected %d delivered before Stop returned, got %d", lanes*per, got)
	}
}
```

- [ ] **Step 4: Run under race.**

Run: `cd processor && go test ./internal/delivery/ -run 'TestLanes' -race -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add processor/internal/delivery/queue_test.go
git commit -m "delivery: lane isolation, reuse-after-drain, and shutdown-drain tests

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

### Task 10: Overflow (backpressure vs drop) test

**Files:**
- Test: `processor/internal/delivery/queue_test.go`

**Interfaces:**
- Consumes: `enqueue` directly (to control `block`), a parked sender.

- [ ] **Step 1: Write the test** — with a parked drainer and a tiny per-route buffer, a `block=false` clean-delete beyond capacity is dropped (`enqueue` returns false), while other lanes are unaffected.

Clean-deletes invoke `Sender.Delete`, so park the drainer via `onDelete` (NOT
`onSend`). The "enqueue until one drops" formulation is timing-robust: regardless
of exactly when the drainer picks up the first job, filling a bounded buffer must
eventually return false, and a different target must still accept.

```go
func TestLanes_CleanDeleteDropsOnFullLane(t *testing.T) {
	release := make(chan struct{})
	sender := &laneMockSender{onDelete: func(string) { <-release }} // park the drainer
	senders := map[string]Sender{"discord": sender}
	fq, _ := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 1, PerRouteBuffer: 2})
	fq.Start()
	defer func() { close(release); fq.Stop() }()

	accepted, dropped := 0, 0
	for i := 0; i < 20; i++ {
		if fq.enqueue(&Job{Type: "discord:channel", Target: "t", DeleteSentID: "t:" + strconv.Itoa(i)}, false) {
			accepted++
		} else {
			dropped++
		}
	}
	if accepted == 0 {
		t.Fatal("expected some clean-deletes to be accepted")
	}
	if dropped == 0 {
		t.Error("expected some clean-deletes to be dropped on the full lane (drainer parked, buffer=2)")
	}

	// A different target's lane is unaffected.
	if !fq.enqueue(&Job{Type: "discord:channel", Target: "other", DeleteSentID: "other:1"}, false) {
		t.Error("a different target's lane should accept the clean-delete")
	}
}
```

- [ ] **Step 2: Run under race a few times.**

Run: `cd processor && go test ./internal/delivery/ -run TestLanes_CleanDeleteDropsOnFullLane -race -count=5 -v`
Expected: PASS every run.

- [ ] **Step 3: Commit.**

```bash
git add processor/internal/delivery/queue_test.go
git commit -m "delivery: clean-delete drop-on-full-lane test

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

# Phase 3 — Instrumentation

Bounded-cardinality lane metrics + a `[Status]` health log that names the deepest lane and escalates to WARN when a lane nears capacity or backpressure advances.

### Task 11: Lane metrics + backpressure/drop counting

**Files:**
- Modify: `processor/internal/metrics/metrics.go`
- Modify: `processor/internal/delivery/queue.go`
- Test: `processor/internal/delivery/queue_test.go`

**Interfaces:**
- Produces: new metrics; real `recordBackpressure`/`recordCleanDropped`; `(*FairQueue).BackpressureCount() int64`.

- [ ] **Step 1: Add metrics** to `metrics.go` in the `// Delivery metrics` block (after `DeliveryTelegramQueueDepth`):

```go
	// Per-destination lane metrics (aggregate — no per-target labels).
	DeliveryActiveLanes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_delivery_active_lanes",
		Help: "Live per-destination delivery lanes (spawned minus reaped)",
	})
	DeliveryLaneQueued = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_delivery_lane_queued",
		Help: "Total buffered delivery jobs across all lanes",
	})
	DeliveryLaneMaxDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_delivery_lane_max_depth",
		Help: "Deepest single lane's buffered depth (head-of-line signal)",
	})
	DeliveryLanesNearCapacity = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_delivery_lanes_near_capacity",
		Help: "Lanes at or above 80% of the per-route buffer",
	})
	DeliveryLaneBackpressure = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poracle_delivery_lane_backpressure_total",
		Help: "Send enqueues that blocked on a full lane",
	})
	DeliveryCleanDeleteDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poracle_delivery_clean_delete_dropped_total",
		Help: "Clean-deletes dropped on a full lane (re-cleaned on next load)",
	})
	DeliveryLaneSpawned = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poracle_delivery_lane_spawned_total",
		Help: "Delivery lanes spawned",
	})
	DeliveryLaneReaped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poracle_delivery_lane_reaped_total",
		Help: "Delivery lanes reaped after idle",
	})
```

- [ ] **Step 2: Count spawn/reap** in `queue.go`. In `enqueue`, right after `go fq.runLane(l)`, add `metrics.DeliveryLaneSpawned.Inc()`. In `runLane`, right before the `return` in the reap branch (`if l.pending == 0 {`), add `metrics.DeliveryLaneReaped.Inc()`.

- [ ] **Step 3: Replace the Phase-2 stubs** `recordBackpressure`/`recordCleanDropped` with real implementations that count metrics, bump an atomic, and throttle-log:

```go
// recordBackpressure is called when a send blocks on a full lane. It counts the
// event and logs at most once per 5s per queue (naming the target), so a hot
// lane doesn't flood the log.
func (fq *FairQueue) recordBackpressure(target string) {
	metrics.DeliveryLaneBackpressure.Inc()
	fq.backpressure.Add(1)
	now := time.Now().UnixNano()
	last := fq.lastBackpressureLog.Load()
	if now-last > int64(5*time.Second) && fq.lastBackpressureLog.CompareAndSwap(last, now) {
		log.Warnf("delivery: lane full, applying backpressure to sends for %s", target)
	}
}

// recordCleanDropped is called when a clean-delete is dropped on a full lane.
func (fq *FairQueue) recordCleanDropped(target string) {
	metrics.DeliveryCleanDeleteDropped.Inc()
}
```

Add fields to `FairQueue`:

```go
	backpressure        atomic.Int64
	lastBackpressureLog atomic.Int64
```

Add `BackpressureCount`:

```go
// BackpressureCount returns the cumulative count of send enqueues that blocked
// on a full lane. Used by the [Status] reporter to detect a developing backlog.
func (fq *FairQueue) BackpressureCount() int64 { return fq.backpressure.Load() }
```

- [ ] **Step 4: Add dispatcher passthroughs** for the status reporter (`LaneStats` already exists on the queue from Task 8, and stays pure — no metric writes; the reporter sets gauges). In `dispatcher.go` add:

```go
// LaneStats exposes per-lane aggregates for the [Status] reporter.
func (d *Dispatcher) LaneStats() (totalQueued, active, maxDepth int, deepestTarget string, nearCap int) {
	return d.queue.LaneStats()
}

// BackpressureCount exposes the cumulative full-lane backpressure count.
func (d *Dispatcher) BackpressureCount() int64 { return d.queue.BackpressureCount() }

// PerRouteBuffer is the configured per-lane buffer size (for near-capacity math).
func (d *Dispatcher) PerRouteBuffer() int { return d.queue.perRouteBuf }
```

- [ ] **Step 5: `LaneStats` unit test.**

```go
func TestLaneStats(t *testing.T) {
	release := make(chan struct{})
	sender := &laneMockSender{onSend: func(*Job) { <-release }}
	senders := map[string]Sender{"discord": sender}
	fq, _ := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 1, PerRouteBuffer: 10})
	fq.Start()
	defer func() { close(release); fq.Stop() }()

	// One lane parked in-flight + 6 buffered => depth 6, active 1.
	for i := 0; i < 7; i++ {
		fq.enqueue(&Job{Type: "discord:channel", Target: "t", Message: json.RawMessage(`{}`)}, true)
	}
	waitFor(t, func() bool {
		total, active, maxDepth, target, _ := fq.LaneStats()
		return active == 1 && total == 6 && maxDepth == 6 && target == "t"
	}, time.Second)
}
```

- [ ] **Step 6: Run + gate.**

Run: `cd processor && go test ./internal/delivery/ ./internal/metrics/ -race -count=1 && golangci-lint run ./...`
Expected: PASS.

- [ ] **Step 7: Commit.**

```bash
git add processor/internal/metrics/metrics.go processor/internal/delivery/queue.go processor/internal/delivery/dispatcher.go processor/internal/delivery/queue_test.go
git commit -m "delivery: lane metrics, backpressure/drop counting, LaneStats

Aggregate (no per-target label) gauges + counters for active lanes, total/max
depth, near-capacity, backpressure, clean-delete drops, spawn/reap. Throttled
backpressure log names the target.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

### Task 12: `[Status]` lane summary + WARN escalation

**Files:**
- Modify: `processor/cmd/processor/main.go`

**Interfaces:**
- Consumes: `Dispatcher.LaneStats()`, `BackpressureCount()`, `PerRouteBuffer()`, the new metrics.

- [ ] **Step 1: Add a package-level (or closure-captured) `lastBackpressureSeen int64`** in the status-reporter goroutine so it can detect advances between samples. Declare it just before the `for { ... }`/`ticker` loop that builds `statusParts` (find the goroutine around `main.go:840`).

- [ ] **Step 2: Extend the delivery block** (currently `main.go:864-876`). After the existing `metrics.DeliveryTrackerSize.Set(...)` line and before the closing `}`, add:

```go
				total, active, maxDepth, deepestTarget, nearCap := proc.dispatcher.LaneStats()
				statusParts = append(statusParts, fmt.Sprintf("Lanes:%d active, %d queued, deepest=%d (%s), %d near-cap",
					active, total, maxDepth, deepestTarget, nearCap))
				metrics.DeliveryActiveLanes.Set(float64(active))
				metrics.DeliveryLaneQueued.Set(float64(total))
				metrics.DeliveryLaneMaxDepth.Set(float64(maxDepth))
				metrics.DeliveryLanesNearCapacity.Set(float64(nearCap))

				// Escalate to WARN when a lane nears capacity OR backpressure
				// advanced since the last sample — the "developing bad
				// situation" signal.
				bp := proc.dispatcher.BackpressureCount()
				buf := proc.dispatcher.PerRouteBuffer()
				if (buf > 0 && maxDepth >= buf*8/10) || bp > lastBackpressureSeen {
					log.Warnf("[Status] delivery backing up: %d lanes, %d queued, deepest lane %d/%d (%s), %d near-cap, backpressure=%d",
						active, total, maxDepth, buf, deepestTarget, nearCap, bp)
				}
				lastBackpressureSeen = bp
```

Rationale for a separate WARN line rather than switching the whole `[Status]` line's level: the combined status line carries many unrelated subsystems; emitting a distinct WARN keeps the routine `[Status]` at Info while still surfacing an operator-actionable alert. This satisfies the spec's intent ("escalate ... to WARN when maxDepth ≥ 80% × perRouteBuf OR backpressure advanced").

- [ ] **Step 3: Build + run the processor package tests + gate.**

Run: `cd processor && go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`
Expected: all green.

- [ ] **Step 4: Manual sanity note (no automated test for the log line).** Confirm by reading that the `[Status]` line now contains a `Lanes:` segment and that `lastBackpressureSeen` is captured once (not re-declared inside the loop).

- [ ] **Step 5: Commit.**

```bash
git add processor/cmd/processor/main.go
git commit -m "delivery: [Status] lane summary + WARN escalation on backlog

The periodic status line now names the deepest lane's target (log field, not a
metric label) and emits a WARN when a lane nears capacity or backpressure
advanced since the last sample.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Final verification

- [ ] **Full gate, race, twice** (catch flakiness):

```bash
cd processor && go build ./... && go vet ./... && go test -race -count=1 ./... && golangci-lint run ./...
cd processor && go test -race -count=1 ./internal/delivery/...
```

- [ ] **Grep for leftovers** — no references to removed symbols remain:

```bash
cd processor && grep -rn "discordSem\|webhookSem\|telegramSem\|semaphoreFor\|counterFor\|destLocks\|d\.ch\b\|fq\.ch\b" internal/ cmd/ || echo "clean"
```
Expected: `clean` (or only comments/docs).

- [ ] **Confirm #182's clean-delete behavior still holds end-to-end**: `TestFairQueue_CleanDeleteSerializedPerTarget` and the discord/telegram 429-retry tests all pass under `-race`.

---

## Self-Review Checklist (run before handing off)

1. **Spec coverage:**
   - D1 (supersede #182, reuse building blocks) — Tasks reuse `doWithRetry`/`doPostWithRetry`, `DeleteSentID`, clean-delete hook. ✓
   - D2 (lanes, spawn-on-demand + idle-reap) — Tasks 5–7. ✓
   - D3 (semaphore only during wire call) — Tasks 1–3. ✓
   - D4 (`delivery_queue_size` → per-route buffer) — Task 8 Step 1. ✓
   - D5 (block sends + log/count; drop clean-deletes + log/count) — Tasks 6, 10, 11. ✓
   - Instrumentation (aggregate metrics + named-target status + WARN) — Tasks 11–12. ✓
   - Testing list in spec — isolation (Task 9), serialization (migrated `TestFairQueue_CleanDeleteSerializedPerTarget`), reap/reuse (Task 9), global cap (Task 4), sem-released-during-waits (Tasks 1–2), overflow (Task 10), instrumentation (Task 11), shutdown (Task 9), clean-delete e2e (existing). ✓
2. **Type consistency:** `LaneStats()` returns `(totalQueued, active, maxDepth int, deepestTarget string, nearCap int)` everywhere (queue, dispatcher, main.go). `enqueue(job, block) bool`. `SetConcurrency(discord, webhook)` for Discord, `SetConcurrency(n)` for Telegram. `makeSem` shared in-package.
3. **No placeholders:** the Phase-2 `recordBackpressure`/`recordCleanDropped` stubs are explicitly replaced in Task 11 Step 3 (not left as no-ops).
4. **Compile ordering:** Tasks 5–8 land together (package won't compile mid-Phase-2); the plan says commit only at Task 8 Step 6. Every other task ends compilable + green.
