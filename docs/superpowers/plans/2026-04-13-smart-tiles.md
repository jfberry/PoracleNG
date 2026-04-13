# Smart Tile Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Skip tile generation when no matched user's template references staticMap; use inline mode (no disk) when all users can accept bytes; fall back to URL mode otherwise.

**Architecture:** Template scanning at compile time determines if a template uses staticMap. At enrichment time (after matching), check each user's resolved template + platform to decide tile mode (skip/inline/URL). Inline mode POSTs without `pregenerate=true` and carries PNG bytes through TilePending → RenderJob → DeliveryJob → delivery.Job → Discord sender. Weather per-user tiles use the same three-tier decision per user.

**Tech Stack:** Go, raymond (Handlebars), tileservercache HTTP API

---

### Task 1: Add UsesTile to TemplateStore

**Files:**
- Modify: `processor/internal/dts/templates.go`

The `Get()` method at line 196 compiles and caches templates. We need to also cache whether the compiled template source contains `staticMap`.

- [ ] **Step 1: Add tileUsage cache field to TemplateStore**

In `processor/internal/dts/templates.go`, add a field to the `TemplateStore` struct (alongside the existing `cache` field):

```go
tileUsage map[*raymond.Template]bool // cached: does this template reference staticMap?
```

Initialize it in the constructor (where `cache` is initialized with `make`):

```go
tileUsage: make(map[*raymond.Template]bool),
```

Clear it in `ClearCache()` and `Reload()` alongside the template cache clear.

- [ ] **Step 2: Add UsesTile method**

Add to `processor/internal/dts/templates.go`:

```go
// UsesTile checks whether the resolved template for the given parameters
// references staticMap/staticmap. Uses the same selection chain as Get().
// Returns true (conservative) if the template can't be found.
func (ts *TemplateStore) UsesTile(templateType, platform, templateID, language string) bool {
	tmpl := ts.Get(templateType, platform, templateID, language)
	if tmpl == nil {
		return true // conservative: unknown template → assume needs tile
	}

	ts.mu.RLock()
	result, ok := ts.tileUsage[tmpl]
	ts.mu.RUnlock()
	if ok {
		return result
	}

	// Scan the template source for staticMap references
	source := tmpl.PrintAST()
	uses := strings.Contains(strings.ToLower(source), "staticmap")

	ts.mu.Lock()
	ts.tileUsage[tmpl] = uses
	ts.mu.Unlock()

	return uses
}
```

Note: `raymond.Template` has a `PrintAST()` method that returns the template source. If this is not available, use the `resolveTemplate` output (the raw Handlebars string before compilation). In that case, store the source alongside the compiled template in the cache, or scan at compile time in `Get()`.

**Alternative if PrintAST is unavailable:** Add a parallel `sourceCache map[string]string` (keyed by cacheKey) populated in `Get()` at line 212 where `tmplStr` is available. Then `UsesTile` looks up the source by the same cacheKey.

- [ ] **Step 3: Build and verify**

Run: `cd processor && go build ./... && go test ./internal/dts/ -count=1`

- [ ] **Step 4: Commit**

```bash
git add processor/internal/dts/templates.go
git commit -m "Add UsesTile to TemplateStore

Checks whether a resolved template references staticMap using the
same selection chain as rendering. Result cached per compiled template."
```

---

### Task 2: Add tile mode constants and tileMode helper

**Files:**
- Create: `processor/cmd/processor/tilemode.go`

- [ ] **Step 1: Create tilemode.go**

