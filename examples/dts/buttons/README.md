# Buttons examples

Drop either file into `config/dts/` and reload DTS (`POST /api/dts/reload` or restart). The button appears on the next matching alert provided `[snapshots] enabled = true` is set in config.

## Files

- **`raid-with-mute.json`** — minimal JSON example showing a mute button + an unsubscribe button on a raid card. Use this as the starting point for adding a single mute button to an existing template.

- **`raid-with-pvp.toml`** — TOML form covering more of the vocabulary: an inline PVP-details response template gated by `show_if = "{{hasPVP}}"`, a mute action button, and a single-line `response_text` button. Multi-line template strings with `{{#if}}` blocks wrap whole embed fields — the TOML format makes that ergonomic in a way JSON DTS cannot.

## Operator notes

- `[snapshots] enabled = false` (the default) silently disables button rendering even when buttons are configured. Operators can keep button definitions in DTS and toggle the feature via config.
- Per the design (#109), action buttons default to `applies_to = ["dm"]` and response-only buttons default to `["any"]` — operators rarely need to set `applies_to` explicitly.
- See `docs/buttons-and-snapshots/` and GitHub issues #108, #109, #110 for the full design + decision history.
