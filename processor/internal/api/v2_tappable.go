package api

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2TappableRule is the strict v2 tappable tracking request/response rule
// object. It mirrors v2QuestRule's reward shape (reward_type required, reward
// and amount optional) without the pokemon-encounter reward, form, shiny and
// summary: tappables have none of them. Edit is present only so a client
// sending edit:true gets the same 422 every edit-less type gives.
type v2TappableRule struct {
	RewardType int  `json:"reward_type" required:"true" doc:"Reward category (quest reward ids; no wildcard); one of 2 (item) | 3 (stardust) | 4 (candy) | 8 (pokecoins) | 9 (xl_candy) | 12 (mega_energy) (required)."`
	Reward     *int `json:"reward,omitempty" minimum:"0" nullable:"true" doc:"Reward selector; meaning depends on reward_type: item id (2), minimum stardust amount (3), minimum pokecoin amount (8), pokemon id (4/9/12). Omit (or send 0) to match ANY reward of that category (stored as 0 = any). Returned as null when at its wildcard."`
	Amount     *int `json:"amount,omitempty" minimum:"0" nullable:"true" doc:"Minimum reward count for reward_type 2/4/9/12. Not accepted for 3/8, whose minimum is reward. Omit to impose no minimum (stored as 0 = any). Returned as null when at its wildcard."`

	// Common fields.
	Distance *int    `json:"distance,omitempty" minimum:"0" maximum:"40000000" nullable:"true" doc:"Radius in metres around the anchor location. Omit (or 0) to match by the profile's geofence areas instead of a radius — 0 means area-based, NOT zero metres (stored as 0). Values above the server's [tracking] max_distance are clamped to it. Returned as null when at its wildcard."`
	Template *string `json:"template,omitempty" nullable:"true" doc:"DTS template name. Omit (or empty) to use the server's configured default template (stored as \"\"). Returned as null when at its wildcard."`
	Ping     *string `json:"ping,omitempty" nullable:"true" maxLength:"255" doc:"Text appended to the alert, typically Discord role/user mentions. Stored verbatim — not restricted to mention tokens as the bot is, so @everyone/@here in a channel rule will ping. Omit for none. Returned as null when empty."`
	Clean    *bool   `json:"clean,omitempty" nullable:"true" doc:"Auto-delete the alert when the tappable disappears (clean bitmask bit 1). Omit to disable (default false). Returned as null when false."`
	Edit     *bool   `json:"edit,omitempty" nullable:"true" doc:"Not supported for tappable tracking — omit, or send null/false (true is rejected with 422). Always returned as null."`

	OverrideLocationLabel *string  `json:"override_location_label,omitempty" nullable:"true" doc:"Saved-location label to use instead of the profile location (requires distance > 0; mutually exclusive with override_areas). Omit for none. Returned as null when unset."`
	OverrideAreas         []string `json:"override_areas,omitempty" doc:"Restrict this rule to these geofence areas (mutually exclusive with distance > 0 and override_location_label). Omit for none. Returned as null when unset."`
}

// validTappableRewardTypes is the set of reward_type values a tappable rule
// can hold (see matching.TappableReward*).
var validTappableRewardTypes = map[int]bool{
	matching.TappableRewardItem:       true,
	matching.TappableRewardStardust:   true,
	matching.TappableRewardCandy:      true,
	matching.TappableRewardPokecoin:   true,
	matching.TappableRewardXLCandy:    true,
	matching.TappableRewardMegaEnergy: true,
}