```go
package main

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

const (
	tileModeSkip   = 0 // no template uses staticMap → don't generate tile
	tileModeInline = 1 // all users can accept bytes → POST without pregenerate
	tileModeURL    = 2 // at least one user needs a fetchable URL → pregenerate
)

// tileMode determines whether we need a tile and in what form.
// Called after matching, before enrichment.
func (ps *ProcessorService) tileMode(templateType string, matched []webhook.MatchedUser) int {
	if ps.dtsRenderer == nil {
		return tileModeSkip
	}
	ts := ps.dtsRenderer.Templates()

	anyNeedsTile := false
	anyNeedsURL := false

	for _, u := range matched {
		// Resolve the template ID the same way the renderer does
		tmplID := u.Template
		if tmplID == "" {
			tmplID = ps.dtsRenderer.ResolveTemplate("")
		}

		lang := u.Language
		if lang == "" {
			lang = ps.cfg.General.Locale
		}

		platform := platformFromUserType(u.Type)

		if !ts.UsesTile(templateType, platform, tmplID, lang) {
			continue // this user's template doesn't use staticMap
		}

		anyNeedsTile = true

		if !ps.canUploadInline(u.Type) {
			anyNeedsURL = true
			break // one URL-needer is enough, no need to check further
		}
	}

	if !anyNeedsTile {
		return tileModeSkip
	}
	if anyNeedsURL {
		return tileModeURL
	}
	return tileModeInline
}

// canUploadInline returns true if this destination type supports receiving
// uploaded image bytes instead of a fetchable URL.
func (ps *ProcessorService) canUploadInline(userType string) bool {
	platform := delivery.PlatformFromType(userType)
	switch platform {
	case "discord":
		return ps.cfg.Discord.UploadEmbedImages
	default:
		return false // Telegram always needs URL
	}
}

// platformFromUserType extracts "discord" or "telegram" for template lookup.
func platformFromUserType(userType string) string {
	if userType == "webhook" {
		return "discord"
	}
	parts := strings.SplitN(userType, ":", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return "discord"
}
```

- [ ] **Step 2: Expose ResolveTemplate on Renderer**

In `processor/internal/dts/renderer.go`, the `resolveTemplate` method (line 99) is unexported. Add an exported wrapper:

```go
// ResolveTemplate returns the template ID to use, applying the default if empty.
func (r *Renderer) ResolveTemplate(trackingTemplate string) string {
	return r.resolveTemplate(trackingTemplate)
}
```

- [ ] **Step 3: Build and verify**

Run: `cd processor && go build ./...`

- [ ] **Step 4: Commit**

```bash
git add processor/cmd/processor/tilemode.go processor/internal/dts/renderer.go
git commit -m "Add tile mode decision helper (skip/inline/URL)

Checks each matched user's resolved template for staticMap usage
and platform for upload capability. Returns tileModeSkip when no
user needs a tile, tileModeInline when all can accept bytes,
tileModeURL when any needs a fetchable URL."
```

---

### Task 3: Add inline tile generation to Resolver

**Files:**
- Modify: `processor/internal/staticmap/staticmap.go`

- [ ] **Step 1: Enhance TilePending for inline mode**

Update TilePending (line 108):

```go
type TilePending struct {
	Result    chan string  // URL mode: receives tile URL
	ResultImg chan []byte  // Inline mode: receives PNG bytes
	Inline    bool        // which mode was requested
	Deadline  time.Time
	Fallback  string
	target    map[string]any
}
```

Add `ApplyInline`:

```go
// ApplyInline writes a marker into the enrichment map and returns the image bytes.
// The actual bytes are carried through the RenderJob, not stored in enrichment.
func (tp *TilePending) ApplyInline() {
	if tp.target != nil {
		tp.target["staticMap"] = "inline"
		tp.target["staticmap"] = "inline"
	}
}
```

- [ ] **Step 2: Add GenerateInlineTile method**

Add after `generatePregenTile`:

