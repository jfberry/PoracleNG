package commands

import (
	"fmt"
	"time"

	"github.com/pokemon/poracleng/processor/internal/state"
)

// formatDuration formats a duration in a compact, operator-readable
// way: "2d 3h 14m" / "3h 14m 12s" / "12s". Rounded to seconds.
//
// Shared by poracle_admin_status.go (uptime, rate-limit TTL) and
// poracle_admin_warnings.go (entry timestamps).
func formatDuration(d time.Duration) string {
	d = max(d.Round(time.Second), 0)
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	mins := int(d / time.Minute)
	d -= time.Duration(mins) * time.Minute
	secs := int(d / time.Second)

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh %dm %ds", hours, mins, secs)
	case mins > 0:
		return fmt.Sprintf("%dm %ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// countTrackingRules returns the total number of tracking rules across all
// types in the given state snapshot. Monsters uses its pre-computed Total
// field; all other types are plain slices.
//
// Shared by poracle_admin_status.go (status snapshot totals) and
// poracle_admin_reload.go (post-reload confirmation message).
func countTrackingRules(s *state.State) int {
	if s == nil {
		return 0
	}
	total := 0
	if s.Monsters != nil {
		total += s.Monsters.Total
	}
	total += len(s.Raids)
	total += len(s.Eggs)
	total += len(s.Invasions)
	total += len(s.Quests)
	total += len(s.Lures)
	total += len(s.Gyms)
	total += len(s.Nests)
	total += len(s.Forts)
	total += len(s.Maxbattles)
	return total
}
