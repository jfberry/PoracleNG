package api

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/guregu/null/v6"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2RaidRule is the strict v2 raid tracking request/response rule object.
//
// All filter fields are optional POINTERS (omitted ⇒ documented default via
// valueOr); there is no required field. team and rsvp_changes are STRING enums
// (teamEnum / rsvpChangesEnum), both stored as ints. gym_id is a nullable string
// (null/empty = any). ping is server-managed (not a caller input).
//
// v2 is STRICT and deliberately drops v1's array expansions: v1 accepted
// `level` as an int OR [int,...] (one row per level) and `pokemon_form` as
// [{pokemon_id,form}] (one row per pair). v2 models level/pokemon_id/form as
// SINGLE ints — a client wanting multiple levels/forms POSTs multiple rule
// objects (the create body is already an array). There is NO pokemon_form
// field and `level` is a plain int; the level-array and pokemon_form shapes are
// now unknown fields and 422 under additionalProperties:false / type checking.
//
// pokemon_id defaults to 9000, the engine sentinel for "track by level, not a
// specific pokemon" (consistent with the shipped maxbattle type). The level
// sentinel is derived from pokemon_id in translateV2Raid (see
// matching/raid.go:65): omit both for "everything" (stored 9000/90), an
// explicit by-level level must be >= 1, a specific pokemon_id stores the 9000
// level placeholder. move/evolution default to 9000 ("any"), per the field
// audit raid table.
type v2RaidRule struct {
	PokemonID *int    `json:"pokemon_id,omitempty" nullable:"true" doc:"Omit pokemon_id to track by raid level (any boss); give a Pokédex id for a specific boss. Omitting stores the by-level sentinel 9000. Returned as null when tracking by level."`
	Form      *int    `json:"form,omitempty" nullable:"true" doc:"Form id (game-master). Omit to match any form (stored as 0 = any). Returned as null when at its wildcard."`
	Costume   *int    `json:"costume,omitempty" nullable:"true" doc:"Costume id. Omit/null = any (stored 9000). 0 = no costume. N = that costume."`
	Level     *int    `json:"level,omitempty" nullable:"true" doc:"Raid tier. Only applies when tracking by level (no pokemon_id) — omit for any tier (stored 90), or give a tier >= 1. With a specific pokemon_id, level is ignored (stored placeholder 9000, matching the bot). Single int — POST multiple rule objects for multiple tiers. Returned as null when stored 90 (any tier) or 9000 (level unused)."`
	Team      *string `json:"team,omitempty" nullable:"true" enum:"harmony,mystic,valor,instinct,any" doc:"Controlling team: harmony|mystic|valor|instinct|any (0|1|2|3|4). Omit to match any team (defaults to 'any', stored as 4). Returned as null when 'any'."`
	Exclusive *bool   `json:"exclusive,omitempty" nullable:"true" doc:"Match EX-raids only. Omit to match regardless (default false). Returned as null when false."`
	Move      *int    `json:"move,omitempty" nullable:"true" doc:"Charge move id (game-master). Omit to match any move (stored as 9000 = the project-wide 'any' sentinel). Returned as null when at its wildcard."`
	Evolution *int    `json:"evolution,omitempty" nullable:"true" doc:"Mega evolution id (game-master). Omit to match any evolution (stored as 9000 = the project-wide 'any' sentinel). Returned as null when at its wildcard."`
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

// translateV2Raid converts a strict v2 raid rule into the stored
// RaidTrackingAPI, applying documented defaults, the team/rsvp enum→int
// translation, the clean bitmask, profile, and validated/normalized override
// fields. ping is always stored "" (server-managed).
func translateV2Raid(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *v2RaidRule) (db.RaidTrackingAPI, error) {
	// team/rsvp are enum-validated by huma; toStored/resolveStored is
	// defence-in-depth and applies the documented default for nil.
	team := teamEnum.resolveStored(req.Team)
	rsvp := rsvpChangesEnum.resolveStored(req.RSVPChanges)

	distance := valueOr(req.Distance, 0)
	const maxDistance = 40000000 // Earth circumference (metres)
	if distance > maxDistance {
		distance = maxDistance
	}

	overrideLabel := valueOr(req.OverrideLocationLabel, "")
	if msg, code := validateOverrideFields(deps, oc, humanID, overrideLabel, req.OverrideAreas, distance); msg != "" {
		return db.RaidTrackingAPI{}, humaErr(code, msg)
	}

	// The level field's meaning depends on pokemon_id (see matching/raid.go:65):
	//   - by-level mode (pokemon_id == 9000): omitted level => 90 ("any tier" /
	//     "everything"); an explicit level must be >= 1 (mirrors v1's "Invalid
	//     level" guard).
	//   - specific-pokemon mode: level is ignored by the matcher and stored as the
	//     9000 placeholder (byte-identical to bot/v1 rows).
	pokemonID := valueOr(req.PokemonID, 9000)
	level := 9000
	if pokemonID == 9000 {
		if req.Level == nil {
			level = 90
		} else {
			level = *req.Level
			if level < 1 {
				return db.RaidTrackingAPI{}, huma.Error422UnprocessableEntity("Invalid level (must be >= 1 when tracking by level)")
			}
		}
	}

	var gymID null.String
	if req.GymID != nil && *req.GymID != "" {
		gymID = null.StringFrom(*req.GymID)
	}

	row := db.RaidTrackingAPI{
		ID:                    humanID,
		ProfileNo:             profileNo,
		Ping:                  "", // server-managed
		Template:              valueOr(req.Template, ""),
		Distance:              distance,
		Team:                  team,
		PokemonID:             pokemonID,
		Form:                  valueOr(req.Form, 0),
		Costume:               valueOr(req.Costume, 9000),
		Level:                 level,
		Exclusive:             db.IntBool(valueOr(req.Exclusive, false)),
		Move:                  valueOr(req.Move, 9000),
		Evolution:             valueOr(req.Evolution, 9000),
		GymID:                 gymID,
		RSVPChanges:           rsvp,
		Clean:                 packClean(valueOr(req.Clean, false), valueOr(req.Edit, false), valueOr(req.Summary, false)),
		OverrideLocationLabel: overrideLabel,
		OverrideAreas:         normalizeOverrideAreas(req.OverrideAreas),
	}
	return row, nil
}

// raidLevelOrNull projects a stored raid/maxbattle level back to the response,
// emitting null when there is no meaningful tier: 9000 (specific-pokemon
// placeholder, incl. legacy bot rows) or 90 (by-level "any tier"). Any other
// value is a concrete tracked tier.
func raidLevelOrNull(level int) *int {
	if level == 9000 || level == 90 {
		return nil
	}
	return &level
}

// raidRowToRule converts a stored RaidTrackingAPI back into the strict v2 rule
// shape for responses.
func raidRowToRule(row *db.RaidTrackingAPI) v2RaidRule {
	team := teamEnum.fromStored(row.Team)
	rsvp := rsvpChangesEnum.fromStored(row.RSVPChanges)
	var gymID *string
	if row.GymID.Valid && row.GymID.String != "" {
		s := row.GymID.String
		gymID = &s
	}
	return v2RaidRule{
		PokemonID: ptrUnless(row.PokemonID, 9000),
		Form:      ptrUnless(row.Form, 0),
		Costume:   ptrUnless(row.Costume, 9000),
		// level has no meaningful tier when stored 9000 (specific-pokemon
		// placeholder, incl. legacy bot rows) or 90 (by-level any-tier).
		Level:                 raidLevelOrNull(row.Level),
		Team:                  ptrUnless(team, "any"),
		Exclusive:             ptrUnless(bool(row.Exclusive), false),
		Move:                  ptrUnless(row.Move, 9000),
		Evolution:             ptrUnless(row.Evolution, 9000),
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

// RegisterV2TrackingRaid registers the strict v2 raid tracking endpoints via the
// generic resource helpers.
func RegisterV2TrackingRaid(api huma.API, deps *TrackingDeps) {
	registerV2Tracking(api, deps, v2TrackingType[v2RaidRule, db.RaidTrackingAPI]{
		Name: "raid",
		Store: func(d *TrackingDeps) store.TrackingStore[db.RaidTrackingAPI] {
			return d.Tracking.Raids
		},
		Translate: translateV2Raid,
		ToRule:    raidRowToRule,
		GetUID:    store.RaidGetUID,
		SetUID:    store.RaidSetUID,
		RowText: func(d *TrackingDeps, tr *i18n.Translator, row *db.RaidTrackingAPI) string {
			return d.RowText.RaidRowText(tr, toRaidTracking(row))
		},
	})
}
