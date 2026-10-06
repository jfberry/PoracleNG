# Validation hook: per-user DTS fields

Status: proposed — open for comment
Date: 2026-10-06

## Summary

Extend the existing validation hook (`[validation] url`, see API.md → "Validation Hook") so that, in addition to approving or denying a matched user, the validator can return a set of **per-user fields** that become available to that user's DTS templates. The request sent to the validator also gains the user's effective language, current profile number, and the tracking-rule UID(s) that produced the match, so the validator has enough context to compute those fields.

Example: a validator returns `{"success": true, "fields": {"exampleUserSpecificField": "gold"}}` and the user's template renders `{{exampleUserSpecificField}}`.

The change is opt-in and additive. With no `[validation] url`, or a validator that never returns `fields`, behaviour is identical to today.

## Goals

- Validator can attach arbitrary JSON fields to an approved user.
- Fields are visible to DTS templates for **every** alert type that runs the hook, in both render paths (pokemon per-user render and the grouped render used by every other type).
- Fields survive into message snapshots, so button responses rendered at click time see the same values as the original alert.
- Validator receives enough per-user context (language, profile, rule UIDs) to compute fields without a second lookup back into Poracle.
- No change in render cost for users without fields.

## Non-goals (v1)

- Quest **summary digests** — validation still gates the digest, but returned fields are ignored for the digest render.
- `poracle-test`, `/api/dts/sendtest`, `/api/dts/render`, `/api/dts/enrich` do not call the validator; templates under test will not see validator fields. (Operators can put sample values in `dts_dictionary` while authoring — see "Precedence" below.)
- Translating or post-processing field values. Values are passed to the template as-is.
- Profile **name** in the request (only the profile number is cheaply available).
- Letting the validator override built-in fields (see "Precedence").

## Wire format

### Request (additions)

Existing fields are unchanged. New fields are additive; validators that ignore unknown keys keep working.

```json
{
  "human": {
    "id":         "123456789012345678",
    "type":       "discord:user",
    "name":       "Alice",
    "language":   "de",
    "profile_no": 2
  },
  "rule_uids":    [4512, 4519],
  "areas":        ["SanFrancisco/Downtown"],
  "webhook_type": "pokemon",
  "webhook":      { "...": "..." }
}
```

| Field | Type | Description |
|---|---|---|
| `human.language` | string | The **effective** language — the user's `humans.language`, falling back to `[general] locale`, then `en`. This is the same language the renderer uses to select the template, so a validator returning localised text should use it. |
| `human.profile_no` | int | The user's current profile number. Matchers only accept rules whose `profile_no` equals the human's current profile, so this is also the profile of every rule in `rule_uids`. Omitted (0) where there is no profile context. |
| `rule_uids` | int[] | Database UIDs of the tracking rules that produced this match. See "Rule UID semantics" below. May be empty. |

#### Rule UID semantics

- **pokemon**: every matching rule for the user (e.g. basic IV + great league + ultra league), because pokemon matching deliberately keeps one entry per rule and the hook is now called once per user (see "Call fan-out").
- **raid, egg, quest, invasion, lure, nest, gym, fort_update, max_battle, pokestop/showcase**: these matchers dedupe by user *inside the matcher*, so only the **first** matching rule's UID is retained. `rule_uids` has exactly one element. Collecting all UIDs for these types would require changing the matchers and is out of scope.
- **weather change** and **quest summary**: no tracking rule is involved; `rule_uids` is `[]`.

### Response (addition)

```json
{"success": true, "fields": {"exampleUserSpecificField": "gold", "exampleTier": 3}}
```

| Field | Type | Description |
|---|---|---|
| `fields` | object | Optional. Arbitrary JSON object. Keys become template field names; values may be any JSON type (strings, numbers, booleans, arrays, nested objects — raymond walks maps and slices as usual). |

Rules:

- `fields` is honoured only when `success` is `true`. On deny it is ignored.
- Fail-open and fail-closed outcomes (timeout, non-2xx, malformed JSON) carry **no** fields. Templates must therefore guard: `{{#if exampleUserSpecificField}}…{{/if}}`.
- A non-object `fields` value (e.g. a string or array) is treated as absent and logged at debug; it does not affect the allow/deny decision.
- An empty object is equivalent to absent.

