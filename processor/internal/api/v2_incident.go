package api

import (
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// incident is a v2-only tracking type for pokestop EVENTS (Showcase, Kecleon,
// etc.). It stores rows in the SAME `invasion` table as invasion tracking,
// discriminated by grunt_type being a known event name (see isEventGruntType in
// v2_invasion.go). The matcher already resolves event invasions to their event
// name (matching.ResolveGruntTypeName), so an event-name grunt_type row matches
// without any matcher change.

// eventNameToDisplayType builds the reverse "lowercased event name →
// display_type" map from gd.Util.PokestopEvent. Used by v2IncidentToRule.
func eventNameToDisplayType(gd *gamedata.GameData) map[string]int {
	out := map[string]int{}
	if gd == nil || gd.Util == nil {
		return out
	}
	for dt, evt := range gd.Util.PokestopEvent {
		if evt.Name == "" {
			continue
		}
		out[strings.ToLower(evt.Name)] = dt
	}
	return out
}

// v2IncidentRule is the strict v2 incident (pokestop-event) tracking rule.
//
// display_type is REQUIRED — the pokestop-event id (gd.Util.PokestopEvent key).
// It down-translates to grunt_type = lower(event name) and gender = 0; the row
// lands in the shared invasion table and is read back / matched by that name.
type v2IncidentRule struct {
	DisplayType int `json:"display_type" required:"true" doc:"Pokestop-event id (game-master, e.g. a Showcase/Kecleon event; no wildcard — required). Resolves to the event's lowercased name as the stored grunt_type. (required)"`

	// Common fields. invasion (shared table) HAS a clean column.
	Distance *int    `json:"distance,omitempty" nullable:"true" doc:"Radius in metres around the anchor location. Omit (or 0) to match by the profile's geofence areas instead of a radius — 0 means area-based, NOT zero metres (stored as 0). Returned as null when at its wildcard."`
	Template *string `json:"template,omitempty" nullable:"true" doc:"DTS template name. Omit (or empty) to use the server's configured default template (stored as \"\"). Returned as null when at its wildcard."`
	Clean    *bool   `json:"clean,omitempty" nullable:"true" doc:"Auto-delete the alert on expiry (clean bitmask bit 1). Omit to disable (default false). Returned as null when false."`
	Edit     *bool   `json:"edit,omitempty" nullable:"true" doc:"Keep the message updated in place (clean bitmask bit 2). Omit to disable (default false). Returned as null when false."`
	Summary  *bool   `json:"summary,omitempty" nullable:"true" doc:"Route into the summary digest (clean bitmask bit 4). Omit to disable (default false). Returned as null when false."`

	OverrideLocationLabel *string  `json:"override_location_label,omitempty" nullable:"true" doc:"Saved-location label to use instead of the profile location (requires distance > 0; mutually exclusive with override_areas). Omit for none. Returned as null when unset."`
	OverrideAreas         []string `json:"override_areas,omitempty" doc:"Restrict this rule to these geofence areas (mutually exclusive with distance > 0 and override_location_label). Omit for none. Returned as null when unset."`
}

// translateV2Incident converts a strict v2 incident rule into the stored
// InvasionTrackingAPI: grunt_type = lower(PokestopEvent[display_type].Name),
// gender = 0. Unknown display_type ⇒ 422. ping "" (server-managed); clean packs
// the bitmask.
func translateV2Incident(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *v2IncidentRule) (db.InvasionTrackingAPI, error) {
	gd := gdFromDeps(deps)
	if gd == nil || gd.Util == nil {
		return db.InvasionTrackingAPI{}, huma.Error422UnprocessableEntity("unknown display_type")
	}
	evt, ok := gd.Util.PokestopEvent[req.DisplayType]
	if !ok || evt.Name == "" {
		return db.InvasionTrackingAPI{}, huma.Error422UnprocessableEntity("unknown display_type")
	}
	gruntType := strings.ToLower(evt.Name)

	distance := valueOr(req.Distance, 0)
	const maxDistance = 40000000 // Earth circumference (metres)
	if distance > maxDistance {
		distance = maxDistance
	}

	overrideLabel := valueOr(req.OverrideLocationLabel, "")
	if msg, code := validateOverrideFields(deps, oc, humanID, overrideLabel, req.OverrideAreas, distance); msg != "" {
		return db.InvasionTrackingAPI{}, humaErr(code, msg)
	}

	row := db.InvasionTrackingAPI{
		ID:                    humanID,
		ProfileNo:             profileNo,
		Ping:                  "", // server-managed
		Template:              valueOr(req.Template, ""),
		Distance:              distance,
		Gender:                0, // events are genderless
		GruntType:             gruntType,
		Clean:                 packClean(valueOr(req.Clean, false), valueOr(req.Edit, false), valueOr(req.Summary, false)),
		OverrideLocationLabel: overrideLabel,
		OverrideAreas:         normalizeOverrideAreas(req.OverrideAreas),
	}
	return row, nil
}

// v2IncidentToRule converts a stored (event) InvasionTrackingAPI back into the
// strict v2 incident rule shape, recovering display_type from the event name.
func v2IncidentToRule(gd *gamedata.GameData, row *db.InvasionTrackingAPI) v2IncidentRule {
	rule := v2IncidentRule{
		Distance:              ptrUnless(row.Distance, 0),
		Template:              ptrUnless(row.Template, ""),
		Clean:                 ptrUnless(db.IsClean(row.Clean), false),
		Edit:                  ptrUnless(db.IsEdit(row.Clean), false),
		Summary:               ptrUnless(db.IsSummary(row.Clean), false),
		OverrideLocationLabel: ptrUnless(row.OverrideLocationLabel, ""),
		OverrideAreas:         ptrUnlessSlice(row.OverrideAreas),
	}
	// display_type is required (the mode-defining field) and always present.
	if dt, ok := eventNameToDisplayType(gd)[strings.ToLower(row.GruntType)]; ok {
		rule.DisplayType = dt
	}
	return rule
}

// RegisterV2TrackingIncident registers the strict v2 incident tracking endpoints.
// The Filter restricts every read path to EVENT rows, so incident and invasion
// (its inverse Filter) partition the shared invasion table.
func RegisterV2TrackingIncident(api huma.API, deps *TrackingDeps) {
	gd := gdFromDeps(deps)
	registerV2Tracking(api, deps, v2TrackingType[v2IncidentRule, db.InvasionTrackingAPI]{
		Name: "incident",
		Store: func(d *TrackingDeps) store.TrackingStore[db.InvasionTrackingAPI] {
			return d.Tracking.Invasions
		},
		Translate: translateV2Incident,
		ToRule: func(row *db.InvasionTrackingAPI) v2IncidentRule {
			return v2IncidentToRule(gd, row)
		},
		GetUID: store.InvasionGetUID,
		SetUID: store.InvasionSetUID,
		RowText: func(d *TrackingDeps, tr *i18n.Translator, row *db.InvasionTrackingAPI) string {
			return d.RowText.InvasionRowText(tr, toInvasionTracking(row))
		},
		Filter: func(row *db.InvasionTrackingAPI) bool {
			return isEventGruntType(row.GruntType, gd)
		},
	})
}
