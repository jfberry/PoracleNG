# Phase 4: Discord Reconciliation & Discord-Specific Commands

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete Phase 4 by implementing Discord reconciliation (periodic role sync + event-driven updates) and all Discord-specific commands, enabling full alerter removal.

**Architecture:** Reconciliation runs inside the discordgo bot as periodic goroutines and event handlers. Discord-specific commands use the discordgo session directly for API calls (channel creation, role management, webhook management). All commands use the existing bot framework (CommandContext, Reply).

**Tech Stack:** discordgo, existing bot framework, existing delivery dispatcher

---

## What Exists

- Discord bot gateway with messageCreate handler, command routing, role fetching
- Bot framework: parser, arg matchers, registry, permissions, target resolution
- Community logic: AddCommunity, RemoveCommunity, CalculateLocationRestrictions
- DTS template rendering for greetings
- Delivery dispatcher for sending messages

## What's Needed

### Discord Reconciliation (HIGH priority)

The reconciliation system syncs Discord role membership with Poracle registration. It has three modes:

**1. Periodic sync** (`syncDiscordRole`, every `check_role_interval` hours):
- Load all `discord:user` humans from DB
- Fetch all guild members across all configured guilds (paginated, 15s stagger between guilds)
- For each user: compare role membership against `user_role` list (or community roles for area security)
- Actions: create/reactivate/disable/update users based on role changes
- Sync `blocked_alerts` from `command_security` config
- Sync channel names/notes if configured

**2. Event-driven** (`guildMemberUpdate`, `guildMemberRemove`):
- `guildMemberUpdate`: when a member's roles change, trigger `reconcileSingleUser`
- `guildMemberRemove`: when a member leaves, trigger reconciliation
- `channelDelete`: auto-disable deleted channels

**3. Channel reconciliation** (`syncDiscordChannels`):
- Verify registered `discord:channel` entries still exist
- Update names/notes, disable missing channels

### Config Fields Needed

```go
type ReconciliationConfig struct {
    Discord ReconciliationDiscordConfig `toml:"discord"`
    Telegram ReconciliationTelegramConfig `toml:"telegram"`
}

type ReconciliationDiscordConfig struct {
    UpdateUserNames          bool `toml:"update_user_names"`
    RemoveInvalidUsers       bool `toml:"remove_invalid_users"`
    RegisterNewUsers         bool `toml:"register_new_users"`
    UpdateChannelNames       bool `toml:"update_channel_names"`
    UpdateChannelNotes       bool `toml:"update_channel_notes"`
    UnregisterMissingChannels bool `toml:"unregister_missing_channels"`
}
```

Also needed on DiscordConfig:
```go
Guilds                []string            `toml:"guilds"`
UserRole              []string            `toml:"user_role"`
CheckRole             bool                `toml:"check_role"`
CheckRoleInterval     int                 `toml:"check_role_interval"` // hours
LostRoleMessage       string              `toml:"lost_role_message"`
DisableAutoGreetings  bool                `toml:"disable_auto_greetings"`
UserRoleSubscription  map[string]RoleSubscription `toml:"user_role_subscription"`
DmLogChannelID        string              `toml:"dm_log_channel_id"`
```

### Discord-Specific Commands

| Command | Lines (JS) | Complexity | Description |
|---------|-----------|-----------|-------------|
| `!channel` | 100 | MEDIUM | Register/unregister Discord channels and webhooks as tracking targets |
| `!webhook` | 115 | MEDIUM | Discord webhook API management (create, list) |
| `!role` | 157 | HIGH | Multi-guild role management with exclusive role sets |
| `!autocreate` | 283 | VERY HIGH | Bulk channel/role creation from JSON templates |
| `!poracle-clean` | 25 | LOW | Delete bot's recent messages in a channel |
| `!poracle-emoji` | 132 | MEDIUM | Upload uicons emojis to guild, generate emoji.json |
| `!poracle-id` | 51 | LOW | Export guild emoji/role IDs |

### Telegram Reconciliation

| Item | Lines (JS) | Complexity | Description |
|------|-----------|-----------|-------------|
| Telegram reconciliation | 310 | MEDIUM | Group membership verification via getChatMember |

---

## Task 1: Reconciliation Config

**Files:**
- Modify: `processor/internal/config/config.go`

Add ReconciliationConfig, extend DiscordConfig with Guilds, UserRole, CheckRole, CheckRoleInterval, LostRoleMessage, DisableAutoGreetings, UserRoleSubscription, DmLogChannelID.

