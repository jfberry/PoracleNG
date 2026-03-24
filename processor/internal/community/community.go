// Package community implements area security logic for filtering geofence
// areas and calculating location restrictions based on community membership.
// Ported from alerter/src/lib/communityLogic.js.
package community

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/config"
)

// FilterAreas returns only those areas that are allowed by the user's community membership.
// If the user belongs to communities "newyork" and "chicago", the returned areas
// are the intersection of the input areas with the union of allowedAreas from both communities.
func FilterAreas(communities []config.CommunityConfig, membership []string, areas []string) []string {
	allowed := make(map[string]bool)
	for _, memberCommunity := range membership {
		comm := findCommunity(communities, memberCommunity)
		if comm == nil {
			continue
		}
		for _, a := range comm.AllowedAreas {
			allowed[strings.ToLower(a)] = true
		}
	}

	var result []string
	for _, a := range areas {
		if allowed[strings.ToLower(a)] {
			result = append(result, a)
		}
	}
	return result
}

// CalculateLocationRestrictions returns the set of location fence names
// for the given community membership. Used for strict_locations enforcement.
func CalculateLocationRestrictions(communities []config.CommunityConfig, membership []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, memberCommunity := range membership {
		comm := findCommunity(communities, memberCommunity)
		if comm == nil {
			continue
		}
		for _, fence := range comm.LocationFence {
			lower := strings.ToLower(fence)
			if !seen[lower] {
				seen[lower] = true
				result = append(result, lower)
			}
		}
	}
	return result
}

// AddCommunity adds a community to the membership list, validates against config,
// and returns the updated sorted list.
func AddCommunity(communities []config.CommunityConfig, existing []string, toAdd string) []string {
	lower := strings.ToLower(toAdd)
	result := make([]string, 0, len(existing)+1)
	found := false
	for _, e := range existing {
		result = append(result, e)
		if e == lower {
			found = true
		}
	}
	if !found {
		result = append(result, lower)
	}
	return validateCommunities(communities, result)
}

// RemoveCommunity removes a community from the membership list, validates against config,
// and returns the updated sorted list.
func RemoveCommunity(communities []config.CommunityConfig, existing []string, toRemove string) []string {
	lower := strings.ToLower(toRemove)
	var result []string
	for _, e := range existing {
		if e != lower {
			result = append(result, e)
		}
	}
	return validateCommunities(communities, result)
}

// IsTelegramCommunityAdmin checks if a Telegram user ID is an admin in any community.
// Returns the list of communities they admin, or nil if none.
func IsTelegramCommunityAdmin(communities []config.CommunityConfig, telegramID string) []string {
	var result []string
	for _, comm := range communities {
		for _, admin := range comm.Telegram.Admins {
			if admin == telegramID {
				result = append(result, strings.ToLower(comm.Name))
				break
			}
		}
	}
	return result
}

// findCommunity looks up a community by name (case-insensitive).
func findCommunity(communities []config.CommunityConfig, name string) *config.CommunityConfig {
	lower := strings.ToLower(name)
	for i := range communities {
		if strings.ToLower(communities[i].Name) == lower {
			return &communities[i]
		}
	}
	return nil
}

// validateCommunities filters the list to only include names that exist in config,
// and returns a sorted result.
func validateCommunities(communities []config.CommunityConfig, membership []string) []string {
	valid := make(map[string]bool, len(communities))
	for _, c := range communities {
		valid[strings.ToLower(c.Name)] = true
	}
	var result []string
	for _, m := range membership {
		if valid[m] {
			result = append(result, m)
		}
	}
	// Sort for consistent output
	sortStrings(result)
	return result
}

// sortStrings sorts a string slice in place (simple insertion sort, lists are tiny).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
