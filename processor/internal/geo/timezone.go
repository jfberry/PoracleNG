package geo

import (
	"time"

	"github.com/ringsaturn/tzf"
)

var finder tzf.F

func init() {
	var err error
	finder, err = tzf.NewDefaultFinder()
	if err != nil {
		panic("failed to initialize timezone finder: " + err.Error())
	}
}

// TTH represents time-to-hatch/despawn remaining.
type TTH struct {
	Days              int  `json:"days"`
	Hours             int  `json:"hours"`
	Minutes           int  `json:"minutes"`
	Seconds           int  `json:"seconds"`
	FirstDateWasLater bool `json:"firstDateWasLater"`
}

// GetTimezone returns the IANA timezone name for a lat/lon.
func GetTimezone(lat, lon float64) string {
	tz := finder.GetTimezoneName(lon, lat)
	if tz == "" {
		return "UTC"
	}
	return tz
}

// TimezoneSource discriminates where ResolveTimezone got its answer.
// Display callers (e.g. !profile / !summary show responses) use it to
// pick the right i18n key for the explanation text — the geo package
// stays free of message strings.
type TimezoneSource int

const (
	// TimezoneFromLocation: lat/lon was non-zero and tzf resolved a
	// valid IANA name from it.
	TimezoneFromLocation TimezoneSource = iota
	// TimezoneFromDefault: lat/lon was zero (or tzf failed) and the
	// operator's configured default timezone was used.
	TimezoneFromDefault
	// TimezoneFromServerLocal: lat/lon was zero AND defaultTZ was
	// empty (or unparseable) — fell back to time.Local.
	TimezoneFromServerLocal
)

// ResolveTimezone returns the *time.Location to use for scheduling
// against a human, the IANA name (or server-local-equivalent), and
// the source kind so callers can render a translated explanation.
//
// Resolution order:
//  1. lat/lon non-zero → tzf lookup
//  2. defaultTZ non-empty → time.LoadLocation(defaultTZ)
//  3. fallback to time.Local (server's timezone)
//
// When tzf or LoadLocation fail, we degrade silently to the next step
// so a malformed config / unknown name can't break the scheduler.
func ResolveTimezone(lat, lon float64, defaultTZ string) (loc *time.Location, name string, source TimezoneSource) {
	if lat != 0 || lon != 0 {
		tz := finder.GetTimezoneName(lon, lat)
		if tz != "" {
			if l, err := time.LoadLocation(tz); err == nil {
				return l, tz, TimezoneFromLocation
			}
		}
	}
	if defaultTZ != "" {
		if l, err := time.LoadLocation(defaultTZ); err == nil {
			return l, defaultTZ, TimezoneFromDefault
		}
	}
	return time.Local, time.Local.String(), TimezoneFromServerLocal
}

// FormatTime formats a unix timestamp using the given Go layout in the specified timezone.
func FormatTime(unixSeconds int64, timezone string, goLayout string) string {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	t := time.Unix(unixSeconds, 0).In(loc)
	return t.Format(goLayout)
}

// FormatNow formats the current time using the given Go layout in the specified timezone.
func FormatNow(timezone string, goLayout string) string {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Format(goLayout)
}

// ComputeTTH computes time remaining from now to a target unix timestamp.
func ComputeTTH(targetUnix int64) TTH {
	now := time.Now().Unix()
	diff := targetUnix - now
	if diff <= 0 {
		return TTH{FirstDateWasLater: true}
	}
	days := int(diff / 86400)
	diff -= int64(days) * 86400
	hours := int(diff / 3600)
	diff -= int64(hours) * 3600
	minutes := int(diff / 60)
	seconds := int(diff - int64(minutes)*60)
	return TTH{
		Days:    days,
		Hours:   hours,
		Minutes: minutes,
		Seconds: seconds,
	}
}

// EndOfDay returns the end-of-day (23:59:59) unix timestamp in the timezone of the given lat/lon.
func EndOfDay(lat, lon float64) int64 {
	tz := GetTimezone(lat, lon)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, loc)
	return endOfDay.Unix()
}

// NextHourBoundary returns the unix timestamp of the next hour boundary from now.
func NextHourBoundary() int64 {
	now := time.Now().Unix()
	currentHour := now - (now % 3600)
	return currentHour + 3600
}
