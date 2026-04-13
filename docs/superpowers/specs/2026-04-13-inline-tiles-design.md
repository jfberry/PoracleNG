# Smart Tile Generation Design

## Goal

Optimise tile generation based on what matched users actually need. Three tiers:
1. **Skip** — no template references staticMap → don't generate a tile at all
2. **Inline** — all users can accept bytes → POST without pregenerate (no disk, no re-download)
3. **URL** — at least one user needs a fetchable URL → current pregenerate flow

## Current Flow

```
Enrichment: always POST ?pregenerate=true → tileserver stores file, returns URL
Render:     wait for URL, bake into DTS template as {{staticMap}}
Delivery:   Discord: download image from URL, upload as attachment (if uploadImages=true)
            Telegram: pass URL in embed/photo, Telegram servers fetch it
```

**Problems:**
- Tile generated even when no template uses it (Telegram users with built-in map)
- Tileserver stores every tile on disk (even with TTL, disk fills during spikes)
- Discord upload mode downloads the same image we just asked the tileserver to store
- Two HTTP requests (pregenerate + download) when one would suffice

## Proposed Flow

```
Enrichment: check matched users' templates → decide tile mode (skip/inline/URL)
            If skip:    don't call tileserver at all
            If inline:  POST without pregenerate → receive PNG bytes
            If URL:     POST ?pregenerate=true → receive URL (current flow)
Render:     skip:   staticMap empty, no tile to resolve
            inline: set staticMap to placeholder, carry bytes through
            URL:    current flow (bake URL into template)
Delivery:   skip:   no image processing
            inline: upload bytes directly (skip DownloadImage)
            URL:    current flow
```

## Tile Mode Decision

At enrichment time, after matching, we have all matched users with their platforms and templates. One pass determines the mode:

```go
const (
    tileModeSkip   = 0 // no template uses staticMap
    tileModeInline = 1 // all users can accept bytes
    tileModeURL    = 2 // at least one user needs a fetchable URL
)

func (ps *ProcessorService) tileMode(matched []webhook.MatchedUser) int {
    anyNeedsTile := false
    anyNeedsURL := false
    for _, u := range matched {
        if ps.templateUsesTile(u.Type, u.Template) {
            anyNeedsTile = true
            if !ps.canUploadInline(u.Type) {
                anyNeedsURL = true
            }
        }
    }
    if !anyNeedsTile { return tileModeSkip }
    if anyNeedsURL  { return tileModeURL }
    return tileModeInline
}
```

### templateUsesTile — resolved via the real template selection chain

The check must use the **same template resolution** as rendering — the 5-level selection chain with two-pass user-beats-fallback logic. A user's tracking rule has `Template` (e.g. `"1"`, `"Normal"`, or empty for default), and combined with their platform and language, `selectEntry` resolves the exact compiled template.

```go
// On TemplateStore:
func (ts *TemplateStore) UsesTile(templateType, platform, templateID, language string) bool {
    // Resolve using the same chain as Get() / selectEntry()
    tmpl := ts.Get(templateType, platform, templateID, language)
    if tmpl == nil {
        return true // conservative: unknown → assume needs tile
    }
    // Check cached result keyed by compiled template pointer
    return ts.tileUsageCache[tmpl]
}
```

The cache is keyed by the **compiled template pointer** (not by type/platform/id/language), because multiple input combinations can resolve to the same template via the fallback chain. The cache is populated lazily on first check, or at compile time when `Get()` compiles a new template.

The check itself is a simple substring scan of the template source for `staticMap` or `staticmap`. No AST parsing needed.

The webhook handler resolves each matched user's template individually:
```go
ps.dtsRenderer.Templates().UsesTile(templateType, platform, user.Template, user.Language)
```

Where `user.Template` is the tracking rule's template field (empty string → resolved to `default_template_name` by the renderer, so `UsesTile` must apply the same default resolution).

### canUploadInline — per platform

```go
func (ps *ProcessorService) canUploadInline(userType string) bool {
    platform := delivery.PlatformFromType(userType)
    switch platform {
    case "discord":
        return ps.cfg.Discord.UploadEmbedImages
    case "telegram":
        return false // Telegram always needs URL (sendPhoto uses URL, inline markdown needs fetchable URL)
    }
    return false
}
```

**Template scanning is required for all tiers.** A Telegram user whose template doesn't reference staticMap doesn't need a URL generated — the scan prevents unnecessary tile generation for users who never display the image. The scan result drives all three tiers, not just skip.

