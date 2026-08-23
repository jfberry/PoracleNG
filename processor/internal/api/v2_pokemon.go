package api

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2PokemonRule is the strict v2 pokemon tracking request/response rule object.
//
// Optional filter fields are POINTERS so "omitted ⇒ documented default" is
// unambiguous: huma leaves an omitted pointer nil (we deliberately use NO
// `default:` tags, which huma WOULD auto-populate the pointer from — verified
// empirically), and the handler applies the documented default via valueOr.
// pokemon_id is required (non-pointer). gender is the only string enum here.
//
// Defaults documented in each field's doc string come from the field audit
// pokemon table.
type v2PokemonRule struct {
	PokemonID int `json:"pokemon_id" required:"true" doc:"Pokédex id (required)"`

	Form *int `json:"form,omitempty" nullable:"true" doc:"Form id (game-master). Omit to match any form (stored as 0 = any). Returned as null when at its wildcard."`

	Costume *int `json:"costume,omitempty" nullable:"true" doc:"Costume id. Omit/null = any (stored 9000). 0 = no costume. N = that costume."`

	MinIV *int `json:"min_iv,omitempty" nullable:"true" doc:"Minimum IV %. Omit to impose no lower bound (stored as -1 = no lower bound). Returned as null when at its wildcard."`
	MaxIV *int `json:"max_iv,omitempty" nullable:"true" doc:"Maximum IV %. Omit to impose no upper bound (stored as 100 = the IV ceiling, i.e. no upper bound). Returned as null when at its wildcard."`

	MinCP *int `json:"min_cp,omitempty" nullable:"true" doc:"Minimum CP. Omit to impose no lower bound (stored as 0 = no lower bound). Returned as null when at its wildcard."`
	MaxCP *int `json:"max_cp,omitempty" nullable:"true" doc:"Maximum CP. Omit to impose no upper bound (stored as 9000 = the CP cap sentinel, i.e. no upper bound). Returned as null when at its wildcard."`

	MinLevel *int `json:"min_level,omitempty" nullable:"true" doc:"Minimum encounter level. Omit to impose no lower bound (stored as 0 = no lower bound). Returned as null when at its wildcard."`
	MaxLevel *int `json:"max_level,omitempty" nullable:"true" doc:"Maximum encounter level. Omit to impose no upper bound (stored as 55 = the level ceiling, i.e. no upper bound). Returned as null when at its wildcard."`

	ATK *int `json:"atk,omitempty" nullable:"true" doc:"Minimum ATK IV (0-15). Omit to impose no floor (stored as 0 = no floor). Returned as null when at its wildcard."`
	DEF *int `json:"def,omitempty" nullable:"true" doc:"Minimum DEF IV (0-15). Omit to impose no floor (stored as 0 = no floor). Returned as null when at its wildcard."`
	STA *int `json:"sta,omitempty" nullable:"true" doc:"Minimum STA IV (0-15). Omit to impose no floor (stored as 0 = no floor). Returned as null when at its wildcard."`

	MaxATK *int `json:"max_atk,omitempty" nullable:"true" doc:"Maximum ATK IV (0-15). Omit to impose no ceiling (stored as 15 = the IV ceiling, i.e. no upper bound). Returned as null when at its wildcard."`
	MaxDEF *int `json:"max_def,omitempty" nullable:"true" doc:"Maximum DEF IV (0-15). Omit to impose no ceiling (stored as 15 = the IV ceiling, i.e. no upper bound). Returned as null when at its wildcard."`
	MaxSTA *int `json:"max_sta,omitempty" nullable:"true" doc:"Maximum STA IV (0-15). Omit to impose no ceiling (stored as 15 = the IV ceiling, i.e. no upper bound). Returned as null when at its wildcard."`

	Gender *string `json:"gender,omitempty" nullable:"true" enum:"any,male,female,genderless" doc:"Gender filter: any|male|female|genderless. Omit to match any gender (defaults to 'any', stored as 0). Returned as null when 'any'."`

	MinWeight *int `json:"min_weight,omitempty" nullable:"true" doc:"Minimum weight in grams. Omit to impose no lower bound (stored as 0 = no lower bound). Returned as null when at its wildcard."`
	MaxWeight *int `json:"max_weight,omitempty" nullable:"true" doc:"Maximum weight in grams. Omit to impose no upper bound (stored as 9000000 = no upper weight sentinel). Returned as null when at its wildcard."`

	MinTime *int `json:"min_time,omitempty" nullable:"true" doc:"Minimum seconds remaining on the spawn. Omit to impose no minimum (stored as 0 = no minimum). Returned as null when at its wildcard."`

	Rarity    *int `json:"rarity,omitempty" nullable:"true" doc:"Minimum rarity tier. Omit to match any rarity (stored as -1 = any). Returned as null when at its wildcard."`
	MaxRarity *int `json:"max_rarity,omitempty" nullable:"true" doc:"Maximum rarity tier (1-6). Omit to impose no upper bound (stored as 6 = the top tier, i.e. no upper bound). Returned as null when at its wildcard."`

	Size    *int `json:"size,omitempty" nullable:"true" doc:"Minimum size tier. Omit to match any size (stored as -1 = any). Returned as null when at its wildcard."`
	MaxSize *int `json:"max_size,omitempty" nullable:"true" doc:"Maximum size tier (1-5). Omit to impose no upper bound (stored as 5 = the top tier, i.e. no upper bound). Returned as null when at its wildcard."`

	PVPRankingLeague    *int `json:"pvp_ranking_league,omitempty" nullable:"true" enum:"0,500,1500,2500" doc:"PVP league CP cap (the stored int IS the cap): 0 | 500 | 1500 | 2500. Omit (or 0) for IV-mode tracking with no PVP filter (stored as 0 = none/IV mode). Returned as null when at its wildcard."`
	PVPRankingBest      *int `json:"pvp_ranking_best,omitempty" nullable:"true" doc:"Best (lowest, 1-based) PVP rank to alert on. Omit to start from rank 1 (stored as 1 = best possible rank). Returned as null when at its wildcard."`
	PVPRankingWorst     *int `json:"pvp_ranking_worst,omitempty" nullable:"true" doc:"Worst (highest) PVP rank to alert on. Omit to impose no upper rank limit (stored as 4096 = no upper rank limit sentinel; PVP ranks never exceed it). Returned as null when at its wildcard."`
	PVPRankingMinCP     *int `json:"pvp_ranking_min_cp,omitempty" nullable:"true" doc:"PVP CP floor. Omit to impose no floor (stored as 0 = no floor). Returned as null when at its wildcard."`
	PVPRankingCap       *int `json:"pvp_ranking_cap,omitempty" nullable:"true" doc:"PVP level cap. Omit to use the league default cap (stored as 0 = league default). Returned as null when at its wildcard."`
	PVPRankingEvolution *int `json:"pvp_ranking_evolution,omitempty" nullable:"true" doc:"Temp-evolution (mega) PVP discriminator selecting which evolution's PVP rank this rule alerts on: 0 = base form, 1 = Mega, 2 = Mega X, 3 = Mega Y. Omit for base form (stored as 0). Returned as null when at its wildcard (0)."`

	// Common fields.
	Distance *int    `json:"distance,omitempty" nullable:"true" doc:"Radius in metres around the anchor location. Omit (or 0) to match by the profile's geofence areas instead of a radius — 0 means area-based, NOT zero metres (stored as 0). Returned as null when at its wildcard."`
	Template *string `json:"template,omitempty" nullable:"true" doc:"DTS template name. Omit (or empty) to use the server's configured default template (stored as \"\"). Returned as null when at its wildcard."`
	Clean    *bool   `json:"clean,omitempty" nullable:"true" doc:"Auto-delete the alert on expiry (clean bitmask bit 1). Omit to disable (default false). Returned as null when false."`
	Edit     *bool   `json:"edit,omitempty" nullable:"true" doc:"Keep the message updated in place (clean bitmask bit 2). Omit to disable (default false). Returned as null when false."`
	Summary  *bool   `json:"summary,omitempty" nullable:"true" doc:"Route into the summary digest (clean bitmask bit 4). Omit to disable (default false). Returned as null when false."`

	OverrideLocationLabel *string  `json:"override_location_label,omitempty" nullable:"true" doc:"Saved-location label to use instead of the profile location (requires distance > 0; mutually exclusive with override_areas). Omit for none. Returned as null when unset."`
	OverrideAreas         []string `json:"override_areas,omitempty" doc:"Restrict this rule to these geofence areas (mutually exclusive with distance > 0 and override_location_label). Omit for none. Returned as null when unset."`
}

