# Raid RSVP-update template + reply chain

This directory holds the design and implementation plan for two related raid-side improvements:

1. **`rsvpChanges` DTS template** — an optional compact template for RSVP-update messages so operators don't have to repeat the full raid card every time someone changes their RSVP. Falls back to `raid` / `egg` when not defined.
2. **Implicit reply chain** — raid and rsvpChanges messages reply to the originating egg (if tracked) so the lifecycle is visible as one thread in the channel.

## Files

- **DESIGN.md** — goals, decisions, why-it-looks-this-way. Read first.
- **IMPLEMENTATION.md** — task-by-task plan. Read second.

## Status

Plan stage. Branch: `raid-rsvp`, branched off `slash-commands-design` so the two surfaces can be tested together.

## Operator surface

If the operator doesn't define an `rsvpChanges` template → current behavior, no change.

If they do (by copying `examples/dts/rsvpChanges/` into `config/dts/`):
- RSVP-change notifications (non-edit mode) use the compact template.
- Each RSVP-change message replies to the previous, forming a thread.
- Egg → raid hand-off also replies, so the whole raid lifecycle threads if both eggs and raids are tracked.
- Cleanup TTH for compact messages = latest RSVP timeslot (not raid end), so the thread doesn't linger past the meaningful window.