## Design

### 1. Validation package (`internal/validation`)

- `Request.Human` gains `Language string` and `ProfileNo int` (`json:"profile_no,omitempty"`).
- `Request` gains `RuleUIDs []int64` (`json:"rule_uids"`, always serialised, `[]` when empty).
- `Response` gains `Fields map[string]any` (`json:"fields,omitempty"`). Decoding uses `json.RawMessage` for `fields` first so a wrong-typed value can be discarded without failing the whole decode.
- `Decision` gains `Fields map[string]any`, populated only on allow.
- `Noop` returns `Decision{Allow: true}` as today (nil fields).

### 2. Matcher output (`webhook.MatchedUser`)

- New `ProfileNo int` — set from `human.CurrentProfileNo` at each construction site: `matching/generic.go`, `matching/human.go` (×2), `matching/gym.go`, `cmd/processor/weather.go`, `cmd/processor/quest_summary_dispatch.go`. Test/synthetic sites may leave it 0.
- New `ValidationFields map[string]any` (`json:"-"`) — set by `filterValidation`, read by the renderer.

### 3. Call fan-out (`cmd/processor/helpers.go` `filterValidation`)

Today the hook is called **once per matched entry**. For pokemon, that means one call per matching rule for the same user. This changes to **once per unique user ID**:

1. Group `matched` by `ID`, preserving first-seen order.
2. Build one `Request` per group: `human` from the first entry, `language` via `effectiveLanguage(entry, cfg.General.Locale)`, `profile_no` from the entry, `rule_uids` = distinct non-zero `RuleUID`s across the group.
3. Fan out under the existing semaphore (`validation_max_concurrent`).
4. Apply each decision to every entry in its group: drop all on deny (one `failure_message` delivery per user, not per entry); on allow set `ValidationFields` on every entry to the same map.

The maps are shared read-only between entries and never mutated after assignment.

Side effect: fewer HTTP calls for pokemon. The existing `ValidationTotal` / `ValidationDuration` metrics now count per-user calls; this should be called out in the changelog.

### 4. Template lookup — precedence

A new `validation map[string]any` layer is added to `LayeredView` (`internal/dts/layered_view.go`). In `GetField` it is checked **after raw webhook fields and before `dts_dictionary`**:

```
original (special) → perUser → emoji → localOverrides → perLang → computed
  → base → aliases → webhook → validation (new) → dtsDict
```

Consequences:

- **Every Poracle-provided field wins** — a validator returning `"iv": 0` or `"name": "x"` cannot change `{{iv}}` or `{{name}}`. This guarantees a misbehaving validator cannot corrupt standard templates.
- A validator field **wins over `dts_dictionary`**. The dictionary is static operator config; the validator value is per-user and more specific. This also lets operators define a default for a validator field in `dts_dictionary` (used when the validator is down or omits the key) and while authoring templates in the editor/test paths.
- `resolveSource` (alias resolution) does **not** consult the validation layer — aliases map built-in names.

**Shadow diagnostics.** When a `LayeredView` is built with a non-empty validation layer, each validation key is checked against the higher-priority layers; if any of them resolves it, a debug log is emitted: `validation: field %q shadowed by built-in field, ignored`. Logging is deduplicated per key name for the process lifetime (a `sync.Map` of seen keys) so a validator that always returns a colliding key logs once, not per alert. Cost is O(fields) lookups per view and only applies when fields are present.

### 5. Rendering

**Pokemon path** (`Renderer.renderForUsers`): pass `user.ValidationFields` into the view constructor. No interaction with `PokemonPerUser` — the per-user enrichment map is untouched, so the nil-when-no-PVPDisplay case is irrelevant.

**Grouped path** (`Renderer.renderGrouped`, all non-pokemon types): users with the same `(template, platform, language, distanceTrack)` share one render. Per-user fields would make that share incorrect, so `renderGroupKey` gains `validationKey string`:

