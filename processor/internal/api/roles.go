package api

import (
	"net/http"

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/discordroles"
)

// RoleDeps holds dependencies for the role API endpoints.
type RoleDeps struct {
	// SessionFunc returns the Discord session, or nil if the bot is not running.
	// This is a function rather than a direct pointer because the Discord bot may
	// be initialized after routes are registered.
	SessionFunc func() *discordgo.Session
	Config      *config.Config
	DB          *sqlx.DB
}

// session returns the current Discord session from the deps, or nil.
func (d *RoleDeps) session() *discordgo.Session {
	if d.SessionFunc == nil {
		return nil
	}
	return d.SessionFunc()
}

// HandleGetRoles returns the GET /api/humans/{id}/roles handler.
// Lists available roles for a Discord user across all configured guilds.
func HandleGetRoles(deps *RoleDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := db.SelectOneHuman(deps.DB, id)
		if err != nil {
			log.Errorf("Roles API: get human: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		if human.Type != "discord:user" {
			c.Data(http.StatusOK, "application/json", []byte("[]"))
			return
		}

		if deps.session() == nil {
			trackingJSONError(c, http.StatusServiceUnavailable, "Discord bot not available")
			return
		}

		roleSubMap := deps.Config.Discord.RoleSubscriptionMap()
		guilds, err := discordroles.ListUserRolesAcrossGuilds(deps.session(), roleSubMap, human.ID)
		if err != nil {
			log.Errorf("Roles API: list roles: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "failed to list roles")
			return
		}

		trackingJSONOK(c, map[string]any{"guilds": guilds})
	}
}

// HandleAddRole returns the POST /api/humans/{id}/roles/add/{roleId} handler.
func HandleAddRole(deps *RoleDeps) gin.HandlerFunc {
	return handleRoleChange(deps, true)
}

// HandleRemoveRole returns the POST /api/humans/{id}/roles/remove/{roleId} handler.
func HandleRemoveRole(deps *RoleDeps) gin.HandlerFunc {
	return handleRoleChange(deps, false)
}

func handleRoleChange(deps *RoleDeps, add bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		roleID := c.Param("roleId")
		if roleID == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing roleId parameter")
			return
		}

		human, err := db.SelectOneHuman(deps.DB, id)
		if err != nil {
			log.Errorf("Roles API: get human: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		if human.Type != "discord:user" {
			c.Data(http.StatusOK, "application/json", []byte("[]"))
			return
		}

		if deps.session() == nil {
			trackingJSONError(c, http.StatusServiceUnavailable, "Discord bot not available")
			return
		}

		roleSubMap := deps.Config.Discord.RoleSubscriptionMap()
		changes, err := discordroles.SetUserRole(deps.session(), roleSubMap, human.ID, roleID, add)
		if err != nil {
			log.Errorf("Roles API: set role: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "failed to set role")
			return
		}

		trackingJSONOK(c, map[string]any{"result": changes})
	}
}

// adminRolesResult holds the administration roles response for each platform.
type adminRolesResult struct {
	Discord  *discordAdminRoles  `json:"discord,omitempty"`
	Telegram *telegramAdminRoles `json:"telegram,omitempty"`
}

type discordAdminRoles struct {
	Channels []string `json:"channels"`
	Webhooks []string `json:"webhooks"`
	Users    bool     `json:"users"`
}

type telegramAdminRoles struct {
	Channels []string `json:"channels"`
	Users    bool     `json:"users"`
}

// HandleGetAdministrationRoles returns the GET /api/humans/{id}/getAdministrationRoles handler.
// Checks delegated administration config to determine what the user can manage.
func HandleGetAdministrationRoles(deps *RoleDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := db.SelectOneHuman(deps.DB, id)
		if err != nil {
			log.Errorf("Roles API: get human for admin roles: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		result := adminRolesResult{}

		// Discord administration roles
		if hasDiscordToken(deps.Config) {
			discord := &discordAdminRoles{
				Channels: []string{},
				Webhooks: []string{},
				Users:    false,
			}

			da := deps.Config.Discord.DelegatedAdministration
			var userRoles []string
			rolesFetched := false

			fetchRoles := func() []string {
				if rolesFetched {
					return userRoles
				}
				rolesFetched = true
				if deps.session() != nil {
					roles, err := discordroles.GetUserRoleIDs(deps.session(), deps.Config.Discord.Guilds, id)
					if err != nil {
						log.Warnf("Roles API: get user roles for %s: %v", id, err)
					} else {
						userRoles = roles
					}
				}
				return userRoles
			}

			// Channel tracking permissions
			if len(da.ChannelTracking) > 0 {
				roles := fetchRoles()
				rolesAndID := append(roles, id)
				rolesAndIDSet := make(map[string]bool, len(rolesAndID))
				for _, r := range rolesAndID {
					rolesAndIDSet[r] = true
				}

				var allChannels map[string][]discordroles.ChannelInfo
				channelsFetched := false

				for targetID, allowedIDs := range da.ChannelTracking {
					if !containsAny(allowedIDs, rolesAndIDSet) {
						continue
					}

					// Lazily fetch all channels
					if !channelsFetched && deps.session() != nil {
						channelsFetched = true
						allChannels, err = discordroles.GetAllChannelIDs(deps.session(), deps.Config.Discord.Guilds)
						if err != nil {
							log.Warnf("Roles API: get all channels: %v", err)
						}
					}

					// Check if targetID is a guild — push all channels in that guild
					for _, guildID := range deps.Config.Discord.Guilds {
						if targetID == guildID {
							if chs, ok := allChannels[guildID]; ok {
								for _, ch := range chs {
									discord.Channels = append(discord.Channels, ch.ID)
								}
							}
						}
					}

					// Check if targetID is a category or channel in any guild
					for _, guildID := range deps.Config.Discord.Guilds {
						chs, ok := allChannels[guildID]
						if !ok {
							continue
						}
						// Check if it's a category
						for _, ch := range chs {
							if ch.CategoryID == targetID {
								discord.Channels = append(discord.Channels, ch.ID)
							}
						}
						// Check if it's a channel directly
						for _, ch := range chs {
							if ch.ID == targetID {
								discord.Channels = append(discord.Channels, targetID)
								break
							}
						}
					}
				}
			}

			// Webhook tracking permissions
			if len(da.WebhookTracking) > 0 {
				roles := fetchRoles()
				rolesSet := make(map[string]bool, len(roles))
				for _, r := range roles {
					rolesSet[r] = true
				}

				for webhookName, allowedIDs := range da.WebhookTracking {
					// Check by user ID
					for _, aid := range allowedIDs {
						if aid == id {
							discord.Webhooks = append(discord.Webhooks, webhookName)
							break
						}
					}
					// Check by role
					if !contains(discord.Webhooks, webhookName) {
						for _, aid := range allowedIDs {
							if rolesSet[aid] {
								discord.Webhooks = append(discord.Webhooks, webhookName)
								break
							}
						}
					}
				}
			}

			// User tracking permissions
			if len(da.UserTracking) > 0 {
				roles := fetchRoles()
				rolesAndID := append(roles, id)
				rolesAndIDSet := make(map[string]bool, len(rolesAndID))
				for _, r := range rolesAndID {
					rolesAndIDSet[r] = true
				}
				for _, allowedID := range da.UserTracking {
					if rolesAndIDSet[allowedID] {
						discord.Users = true
						break
					}
				}
			}

			result.Discord = discord
		}

		// Telegram administration roles
		if hasTelegramToken(deps.Config) {
			telegram := &telegramAdminRoles{
				Channels: []string{},
				Users:    false,
			}

			tda := deps.Config.Telegram.DelegatedAdministration

			// Channel tracking — Telegram uses user ID only (no roles)
			if len(tda.ChannelTracking) > 0 {
				for channelID, allowedIDs := range tda.ChannelTracking {
					for _, aid := range allowedIDs {
						if aid == id {
							telegram.Channels = append(telegram.Channels, channelID)
							break
						}
					}
				}
			}

			// User tracking
			if len(tda.UserTracking) > 0 {
				for _, aid := range tda.UserTracking {
					if aid == id {
						telegram.Users = true
						break
					}
				}
			}

			result.Telegram = telegram
		}

		trackingJSONOK(c, map[string]any{"admin": result})
	}
}

// hasDiscordToken returns true if a Discord token is configured.
func hasDiscordToken(cfg *config.Config) bool {
	tokens := cfg.Discord.DiscordTokens()
	return len(tokens) > 0 && tokens[0] != ""
}

// hasTelegramToken returns true if a Telegram token is configured.
func hasTelegramToken(cfg *config.Config) bool {
	tokens := cfg.Telegram.TelegramTokens()
	return len(tokens) > 0 && tokens[0] != ""
}

// containsAny returns true if any element of allowed is in the set.
func containsAny(allowed []string, set map[string]bool) bool {
	for _, a := range allowed {
		if set[a] {
			return true
		}
	}
	return false
}

// contains checks if a string slice contains a value.
func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
