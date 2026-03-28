package matching

import (
	"fmt"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// InvasionData holds processed invasion data for matching.
type InvasionData struct {
	PokestopID string
	GruntType  string // lowercased grunt type string
	Gender     int
	Latitude   float64
	Longitude  float64
}

// InvasionMatcher performs in-memory invasion matching.
type InvasionMatcher struct {
	StrictLocations     bool
	AreaSecurityEnabled bool
}

// Match returns all matched users for an invasion.
func (m *InvasionMatcher) Match(data *InvasionData, st *state.State) []webhook.MatchedUser {
	if st == nil {
		return nil
	}

	matchedAreaNames := st.Geofence.MatchedAreaNames(data.Latitude, data.Longitude)
	var trackings []trackingUserData

	gruntType := strings.ToLower(data.GruntType)

	for _, inv := range st.Invasions {
		// grunt_type match OR 'everything'
		invGrunt := strings.ToLower(inv.GruntType)
		if !(invGrunt == gruntType || invGrunt == "everything") {
			continue
		}
		// gender match OR 0 (any)
		if !(inv.Gender == data.Gender || inv.Gender == 0) {
			continue
		}

		trackings = append(trackings, trackingUserData{
			HumanID:   inv.ID,
			ProfileNo: inv.ProfileNo,
			Distance:  inv.Distance,
			Template:  inv.Template,
			Clean:     inv.Clean,
			Ping:      inv.Ping,
		})
	}

	return ValidateHumansGeneric(
		trackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"invasion",
	)
}

// ResolveGruntTypeName returns the type name for matching against tracking rules.
// The !invasion command stores the lowercased English type name (e.g. "electric", "water"),
// event name (e.g. "kecleon"), or "everything" as a catch-all.
// This function maps the numeric grunt/display IDs → name via GameData.
func ResolveGruntTypeName(gruntTypeID, displayType int, gd *gamedata.GameData) string {
	// Event invasions (Kecleon, Showcase, etc.) — match by event name
	if displayType >= 7 && gd != nil {
		if evtInfo, ok := gd.Util.PokestopEvent[displayType]; ok {
			return strings.ToLower(evtInfo.Name)
		}
		return fmt.Sprintf("e%d", displayType)
	}
	if gruntTypeID == 0 {
		return "0"
	}
	// Regular grunts — match by pokemon type name or template-derived name
	if gd != nil {
		if grunt, ok := gd.Grunts[gruntTypeID]; ok {
			// Typed grunts (Electric, Water, etc.) — resolve via TypeID
			if grunt.TypeID > 0 {
				if typeInfo, ok := gd.Types[grunt.TypeID]; ok {
					return strings.ToLower(typeInfo.Name)
				}
			}
			// Untyped grunts (Metal, Darkness, Mixed) — derive from template
			return gamedata.TypeNameFromTemplate(grunt.Template)
		}
	}
	return fmt.Sprintf("%d", gruntTypeID)
}
