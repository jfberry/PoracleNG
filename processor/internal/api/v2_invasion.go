package api

import (
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// invasion and incident BOTH back onto the `invasion` table (stored grunt_type
// string + gender int). A row is an INCIDENT iff its grunt_type is a known
// pokestop-event name; otherwise it is an INVASION (a pokemon-type name, or the
// catch-all "everything"/"boss"). isEventGruntType is the discriminator; the two
// types register opposite Filters over it so each endpoint sees only its rows.

// isEventGruntType reports whether a stored grunt_type string is a pokestop
// event name (lowercased gd.Util.PokestopEvent[*].Name). Comparison is
// lowercased on both sides. gd or its Util being nil ⇒ never an event.
//
// Disjointness assumption: the invasion/incident partition assumes the three
// grunt_type name spaces — pokemon-type names, the catch-all "everything"/"boss",
// and PokestopEvent names — are mutually disjoint sets (true in current data). A
// future PokestopEvent named like a pokemon type (or "everything"/"boss") would
// be misfiled by this discriminator.
func isEventGruntType(gruntType string, gd *gamedata.GameData) bool {
	if gd == nil || gd.Util == nil {
		return false
	}
	want := strings.ToLower(gruntType)
	for _, evt := range gd.Util.PokestopEvent {
		if strings.ToLower(evt.Name) == want {
			return true
		}
	}
	return false
}

// gdFromDeps returns the GameData reachable through the rowtext generator, or
// nil when unset (defence-in-depth; translation handlers 422 on a nil gd path).
func gdFromDeps(deps *TrackingDeps) *gamedata.GameData {
	if deps.RowText == nil {
		return nil
	}
	return deps.RowText.GD
}

// canonicalGruntTypes returns the set of grunt_type strings this endpoint
// accepts and emits: every template-derived grunt name (type names like
// "water" plus named grunts like "giovanni", "blanche", "mixed", "npc_0"),
// plus the two catch-alls.
//
// Pokestop-event names are deliberately absent — those rows live in the same
// table but belong to /incident, and letting both endpoints write the same
// name would break the partition.
//
// This mirrors the validTypes set the !invasion command builds, so the bot and
// the API accept the same vocabulary by construction rather than by
// coincidence.
func canonicalGruntTypes(gd *gamedata.GameData) map[string]bool {
	out := map[string]bool{"everything": true, "boss": true}
	if gd == nil {
		return out
	}
	for _, g := range gd.Grunts {
		if g == nil {
			continue
		}
		if name := gruntTypeName(gd, g); name != "" && !isEventGruntType(name, gd) {
			out[name] = true
		}
	}
	for _, ti := range gd.Types {
		if ti != nil && ti.Name != "" {
			out[strings.ToLower(ti.Name)] = true
		}
	}
	return out
}

// v2InvasionRule is the strict v2 invasion tracking request/response rule object.
//
// AT MOST ONE of {grunt_type, type_id, grunt_id, everything, boss} may be set
// per rule (>1 ⇒ 422). Omitting all of them is the wildcard and stores
// "everything" — v2 uses blank-means-wildcard throughout rather than requiring
// a magic value. All of them resolve to the stored grunt_type string the
// matcher compares against:
//   - grunt_type: stored as given, after validation against canonicalGruntTypes.
//   - type_id  (1-18): grunt_type = lower(gd.Types[type_id].Name); + optional gender.
//   - grunt_id (game-master id): grunt_type derived from the grunt's TypeID/template;
//     gender is the grunt's own gender (a specific grunt implies its gender).
//   - everything=true: grunt_type = "everything" (matches every invasion).
//   - boss=true:       grunt_type = "boss" (matches only boss encounters).
//
// READ CONTRACT: a read emits exactly ONE targeting field, and it is whatever
// the rule is stored as — today always grunt_type. type_id and grunt_id are
// write-side conveniences and are never emitted.
//
// That contract is what makes a future grunt_id COLUMN additive rather than
// breaking: rules stored as a grunt id would start reading back as grunt_id,
// while type-level and legacy rules keep reading back as grunt_type, and a
// client already handling "exactly one of these will be present" needs no
// change. Before this, reads emitted type_id when the stored name happened to
// be a pokemon type and NOTHING otherwise — 41 of the 59 distinct grunt_type
// values in shipped data, so GET → PUT was impossible for most rules (#209).
//
// gender is valid with type_id and grunt_type (("mixed", gender 1) has to be
// expressible and is not a type). gender + grunt_id/everything/boss ⇒ 422.
//
// (Pokestop-event rows live in the SAME table but are addressed via the separate
// /incident endpoint; see v2_incident.go.)
type v2InvasionRule struct {
	TypeID     *int    `json:"type_id,omitempty" minimum:"1" maximum:"18" doc:"Pokemon type id (1-18); grunt_type resolves to that type's lowercased name. At most one mode field may be set — this OR grunt_id OR grunt_type OR everything OR boss. Omitting all of them matches EVERY invasion (stored \"everything\") — blank is the wildcard, as elsewhere in v2. In responses, the active mode field is always present; the other mode fields are omitted/null."`
	GruntID    *int    `json:"grunt_id,omitempty" minimum:"0" doc:"Game-master grunt id; resolves to that grunt's name and implies its own gender. One-of mode field. Mutually exclusive with the others; gender not allowed. Read-back reports the rule as grunt_type, because that is what is stored — see grunt_type."`
	GruntType  *string `json:"grunt_type,omitempty" doc:"The stored targeting value, and the ONE targeting field every read emits. Accepts any grunt name the bot accepts: a pokemon type (\"water\"), a named grunt (\"giovanni\", \"blanche\", \"mixed\", \"npc_0\"), or a catch-all (\"everything\", \"boss\"). type_id and grunt_id are input conveniences that resolve to one of these. Pokestop-event names are not valid here — use /incident. Enumerate valid names from GET /api/masterdata/grunts. One-of mode field; may be combined with gender."`
	Everything *bool   `json:"everything,omitempty" doc:"Catch-all wildcard: set true to match EVERY invasion (stored grunt_type \"everything\"). Equivalent to omitting every mode field, which is the wildcard; set it explicitly when you want the intent on the wire. Mutually exclusive with the others; gender not allowed."`
	Boss       *bool   `json:"boss,omitempty" doc:"Catch-all for boss encounters: set true to match only boss invasions (stored grunt_type \"boss\"). One-of mode field. Mutually exclusive with the others; gender not allowed."`
	Gender     *string `json:"gender,omitempty" nullable:"true" enum:"any,male,female" doc:"Grunt gender filter: any|male|female. Omit to match any gender (defaults to 'any', stored as 0). ONLY valid together with type_id. Returned as null when 'any' (or when not in type_id mode)."`

	// Common fields. invasion HAS a clean column ⇒ clean/edit/summary apply.
	Distance *int    `json:"distance,omitempty" minimum:"0" maximum:"40000000" nullable:"true" doc:"Radius in metres around the anchor location. Omit (or 0) to match by the profile's geofence areas instead of a radius — 0 means area-based, NOT zero metres (stored as 0). Returned as null when at its wildcard."`
	Template *string `json:"template,omitempty" nullable:"true" doc:"DTS template name. Omit (or empty) to use the server's configured default template (stored as \"\"). Returned as null when at its wildcard."`
	Clean    *bool   `json:"clean,omitempty" nullable:"true" doc:"Auto-delete the alert on expiry (clean bitmask bit 1). Omit to disable (default false). Returned as null when false."`
	Edit     *bool   `json:"edit,omitempty" nullable:"true" doc:"Keep the message updated in place (clean bitmask bit 2). Omit to disable (default false). Returned as null when false."`
	Summary  *bool   `json:"summary,omitempty" nullable:"true" doc:"Route into the summary digest (clean bitmask bit 4). Omit to disable (default false). Returned as null when false."`

	OverrideLocationLabel *string  `json:"override_location_label,omitempty" nullable:"true" doc:"Saved-location label to use instead of the profile location (requires distance > 0; mutually exclusive with override_areas). Omit for none. Returned as null when unset."`
	OverrideAreas         []string `json:"override_areas,omitempty" doc:"Restrict this rule to these geofence areas (mutually exclusive with distance > 0 and override_location_label). Omit for none. Returned as null when unset."`
}

// translateV2Invasion converts a strict v2 invasion rule into the stored
// InvasionTrackingAPI. It enforces exactly-one-mode and gender placement, then
// down-translates the chosen mode into (grunt_type, gender). ping is "" (server
// managed); clean packs the bitmask.
func translateV2Invasion(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *v2InvasionRule) (db.InvasionTrackingAPI, error) {
	gd := gdFromDeps(deps)

	// Exactly-one-mode.
	modes := 0
	if req.TypeID != nil {
		modes++
	}
	if req.GruntID != nil {
		modes++
	}
	if req.GruntType != nil {
		modes++
	}
	if req.Everything != nil && *req.Everything {
		modes++
	}
	if req.Boss != nil && *req.Boss {
		modes++
	}
	if modes > 1 {
		return db.InvasionTrackingAPI{}, huma.Error422UnprocessableEntity(
			"grunt_type, type_id, grunt_id, everything and boss are mutually exclusive (set at most one)")
	}

	// gender is only valid with the name-shaped modes: a specific grunt_id
	// already implies its gender, and the catch-alls have none.
	if req.Gender != nil && req.TypeID == nil && req.GruntType == nil {
		return db.InvasionTrackingAPI{}, huma.Error422UnprocessableEntity(
			"gender is only valid together with type_id or grunt_type")
	}

	var gruntType string
	gender := 0

	switch {
	case req.TypeID != nil:
		ti, ok := lookupType(gd, *req.TypeID)
		if !ok {
			return db.InvasionTrackingAPI{}, huma.Error422UnprocessableEntity(
				"unknown type_id")
		}
		gruntType = strings.ToLower(ti.Name)
		gender = invasionGenderEnum.resolveStored(req.Gender)

	case req.GruntType != nil:
		// Normalise the spelling first: legacy rows hold "npc 0" where the
		// canonical set holds "npc_0". Storing the normalised form repairs the
		// rule as well as accepting it — the space form has never matched.
		name := gamedata.NormaliseGruntTypeName(*req.GruntType)
		if !canonicalGruntTypes(gd)[name] {
			if isEventGruntType(name, gd) {
				return db.InvasionTrackingAPI{}, huma.Error422UnprocessableEntity(
					"grunt_type " + name + " is a pokestop event — track it via the incident endpoint")
			}
			return db.InvasionTrackingAPI{}, huma.Error422UnprocessableEntity(
				"unknown grunt_type: " + name)
		}
		gruntType = name
		gender = invasionGenderEnum.resolveStored(req.Gender)

	case req.GruntID != nil:
		grunt, ok := lookupGrunt(gd, *req.GruntID)
		if !ok {
			return db.InvasionTrackingAPI{}, huma.Error422UnprocessableEntity(
				"unknown grunt_id")
		}
		gruntType = gruntTypeName(gd, grunt)
		gender = grunt.Gender // a specific grunt implies its gender (1 or 2)

	case req.Everything != nil && *req.Everything:
		gruntType = "everything"

	case req.Boss != nil && *req.Boss:
		gruntType = "boss"

	default:
		// No targeting field at all is the wildcard. v2 uses blank-means-
		// wildcard consistently rather than requiring a magic value, so an
		// omitted target reads the same way here as an omitted filter does on
		// every other type. Stored as "everything", which is the value the
		// matcher compares against.
		//
		// Note this is NOT bot parity: bare !invasion prints usage rather than
		// tracking everything. The convention is the reason, not the command.
		gruntType = "everything"
	}

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
		Gender:                gender,
		GruntType:             gruntType,
		Clean:                 packClean(valueOr(req.Clean, false), valueOr(req.Edit, false), valueOr(req.Summary, false)),
		OverrideLocationLabel: overrideLabel,
		OverrideAreas:         normalizeOverrideAreas(req.OverrideAreas),
	}
	return row, nil
}

