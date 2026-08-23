package api

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2GymRule is the strict v2 gym tracking request/response rule object.
//
// team is a REQUIRED string enum (harmony|mystic|valor|instinct|any → 0|1|2|3|4),
// matching v1's gin handler which 400s when team is absent or out of range.
// It is a non-pointer field with required:"true" + the teamEnum string set, so
// an omitted or unknown team is rejected by huma's generated schema (422). The
// remaining filters are optional pointers (omitted ⇒ documented default via
// valueOr). gym has a clean column, so clean/edit/summary bools pack into the
// bitmask. ping is server-managed (not a caller input).
type v2GymRule struct {
	Team string `json:"team" required:"true" enum:"harmony,mystic,valor,instinct,any" doc:"Required controlling team: harmony|mystic|valor|instinct|any (0|1|2|3|4; no wildcard — required). Use 'any' to match regardless of team (stored as 4)."`

	SlotChanges   *bool   `json:"slot_changes,omitempty" nullable:"true" doc:"Alert on slot (defender count) changes. Omit to disable (default false). Returned as null when false."`
	BattleChanges *bool   `json:"battle_changes,omitempty" nullable:"true" doc:"Alert on battle (in-progress) changes. Omit to disable (default false). Returned as null when false."`
	GymID         *string `json:"gym_id,omitempty" nullable:"true" doc:"Restrict to a specific gym id. Omit (or empty/null) to match any gym (stored as null). Returned as null when unset."`

	// Common fields.
	Distance *int    `json:"distance,omitempty" nullable:"true" doc:"Radius in metres around the anchor location. Omit (or 0) to match by the profile's geofence areas instead of a radius — 0 means area-based, NOT zero metres (stored as 0). Returned as null when at its wildcard."`
	Template *string `json:"template,omitempty" nullable:"true" doc:"DTS template name. Omit (or empty) to use the server's configured default template (stored as \"\"). Returned as null when at its wildcard."`
	Clean    *bool   `json:"clean,omitempty" nullable:"true" doc:"Auto-delete the alert on expiry (clean bitmask bit 1). Omit to disable (default false). Returned as null when false."`
	Edit     *bool   `json:"edit,omitempty" nullable:"true" doc:"Keep the message updated in place (clean bitmask bit 2). Omit to disable (default false). Returned as null when false."`
	Summary  *bool   `json:"summary,omitempty" nullable:"true" doc:"Route into the summary digest (clean bitmask bit 4). Omit to disable (default false). Returned as null when false."`

	OverrideLocationLabel *string  `json:"override_location_label,omitempty" nullable:"true" doc:"Saved-location label to use instead of the profile location (requires distance > 0; mutually exclusive with override_areas). Omit for none. Returned as null when unset."`
	OverrideAreas         []string `json:"override_areas,omitempty" doc:"Restrict this rule to these geofence areas (mutually exclusive with distance > 0 and override_location_label). Omit for none. Returned as null when unset."`
}

// translateV2Gym converts a strict v2 gym rule into the stored GymTrackingAPI,
// applying the required team enum→int, documented defaults, the clean bitmask,
// profile, and validated/normalized override fields. ping is always stored ""
// (server-managed).
func translateV2Gym(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *v2GymRule) (db.GymTrackingAPI, error) {
	// team is required and enum-validated by huma; toStored is defence-in-depth.
	team, ok := teamEnum.toStored(req.Team)
	if !ok {
		return db.GymTrackingAPI{}, huma.Error422UnprocessableEntity("Invalid team: " + req.Team)
	}

	distance := valueOr(req.Distance, 0)
	const maxDistance = 40000000 // Earth circumference (metres)
	if distance > maxDistance {
		distance = maxDistance
	}

	overrideLabel := valueOr(req.OverrideLocationLabel, "")
	if msg, code := validateOverrideFields(deps, oc, humanID, overrideLabel, req.OverrideAreas, distance); msg != "" {
		return db.GymTrackingAPI{}, humaErr(code, msg)
	}

	var gymID *string
	if req.GymID != nil && *req.GymID != "" {
		gymID = req.GymID
	}

	row := db.GymTrackingAPI{
		ID:                    humanID,
		ProfileNo:             profileNo,
		Ping:                  "", // server-managed
		Template:              valueOr(req.Template, ""),
		Distance:              distance,
		Team:                  team,
		SlotChanges:           db.IntBool(valueOr(req.SlotChanges, false)),
		BattleChanges:         db.IntBool(valueOr(req.BattleChanges, false)),
		GymID:                 gymID,
		Clean:                 packClean(valueOr(req.Clean, false), valueOr(req.Edit, false), valueOr(req.Summary, false)),
		OverrideLocationLabel: overrideLabel,
		OverrideAreas:         normalizeOverrideAreas(req.OverrideAreas),
	}
	return row, nil
}

// gymRowToRule converts a stored GymTrackingAPI back into the strict v2 rule
// shape for responses.
func gymRowToRule(row *db.GymTrackingAPI) v2GymRule {
	var gymID *string
	if row.GymID != nil && *row.GymID != "" {
		gymID = row.GymID
	}
	return v2GymRule{
		Team:                  teamEnum.fromStored(row.Team), // required, always present
		SlotChanges:           ptrUnless(bool(row.SlotChanges), false),
		BattleChanges:         ptrUnless(bool(row.BattleChanges), false),
		GymID:                 gymID,
		Distance:              ptrUnless(row.Distance, 0),
		Template:              ptrUnless(row.Template, ""),
		Clean:                 ptrUnless(db.IsClean(row.Clean), false),
		Edit:                  ptrUnless(db.IsEdit(row.Clean), false),
		Summary:               ptrUnless(db.IsSummary(row.Clean), false),
		OverrideLocationLabel: ptrUnless(row.OverrideLocationLabel, ""),
		OverrideAreas:         ptrUnlessSlice(row.OverrideAreas),
	}
}

// RegisterV2TrackingGym registers the strict v2 gym tracking endpoints via the
// generic resource helpers.
func RegisterV2TrackingGym(api huma.API, deps *TrackingDeps) {
	registerV2Tracking(api, deps, v2TrackingType[v2GymRule, db.GymTrackingAPI]{
		Name: "gym",
		Store: func(d *TrackingDeps) store.TrackingStore[db.GymTrackingAPI] {
			return d.Tracking.Gyms
		},
		Translate: translateV2Gym,
		ToRule:    gymRowToRule,
		GetUID:    store.GymGetUID,
		SetUID:    store.GymSetUID,
		RowText: func(d *TrackingDeps, tr *i18n.Translator, row *db.GymTrackingAPI) string {
			return d.RowText.GymRowText(tr, toGymTracking(row))
		},
	})
}
