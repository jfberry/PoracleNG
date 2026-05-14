# `!autocreate` and `channelTemplate.json`

`!autocreate` is an admin-only Discord command that builds out a section of your guild from a JSON template — categories, channels, role permissions, optional webhooks, optional private threads with a button-driven picker, and the Poracle tracking rules each one starts with.

> **Audience.** This document is for server admins writing or editing `config/channelTemplate.json`. The command itself is documented inline; this doc is the reference for the file format.

---

## Quick start

1. Create `config/channelTemplate.json` with one or more templates. Reference example: `examples/channelTemplate-threads.json`.
2. Make sure the Poracle bot has these guild permissions: **Manage Channels**, **Manage Webhooks** (if any channel uses `controlType: "webhook"`), **Manage Roles** (if any role overrides are configured), **Create Private Threads** + **Manage Threads** (if any channel has a `threads` block).
3. In any channel of the target guild, an admin runs:
   ```
   !autocreate <templateName> <arg0> <arg1> ...
   ```
4. The bot replies inline as it creates each piece.

The command can also be invoked from anywhere by passing `guild<guildID>` as an argument:
```
!autocreate area apollo guild<123456789012345678>
```

---

## File structure

`channelTemplate.json` is an **array** of template objects. Each object has a `name` (matched against the first argument of `!autocreate`) and a `definition` block.

```json
[
  {
    "name": "area",
    "definition": {
      "category": { ... },
      "channels": [ ... ]
    }
  },
  {
    "name": "stress",
    "definition": { ... }
  }
]
```

| Top-level field | Type | Required | Notes |
|-----------------|------|----------|-------|
| `name` | string | yes | Looked up case-sensitively against the first arg of `!autocreate`. |
| `definition.category` | object | no | If present, a Discord category is created and any channels created in this run are placed inside it. |
| `definition.channels` | array | yes | One entry per channel to create. |

### Placeholders

Every string field in the template that ends up as user-visible text supports `{0}`, `{1}`, `{2}` … placeholders, replaced with the positional args after the template name. For example, with `!autocreate area apollo`:

- `"channelName": "{0}_master"` → `apollo_master`
- `"topic": "Master area channel for {0}"` → `Master area channel for apollo`

For commands fed through `commands:` and `threads[].commands:` arrays, the parent channel's args are passed verbatim. Threads additionally get the thread name appended as the *next* placeholder after the parent's args, so `!area add {0}` referencing position 0 still works inside the thread.

Spaces in args are converted to underscores when feeding placeholders into the channel/thread body, so `!autocreate area "old town"` produces `old_town_master` rather than a broken channel name.

---

## Category

```json
"category": {
  "categoryName": "{0} Pokemon",
  "roles": [ ... ]
}
```