// valueOr returns *p when p is non-nil, else def. The strict-default helper.
func valueOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// packClean collapses the clean/edit/summary booleans into the stored bitmask
// (bit1=clean, bit2=edit, bit4=summary). See internal/db/clean.go.
func packClean(clean, edit, summary bool) int {
	v := 0
	if clean {
		v |= 1
	}
	if edit {
		v |= 2
	}
	if summary {
		v |= 4
	}
	return v
}

// translateV2Pokemon converts a strict v2 pokemon rule into the stored
// MonsterTrackingAPI, applying documented defaults, gender enum→int, the clean
// bitmask, profile, and validated/normalized override fields. ping is always
// stored "" (server-managed). Returns an huma error on override-field violation.
func translateV2Pokemon(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *v2PokemonRule) (db.MonsterTrackingAPI, error) {
	distance := valueOr(req.Distance, 0)
	const maxDistance = 40000000 // Earth circumference (metres)
	if distance > maxDistance {
		distance = maxDistance
	}

	overrideLabel := valueOr(req.OverrideLocationLabel, "")
	if msg, code := validateOverrideFields(deps, oc, humanID, overrideLabel, req.OverrideAreas, distance); msg != "" {
		return db.MonsterTrackingAPI{}, humaErr(code, msg)
	}

	template := valueOr(req.Template, "")

	row := db.MonsterTrackingAPI{
		ID:                    humanID,
		ProfileNo:             profileNo,
		Ping:                  "", // server-managed
		Template:              template,
		Distance:              distance,
		PokemonID:             req.PokemonID,
		Form:                  valueOr(req.Form, 0),
		Costume:               valueOr(req.Costume, 9000),
		MinIV:                 valueOr(req.MinIV, -1),
		MaxIV:                 valueOr(req.MaxIV, 100),
		MinCP:                 valueOr(req.MinCP, 0),
		MaxCP:                 valueOr(req.MaxCP, 9000),
		MinLevel:              valueOr(req.MinLevel, 0),
		MaxLevel:              valueOr(req.MaxLevel, 55),
		ATK:                   valueOr(req.ATK, 0),
		DEF:                   valueOr(req.DEF, 0),
		STA:                   valueOr(req.STA, 0),
		MaxATK:                valueOr(req.MaxATK, 15),
		MaxDEF:                valueOr(req.MaxDEF, 15),
		MaxSTA:                valueOr(req.MaxSTA, 15),
		Gender:                genderEnum.resolveStored(req.Gender),
		MinWeight:             valueOr(req.MinWeight, 0),
		MaxWeight:             valueOr(req.MaxWeight, 9000000),
		MinTime:               valueOr(req.MinTime, 0),
		Rarity:                valueOr(req.Rarity, -1),
		MaxRarity:             valueOr(req.MaxRarity, 6),
		Size:                  valueOr(req.Size, -1),
		MaxSize:               valueOr(req.MaxSize, 5),
		PVPRankingLeague:      valueOr(req.PVPRankingLeague, 0),
		PVPRankingBest:        valueOr(req.PVPRankingBest, 1),
		PVPRankingWorst:       valueOr(req.PVPRankingWorst, 4096),
		PVPRankingMinCP:       valueOr(req.PVPRankingMinCP, 0),
		PVPRankingCap:         valueOr(req.PVPRankingCap, 0),
		PVPRankingEvolution:   valueOr(req.PVPRankingEvolution, 0),
		Clean:                 packClean(valueOr(req.Clean, false), valueOr(req.Edit, false), valueOr(req.Summary, false)),
		OverrideLocationLabel: overrideLabel,
		OverrideAreas:         normalizeOverrideAreas(req.OverrideAreas),
	}
	return row, nil
}

