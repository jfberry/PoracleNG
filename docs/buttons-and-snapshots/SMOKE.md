# Buttons & Snapshots & TOML DTS — Smoke Tests

Manual verification checklist per phase. Each phase should be runnable end-to-end on a dev environment before moving on.

Prerequisites for every phase:
- Dev processor running with a known DTS configuration.
- A test Discord guild + bot with `MESSAGE_CONTENT` + `GUILD_MEMBERS` intents (existing config).
- `!poracle-test` available for synthetic webhook firing.

---

## Phase 1: Snapshot store

### 1.1 Opt-in default

- [ ] Start processor with no `[snapshots]` section in config.
- [ ] Confirm: no `config/.cache/snapshots/` directory created.
- [ ] `GET /api/snapshots/<any-msg-id>` returns 503.
- [ ] Fire a pokemon alert via `!poracle-test`. No snapshot written.

### 1.2 Enabled writes

- [ ] Add `[snapshots] enabled = true` to config; restart.
- [ ] Confirm: `config/.cache/snapshots/` exists.
- [ ] Fire a pokemon alert via `!poracle-test`. Find the Discord message ID from the bot's log.
- [ ] `GET /api/snapshots/<messageID>` returns 200 with a JSON `Snapshot` object.
- [ ] Snapshot has all fields populated: `MessageID`, `Target`, `TargetType=dm`, `AlertType=monster`, `TemplateType=monster`, `TemplateRequested`, `TemplateSelected`, `Language`, `Platform=discord`, `TrackingUIDs`, `View` (non-empty).

### 1.3 Edit overwrites

