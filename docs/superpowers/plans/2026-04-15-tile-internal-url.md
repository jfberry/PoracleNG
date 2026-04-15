# Tile internal URL + per-batch prefetch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the processor from going through Cloudflare for its own tileserver calls, and stop the N-downloads-per-event waste that happens when an event renders to N Discord destinations with `uploadEmbedImages` on plus at least one Telegram destination. Introduce an optional `[staticmap] internal_url` that the processor uses for all its own tileserver HTTP (render, pregenerate lookup, upload-images pre-fetch). Fetch bytes **once per render batch** when needed, not once per destination.

**Architecture:** Today `tileMode()` returns `Skip` / `URL` / `Inline`. Add a fourth mode `URLWithBytes` for mixed batches where some destinations need a fetchable URL (Telegram, upload-off Discord) and some destinations would benefit from pre-fetched bytes (Discord with `uploadEmbedImages=true`). In this mode the tileserver is hit twice: pregenerate to obtain the public URL (which goes in the message text), then a single internal GET via `internal_url` to fetch the bytes (which ride on the render batch and attach to every `delivery.Job`). Delivery's existing `len(StaticMapData) > 0` short-circuit in `discord.go:185`, `:227` already does the right thing — Discord-upload jobs consume the bytes, Telegram jobs ignore them, upload-off Discord jobs also ignore them (because `NormalizeAndExtractImage` returns empty `imageURL` when `uploadImages=false`).

**Tech Stack:** Go 1.22+. No new dependencies.

## File Structure

Files modified:
- `processor/internal/config/config.go` — add `InternalURL` field to staticmap config section
- `config/config.example.toml` — document `internal_url`
- `processor/internal/api/config_schema.go` — add editor metadata
- `processor/internal/staticmap/staticmap.go` — add `Config.InternalURL`, default handling, new `SubmitTileBoth` method + worker path
- `processor/internal/enrichment/tile_mode.go` (or wherever `TileModeSkip/URL/Inline` is defined) — add `TileModeURLWithBytes` constant
- `processor/cmd/processor/tilemode.go` — return the new mode when conditions are met
- `processor/cmd/processor/render.go` — handle the new mode: wait for both URL and bytes, apply URL to enrichment, attach bytes to every job in the batch
- `processor/cmd/processor/pokemon.go`, `raid.go`, `fort.go`, `gym.go`, `lure.go`, `nest.go`, `quest.go`, `invasion.go`, `maxbattle.go`, `weather.go` — each webhook handler that calls `ps.tileMode()` and branches on the result needs to route `URLWithBytes` to the new `SubmitTileBoth` path
- `processor/cmd/processor/main.go` — pass `InternalURL` into `staticmap.Config`
- `CLAUDE.md` — document the new mode and internal_url

Reference for understanding:
- `staticmap/staticmap.go:107-127` — `TilePending` struct with `Result chan string` (URL) and `ResultImg chan []byte` (inline).
- `staticmap/staticmap.go:278-320` — worker loop with `SubmitTileInline` and `Inline` branch; the URLWithBytes worker is a sibling.
- `cmd/processor/render.go:48-82` — render worker's tile-resolution `select`; add a URLWithBytes branch that reads from both channels.
- `cmd/processor/render.go:139` — job construction sets `StaticMapData: job.TileImageData`.
- `delivery/discord.go:185`, `:227` — existing `len(staticMapData) > 0 && imageURL != ""` short-circuit.

Mode decision matrix for reference:

| matched destinations | `uploadEmbedImages` | mode returned |
|---|---|---|
| no template uses staticMap | any | `Skip` |
| all Discord, all support upload-inline (uploadEmbedImages=true, no Telegram) | true | `Inline` |
| all Telegram, or any upload-off Discord with no upload-on Discord | any | `URL` |
| at least one URL-needer AND at least one Discord with uploadEmbedImages=true | true | `URLWithBytes` *(NEW)* |
| everyone is either Telegram or upload-off Discord | false | `URL` |

---

