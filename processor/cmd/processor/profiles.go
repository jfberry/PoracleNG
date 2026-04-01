package main

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geo"
)

// profileScheduleMinutes are the minutes past each hour when the profile
// scheduler runs. Includes quarter-hour marks (:15, :45) so that users who
// set profile switches at common quarter-hour intervals see them activate
// promptly rather than waiting up to 5 minutes.
var profileScheduleMinutes = []int{0, 10, 15, 20, 30, 40, 45, 50}

// runProfileScheduler checks at fixed minutes past each hour for profiles
// with active_hours rules that match the current local time, and switches
// users to those profiles. Wall-clock alignment ensures the 10-minute
// matching window works reliably.
func (ps *ProcessorService) runProfileScheduler() {
	for {
		now := time.Now()
		next := nextScheduleTime(now, profileScheduleMinutes)
		time.Sleep(next.Sub(now))

		ps.checkProfileSwitches()
	}
}

// nextScheduleTime returns the next wall-clock time at one of the given
// minute marks.
func nextScheduleTime(now time.Time, minutes []int) time.Time {
	nowMin := now.Minute()
	nowSec := now.Second()*1e9 + now.Nanosecond()

	for _, m := range minutes {
		if m > nowMin || (m == nowMin && nowSec == 0) {
			return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), m, 0, 0, now.Location())
		}
	}
	// Wrap to first slot of next hour
	return time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, minutes[0], 0, 0, now.Location())
}

func (ps *ProcessorService) checkProfileSwitches() {
	st := ps.stateMgr.Get()
	if st == nil {
		return
	}

	// Collect profiles with active hours, ordered by profile_no per user.
	// The map iteration order is random, so collect and sort.
	type profileEntry struct {
		profile *db.Profile
	}
	var candidates []profileEntry
	for _, p := range st.Profiles {
		if len(p.ParsedActiveHours) > 0 {
			candidates = append(candidates, profileEntry{profile: p})
		}
	}

	if len(candidates) == 0 {
		return
	}

	log.Debug("Profile scheduler: checking for active profile changes")

	needsReload := false
	activated := make(map[string]bool) // track first match per user ID

	for _, c := range candidates {
		p := c.profile

		if activated[p.ID] {
			continue
		}

		human, ok := st.Humans[p.ID]
		if !ok {
			continue
		}

		if !isProfileActive(p.ParsedActiveHours, human.Latitude, human.Longitude) {
			continue
		}

		activated[p.ID] = true

		if human.CurrentProfileNo == p.ProfileNo {
			continue
		}

		log.Infof("Profile scheduler: setting %s to profile %d - %s", p.ID, p.ProfileNo, p.Name)

		_, err := ps.database.Exec(
			"UPDATE humans SET current_profile_no = ?, area = ?, latitude = ?, longitude = ? WHERE id = ?",
			p.ProfileNo, p.Area, p.Latitude, p.Longitude, p.ID,
		)
		if err != nil {
			log.Errorf("Profile scheduler: failed to update human %s: %s", p.ID, err)
			continue
		}

		needsReload = true

		// Send notification
		tr := ps.translations.For(human.Language)
		msg := tr.Tf("profile.switched", p.Name)

		ps.dispatchMessage(human.ID, human.Type, human.Name, msg, "ProfileScheduler")
	}

	if needsReload {
		ps.triggerReload()
	}
}

// isProfileActive checks whether any active_hours entry matches the current
// local time at the given coordinates.
func isProfileActive(entries []db.ActiveHourEntry, lat, lon float64) bool {
	var now time.Time
	if lat != 0 || lon != 0 {
		tz := geo.GetTimezone(lat, lon)
		loc, err := time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
		now = time.Now().In(loc)
	} else {
		now = time.Now().UTC()
	}

	nowHour := now.Hour()
	nowMin := now.Minute()
	nowDow := isoDow(now.Weekday())
	yesterdayDow := nowDow - 1
	if yesterdayDow < 1 {
		yesterdayDow = 7
	}

	for _, e := range entries {
		if matchesTimeWindow(e, nowDow, yesterdayDow, nowHour, nowMin) {
			return true
		}
	}
	return false
}

// matchesTimeWindow implements the same 10-minute window logic as the original implementation:
//   - Same day, same hour, within 10 minutes of scheduled minute
//   - First 10 minutes of new hour matching last 10 minutes of previous hour
//   - Midnight boundary (hour 0, minute <10, yesterday at hour 23, minute >50)
func matchesTimeWindow(e db.ActiveHourEntry, nowDow, yesterdayDow, nowHour, nowMin int) bool {
	// Within 10 minutes in same hour
	if e.Day == nowDow && e.Hours == nowHour && nowMin >= e.Mins && (nowMin-e.Mins) < 10 {
		return true
	}
	// First 10 minutes of new hour, schedule was in last 10 minutes of previous hour
	if nowMin < 10 && e.Day == nowDow && e.Hours == nowHour-1 && e.Mins > 50 {
		return true
	}
	// Midnight boundary
	if nowHour == 0 && nowMin < 10 && e.Day == yesterdayDow && e.Hours == 23 && e.Mins > 50 {
		return true
	}
	return false
}

// isoDow converts Go's time.Weekday (Sunday=0) to ISO weekday (Monday=1, Sunday=7).
func isoDow(wd time.Weekday) int {
	if wd == time.Sunday {
		return 7
	}
	return int(wd)
}
