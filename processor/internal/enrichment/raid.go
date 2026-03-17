package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Raid builds enrichment fields for a raid or egg webhook.
func (e *Enricher) Raid(raid *webhook.RaidWebhook, firstNotification bool) map[string]any {
	m := make(map[string]any)
	m["firstNotification"] = firstNotification

	tz := geo.GetTimezone(raid.Latitude, raid.Longitude)

	addSunTimes(m, raid.Latitude, raid.Longitude, tz)

	// Cell weather
	cellID := tracker.GetWeatherCellID(raid.Latitude, raid.Longitude)
	m["gameWeatherId"] = e.WeatherProvider.GetCurrentWeatherInCell(cellID)

	if raid.PokemonID > 0 {
		// Hatched raid: disappearTime from end, tth from now to end
		m["disappearTime"] = geo.FormatTime(raid.End, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(raid.End)

		// Weather change time: the hour boundary before end
		weatherChangeTS := raid.End - (raid.End % 3600)
		m["weatherChangeTime"] = geo.FormatTime(weatherChangeTS, tz, e.TimeLayout)

		// Weather forecast for boost change detection (triggers AccuWeather fetch if configured)
		forecast := e.GetForecast(cellID)
		m["weatherForecastCurrent"] = forecast.Current
		m["weatherForecastNext"] = forecast.Next
		m["nextHourTimestamp"] = tracker.GetNextHourTimestamp()
	} else {
		// Egg: hatchTime from start, tth from now to start
		m["hatchTime"] = geo.FormatTime(raid.Start, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(raid.Start)
	}

	// Format RSVP timeslots
	if len(raid.RSVPs) > 0 {
		rsvpTimes := make([]map[string]any, len(raid.RSVPs))
		for i, r := range raid.RSVPs {
			rsvpTimes[i] = map[string]any{
				"timeslot":    r.Timeslot,
				"going_count": r.GoingCount,
				"maybe_count": r.MaybeCount,
				"time":        geo.FormatTime(r.Timeslot/1000, tz, e.TimeLayout),
			}
		}
		m["rsvps"] = rsvpTimes
	}

	return m
}
