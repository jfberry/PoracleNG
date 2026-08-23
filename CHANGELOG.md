# Changelog

All notable changes to PoracleNG are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- **v2 mutes API.** `GET/POST /api/v2/humans/{id}/mutes` and
  `DELETE /api/v2/humans/{id}/mutes[?scope=&value=]` expose the in-memory alert
  mutes (the `!mute` / alert-button feature) over HTTP, and the v2 full
  snapshot now carries a `mutes` array. Mutes remain volatile — they are
  cleared by a processor restart.
- **OpenAPI 3.1 spec + interactive docs.** The processor now serves a single
  OpenAPI 3.1 document at `GET /openapi.json` with interactive documentation at
  `GET /docs` (both public, no `X-Poracle-Secret` required). The spec covers the
  entire `/api/*` and `/api/v2/*` surface, generated from the code via
  [huma](https://github.com/danielgtaylor/huma) mounted on gin.
- **New strict `/api/v2` API** for tracking and humans/profiles — typed bodies,
  `additionalProperties: false`, enforced required fields, and no lenient type
  coercion. A malformed request gets a clear `422` instead of a silent guess.
  - **Tracking** is human-scoped: `GET|POST /api/v2/humans/{id}/tracking/{type}`,
    `GET|PUT|DELETE /api/v2/humans/{id}/tracking/{type}/{uid}`, bulk
    `DELETE …/{type}?uid=1,2,3`, and a full snapshot at
    `GET /api/v2/humans/{id}/tracking` (human + all-type rules + profiles +
    locations + summaries). Supports `?profile=`, `?include_descriptions=`,
    `?silent=`, and `?all_profiles=`.
  - **humans/profiles** are exposed as discrete, typed action endpoints under
    `/api/v2/humans/{id}/…` (enable/disable, admin-disable, language, location,
    areas, check-location, locations CRUD, roles, profiles CRUD with a strict
    typed `active_hours` schema, and profile switch).
- **New `incident` tracking type** (game `PokestopEvent`, e.g. Showcase) —
  available only via the `/api/v2` tracking surface, bringing the v2 tracking
  type count to 11 (pokemon, raid, egg, quest, invasion, incident, lure, nest,
  gym, fort, maxbattle).
- **New `PUT /api/v2/humans/{id}/locations/{label}`** — update a saved
  location's coordinates in place, completing saved-location CRUD (v1 required
  delete + re-add to move a location).

### Changed

- **Error bodies on the huma `/api` surface are now RFC 9457
  `application/problem+json`** (`status`, `title`, `detail`, `errors[]`),
  replacing the old ad-hoc `{ "status": "error", "message": … }` /
  `{ "error": … }` bodies. **This is a behavior change for clients that parse
  error response bodies.** HTTP status codes are unchanged, though a few
  input-validation failures that were manual `400`s now surface as huma
  validation `422`s.
- **`include_empty` on fort tracking now defaults to `true`** when omitted,
  honoring the `forts` DB column default. The previous gin handler defaulted it
  to `false`; API clients that omit `include_empty` now get `true`.

### Deprecated

- **v1 tracking/humans/profiles endpoints are frozen and
  deprecated-but-supported.** `/api/tracking/*`, `/api/humans/*`,
  `/api/profiles/*` (and `/api/tracking/pokemon/refresh`) remain on gin,
  unchanged and fully functional — clients migrate to `/api/v2` on their own
  schedule. Migration is encouraged to access new tracking types (e.g.
  `incident`) and the cleaner strict contract. No sunset date is set yet.
