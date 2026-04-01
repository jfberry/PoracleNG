package bot

import (
	"encoding/json"

	"github.com/pokemon/poracleng/processor/internal/config"
)

// IsAdmin checks if user is in the admin list for the given platform.
func IsAdmin(cfg *config.Config, platform, userID string) bool {
	var admins []string
	switch platform {
	case "discord":
		admins = cfg.Discord.Admins
	case "telegram":
		admins = cfg.Telegram.Admins
	}
	for _, a := range admins {
		if a == userID {
			return true
		}
	}
	return false
}

// CommandAllowed checks [discord] command_security for a command.
// Returns true if:
//   - No command_security entry exists for this command (unrestricted)
//   - User ID is in the allowed list
//   - Any of the user's roles is in the allowed list
//
// Telegram always returns true (no role-based command security).
func CommandAllowed(cfg *config.Config, platform, cmdKey string, userID string, userRoles []string) bool {
	if platform != "discord" {
		return true // Telegram has no role-based security
	}

	// Map identifier keys to the command_security config keys
	// command_security uses short names like "raid", "monster", "gym"
	securityName := commandSecurityName(cmdKey)
	if securityName == "" {
		return true // no security mapping for this command
	}

	allowedIDs, ok := cfg.Discord.CommandSecurity[securityName]
	if !ok || len(allowedIDs) == 0 {
		return true // no restriction configured
	}

	// Check user ID directly
	for _, id := range allowedIDs {
		if id == userID {
			return true
		}
	}

	// Check user roles
	for _, role := range userRoles {
		for _, id := range allowedIDs {
			if id == role {
				return true
			}
		}
	}

	return false
}

// CalculateChannelPermissions checks delegated_administration.channel_tracking.
// Returns true if the user (by ID or role) is allowed to manage tracking
// in the given channel, guild, or category.
func CalculateChannelPermissions(cfg *config.Config, userID string, userRoles []string, channelID, guildID, categoryID string) bool {
	ct := cfg.Discord.DelegatedAdministration.ChannelTracking
	if len(ct) == 0 {
		return false
	}

	// Check each of: channelID, guildID, categoryID
	for _, targetID := range []string{channelID, guildID, categoryID} {
		if targetID == "" {
			continue
		}
		allowedIDs, ok := ct[targetID]
		if !ok {
			continue
		}
		if containsStr(allowedIDs, userID) {
			return true
		}
		for _, role := range userRoles {
			if containsStr(allowedIDs, role) {
				return true
			}
		}
	}

	return false
}

// CanAdminWebhook checks delegated_administration.webhook_tracking.
func CanAdminWebhook(cfg *config.Config, userID string, webhookName string) bool {
	wt := cfg.Discord.DelegatedAdministration.WebhookTracking
	if len(wt) == 0 {
		return false
	}
	allowedIDs, ok := wt[webhookName]
	if !ok {
		return false
	}
	return containsStr(allowedIDs, userID)
}

// BlockedAlerts derives blocked alert types from command_security configuration.
// For a user who lacks the role for "raid", "monster", etc., those alert types
// are returned as blocked. Used by reconciliation to set blocked_alerts on users.
// Returns nil if no command_security is configured, or a JSON-encoded string pointer
// if any alerts are blocked.
func BlockedAlerts(cfg *config.Config, userID string, userRoles []string) *string {
	if len(cfg.Discord.CommandSecurity) == 0 {
		return nil
	}

	// Alert types and feature keys that map to command_security entries.
	// Matches the alerter's computeBlockedAlerts set.
	commands := []string{"raid", "monster", "gym", "specificgym", "lure", "nest", "egg", "invasion", "pvp", "maxbattle"}

	var blocked []string
	for _, cmd := range commands {
		allowedIDs, ok := cfg.Discord.CommandSecurity[cmd]
		if !ok || len(allowedIDs) == 0 {
			continue // not restricted
		}
		// Check if user has access
		hasAccess := false
		if containsStr(allowedIDs, userID) {
			hasAccess = true
		}
		if !hasAccess {
			for _, role := range userRoles {
				if containsStr(allowedIDs, role) {
					hasAccess = true
					break
				}
			}
		}
		if !hasAccess {
			blocked = append(blocked, cmd)
		}
	}

	if len(blocked) == 0 {
		return nil
	}

	blockedJSON, _ := json.Marshal(blocked)
	s := string(blockedJSON)
	return &s
}

// commandSecurityName maps command identifier keys to command_security config names.
func commandSecurityName(cmdKey string) string {
	switch cmdKey {
	case "cmd.track":
		return "monster"
	case "cmd.raid":
		return "raid"
	case "cmd.egg":
		return "egg"
	case "cmd.quest":
		return "quest"
	case "cmd.gym":
		return "gym"
	case "cmd.lure":
		return "lure"
	case "cmd.invasion", "cmd.incident":
		return "invasion"
	case "cmd.nest":
		return "nest"
	case "cmd.maxbattle":
		return "maxbattle"
	default:
		return ""
	}
}

// CheckFeaturePermission checks whether a user has access to a specific
// command_security feature key (e.g. "pvp", "specificgym"). These are
// per-parameter permissions that restrict specific features within a command,
// not whole-command access.
func CheckFeaturePermission(cfg *config.Config, platform, featureKey, userID string, userRoles []string) bool {
	if platform != "discord" {
		return true
	}
	allowedIDs, ok := cfg.Discord.CommandSecurity[featureKey]
	if !ok || len(allowedIDs) == 0 {
		return true // not restricted
	}
	for _, id := range allowedIDs {
		if id == userID {
			return true
		}
	}
	for _, role := range userRoles {
		for _, id := range allowedIDs {
			if id == role {
				return true
			}
		}
	}
	return false
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
