# Inline Tile Generation Design

## Goal

Avoid storing tile images on the tileserver disk when all destinations can accept uploaded image bytes. When possible, POST to tileserver without `pregenerate=true`, receive image bytes in the response, and upload them directly to Discord/Telegram — zero disk usage, one fewer HTTP round-trip.

## Current Flow

```
Enrichment: POST ?pregenerate=true → tileserver stores file, returns URL
Render:     wait for URL, bake into DTS template as {{staticMap}}
Delivery:   Discord: download image from URL, upload as attachment (if uploadImages=true)
            Telegram: pass URL in embed/photo, Telegram servers fetch it
```

**Problems:**
- Tileserver stores every tile on disk (even with TTL, disk fills during spikes)
- Discord upload mode downloads the same image we just asked the tileserver to store
- Two HTTP requests (pregenerate + download) when one would suffice

## Proposed Flow

```
Enrichment: check matched users → decide tile mode (inline vs URL)
            If inline:  POST without pregenerate → receive PNG bytes
            If URL:     POST ?pregenerate=true → receive URL (current flow)
Render:     inline: set staticMap to placeholder, carry bytes through
            URL: current flow (bake URL into template)
Delivery:   inline: upload bytes directly (skip DownloadImage)
            URL: current flow
```

## When Can We Use Inline Mode?

A webhook fans out to N matched users. Inline mode is safe when **no** user needs a fetchable URL. A user needs a fetchable URL when:

1. **Discord without `upload_embed_images`** — the URL stays in the embed, Discord fetches it
2. **Telegram with inline image pattern** — `[\u200A]({{{staticMap}}})` in the template content, Telegram servers must fetch the URL
3. **Telegram `photo` field as URL** — current `sendPhoto` sends a URL, Telegram fetches it

Inline mode is safe when:
- **Discord with `upload_embed_images=true`** — always (bytes uploaded as attachment)
- **Telegram with `sendPhoto` multipart upload** — if we add multipart support (future)
- **Telegram without any staticMap reference in template** — no image to worry about

### DTS Template Classification

At template load time, classify each (type, platform) as `needsPublicURL`:

| Template pattern | needsPublicURL |
|-----------------|----------------|
| `{{{staticMap}}}` anywhere in template content/description strings | true |
| `embed.image.url` containing `{{{staticMap}}}` + Discord `upload_embed_images=true` | false (gets replaced with `attachment://map.png`) |
| `"photo": "{{{staticMap}}}"` (Telegram photo field) | true (until multipart sendPhoto) |
| No staticMap reference at all | false |

**Simplification for v1:** Since Telegram currently always needs a URL (both inline and photo), the check is:
- All matched users are Discord AND `upload_embed_images=true` → **inline mode**
- Any Telegram user OR Discord without upload → **URL mode**

This avoids template scanning entirely for v1. Template scanning becomes relevant when we add Telegram multipart sendPhoto.

## Implementation

### 1. Tile Mode Decision (enrichment time)

The webhook handler (pokemon.go, raid.go, etc.) already has `matched []webhook.MatchedUser` before calling the enricher. Add a helper:

```go
// canUseInlineTile returns true if all matched users can accept
// uploaded image bytes instead of a fetchable URL.
func (ps *ProcessorService) canUseInlineTile(matched []webhook.MatchedUser) bool {
    if !ps.cfg.Discord.UploadEmbedImages {
        return false
    }
    for _, u := range matched {
        if !strings.HasPrefix(u.Type, "discord:") && u.Type != "webhook" {
            return false // non-Discord user needs URL
        }
    }
    return true
}
```

### 2. TilePending Enhancement

Add image bytes support alongside the existing URL channel:

```go
type TilePending struct {
    Result    chan string     // URL mode: receives tile URL
    ResultImg chan []byte     // Inline mode: receives PNG bytes
    Inline    bool           // which mode was requested
    Deadline  time.Time
    Fallback  string
    target    map[string]any
}

func (tp *TilePending) Apply(url string) { ... }       // existing
func (tp *TilePending) ApplyInline(data []byte) { ... } // new: stores bytes
```

### 3. Resolver Changes (staticmap.go)

New method `GenerateInlineTile` — same as `generatePregenTile` but:
- No `?pregenerate=true` query param
- Response is PNG bytes, not a URL string
- Returns `[]byte` instead of `string`

The tile worker checks `req.pending.Inline` to choose which method to call.

### 4. Enricher Changes

`addStaticMap` gains an `inline bool` parameter. When inline:
- Calls `SubmitTileInline` instead of `SubmitTile`
- Sets `staticMap` in enrichment to a marker value (e.g. `"inline:pending"`)

### 5. RenderJob / Render Changes

`RenderJob` already carries `TilePending`. The render worker:
- URL mode: waits on `Result`, calls `Apply(url)` → writes URL into enrichment → template renders with URL
- Inline mode: waits on `ResultImg`, stores bytes on the render job → template renders with `staticMap` set to a placeholder or empty (delivery handles the image separately)

The rendered `Message` in `DeliveryJob` won't have a real staticMap URL. Instead, the image bytes need to travel alongside the message.

### 6. DeliveryJob / delivery.Job Enhancement

Add `StaticMapData []byte` to both structs:

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

The renderer sets `StaticMapData` on each `DeliveryJob` when inline mode is active. `processRenderJob` copies it to `delivery.Job`.

### 7. Discord Sender Changes

In `postMessage` / `postWebhook`:

```go
if len(job.StaticMapData) > 0 {
    // Skip DownloadImage — we already have the bytes
    normalized = ReplaceEmbedImageURL(normalized)
    buf, ct, _ := BuildMultipartMessage(normalized, job.StaticMapData, "files[0]")
    reqBody = buf
    contentType = ct
} else if imageURL != "" {
    // Current flow: download and upload
}
```

### 8. Webhook Handler Changes

Each handler passes the inline decision to the enricher:

```go
inline := ps.canUseInlineTile(matched)
enrichment, tilePending := ps.enricher.Pokemon(&pokemon, processed, inline)
```

## What Stays the Same

- URL mode is the default and fallback — inline is an optimisation
- Telegram delivery unchanged (always needs URL for now)
- Template rendering unchanged (staticMap is still a field in the view)
- Non-tileservercache providers (google, osm, mapbox) unchanged
- The TTL feature works alongside this (for URL mode tiles)

## Future: Telegram Multipart sendPhoto

To extend inline mode to Telegram:
1. Add multipart support to `sendPhoto` (send file data instead of URL)
2. Classify templates at load time — scan for `{{{staticMap}}}` in markdown link patterns
3. If template uses `photo` field only (not inline markdown), inline mode is safe with multipart sendPhoto
4. Update `canUseInlineTile` to accept Telegram users whose templates are photo-only

## Risks

- **Shared tiles:** If the same webhook type generates the same tile for multiple render groups (different templates), inline mode generates the tile twice (once per group) while URL mode generates once and reuses. Mitigated: the tile data is identical, and avoiding disk + download offsets the extra render.
- **Memory pressure:** Image bytes (~50-200KB per tile) travel through channels and structs. For high-volume webhooks with many matched users, this adds memory. Mitigated: bytes are shared across all users in the same render group (not copied per user).
- **Fallback:** If the tileserver returns an error in inline mode, there's no fallback URL. The image simply won't appear. Same as current behaviour when tile generation fails.
