# Editor Handoff — Platform-Agnostic (help) Templates & Readonly Visibility

**For:** the DTS template editor (`~/dev/poracle-embed-visualizer`).
**From:** PoracleNG processor, branch `fix/dts-agnostic-platform-save` (PR #176).
**Date:** 2026-07-27.

## The bug this closes
Loading a fallback **help** template (e.g. `help/fort`) in the editor and saving it to edit later produced a **duplicate** in the editable-templates list: the readonly fallback **plus** a second `discord` copy. Root cause was two-sided:

- **Server** rejected `platform=""` on save, forcing help to be saved with a concrete platform.
- **Editor** coerces an empty platform to `"discord"` on load, so the agnostic help fallback (`platform=""`) becomes a `discord`-specific entry that can't shadow the `""` fallback (the override key includes platform).

The **server half is shipped** (see "Server contract" below). This doc is the **editor half**.

## Key fact: `help` is the ONLY platform-agnostic type
Every other DTS type — `monster`, `monsterNoIv`, `raid`, `egg`, `quest`, `questSummary`, `invasion`, `incident`, `lure`, `nest`, `gym`, `fort-update`, `maxbattle`, `showcase`, `monsterChanged`, `weatherchange`, `rsvpChanges`, `buttonResponse` — is **platform-specific** and MUST carry a concrete platform (`discord`/`telegram`). Only `help` is agnostic: its bundled fallbacks ship with `platform=""` (from `fallbacks/dts/help/*.json`) and a single entry serves all platforms.

Hardcode this to match the server:

```js
const AGNOSTIC_TYPES = new Set(['help']);
const isAgnostic = (type) => AGNOSTIC_TYPES.has(type);
```

(The server's equivalent is `dts.IsPlatformAgnosticType`, currently `{help}`. There is no API to query the set — keep this client list in sync if the server ever adds more agnostic types.)

## Editor changes

### 1. Stop coercing empty platform to `"discord"` for agnostic types
`src/hooks/useDts.js:8`:

```js
.map((e) => ({ ...e, id: String(e.id ?? '1'), platform: e.platform || 'discord', language: e.language ?? '' }));
```

`e.platform || 'discord'` is what turns the fallback's `""` into `"discord"`. Preserve `""` for agnostic types:

```js
.map((e) => ({
  ...e,
  id: String(e.id ?? '1'),
  platform: isAgnostic(e.type) ? (e.platform ?? '') : (e.platform || 'discord'),
  language: e.language ?? '',
}));
```

Apply the same to the other coercion sites: `useDts.js:180` (`platform: template.platform || 'discord'`) and the new-template default at `useDts.js:15` (`platform: 'discord'` — a **new** help template should default to `""`, not `discord`).

### 2. Show agnostic entries regardless of the platform tab
The list filters by `t.platform === filters.platform` (`useDts.js:32, 41, 47, 70, 81, 142`). An agnostic help entry (`platform=""`) fails `"" === "discord"` and would vanish from every platform tab. Include agnostic entries in any tab:

```js
const platformMatches = (t) => isAgnostic(t.type) || t.platform === filters.platform;
```

(Or give `help` its own platform-neutral view — but "show in every tab" is the least surprising.)

### 3. Save agnostic templates with `platform=""`
When saving a help template, POST `platform: ""` (not `"discord"`). With change #1 preserving `""` through state, this happens naturally as long as the save path doesn't re-inject a platform. Server-side the file is then written as `config/dts/help-fort.json` (no platform segment).

Also fix the **download** filename at `src/App.jsx:319`:

```js
a.download = `${entry.type}-${entry.id || 'default'}-${entry.platform || 'discord'}.json`;
```

Omit the platform segment when empty so an agnostic download is `help-fort.json` (mirrors the server's `entryFilename`).

### 4. Show readonly (fallback) entries in the list, clearly marked
`GET /api/dts/templates` returns **`readonly: true`** on every bundled/fallback entry. **Surface these in the template list — don't hide them** — with a clear badge such as **"read-only (fallback)"** (and, ideally, an affordance to "copy / override"). This makes it obvious which rows are:

- editable in place (the user's own `config/dts/` entries, `readonly` absent/false), vs
- read-only fallbacks the user can copy to create an override.

When the user saves an override of a readonly fallback, the server **drops the readonly entry** from the returned list (the user's copy shadows it), so the row naturally flips from "read-only" to editable — no duplicate. Rendering the `readonly` flag is what makes that transition legible to the user.

## Server contract (already shipped — PR #176)
- `POST /api/dts/templates` now **accepts `platform=""` for agnostic types** (`help`). Non-agnostic types still return **400** without a platform (unchanged).
- A saved `(help, <id>, "")` override shares the fallback's key and **shadows** it: the editable list then shows exactly **one** `help/<id>` entry (the user's), with the readonly fallback dropped.
- Saved agnostic files are named `help-<id>.json` (no platform segment).
- `GET /api/dts/templates` continues to return `readonly: true` on fallback entries — use it for the badge in change #4.

## Migration note (existing duplicate)
The duplicate currently visible on the running instance comes from an already-saved `config/dts/help-fort-discord.json` (platform `discord`) written before this fix. Delete it (editor delete button or `rm`) to clear the current duplicate; the fix only prevents **new** ones.
