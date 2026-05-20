# Buttons & Snapshots & TOML DTS — Design

> **Status:** Settled. Implementation tracked in `IMPLEMENTATION.md`. Full design rationale is in the GitHub issues; this document is a navigation aid.

## Design issues

The three issues capture decision history, alternatives considered, and the reasoning for every meaningful choice. Read these before opening `IMPLEMENTATION.md`:

- **[#108 Snapshot enrichment store](https://github.com/jfberry/PoracleNG/issues/108)** — per-delivery resolved-view storage, opt-in via config, pogreb-backed, JSON-serialised, schema-versioned. Underpins everything else.
- **[#109 Buttons + `!mute` commands](https://github.com/jfberry/PoracleNG/issues/109)** — Discord button components on alerts, `!mute` / `!unmute` command surface with `!tracked` integration, in-memory mute table with `filterMuted` matcher step, action handler registry (mute / unsubscribe / redeliver / render), three response template shapes, action-level `applies_to` defaults, click-time `visible_to` gating.
- **[#110 TOML DTS format](https://github.com/jfberry/PoracleNG/issues/110)** — TOML as an alternative authoring format alongside JSON, template body as a single multi-line string, structured TOML tables for metadata and buttons. Config editor round-trips JSON internally and preserves on-disk format with pre-write backup.

## Key decisions locked

These appear across the three issues; collected here so the implementation phase doesn't have to re-derive them.

1. **Snapshots are opt-in** (`[snapshots] enabled = false`). With snapshots off, the render path skips Discord `components` entirely — buttons declared in DTS are silently inert. Operators can keep button definitions in their templates and turn them on later by flipping the snapshot knob.

2. **Per-delivery snapshots**, not per-event. One snapshot per delivered message (per user, per channel). Self-contained, trivially readable, simpler than reconstructing views from raw layers.

3. **Edits write new snapshots.** Each edit re-renders through the pipeline; the new template's buttons replace the old via Discord's `components` field; a fresh snapshot overwrites the previous. Snapshots are always "what the user currently sees." Edit code in `delivery/discord.go` must thread `components` through edits.

4. **In-memory mute table**, lost on restart. Mutes are short-lived; restart frequency is low; users re-apply via command or button if needed.

5. **`filterMuted` runs between `filterValidation` and render.** Per matched user, per matched UID. One `MuteEntry` per UID for tracking-scope mutes.

6. **Profile axis is orthogonal to mute scope.** Entity mutes (`gym`, `pokestop`, `station`, `pokemon`, `area`, `everything`) apply across all profiles; UID-scoped mutes are naturally profile-scoped because UIDs are unique per `(human_id, profile_no, rule)`.

   *Vocabulary note:* the scope noun `pokemon` (was `species` in earlier drafts) and `everything` (was `user`) match the existing command vocabulary for consistency. The conflict between tracking-rule mutes and pokemon-entity mutes is resolved by the `id:X` form — `!mute pokemon pikachu` is an entity mute, `!mute pokemon id:45` is a tracking-rule mute. UIDs are already visible in `!tracked` output.

7. **Buttons bypass `command_security`.** DTS attachment is the operator's authorization decision. Commands honor `command_security` as usual.

8. **`applies_to` defaults are action-level.** Action buttons that mutate state (`mute`, `unsubscribe`) default to `["dm"]`. Response-template buttons and ephemeral-only actions default to `["any"]`. Operators override per-button as needed.

9. **`Snapshot` carries both `TemplateRequested` and `TemplateSelected`.** Exact-template re-render uses `TemplateSelected`; "re-resolve through chain" uses `TemplateRequested`.

10. **`buttonResponse` is a new template type** for shared response templates. Same 6-priority selection chain; opts out of alert-type scoping.

11. **TOML template body is one multi-line string** — `"""..."""`. Same render flow as today's `templateFile`. TOML structure used only for metadata + adjacent concepts (buttons, future per-template settings). Inlining the template body into TOML's structural tables would recreate the JSON+Handlebars structural mismatch and is explicitly NOT done.

12. **Config editor: normalised JSON internally, format preserved on disk** (Option C in #110). Editor speaks JSON; save serialises back to original format. Pre-write backup catches lossy round-trips. Comments + ordering not preserved; multi-line template strings are.

13. **Telegram parity deferred.** Telegram inline keyboards differ enough that demand-gathering precedes design — see #112.

## What this branch is NOT

- It is not a rewrite of the existing DTS system. JSON DTS stays first-class indefinitely.
- It is not a replacement for the existing `!untrack` / `<type> remove` commands. Mute commands sit alongside and reuse those parsers.
- It is not a Telegram feature in v1.
- It is not a persistent-mute design. In-memory only.
