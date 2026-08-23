package api

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/guregu/null/v6"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2EggRule is the strict v2 egg tracking request/response rule object.
//
// level is REQUIRED (non-pointer, required:"true") and must be >= 1 — v1 returns
// 400 for level < 1; v2 surfaces this as a 422. team and rsvp_changes are STRING
// enums (teamEnum / rsvpChangesEnum), both stored as ints. gym_id is a nullable
// string (null/empty = any). ping is server-managed (not a caller input).
//
// v2 is STRICT and deliberately drops v1's level-array expansion: v1 accepted
// `level` as an int OR [int,...] (one row per level). v2 models level as a
// SINGLE int — a client wanting multiple levels POSTs multiple rule objects (the
// create body is already an array). A level array is now a type mismatch (422).
type v2EggRule struct {
	Level int `json:"level" required:"true" minimum:"1" doc:"Egg tier (required, >= 1; no wildcard — eggs are always tracked by tier). Single int — POST multiple rule objects for multiple tiers."`

	Team      *string `json:"team,omitempty" nullable:"true" enum:"harmony,mystic,valor,instinct,any" doc:"Controlling team: harmony|mystic|valor|instinct|any (0|1|2|3|4). Omit to match any team (defaults to 'any', stored as 4). Returned as null when 'any'."`
	Exclusive *bool   `json:"exclusive,omitempty" nullable:"true" doc:"Match EX-raid eggs only. Omit to match regardless (default false). Returned as null when false."`
	GymID     *string `json:"gym_id,omitempty" nullable:"true" doc:"Restrict to a specific gym id. Omit (or empty/null) to match any gym (stored as null). Returned as null when unset."`

	RSVPChanges *string `json:"rsvp_changes,omitempty" nullable:"true" enum:"none,rsvp,rsvp_only" doc:"RSVP change handling: none|rsvp|rsvp_only (0|1|2). Omit to disable RSVP updates (defaults to 'none', stored as 0). Returned as null when 'none'."`

	// Common fields.
	Distance *int    `json:"distance,omitempty" nullable:"true" doc:"Radius in metres around the anchor location. Omit (or 0) to match by the profile's geofence areas instead of a radius — 0 means area-based, NOT zero metres (stored as 0). Returned as null when at its wildcard."`
	Template *string `json:"template,omitempty" nullable:"true" doc:"DTS template name. Omit (or empty) to use the server's configured default template (stored as \"\"). Returned as null when at its wildcard."`
	Clean    *bool   `json:"clean,omitempty" nullable:"true" doc:"Auto-delete the alert on expiry (clean bitmask bit 1). Omit to disable (default false). Returned as null when false."`
	Edit     *bool   `json:"edit,omitempty" nullable:"true" doc:"Keep the message updated in place (clean bitmask bit 2). Omit to disable (default false). Returned as null when false."`
	Summary  *bool   `json:"summary,omitempty" nullable:"true" doc:"Route into the summary digest (clean bitmask bit 4). Omit to disable (default false). Returned as null when false."`

	OverrideLocationLabel *string  `json:"override_location_label,omitempty" nullable:"true" doc:"Saved-location label to use instead of the profile location (requires distance > 0; mutually exclusive with override_areas). Omit for none. Returned as null when unset."`
	OverrideAreas         []string `json:"override_areas,omitempty" doc:"Restrict this rule to these geofence areas (mutually exclusive with distance > 0 and override_location_label). Omit for none. Returned as null when unset."`
}

