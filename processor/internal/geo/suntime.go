package geo

import (
	"time"

	"github.com/kixorz/suncalc"
)

// SunTimes holds pre-computed day/night phase flags for a given time and location.
type SunTimes struct {
	NightTime bool `json:"nightTime"`
	DawnTime  bool `json:"dawnTime"`
	DuskTime  bool `json:"duskTime"`
}

// ComputeSunTimes determines whether the given unix timestamp at lat/lon
// falls in night, dawn (sunrise to sunrise+1h), or dusk (sunset-1h to sunset).
// Mirrors the logic in nightTime.js.
// The timezone string is used to convert the check time to the correct local
// calendar day for sunrise/sunset calculation.
func ComputeSunTimes(unixSeconds int64, lat, lon float64, timezone string) SunTimes {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	checkTime := time.Unix(unixSeconds, 0).In(loc)

	times := suncalc.GetTimes(checkTime, lat, lon)

	sunrise := times[suncalc.Sunrise].Value
	sunset := times[suncalc.Sunset].Value
	dawnEnd := sunrise.Add(1 * time.Hour)
	duskStart := sunset.Add(-1 * time.Hour)

	isNight := !isBetween(checkTime, sunrise, sunset)
	isDawn := isBetween(checkTime, sunrise, dawnEnd)
	isDusk := isBetween(checkTime, duskStart, sunset)

	return SunTimes{
		NightTime: isNight,
		DawnTime:  isDawn,
		DuskTime:  isDusk,
	}
}

func isBetween(t, start, end time.Time) bool {
	return (t.Equal(start) || t.After(start)) && (t.Equal(end) || t.Before(end))
}
