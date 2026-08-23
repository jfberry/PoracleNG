package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2_tracking.go is the GENERIC resource-helper layer for the strict /api/v2
// tracking surface. The 10 remaining tracking types plug into this without
// re-implementing the human-scoped addressing, (human,uid) ownership scoping,
// diff/insert flow, confirmation push, or response shaping.
//
// A new type registers by supplying a *v2TrackingType[Req, T] describing:
//   - its strict request struct Req and stored *API struct T
//   - the TrackingStore[T] for its table
//   - translate: strict Req → *T (defaults, enum→int, clean bitmask, override fields)
//   - getUID/setUID accessors
//   - rowText: render a rule's human-readable description (for include_descriptions)
//   - removedPrefix-style i18n is handled centrally here
//
// then calls registerV2Tracking(api, deps, typ). RegisterV2TrackingPokemon in
// v2_pokemon.go is the worked example.

// v2RuleEnvelope is the wire shape of a single rule in a v2 response. The rule
// fields are the strict Req object (so reads and writes share one schema), plus
// the server-assigned uid and an optional human-readable description.
//
// We model it generically via the embedded Req so each type's response carries
// exactly that type's fields. uid + description are siblings.
type v2RuleEnvelope[Req any] struct {
	UID         int64  `json:"uid"`
	Description string `json:"description,omitempty"`
	Rule        Req    `json:"-"` // flattened at marshal time

	// ProfileNo, when non-nil, adds the rule's profile_no to the flattened
	// object. It is set ONLY by the full-snapshot endpoint in all_profiles mode
	// (so a client can tell which profile a rule belongs to). The per-type list
	// endpoints leave it nil, so their wire shape and OpenAPI schema are
	// unchanged.
	ProfileNo *int `json:"-"`
}

// v2TrackingType captures everything the generic layer needs to serve one
// tracking type's strict CRUD endpoints.
type v2TrackingType[Req any, T any] struct {
	// Name is the URL path segment, OpenAPI tag, and the noun used in op
	// summaries (e.g. "pokemon" → "List pokemon tracking rules").
	Name string

	// Store is the typed store for this table.
	Store func(deps *TrackingDeps) store.TrackingStore[T]

	// Translate converts a validated strict request into a stored *T row,
	// applying documented defaults, enum→int, the clean bitmask, profile, and
	// override fields. It returns an *huma error on validation failure
	// (mutually-exclusive override rules, etc.).
	//
	// humanID + profileNo are resolved by the generic layer. oc is the
	// pre-fetched override context (built once per request, outside any loop).
	Translate func(deps *TrackingDeps, humanID string, profileNo int, oc overrideContext, req *Req) (T, error)

	// ToRule converts a stored row back into the strict request shape for
	// responses (the inverse of Translate, minus override-context lookups).
	ToRule func(row *T) Req

	// GetUID / SetUID access the uid field on T.
	GetUID func(*T) int64
	SetUID func(*T, int64)

	// RowText renders a rule's human-readable description in the human's
	// language (for ?include_descriptions=true). tr is the human's translator.
	RowText func(deps *TrackingDeps, tr *i18n.Translator, row *T) string

	// Filter, when non-nil, restricts every READ/scoping path to rows for which
	// it returns true. This lets two tracking types share one underlying store
	// (table) yet each only see its own rows: invasion and incident both back
	// onto the invasion table, discriminated by grunt_type. nil = no filter (the
	// common single-type-per-store case; behaviour identical to before).
	//
	// Applied on: list, get-by-uid (a row failing Filter → 404), delete-by-uid
	// and bulk-delete (a uid whose row fails Filter → 404 / skipped), and the
	// existing-rows set fed into the create/PUT diff (so one type never diffs
	// against the other's rows).
	Filter func(*T) bool
}

// passesFilter reports whether row is visible to this type. A nil Filter admits
// everything (the single-type-per-store case).
func (typ v2TrackingType[Req, T]) passesFilter(row *T) bool {
	return typ.Filter == nil || typ.Filter(row)
}

