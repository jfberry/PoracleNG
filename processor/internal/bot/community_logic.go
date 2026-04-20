package bot

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/config"
)

// AddCommunity adds a community to the membership list if it's valid in config.
func AddCommunity(cfg *config.Config, existing []string, communityToAdd string) []string {
	lower := strings.ToLower(communityToAdd)
	validKeys := communityKeysLower(cfg)

	// Add if not already present
	found := slices.Contains(existing, lower)
	result := existing
	if !found {
		result = append(result, lower)
	}

	// Filter to valid communities only, sort
	var filtered []string
	for _, c := range result {
		if validKeys[c] {
			filtered = append(filtered, c)
		}
	}
	return sortStrings(filtered)
}

// RemoveCommunity removes a community from the membership list.
func RemoveCommunity(cfg *config.Config, existing []string, communityToRemove string) []string {
	lower := strings.ToLower(communityToRemove)
	validKeys := communityKeysLower(cfg)

	var result []string
	for _, c := range existing {
		if c != lower && validKeys[c] {
			result = append(result, c)
		}
	}
	return sortStrings(result)
}

// CalculateLocationRestrictions builds a location fence list from community config.
func CalculateLocationRestrictions(cfg *config.Config, communities []string) []string {
	restrictions := make(map[string]bool)
	for _, comm := range communities {
		for _, cc := range cfg.Area.Communities {
			if strings.EqualFold(cc.Name, comm) {
				for _, fence := range cc.LocationFence {
					restrictions[strings.ToLower(fence)] = true
				}
			}
		}
	}
	var result []string
	for k := range restrictions {
		result = append(result, k)
	}
	return sortStrings(result)
}

// FindCommunityForChannel finds which community a channel belongs to (for registration).
func FindCommunityForChannel(cfg *config.Config, platform, channelID string) string {
	for _, cc := range cfg.Area.Communities {
		var channels []string
		if platform == "discord" {
			channels = cc.Discord.Channels
		} else if platform == "telegram" {
			channels = cc.Telegram.Channels
		}
		if slices.Contains(channels, channelID) {
			return cc.Name
		}
	}
	return ""
}

// IsRegistrationChannel checks if the channel is a valid registration channel.
func IsRegistrationChannel(cfg *config.Config, platform, channelID string) bool {
	if cfg.Area.Enabled {
		return FindCommunityForChannel(cfg, platform, channelID) != ""
	}
	var channels []string
	if platform == "discord" {
		channels = cfg.Discord.Channels
	} else if platform == "telegram" {
		channels = cfg.Telegram.Channels
	}
	return slices.Contains(channels, channelID)
}

// CommunityNames returns all configured community names sorted.
func CommunityNames(cfg *config.Config) []string {
	names := make([]string, len(cfg.Area.Communities))
	for i, cc := range cfg.Area.Communities {
		names[i] = cc.Name
	}
	return sortStrings(names)
}

// ParseCommunityMembership parses the community_membership JSON column.
func ParseCommunityMembership(jsonStr string) []string {
	if jsonStr == "" || jsonStr == "[]" {
		return nil
	}
	var communities []string
	if err := json.Unmarshal([]byte(jsonStr), &communities); err != nil {
		return nil
	}
	return communities
}

func communityKeysLower(cfg *config.Config) map[string]bool {
	keys := make(map[string]bool)
	for _, cc := range cfg.Area.Communities {
		keys[strings.ToLower(cc.Name)] = true
	}
	return keys
}

func sortStrings(s []string) []string {
	if len(s) == 0 {
		return s
	}
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
	return s
}
