# rsvpChanges — compact RSVP-update template

When a raid's RSVP counts change (someone joins or switches a timeslot)
the processor normally re-renders the full raid or egg template. This
folder provides a lightweight alternative: a small embed that shows only
the current timeslot table and map links, without repeating the full
boss card on every update.

## When the processor uses this template

The `rsvpChanges` template type is used when **all** of the following hold:

1. A Golbat webhook arrives whose RSVP counts differ from the previous
   notification for the same raid.
2. The tracking rule for the destination has `rsvp_changes` set to 1
   (always notify on changes) or 2 (notify only when RSVPs are present).
3. A `rsvpChanges` template entry exists in the DTS config for the
   destination's platform, template ID, and language.

If condition 3 is not met the processor falls back to rendering the
standard `raid` or `egg` template for that destination.

## Cleanup / message TTH

RSVP-update messages are cleaned at the **latest future RSVP timeslot**,
not at the raid end time. Once the last timeslot has passed the message
is no longer meaningful, so it is removed earlier than the raid card
would be. If no future timeslot is found (all slots have already passed)
the processor falls back to the raid end time.

## Template fields used

| Field | Description |
|---|---|
| `levelName` | Human-readable raid tier (e.g. "Mega", "Ultra Beast", "Level 3") |
| `gymName` | Gym / landmark name |
| `ex` | Boolean — true for EX-eligible gyms |
| `imgUrl` | Boss icon (or egg icon for unhatched raids) |
| `gymColor` | Current team colour hex |
| `rsvps` | Array of future timeslot objects |
| `rsvps[].time` | Formatted local time string |
| `rsvps[].timeSlot` | Unix timestamp in seconds (use with `<t:N:R>` in Discord) |
| `rsvps[].goingCount` | Number of players marked "going" |
| `rsvps[].maybeCount` | Number of players marked "maybe" |
| `googleMapUrl` | Google Maps deep link |
| `appleMapUrl` | Apple Maps deep link |
| `wazeMapUrl` | Waze deep link |

All other raid enrichment fields (`fullName`, `quickMoveName`, `cp`,
`staticMap`, `weaknessEmoji`, etc.) are also available if you want to
extend the template.

## How to install

Copy the contents of this folder into your `config/dts/` directory:

```
cp examples/dts/rsvpChanges/rsvp-update.json config/dts/
```

Then reload DTS templates:

```
POST /api/dts/reload
```

or restart the processor. The next RSVP-change webhook for any
destination that matches `type=rsvpChanges, platform=discord,
id=default, language=en` will use this compact card instead of
the full raid embed.

To add translations or a Telegram variant, add additional entries
to `rsvp-update.json` following the same JSON shape with the
appropriate `platform` and `language` values.
