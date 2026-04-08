package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jellydator/ttlcache/v3"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// HumanLookup is the subset of HumanStore needed by the resolve handler —
// just enough to look up a registered destination by its stored ID. Used to
// resolve webhook names (which only exist in the humans table) and to enrich
// Discord/Telegram lookups with PoracleNG-specific metadata (notes, area).
type HumanLookup interface {
	Get(id string) (*store.Human, error)
}

// ResolveDeps holds dependencies for the resolve handler.
type ResolveDeps struct {
	DiscordSession *discordgo.Session          // nil if Discord not configured
	TelegramAPI    *tgbotapi.BotAPI            // nil if Telegram not configured
	Humans         HumanLookup                 // nil if not wired (lookup is skipped)
	Cache          *ttlcache.Cache[string, any]
}

// NewResolveCache creates a ttlcache for resolved IDs with 10 minute TTL.
func NewResolveCache() *ttlcache.Cache[string, any] {
	cache := ttlcache.New[string, any](
		ttlcache.WithTTL[string, any](10 * time.Minute),
	)
	go cache.Start()
	return cache
}

// HandleResolve batch-resolves Discord/Telegram IDs to names.
// POST /api/resolve
//
// The "destinations" array is for IDs of unknown type — the resolver tries
// each one as a Discord user, channel, role, guild, and Telegram chat,
// returning a flat map of "best match" results. Used by the alert_limits
// overrides editor where the target could be any kind of destination.
func HandleResolve(deps ResolveDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Discord *struct {
				Users    []string `json:"users"`
				Roles    []string `json:"roles"`
				Channels []string `json:"channels"`
				Guilds   []string `json:"guilds"`
			} `json:"discord"`
			Telegram *struct {
				Chats []string `json:"chats"`
			} `json:"telegram"`
			Destinations []string `json:"destinations"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}

		result := gin.H{"status": "ok"}

		// Resolve unknown-type destinations by trying every category in turn
		if len(req.Destinations) > 0 {
			destinations := make(map[string]any)
			for _, id := range req.Destinations {
				if resolved := resolveAnyDestination(deps, id); resolved != nil {
					destinations[id] = resolved
				}
			}
			result["destinations"] = destinations
		}

		// Discord resolution
		if req.Discord != nil && deps.DiscordSession != nil {
			discord := make(map[string]any)

			if len(req.Discord.Users) > 0 {
				users := make(map[string]any)
				for _, id := range req.Discord.Users {
					if resolved := resolveDiscordUser(deps, id); resolved != nil {
						users[id] = resolved
					}
				}
				discord["users"] = users
			}

			if len(req.Discord.Roles) > 0 {
				roles := make(map[string]any)
				for _, id := range req.Discord.Roles {
					if resolved := resolveDiscordRole(deps, id); resolved != nil {
						roles[id] = resolved
					}
				}
				discord["roles"] = roles
			}

			if len(req.Discord.Channels) > 0 {
				channels := make(map[string]any)
				for _, id := range req.Discord.Channels {
					if resolved := resolveDiscordChannel(deps, id); resolved != nil {
						channels[id] = resolved
					}
				}
				discord["channels"] = channels
			}

			if len(req.Discord.Guilds) > 0 {
				guilds := make(map[string]any)
				for _, id := range req.Discord.Guilds {
					if resolved := resolveDiscordGuild(deps, id); resolved != nil {
						guilds[id] = resolved
					}
				}
				discord["guilds"] = guilds
			}

			result["discord"] = discord
		}

		// Telegram resolution
		if req.Telegram != nil && deps.TelegramAPI != nil {
			telegram := make(map[string]any)

			if len(req.Telegram.Chats) > 0 {
				chats := make(map[string]any)
				for _, id := range req.Telegram.Chats {
					if resolved := resolveTelegramChat(deps, id); resolved != nil {
						chats[id] = resolved
					}
				}
				telegram["chats"] = chats
			}

			result["telegram"] = telegram
		}

		c.JSON(http.StatusOK, result)
	}
}

// resolveAnyDestination tries to identify a destination ID across every
// known source. Used when the schema field's type is unknown (e.g.
// alert_limits.overrides.target which can be a channel/user ID or a
// webhook name).
//
// Lookup order:
//  1. PoracleNG humans table — covers webhook names and gives PoracleNG-side
//     metadata (Name, Notes, Area) for any registered destination
//  2. Discord (channel, user, role, guild)
//  3. Telegram (chat)
//
// Result includes a "kind" field. The "stale" field is true when the ID
// exists in the humans table but the corresponding platform entity
// (Discord channel/user, Telegram chat) could not be found via the
// platform API — meaning the destination is registered but no longer
// reachable. The editor should warn the user before letting them keep
// stale targets in their config.
//
// "stale" is omitted (not false) when:
//   - The match comes purely from a platform API (no humans entry)
//   - The match is a webhook (no platform API to verify against)
//   - No platform bot is configured for the destination type
func resolveAnyDestination(deps ResolveDeps, id string) map[string]any {
	var humansResult map[string]any
	var humansType string

	// 1. Humans table — handles webhook names and registered destinations
	if deps.Humans != nil {
		if h, err := deps.Humans.Get(id); err == nil && h != nil {
			humansType = h.Type
			humansResult = map[string]any{
				"kind":    h.Type, // e.g. "webhook", "discord:channel", "telegram:user"
				"name":    h.Name,
				"enabled": h.Enabled,
			}
			if h.Notes != "" {
				humansResult["notes"] = h.Notes
			}
			if len(h.Area) > 0 {
				humansResult["areas"] = h.Area
			}
		}
	}

	// 2. Discord — try every category for any unknown ID
	if deps.DiscordSession != nil {
		if r := resolveDiscordChannel(deps, id); r != nil {
			return mergeResolved(humansResult, r, "discord:channel")
		}
		if r := resolveDiscordUser(deps, id); r != nil {
			return mergeResolved(humansResult, r, "discord:user")
		}
		if r := resolveDiscordRole(deps, id); r != nil {
			return mergeResolved(humansResult, r, "discord:role")
		}
		if r := resolveDiscordGuild(deps, id); r != nil {
			return mergeResolved(humansResult, r, "discord:guild")
		}
	}

	// 3. Telegram — for chat IDs not in humans
	if deps.TelegramAPI != nil {
		if r := resolveTelegramChat(deps, id); r != nil {
			return mergeResolved(humansResult, r, "telegram:chat")
		}
	}

	// Platform lookup failed. If the humans table had a match AND the
	// destination type SHOULD have been verifiable via platform API, mark
	// it stale so the editor can warn the user.
	if humansResult != nil {
		if isPlatformVerifiable(humansType, deps) {
			humansResult["stale"] = true
		}
		return humansResult
	}

	return nil
}

// isPlatformVerifiable returns true if the given humans.type can be checked
// against a platform API and the relevant bot is configured. Used to decide
// whether a missing platform lookup means "stale" or "no platform to ask".
func isPlatformVerifiable(humansType string, deps ResolveDeps) bool {
	switch humansType {
	case "discord:user", "discord:channel", "discord:thread":
		return deps.DiscordSession != nil
	case "telegram:user", "telegram:channel", "telegram:group":
		return deps.TelegramAPI != nil
	}
	// "webhook" and any unknown type can't be verified — don't claim stale
	return false
}

// mergeResolved combines a humans-table base record with a platform API
// lookup. Platform-API fields fill in any blanks but don't overwrite
// PoracleNG-side fields (name from humans table is the user's chosen
// label, which is more useful than the platform display name).
func mergeResolved(base, platform map[string]any, kind string) map[string]any {
	if base == nil {
		platform["kind"] = kind
		return platform
	}
	for k, v := range platform {
		if _, exists := base[k]; !exists {
			base[k] = v
		}
	}
	if _, hasKind := base["kind"]; !hasKind {
		base["kind"] = kind
	}
	return base
}

// cachedResolve checks the cache before calling the platform API.
// Returns nil if the fetch returns nil (not cached).
func cachedResolve(deps ResolveDeps, key string, fetch func() map[string]any) map[string]any {
	if item := deps.Cache.Get(key); item != nil {
		if v, ok := item.Value().(map[string]any); ok {
			return v
		}
	}
	val := fetch()
	if val != nil {
		deps.Cache.Set(key, val, ttlcache.DefaultTTL)
	}
	return val
}

func resolveDiscordUser(deps ResolveDeps, id string) map[string]any {
	return cachedResolve(deps, "discord:user:"+id, func() map[string]any {
		user, err := deps.DiscordSession.User(id)
		if err != nil {
			return nil
		}
		result := map[string]any{"name": user.Username}
		if user.GlobalName != "" {
			result["globalName"] = user.GlobalName
		}
		return result
	})
}

func resolveDiscordRole(deps ResolveDeps, id string) map[string]any {
	return cachedResolve(deps, "discord:role:"+id, func() map[string]any {
		// Search all guilds the bot is in
		for _, guild := range deps.DiscordSession.State.Guilds {
			roles, err := deps.DiscordSession.GuildRoles(guild.ID)
			if err != nil {
				continue
			}
			for _, r := range roles {
				if r.ID == id {
					guildName := guild.Name
					if guildName == "" {
						if g, err := deps.DiscordSession.Guild(guild.ID); err == nil {
							guildName = g.Name
						}
					}
					return map[string]any{
						"name":    r.Name,
						"guild":   guildName,
						"guildId": guild.ID,
					}
				}
			}
		}
		return nil
	})
}

func resolveDiscordChannel(deps ResolveDeps, id string) map[string]any {
	return cachedResolve(deps, "discord:channel:"+id, func() map[string]any {
		ch, err := deps.DiscordSession.Channel(id)
		if err != nil {
			return nil
		}
		result := map[string]any{
			"name": ch.Name,
			"type": channelTypeName(ch.Type),
		}
		if ch.GuildID != "" {
			if g, err := deps.DiscordSession.Guild(ch.GuildID); err == nil {
				result["guild"] = g.Name
				result["guildId"] = ch.GuildID
			}
		}
		if ch.ParentID != "" {
			if parent, err := deps.DiscordSession.Channel(ch.ParentID); err == nil {
				result["categoryName"] = parent.Name
			}
		}
		return result
	})
}

func resolveDiscordGuild(deps ResolveDeps, id string) map[string]any {
	return cachedResolve(deps, "discord:guild:"+id, func() map[string]any {
		g, err := deps.DiscordSession.Guild(id)
		if err != nil {
			return nil
		}
		return map[string]any{"name": g.Name}
	})
}

func resolveTelegramChat(deps ResolveDeps, id string) map[string]any {
	return cachedResolve(deps, "telegram:chat:"+id, func() map[string]any {
		chatID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return nil
		}
		chatCfg := tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}}
		chat, err := deps.TelegramAPI.GetChat(chatCfg)
		if err != nil {
			return nil
		}
		name := chat.FirstName
		if chat.LastName != "" {
			name += " " + chat.LastName
		}
		if name == "" {
			name = chat.Title
		}
		if name == "" {
			name = chat.UserName
		}
		return map[string]any{"name": name, "type": string(chat.Type)}
	})
}

func channelTypeName(t discordgo.ChannelType) string {
	switch t {
	case discordgo.ChannelTypeGuildText:
		return "text"
	case discordgo.ChannelTypeGuildVoice:
		return "voice"
	case discordgo.ChannelTypeGuildCategory:
		return "category"
	case discordgo.ChannelTypeGuildNews:
		return "news"
	case discordgo.ChannelTypeGuildStageVoice:
		return "stage"
	default:
		return fmt.Sprintf("type_%d", t)
	}
}