// translateV2Tappable converts a strict v2 tappable rule into the stored
// TappableTrackingAPI. It rejects edit:true, an out-of-set reward_type, and an
// amount on stardust/pokecoin rules (the matcher reads their minimum from
// reward, so an amount there would be silently ignored).
func translateV2Tappable(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *v2TappableRule) (db.TappableTrackingAPI, error) {
	if err := rejectEdit("tappable", req.Edit); err != nil {
		return db.TappableTrackingAPI{}, err
	}
	if !validTappableRewardTypes[req.RewardType] {
		return db.TappableTrackingAPI{}, huma.Error422UnprocessableEntity("Unrecognised reward_type value",
			&huma.ErrorDetail{Location: "body.reward_type", Message: "must be one of 2, 3, 4, 8, 9, 12", Value: req.RewardType})
	}
	amount := valueOr(req.Amount, wcTappableAmount)
	if amount != wcTappableAmount && (req.RewardType == matching.TappableRewardStardust || req.RewardType == matching.TappableRewardPokecoin) {
		msg := "amount does not apply to reward_type 3 (stardust) or 8 (pokecoins); set the minimum in reward"
		return db.TappableTrackingAPI{}, huma.Error422UnprocessableEntity(msg,
			&huma.ErrorDetail{Location: "body.amount", Message: msg, Value: amount})
	}

	distance := clampV2Distance(deps, valueOr(req.Distance, wcDistance))

	overrideLabel := valueOr(req.OverrideLocationLabel, "")
	if msg, code := validateOverrideFields(deps, oc, humanID, overrideLabel, req.OverrideAreas, distance); msg != "" {
		return db.TappableTrackingAPI{}, humaErr(code, msg)
	}

	return db.TappableTrackingAPI{
		ID:                    humanID,
		ProfileNo:             profileNo,
		Ping:                  valueOr(req.Ping, ""),
		Template:              valueOr(req.Template, ""),
		Distance:              distance,
		RewardType:            req.RewardType,
		Reward:                valueOr(req.Reward, wcTappableReward),
		Amount:                amount,
		Clean:                 packClean(valueOr(req.Clean, false), false, false),
		OverrideLocationLabel: overrideLabel,
		OverrideAreas:         canonicalOverrideAreas(deps, req.OverrideAreas),
	}, nil
}

// tappableRowToRule converts a stored TappableTrackingAPI back into the strict
// v2 rule shape for responses.
func tappableRowToRule(row *db.TappableTrackingAPI) v2TappableRule {
	return v2TappableRule{
		RewardType:            row.RewardType,
		Reward:                ptrUnless(row.Reward, wcTappableReward),
		Amount:                ptrUnless(row.Amount, wcTappableAmount),
		Distance:              ptrUnless(row.Distance, wcDistance),
		Template:              ptrUnless(row.Template, ""),
		Ping:                  ptrUnless(row.Ping, ""),
		Clean:                 ptrUnless(db.IsClean(row.Clean), false),
		Edit:                  nil, // edit is not supported for this type; always null
		OverrideLocationLabel: ptrUnless(row.OverrideLocationLabel, ""),
		OverrideAreas:         ptrUnlessSlice(row.OverrideAreas),
	}
}

// toTappableTracking adapts the API row to the rowtext shape.
func toTappableTracking(row *db.TappableTrackingAPI) *db.TappableTracking {
	return &db.TappableTracking{
		UID:                   row.UID,
		ID:                    row.ID,
		ProfileNo:             row.ProfileNo,
		RewardType:            row.RewardType,
		Reward:                row.Reward,
		Amount:                row.Amount,
		Distance:              row.Distance,
		Ping:                  row.Ping,
		Clean:                 row.Clean,
		Template:              row.Template,
		OverrideLocationLabel: row.OverrideLocationLabel,
		OverrideAreas:         row.OverrideAreas,
	}
}

// RegisterV2TrackingTappable registers the strict v2 tappable tracking
// endpoints via the generic resource helpers. There is no v1 tappable API.
func RegisterV2TrackingTappable(api huma.API, deps *TrackingDeps) {
	registerV2Tracking(api, deps, v2TrackingType[v2TappableRule, db.TappableTrackingAPI]{
		Name: "tappable",
		Store: func(d *TrackingDeps) store.TrackingStore[db.TappableTrackingAPI] {
			return d.Tracking.Tappables
		},
		Translate: translateV2Tappable,
		ToRule:    tappableRowToRule,
		GetUID:    store.TappableGetUID,
		SetUID:    store.TappableSetUID,
		RowText: func(d *TrackingDeps, tr *i18n.Translator, row *db.TappableTrackingAPI) string {
			return d.RowText.TappableRowText(tr, toTappableTracking(row))
		},
	})
}
