# incident — event-only pokestop template

Invasion webhooks come in two flavours:

1. **Grunt invasions** — Team Rocket grunts with pokemon rewards (`gruntTypeID > 0`).
   These render through the `invasion` template as before.
2. **Incident invasions** — event-only pokestop overlays such as Kecleon,
   Gold Pokestop, and Showcase (`gruntTypeID == 0 && displayType >= 7`).
   These render through the `incident` template.

The split is webhook-level: every matching user for an incident webhook receives
the `incident` template; every matching user for a grunt webhook receives the
`invasion` template. There is no per-user toggle.

## Why a separate template?

Grunt templates reference `gruntRewardsList`, `genderEmoji`, and the gender
block. Those fields are not populated for incidents, so a combined template
requires defensive `{{#if}}` guards throughout. The `incident` template type
ships a trimmed surface — only pokestop, location, time, weather, and four
new convenience aliases — so operators can write a clean, simple card.

## Available fields (incident-specific)

| Field | Description |
|---|---|
| `incidentType` | Translated display-type label (e.g. "Gold Pokéstop", "Kecleon"). Alias for `gruntName`. |
| `incidentSlug` | Lowercase event slug for `{{#if (eq incidentSlug "kecleon")}}` dispatch. Alias for `gruntType`. |
| `incidentEmoji` | Resolved per-platform emoji for the event icon. Alias for `gruntTypeEmoji`. |
| `color` | Event color hex for Discord embed colour. Alias for `gruntTypeColor`. |
| `pokestopName` | Pokestop name. |
| `disappearTime` / `time` | Incident expiry time (formatted). |
| `expirationTimestamp` | Unix expiry timestamp — use with `<t:N:R>` in Discord. |
| `tthh`, `tthm`, `tths` | Time remaining (hours, minutes, seconds). |
| `addr` | Reverse-geocoded address. |
| `googleMapUrl`, `appleMapUrl`, `wazeMapUrl` | Map links. |
| `staticMap` | Static map tile URL. |
| `imgUrl` | Event icon URL. |
| `gameWeatherName`, `gameWeatherEmoji` | S2 cell weather. |

Grunt / reward / gender fields (`gruntRewardsList`, `genderEmoji`, etc.) are
**not available** for incidents.

All common fields (location, maps, time, weather) are also available — see
the Common Fields section of DTS.md.

## How to install

Copy the JSON file into your `config/dts/` directory:

```sh
cp examples/dts/incident/incident-update.json config/dts/
```

Then reload DTS templates:

```
POST /api/dts/reload
```

or restart the processor.

To add translations or a Telegram variant, add additional entries to
`incident-update.json` following the same JSON shape with the appropriate
`platform` and `language` values.