// translateV2Egg converts a strict v2 egg rule into the stored EggTrackingAPI,
// applying the required level >= 1 validation, documented defaults, the
// team/rsvp enum→int translation, the clean bitmask, profile, and
// validated/normalized override fields. ping is always stored "" (server-managed).
func translateV2Egg(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *v2EggRule) (db.EggTrackingAPI, error) {
	// level is required + minimum:1 by huma's schema; this is defence-in-depth
	// and mirrors v1's explicit "Invalid level" 400 (surfaced as 422 in v2).
	if req.Level < 1 {
		return db.EggTrackingAPI{}, huma.Error422UnprocessableEntity("Invalid level (must be >= 1)")
	}

	// team/rsvp are enum-validated by huma; resolveStored applies the documented
	// default for nil and is defence-in-depth otherwise.
	team := teamEnum.resolveStored(req.Team)
	rsvp := rsvpChangesEnum.resolveStored(req.RSVPChanges)

	distance := valueOr(req.Distance, 0)
	const maxDistance = 40000000 // Earth circumference (metres)
	if distance > maxDistance {
		distance = maxDistance
	}

	overrideLabel := valueOr(req.OverrideLocationLabel, "")
	if msg, code := validateOverrideFields(deps, oc, humanID, overrideLabel, req.OverrideAreas, distance); msg != "" {
		return db.EggTrackingAPI{}, humaErr(code, msg)
	}

	var gymID null.String
	if req.GymID != nil && *req.GymID != "" {
		gymID = null.StringFrom(*req.GymID)
	}

	row := db.EggTrackingAPI{
		ID:                    humanID,
		ProfileNo:             profileNo,
		Ping:                  "", // server-managed
		Template:              valueOr(req.Template, ""),
		Distance:              distance,
		Team:                  team,
		Level:                 req.Level,
		Exclusive:             db.IntBool(valueOr(req.Exclusive, false)),
		GymID:                 gymID,
		RSVPChanges:           rsvp,
		Clean:                 packClean(valueOr(req.Clean, false), valueOr(req.Edit, false), valueOr(req.Summary, false)),
		OverrideLocationLabel: overrideLabel,
		OverrideAreas:         normalizeOverrideAreas(req.OverrideAreas),
	}
	return row, nil
}

// eggRowToRule converts a stored EggTrackingAPI back into the strict v2 rule
// shape for responses.
func eggRowToRule(row *db.EggTrackingAPI) v2EggRule {
	team := teamEnum.fromStored(row.Team)
	rsvp := rsvpChangesEnum.fromStored(row.RSVPChanges)
	var gymID *string
	if row.GymID.Valid && row.GymID.String != "" {
		s := row.GymID.String
		gymID = &s
	}
	return v2EggRule{
		Level:                 row.Level, // required, always present
		Team:                  ptrUnless(team, "any"),
		Exclusive:             ptrUnless(bool(row.Exclusive), false),
		GymID:                 gymID,
		RSVPChanges:           ptrUnless(rsvp, "none"),
		Distance:              ptrUnless(row.Distance, 0),
		Template:              ptrUnless(row.Template, ""),
		Clean:                 ptrUnless(db.IsClean(row.Clean), false),
		Edit:                  ptrUnless(db.IsEdit(row.Clean), false),
		Summary:               ptrUnless(db.IsSummary(row.Clean), false),
		OverrideLocationLabel: ptrUnless(row.OverrideLocationLabel, ""),
		OverrideAreas:         ptrUnlessSlice(row.OverrideAreas),
	}
}

// RegisterV2TrackingEgg registers the strict v2 egg tracking endpoints via the
// generic resource helpers.
func RegisterV2TrackingEgg(api huma.API, deps *TrackingDeps) {
	registerV2Tracking(api, deps, v2TrackingType[v2EggRule, db.EggTrackingAPI]{
		Name: "egg",
		Store: func(d *TrackingDeps) store.TrackingStore[db.EggTrackingAPI] {
			return d.Tracking.Eggs
		},
		Translate: translateV2Egg,
		ToRule:    eggRowToRule,
		GetUID:    store.EggGetUID,
		SetUID:    store.EggSetUID,
		RowText: func(d *TrackingDeps, tr *i18n.Translator, row *db.EggTrackingAPI) string {
			return d.RowText.EggRowText(tr, toEggTracking(row))
		},
	})
}