```go
// GenerateInlineTile POSTs to the tileserver without pregenerate=true,
// receiving the rendered PNG bytes directly. No file is stored on disk.
func (r *Resolver) GenerateInlineTile(maptype string, data map[string]any, staticMapType string) []byte {
	// Same circuit breaker as generatePregenTile
	r.mu.Lock()
	if r.consecutiveErrors >= r.config.TileserverFailureThreshold {
		elapsed := time.Since(r.circuitOpenSince)
		cooldown := time.Duration(r.config.TileserverCooldownMs) * time.Millisecond
		if elapsed < cooldown {
			r.mu.Unlock()
			metrics.TileTotal.WithLabelValues("circuit_break").Inc()
			return nil
		}
		if r.halfOpenProbeActive {
			r.mu.Unlock()
			metrics.TileTotal.WithLabelValues("circuit_break").Inc()
			return nil
		}
		r.halfOpenProbeActive = true
	}
	r.mu.Unlock()

	metrics.TileInFlight.Inc()
	defer metrics.TileInFlight.Dec()
	start := time.Now()

	mapPath := "staticmap"
	templateType := ""
	if strings.EqualFold(staticMapType, "multistaticmap") {
		mapPath = "multistaticmap"
		templateType = "multi-"
	}

	// No pregenerate=true — tileserver returns image bytes directly
	reqURL := fmt.Sprintf("%s/%s/poracle-%s%s",
		r.config.ProviderURL, mapPath, templateType, maptype)

	body, err := json.Marshal(data)
	if err != nil {
		log.Warnf("staticmap: marshal inline data: %s", err)
		metrics.TileTotal.WithLabelValues("error").Inc()
		return nil
	}

	log.Debugf("staticmap: POST inline %s type=%s%s body=%s", reqURL, templateType, maptype, string(body))

	resp, err := r.client.Post(reqURL, "application/json", bytes.NewReader(body))
	if err != nil {
		r.recordError()
		metrics.TileTotal.WithLabelValues("error").Inc()
		metrics.TileDuration.Observe(time.Since(start).Seconds())
		log.Warnf("staticmap: inline request failed: %s", err)
		return nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		r.recordError()
		metrics.TileTotal.WithLabelValues("error").Inc()
		metrics.TileDuration.Observe(time.Since(start).Seconds())
		log.Warnf("staticmap: read inline response: %s", err)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		r.recordError()
		metrics.TileTotal.WithLabelValues("error").Inc()
		metrics.TileDuration.Observe(time.Since(start).Seconds())
		log.Warnf("staticmap: inline %s got status %d: %s", reqURL, resp.StatusCode, string(respBody[:min(len(respBody), 200)]))
		return nil
	}

	r.recordSuccess()
	duration := time.Since(start)
	metrics.TileTotal.WithLabelValues("inline_ok").Inc()
	metrics.TileDuration.Observe(duration.Seconds())
	r.statCalls.Add(1)
	r.statTotalMs.Add(duration.Milliseconds())

	return respBody
}
```

- [ ] **Step 3: Add SubmitTileInline method**

```go
// SubmitTileInline queues an inline tile request that returns image bytes.
func (r *Resolver) SubmitTileInline(maptype string, data map[string]any, staticMapType string, target map[string]any) *TilePending {
	pending := &TilePending{
		ResultImg: make(chan []byte, 1),
		Inline:    true,
		Deadline:  time.Now().Add(r.TileDeadline()),
		Fallback:  r.config.FallbackURL,
		target:    target,
	}

	select {
	case r.tileQueue <- tileRequest{pending: pending, maptype: maptype, data: data, staticMapType: staticMapType}:
		metrics.TileQueueDepth.Set(float64(len(r.tileQueue)))
	default:
		pending.ResultImg <- nil
		metrics.TileTotal.WithLabelValues("queue_full").Inc()
		log.Warnf("staticmap: tile queue full, skipping inline tile for %s", maptype)
	}

	return pending
}
```

- [ ] **Step 4: Update tileWorker to handle inline mode**

Update `tileWorker` (line 261):

