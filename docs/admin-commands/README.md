# Admin Commands — Design + Implementation Plan

This directory holds the design and implementation plan for live administrative commands in PoracleNG:

1. **`!untrack <type>`** — accept the slash-style form on text commands too.
2. **`!poracle-admin`** — umbrella for live ops (slash-command maintenance, reload, status, ratelimit, summary, reconcile, emoji, cache, maintenance pause/resume).

## Files

- **DESIGN.md** — goals, debate points, full command reference. Read first for the "why."
- **IMPLEMENTATION.md** — phased task plan with file targets. Read second for the "how."

## Status

Implemented. Branch: `slash-commands-design` (implemented alongside the slash-command surface). Smoke verification pending; see `SMOKE.md`.

## Why

A handful of common operator actions today require either editing the config and restarting the processor (slash command sync, clearing global registrations), hitting an HTTP endpoint by hand (`/api/reload`, `/api/dts/reload`), or grepping logs to answer "why isn't user X getting alerts" (rate-limit state is private). Surface these as live Discord/Telegram commands so operators can do their job from the chat client they already have open.

## Slicing

The plan was structured for three PRs but was implemented as a single branch (`slash-commands-design`) alongside the Discord slash-command surface. All phases shipped together rather than incrementally. Each subgroup remains independently auditable in its own file (`poracle_admin_reload.go`, `poracle_admin_status.go`, etc.).
