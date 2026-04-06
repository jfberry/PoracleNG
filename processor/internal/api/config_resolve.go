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
)

// ResolveDeps holds dependencies for the resolve handler.
type ResolveDeps struct {
	DiscordSession *discordgo.Session          // nil if Discord not configured
	TelegramAPI    *tgbotapi.BotAPI            // nil if Telegram not configured
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
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}

		result := gin.H{"status": "ok"}

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