```go
func (r *Resolver) tileWorker() {
	defer r.wg.Done()
	for {
		select {
		case req := <-r.tileQueue:
			metrics.TileQueueDepth.Set(float64(len(r.tileQueue)))
			if req.pending.Inline {
				if time.Now().After(req.pending.Deadline) {
					req.pending.ResultImg <- nil
					metrics.TileTotal.WithLabelValues("deadline").Inc()
					continue
				}
				imgData := r.GenerateInlineTile(req.maptype, req.data, req.staticMapType)
				req.pending.ResultImg <- imgData
			} else {
				if time.Now().After(req.pending.Deadline) {
					req.pending.Result <- req.pending.Fallback
					metrics.TileTotal.WithLabelValues("deadline").Inc()
					continue
				}
				url := r.generatePregenTile(req.maptype, req.data, req.staticMapType)
				if url == "" {
					url = req.pending.Fallback
				}
				req.pending.Result <- url
			}
		case <-r.done:
			return
		}
	}
}
```

- [ ] **Step 5: Build and test**

Run: `cd processor && go build ./... && go test ./internal/staticmap/ -count=1`

- [ ] **Step 6: Commit**

```bash
git add processor/internal/staticmap/staticmap.go
git commit -m "Add inline tile generation (POST without pregenerate)

TilePending supports both URL mode (Result chan string) and inline
mode (ResultImg chan []byte). GenerateInlineTile POSTs without
pregenerate=true and returns PNG bytes. SubmitTileInline queues
inline requests. tileWorker handles both modes."
```

---

### Task 4: Update enricher to accept tile mode

**Files:**
- Modify: `processor/internal/enrichment/enrichment.go`

- [ ] **Step 1: Update addStaticMap to accept tile mode**

Change `addStaticMap` signature (line 172) to accept a tile mode parameter:

```go
func (e *Enricher) addStaticMap(m map[string]any, maptype string, lat, lon float64, webhookFields map[string]any, tileMode int) *staticmap.TilePending {
	if e.StaticMap == nil || tileMode == 0 { // 0 = tileModeSkip
		return nil
	}

	merged := make(map[string]any, len(m)+len(webhookFields)+2)
	maps.Copy(merged, m)
	maps.Copy(merged, webhookFields)
	merged["latitude"] = lat
	merged["longitude"] = lon
	keys, pregenKeys := staticMapFieldsForType(maptype)

	if tileMode == 1 { // tileModeInline
		filtered := filterFields(merged, pregenKeys)
		// Add nearby stops to the filtered copy if configured
		e.StaticMap.AddNearbyStops(filtered, merged, maptype)
		return e.StaticMap.SubmitTileInline(maptype, filtered, e.StaticMap.GetStaticMapType(maptype), m)
	}

	// tileModeURL (2) — current flow
	url, pending := e.StaticMap.GetStaticMapURLAsync(maptype, merged, keys, pregenKeys, m)
	if pending != nil {
		return pending
	}
	m["staticMap"] = url
	m["staticmap"] = url
	return nil
}
```

Note: `AddNearbyStops` and `GetStaticMapType` may need to be exposed from the Resolver. Check if `addNearbyStops` and `getConfigForTileType` need public wrappers.

- [ ] **Step 2: Update all callers of addStaticMap**

Every enricher type calls `addStaticMap`. Add `tileMode` as the last parameter. For now, pass `2` (tileModeURL) as default — webhook handlers will pass the real value in Task 6.

Files to update: `pokemon.go`, `raid.go`, `invasion.go`, `quest.go`, `lure.go`, `nest.go`, `gym.go`, `fort.go`, `maxbattle.go`, `weather.go` in `processor/internal/enrichment/`.

Also update `enrich.go` (the API enrichment path) to pass `2` (tileModeURL).

- [ ] **Step 3: Update each type-specific enricher signature**

Each `Enricher.Pokemon()`, `Enricher.Raid()`, etc. needs to accept and pass through `tileMode int`. Update signatures and pass through to `addStaticMap`.

- [ ] **Step 4: Build and test**

Run: `cd processor && go build ./... && go test ./...`

- [ ] **Step 5: Commit**

```bash
git add -u
git commit -m "Pass tile mode through enrichment pipeline

addStaticMap accepts tileModeSkip (0), tileModeInline (1), or
tileModeURL (2). Skip returns nil immediately. Inline submits via
SubmitTileInline. URL uses current flow. All enrichers pass through
the mode; default is tileModeURL for backward compatibility."
```

