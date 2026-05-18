# Raid RSVP — Smoke Test Checklist

Operator checklist for verifying the raid-rsvp feature against a real Golbat raid stream on a dev deployment.

---

## Pre-flight

- [ ] Branch `raid-rsvp` deployed and running.
- [ ] At least one `!raid <level>` tracking rule with `rsvp` (or `rsvp_only`) and `clean` (or `edit`) set.
- [ ] `examples/dts/rsvpChanges/rsvp-update.json` copied into `config/dts/` — or intentionally omitted to test fallback behavior first.
- [ ] DTS reload performed (`!poracle-admin reload dts`) so any newly installed template is active.

---

## Fallback behavior (no rsvpChanges template installed)

- [ ] Active raid arrives → full `raid` template renders. (Unchanged from before this feature.)
- [ ] RSVP change arrives → full `raid` template renders (fallback, because no `rsvpChanges` template is present).
- [ ] ReplyKey is still set — verify by tracking both eggs and raids (with `clean` or `edit` bit), triggering an egg → raid hand-off, and confirming the raid message arrives as a reply to the egg message in Discord/Telegram.

---

## With rsvpChanges template installed

- [ ] Active raid arrives → full `raid` template renders.
- [ ] First RSVP change arrives → compact `rsvpChanges` template renders; message is a reply to the original raid message.
- [ ] Second RSVP change arrives → compact `rsvpChanges` template renders; message is a reply to the previous `rsvpChanges` message (chain continues).
- [ ] When `clean` bit set: each `rsvpChanges` message auto-deletes at the latest future RSVP timeslot TTH (not at `raid.End`).
- [ ] When `edit` bit set: `rsvpChanges` template is **ignored**; the original raid message is edited in-place on every RSVP update.

---

## Egg → raid chain

- [ ] Egg rule has `clean` or `edit` bit set; raid rule has `clean` or `edit` bit set.
- [ ] Egg arrival → standalone egg message sent.
- [ ] Egg hatches into active raid → raid message arrives as a reply to the egg message (shared `raidlife:{gymID}:{raidEnd}` ReplyKey).

---

## rsvp_only mode (rsvp_changes = 2)

- [ ] No RSVPs yet for a tracked raid → no message sent (rsvp_only suppresses the initial alert).
- [ ] First RSVP arrives → full `raid` template fires (first-notification rule).
- [ ] Subsequent RSVP changes → `rsvpChanges` template if installed; `raid` template otherwise.

---

## Health check

- [ ] No unexpected errors in processor logs after RSVP-change jobs are processed.
- [ ] No metrics regressions expected — this feature is pass-through for non-RSVP raids.

---

## Operator tips

- If RSVP-change messages do not reply to the original raid message, confirm that the `clean` or `edit` bit is set on the raid tracking rule. Without message tracking enabled the `ReplyKey` is stored but no prior message exists to reply to.
- If the `rsvpChanges` template is installed but not being used, run `!poracle-admin reload dts` and check the processor log for template-load errors.
- If clean-deletion fires at raid end instead of the latest RSVP timeslot, confirm you are running the `raid-rsvp` branch — the `OverrideCleanTTH` field was added in this branch.
- To inspect the reply chain, enable `[webhookLogging]` in config and compare the `message_reference` IDs in successive deliveries for the same gym.
