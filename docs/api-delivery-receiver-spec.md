# PoracleNG API Delivery — Receiver Specification

**Status:** Draft for partner review
**Audience:** Third parties implementing an HTTP endpoint that receives Poracle alerts.

This document is the complete behavioural contract for an **API delivery receiver**. If you implement everything here, your endpoint will interoperate with PoracleNG with no further coordination. It describes *how your endpoint must behave* — the transport, the request envelope, the responses you must return, and the lifecycle of a message. The exact `payload` field list is agreed separately as a partner template pack (see §8).

Nothing in this document requires knowledge of Poracle's internals.

---

## 1. Overview

A Poracle operator configures your endpoint URL and a shared secret. When an alert matches a user or channel whose destination type is `api:user` / `api:channel`, Poracle renders the alert and **POSTs it to your endpoint**, wrapped in a stable JSON envelope.

There are three operations — **send**, **edit**, **delete** — all delivered as a `POST` to the same URL, discriminated by an `op` field. One HTTP request carries exactly one message. Poracle does not batch; you may batch internally.

---

## 2. Endpoint & authentication

- **One URL.** Every operation is a `POST` to the single URL the operator configured.
- **HTTPS** is expected in production.
- **Shared secret.** Every request carries the secret in a header (default `X-Poracle-Secret`). The operator may configure a different header and an optional value prefix — e.g. `Authorization: Bearer <secret>`. Use a constant-time comparison. Reject any request whose secret does not match with `401` or `403`.

### Request headers

| Header | Value |
|---|---|
| `Content-Type` | `application/json; charset=utf-8` |
| `X-Poracle-Secret` | The shared secret, verbatim (header name/prefix operator-configurable). |
| `X-Poracle-Op` | `send`, `edit`, or `delete` — mirrors the body field. |
| `X-Poracle-Message-Id` | Mirrors `message_id` in the body, so you can dedupe/log without parsing the body. |
| `User-Agent` | `PoracleNG/<version>` |

`X-Poracle-Signature` and `X-Poracle-Timestamp` are **reserved** for a future optional HMAC body signature. They are not sent today. **Ignore any `X-Poracle-*` header you don't recognise** rather than rejecting the request.

---

## 3. The envelope

All three operations share one JSON shape. Fields marked *omitted when empty* are absent entirely rather than sent as `null` or `""`.

### 3.1 `op: "send"`

```json
{
  "version": 1,
  "op": "send",
  "message_id": "7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f",
  "revision": 0,
  "sent_at": 1770000000,
  "alert_type": "pokemon",
  "template_type": "monster",
  "template_id": "diadem",
  "destination": { "id": "u-42", "type": "api:user", "name": "James", "language": "en" },
  "location": { "lat": 51.5074, "lon": -0.1278 },
  "expires_at": 1770001800,
  "lifecycle": { "clean": true, "editable": false },
  "in_reply_to": "a1b2c3d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
  "tracking_uids": [45, 46],
  "areas": ["london", "city"],
  "payload": { "…rendered alert content…" }
}
```

| Field | Type | Meaning |
|---|---|---|
| `version` | int | Envelope version. `1` today. **Reject major versions you don't understand** rather than guessing. Additive fields do not bump it. |
| `op` | string | `send` \| `edit` \| `delete`. |
| `message_id` | string | UUIDv4 identifying the **logical message** — constant across the send, every edit, and the delete. Always colon-free (`^[0-9a-f-]{36}$`). |
| `revision` | int | **Reserved — always `0` in this version.** A future version will make it monotonic (`0` for send, `1`,`2`,… for edits) as a richer idempotency key. Today it carries no information; do **not** use it to distinguish operations (see §5). |
| `sent_at` | int | Unix seconds when Poracle issued the request. |
| `alert_type` | string | The **source** alert kind (`pokemon`, `raid`, `quest`, …, or `system` — §6.1). Stable across a message's lifecycle. See §8. |
| `template_type` | string | Which template **type** rendered `payload` — usually equal to `alert_type`, but change events differ (e.g. `alert_type: "pokemon"` with `template_type: "monsterChanged"`). **Key your payload parsing on this field.** See §8. |
| `template_id` | string | Which template **pack** produced `payload` (e.g. `diadem`); lets you switch on operator template variants. |
| `destination.id` | string | The stable destination identifier the operator assigned. `^[A-Za-z0-9._~-]{1,128}$` (colon-free). |
| `destination.type` | string | `api:user` or `api:channel`. |
| `destination.name` | string | Display label. |
| `destination.language` | string | Locale `payload` was rendered in. |
| `location` | object | `{lat, lon}`. *Omitted when the alert has no location.* |
| `expires_at` | int | Unix seconds when the alert stops being relevant (despawn, raid end, quest expiry). *Omitted when none.* |
| `lifecycle.clean` | bool | `true` ⇒ Poracle intends to send an `op:"delete"` near `expires_at` (§6). |
| `lifecycle.editable` | bool | `true` ⇒ Poracle may send `op:"edit"` for this message. |
| `in_reply_to` | string | *Omitted when empty.* The identifier of an earlier message to the same destination that this one continues (e.g. egg → raid on the same gym). Its value is the `id` you returned for that earlier message, or that message's `message_id` if you returned none. |
| `tracking_uids` | int[] | *Omitted when empty.* IDs of the tracking rules that matched — stable handles you can use to offer "stop tracking this" via the management API (§7). |
| `areas` | string[] | *Omitted when empty.* Geofence area names containing the alert. |
| `payload` | object | The rendered alert content (§8). Always a JSON object. |