// lookupType / lookupGrunt are nil-safe accessors over GameData.
func lookupType(gd *gamedata.GameData, id int) (*gamedata.TypeInfo, bool) {
	if gd == nil {
		return nil, false
	}
	ti, ok := gd.Types[id]
	if !ok || ti == nil {
		return nil, false
	}
	return ti, true
}

func lookupGrunt(gd *gamedata.GameData, id int) (*gamedata.Grunt, bool) {
	if gd == nil {
		return nil, false
	}
	g, ok := gd.Grunts[id]
	if !ok || g == nil {
		return nil, false
	}
	return g, true
}

// gruntTypeName resolves a grunt to its stored grunt_type string, mirroring
// matching.ResolveGruntTypeName's regular-grunt branch: typed grunts resolve via
// TypeID → type name; untyped grunts derive from the template.
func gruntTypeName(gd *gamedata.GameData, grunt *gamedata.Grunt) string {
	if grunt.TypeID > 0 && gd != nil {
		if ti, ok := gd.Types[grunt.TypeID]; ok && ti != nil {
			return strings.ToLower(ti.Name)
		}
	}
	return gamedata.TypeNameFromTemplate(grunt.Template)
}

// v2InvasionToRule converts a stored InvasionTrackingAPI back into the strict v2
// rule shape.
//
// It emits exactly ONE targeting field — grunt_type, the value the row actually
// stores — for every rule, the "everything"/"boss" catch-alls included. type_id
// and grunt_id are write-side conveniences that resolve to a grunt_type on the
// way in and are never emitted. gender rides alongside for the name-shaped
// rules and is nulled at its 'any' wildcard.
func v2InvasionToRule(gd *gamedata.GameData, row *db.InvasionTrackingAPI) v2InvasionRule {
	rule := v2InvasionRule{
		Distance:              ptrUnless(row.Distance, 0),
		Template:              ptrUnless(row.Template, ""),
		Clean:                 ptrUnless(db.IsClean(row.Clean), false),
		Edit:                  ptrUnless(db.IsEdit(row.Clean), false),
		Summary:               ptrUnless(db.IsSummary(row.Clean), false),
		OverrideLocationLabel: ptrUnless(row.OverrideLocationLabel, ""),
		OverrideAreas:         ptrUnlessSlice(row.OverrideAreas),
	}

	// Emit exactly one targeting field: what the rule is STORED as.
	//
	// Every value this codebase can WRITE reads back as a value
	// translateV2Invasion accepts, so GET → PUT round-trips (#209).
	//
	// Legacy data is the caveat. A row written before the canonical set used
	// underscores may hold a space-separated name ("npc 0", "player team
	// leader"). translateV2Invasion normalises those on the way in, so they
	// round-trip too — but they round-trip to the UNDERSCORE form, which is a
	// different string from the one that was read. A client diffing read
	// against write will see that change; it is a repair, not a drift (see
	// normaliseGruntTypeInput).
	name := strings.ToLower(row.GruntType)
	rule.GruntType = &name
	if name != "everything" && name != "boss" {
		rule.Gender = ptrUnless(invasionGenderEnum.fromStored(row.Gender), "any")
	}
	return rule
}

// RegisterV2TrackingInvasion registers the strict v2 invasion tracking endpoints.
// The Filter restricts every read path to NON-event rows (event rows belong to
// the /incident endpoint), so the two types coexist in the shared invasion table.
func RegisterV2TrackingInvasion(api huma.API, deps *TrackingDeps) {
	gd := gdFromDeps(deps)
	registerV2Tracking(api, deps, v2TrackingType[v2InvasionRule, db.InvasionTrackingAPI]{
		Name: "invasion",
		Store: func(d *TrackingDeps) store.TrackingStore[db.InvasionTrackingAPI] {
			return d.Tracking.Invasions
		},
		Translate: translateV2Invasion,
		ToRule: func(row *db.InvasionTrackingAPI) v2InvasionRule {
			return v2InvasionToRule(gd, row)
		},
		GetUID: store.InvasionGetUID,
		SetUID: store.InvasionSetUID,
		RowText: func(d *TrackingDeps, tr *i18n.Translator, row *db.InvasionTrackingAPI) string {
			return d.RowText.InvasionRowText(tr, toInvasionTracking(row))
		},
		Filter: func(row *db.InvasionTrackingAPI) bool {
			return !isEventGruntType(row.GruntType, gd)
		},
	})
}
