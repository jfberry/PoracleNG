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
