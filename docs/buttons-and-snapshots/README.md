# Buttons & Snapshots & TOML DTS

End-to-end feature work covering three interrelated GitHub issues:

- **#108** — Snapshot enrichment store (foundation).
- **#109** — Discord ephemeral interactive buttons + `!mute` / `!unmute` commands.
- **#110** — Optional TOML format for DTS templates.
- **#112** — Telegram button surface (input gathering; not implemented here).

## Documents in this folder

- **`DESIGN.md`** — short summary + pointers to the issues for full design rationale.
- **`IMPLEMENTATION.md`** — phased task list with checkboxes. Where the actual build work is laid out.
- **`SMOKE.md`** — manual verification checklist per phase.

## Branch

Single feature branch: `buttons-and-snapshots` off `raid-rsvp`. The dev chain stacks:

```
main
 └── slash-commands-design
      └── raid-rsvp
           └── buttons-and-snapshots  ← us
```

`slash-commands-design` and `raid-rsvp` are not pre-merged to `main`; we develop on top of them in sequence.

## Scope at a glance

- Opt-in (`[snapshots] enabled = false` by default) on-disk store of resolved enrichment views per delivered message.
- Discord buttons attached to alerts that either render an ephemeral follow-up or dispatch a named action (mute, unsubscribe, redeliver, render).
- `!mute` / `!unmute` commands with parity to the existing `!untrack` / `<type> remove` duality; mutes display on `!tracked`.
- Optional TOML format for DTS authoring; config editor round-trips JSON internally and preserves the on-disk format with pre-write backup.

## Out of scope (this branch)

- Telegram button surface — separate input-gathering issue (#112).
- Buttons attached to response messages (drill-down menus) — v2.
- Persisted mutes — in-memory only.
- Hot-tier caching on snapshot reads — defer until metrics justify.
