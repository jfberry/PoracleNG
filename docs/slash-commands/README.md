# Slash Commands — Design + Implementation Plan

This directory holds the design and implementation plan for an optional Discord slash command surface for PoracleNG.

## Files

- **DESIGN.md** — design decisions, debate-points, full command reference. Read first for the "why."
- **IMPLEMENTATION.md** — task-by-task TDD plan, 48 tasks across 7 phases, self-review at end. Read second for the "how."

## Status

Plan stage. Not yet implemented. Branch: `slash-commands-design`.

## Summary

Slash commands as a thin facade over the existing text-command framework. Slash handlers map Discord options → text-command tokens; existing `bot.Command.Run` handles dispatch, validation, DB writes, and reply construction unchanged.

Slash is:
- **Optional** — gated behind `[discord.slash_commands] enabled = true` and an explicit `enable = [...]` allow-list.
- **Personal-DM-scope only** — always targets the invoking user; no `user:` / `name:` admin overrides.
- **All-ephemeral by default** — Poracle conversations are inherently private; no public-channel response surface.
- **Coexisting with text commands** — existing `!` text commands work unchanged.

24 commands surfaced: `/version /tracked /help /info /language /start /stop /track /raid /egg /quest /tappable /invasion /incident /lure /nest /maxbattle /gym /fort /untrack /area /profile /location /summary`. `/tappable` takes one required `reward` kind (candy, XL candy, mega energy, item, stardust, pokecoins, everything) qualified by `pokemon` / `item` / `amount`; `/untrack tappable` removes a tappable rule.

Text-only commands (deliberately): `!poracle`, `!poracle-test`, admin operations (`!broadcast`, `!apply`, `!enable`, `!disable`, `!unregister`, `!channel`, `!webhook`, `!autocreate`, `!role`, `!ask`, `!script`, `!userlist`, `!backup`, `!restore`).