// pokemonRowToRule converts a stored MonsterTrackingAPI back into the strict v2
// rule shape for responses. Required fields (pokemon_id) keep their value; every
// optional filter at its documented wildcard/default is projected to null (Part B
// of #138) so the response shows only the rule's meaningful filters and never
// leaks a magic sentinel. The wildcards passed to ptrUnless mirror the defaults
// translateV2Pokemon applies via valueOr.
func pokemonRowToRule(row *db.MonsterTrackingAPI) v2PokemonRule {
	gender := genderEnum.fromStored(row.Gender)
	return v2PokemonRule{
		PokemonID:             row.PokemonID,
		Form:                  ptrUnless(row.Form, 0),
		Costume:               ptrUnless(row.Costume, 9000),
		MinIV:                 ptrUnless(row.MinIV, -1),
		MaxIV:                 ptrUnless(row.MaxIV, 100),
		MinCP:                 ptrUnless(row.MinCP, 0),
		MaxCP:                 ptrUnless(row.MaxCP, 9000),
		MinLevel:              ptrUnless(row.MinLevel, 0),
		MaxLevel:              ptrUnless(row.MaxLevel, 55),
		ATK:                   ptrUnless(row.ATK, 0),
		DEF:                   ptrUnless(row.DEF, 0),
		STA:                   ptrUnless(row.STA, 0),
		MaxATK:                ptrUnless(row.MaxATK, 15),
		MaxDEF:                ptrUnless(row.MaxDEF, 15),
		MaxSTA:                ptrUnless(row.MaxSTA, 15),
		Gender:                ptrUnless(gender, "any"),
		MinWeight:             ptrUnless(row.MinWeight, 0),
		MaxWeight:             ptrUnless(row.MaxWeight, 9000000),
		MinTime:               ptrUnless(row.MinTime, 0),
		Rarity:                ptrUnless(row.Rarity, -1),
		MaxRarity:             ptrUnless(row.MaxRarity, 6),
		Size:                  ptrUnless(row.Size, -1),
		MaxSize:               ptrUnless(row.MaxSize, 5),
		PVPRankingLeague:      ptrUnless(row.PVPRankingLeague, 0),
		PVPRankingBest:        ptrUnless(row.PVPRankingBest, 1),
		PVPRankingWorst:       ptrUnless(row.PVPRankingWorst, 4096),
		PVPRankingMinCP:       ptrUnless(row.PVPRankingMinCP, 0),
		PVPRankingCap:         ptrUnless(row.PVPRankingCap, 0),
		PVPRankingEvolution:   ptrUnless(row.PVPRankingEvolution, 0),
		Distance:              ptrUnless(row.Distance, 0),
		Template:              ptrUnless(row.Template, ""),
		Clean:                 ptrUnless(db.IsClean(row.Clean), false),
		Edit:                  ptrUnless(db.IsEdit(row.Clean), false),
		Summary:               ptrUnless(db.IsSummary(row.Clean), false),
		OverrideLocationLabel: ptrUnless(row.OverrideLocationLabel, ""),
		OverrideAreas:         ptrUnlessSlice(row.OverrideAreas),
	}
}