| Field | Type | Notes |
|-------|------|-------|
| `categoryName` | string | The category's display name. Placeholders allowed. |
| `roles` | array of `roleEntry` | Permission overrides applied to the category. See [Role entry](#role-entry). |

If `category` is omitted, channels are created at the guild root.

---

## Channels

`channels` is an array. Each entry creates one channel.

```json
{
  "channelName": "{0}-iv100",
  "channelType": "text",
  "topic": "Hundos for {0}",
  "controlType": "bot",
  "webhookName": "",
  "roles": [ ... ],
  "commands": [
    "area add {0}",
    "track everything iv100"
  ],
  "threadPicker": { ... },
  "threads": [ ... ]
}
```

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `channelName` | string | required | Discord channel name. Placeholders allowed. |
| `channelType` | `"text"` / `"voice"` | `"text"` | Voice channels can't carry alerts; use them for adjacent voice rooms. |
| `topic` | string | none | Channel topic line. Placeholders allowed. Text channels only. |
| `controlType` | `"bot"` / `"webhook"` / `""` | `""` | What Poracle uses to send to this channel. **Empty means the channel is created but not registered with Poracle** — no commands run, no alerts delivered. Use empty for adjacent-context channels (chatrooms, voice). |
| `webhookName` | string | falls back to `channelName` | Display name of the created webhook. Only used when `controlType: "webhook"`. |
| `roles` | array of `roleEntry` | none | Permission overrides on this specific channel. |
| `commands` | array of strings | none | Poracle commands to run **as the channel** (or webhook) immediately after registration. See [Commands](#commands). |
| `threadPicker` | object | none | If set, a button-driven join post is created in this channel after threads are made. See [Thread picker](#thread-picker). |
| `threads` | array of `threadEntry` | none | Private threads under this channel, each registered as its own Poracle target. See [Threads](#threads). |

### Two ways to register a channel with Poracle

`controlType: "bot"` registers the channel itself; the bot posts to it directly via the Discord API. The `humans` row uses `id = <channelID>`, `type = "discord:channel"`.

`controlType: "webhook"` creates a webhook on the channel and registers *that webhook* as the alert target. The `humans` row uses `id = <webhook URL>`, `type = "webhook"`. Use webhooks when you want the messages to come from a per-channel persona ("Apollo Hundos" rather than "Poracle").

### Re-runs

`!autocreate` does **not** detect existing channels. Running the same template twice will create a second category and a second set of channels with the same names — Discord allows that. Don't re-run unless you want duplicates. (Threads are an exception — see [Threads § Re-running](#re-running-threads).)

---

## Role entry

`roleEntry` controls the permission overrides applied to a category, channel, or thread parent. Each entry targets one role by **name** (matched against the guild's existing roles after placeholder expansion). Use `@everyone` to target the guild default role.

```json
{
  "name": "{0}",
  "view": true,
  "viewHistory": true,
  "send": false,
  "react": true
}
```

Each permission flag is a tri-state boolean:

| Flag value | Meaning |
|------------|---------|
| `true` | Allow this permission (overrides any deny inherited from above) |
| `false` | Deny this permission |
| Field absent | Inherit from parent — no override |

### Full permission flag reference

These map 1:1 to Discord's permission bits.

| Field | Discord permission |
|-------|--------------------|
| `view` | View Channel |
| `viewHistory` | Read Message History |
| `send` | Send Messages |
| `react` | Add Reactions |
| `pingEveryone` | Mention @everyone, @here, and All Roles |
| `embedLinks` | Embed Links |
| `attachFiles` | Attach Files |
| `sendTTS` | Send Text-to-Speech Messages |
| `externalEmoji` | Use External Emoji |
| `externalStickers` | Use External Stickers |
| `createPublicThreads` | Create Public Threads |
| `createPrivateThreads` | Create Private Threads |
| `sendThreads` | Send Messages in Threads |
| `slashCommands` | Use Application Commands |
| `connect` | Connect (voice) |
| `speak` | Speak (voice) |
| `autoMic` | Use Voice Activity |
| `stream` | Video / Go Live |
| `vcActivities` | Use Activities (voice) |
| `prioritySpeaker` | Priority Speaker |
| `createInvite` | Create Invite |
| `channels` | Manage Channels |
| `messages` | Manage Messages |
| `roles` | Manage Roles |
| `webhooks` | Manage Webhooks |
| `threads` | Manage Threads |
| `events` | Manage Events |
| `mute` | Mute Members (voice) |
| `deafen` | Deafen Members (voice) |
| `move` | Move Members (voice) |

### Pattern: hide a channel from everyone except one role

```json
"roles": [
  { "name": "@everyone", "view": false },
  { "name": "{0}", "view": true, "viewHistory": true, "send": false, "react": true }
]
```

`@everyone` denies view, the named role allows view + viewHistory but not send. The role is built from arg `{0}`, so `!autocreate area apollo` would target a role literally named `apollo`.

---

## Commands

`commands` is an array of bot command strings (without the `!` prefix). They run **immediately after the channel is registered**, as the channel's Poracle target. Commands execute through the same registry the user-facing bot uses, so anything you can type as `!command args` can go in this list.

```json
"commands": [
  "area add {0}",
  "track everything iv100",
  "raid level5 level6 clean",
  "egg level5 level6 clean"
]
```

Commands are executed in order. `area add` is typically first, since other tracking commands warn about "no area set" if it isn't.

The autocreate runner refreshes the human row from the DB between commands, so a later `!track` correctly sees the area set by an earlier `!area add`.

### What gets echoed to the calling channel

Each command echoes a `>>> Executing <expanded text>` line followed by the bot's normal reply. For threads, the echo reads `>>> [<threadName>] <expanded text>`.

---

## Threads

A `threads` block under a `channelEntry` creates one **private thread** per entry under that parent text channel.

```json
"threads": [
  {
    "name": "{0}-Hundo",
    "buttonLabel": "💯 Hundos",
    "buttonStyle": "success",
    "commands": [
      "area add {0}",
      "track everything iv100"
    ]
  },
  {
    "name": "{0}-Nundo",
    "buttonLabel": "🥚 Nundos",
    "buttonStyle": "danger",
    "commands": [
      "area add {0}",
      "track everything iv0-0"
    ]
  }
]
```

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `name` | string | required | Thread name. Placeholders allowed (`{0}`, `{1}`, …). The thread name is appended to the parent's args as the next placeholder, so commands inside the thread can reference it. |
| `buttonLabel` | string | falls back to `name` | Text shown on the picker button. Placeholders allowed. |
| `buttonStyle` | `"primary"` / `"secondary"` / `"success"` / `"danger"` | `"secondary"` | Discord button style. |
| `commands` | array of strings | none | Same shape as channel-level `commands`, but executed against the thread's Poracle target (`type: "discord:thread"`). |

### What `!autocreate` does for threads

For each entry, in order:

1. Creates a private thread under the parent channel with `auto_archive_duration = 7d`.
2. Inserts a `humans` row with `id = <thread channel ID>`, `type = "discord:thread"`, `current_profile_no = 1`, default profile created.
3. Runs the configured `commands` against that target.
4. Persists the entry into `config/.cache/autocreate-threads.json` so subsequent runs can be idempotent.

### Re-running threads

Unlike top-level channels, the thread block **is** idempotent. The cache stores `(parent channel ID, button label, thread ID, style)` tuples. On a re-run, threads whose label already exists are skipped (the `>> Thread X already cached (id); skipping create` line is printed) and only the picker post is re-rendered — useful when you change a button style or add a new thread to an existing master.

The cache file is at `config/.cache/autocreate-threads.json`. Delete it to force a fresh run; existing Discord threads won't be deleted by the bot, only orphaned in the cache.

### Permission requirements for threads

The bot must have **Create Private Threads** and **Manage Threads** on the parent channel (or the role it's running as must inherit them). Discord enforces parent-channel View Channel for thread visibility — when a user loses the role granting view of the master channel, they automatically lose access to its threads, so no Poracle-side reconciliation is needed.

---

## Thread picker

When `threadPicker` is set on a channel that also has a `threads` block, `!autocreate` posts (or edits) a message in the parent channel containing the embed plus one button per thread.

```json
"threadPicker": {
  "embedTitle": "Area alerts for {0}",
  "embedDescription": "Click the buttons below to activate the private thread for the alerts you want to follow.",
  "pinned": true
}
```

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `embedTitle` | string | none | Title line on the picker embed. Placeholders allowed. Only the first picker message carries the embed if multiple are needed. |
| `embedDescription` | string | none | Description body. Placeholders allowed. |
| `pinned` | bool | `false` | If true, the picker (first message) is pinned to the channel after posting. |

### How the buttons work

Each button's `custom_id` encodes the master channel ID and the thread ID directly (`poracle:thread:<masterID>:<threadID>:join`). The handler is stateless — it survives bot restarts, no warm state needed. When a user clicks:

1. The bot verifies the user has **View Channel** on the master channel (the picker's home). If not → ephemeral 🙅 reply, no action.
2. If the user is already a member of the thread → ephemeral 👌 "you're already in" reply.
3. Otherwise the user is added to the thread via `ThreadMemberAdd` and gets an ephemeral ✅ reply.

### How many buttons can I have?

Discord allows 5 buttons per row, 5 rows per message. The picker generates rows of 5 then chunks into multiple messages above 25; only the first message carries the embed. There's no hard cap — 30, 50, 100 thread entries all work. 25 buttons per master is plenty for almost every real deployment, though.

### Picker idempotency

On a re-run, the bot:
- Edits the existing picker message(s) in place if the cache holds their IDs.
- Sends additional messages if the thread count grew past the previous picker capacity.
- Deletes stale messages if the thread count shrank.

Edit failures fall through to "send fresh"; delete failures are logged but don't abort.

---

## Worked example

`config/channelTemplate.json`:

```json
[
  {
    "name": "area",
    "definition": {
      "category": {
        "categoryName": "{0}",
        "roles": [{ "name": "@everyone", "view": false }]
      },
      "channels": [
        {
          "channelName": "{0}_master",
          "channelType": "text",
          "topic": "Master area channel for {0}",
          "controlType": "bot",
          "roles": [
            { "name": "@everyone", "view": true, "viewHistory": true, "send": false, "react": true }
          ],
          "commands": ["area add {0}"],
          "threadPicker": {
            "embedTitle": "Area alerts for {0}",
            "embedDescription": "Click to activate a private thread for the alerts you want.",
            "pinned": true
          },
          "threads": [
            {
              "name": "{0}-Hundo",
              "buttonLabel": "💯 Hundos",
              "buttonStyle": "success",
              "commands": ["area add {0}", "track everything iv100"]
            },
            {
              "name": "{0}-PVP",
              "buttonLabel": "🛡 Top PVP",
              "buttonStyle": "primary",
              "commands": ["area add {0}", "track everything great5 ultra5"]
            }
          ]
        }
      ]
    }
  }
]
```

Invocation:
```
!autocreate area apollo
```

Result:
- Discord category `apollo`, hidden from `@everyone`.
- Text channel `apollo_master` inside it, role-configured for read+react but no send.
- The master channel registered with Poracle as a `discord:channel` target with area `apollo`.
- Two private threads (`apollo-Hundo`, `apollo-PVP`), each registered as a `discord:thread` target with their own tracking rules.
- A pinned picker post in `apollo_master` with two buttons (`💯 Hundos` / `🛡 Top PVP`); clicking either invites the user to that thread.

---

## Troubleshooting

### "I can't find that channel template!"
The first arg of `!autocreate` doesn't match any `name` in the array. Names are case-sensitive.

### "No channel templates defined"
`config/channelTemplate.json` doesn't exist. Place it relative to your processor `BaseDir`, not next to the binary.

### "No guild has been set"
You ran the command in DMs without `guild<id>`. Either run inside a guild channel or add `guild<123…>` to the args.

### Thread created but commands didn't run
Look for `>>> [<threadName>] <command>` lines in the channel where you ran `!autocreate`. If they're absent, the command parser failed — check the bot logs for `Unknown command: …`. If they're present but the command's reply is missing, the command itself failed — the bot logs the underlying error at `Errorf` level.

### `!tracked` from inside a thread shows "You're not tracking any pokemon" but the autocreate said it added them
This was a bug pre-`e49b235` — the autocreated human's `current_profile_no` defaulted to 0 while the autocreate-time inserts went to profile 1. Fixed: the autocreate path now explicitly sets `current_profile_no = 1`. If you have legacy `humans` rows from before that fix, run `UPDATE humans SET current_profile_no = 1 WHERE current_profile_no = 0` against the affected IDs.

### Picker post isn't appearing
Check the bot's permissions on the master channel — it needs **Send Messages** and **Embed Links**. The picker emit failure is logged at warn level; threads are still created and registered.

### Cache file
`config/.cache/autocreate-threads.json` is created on first thread-picker run. Inspect it to see what the bot thinks exists. Delete to force fresh thread creation on the next `!autocreate` run.