---

## Task 2: Discord Reconciliation Core

**Files:**
- Create: `processor/internal/discordbot/reconciliation.go`

Port `discordReconciliation.js` to Go. Key methods:

```go
type Reconciliation struct {
    session      *discordgo.Session
    db           *sqlx.DB
    cfg          *config.Config
    translations *i18n.Bundle
    dts          *dts.TemplateStore
    dispatcher   *delivery.Dispatcher
    // greeting rate limiting
    greetingCount          int
    lastGreetingMinute     int64
}

func (r *Reconciliation) SyncDiscordRole()      // periodic full sync
func (r *Reconciliation) ReconcileSingleUser(id) // event-driven single user
func (r *Reconciliation) SyncDiscordChannels()   // channel name/note sync
func (r *Reconciliation) LoadAllGuildUsers()     // paginated member fetch
func (r *Reconciliation) ReconcileUser(...)      // core logic per user
func (r *Reconciliation) DisableUser(user)       // disable or delete based on roleCheckMode
func (r *Reconciliation) SendGreetings(id)       // DTS greeting via DM (rate limited)
func (r *Reconciliation) SendGoodbye(id)         // lost role message
func (r *Reconciliation) RemoveRoles(user)       // remove subscription roles
```

Key behaviors to port exactly:
- **Greeting rate limit**: max 10 per minute to avoid Discord ban
- **Guild stagger**: 15s delay between guild member fetches
- **blocked_alerts sync**: derive from command_security per user's roles
- **Area security mode**: rebuild community_membership from roles
- **Non-area-security mode**: simple user_role membership check
- **roleCheckMode**: "ignore" (log only), "disable-user" (set admin_disable), "delete" (remove entirely)

---

## Task 3: Discord Event Handlers

**Files:**
- Modify: `processor/internal/discordbot/bot.go`

Add event handlers:
```go
session.AddHandler(b.onGuildMemberUpdate)
session.AddHandler(b.onGuildMemberRemove)
session.AddHandler(b.onChannelDelete)
```

- `onGuildMemberUpdate`: call `reconciliation.ReconcileSingleUser(memberID, cfg.Reconciliation.Discord.RemoveInvalidUsers)`
- `onGuildMemberRemove`: same
- `onChannelDelete`: disable channel in DB if registered

---

## Task 4: Periodic Reconciliation Loop

**Files:**
- Modify: `processor/internal/discordbot/bot.go`

Start a goroutine that runs `SyncDiscordRole()` every `check_role_interval` hours (if `check_role` is enabled). Also run `SyncDiscordChannels()` in the same cycle.

---

## Task 5: !channel command

**Files:**
- Create: `processor/internal/discordbot/channel.go`

Port alerter's channel.js. This is Discord-specific (uses discordgo session).

Subcommands:
- `!channel add <name> <channelID>` — register a channel
- `!channel add name<webhookname> <webhookURL>` — register a webhook
- `!channel remove <channelID|webhookname>` — unregister

Uses `s.Channel(id)` to validate channel exists. Inserts into humans table with type `discord:channel` or `webhook`.

Note: this command needs the discordgo session, so it lives in the discordbot package, not the shared commands package. Register it directly in the Discord bot's command handler.

---

## Task 6: !webhook command

**Files:**
- Create: `processor/internal/discordbot/webhook.go`

Port alerter's webhook.js. Discord-specific.

- `!webhook list` — list webhooks in current channel
- `!webhook create <name>` — create a Discord webhook in current channel

Uses `s.ChannelWebhooks(channelID)` and `s.WebhookCreate(channelID, name, "")`.

---

## Task 7: !role command

**Files:**
- Create: `processor/internal/discordbot/role.go`

Port alerter's role.js + discordRoleSetter.js. The most complex Discord-specific command.

- `!role` — list available roles across guilds
- `!role <rolename>` — toggle role on/off
- Handles exclusive role sets (mutually exclusive roles)
- Multi-guild: iterate all guilds in `user_role_subscription`
- Uses `s.GuildMember()`, `s.GuildMemberRoleAdd()`, `s.GuildMemberRoleRemove()`

---

## Task 8: !poracle-clean command

**Files:**
- Create: `processor/internal/discordbot/clean.go`

Simple: fetch last 100 messages, delete those authored by the bot.

