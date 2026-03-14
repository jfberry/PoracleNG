package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Raid builds enrichment fields for a raid or egg webhook.
func (e *Enricher) Raid(raid *webhook.RaidWebhook, firstNotification bool) map[string]interface{} {
	m := make(map[string]interface{})
	m["firstNotification"] = firstNotification

	tz := geo.GetTimezone(raid.Latitude, raid.Longitude)

	addSunTimes(m, raid.Latitude, raid.Longitude, tz)

	if raid.PokemonID > 0 {
		// Hatched raid: disappearTime from end, tth from now to end
		m["disappearTime"] = geo.FormatTime(raid.End, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(raid.End)

		// Weather change time: the hour boundary before end
		weatherChangeTS := raid.End - (raid.End % 3600)
		m["weatherChangeTime"] = geo.FormatTime(weatherChangeTS, tz, e.TimeLayout)
	} else {
		// Egg: hatchTime from start, tth from now to start
		m["hatchTime"] = geo.FormatTime(raid.Start, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(raid.Start)
	}

	// Format RSVP timeslots
	if len(raid.RSVPs) > 0 {
		rsvpTimes := make([]map[string]interface{}, len(raid.RSVPs))
		for i, r := range raid.RSVPs {
			rsvpTimes[i] = map[string]interface{}{
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