// scopedRows returns the human+profile rows visible to this type (Filter applied
// when set). It is the single read entrypoint every CRUD path funnels through so
// a shared-store type can never observe another type's rows.
func (typ v2TrackingType[Req, T]) scopedRows(deps *TrackingDeps, humanID string, profileNo int) ([]T, error) {
	rows, err := typ.Store(deps).SelectByIDProfile(humanID, profileNo)
	if err != nil {
		return nil, err
	}
	if typ.Filter == nil {
		return rows, nil
	}
	out := rows[:0:0]
	for i := range rows {
		if typ.Filter(&rows[i]) {
			out = append(out, rows[i])
		}
	}
	return out, nil
}

// --- huma input/output types ---------------------------------------------

// profileSentinel marks "profile query param not supplied"; the handler then
// falls back to the human's active profile. huma does not support pointer query
// params, so we use a default-valued int sentinel instead of *int.
const profileSentinel = -1

// v2ListInput is the input for list + bulk-delete (collection-level) ops.
type v2ListInput struct {
	ID                  string `path:"id" doc:"Human id (the owning user); ownership scope for every rule"`
	Profile             int    `query:"profile" default:"-1" doc:"Profile number; defaults to the human's active profile"`
	IncludeDescriptions bool   `query:"include_descriptions" doc:"Add a human-readable description to each rule in the response"`
	Silent              bool   `query:"silent" doc:"Apply without sending the confirmation push (mutations only)"`
}

// v2ItemInput is the input for single-item ops (get/put/delete by uid).
type v2ItemInput struct {
	ID                  string `path:"id" doc:"Human id (the owning user); ownership scope"`
	UID                 int64  `path:"uid" doc:"Tracking rule uid (scoped to this human)"`
	Profile             int    `query:"profile" default:"-1" doc:"Profile number; defaults to the human's active profile"`
	IncludeDescriptions bool   `query:"include_descriptions" doc:"Add a human-readable description to the rule in the response"`
	Silent              bool   `query:"silent" doc:"Apply without sending the confirmation push (mutations only)"`
}

// v2BulkDeleteInput is the input for collection bulk-delete (?uid=1,2,3).
type v2BulkDeleteInput struct {
	ID                  string `path:"id" doc:"Human id (the owning user); ownership scope"`
	Profile             int    `query:"profile" default:"-1" doc:"Profile number; defaults to the human's active profile"`
	UID                 string `query:"uid" required:"true" doc:"Comma-separated rule uids to delete (e.g. 1,2,3)"`
	IncludeDescriptions bool   `query:"include_descriptions" doc:"Add a human-readable description to each deleted rule"`
	Silent              bool   `query:"silent" doc:"Apply without sending the confirmation push"`
}

// v2SendConfirmation is the seam the v2 push functions use to dispatch the
// confirmation message. It defaults to sendConfirmation; tests override it to
// observe whether (and what) a push was attempted, especially to assert that
// ?silent=true suppresses it entirely.
var v2SendConfirmation = sendConfirmation

// resolveHuman looks up the human and resolves the effective profile number.
// Returns an huma 404 when the human does not exist.
func resolveHuman(deps *TrackingDeps, id string, profile int) (*store.HumanLite, int, error) {
	human, err := deps.Humans.GetLite(id)
	if err != nil {
		return nil, 0, huma.Error500InternalServerError("database error")
	}
	if human == nil {
		return nil, 0, huma.Error404NotFound("human not found")
	}
	profileNo := human.CurrentProfileNo
	if profile != profileSentinel {
		profileNo = profile
	}
	return human, profileNo, nil
}

// v2SnapshotProvider is a type-erased view over one registered tracking type,
// used by the full-snapshot endpoint (GET /v2/humans/{id}/tracking). It reuses
// the EXACT same scoping + envelope-building logic the per-type list endpoint
// uses, so the rules in the snapshot are byte-identical to the per-type list for
// the same (human, profile, includeDesc). The only addition is the optional
// profile_no field injected in all_profiles mode (see buildSnapshotEnvelopes).
//
// typeName is the tracking-type URL segment (the snapshot's "tracking" map key).
// When allProfiles is true, the provider spans every profile the human owns and
// each returned rule carries its profile_no.
type v2SnapshotProvider struct {
	// TypeName is the "tracking" map key (e.g. "pokemon", "invasion", "incident").
	TypeName string
	// Rules returns the rules-as-envelopes (already JSON-marshalled) for this
	// type, scoped to the human (and profile, or all profiles).
	Rules func(human *store.HumanLite, profiles []store.Profile, profileNo int, allProfiles, includeDesc bool) ([]json.RawMessage, error)
}