> **Version note.** `in_reply_to`, `tracking_uids`, and `areas` are emitted whenever they have a value, following the *omitted when empty* rule above — build your receiver to read them today, not as a future capability. `revision` is the sole reserved field in this version: always `0`. A future version may make it monotonic as a richer idempotency key, but do not key on it today (see §3.1 and §5). The static map URL is not an envelope field at all — see §8, `payload.static_map`.

### 3.2 `op: "edit"`

Identical to `send`, plus `provider_message_id` (present only if you returned an `id` for the original send). `payload` is the **full replacement** content — edits are not patches. `message_id` is unchanged; `revision` stays `0` in this version.

### 3.3 `op: "delete"`

```json
{
  "version": 1,
  "op": "delete",
  "message_id": "7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f",
  "revision": 0,
  "provider_message_id": "abc123",
  "sent_at": 1770001800,
  "destination": { "id": "u-42", "type": "api:user", "name": "James", "language": "en" }
}
```

No `payload`, no `alert_type`, no `location`.

---

## 4. Responses you must return

Respond within ~10 seconds (the operator-configured timeout defaults to 10s).

### Success — return any 2xx

`200`, `201`, `202`, or `204`. `202` is the natural answer if you queue internally.

The body is optional. If you want Poracle to address later edits/deletes by **your** identifier, return:

```json
{ "id": "abc123" }
```

`id` must match `^[A-Za-z0-9._~-]{1,128}$` (**no colon**). An invalid `id` is ignored (Poracle then addresses the message by `message_id`). Any other response keys are ignored. If you have no identifier of your own, return an empty body or `{}` — `message_id` is guaranteed unique and stable, so edits/deletes still resolve.

### Failure — what each status makes Poracle do

| You return | Poracle does |
|---|---|
| `429` | Honours `Retry-After` (seconds), then retries. |
| `404` / `410` | Treats the destination as **permanently gone**: increments a consecutive-failure counter; after a threshold the destination is disabled. **On `op:"delete"` only, `404` means success** (already gone). |
| `401` / `403` | Logs an auth error and drops the message. Does **not** retry and does **not** count toward disabling — a wrong secret is an operator problem, not a per-destination one. |
| other `4xx` | Logs the body and drops. No retry, no failure count. Use this for a payload you can't process. |
| `5xx`, timeout, connection error | Retries with backoff (default 3 attempts), then drops. **Never counts toward disabling** — transient failures affect every destination on your endpoint equally, so they are treated as an endpoint problem, not a destination problem. |

**Important:** `404`/`410` are the **only** responses that escalate toward disabling a destination — return them only when a destination genuinely no longer exists. A sustained outage on your side (`5xx`/timeouts) drops messages after retries but never disables destinations; they resume automatically when your endpoint recovers.

**Edits referencing a message you no longer have** (expired your side, or the delete raced it): return `2xx` and ignore it (or apply it as a fresh message — your choice). Do **not** return `404` for an unknown *message* — `404` on an edit means the *destination* is gone and counts toward disabling it.

---

## 5. Idempotency & ordering

Poracle serialises **sends and edits** per destination: at most one send/edit per `destination.id` is in flight at a time, and they arrive in order. A retry of a failed request always completes before the next send/edit to that destination. **One exception:** an expiry-triggered `delete` (§6) fires from a separate timer and may arrive concurrently with an in-flight send/edit for the same destination — treat a delete as final (a stray edit arriving just after it falls under the unknown-message rule in §4). There is no ordering guarantee *across different* destinations. These guarantees are what make the rules below safe.

