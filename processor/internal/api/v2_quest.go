package api

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2QuestRule is the strict v2 quest tracking request/response rule object.
//
// reward_type is REQUIRED (non-pointer) and must be one of the documented
// discrete set; it is a plain INT (a game-master/proto reward category, not a
// string enum). The discrete set is validated explicitly in translateV2Quest
// because huma's min/max/enum can't express a sparse int set cleanly (matching
// the v1 handler's validRewardTypes guard, surfaced as 422 in v2). The remaining
// filter fields are optional pointers (omitted ⇒ documented default via
// valueOr). ping is server-managed (not a caller input).
//
// summary opts a matched quest into the summary digest (clean bit 4) — the
// distinguishing behaviour of quest tracking. quest also carries the clean/edit
// lifecycle bits, all packed into the clean column.
type v2QuestRule struct {
	RewardType int   `json:"reward_type" required:"true" doc:"Reward category (game-master proto id; no wildcard); one of 2 (item) | 3 (stardust) | 4 (candy) | 7 (pokemon) | 8 (pokecoins) | 12 (mega_energy) (required)."`
	Reward     *int  `json:"reward,omitempty" nullable:"true" doc:"Reward selector (game-master id; meaning depends on reward_type: item id (2), stardust amount (3), pokecoin amount (8), pokemon/candy id (4/7/12)). Omit (or send 0) to match ANY reward of that category — i.e. {reward_type:2} is 'all items', {reward_type:7} is 'all pokemon', {reward_type:8} is 'any pokecoin amount' (stored as 0 = any). Returned as null when at its wildcard."`
	Form       *int  `json:"form,omitempty" nullable:"true" doc:"Form id (game-master), meaningful when reward_type=7 (pokemon). Omit to match any form (stored as 0 = any). Returned as null when at its wildcard."`
	Shiny      *bool `json:"shiny,omitempty" nullable:"true" doc:"Match shiny-possible quest rewards only. Omit to match regardless (default false). Returned as null when false."`
	Amount     *int  `json:"amount,omitempty" nullable:"true" doc:"Minimum reward amount, meaningful for reward_type 2/4/12. Omit to impose no minimum (stored as 0 = any). Returned as null when at its wildcard."`

	// Common fields.
	Distance *int    `json:"distance,omitempty" nullable:"true" doc:"Radius in metres around the anchor location. Omit (or 0) to match by the profile's geofence areas instead of a radius — 0 means area-based, NOT zero metres (stored as 0). Returned as null when at its wildcard."`
	Template *string `json:"template,omitempty" nullable:"true" doc:"DTS template name. Omit (or empty) to use the server's configured default template (stored as \"\"). Returned as null when at its wildcard."`
	Clean    *bool   `json:"clean,omitempty" nullable:"true" doc:"Auto-delete the alert on expiry (clean bitmask bit 1). Omit to disable (default false). Returned as null when false."`
	Edit     *bool   `json:"edit,omitempty" nullable:"true" doc:"Keep the message updated in place (clean bitmask bit 2). Omit to disable (default false). Returned as null when false."`
	Summary  *bool   `json:"summary,omitempty" nullable:"true" doc:"Route into the summary digest (clean bitmask bit 4). Omit to disable (default false). Returned as null when false."`

	OverrideLocationLabel *string  `json:"override_location_label,omitempty" nullable:"true" doc:"Saved-location label to use instead of the profile location (requires distance > 0; mutually exclusive with override_areas). Omit for none. Returned as null when unset."`
	OverrideAreas         []string `json:"override_areas,omitempty" doc:"Restrict this rule to these geofence areas (mutually exclusive with distance > 0 and override_location_label). Omit for none. Returned as null when unset."`
}

// translateV2Quest converts a strict v2 quest rule into the stored
// QuestTrackingAPI, applying documented defaults, the clean bitmask (incl. the
// summary bit), profile, and validated/normalized override fields. It rejects an
// out-of-set reward_type with a 422 (matching the v1 handler's validRewardTypes
// guard, which returns 400). ping is always stored "" (server-managed).
func translateV2Quest(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *v2QuestRule) (db.QuestTrackingAPI, error) {
	if !validRewardTypes[req.RewardType] {
		return db.QuestTrackingAPI{}, huma.Error422UnprocessableEntity("Unrecognised reward_type value")
	}

	distance := valueOr(req.Distance, 0)
	const maxDistance = 40000000 // Earth circumference (metres)
	if distance > maxDistance {
		distance = maxDistance
	}

	overrideLabel := valueOr(req.OverrideLocationLabel, "")
	if msg, code := validateOverrideFields(deps, oc, humanID, overrideLabel, req.OverrideAreas, distance); msg != "" {
		return db.QuestTrackingAPI{}, humaErr(code, msg)
	}

	row := db.QuestTrackingAPI{
		ID:                    humanID,
		ProfileNo:             profileNo,
		Ping:                  "", // server-managed
		Template:              valueOr(req.Template, ""),
		Distance:              distance,
		RewardType:            req.RewardType,
		Reward:                valueOr(req.Reward, 0),
		Form:                  valueOr(req.Form, 0),
		Shiny:                 db.IntBool(valueOr(req.Shiny, false)),
		Amount:                valueOr(req.Amount, 0),
		Clean:                 packClean(valueOr(req.Clean, false), valueOr(req.Edit, false), valueOr(req.Summary, false)),
		OverrideLocationLabel: overrideLabel,
		OverrideAreas:         normalizeOverrideAreas(req.OverrideAreas),
	}
	return row, nil
}

// questRowToRule converts a stored QuestTrackingAPI back into the strict v2 rule
// shape for responses.
func questRowToRule(row *db.QuestTrackingAPI) v2QuestRule {
	return v2QuestRule{
		RewardType:            row.RewardType, // required, always present
		Reward:                ptrUnless(row.Reward, 0),
		Form:                  ptrUnless(row.Form, 0),
		Shiny:                 ptrUnless(bool(row.Shiny), false),
		Amount:                ptrUnless(row.Amount, 0),
		Distance:              ptrUnless(row.Distance, 0),
		Template:              ptrUnless(row.Template, ""),
		Clean:                 ptrUnless(db.IsClean(row.Clean), false),
		Edit:                  ptrUnless(db.IsEdit(row.Clean), false),
		Summary:               ptrUnless(db.IsSummary(row.Clean), false),
		OverrideLocationLabel: ptrUnless(row.OverrideLocationLabel, ""),
		OverrideAreas:         ptrUnlessSlice(row.OverrideAreas),
	}
}

// RegisterV2TrackingQuest registers the strict v2 quest tracking endpoints via
// the generic resource helpers.
func RegisterV2TrackingQuest(api huma.API, deps *TrackingDeps) {
	registerV2Tracking(api, deps, v2TrackingType[v2QuestRule, db.QuestTrackingAPI]{
		Name: "quest",
		Store: func(d *TrackingDeps) store.TrackingStore[db.QuestTrackingAPI] {
			return d.Tracking.Quests
		},
		Translate: translateV2Quest,
		ToRule:    questRowToRule,
		GetUID:    store.QuestGetUID,
		SetUID:    store.QuestSetUID,
		RowText: func(d *TrackingDeps, tr *i18n.Translator, row *db.QuestTrackingAPI) string {
			return d.RowText.QuestRowText(tr, toQuestTracking(row))
		},
	})
}
