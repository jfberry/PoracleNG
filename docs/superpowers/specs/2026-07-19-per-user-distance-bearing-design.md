# Per-User Distance & Bearing for All Alert Types

**Date:** 2026-07-19
**Status:** Design approved, not yet implemented
**Relationship:** Prerequisite for `2026-07-19-api-delivery-destination-design.md`, but independently useful — it fixes a defect visible to every Discord and Telegram operator today.

---

## Problem

`{{distance}}`, `{{bearing}}` and `{{bearingEmoji}}` resolve to empty for every alert type except pokemon. A user tracking raids with `d:2000` cannot show "1.2 km away" in a raid template, though the same template works for pokemon.

The values are not merely dropped at render time — they are computed and then discarded.

## Cause

Three facts combine:

1. **The matcher computes them for every type.** `matching/generic.go:120-135` — `ValidateHumansGeneric` calls `Bearing(anchorLat, anchorLon, lat, lon)` and populates `MatchedUser.Distance`, `.Bearing`, `.CardinalDirection` and `.TrackDistance` on every match, regardless of alert type.

2. **Only the PVP path turns them into template fields.** `enrichment/peruser.go:61-64` writes `distance`, `bearing` and `bearingEmojiKey` into the per-user map. That function exists to build the per-user PVP display list, so it is called only for pokemon. For every other type `perUserEnrichment` is nil.

3. **Group rendering is predicated on their absence.** `dts/renderer.go:359` delegates to `renderGrouped` whenever `perUserEnrichment == nil`, on the stated reasoning that users sharing `(template, platform, language)` "get identical rendered output". `renderGrouped:594` then builds the per-user layer as `{"userDistanceTrack": key.distanceTrack}` — the flag, never the values.

The optimisation's correctness precondition is guaranteed by the very gap it causes. That circularity is why the bug survived: grouping is genuinely safe *today*, and only becomes unsafe once the fields are populated.

`CLAUDE.md` compounds the confusion by stating that in group rendering "only per-user fields (distance, bearing) are patched afterwards". No such patching exists. That line is corrected as part of this work.

## Approach

Mirror `TemplateStore.UsesTile`. That mechanism already exists for exactly this shape of problem — "does this template reference a field expensive enough that we should change our execution strategy?" — and is cached, source-based, and conservative on failure.

### 1. `TemplateStore.UsesPerUserFields(templateType, platform, templateID, language) bool`

Structurally identical to `UsesTile` (`dts/templates.go`): consult `perUserUsage` cache; on miss, force template resolution to populate `sourceCache`; scan the source; memoise. Returns `true` conservatively when the template cannot be resolved.

**The scan must match token forms, not bare substrings.** `strings.Contains(src, "distance")` false-positives on `userDistanceTrack` and `userTrackDistance` — both legitimate fields a template may use *without* needing per-user rendering. A template using only the track flag must keep grouping. Match the handlebars token forms: `{{distance}}`, `{{bearing}}`, `{{bearingEmoji}}`, and their `{{{...}}}` and subexpression variants. A small regexp over `\{\{\{?\s*#?\s*(distance|bearing|bearingEmoji)\b` is sufficient and is what the tests pin.

### 2. Populate per-user distance/bearing for all types

Add `enrichment.BuildPositionalPerUser(users []webhook.MatchedUser) map[string]map[string]any` returning `{distance, bearing, bearingEmojiKey}` keyed by user ID, sourced straight from `MatchedUser`. This is deliberately separate from `BuildPerUser` (the PVP path) so the two concerns don't tangle: PVP enrichment stays pokemon-only, positional enrichment applies to everything.

Each webhook handler passes the result as `perUserEnrichment` where it currently passes nil. For pokemon, the existing PVP per-user map already carries these fields and is unchanged.

### 3. Group only when safe

In `renderForUsers`, the grouping decision changes from "is `perUserEnrichment` nil" to "does this batch need per-user rendering":

- If no template in the batch references the per-user fields → group as today. Zero regression for the overwhelmingly common case.
- Otherwise → render per-user.

The decision is per `(template, platform, language)` group, not per batch, so a Discord template that uses `{{distance}}` does not force per-user rendering on a Telegram group whose template doesn't.

`userDistanceTrack` stays in the group key; it is orthogonal.

## Performance

Group rendering exists because a channel alert fanning out to many destinations otherwise re-renders identical output. That saving is preserved exactly for templates that don't reference the fields — the check is a cached boolean.

Templates that do reference them lose grouping, which is not a regression but the correct cost of correct output: the rendered bodies genuinely differ per user. Operators who care more about throughput than distance simply don't use the field, and the existing behaviour is what they get.

Worth noting the asymmetry: distance is most useful for personal DM tracking, which is per-user anyway; it is least useful for channel alerts, which are the case grouping optimises. The costs land where they matter least.

## Testing

- `UsesPerUserFields` returns `true` for `{{distance}}`, `{{bearing}}`, `{{bearingEmoji}}`, `{{{distance}}}` and subexpression uses; **`false`** for a template using only `userDistanceTrack` or `userTrackDistance`. This is the regression that would silently destroy grouping for existing templates.
- `false` for a template referencing neither; `true` when the template cannot be resolved.
- A raid render with two users at different locations produces two distinct bodies with the correct per-user distances, and — with a template that omits the fields — a single grouped render shared by both.
- `bearingEmojiKey` resolves to the correct cardinal emoji per user for a non-pokemon type.
- Pokemon rendering is unchanged: existing per-user PVP tests must pass untouched.
- Area-based rules (`TrackDistance == 0`) emit `distance = 0` rather than a spurious value.

## Out of scope

- Changing what the matcher computes. `MatchedUser` already carries everything needed.
- Reworking the anchor resolution order (per-rule override → profile location → account default). Unchanged.
- Adding distance to summary digests (`questSummary`), which aggregate many locations and have no single distance.

## Follow-up

Correct the group-rendering description in `CLAUDE.md` — both the false "patched afterwards" claim and, once this lands, the grouping precondition itself.