---

### Task 5: Add StaticMapData to DeliveryJob and delivery.Job

**Files:**
- Modify: `processor/internal/webhook/types.go`
- Modify: `processor/internal/delivery/delivery.go`
- Modify: `processor/cmd/processor/render.go`

- [ ] **Step 1: Add StaticMapData to DeliveryJob**

In `processor/internal/webhook/types.go`, add after `Language` (line 424):

```go
StaticMapData []byte `json:"-"` // inline tile image bytes (not serialised)
```

- [ ] **Step 2: Add StaticMapData to delivery.Job**

In `processor/internal/delivery/delivery.go`, add after `Lon` (line 37):

```go
StaticMapData []byte `json:"-"` // inline tile image bytes
```

- [ ] **Step 3: Update RenderJob to carry image data**

In `processor/cmd/processor/render.go`, add to RenderJob struct:

```go
TileImageData []byte // inline tile bytes, set during tile resolution
```

- [ ] **Step 4: Update processRenderJob tile resolution for inline mode**

Update the tile resolution block (line 47):

```go
if job.TilePending != nil {
	queueLen := len(ps.renderCh)
	queueCap := cap(ps.renderCh)
	if queueCap > 0 && float64(queueLen)/float64(queueCap) > 0.8 {
		if job.TilePending.Inline {
			job.TilePending.ApplyInline()
		} else {
			job.TilePending.Apply(job.TilePending.Fallback)
		}
		metrics.RenderTileSkipped.Inc()
	} else if job.TilePending.Inline {
		select {
		case imgData := <-job.TilePending.ResultImg:
			if imgData != nil {
				job.TilePending.ApplyInline()
				job.TileImageData = imgData
			}
		case <-time.After(time.Until(job.TilePending.Deadline)):
			// no image, proceed without
		}
	} else {
		select {
		case url := <-job.TilePending.Result:
			if url != "" {
				job.TilePending.Apply(url)
			} else {
				job.TilePending.Apply(job.TilePending.Fallback)
			}
		case <-time.After(time.Until(job.TilePending.Deadline)):
			job.TilePending.Apply(job.TilePending.Fallback)
		}
	}
}
```

- [ ] **Step 5: Pass TileImageData through renderer to DeliveryJob**

The DTS renderer builds `DeliveryJob` entries. The `TileImageData` needs to be set on each `DeliveryJob`. Since the renderer doesn't know about tile data, the simplest approach: set it in `processRenderJob` after rendering, before dispatch:

In the dispatch loop (line 109):

```go
for _, j := range jobs {
	ps.dispatcher.Dispatch(&delivery.Job{
		Target:        j.Target,
		Type:          j.Type,
		Message:       j.Message,
		TTH:           tthFromMap(j.TTH),
		Clean:         j.Clean,
		Name:          j.Name,
		LogReference:  j.LogReference,
		Lat:           parseCoordFloat(j.Lat),
		Lon:           parseCoordFloat(j.Lon),
		EditKey:       j.EditKey,
		StaticMapData: job.TileImageData, // from RenderJob, shared across all jobs
	})
}
```

- [ ] **Step 6: Build and test**

Run: `cd processor && go build ./... && go test ./...`

- [ ] **Step 7: Commit**

```bash
git add -u
git commit -m "Carry inline tile bytes through render → delivery pipeline

StaticMapData on DeliveryJob and delivery.Job carries PNG bytes from
inline tile generation. processRenderJob resolves inline tiles and
copies bytes to all dispatched jobs."
```

---

### Task 6: Update webhook handlers to pass tile mode

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

- [ ] **Step 1: Update pokemon.go**

Before the enrichment call (line 169), compute tile mode:

```go
mode := ps.tileMode("monster", matched)
baseEnrichment, tilePending := ps.enricher.Pokemon(&pokemon, processed, mode)
```