- **Network retries carry an identical body.** When Poracle retries a failed request (`5xx`, `429`, or a connection error) it re-sends the same `op` with a byte-identical body. Processing such a retry twice must be harmless. Deduplicating identical retries is optional.
- **`op:"send"`** uses a fresh `message_id` every time, so you may dedupe sends by `message_id`.
- **`op:"edit"`** replaces the message content in full (not a patch). **Apply every edit** — replacement is naturally idempotent, and per-destination ordering guarantees edits arrive in sequence, so "last edit wins" is always correct. Do **not** dedupe edits by `revision`; it is always `0` in this version (see §3.1).
- A `delete` may arrive for a `message_id` you never saw (e.g. the original send failed after Poracle recorded it). Respond `404` or `200` — both are safe.
- Store a mapping from `message_id` → your record so `edit`/`delete` resolve even when you returned no `id`.

---

## 6. Message lifecycle & expiry

`expires_at` is **advisory and authoritative for your own housekeeping** — it's present whenever the alert has a natural expiry, regardless of `lifecycle.clean`.

- `lifecycle.clean = false` — Poracle will never send a `delete`. Expire the message yourself on `expires_at` if you wish.
- `lifecycle.clean = true` — Poracle intends to send `op:"delete"` at `expires_at`. This is reliable in normal operation, **but treat `expires_at` as the backstop**: after a Poracle outage the delete might never arrive.

In short: act on `delete` when it comes; fall back to `expires_at` when it doesn't.

### 6.1 Delivery guarantees & system messages

Delivery is **best-effort, not guaranteed**. Two Poracle-side mechanisms can drop alerts before they reach you:

- **Alert limits.** Each destination has a message budget per time window (operator-configured; by default `api:user` draws the DM limit — 20 messages per ~4 minutes — and `api:channel` the channel limit, 40). Alerts over the limit are silently dropped until the window resets. If a destination tracks broadly, size its rules (or ask the operator for a limit override) accordingly.
- **Backpressure.** Under extreme load Poracle sheds work rather than queueing unboundedly.

**System messages.** Operational notifications — a rate-limit breach notice, a "destination disabled" farewell — are delivered as ordinary `op:"send"` envelopes with `alert_type: "system"` and the fixed payload shape:

```json
{ "content": "…human-readable notification text…" }
```

System messages never carry the partner-pack schema, are never edited or deleted, and bypass the alert limits (so the message telling you a destination was rate-limited cannot itself be rate-limited). Handle them however suits you — log them, surface them to the affected user, or ignore them — but do not reject them as malformed.

---

## 7. Managing destinations

Destinations (`api:user` / `api:channel`) and their tracking rules are created and managed **by the integrator over Poracle's v2 REST API**. There is no chat-command surface for API destinations.

**Note — this is a different credential.** Calls *to* Poracle's management API are authenticated with the operator's **API secret** (`[processor] api_secret`, sent as `X-Poracle-Secret` in your requests to Poracle). The secret Poracle presents *to your endpoint* on each delivery (`[api_delivery] secret`, §2) is a separate value — the two are only ever equal if the operator deliberately configures them that way. Ask the operator for both.

```
POST /api/v2/humans
{ "id": "u-42", "name": "James", "type": "api:user", "language": "en" }

POST /api/v2/humans/u-42/setLocation/51.5074/-0.1278
POST /api/v2/humans/u-42/tracking/pokemon
      [ { "pokemon_id": 25, "min_iv": 90, "distance": 5000, "clean": 1 } ]

GET  /api/v2/humans/u-42/tracking   # full snapshot: human, rules, profiles, locations, mutes
```

You choose `destination.id` at creation; it must match `^[A-Za-z0-9._~-]{1,128}$`. The `tracking_uids` in an alert envelope reference the rules from these calls, so you can build "stop tracking this" actions by calling the v2 tracking-delete endpoints.

---

## 8. Payload schema

`payload` is produced by a Handlebars template. Its exact field set is the **canonical schema agreed with you as a partner template pack** — one self-contained file per integration. The schema is `snake_case`, prefers machine values (unix timestamps, English enum arrays) over pre-formatted strings, and omits presentation-only fields (per-platform emoji, operator-specific map links). Extra keys may appear; **ignore keys you don't recognise**. A key is never removed or renamed without re-agreement.

**`alert_type`** identifies the *source* alert and is one of: `pokemon`, `raid`, `egg`, `quest`, `questSummary`, `invasion`, `incident`, `lure`, `nest`, `gym`, `fort-update`, `maxbattle`, `weatherchange` — plus `system` (§6.1). It stays constant across a message's lifecycle (an edited Pokémon alert is still `alert_type: "pokemon"`).

**`template_type`** identifies which template rendered `payload`, and is what selects the payload schema — **key your parsing on it**. It usually equals `alert_type`, with these exceptions: `monster` / `monsterNoIv` (a fresh Pokémon alert, with/without encounter stats), `monsterChanged` (a change event for an already-alerted Pokémon — the payload carries a `previous` object; `alert_type` stays `"pokemon"`), `rsvpChanges` (an RSVP update for a raid/egg; `alert_type` stays `"raid"`/`"egg"`), and `showcase` (a Showcase-flavoured incident; `alert_type` stays `"incident"`).