## Task 1: Add `internal_url` to staticmap config

**Files:**
- Modify: `processor/internal/config/config.go`
- Modify: `config/config.example.toml`
- Modify: `processor/internal/api/config_schema.go`

- [ ] **Step 1: Add field to StaticMapConfig struct**

In `processor/internal/config/config.go`, find the staticmap config section (has `tileserver_url`, `tileserver_timeout`, etc.) and add:

```go
InternalURL string `toml:"internal_url"` // optional private URL for processor→tileserver HTTP; defaults to tileserver_url
```

- [ ] **Step 2: Document in config.example.toml**

Add after `tileserver_url` in `[staticmap]`:

```toml
# internal_url = "http://tileserver.internal:8080"  # optional: URL the processor uses for its own tileserver calls (render, pregenerate lookup, upload-images pre-fetch). If unset, tileserver_url is used. Useful when tileserver_url points to a public HTTPS endpoint (e.g. behind Cloudflare) but the processor can reach the tileserver directly on a private network.
```

- [ ] **Step 3: Add to config-editor schema**

In `processor/internal/api/config_schema.go`, near the existing `tileserver_url` entry, add:

```go
{Name: "internal_url", Type: "string", Default: "", Description: "Private URL the processor uses for its own tileserver calls (render, pregenerate fetch, upload-images pre-fetch). Leave empty to reuse tileserver_url.", Advanced: true},
```

- [ ] **Step 4: Verify build**