- [ ] **Step 2: Update all other non-weather handlers**

Same pattern for raid.go, invasion.go, quest.go, lure.go, nest.go, gym.go, fort.go, maxbattle.go — compute `ps.tileMode(templateType, matched)` before enrichment and pass to the enricher.

- [ ] **Step 3: Update weather.go**

Weather is special — per-user tiles. For the base tile (shared):

```go
baseMode := ps.tileMode("weatherchange", matched)
baseEnrichment, baseTilePending := ps.enricher.Weather(change, baseMode)
```

For per-user tiles inside the loop:

```go
userMode := ps.tileMode("weatherchange", []webhook.MatchedUser{user})
```

Pass `userMode` to `WeatherTranslate` so it can decide whether to generate a per-user tile.

- [ ] **Step 4: Build and test**

Run: `cd processor && go build ./... && go test ./...`

- [ ] **Step 5: Commit**

```bash
git add -u
git commit -m "Pass tile mode from webhook handlers to enrichment

Each handler computes tileMode after matching and passes to enricher.
Weather handler computes per-user tile mode for showAlteredPokemon.
tileModeSkip avoids tile generation entirely when no template uses it."
```

---

### Task 7: Update Discord sender to use StaticMapData

**Files:**
- Modify: `processor/internal/delivery/discord.go`

- [ ] **Step 1: Update postMessage**

Before the `DownloadImage` path (line 185), check for inline bytes:

```go
if len(job.StaticMapData) > 0 && imageURL != "" {
	// Inline tile: bytes already available, skip download
	log.Debugf("discord: using inline tile for bot/%s (%d bytes)", channelID, len(job.StaticMapData))
	normalized = ReplaceEmbedImageURL(normalized)
	buf, ct, err := BuildMultipartMessage(normalized, job.StaticMapData, "files[0]")
	if err == nil {
		reqBody = buf
		contentType = ct
	}
} else if imageURL != "" {
	// Current flow: download and upload
	...
}
```

Note: `postMessage` currently takes `(ctx, channelID, message)` — it doesn't have access to `job.StaticMapData`. The `Send` method dispatches to `postMessage`/`postWebhook`. Need to pass `StaticMapData` through. Options:
- Change `postMessage` signature to accept `staticMapData []byte`
- Or pass the full `*Job` instead of just `message`

Cleanest: pass `staticMapData []byte` as an additional parameter to `postMessage` and `postWebhook`.

- [ ] **Step 2: Update postWebhook similarly**

Same pattern for the webhook path.

- [ ] **Step 3: Update Send to pass StaticMapData**

Update `Send` (line 46) to pass `job.StaticMapData` to `postMessage`/`postWebhook`.

- [ ] **Step 4: Build and test**

Run: `cd processor && go build ./... && go test ./internal/delivery/ -count=1`

- [ ] **Step 5: Commit**

```bash
git add processor/internal/delivery/discord.go
git commit -m "Use inline tile bytes in Discord sender, skip download

When StaticMapData is present on the Job, use it directly instead of
downloading from the tileserver URL. Saves one HTTP round-trip and
avoids needing a fetchable URL for localhost tileservers."
```

---

### Task 8: Full build, all tests, manual verification

- [ ] **Step 1: Full build**

Run: `cd processor && go build ./...`

- [ ] **Step 2: Run all tests**

Run: `cd processor && go test ./... -count=1`

- [ ] **Step 3: Manual verification**

1. Set `upload_embed_images = true` in Discord config
2. Send a pokemon webhook with only Discord users tracking
3. Verify logs show `staticmap: POST inline` (not `pregenerate=true`)
4. Verify tile appears in Discord message
5. Verify tileserver disk — no new file created
6. Test with a Telegram user tracking same pokemon — should fall back to URL mode
7. Test with a Telegram user whose template doesn't use staticMap — should skip tile

- [ ] **Step 4: Commit any remaining fixes**

```bash
git add -u
git commit -m "Fix remaining smart tile issues from full test run"
```