// ptr returns a pointer to v (response builder helper).
func ptr[T any](v T) *T { return &v }

// ptrUnless is the response-projection helper that hides a wildcard/default value
// as JSON null (Part B of #138). It returns nil when v equals wildcard (the
// documented "match-any"/default sentinel for that field) so the envelope emits
// `null` — symmetric with the request side, where omitting a field means
// "match-any". For any meaningful (non-default) value it returns &v. Required
// fields and the active invasion/incident mode field bypass this helper (they use
// ptr/direct value and are always present).
func ptrUnless[T comparable](v, wildcard T) *T {
	if v == wildcard {
		return nil
	}
	return &v
}

// ptrUnlessSlice hides an empty slice as JSON null (the slice analogue of
// ptrUnless). A nil/empty slice → nil pointer-equivalent (returns nil slice,
// which the envelope emits as `null`); a non-empty slice is returned as-is.
func ptrUnlessSlice[T any](v []T) []T {
	if len(v) == 0 {
		return nil
	}
	return v
}

// RegisterV2TrackingPokemon registers the strict v2 pokemon tracking endpoints
// (list/create/get/put/delete/bulk-delete) via the generic resource helpers.
func RegisterV2TrackingPokemon(api huma.API, deps *TrackingDeps) {
	registerV2Tracking(api, deps, v2TrackingType[v2PokemonRule, db.MonsterTrackingAPI]{
		Name: "pokemon",
		Store: func(d *TrackingDeps) store.TrackingStore[db.MonsterTrackingAPI] {
			return d.Tracking.Monsters
		},
		Translate: translateV2Pokemon,
		ToRule:    pokemonRowToRule,
		GetUID:    store.MonsterGetUID,
		SetUID:    store.MonsterSetUID,
		RowText: func(d *TrackingDeps, tr *i18n.Translator, row *db.MonsterTrackingAPI) string {
			return d.RowText.MonsterRowText(tr, toMonsterTracking(row))
		},
	})
}
