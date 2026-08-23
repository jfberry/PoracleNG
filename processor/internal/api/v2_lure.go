package api

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2LureRule is the strict v2 lure tracking request/response rule object.
//
// lure_id is REQUIRED (non-pointer) and must be one of the documented discrete
// set; it is a plain INT (a game-master dictionary value, not a string enum).
// The discrete set is validated explicitly in translateV2Lure because huma's
// min/max/enum can't express a sparse int set cleanly. Common fields are
// optional pointers (omitted ⇒ documented default via valueOr). ping is
// server-managed (not a caller input).
type v2LureRule struct {
	LureID int `json:"lure_id" required:"true" doc:"Lure module id (game-master item id); one of 0 | 501 | 502 | 503 | 504 | 505 | 506 (required). Use the in-set value 0 to match ANY lure type — it is the wildcard, but unlike optional filters this required field has no omit-to-wildcard; send 0 explicitly."`

	// Common fields.
	Distance *int    `json:"distance,omitempty" nullable:"true" doc:"Radius in metres around the anchor location. Omit (or 0) to match by the profile's geofence areas instead of a radius — 0 means area-based, NOT zero metres (stored as 0). Returned as null when at its wildcard."`
	Template *string `json:"template,omitempty" nullable:"true" doc:"DTS template name. Omit (or empty) to use the server's configured default template (stored as \"\"). Returned as null when at its wildcard."`
	Clean    *bool   `json:"clean,omitempty" nullable:"true" doc:"Auto-delete the alert on expiry (clean bitmask bit 1). Omit to disable (default false). Returned as null when false."`
	Edit     *bool   `json:"edit,omitempty" nullable:"true" doc:"Keep the message updated in place (clean bitmask bit 2). Omit to disable (default false). Returned as null when false."`
	Summary  *bool   `json:"summary,omitempty" nullable:"true" doc:"Route into the summary digest (clean bitmask bit 4). Omit to disable (default false). Returned as null when false."`

	OverrideLocationLabel *string  `json:"override_location_label,omitempty" nullable:"true" doc:"Saved-location label to use instead of the profile location (requires distance > 0; mutually exclusive with override_areas). Omit for none. Returned as null when unset."`
	OverrideAreas         []string `json:"override_areas,omitempty" doc:"Restrict this rule to these geofence areas (mutually exclusive with distance > 0 and override_location_label). Omit for none. Returned as null when unset."`
}

// translateV2Lure converts a strict v2 lure rule into the stored LureTrackingAPI,
// applying documented defaults, the clean bitmask, profile, and
// validated/normalized override fields. It rejects an out-of-set lure_id with a
// 422 (matching the v1 handler's validLureIDs guard). ping is always stored ""
// (server-managed).
func translateV2Lure(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *v2LureRule) (db.LureTrackingAPI, error) {
	if !validLureIDs[req.LureID] {
		return db.LureTrackingAPI{}, huma.Error422UnprocessableEntity("Unrecognised lure_id value")
	}

	distance := valueOr(req.Distance, 0)
	const maxDistance = 40000000 // Earth circumference (metres)
	if distance > maxDistance {
		distance = maxDistance
	}

	overrideLabel := valueOr(req.OverrideLocationLabel, "")
	if msg, code := validateOverrideFields(deps, oc, humanID, overrideLabel, req.OverrideAreas, distance); msg != "" {
		return db.LureTrackingAPI{}, humaErr(code, msg)
	}

	row := db.LureTrackingAPI{
		ID:                    humanID,
		ProfileNo:             profileNo,
		Ping:                  "", // server-managed
		Template:              valueOr(req.Template, ""),
		Distance:              distance,
		LureID:                req.LureID,
		Clean:                 packClean(valueOr(req.Clean, false), valueOr(req.Edit, false), valueOr(req.Summary, false)),
		OverrideLocationLabel: overrideLabel,
		OverrideAreas:         normalizeOverrideAreas(req.OverrideAreas),
	}
	return row, nil
}

// lureRowToRule converts a stored LureTrackingAPI back into the strict v2 rule
// shape for responses.
func lureRowToRule(row *db.LureTrackingAPI) v2LureRule {
	return v2LureRule{
		LureID:                row.LureID, // required, always present
		Distance:              ptrUnless(row.Distance, 0),
		Template:              ptrUnless(row.Template, ""),
		Clean:                 ptrUnless(db.IsClean(row.Clean), false),
		Edit:                  ptrUnless(db.IsEdit(row.Clean), false),
		Summary:               ptrUnless(db.IsSummary(row.Clean), false),
		OverrideLocationLabel: ptrUnless(row.OverrideLocationLabel, ""),
		OverrideAreas:         ptrUnlessSlice(row.OverrideAreas),
	}
}

// RegisterV2TrackingLure registers the strict v2 lure tracking endpoints via the
// generic resource helpers.
func RegisterV2TrackingLure(api huma.API, deps *TrackingDeps) {
	registerV2Tracking(api, deps, v2TrackingType[v2LureRule, db.LureTrackingAPI]{
		Name: "lure",
		Store: func(d *TrackingDeps) store.TrackingStore[db.LureTrackingAPI] {
			return d.Tracking.Lures
		},
		Translate: translateV2Lure,
		ToRule:    lureRowToRule,
		GetUID:    store.LureGetUID,
		SetUID:    store.LureSetUID,
		RowText: func(d *TrackingDeps, tr *i18n.Translator, row *db.LureTrackingAPI) string {
			return d.RowText.LureRowText(tr, toLureTracking(row))
		},
	})
}