// registerV2Tracking wires the six strict endpoints for one tracking type. It
// also appends a snapshot provider to deps so the full-snapshot endpoint can
// serve this type's rules via the same list logic. The provider list lives on
// the per-instance *TrackingDeps (built once per server/test), so it is NOT a
// leaky package-level global: a fresh deps starts with an empty provider list.
func registerV2Tracking[Req any, T any](api huma.API, deps *TrackingDeps, typ v2TrackingType[Req, T]) {
	deps.v2SnapshotProviders = append(deps.v2SnapshotProviders, v2SnapshotProvider{
		TypeName: typ.Name,
		Rules: func(human *store.HumanLite, profiles []store.Profile, profileNo int, allProfiles, includeDesc bool) ([]json.RawMessage, error) {
			return buildSnapshotRules(deps, typ, human, profiles, profileNo, allProfiles, includeDesc)
		},
	})
	registerV2TrackingOps(api, deps, typ)
}

// registerV2TrackingOps wires the six strict endpoints for one tracking type.
func registerV2TrackingOps[Req any, T any](api huma.API, deps *TrackingDeps, typ v2TrackingType[Req, T]) {
	base := "/v2/humans/{id}/tracking/" + typ.Name
	tag := []string{"v2-tracking"}
	sec := []map[string][]string{{"poracleSecret": {}}}

	// GET list → {rules:[...]}
	huma.Register(api, huma.Operation{
		OperationID: "v2-list-" + typ.Name, Method: "GET", Path: base,
		Summary: "List " + typ.Name + " tracking rules",
		Description: "Returns {rules:[...]} for the human's selected profile. " +
			"With ?include_descriptions=true each rule gains a human-readable description.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2ListInput) (*v2ListOutput[Req], error) {
		human, profileNo, err := resolveHuman(deps, in.ID, in.Profile)
		if err != nil {
			return nil, err
		}
		rows, err := typ.scopedRows(deps, human.ID, profileNo)
		if err != nil {
			log.Errorf("v2 tracking %s list: %s", typ.Name, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		out := &v2ListOutput[Req]{}
		out.Body.Rules = buildRuleEnvelopes(deps, typ, human, rows, in.IncludeDescriptions)
		return out, nil
	})

	// POST create → {created, updated, unchanged}
	huma.Register(api, huma.Operation{
		OperationID: "v2-create-" + typ.Name, Method: "POST", Path: base,
		Summary: "Create " + typ.Name + " tracking rule(s)",
		Description: "Body is an array of strict rule objects. Returns {created,updated,unchanged} " +
			"(POST keeps the diff/upsert behaviour). With ?include_descriptions=true each rule gains a description. " +
			"?silent=true suppresses the confirmation push.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2CreateInput[Req]) (*v2CreateOutput[Req], error) {
		return v2HandleCreate(deps, typ, in)
	})

	// GET by uid → {rules:[<one>]}
	huma.Register(api, huma.Operation{
		OperationID: "v2-get-" + typ.Name, Method: "GET", Path: base + "/{uid}",
		Summary:     "Fetch one " + typ.Name + " tracking rule",
		Description: "Scoped by (human, uid). 404 if the uid is not owned by this human.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2ItemInput) (*v2ListOutput[Req], error) {
		human, profileNo, err := resolveHuman(deps, in.ID, in.Profile)
		if err != nil {
			return nil, err
		}
		row, err := v2FindOwnedRow(typ, deps, human.ID, profileNo, in.UID)
		if err != nil {
			return nil, err
		}
		out := &v2ListOutput[Req]{}
		out.Body.Rules = buildRuleEnvelopes(deps, typ, human, []T{*row}, in.IncludeDescriptions)
		return out, nil
	})

	// PUT full-replace → {updated:[<new uid>]}
	huma.Register(api, huma.Operation{
		OperationID: "v2-put-" + typ.Name, Method: "PUT", Path: base + "/{uid}",
		Summary: "Full-replace one " + typ.Name + " tracking rule",
		Description: "Full replace: the body fully specifies the rule; omitted filters reset to documented defaults. " +
			"The engine is delete+insert, so the replacement rule receives a NEW uid (returned in {updated}). " +
			"Scoped by (human, uid); 404 if the uid is not owned by this human.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2PutInput[Req]) (*v2CreateOutput[Req], error) {
		return v2HandlePut(deps, typ, in)
	})

	// DELETE by uid → {deleted:[...]}
	huma.Register(api, huma.Operation{
		OperationID: "v2-delete-" + typ.Name, Method: "DELETE", Path: base + "/{uid}",
		Summary:     "Delete one " + typ.Name + " tracking rule",
		Description: "Scoped by (human, uid). Returns {deleted:[...]}. 404 if the uid is not owned by this human.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2ItemInput) (*v2DeleteOutput[Req], error) {
		human, profileNo, err := resolveHuman(deps, in.ID, in.Profile)
		if err != nil {
			return nil, err
		}
		row, err := v2FindOwnedRow(typ, deps, human.ID, profileNo, in.UID)
		if err != nil {
			return nil, err
		}
		if err := typ.Store(deps).DeleteByUID(human.ID, in.UID); err != nil {
			log.Errorf("v2 tracking %s delete: %s", typ.Name, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		v2PushRemoved(deps, typ, human, []T{*row}, in.Silent)
		out := &v2DeleteOutput[Req]{}
		out.Body.Deleted = buildRuleEnvelopes(deps, typ, human, []T{*row}, in.IncludeDescriptions)
		return out, nil
	})

	// DELETE bulk (?uid=1,2,3) → {deleted:[...]}
	huma.Register(api, huma.Operation{
		OperationID: "v2-bulk-delete-" + typ.Name, Method: "DELETE", Path: base,
		Summary:     "Bulk-delete " + typ.Name + " tracking rules",
		Description: "Deletes the comma-separated ?uid=1,2,3 rules (scoped to this human). Returns {deleted:[...]}.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2BulkDeleteInput) (*v2DeleteOutput[Req], error) {
		human, profileNo, err := resolveHuman(deps, in.ID, in.Profile)
		if err != nil {
			return nil, err
		}
		uids, err := parseUIDList(in.UID)
		if err != nil {
			return nil, err
		}
		rows, err := typ.scopedRows(deps, human.ID, profileNo)
		if err != nil {
			return nil, huma.Error500InternalServerError("database error")
		}
		// Only the requested uids whose rows are visible to this type are acted
		// on; a uid belonging to the other shared-store type is silently skipped
		// (it never appears in scopedRows, so owned excludes it).
		owned := filterOwnedRows(typ, rows, uids)
		ownedUIDs := make([]int64, len(owned))
		for i := range owned {
			ownedUIDs[i] = typ.GetUID(&owned[i])
		}
		if err := typ.Store(deps).DeleteByUIDs(human.ID, ownedUIDs); err != nil {
			log.Errorf("v2 tracking %s bulk delete: %s", typ.Name, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		v2PushRemoved(deps, typ, human, owned, in.Silent)
		out := &v2DeleteOutput[Req]{}
		out.Body.Deleted = buildRuleEnvelopes(deps, typ, human, owned, in.IncludeDescriptions)
		return out, nil
	})
}

// --- create / put handlers -----------------------------------------------

// v2CreateInput is a POST body of strict rule objects (array). Params are
// declared inline (not via embedding) because huma's param discovery does not
// reliably promote params from an embedded struct that also carries a Body.
type v2CreateInput[Req any] struct {
	ID                  string `path:"id" doc:"Human id (the owning user); ownership scope for every rule"`
	Profile             int    `query:"profile" default:"-1" doc:"Profile number; defaults to the human's active profile"`
	IncludeDescriptions bool   `query:"include_descriptions" doc:"Add a human-readable description to each rule in the response"`
	Silent              bool   `query:"silent" doc:"Apply without sending the confirmation push"`
	Body                []Req
}

// v2PutInput is a PUT body of a single strict rule object.
type v2PutInput[Req any] struct {
	ID                  string `path:"id" doc:"Human id (the owning user); ownership scope"`
	UID                 int64  `path:"uid" doc:"Tracking rule uid (scoped to this human)"`
	Profile             int    `query:"profile" default:"-1" doc:"Profile number; defaults to the human's active profile"`
	IncludeDescriptions bool   `query:"include_descriptions" doc:"Add a human-readable description to the rule in the response"`
	Silent              bool   `query:"silent" doc:"Apply without sending the confirmation push"`
	Body                Req
}

func v2HandleCreate[Req any, T any](deps *TrackingDeps, typ v2TrackingType[Req, T], in *v2CreateInput[Req]) (*v2CreateOutput[Req], error) {
	human, profileNo, err := resolveHuman(deps, in.ID, in.Profile)
	if err != nil {
		return nil, err
	}
	if len(in.Body) == 0 {
		return nil, huma.Error422UnprocessableEntity("body must contain at least one rule")
	}

	oc, ocMsg, ocCode := newOverrideContext(deps, human.ID)
	if ocMsg != "" {
		return nil, humaErr(ocCode, ocMsg)
	}

	candidates := make([]T, 0, len(in.Body))
	for i := range in.Body {
		row, terr := typ.Translate(deps, human.ID, profileNo, oc, &in.Body[i])
		if terr != nil {
			return nil, terr
		}
		candidates = append(candidates, row)
	}

	// Scope the existing set through Filter so a shared-store type never diffs
	// (and thus never deletes-as-update) against the other type's rows.
	existing, err := typ.scopedRows(deps, human.ID, profileNo)
	if err != nil {
		return nil, huma.Error500InternalServerError("database error")
	}

	diff, err := store.ApplyDiff(typ.Store(deps), human.ID, existing, candidates, typ.GetUID, typ.SetUID)
	if err != nil {
		log.Errorf("v2 tracking %s create: %s", typ.Name, err)
		return nil, huma.Error500InternalServerError("database error")
	}

	reloadState(deps)
	v2PushDiff(deps, typ, human, diff, in.Silent)

	out := &v2CreateOutput[Req]{}
	out.Body.Created = buildRuleEnvelopes(deps, typ, human, diff.Inserts, in.IncludeDescriptions)
	out.Body.Updated = buildRuleEnvelopes(deps, typ, human, diff.Updates, in.IncludeDescriptions)
	out.Body.Unchanged = buildRuleEnvelopes(deps, typ, human, diff.AlreadyPresent, in.IncludeDescriptions)
	return out, nil
}

func v2HandlePut[Req any, T any](deps *TrackingDeps, typ v2TrackingType[Req, T], in *v2PutInput[Req]) (*v2CreateOutput[Req], error) {
	human, profileNo, err := resolveHuman(deps, in.ID, in.Profile)
	if err != nil {
		return nil, err
	}

	// Ownership guard: the addressed uid must exist and belong to this human.
	if _, err := v2FindOwnedRow(typ, deps, human.ID, profileNo, in.UID); err != nil {
		return nil, err
	}

	oc, ocMsg, ocCode := newOverrideContext(deps, human.ID)
	if ocMsg != "" {
		return nil, humaErr(ocCode, ocMsg)
	}

	row, terr := typ.Translate(deps, human.ID, profileNo, oc, &in.Body)
	if terr != nil {
		return nil, terr
	}

	// App-level identity validation, mirroring the insert path's
	// DiffAndClassify semantics: a body that is an exact duplicate of a
	// DIFFERENT rule is rejected before anything is deleted. Constraint
	// management is deliberately application-side in this schema (no DB
	// transactions); without this check, the delete-then-insert below could
	// destroy the addressed rule when the insert collides — deterministic
	// data loss on databases still carrying the legacy invasion/lures
	// unique keys, a silent duplicate on the keyless tables.
	existing, err := typ.scopedRows(deps, human.ID, profileNo)
	if err != nil {
		return nil, huma.Error500InternalServerError("database error")
	}
	for i := range existing {
		if typ.GetUID(&existing[i]) == in.UID {
			continue
		}
		if noMatch, isDup, _, _ := db.DiffTracking(&existing[i], &row); !noMatch && isDup {
			return nil, huma.Error409Conflict(fmt.Sprintf(
				"an identical rule already exists (uid %d)", typ.GetUID(&existing[i])))
		}
	}

	// Full replace = delete the old uid, insert the fully-specified body (new uid).
	if err := typ.Store(deps).DeleteByUID(human.ID, in.UID); err != nil {
		log.Errorf("v2 tracking %s put delete: %s", typ.Name, err)
		return nil, huma.Error500InternalServerError("database error")
	}
	newUID, err := typ.Store(deps).Insert(&row)
	if err != nil {
		log.Errorf("v2 tracking %s put insert: %s", typ.Name, err)
		return nil, huma.Error500InternalServerError("database error")
	}
	typ.SetUID(&row, newUID)

	reloadState(deps)
	v2PushDiff(deps, typ, human, store.DiffResult[T]{Updates: []T{row}}, in.Silent)

	out := &v2CreateOutput[Req]{}
	out.Body.Updated = buildRuleEnvelopes(deps, typ, human, []T{row}, in.IncludeDescriptions)
	return out, nil
}

// --- helpers --------------------------------------------------------------

// v2FindOwnedRow returns the row with the given uid owned by humanID+profile, or
// an huma 404 if not found. This is the handler-layer ownership guard; the SQL
// layer also enforces WHERE id=? AND uid=?.
func v2FindOwnedRow[Req any, T any](typ v2TrackingType[Req, T], deps *TrackingDeps, humanID string, profileNo int, uid int64) (*T, error) {
	rows, err := typ.Store(deps).SelectByIDProfile(humanID, profileNo)
	if err != nil {
		return nil, huma.Error500InternalServerError("database error")
	}
	for i := range rows {
		if typ.GetUID(&rows[i]) == uid {
			// A uid that exists but belongs to the other type sharing this store
			// (Filter rejects it) is, for this endpoint, "not found".
			if !typ.passesFilter(&rows[i]) {
				break
			}
			return &rows[i], nil
		}
	}
	return nil, huma.Error404NotFound(fmt.Sprintf("%s rule %d not found for this human", typ.Name, uid))
}

// filterOwnedRows returns the subset of rows whose uid is in the requested set.
func filterOwnedRows[Req any, T any](typ v2TrackingType[Req, T], rows []T, uids []int64) []T {
	want := make(map[int64]bool, len(uids))
	for _, u := range uids {
		want[u] = true
	}
	var out []T
	for i := range rows {
		if want[typ.GetUID(&rows[i])] {
			out = append(out, rows[i])
		}
	}
	return out
}

// buildRuleEnvelopes converts stored rows to response rule objects, optionally
// attaching a description. Each envelope flattens the strict Req fields plus uid
// (and description when requested) via custom marshalling.
func buildRuleEnvelopes[Req any, T any](deps *TrackingDeps, typ v2TrackingType[Req, T], human *store.HumanLite, rows []T, includeDesc bool) []v2RuleEnvelope[Req] {
	if len(rows) == 0 {
		return []v2RuleEnvelope[Req]{}
	}
	var tr *i18n.Translator
	if includeDesc {
		tr = translatorFor(deps, human)
	}
	out := make([]v2RuleEnvelope[Req], len(rows))
	for i := range rows {
		env := v2RuleEnvelope[Req]{
			UID:  typ.GetUID(&rows[i]),
			Rule: typ.ToRule(&rows[i]),
		}
		if includeDesc {
			env.Description = typ.RowText(deps, tr, &rows[i])
		}
		out[i] = env
	}
	return out
}

// buildSnapshotRules returns this type's rules-as-marshalled-JSON for the
// full-snapshot endpoint. It reuses the SAME scopedRows + buildRuleEnvelopes
// path the per-type list endpoint uses, so a snapshot rule is byte-identical to
// the per-type list rule for the same (human, profile, includeDesc) — the only
// difference is the optional profile_no injected in all_profiles mode.
//
// Single-profile mode (allProfiles=false): one scopedRows(profileNo) read,
// envelopes without profile_no — exactly the per-type list shape.
//
// all_profiles mode: enumerate every profile the human owns, read each, and
// stamp profile_no onto every envelope so a client can tell them apart. (The
// per-type list logic lists one profile; spanning profiles is the snapshot's
// job, done here by looping the human's profiles — mirroring v1 allProfiles,
// which is profile-agnostic.)
func buildSnapshotRules[Req any, T any](
	deps *TrackingDeps,
	typ v2TrackingType[Req, T],
	human *store.HumanLite,
	profiles []store.Profile,
	profileNo int,
	allProfiles, includeDesc bool,
) ([]json.RawMessage, error) {
	humanID := human.ID

	emit := func(rows []T, stampProfile *int) ([]json.RawMessage, error) {
		envs := buildRuleEnvelopes(deps, typ, human, rows, includeDesc)
		out := make([]json.RawMessage, len(envs))
		for i := range envs {
			envs[i].ProfileNo = stampProfile
			b, err := json.Marshal(envs[i])
			if err != nil {
				return nil, err
			}
			out[i] = b
		}
		return out, nil
	}

	if !allProfiles {
		rows, err := typ.scopedRows(deps, humanID, profileNo)
		if err != nil {
			return nil, err
		}
		return emit(rows, nil)
	}

	out := []json.RawMessage{}
	for i := range profiles {
		pn := profiles[i].ProfileNo
		rows, err := typ.scopedRows(deps, humanID, pn)
		if err != nil {
			return nil, err
		}
		stamp := pn
		marshalled, err := emit(rows, &stamp)
		if err != nil {
			return nil, err
		}
		out = append(out, marshalled...)
	}
	return out, nil
}

// parseUIDList parses a comma-separated uid list into []int64. Any non-int token
// or an empty list yields a 422.
func parseUIDList(s string) ([]int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, huma.Error422UnprocessableEntity("uid query parameter is required (comma-separated)")
	}
	parts := strings.Split(s, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, huma.Error422UnprocessableEntity("invalid uid list: empty token")
		}
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid uid: " + p)
		}
		out = append(out, v)
	}
	return out, nil
}

// humaErr maps an (httpStatus, message) pair from the shared override
// validators into an huma error. The override validators predate v2 and return
// 400 for validation failures; v2 is strict and surfaces these as 422
// problem+json (the default branch). 404/500 pass through unchanged.
func humaErr(code int, msg string) error {
	switch code {
	case 404:
		return huma.Error404NotFound(msg)
	case 500:
		return huma.Error500InternalServerError(msg)
	default:
		return huma.Error422UnprocessableEntity(msg)
	}
}

// --- confirmation pushes --------------------------------------------------

// v2PushDiff builds and sends the assembled, prefixed confirmation message for a
// create/put diff (new/updated/unchanged), exactly as v1's HandleCreateMonster
// does. The assembled message is the Discord/Telegram push only — never the HTTP
// body. Skipped when silent.
func v2PushDiff[Req any, T any](deps *TrackingDeps, typ v2TrackingType[Req, T], human *store.HumanLite, diff store.DiffResult[T], silent bool) {
	if silent {
		return
	}
	tr := translatorFor(deps, human)
	language := resolveLanguage(deps, human)

	total := len(diff.AlreadyPresent) + len(diff.Updates) + len(diff.Inserts)
	var message string
	if total > 50 {
		message = tr.Tf("tracking.bulk_changes",
			bot.CommandPrefixForType(deps.Config, human.Type), tr.T("tracking.tracked"))
	} else {
		var sb strings.Builder
		for i := range diff.AlreadyPresent {
			sb.WriteString(tr.T("tracking.unchanged"))
			sb.WriteString(typ.RowText(deps, tr, &diff.AlreadyPresent[i]))
			sb.WriteByte('\n')
		}
		for i := range diff.Updates {
			sb.WriteString(tr.T("tracking.updated"))
			sb.WriteString(typ.RowText(deps, tr, &diff.Updates[i]))
			sb.WriteByte('\n')
		}
		for i := range diff.Inserts {
			sb.WriteString(tr.T("tracking.new"))
			sb.WriteString(typ.RowText(deps, tr, &diff.Inserts[i]))
			sb.WriteByte('\n')
		}
		message = sb.String()
	}
	if message != "" {
		v2SendConfirmation(deps, human, message, language)
	}
}

// v2PushRemoved builds and sends the removal confirmation push for deleted rows.
func v2PushRemoved[Req any, T any](deps *TrackingDeps, typ v2TrackingType[Req, T], human *store.HumanLite, rows []T, silent bool) {
	if silent || len(rows) == 0 {
		return
	}
	tr := translatorFor(deps, human)
	language := resolveLanguage(deps, human)
	var sb strings.Builder
	for i := range rows {
		sb.WriteString(tr.T("tracking.removed_prefix"))
		sb.WriteString(typ.RowText(deps, tr, &rows[i]))
		sb.WriteByte('\n')
	}
	if sb.Len() > 0 {
		v2SendConfirmation(deps, human, sb.String(), language)
	}
}
