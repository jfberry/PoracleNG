# Admin Commands — Design + Implementation Plan

This directory holds the design and implementation plan for live administrative commands in PoracleNG:

1. **`!untrack <type>`** — accept the slash-style form on text commands too.
2. **`!poracle-admin`** — umbrella for live ops (slash-command maintenance, reload, status, ratelimit, summary, reconcile, emoji, cache, maintenance pause/resume).

## Files

- **DESIGN.md** — goals, debate points, full command reference. Read first for the "why."
- **IMPLEMENTATION.md** — phased task plan with file targets. Read second for the "how."

## Status

Plan stage. Not yet implemented. Intended branch: `admin-commands`.

## Why

A handful of common operator actions today require either editing the config and restarting the processor (slash command sync, clearing global registrations), hitting an HTTP endpoint by hand (`/api/reload`, `/api/dts/reload`), or grepping logs to answer "why isn't user X getting alerts" (rate-limit state is private). Surface these as live Discord/Telegram commands so operators can do their job from the chat client they already have open.

## Slicing

The plan is structured so any single phase can ship as its own PR. Recommended slicing for low-risk delivery:

1. **PR-A (foundation):** `!untrack <type>` reroute + `!poracle-admin` skeleton + `reload` subgroup. ~150 LoC, no new introspection APIs.
2. **PR-B (introspection):** new read-only state-inspection APIs (webhook rate, ratelimit, cache stats), plus the `status` subcommand that consumes them and `!info poracle` convergence.
3. **PR-C (the rest):** `slash`, `ratelimit`, `summary`, `reconcile`, `emoji`, `cache`, `maintenance`.

Each PR is independently revertable.