```go
messages, _ := s.ChannelMessages(channelID, 100, "", "", "")
for _, msg := range messages {
    if msg.Author.ID == s.State.User.ID {
        s.ChannelMessageDelete(channelID, msg.ID)
    }
}
```

---

## Task 9: !poracle-id command

**Files:**
- Create: `processor/internal/discordbot/id_export.go`

Export guild emojis and roles as text file.

```go
emojis, _ := s.GuildEmojis(guildID)
roles, _ := s.GuildRoles(guildID)
// Format as "emoji_name: emoji_id" and "role_name: role_id"
```

---

## Task 10: !poracle-emoji command

**Files:**
- Create: `processor/internal/discordbot/emoji.go`

Port alerter's poracle-emoji.js:
- Check configured image URL is a uicons repository
- Download emoji images for types, weather, lures, teams
- Upload to Discord guild via `s.GuildEmojiCreate()`
- Generate emoji.json config with Discord emoji references
- Support overwrite mode

---

## Task 11: !autocreate command

**Files:**
- Create: `processor/internal/discordbot/autocreate.go`

The most complex Discord-specific command. Port alerter's autocreate.js:
- Load channel template from `config/channelTemplate.json`
- Create categories with permissions
- Create channels with role overwrites
- Create roles if they don't exist
- Map Discord permission flags
- Execute sub-commands per created channel
- Parameter substitution `{0}`, `{1}` in template strings

---

## Task 12: Telegram Reconciliation

**Files:**
- Create: `processor/internal/telegrambot/reconciliation.go`

Port alerter's telegramReconciliation.js:
- Periodic check: verify registered telegram users are still members of configured groups
- Uses `getChatMember` Bot API call
- Actions: disable/delete users who left groups
- Simpler than Discord (no roles, just group membership)

---

## Task 13: DM Log Channel

**Files:**
- Modify: `processor/internal/discordbot/bot.go`

When `dm_log_channel_id` is configured, log incoming DMs to that Discord channel for admin visibility.

---

## Task 14: Remaining Phase 3 Commands

While we're here, also implement the remaining Phase 3 commands:

- `!backup` — query all tracking tables, write JSON to `backups/` directory
- `!restore` — read JSON from `backups/`, delete existing, insert restored
- `!apply` — execute commands as other users (dynamic command dispatch)
- `!broadcast` — location-based mass messaging

---

## Implementation Order

1. **Config** (Task 1) — foundation for everything
2. **Reconciliation core** (Task 2) — most critical, highest complexity
3. **Event handlers + periodic loop** (Tasks 3-4) — wire reconciliation to bot
4. **!channel** (Task 5) — needed for channel tracking
5. **!poracle-clean, !poracle-id** (Tasks 8-9) — simple, quick wins
6. **!role** (Task 7) — needed for managed communities
7. **!webhook** (Task 6) — less common but needed
8. **!poracle-emoji** (Task 10) — nice-to-have
9. **!autocreate** (Task 11) — least common, most complex
10. **Telegram reconciliation** (Task 12) — simpler, can parallel
11. **DM log** (Task 13) — nice-to-have
12. **Remaining commands** (Task 14) — backup/restore/apply/broadcast

---

## Design Notes

### Discord-Specific Commands Architecture

Discord-specific commands need the `*discordgo.Session` which is NOT available in the shared `bot.CommandContext`. Two approaches:

**Option A**: Add session to CommandContext (leaks Discord-specific dependency into shared package)
**Option B**: Handle Discord commands separately in the bot handler before/after the shared command framework

**Recommended: Option B**. In `discordbot/bot.go`, check if the command key matches a Discord-specific command BEFORE delegating to the shared registry. The Discord-specific commands are implemented in the `discordbot` package and have direct access to the session.

```go
// In onMessageCreate, after parsing:
if b.handleDiscordCommand(s, m, cmd, ctx) {
    continue // handled by Discord-specific handler
}
// Fall through to shared command registry
handler := b.registry.Lookup(cmd.CommandKey)
```

### Reconciliation Greeting via Dispatcher

The alerter sent greetings directly via Discord.js. In Go, use the existing delivery dispatcher to send the rendered greeting DTS template, ensuring it goes through the same rate-limiting and retry logic as alert messages.

### Testing Strategy

- Reconciliation: test with a small guild, verify user creation/disable/delete
- Channel command: test register/unregister, verify tracking works on registered channel
- Role command: test in a guild with configured subscription roles
- Parity test: compare behavior with alerter running side-by-side