### Decision Matrix

| Matched users | Any template uses staticMap? | Tile mode |
|---|---|---|
| All Telegram, no staticMap in templates | No | **Skip** |
| All Telegram, some use staticMap | Yes, need URL | **URL** |
| All Discord + upload, some use staticMap | Yes, all can inline | **Inline** |
| All Discord + upload, none use staticMap | No | **Skip** |
| Mix Discord + Telegram, some use staticMap | Yes, need URL | **URL** |
| Mix, none use staticMap | No | **Skip** |
| Discord without upload | Yes, need URL | **URL** |

## Implementation

### 1. Template staticMap Detection (TemplateStore)

Add to `TemplateStore`:

```go
// usesTileCache maps "type/platform/templateID" → bool
// Built at load/reload time by checking if compiled template source contains staticMap.
usesTileCache map[string]bool
```

Computed during `Get()` or at `LoadTemplates`/`Reload` time. For templateFile entries, scan the raw file content. For inline templates, scan the JSON-stringified template.

Public method:
```go
func (ts *TemplateStore) UsesTile(templateType, platform, templateID string) bool
```

Falls back to `true` (conservative) if the template isn't found — ensures we don't skip tiles for unknown templates.

### 2. Tile Mode Helper (ProcessorService)

```go
func (ps *ProcessorService) tileMode(templateType string, matched []webhook.MatchedUser) int
```

Calls `UsesTile` for each user's resolved template. Calls `canUploadInline` for platform check. Returns `tileModeSkip`, `tileModeInline`, or `tileModeURL`.

### 3. Enricher Changes

`addStaticMap` gains a `tileMode int` parameter:

- `tileModeSkip` → return nil immediately, don't set staticMap
- `tileModeInline` → call new `SubmitTileInline` → returns `TilePending` with `Inline=true`
- `tileModeURL` → current flow (`SubmitTile`)

Each type-specific enricher (`Pokemon`, `Raid`, `Invasion`, etc.) passes `tileMode` through.

### 4. TilePending Enhancement

```go
type TilePending struct {
    Result    chan string  // URL mode: receives tile URL
    ResultImg chan []byte  // Inline mode: receives PNG bytes
    Inline    bool         // which mode was used
    Deadline  time.Time
    Fallback  string
    target    map[string]any
}

func (tp *TilePending) Apply(url string) {
    // existing: writes staticMap URL into enrichment
}

func (tp *TilePending) ApplyInline(data []byte) {
    // writes staticMap = "inline" marker into enrichment
    // stores bytes for delivery
}
```

### 5. Resolver Changes (staticmap.go)

New method:
```go
func (r *Resolver) GenerateInlineTile(maptype string, data map[string]any, staticMapType string) []byte
```

Same as `generatePregenTile` but:
- No `?pregenerate=true` query param
- Reads response body as `[]byte` (PNG image)
- Returns `[]byte` instead of URL string

Tile worker checks `req.pending.Inline` to choose which method to call:
```go
if req.pending.Inline {
    imgData := r.GenerateInlineTile(req.maptype, req.data, req.staticMapType)
    req.pending.ResultImg <- imgData
} else {
    url := r.generatePregenTile(req.maptype, req.data, req.staticMapType)
    req.pending.Result <- url
}
```

### 6. Render Worker Changes

`processRenderJob` tile resolution:

```go
if job.TilePending != nil {
    if job.TilePending.Inline {
        // Wait for image bytes
        select {
        case data := <-job.TilePending.ResultImg:
            job.TilePending.ApplyInline(data)
            job.TileImageData = data  // new field on RenderJob
        case <-time.After(deadline):
            // no image, proceed without
        }
    } else {
        // Current URL flow
        select {
        case url := <-job.TilePending.Result:
            job.TilePending.Apply(url)
        case <-time.After(deadline):
            job.TilePending.Apply(job.TilePending.Fallback)
        }
    }
}
```

### 7. DeliveryJob / delivery.Job

Add `StaticMapData []byte` to both:

```go
type DeliveryJob struct {
    // ... existing fields ...
    StaticMapData []byte `json:"-"` // inline tile bytes, not serialised
}

type Job struct {
    // ... existing fields ...
    StaticMapData []byte `json:"-"` // inline tile bytes
}
```

The renderer copies `RenderJob.TileImageData` → each `DeliveryJob.StaticMapData`. `processRenderJob` copies `DeliveryJob.StaticMapData` → `delivery.Job.StaticMapData`.