- [ ] Fire a raid alert with `edit` clean-bit set.
- [ ] Trigger an RSVP edit (use raid-rsvp's test path or a synthetic RSVP webhook).
- [ ] Confirm the message in Discord still has its buttons after the edit (if Phase 3 has landed).
- [ ] `GET /api/snapshots/<messageID>` reflects the updated view, not the original.

### 1.4 Clean-deletion drops snapshot

- [ ] Fire a pokemon alert with `clean` bit set and a short TTH (e.g. a test pokemon with `disappear_time` 2 min in the future).
- [ ] Wait for TTH + 30s.
- [ ] `GET /api/snapshots/<messageID>` returns 404. The Discord message should also be deleted.

### 1.5 Safety sweep

- [ ] Set `[snapshots] max_age = "1m"` and `sweep_interval = "30s"`.
- [ ] Fire an alert.
- [ ] Manually delete the Discord message (so clean-deletion callback doesn't fire).
- [ ] Wait 90s.
- [ ] Confirm via metrics: `poracle_snapshot_sweep_deletions_total` incremented.
- [ ] `GET /api/snapshots/<messageID>` returns 404.

### 1.6 Write failure soft-fail

- [ ] Manually chmod the snapshots directory to read-only.
- [ ] Fire an alert.
- [ ] Confirm: the alert is delivered (message visible in Discord). Logs show a warn about snapshot write. The processor does not crash.

---

## Phase 2: Mute infrastructure + commands

### 2.1 Property mute via command

> Gym, pokestop, and station IDs are 32-char hex with a `.16` suffix (e.g. `1a15c33709c147fd85eeb9e6bb1e1c14.16`). Names also work (e.g. `"Victoria Park Entrance"`) and resolve via the same scanner DB lookup `!raid gym:…` uses. The scope noun (`gym`, `pokemon`, `area`, etc.) is positional after `!mute` — `:` notation is reserved for parameters like `duration:` and `id:`. Multi-word values need quotes.

- [ ] `!mute gym "Victoria Park Entrance" duration:5m` in a DM (or `!mute gym <hex-id>` if you have a known ID).
- [ ] Confirm: bot replies with `msg.mute.added` confirmation naming the gym.
- [ ] Fire a raid alert at that gym via `!poracle-test`. Confirm: no alert delivered to the user.
- [ ] Fire a raid alert at a different gym. Confirm: alert delivered.

### 2.2 Mute appears in `!tracked`

- [ ] `!tracked` in the same DM. Confirm:
  - Tracking rules listed as before.
  - "Property mutes" section at the bottom shows the gym mute (by name if resolved, else by hex ID) with remaining time.

### 2.3 Per-type mute by UID

- [ ] `!tracked` to find a tracking rule UID for raids.
- [ ] `!mute raid id:<uid> duration:5m`. Confirm: bot reply.
- [ ] `!tracked` shows the rule with `🔇 muted` indicator.
- [ ] Fire a raid alert that matches that rule. Confirm: dropped.
- [ ] `!raid mute id:<uid>` (alternate form) works equivalently.

### 2.4 Unmute

- [ ] `!unmute gym "Victoria Park Entrance"` (or the hex form). Confirm: bot reply.
- [ ] `!tracked` no longer lists the property mute.
- [ ] Fire a raid alert at that gym. Confirm: delivered.

### 2.5 Unmute all

- [ ] Apply 3 mutes (property gym + UID + property pokemon).
- [ ] `!unmute all`. Confirm: bot reply with count.
- [ ] `!tracked` shows no mutes.

### 2.6 Automatic expiry

- [ ] `!mute pokemon pikachu duration:1m`.
- [ ] Wait 90s.
- [ ] `!tracked` no longer lists the mute.
- [ ] Fire a pikachu alert. Confirm: delivered.

### 2.6a Vocabulary aliases

- [ ] `!mute everything duration:1m` — confirm: self-mute all alerts applied.
- [ ] Fire any alert. Confirm: dropped.
- [ ] `!unmute everything` — confirm: clears.
- [ ] `!unmute all` works equivalently.

### 2.6b Tracking-rule vs entity mute disambiguation

- [ ] `!mute pokemon pikachu` — entity mute on the species.
- [ ] `!tracked` shows it under "Property mutes" as `pokemon:25`.
- [ ] `!unmute pokemon pikachu` to clear.
- [ ] `!tracked` to find a pokemon tracking UID for a pikachu-matching rule.
- [ ] `!mute pokemon id:<uid>` — tracking-rule mute.
- [ ] `!tracked` shows it inline beside the tracking rule with `🔇 muted` indicator.
- [ ] Both forms work distinctly — confirm `!mute pokemon pikachu` and `!mute pokemon id:<uid>` can co-exist.

### 2.7 Profile axis

- [ ] Set up two profiles for the user.
- [ ] On profile 1: `!mute gym "Victoria Park Entrance" duration:10m`. (entity mute — should apply across profiles)
- [ ] Switch to profile 2.
- [ ] Fire an alert for that gym. Confirm: dropped.
- [ ] `!mute raid id:<profile-2-uid>` (UID mute, profile 2-specific).
- [ ] Switch to profile 1. `!tracked`: shouldn't show the profile-2 UID mute under any profile-1 rule.

---

## Phase 3: Buttons end-to-end

### 3.1 Quick start example

- [ ] Add the Quick Start example from #109 to `config/dts/raid-with-button.json`.
- [ ] `POST /api/dts/reload`.
- [ ] Confirm: no errors in logs.
- [ ] Fire a raid alert in a DM. Confirm: message has one "Mute this gym (1h)" button.

### 3.2 Click → mute applied

- [ ] Click the button.
- [ ] Confirm: ephemeral confirmation message ("Muted gym X for 1 hour").
- [ ] `!tracked` lists the property mute.
- [ ] Fire another raid alert at the same gym. Confirm: dropped.

### 3.3 `applies_to` filtering

- [ ] Configure the same template for a channel destination.
- [ ] Fire a raid alert that goes to the channel.
- [ ] Confirm: NO mute button visible on the channel message (mute defaults to `["dm"]`).
- [ ] Add `applies_to = ["dm", "channel"]` to the button definition, reload, re-fire.
- [ ] Confirm: button now visible on the channel message.

### 3.4 `show_if` evaluation

- [ ] Add a "Show PVP details" response-template button to a pokemon template with `show_if = "{{pvpAvailable}}"`.
- [ ] Fire a pokemon alert with PVP data. Confirm: button present.
- [ ] Fire a pokemon alert without PVP data. Confirm: button absent.

### 3.5 Response template — inline

- [ ] Use the PVP-details inline example from #110.
- [ ] Click the button. Confirm: ephemeral embed with PVP rankings rendered.

### 3.6 Response template — by id

- [ ] Define a `buttonResponse` entry: `id="coordinates"`, `type="buttonResponse"`, simple template showing lat/lon.
- [ ] Add a button referencing `response_template_id = "coordinates"` to a pokemon template.
- [ ] Click the button. Confirm: ephemeral coordinates card.

### 3.7 Response — single line text

- [ ] Add a button with `response_text = "📍 {{latitude}}, {{longitude}}"`.
- [ ] Click. Confirm: ephemeral text message with the coordinates.

### 3.8 Error paths

- [ ] **Expired:** Use a manually-fired alert with `[snapshots] max_age = "10s"`. Wait 30s. Click the button. Confirm: ephemeral `msg.button.expired`.
- [ ] **Unavailable:** Click a button. Remove the button definition from DTS, reload. Click the button again. Confirm: ephemeral `msg.button.unavailable`.
- [ ] **Unauthorized:** Send an alert to a channel. As a non-admin user, click a button with `visible_to = "admin"`. Confirm: ephemeral `msg.button.unauthorized`. Then send the same alert to a non-admin user's DM and confirm the admin button is hidden entirely.
- [ ] **Cooldown:** Double-click a button rapidly. Confirm: second click returns `msg.button.cooldown`.

### 3.9 Edit preserves buttons

- [ ] Fire a raid alert with button + `edit` bit set.
- [ ] Trigger an RSVP edit.
- [ ] Confirm: edited message STILL has the button (regression check on Task 3.4).

### 3.10 Snapshots disabled → no buttons

- [ ] Set `[snapshots] enabled = false`. Restart.
- [ ] DTS still has buttons declared.
- [ ] Fire an alert. Confirm: message has NO buttons. No errors in logs.

### 3.11 `unsubscribe` action

- [ ] Add an unsubscribe button to a raid template with `scope = "tracking"`, `applies_to = ["dm"]`.
- [ ] Fire a raid alert in DM that matches a known tracking rule.
- [ ] Click the unsubscribe button. Confirm: ephemeral confirmation listing the deleted rule(s).
- [ ] `!tracked` no longer shows that rule.
- [ ] Fire another matching raid. Confirm: NO alert (the rule is gone).

### 3.12 `redeliver` action

- [ ] Fire a raid alert in a channel.
- [ ] As a registered user, click the redeliver button.
- [ ] Confirm: same alert appears in your DM, rendered with the same template.

---

## Phase 4: TOML loader + editor round-trip

### 4.1 TOML file loads

- [ ] Place `examples/dts/buttons/raid-with-pvp.toml` in `config/dts/`.
- [ ] `POST /api/dts/reload`. Confirm: no errors.
- [ ] `GET /api/dts/templates`. Confirm: the entries from the TOML file appear with `source_format = "toml"`.

### 4.2 TOML buttons work at runtime

- [ ] Fire a raid alert that matches the TOML entry. Confirm: button(s) attached.
- [ ] Click. Confirm: action / response works as for JSON entries.

### 4.3 Bad TOML doesn't crash

- [ ] Add a TOML file with a syntax error (unclosed string, missing bracket).
- [ ] `POST /api/dts/reload`. Confirm: warn in logs identifying the file + problem; other entries still load.

### 4.4 Duplicate-conflict WARN

- [ ] Two files in `config/dts/` with the same `(type, id, platform, language)` key. Reload.
- [ ] Confirm: WARN log lists the conflict and which file wins.
- [ ] `config/dts/foo.json` overriding `config/dts.json` (same key) → NO WARN.

### 4.5 Editor round-trip

- [ ] Open a TOML entry in the config editor (PoracleWeb or direct API call).
- [ ] Confirm: surfaced as JSON.
- [ ] Edit a field (e.g. add an inline comment to the description — not the TOML file, the JSON form via editor).
- [ ] Save.
- [ ] Confirm: file on disk is STILL TOML. Original file backed up to `<name>.toml.bak` (or whatever the existing convention is).
- [ ] Reload DTS, confirm the edit is live.

### 4.6 Multi-line strings survive round-trip

- [ ] TOML entry with a `"""..."""` template body containing `{{#if}}` blocks.
- [ ] Edit a metadata field via the editor (not the template body). Save.
- [ ] Confirm: the template body is preserved exactly. Render still works.

---

## Phase 5: Documentation + polish

### 5.1 Docs cross-link

- [ ] `CLAUDE.md` mentions snapshot store, mute infrastructure, button actions, TOML DTS — each with file paths.
- [ ] `DTS.md` has a Buttons section + a TOML section.
- [ ] `README.md` (project root) has a one-paragraph operator summary.

### 5.2 Examples work

- [ ] Drop each `examples/dts/buttons/*` file into `config/dts/`. Reload. Fire a matching alert. Confirm: button appears and works.

### 5.3 Final test run

- [ ] `go test ./...` from `processor/` — green.
- [ ] `go vet ./...` — clean.
- [ ] `staticcheck ./...` (or your equivalent) — clean.
- [ ] Manual: run through every phase's smoke test above in sequence on a clean dev env.

---

## After all phases pass

Open the PR. Reference issues #108, #109, #110, #112. PR description should:

- Summarise what each phase delivers.
- Link to this doc.
- Note the `[snapshots] enabled = false` default explicitly.
- Note operators upgrading from earlier versions don't need to change anything (additive feature, opt-in).
