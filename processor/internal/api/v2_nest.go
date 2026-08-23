package api

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2NestRule is the strict v2 nest tracking request/response rule object.
//
// All filter fields are optional POINTERS (omitted ⇒ documented default via
// valueOr); there is no required field. No enums. Defaults come from the field
// audit nest table. ping is server-managed (not a caller input).
type v2NestRule struct {
	PokemonID   *int `json:"pokemon_id,omitempty" nullable:"true" doc:"Pokédex id of the nesting species. Omit to match any species (stored as 0 = any). Returned as null when at its wildcard."`
	Form        *int `json:"form,omitempty" nullable:"true" doc:"Form id (game-master). Omit to match any form (stored as 0 = any). Returned as null when at its wildcard."`
	MinSpawnAvg *int `json:"min_spawn_avg,omitempty" nullable:"true" doc:"Minimum hourly spawn average to alert on. Omit to impose no minimum (stored as 0 = any). Returned as null when at its wildcard."`

	// Common fields.
	Distance *int    `json:"distance,omitempty" nullable:"true" doc:"Radius in metres around the anchor location. Omit (or 0) to match by the profile's geofence areas instead of a radius — 0 means area-based, NOT zero metres (stored as 0). Returned as null when at its wildcard."`
	Template *string `json:"template,omitempty" nullable:"true" doc:"DTS template name. Omit (or empty) to use the server's configured default template (stored as \"\"). Returned as null when at its wildcard."`
	Clean    *bool   `json:"clean,omitempty" nullable:"true" doc:"Auto-delete the alert on expiry (clean bitmask bit 1). Omit to disable (default false). Returned as null when false."`
	Edit     *bool   `json:"edit,omitempty" nullable:"true" doc:"Keep the message updated in place (clean bitmask bit 2). Omit to disable (default false). Returned as null when false."`
	Summary  *bool   `json:"summary,omitempty" nullable:"true" doc:"Route into the summary digest (clean bitmask bit 4). Omit to disable (default false). Returned as null when false."`

	OverrideLocationLabel *string  `json:"override_location_label,omitempty" nullable:"true" doc:"Saved-location label to use instead of the profile location (requires distance > 0; mutually exclusive with override_areas). Omit for none. Returned as null when unset."`
	OverrideAreas         []string `json:"override_areas,omitempty" doc:"Restrict this rule to these geofence areas (mutually exclusive with distance > 0 and override_location_label). Omit for none. Returned as null when unset."`
}

// translateV2Nest converts a strict v2 nest rule into the stored NestTrackingAPI,
// applying documented defaults, the clean bitmask, profile, and
// validated/normalized override fields. ping is always stored "" (server-managed).
func translateV2Nest(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *v2NestRule) (db.NestTrackingAPI, error) {
	distance := valueOr(req.Distance, 0)
	const maxDistance = 40000000 // Earth circumference (metres)
	if distance > maxDistance {
		distance = maxDistance
	}

	overrideLabel := valueOr(req.OverrideLocationLabel, "")
	if msg, code := validateOverrideFields(deps, oc, humanID, overrideLabel, req.OverrideAreas, distance); msg != "" {
		return db.NestTrackingAPI{}, humaErr(code, msg)
	}

	row := db.NestTrackingAPI{
		ID:                    humanID,
		ProfileNo:             profileNo,
		Ping:                  "", // server-managed
		Template:              valueOr(req.Template, ""),
		Distance:              distance,
		PokemonID:             valueOr(req.PokemonID, 0),
		MinSpawnAvg:           valueOr(req.MinSpawnAvg, 0),
		Form:                  valueOr(req.Form, 0),
		Clean:                 packClean(valueOr(req.Clean, false), valueOr(req.Edit, false), valueOr(req.Summary, false)),
		OverrideLocationLabel: overrideLabel,
		OverrideAreas:         normalizeOverrideAreas(req.OverrideAreas),
	}
	return row, nil
}

// nestRowToRule converts a stored NestTrackingAPI back into the strict v2 rule
// shape for responses.
func nestRowToRule(row *db.NestTrackingAPI) v2NestRule {
	return v2NestRule{
		PokemonID:             ptrUnless(row.PokemonID, 0),
		Form:                  ptrUnless(row.Form, 0),
		MinSpawnAvg:           ptrUnless(row.MinSpawnAvg, 0),
		Distance:              ptrUnless(row.Distance, 0),
		Template:              ptrUnless(row.Template, ""),
		Clean:                 ptrUnless(db.IsClean(row.Clean), false),
		Edit:                  ptrUnless(db.IsEdit(row.Clean), false),
		Summary:               ptrUnless(db.IsSummary(row.Clean), false),
		OverrideLocationLabel: ptrUnless(row.OverrideLocationLabel, ""),
		OverrideAreas:         ptrUnlessSlice(row.OverrideAreas),
	}
}

// RegisterV2TrackingNest registers the strict v2 nest tracking endpoints via the
// generic resource helpers.
func RegisterV2TrackingNest(api huma.API, deps *TrackingDeps) {
	registerV2Tracking(api, deps, v2TrackingType[v2NestRule, db.NestTrackingAPI]{
		Name: "nest",
		Store: func(d *TrackingDeps) store.TrackingStore[db.NestTrackingAPI] {
			return d.Tracking.Nests
		},
		Translate: translateV2Nest,
		ToRule:    nestRowToRule,
		GetUID:    store.NestGetUID,
		SetUID:    store.NestSetUID,
		RowText: func(d *TrackingDeps, tr *i18n.Translator, row *db.NestTrackingAPI) string {
			return d.RowText.NestRowText(tr, toNestTracking(row))
		},
	})
}