- empty string when the user has no fields (the common case — grouping is unchanged);
- otherwise a stable fingerprint of the fields: `encoding/json` marshal (which sorts map keys) hashed with FNV-64a, hex-encoded.

Users with identical fields still share a render. The group's view gets the group's (shared) fields map. The fingerprint is computed once per user per render batch; the cost only applies when fields are present.

`monsterChanged` and `rsvpChanges` go through the same two paths and need no special handling.

**Constructor signature.** `NewLayeredView` takes the fields map as an additional parameter (nil allowed). All existing call sites (renderer, button responses, test helpers) pass nil except the two above and the snapshot path.

### 6. Snapshots

- `snapshots.Snapshot` gains `ValidationFields map[string]any \`json:"validationFields,omitempty"\``. No `SchemaVersion` bump — older records simply decode with a nil map.
- `buildSnapshot` (`cmd/processor/render.go`) copies the fields from the delivered user's `MatchedUser`. `DeliveryJob` → snapshot already resolves the user by `Target`; the same lookup supplies `ValidationFields`.
- `Renderer.BuildLayeredViewFromSnapshot` passes `snap.ValidationFields` into the view.
- `Snapshot.Lookup` checks `ValidationFields` last (after `WebhookFields`), mirroring view precedence.

### 7. Quest summary

`quest_summary_dispatch.go` builds a single synthetic `MatchedUser` for validation. It now sets `Language` (already does), `ProfileNo`, and leaves `RuleUID` 0 (`rule_uids: []`). Returned fields are discarded (non-goal for v1); the allow/deny behaviour is unchanged.

## Error handling

| Situation | Behaviour |
|---|---|
| `fields` absent / empty / null | No validation layer; render identical to today. |
| `fields` not an object | Treated as absent, debug log, decision unaffected. |
| Validator unreachable (either fail mode) | No fields. |
| Field key collides with built-in | Built-in wins; one debug log per key name. |
| Large payloads | No explicit cap in v1. Fields are held per matched user only for the life of the render job, and in snapshots (which already store full enrichment). Documented as "keep it small". |

## Documentation

- API.md → "Validation Hook": new request fields table rows, `rule_uids` semantics, response `fields`, precedence, the `dts_dictionary` default trick, a worked example extending the existing paid-tier pseudocode.
- `config/config.example.toml` `[validation]` comment: mention `fields`.
- CLAUDE.md: one paragraph in the "LayeredView" section listing the new layer, and a note in the webhook flow where validation runs.
- Changelog: per-user (not per-entry) validator call count for pokemon.

## Testing

- `validation`: decode `fields` on allow; ignore on deny; non-object `fields` → absent and still allowed; request JSON contains `language`, `profile_no`, `rule_uids` (and `rule_uids: []` when empty).
- `filterValidation`: three pokemon entries for one user → one validator call, request carries all three rule UIDs, all three entries get the same fields; deny drops all three and sends one `failure_message`; mixed users keep first-seen order.
- `LayeredView`: validation field resolves; built-in key wins over validation; validation wins over `dts_dictionary`; shadow log emitted once per key.
- `renderGrouped`: two users identical except fields → two renders; identical fields → one render; users with no fields grouped as before.
- `renderForUsers`: pokemon user sees `{{exampleUserSpecificField}}`.
- Snapshot round-trip: fields written, read back, visible in `BuildLayeredViewFromSnapshot` and `Lookup`; old record without the key decodes.
- Matchers: `ProfileNo` populated from `CurrentProfileNo`.

## Open questions for comment

1. **Precedence** — is "built-ins always win" right, or do operators want a way to deliberately override a built-in (e.g. rename a channel-specific `name`)? The proposed layering leaves room to add an explicit opt-in later.
2. **Rule UIDs for non-pokemon types** — is "first matching rule only" acceptable, or is the full set needed badly enough to change the per-type matchers' dedup?
3. **Quest summaries** — should v1 also apply fields to the digest render?
4. **Payload cap** — should there be a hard limit on `fields` size/key count, rejecting oversize responses?
