// Package discordroles provides shared Discord role management functions
// used by both the !role bot command and the API role endpoints.
package discordroles

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
)

// GuildRoleInfo holds role listing info for a guild.
type GuildRoleInfo struct {
	Name  string          `json:"name"`
	Roles GuildRolesGroup `json:"roles"`
}

// GuildRolesGroup holds the exclusive and general roles for a guild.
type GuildRolesGroup struct {
	Exclusive [][]RoleInfo `json:"exclusive"`
	General   []RoleInfo   `json:"general"`
}

// RoleInfo holds a single role's listing info.
type RoleInfo struct {
	Description string `json:"description"`
	ID          string `json:"id"`
	Set         bool   `json:"set"`
}

// ListUserRolesAcrossGuilds returns available roles for a user across all configured guilds.
// The response format matches the alerter's DiscordRoleSetter.list().
func ListUserRolesAcrossGuilds(s *discordgo.Session, roleSubMap map[string]config.RoleSubscriptionEntry, userID string) ([]GuildRoleInfo, error) {
	var result []GuildRoleInfo

	for guildID, entry := range roleSubMap {
		guild, err := s.Guild(guildID)
		if err != nil {
			log.Warnf("discordroles: fetch guild %s: %v", guildID, err)
			continue
		}

		member, err := s.GuildMember(guildID, userID)
		if err != nil {
			log.Debugf("discordroles: fetch member %s in guild %s: %v", userID, guildID, err)
			continue
		}

		memberRoles := make(map[string]bool, len(member.Roles))
		for _, r := range member.Roles {
			memberRoles[r] = true
		}

		info := GuildRoleInfo{
			Name: guild.Name,
			Roles: GuildRolesGroup{
				Exclusive: make([][]RoleInfo, 0),
				General:   make([]RoleInfo, 0),
			},
		}

		// Exclusive role sets
		for _, exSet := range entry.ExclusiveRoles {
			var exGroup []RoleInfo
			for desc, roleID := range exSet {
				exGroup = append(exGroup, RoleInfo{
					Description: desc,
					ID:          roleID,
					Set:         memberRoles[roleID],
				})
			}
			info.Roles.Exclusive = append(info.Roles.Exclusive, exGroup)
		}

		// General roles
		for desc, roleID := range entry.Roles {
			info.Roles.General = append(info.Roles.General, RoleInfo{
				Description: desc,
				ID:          roleID,
				Set:         memberRoles[roleID],
			})
		}

		result = append(result, info)
	}

	if result == nil {
		result = []GuildRoleInfo{}
	}
	return result, nil
}

// SetUserRole adds or removes a Discord role from a user by role ID.
// It searches all guilds in the role subscription map to find matching roles.
// For exclusive sets, adding a role automatically removes others in the same set.
// Returns the list of role changes made (matching the alerter's DiscordRoleSetter.setRole format).
func SetUserRole(s *discordgo.Session, roleSubMap map[string]config.RoleSubscriptionEntry, userID, roleID string, add bool) ([]RoleInfo, error) {
	var changes []RoleInfo

	for guildID, entry := range roleSubMap {
		member, err := s.GuildMember(guildID, userID)
		if err != nil {
			// User not in this guild — skip (matching alerter's 404 handling)
			log.Debugf("discordroles: fetch member %s in guild %s: %v", userID, guildID, err)
			continue
		}

		memberRoles := make(map[string]bool, len(member.Roles))
		for _, r := range member.Roles {
			memberRoles[r] = true
		}

		// Check general roles
		for desc, rid := range entry.Roles {
			if rid == roleID {
				if add {
					if err := s.GuildMemberRoleAdd(guildID, userID, roleID); err != nil {
						return nil, fmt.Errorf("add role %s: %w", desc, err)
					}
					changes = append(changes, RoleInfo{Description: desc, ID: roleID, Set: true})
				} else {
					if err := s.GuildMemberRoleRemove(guildID, userID, roleID); err != nil {
						return nil, fmt.Errorf("remove role %s: %w", desc, err)
					}
					changes = append(changes, RoleInfo{Description: desc, ID: roleID, Set: false})
				}
			}
		}

		// Check exclusive role sets
		for _, exSet := range entry.ExclusiveRoles {
			for desc, rid := range exSet {
				if rid == roleID {
					if add {
						// Add this role and remove others in the same exclusive set
						for otherDesc, otherRoleID := range exSet {
							if otherRoleID == roleID {
								if err := s.GuildMemberRoleAdd(guildID, userID, roleID); err != nil {
									return nil, fmt.Errorf("add exclusive role %s: %w", desc, err)
								}
								changes = append(changes, RoleInfo{Description: otherDesc, ID: roleID, Set: true})
							} else if memberRoles[otherRoleID] {
								if err := s.GuildMemberRoleRemove(guildID, userID, otherRoleID); err != nil {
									return nil, fmt.Errorf("remove exclusive role %s: %w", otherDesc, err)
								}
								changes = append(changes, RoleInfo{Description: otherDesc, ID: otherRoleID, Set: false})
							}
						}
					} else {
						if err := s.GuildMemberRoleRemove(guildID, userID, roleID); err != nil {
							return nil, fmt.Errorf("remove role %s: %w", desc, err)
						}
						changes = append(changes, RoleInfo{Description: desc, ID: roleID, Set: false})
					}
				}
			}
		}
	}

	if changes == nil {
		changes = []RoleInfo{}
	}
	return changes, nil
}

// GetUserRoleIDs returns all role IDs a user has across the given guilds.
// Matches the alerter's DiscordUtil.getUserRoles() behavior.
func GetUserRoleIDs(s *discordgo.Session, guildIDs []string, userID string) ([]string, error) {
	var roleIDs []string

	for _, guildID := range guildIDs {
		member, err := s.GuildMember(guildID, userID)
		if err != nil {
			// Unknown user/member in this guild — skip (matching alerter behavior)
			log.Debugf("discordroles: fetch member %s in guild %s for roles: %v", userID, guildID, err)
			continue
		}
		roleIDs = append(roleIDs, member.Roles...)
	}

	if roleIDs == nil {
		roleIDs = []string{}
	}
	return roleIDs, nil
}

// ChannelInfo holds basic Discord channel info for administration role checks.
type ChannelInfo struct {
	ID         string `json:"id"`
	CategoryID string `json:"categoryId"`
}

// GetAllChannelIDs returns all text channel IDs for the given guilds, grouped by guild.
// Matches the alerter's DiscordUtil.getAllChannels() behavior.
func GetAllChannelIDs(s *discordgo.Session, guildIDs []string) (map[string][]ChannelInfo, error) {
	result := make(map[string][]ChannelInfo)

	for _, guildID := range guildIDs {
		channels, err := s.GuildChannels(guildID)
		if err != nil {
			log.Warnf("discordroles: fetch channels for guild %s: %v", guildID, err)
			continue
		}

		var chList []ChannelInfo
		for _, ch := range channels {
			// Include text-based channels (not DM, not category)
			switch ch.Type {
			case discordgo.ChannelTypeGuildText,
				discordgo.ChannelTypeGuildNews,
				discordgo.ChannelTypeGuildNewsThread,
				discordgo.ChannelTypeGuildPublicThread,
				discordgo.ChannelTypeGuildPrivateThread,
				discordgo.ChannelTypeGuildForum:
				chList = append(chList, ChannelInfo{
					ID:         ch.ID,
					CategoryID: ch.ParentID,
				})
			}
		}
		result[guildID] = chList
	}

	return result, nil
}