```bash
cd processor && go build ./...
```
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/config/ processor/internal/api/config_schema.go config/config.example.toml
git commit -m "Add [staticmap] internal_url config option"
```

---

## Task 2: Plumb InternalURL into staticmap.Config

**Files:**
- Modify: `processor/internal/staticmap/staticmap.go`
- Modify: `processor/cmd/processor/main.go`

- [ ] **Step 1: Add `InternalURL` to staticmap.Config**

In `processor/internal/staticmap/staticmap.go`, find the `Config` struct (line 50ish, has `ProviderURL`) and add a field:

```go
// InternalURL is the URL the processor uses for its own HTTP calls to the
// tileserver (render, pregenerate fetch, upload-images pre-fetch). Set when
// ProviderURL is public (e.g. Cloudflare-fronted) and there is a direct path.
// Empty = use ProviderURL for everything.
InternalURL string
```

In the `NewResolver` constructor, default it:

```go
if config.InternalURL == "" {
    config.InternalURL = config.ProviderURL
}
```

- [ ] **Step 2: Add an `internalBase()` accessor**

```go
// internalBase returns the base URL the processor uses for its own
// tileserver calls. Always non-empty after NewResolver.
func (r *Resolver) internalBase() string {
    return r.config.InternalURL
}
```

Do **not** change any existing call site yet — all existing code keeps using `r.config.ProviderURL`. Subsequent tasks flip the fetch call sites to `internalBase()`.

- [ ] **Step 3: Wire from main.go**

In `processor/cmd/processor/main.go`, find where the staticmap `Config` is built (grep for `staticmap.Config{` or `staticmap.NewResolver`). Add:

```go
InternalURL: cfg.StaticMap.InternalURL,
```

- [ ] **Step 4: Verify build + tests**

```bash
cd processor && go build ./... && go test ./internal/staticmap/...
```
Expected: pass (no behaviour change yet).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/staticmap/staticmap.go processor/cmd/processor/main.go
git commit -m "Plumb InternalURL into staticmap.Config (unused yet)"
```

---

## Task 3: Switch processor fetch call sites to `internalBase()`

**Files:**
- Modify: `processor/internal/staticmap/staticmap.go`

This retires Cloudflare from the processor's existing fetch paths immediately. Does not yet add the URLWithBytes mode — that's Task 5.

- [ ] **Step 1: Identify fetch call sites**

Run:
```bash
grep -n 'r\.config\.ProviderURL' processor/internal/staticmap/staticmap.go
```

Expected to find uses in:
- Inline request (line 730ish): `fmt.Sprintf("%s/%s/poracle-%s%s?nocache=true", r.config.ProviderURL, ...)` — processor POSTing to render an inline tile. **Swap to `r.internalBase()`**.
- Pregenerate request (line 619ish): `fmt.Sprintf("%s/%s/poracle-%s%s?%s", r.config.ProviderURL, ..., pregenQuery)` — processor POSTing to pregenerate. **Swap to `r.internalBase()`**.

Do **not** swap the pregenerate-URL-returned-to-caller site. Find it (around line 689): `tileURL := fmt.Sprintf("%s/%s/pregenerated/%s", r.config.ProviderURL, mapPath, result)`. This URL goes into the rendered message and needs to be the public URL. **Leave as `ProviderURL`.**

Also leave any code that builds URLs for inclusion in templates or for the DTS view (the `url.Parse(...)` at line 554 for distanceMap/locationMap routes) — those are URLs consumers fetch.

- [ ] **Step 2: Swap the two fetch call sites**

Replace `r.config.ProviderURL` with `r.internalBase()` at only the two POST-request locations (inline request and pregenerate request).

- [ ] **Step 3: Verify build + tests**

```bash
cd processor && go build ./... && go test ./internal/staticmap/...
```
Expected: pass. Existing tests use `ProviderURL` only; when `InternalURL` is empty, `internalBase()` returns `ProviderURL` so behaviour is unchanged.

- [ ] **Step 4: Add a test that verifies internal vs public URL routing**

In `processor/internal/staticmap/staticmap_test.go`, add:

```go
func TestInternalURLUsedForFetchButNotForPublicURL(t *testing.T) {
    // Spin up two test servers. Inline request should land on internalSrv;
    // pregenerate-URL returned to caller should use publicSrv.
    var internalHits, publicHits atomic.Int32
    internalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        internalHits.Add(1)
        w.Header().Set("Content-Type", "image/png")
        w.Write([]byte("PNG"))
    }))
    defer internalSrv.Close()
    publicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        publicHits.Add(1)
        w.WriteHeader(http.StatusOK)
    }))
    defer publicSrv.Close()

    r, err := NewResolver(Config{
        ProviderURL:         publicSrv.URL,
        InternalURL:         internalSrv.URL,
        TileserverTimeout:   5000,
        TileserverConcurrency: 1,
        TileQueueSize:       10,
    })
    if err != nil { t.Fatalf("NewResolver: %v", err) }
    defer r.Close()

    pending := r.SubmitTileInline("monster", map[string]any{}, "staticmap")
    select {
    case img := <-pending.ResultImg:
        if len(img) == 0 { t.Fatal("expected inline bytes, got none") }
    case <-time.After(2 * time.Second):
        t.Fatal("timeout waiting for inline tile")
    }

    if internalHits.Load() != 1 {
        t.Fatalf("expected 1 internal hit, got %d", internalHits.Load())
    }
    if publicHits.Load() != 0 {
        t.Fatalf("expected 0 public hits for inline fetch, got %d", publicHits.Load())
    }
}
```

- [ ] **Step 5: Commit**

```bash
git add processor/internal/staticmap/
git commit -m "Route processor tileserver fetches through InternalURL

Inline and pregenerate POST requests now use r.internalBase() so they
bypass any Cloudflare / public-facing proxy in front of the tileserver.
The URL returned to callers (embedded in rendered messages) still
uses ProviderURL so Telegram/Discord can fetch it.
When InternalURL is unset, internalBase() falls back to ProviderURL —
zero behaviour change for users who don't opt in."
```

---

## Task 4: Add `TileModeURLWithBytes` constant and update `tileMode()`

**Files:**
- Modify: `processor/internal/enrichment/*.go` (wherever `TileModeSkip`/`URL`/`Inline` are defined)
- Modify: `processor/cmd/processor/tilemode.go`

- [ ] **Step 1: Find the existing TileMode constants**

```bash
grep -rn 'TileModeSkip\|TileModeURL\|TileModeInline' processor/internal/enrichment/ | head -5
```

- [ ] **Step 2: Add the new constant**

In the same file, after `TileModeInline`:

```go
// TileModeURLWithBytes is like TileModeURL for the public URL embedded in
// the rendered message, but also pre-fetches the tile bytes once via the
// staticmap internal URL so Discord-upload-images destinations in the same
// batch don't each re-download the image. Used when the batch mixes
// URL-needer destinations (Telegram, upload-off Discord) with Discord
// destinations that have uploadEmbedImages=true.
TileModeURLWithBytes = 3
```

- [ ] **Step 3: Update tileMode() in cmd/processor/tilemode.go**

Change the function to track both `anyURLNeeder` and `anyDiscordUpload` (the latter is a destination whose `canUploadInline` returns true):

```go
func (ps *ProcessorService) tileMode(templateType string, matched []webhook.MatchedUser) int {
    if ps.dtsRenderer == nil {
        return enrichment.TileModeSkip
    }
    ts := ps.dtsRenderer.Templates()

    var anyNeedsTile, anyURLNeeder, anyDiscordUpload bool

    for _, u := range matched {
        tmplID := u.Template
        if tmplID == "" {
            tmplID = ps.dtsRenderer.ResolveTemplate("")
        }
        lang := u.Language
        if lang == "" {
            lang = ps.cfg.General.Locale
        }
        platform := delivery.PlatformFromType(u.Type)
        if !ts.UsesTile(templateType, platform, tmplID, lang) {
            continue
        }
        anyNeedsTile = true
        if canUploadInline(u.Type, ps.cfg.Discord.UploadEmbedImages) {
            anyDiscordUpload = true
        } else {
            anyURLNeeder = true
        }
    }

    switch {
    case !anyNeedsTile:
        metrics.TileModeTotal.WithLabelValues("skip").Inc()
        return enrichment.TileModeSkip
    case anyURLNeeder && anyDiscordUpload:
        metrics.TileModeTotal.WithLabelValues("url_with_bytes").Inc()
        return enrichment.TileModeURLWithBytes
    case anyURLNeeder:
        metrics.TileModeTotal.WithLabelValues("url").Inc()
        return enrichment.TileModeURL
    default:
        metrics.TileModeTotal.WithLabelValues("inline").Inc()
        return enrichment.TileModeInline
    }
}
```

- [ ] **Step 4: Verify build**

```bash
cd processor && go build ./...
```
Expected: clean. Downstream callers of `tileMode()` will now need to handle the new value — subsequent tasks address each webhook handler. For now, nothing returns `URLWithBytes` yet in production (only when mixed batch hits the handler).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/enrichment/ processor/cmd/processor/tilemode.go
git commit -m "Add TileModeURLWithBytes mode decision

Recognises mixed batches: any Telegram/upload-off destination plus any
Discord destination with uploadEmbedImages=true. Nothing consumes the
new mode yet — subsequent commits wire it through the tile resolver
and the webhook handlers."
```

---

## Task 5: Add `SubmitTileBoth` to the staticmap Resolver

**Files:**
- Modify: `processor/internal/staticmap/staticmap.go`

Submits a pregenerate request (public URL for the message) and, on success, issues a single internal GET to download the bytes. Both results ride on the same `TilePending`.

- [ ] **Step 1: Understand the existing shape**

Read `processor/internal/staticmap/staticmap.go:107-127` (the `TilePending` struct). It already has both `Result chan string` and `ResultImg chan []byte` — the URLWithBytes mode uses both; existing URL-only and inline-only modes use exactly one each.

Read `processor/internal/staticmap/staticmap.go:220-320` (the worker loop). Existing branches are `req.pending.Inline` (inline path) vs the default URL path. Add a third branch for URLWithBytes.

- [ ] **Step 2: Add a mode enum to the internal tileRequest**

```go
type tileRequestMode int

const (
    tileRequestURL tileRequestMode = iota
    tileRequestInline
    tileRequestBoth
)
```

And on the `tileRequest` struct:

```go
mode tileRequestMode
```

Replace the existing `Inline` boolean branch logic with a switch on `mode`. Treat `tileRequestURL` and `tileRequestInline` the same as today; add `tileRequestBoth`.

- [ ] **Step 3: Add `SubmitTileBoth` public method**

Near the existing `SubmitTile` / `SubmitTileInline`:

```go
// SubmitTileBoth queues a pregenerate request for the public URL AND
// fetches the tile bytes once via the internal URL. Both channels on
// the returned TilePending receive a value — Result first (URL), then
// ResultImg (bytes). On any failure, the failed channel receives the
// zero value (empty string for URL, nil for bytes) and Fallback is used.
func (r *Resolver) SubmitTileBoth(maptype string, data map[string]any, staticMapType string, target map[string]any) *TilePending {
    pending := &TilePending{
        Result:    make(chan string, 1),
        ResultImg: make(chan []byte, 1),
        Both:      true,
        Deadline:  time.Now().Add(time.Duration(r.config.TileserverTimeout) * time.Millisecond),
        Fallback:  r.config.FallbackURL,
        target:    target,
    }

    select {
    case r.tileQueue <- tileRequest{
        maptype:       maptype,
        data:          data,
        staticMapType: staticMapType,
        pending:       pending,
        mode:          tileRequestBoth,
    }:
    default:
        pending.Result <- r.config.FallbackURL
        pending.ResultImg <- nil
    }
    return pending
}
```

Add `Both bool` to the `TilePending` struct so render.go can detect the mode.

- [ ] **Step 4: Implement the worker branch**

In the worker loop's switch on `req.mode`:

```go
case tileRequestBoth:
    // Step 1: pregenerate to get the public URL.
    publicURL := r.generatePregenerateURL(req.maptype, req.data, req.staticMapType)
    if publicURL == "" {
        req.pending.Result <- req.pending.Fallback
        req.pending.ResultImg <- nil
        continue
    }
    req.pending.Result <- publicURL

    // Step 2: fetch the bytes via internal_url using the same path.
    // publicURL has form "{ProviderURL}/staticmap/pregenerated/{id}".
    // Rewrite the base to internalBase() for the fetch.
    fetchURL := strings.Replace(publicURL, r.config.ProviderURL, r.internalBase(), 1)
    imgBytes := r.downloadTileBytes(fetchURL)
    req.pending.ResultImg <- imgBytes // may be nil — render.go tolerates nil
```

Extract the pregenerate logic into `generatePregenerateURL` (returns string, empty on failure) and the download into `downloadTileBytes(url string) []byte` (returns nil on failure). These are refactor extracts from the existing inline/pregenerate paths.

- [ ] **Step 5: Test**

Add a test mirroring Task 3's pattern but for `SubmitTileBoth`:

- Internal server returns PNG bytes for pregenerated path.
- Public server handles pregenerate request and returns a URL pointing to `publicSrv.URL/...`.
- Assert: `Result` channel receives a URL containing `publicSrv.URL`; `ResultImg` channel receives non-empty bytes; internal server receives exactly one hit for the bytes download.

- [ ] **Step 6: Build + tests**

```bash
cd processor && go build ./... && go test ./internal/staticmap/...
```
Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/staticmap/
git commit -m "Add Resolver.SubmitTileBoth for URLWithBytes mode

Pregenerates the public URL (via ProviderURL, embedded in message text
for Telegram / upload-off Discord), then downloads the bytes once via
InternalURL (attached to the render batch, consumed by any
Discord-upload destinations). Both results ride on the existing
TilePending plumbing."
```

---

## Task 6: Handle URLWithBytes in render.go

**Files:**
- Modify: `processor/cmd/processor/render.go`

- [ ] **Step 1: Read the current tile-resolution block (lines 48-82)**

Three modes handled today: non-nil `TilePending` without `Inline` (URL path), with `Inline` (bytes path), and a deadline fallback. Extend the `if job.TilePending.Inline` branch to also handle `job.TilePending.Both`.

- [ ] **Step 2: Add the URLWithBytes branch**

```go
if job.TilePending != nil {
    switch {
    case job.TilePending.Both:
        // Wait for both URL and bytes, each with its own deadline.
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
        select {
        case imgData := <-job.TilePending.ResultImg:
            job.TileImageData = imgData // may be nil — delivery tolerates
        case <-time.After(time.Until(job.TilePending.Deadline)):
            // bytes timed out — Discord-upload destinations fall back
            // to URL fetch (and log a download failure if that also fails).
        }
    case job.TilePending.Inline:
        // (existing code)
    default:
        // (existing URL-only code)
    }
}
```

- [ ] **Step 3: Verify bytes attach to every job in the batch**

`job.TileImageData` is already passed to every constructed `delivery.Job` at `render.go:139` via `StaticMapData: job.TileImageData`. No change needed — all per-user jobs in the batch inherit the same bytes. Discord-upload jobs consume them; Telegram and upload-off-Discord jobs ignore them.

- [ ] **Step 4: Build + tests**

```bash
cd processor && go build ./... && go test ./...
```
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add processor/cmd/processor/render.go
git commit -m "Handle TileModeURLWithBytes in render worker

Waits for both the public URL (applied to enrichment for template
rendering) and the prefetched bytes (attached to every Job in the
batch). Discord-upload destinations skip per-destination download
because delivery's short-circuit fires on non-empty StaticMapData."
```

---

## Task 7: Route URLWithBytes from webhook handlers to `SubmitTileBoth`

**Files:**
- Modify: `processor/cmd/processor/pokemon.go`, `raid.go`, `fort.go`, `gym.go`, `lure.go`, `nest.go`, `quest.go`, `invasion.go`, `maxbattle.go`, `weather.go`

Each handler has the pattern (example from pokemon.go):

```go
mode := ps.tileMode("monster", matched)
...
enrichmentData, tilePending := ps.enricher.Pokemon(..., mode)
```

The enricher's branching on `mode` needs a new branch for URLWithBytes that calls `r.SubmitTileBoth` instead of `r.SubmitTile` / `r.SubmitTileInline`.

- [ ] **Step 1: Find the enricher's tile-mode switch**

```bash
grep -rn 'TileModeInline\|TileModeURL\|SubmitTileInline\|SubmitTile\b' processor/internal/enrichment/ | head -20
```

Each enrichment entry-point (`enricher.Pokemon`, `.Raid`, etc.) has a `switch mode` that today handles Skip / URL / Inline. Add a URLWithBytes case calling `SubmitTileBoth`.

- [ ] **Step 2: Update one entry-point as a template**

Pick `enricher.Pokemon`. The existing `case enrichment.TileModeInline:` and `case enrichment.TileModeURL:` each submit to the resolver and return a `TilePending`. Add:

```go
case enrichment.TileModeURLWithBytes:
    pending := r.staticMap.SubmitTileBoth(maptype, data, staticMapType, target)
    return enrichmentData, pending
```

The returned `TilePending` is identical in shape to the existing modes — the `Both` flag is what `render.go` (Task 6) switches on.

- [ ] **Step 3: Repeat for every other enrichment entry-point that already handles Inline**

Each enrichment method that currently has Inline-vs-URL branching gets a URLWithBytes case that calls `SubmitTileBoth`.

- [ ] **Step 4: Build + full tests**

```bash
cd processor && go build ./... && go test ./...
```
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/enrichment/ processor/cmd/processor/
git commit -m "Route TileModeURLWithBytes through the enrichment layer

Every webhook-handler entry-point that previously branched on
TileModeInline/URL now also handles URLWithBytes by calling
Resolver.SubmitTileBoth. Behaviour is unchanged for users who haven't
set InternalURL and whose batches don't mix URL-needers with
Discord-upload destinations — the new branch only fires when tileMode()
returns URLWithBytes."
```

---

## Task 8: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (staticmap section)

- [ ] **Step 1: Add a short section describing the internal URL + URLWithBytes mode**

After the existing staticmap paragraph:

```markdown
### Tileserver: internal URL and URLWithBytes mode

`[staticmap] internal_url` is an optional private URL the processor uses for
its own HTTP calls to the tileserver (render, pregenerate fetch,
upload-images pre-fetch). It defaults to `provider_url`. Useful when
`provider_url` is a public HTTPS endpoint (e.g. Cloudflare-fronted) — the
public URL is still what appears in rendered messages (so Telegram/Discord
can fetch tiles), but the processor itself talks to the tileserver directly
over the private network, avoiding intermediary proxying and latency.

Tile modes (see `cmd/processor/tilemode.go`):
- `Skip` — template doesn't use `{{staticMap}}`.
- `Inline` — all destinations support upload-images; tile bytes flow through
  the render pipeline and attach directly to each delivery Job.
- `URL` — the tile URL is embedded in the message and each consumer fetches
  it themselves (Telegram always; Discord when uploadEmbedImages=false;
  Discord when uploadEmbedImages=true downloads once per destination).
- `URLWithBytes` — mixed batches where some destinations need the URL
  (Telegram / upload-off Discord) and some would benefit from prefetched
  bytes (Discord with uploadEmbedImages=true). The processor pregenerates
  to get the public URL for the message, then GETs the bytes once via
  internal_url. Bytes attach to every Job in the batch; delivery's
  existing `len(StaticMapData) > 0` short-circuit means upload-on Discord
  destinations skip their own fetch, while Telegram and upload-off Discord
  jobs harmlessly ignore the bytes.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "CLAUDE.md: document internal_url and TileModeURLWithBytes"
```

---

## Self-Review

Spec coverage:
- Internal fetch goes via `internal_url`: Tasks 2 and 3.
- Public URL still embedded in rendered message: unchanged (Task 3 explicitly leaves the `pregenerated/{id}` URL construction using ProviderURL).
- Bytes fetched once per batch rather than once per Discord destination: Task 5 issues exactly one internal GET; Task 6 attaches to every Job in the batch.
- Discord-upload destinations consume bytes, skip HTTP download: delivery's existing short-circuit (`discord.go:185`, `:227`) handles this without code changes.
- Telegram and upload-off Discord ignore bytes: confirmed by reading the existing delivery code (telegram.go doesn't reference StaticMapData; discord.go's multipart path requires both bytes AND imageURL, and imageURL comes back empty from `NormalizeAndExtractImage` when `uploadImages=false`).
- `internal_url` unset = current behaviour: Task 2 step 1 defaults to `ProviderURL`; Task 5's rewrite becomes a no-op.

Type consistency:
- `TilePending.Both` new boolean flag used in Task 5 and read in Task 6.
- `tileRequestMode` enum used in worker loop only; external API is still `SubmitTile` / `SubmitTileInline` / `SubmitTileBoth`.
- `TileModeURLWithBytes = 3` — verify the existing constants use sequential iota; if so, align.

Placeholder scan: every step has concrete code or exact commands. No "TBD".

Risk areas:
- The URL rewrite in Task 5's worker assumes `publicURL` starts with `ProviderURL` verbatim. This holds because the pregenerate code at `staticmap.go:689` builds the URL as `fmt.Sprintf("%s/%s/pregenerated/%s", r.config.ProviderURL, ...)`. If a future change introduces CDN-style path divergence between `ProviderURL` and `InternalURL`, the rewrite breaks silently (falls through to the fetch against the wrong host). Mitigation: the fetch failure will log a tile-download error and the affected Discord-upload destinations will fall back to fetching the public URL themselves (existing behaviour today) — degradation, not breakage.
- The bytes-timeout in Task 6 is separate from the URL-timeout. If bytes fetch hangs, URL still applied and Telegram/upload-off Discord jobs still deliver; upload-on Discord jobs get no bytes and fall back to per-destination URL fetch. This is strictly no worse than today.