Note: all users in a render group share the same image bytes (same template renders the same tile). The `[]byte` slice header is copied, not the underlying data — no memory duplication per user.

### 8. Discord Sender Changes

In `postMessage` / `postWebhook`:

```go
// Check for inline tile data first
if len(job.StaticMapData) > 0 {
    normalized = ReplaceEmbedImageURL(normalized)
    buf, ct, _ := BuildMultipartMessage(normalized, job.StaticMapData, "files[0]")
    reqBody = buf
    contentType = ct
} else if imageURL != "" && ds.uploadImages {
    // Current flow: download and upload
    imageData, err := DownloadImage(ds.client, imageURL)
    ...
}
```

### 9. Webhook Handler Changes

Each handler (pokemon.go, raid.go, etc.) passes tile mode to the enricher:

```go
mode := ps.tileMode("monster", matched)
enrichment, tilePending := ps.enricher.Pokemon(&pokemon, processed, mode)
```

## Weather: Per-User Tiles

Weather change alerts are a special case. When `showAlteredPokemonStaticMap` is enabled, each user gets a **unique tile** showing their specific active pokemon at their locations. The weather handler already sends one RenderJob per user (not grouped), each with its own TilePending.

### How weather works today

1. **Base tile** (`showAlteredPokemonStaticMap=false`): generated once in `Enricher.Weather()`, shared across all users. Normal three-tier model applies.
2. **Per-user tile** (`showAlteredPokemonStaticMap=true`): generated inside `WeatherTranslate()` per language/user, with that user's `activePokemons` data baked into the tile. One tile per user, one RenderJob per user.

### Weather + tile mode

Since weather already dispatches per-user RenderJobs, the tile mode decision is per-user:

```go
for _, user := range matched {
    mode := ps.tileMode("weatherchange", []webhook.MatchedUser{user})
    // pass mode to WeatherTranslate for per-user tile
    // OR pass mode to base enrichment for shared tile
}
```

Per-user weather tiles are inherently expensive (one tileserver call per user). The skip tier is especially valuable here — if a user's weather template doesn't reference staticMap, we save the entire per-user tile generation.

## Tile Generation: One Per RenderJob, Not Per Render Group

**Important clarification:** tiles are generated once per webhook in enrichment (before rendering), not per render group. All render groups within the same RenderJob share the same enrichment map, so they share the same tile URL/bytes. The `TilePending.Apply` writes `staticMap` into the shared enrichment base map, and all render groups read from it.

```
One webhook → one enrichment → one tile → one RenderJob
    → render group A (discord/en/template1) → reads staticMap from shared enrichment
    → render group B (telegram/de/template2) → reads same staticMap
    → render group C (discord/en/template2) → reads same staticMap
```

There is **no duplicate tile generation** across render groups. The tile mode decision (skip/inline/URL) is made once per webhook, covering all matched users.

The only exception is weather with per-user tiles — each user gets their own RenderJob and their own tile.

## What Stays the Same

- URL mode is the default and fallback
- Telegram delivery unchanged (always URL mode for now)
- Non-tileservercache providers unchanged (google, osm, mapbox always return URLs)
- Template rendering unchanged (staticMap is still a field in the view)
- TTL feature works alongside (for URL mode tiles)
- Bot commands (!area show, !location) unchanged — they use synchronous `GetPregeneratedTileURL`

## Future: Telegram Optimisations

1. **Multipart sendPhoto** — upload bytes to Telegram instead of URL → enables inline mode for Telegram `photo` field templates
2. **Template-aware skip for inline images** — with multipart sendPhoto, the only case needing a URL is Telegram inline markdown pattern (`[\u200A]({{{staticMap}}})`). Template scanning would distinguish photo-only (inline safe) from inline-markdown (needs URL).

## Risks

- **Memory:** Image bytes (~50-200KB) travel through channels and are stored on the delivery.Job. For a pokemon webhook matching 100 users in one render group, only one copy of the bytes exists (shared slice). For weather per-user tiles, each user has their own ~100KB. Acceptable.
- **Fallback:** If inline tile generation fails, no image appears. Same as current URL-mode failure behaviour.
- **Conservative default:** Unknown templates default to `usesTile=true`. Unknown platforms default to `canUploadInline=false`. The system never skips a tile incorrectly — it may over-generate but never under-generate.
- **Weather cost:** Per-user weather tiles remain expensive regardless of tile mode. The skip tier helps when a user's template doesn't use staticMap. Inline mode avoids disk but doesn't reduce the number of tileserver calls.
