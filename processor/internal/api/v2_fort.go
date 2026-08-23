package api

import (
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2FortChangeTypes is the set of accepted change_types values. These must match
// what Golbat emits in `change_type` / `edit_types[]` (the matcher's
// changeTypesMatch parses the stored JSON array against them). `photo` is NOT
// accepted here — the bot keyword maps to `image_url`, which is the stored value.
var v2FortChangeTypes = map[string]bool{
	"location": true, "new": true, "removal": true,
	"image_url": true, "name": true, "description": true,
}

// v2FortRule is the strict v2 fort tracking request/response rule object.
//
// fort_type is a string enum (pokestop|gym|everything, default everything) STORED
// AS THE CANONICAL STRING (the forts.fort_type column holds the string itself,
// not an int) — validated via fortTypeEnum.canonical. Note the bot/API split:
// the !fort bot command accepts a `station` keyword, but the API does NOT —
// `station` (and any other value) is rejected 422 here, matching v1's
// validFortTypes set (signed-off decision #1).
//
// fort has NO clean column, so this struct intentionally omits clean/edit/summary
// and translateV2Fort never packs a clean bitmask.
//
// include_empty defaults to TRUE on omission — a DELIBERATE behaviour change from
// v1's gin handler (which defaulted false) to honour the DB column default
// (COALESCE(include_empty, true)). Signed-off decision #2; flagged for CHANGELOG.
type v2FortRule struct {
	FortType     *string  `json:"fort_type,omitempty" nullable:"true" enum:"pokestop,gym,everything" doc:"Fort type: pokestop|gym|everything. Omit to match all fort types (defaults to the catch-all 'everything', the stored DB column default). The bot also accepts 'station'; the API does not. Returned as null when 'everything' (the wildcard)."`
	IncludeEmpty *bool    `json:"include_empty,omitempty" nullable:"true" doc:"Include forts with no active subject. Omit to include them (defaults to TRUE, honouring the DB column default; differs from the v1 gin handler which defaulted false). Returned as null when at its default (true)."`
	ChangeTypes  []string `json:"change_types,omitempty" doc:"Edit kinds to alert on; subset of: location|new|removal|image_url|name|description. Omit (or empty) to match ANY change type (stored as the empty array [] = any). Returned as null when unset (any)."`

	// Common fields. fort has NO clean column ⇒ no clean/edit/summary here.
	Distance *int    `json:"distance,omitempty" nullable:"true" doc:"Radius in metres around the anchor location. Omit (or 0) to match by the profile's geofence areas instead of a radius — 0 means area-based, NOT zero metres (stored as 0). Returned as null when at its wildcard."`
	Template *string `json:"template,omitempty" nullable:"true" doc:"DTS template name. Omit (or empty) to use the server's configured default template (stored as \"\"). Returned as null when at its wildcard."`

	OverrideLocationLabel *string  `json:"override_location_label,omitempty" nullable:"true" doc:"Saved-location label to use instead of the profile location (requires distance > 0; mutually exclusive with override_areas). Omit for none. Returned as null when unset."`
	OverrideAreas         []string `json:"override_areas,omitempty" doc:"Restrict this rule to these geofence areas (mutually exclusive with distance > 0 and override_location_label). Omit for none. Returned as null when unset."`
}

// translateV2Fort converts a strict v2 fort rule into the stored FortTrackingAPI.
// fort_type is validated to its canonical STRING form and stored as-is;
// include_empty defaults TRUE; change_types each validated then JSON-encoded as
// the stored varchar array; no clean bitmask (fort has no clean column). ping is
// always stored "" (server-managed).
func translateV2Fort(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *v2FortRule) (db.FortTrackingAPI, error) {
	// fort_type: store the canonical string. huma's enum schema already rejects
	// unknown strings (incl. `station`); canonical is defence-in-depth and also
	// resolves the omitted ⇒ default-"everything" case.
	fortType := fortTypeEnum.defValue
	if req.FortType != nil && *req.FortType != "" {
		c, ok := fortTypeEnum.canonical(*req.FortType)
		if !ok {
			return db.FortTrackingAPI{}, huma.Error422UnprocessableEntity(
				"Invalid fort_type: " + *req.FortType + " (must be pokestop, gym, or everything)")
		}
		fortType = c
	}

	// include_empty defaults TRUE on omission (DB column default; signed-off
	// behaviour change vs v1). CHANGELOG note required (Phase 5).
	includeEmpty := db.IntBool(valueOr(req.IncludeEmpty, true))

	// change_types: validate each value against the accepted set, then JSON-encode
	// the array as the stored varchar (empty ⇒ "[]", matching v1 + the bot).
	changeTypes := "[]"
	if len(req.ChangeTypes) > 0 {
		for _, ct := range req.ChangeTypes {
			if !v2FortChangeTypes[ct] {
				return db.FortTrackingAPI{}, huma.Error422UnprocessableEntity(
					"Invalid change_types value: " + ct +
						" (must be one of location, new, removal, image_url, name, description)")
			}
		}
		b, err := json.Marshal(req.ChangeTypes)
		if err != nil {
			return db.FortTrackingAPI{}, huma.Error422UnprocessableEntity("invalid change_types")
		}
		changeTypes = string(b)
	}

	distance := valueOr(req.Distance, 0)
	const maxDistance = 40000000 // Earth circumference (metres)
	if distance > maxDistance {
		distance = maxDistance
	}

	overrideLabel := valueOr(req.OverrideLocationLabel, "")
	if msg, code := validateOverrideFields(deps, oc, humanID, overrideLabel, req.OverrideAreas, distance); msg != "" {
		return db.FortTrackingAPI{}, humaErr(code, msg)
	}

	row := db.FortTrackingAPI{
		ID:                    humanID,
		ProfileNo:             profileNo,
		Ping:                  "", // server-managed
		Template:              valueOr(req.Template, ""),
		Distance:              distance,
		FortType:              fortType,
		IncludeEmpty:          includeEmpty,
		ChangeTypes:           changeTypes,
		OverrideLocationLabel: overrideLabel,
		OverrideAreas:         normalizeOverrideAreas(req.OverrideAreas),
	}
	return row, nil
}

// fortRowToRule converts a stored FortTrackingAPI back into the strict v2 rule
// shape for responses. The stored change_types JSON array is decoded back to a
// []string; a malformed/empty value yields an empty slice.
func fortRowToRule(row *db.FortTrackingAPI) v2FortRule {
	var changeTypes []string
	if row.ChangeTypes != "" {
		_ = json.Unmarshal([]byte(row.ChangeTypes), &changeTypes)
	}
	return v2FortRule{
		// fort_type wildcard is the catch-all "everything" (its documented default);
		// include_empty defaults TRUE (DB column default) so true is hidden as null.
		FortType:              ptrUnless(row.FortType, fortTypeEnum.defValue),
		IncludeEmpty:          ptrUnless(bool(row.IncludeEmpty), true),
		ChangeTypes:           ptrUnlessSlice(changeTypes),
		Distance:              ptrUnless(row.Distance, 0),
		Template:              ptrUnless(row.Template, ""),
		OverrideLocationLabel: ptrUnless(row.OverrideLocationLabel, ""),
		OverrideAreas:         ptrUnlessSlice(row.OverrideAreas),
	}
}

// RegisterV2TrackingFort registers the strict v2 fort tracking endpoints via the
// generic resource helpers.
func RegisterV2TrackingFort(api huma.API, deps *TrackingDeps) {
	registerV2Tracking(api, deps, v2TrackingType[v2FortRule, db.FortTrackingAPI]{
		Name: "fort",
		Store: func(d *TrackingDeps) store.TrackingStore[db.FortTrackingAPI] {
			return d.Tracking.Forts
		},
		Translate: translateV2Fort,
		ToRule:    fortRowToRule,
		GetUID:    store.FortGetUID,
		SetUID:    store.FortSetUID,
		RowText: func(d *TrackingDeps, tr *i18n.Translator, row *db.FortTrackingAPI) string {
			return d.RowText.FortUpdateRowText(tr, toFortTracking(row))
		},
	})
}