Every payload carries a common block (address, icon URL, map URLs, static map URL, time-remaining, sun times) plus per-type fields. The static map URL is the payload field `static_map`, alongside `icon_url` and `map_urls` — it is **not** an envelope field. It is present only when the template that produced this payload references it; a partner pack that never wants tiles simply omits `{{staticMap}}` from its templates and the key never appears. The full field-by-field schema is delivered with your partner pack and validated by an automated conformance test on the Poracle side, so what you receive always matches what was agreed.

**Scope of the conformance guarantee:** the automated test validates the *unmodified shipped pack*. An operator may copy the pack into their config directory and customise it — added keys are fine (ignore what you don't recognise), but a customised copy that removes or renames canonical keys is outside the guarantee; that constraint is part of the operator's agreement, not something Poracle enforces at runtime.

Poracle's own reference implementation of this contract is the `diadem` partner pack — shipped at `fallbacks/dts/diadem.toml` with `id="diadem"`, selected as the default via `[api_delivery] template="diadem"`. It ships 16 template entries — one per `template_type`: the 13 alert-sourced types above (with `monster` covering fresh Pokémon alerts) plus `monsterChanged`, `rsvpChanges`, and a dedicated `showcase` entry for Showcase-flavoured `incident` alerts. It is validated on every change by a real-enrichment conformance test (`processor/cmd/processor/api_pack_conformance_test.go`) that renders each entry and asserts the output matches this schema. Each additional partner integration follows the same pattern: its own self-contained `<partner>.toml` with `id="<partner>"`, coexisting with `diadem` in the same install.

*(The complete canonical field mapping lives in the PoracleNG design document `docs/superpowers/specs/2026-07-19-api-delivery-destination-design.md` §1.7; your partner pack is that schema in executable form.)*

---

## 9. Worked example

**Send** — a 100% Pikachu, on a rule with clean-deletion enabled:

```http
POST /your-endpoint HTTP/1.1
X-Poracle-Secret: s3cr3t
X-Poracle-Op: send
X-Poracle-Message-Id: 7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f
Content-Type: application/json; charset=utf-8

{"version":1,"op":"send","message_id":"7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f","revision":0,
 "sent_at":1770000000,"alert_type":"pokemon","template_type":"monster","template_id":"diadem",
 "destination":{"id":"u-42","type":"api:user","name":"James","language":"en"},
 "location":{"lat":51.5074,"lon":-0.1278},"expires_at":1770001800,
 "lifecycle":{"clean":true,"editable":false},
 "payload":{"pokemon_id":25,"name":"Pikachu","iv":100,"cp":842,"level":25}}
```

```http
HTTP/1.1 202 Accepted
Content-Type: application/json

{"id":"abc123"}
```

**Delete** — 30 minutes later at despawn:

```http
POST /your-endpoint HTTP/1.1
X-Poracle-Op: delete
X-Poracle-Message-Id: 7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f

{"version":1,"op":"delete","message_id":"7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f","revision":0,
 "provider_message_id":"abc123","sent_at":1770001800,
 "destination":{"id":"u-42","type":"api:user","name":"James","language":"en"}}
```

```http
HTTP/1.1 204 No Content
```

---

## 10. Implementation checklist

- [ ] One `POST` route, dispatching on `op`.
- [ ] Constant-time secret comparison; reject with `401`/`403`.
- [ ] Reject `version` values you don't understand.
- [ ] Apply every `edit` (full replacement); dedupe only identical network retries. Do not key on `revision` (always `0` today).
- [ ] Store `message_id → your record` so `edit`/`delete` resolve even when you return no `id`.
- [ ] Return `{"id": "..."}` only if it's colon-free and matches `^[A-Za-z0-9._~-]{1,128}$`.
- [ ] Return `2xx` fast; do rendering / fan-out asynchronously.
- [ ] Return `404`/`410` only when a destination is genuinely gone — never for transient failures (use `5xx`).
- [ ] Return `2xx` for an `edit` referencing an unknown message — never `404` (that escalates against the destination).
- [ ] Key payload parsing on `template_type` (not `alert_type` — change events reuse the source alert_type).
- [ ] Accept `alert_type: "system"` envelopes (`{"content": text}` payload) without treating them as malformed.
- [ ] Expire messages on `expires_at` as a backstop, even when `lifecycle.clean` is `true`.
- [ ] Treat `delete` for an unknown `message_id` as success.
- [ ] Ignore unrecognised envelope keys and `X-Poracle-*` headers.
