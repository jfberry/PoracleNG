package api

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2_patch.go serves PATCH /api/v2/humans/{id}/tracking/{type}: set one or more
// delivery settings on a list of the human's rules, with an outcome per rule.
// Each rule goes through the PUT path: the stored row is read as its v2 rule,
// the named settings are applied, and the result is translated, validated,
// conflict-checked and written back under the same uid (v2ReplaceRow).

// v2PatchSet holds the settings a PATCH may set. Tags and bounds mirror the
// rule fields of the same names. A nil field is left as stored.
type v2PatchSet struct {
	Distance *int    `json:"distance,omitempty" minimum:"0" maximum:"40000000" doc:"Radius in metres around the anchor location; 0 matches by geofence areas instead. Clamped to the server's [tracking] max_distance unless the human is an admin or not a user (channel, webhook, group, thread), or the request is trusted."`
	Clean    *bool   `json:"clean,omitempty" doc:"Auto-delete the alert on expiry (clean bitmask bit 1)."`
	Edit     *bool   `json:"edit,omitempty" doc:"Edit the alert in place on updates (clean bitmask bit 2). 422 for types that do not support edit (gym, invasion, maxbattle, nest, quest)."`
	Summary  *bool   `json:"summary,omitempty" doc:"Route into the summary digest (clean bitmask bit 4). Quest only; 422 for other types."`
	Template *string `json:"template,omitempty" doc:"DTS template name; empty uses the server's default template."`
	Ping     *string `json:"ping,omitempty" maxLength:"255" doc:"Text appended to the alert, typically Discord role/user mentions; empty for none."`
}

// v2PatchBody is the PATCH request body.
type v2PatchBody struct {
	UIDs []int64    `json:"uids" minItems:"1" doc:"Rule uids to update, processed in this order (it decides which rule wins a conflict). A repeated uid is processed once, at its first position."`
	Set  v2PatchSet `json:"set" doc:"Settings to apply to every listed rule; at least one is required. Fields not named are left as stored."`
}

// v2PatchInput is the PATCH input. Params are inline for the same reason as
// v2CreateInput.
type v2PatchInput struct {
	ID                  string `path:"id" doc:"Human id (the owning user); ownership scope for every rule"`
	Profile             int    `query:"profile" default:"-1" doc:"Profile number; defaults to the human's active profile"`
	IncludeDescriptions bool   `query:"include_descriptions" doc:"Add a human-readable description to each rule in the response"`
	Silent              bool   `query:"silent" doc:"Apply without sending the confirmation push"`
	Trusted             bool   `query:"trusted" doc:"Allow override_areas to name fences that are not userSelectable, for a client that has already authorised the change. Unknown fence names are still rejected; area_security community restrictions still apply. Also lifts [tracking] max_distance and max_areas."`
	DryRun              bool   `query:"dry_run" doc:"Validate and classify without writing: the same per-rule outcomes, with dry_run:true. Nothing is stored, reloaded or pushed."`
	Body                v2PatchBody
}

// Refusal reasons.
const (
	v2PatchInvalid  = "invalid"
	v2PatchConflict = "conflict"
	v2PatchNotFound = "not_found"
)

// v2PatchRefusal is a rule the PATCH left alone, and why.
type v2PatchRefusal struct {
	UID           int64  `json:"uid" doc:"The requested rule uid"`
	Reason        string `json:"reason" enum:"invalid,conflict,not_found" doc:"invalid: the patched rule fails validation; conflict: it would duplicate, or be a one-field update of, another rule; not_found: not this human's rule of this type in this profile. Switch on this, not on detail."`
	Detail        string `json:"detail" doc:"Human-readable explanation"`
	ConflictsWith int64  `json:"conflicts_with,omitempty" doc:"For conflict: the uid of the rule it would collide with"`
}

// v2PatchOutput is the PATCH response. Every listed uid appears in exactly one
// of updated, unchanged or refused.
type v2PatchOutput[Req any] struct {
	Body struct {
		Updated   []v2UpdatedRuleEnvelope[Req] `json:"updated" doc:"Rules changed, with the stored rule each replaced as previous"`
		Unchanged []v2RuleEnvelope[Req]        `json:"unchanged" doc:"Rules that already had the requested settings"`
		Refused   []v2PatchRefusal             `json:"refused" doc:"Rules left alone, with the reason"`
		DryRun    bool                         `json:"dry_run,omitempty" doc:"True when the request was a dry run: nothing was written"`
	}
}

// v2EditUnsupported lists the types whose Translate refuses edit:true (via
// rejectEdit) because their alerts never carry an edit key. PATCH rejects edit
// for them up front; TestV2Patch_EditUnsupportedMatchesTranslate keeps this in
// step with the Translate functions.
var v2EditUnsupported = map[string]bool{
	"gym": true, "invasion": true, "maxbattle": true, "nest": true, "quest": true,
}

// v2SummaryTypes lists the types with a summary digest. Every rule struct has
// a summary field, but only quest summaries are dispatched; setting it in bulk
// elsewhere would store a bit nothing reads.
var v2SummaryTypes = map[string]bool{"quest": true}

// applyV2PatchSet copies every non-nil field of set onto the field of *req with
// the same JSON name. It returns an error naming the first set field *req has
// no matching field for (e.g. clean on fort).
func applyV2PatchSet(req any, set v2PatchSet) error {
	rv := reflect.ValueOf(req).Elem()
	fields := make(map[string]reflect.Value, rv.NumField())
	for i := 0; i < rv.NumField(); i++ {
		fields[v2JSONName(rv.Type().Field(i))] = rv.Field(i)
	}
	sv := reflect.ValueOf(set)
	for i := 0; i < sv.NumField(); i++ {
		val := sv.Field(i)
		if val.IsNil() {
			continue
		}
		name := v2JSONName(sv.Type().Field(i))
		dst, ok := fields[name]
		if !ok || dst.Type() != val.Type() {
			return fmt.Errorf("%s cannot be set on this tracking type", name)
		}
		cp := reflect.New(val.Type().Elem())
		cp.Elem().Set(val.Elem())
		dst.Set(cp)
	}
	return nil
}

// v2JSONName is a struct field's JSON property name ("" when not serialised).
func v2JSONName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	if name == "-" {
		return ""
	}
	return name
}

// v2ValidatePatchSet is the whole-request check, made before any rule is read:
// at least one setting, and only settings this type supports.
func v2ValidatePatchSet[Req any](typeName string, set v2PatchSet) error {
	if set == (v2PatchSet{}) {
		return huma.Error422UnprocessableEntity("set must name at least one setting")
	}
	var probe Req
	if err := applyV2PatchSet(&probe, set); err != nil {
		return huma.Error422UnprocessableEntity(err.Error(), &huma.ErrorDetail{Location: "body.set", Message: err.Error()})
	}
	if set.Summary != nil && !v2SummaryTypes[typeName] {
		msg := "summary is not supported for " + typeName + " tracking"
		return huma.Error422UnprocessableEntity(msg, &huma.ErrorDetail{Location: "body.set.summary", Message: msg})
	}
	if v2EditUnsupported[typeName] {
		if err := rejectEdit(typeName, set.Edit); err != nil {
			return err
		}
	}
	return nil
}

// v2ErrDetail extracts the human-readable message from a Translate error.
func v2ErrDetail(err error) string {
	var em *huma.ErrorModel
	if errors.As(err, &em) && em.Detail != "" {
		return em.Detail
	}
	return err.Error()
}

// uniqueUIDs drops repeated uids, keeping each at its first position.
func uniqueUIDs(uids []int64) []int64 {
	seen := make(map[int64]bool, len(uids))
	out := make([]int64, 0, len(uids))
	for _, u := range uids {
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// v2PatchCandidate is a requested rule whose patch changes it, awaiting the
// conflict check.
type v2PatchCandidate[T any] struct {
	pos     int // index in the unique uid list
	uid     int64
	stored  T // as stored (previous)
	patched T // translated replacement
}

func v2HandlePatch[Req any, T any](deps *TrackingDeps, typ v2TrackingType[Req, T], in *v2PatchInput) (*v2PatchOutput[Req], error) {
	human, profileNo, err := resolveHuman(deps, in.ID, in.Profile)
	if err != nil {
		return nil, err
	}
	if err := v2ValidatePatchSet[Req](typ.Name, in.Body.Set); err != nil {
		return nil, err
	}
	oc, ocMsg, ocCode := newOverrideContext(deps, human.ID, in.Trusted)
	if ocMsg != "" {
		return nil, humaErr(ocCode, ocMsg)
	}

	// One read: the type's rows for this human+profile, as stored and in the
	// canonical form the diff compares.
	stored, err := typ.scopedRows(deps, human.ID, profileNo)
	if err != nil {
		log.Errorf("v2 tracking %s patch: %s", typ.Name, err)
		return nil, huma.Error500InternalServerError("database error")
	}
	canon := make([]T, len(stored))
	copy(canon, stored)
	index := make(map[int64]int, len(stored))
	for i := range canon {
		if typ.Canonicalize != nil {
			typ.Canonicalize(&canon[i])
		}
		index[typ.GetUID(&canon[i])] = i
	}

	uids := uniqueUIDs(in.Body.UIDs)
	refused := make(map[int]v2PatchRefusal)
	unchanged := make(map[int]T)
	var candidates []v2PatchCandidate[T]

	// Pass 1: read, patch, translate; decide not_found / invalid / unchanged.
	for pos, uid := range uids {
		i, ok := index[uid]
		if !ok {
			refused[pos] = v2PatchRefusal{UID: uid, Reason: v2PatchNotFound,
				Detail: fmt.Sprintf("%s rule %d not found for this human", typ.Name, uid)}
			continue
		}
		req := typ.ToRule(&stored[i])
		if err := applyV2PatchSet(&req, in.Body.Set); err != nil {
			// Unreachable: v2ValidatePatchSet applied the same set to a Req.
			refused[pos] = v2PatchRefusal{UID: uid, Reason: v2PatchInvalid, Detail: err.Error()}
			continue
		}
		row, terr := typ.Translate(deps, human.ID, profileNo, oc, &req)
		if terr != nil {
			refused[pos] = v2PatchRefusal{UID: uid, Reason: v2PatchInvalid, Detail: v2ErrDetail(terr)}
			continue
		}
		if _, isDup, _, _ := DiffTracking(&canon[i], &row); isDup {
			unchanged[pos] = stored[i]
			continue
		}
		typ.SetUID(&row, uid)
		candidates = append(candidates, v2PatchCandidate[T]{pos: pos, uid: uid, stored: stored[i], patched: row})
	}

	// Pass 2: the squash check, in uids order, against the rules as this PATCH
	// leaves them. A collision between two changed rules is reported on the
	// later one, so the earlier one wins: while a candidate is checked, the
	// candidates after it are left out of the comparison set; each is checked
	// in its turn against everything before it.
	pending := make(map[int64]bool, len(candidates))
	for _, c := range candidates {
		pending[c.uid] = true
	}
	working := make([]T, len(canon))
	copy(working, canon)
	policy := store.RowDiffPolicy[T]{GetUID: typ.GetUID, SetUID: typ.SetUID}
	var updated []v2PatchCandidate[T]
	for _, c := range candidates {
		delete(pending, c.uid)
		compare := make([]T, 0, len(working))
		for j := range working {
			if !pending[typ.GetUID(&working[j])] {
				compare = append(compare, working[j])
			}
		}
		if other, conflict := policy.Conflicts(compare, &c.patched, c.uid); conflict {
			refused[c.pos] = v2PatchRefusal{UID: c.uid, Reason: v2PatchConflict,
				Detail: fmt.Sprintf("would duplicate rule %d", other), ConflictsWith: other}
			continue
		}
		working[index[c.uid]] = c.patched
		updated = append(updated, c)
	}

	if !in.DryRun && len(updated) > 0 {
		rows := make([]T, 0, len(updated))
		for k := range updated {
			if err := v2ReplaceRow(deps, typ, human.ID, updated[k].uid, &updated[k].patched); err != nil {
				// Rules already written stay written; the PATCH is idempotent,
				// so the client can repeat it.
				log.Errorf("v2 tracking %s patch: %s", typ.Name, err)
				reloadState(deps)
				return nil, huma.Error500InternalServerError("database error")
			}
			rows = append(rows, updated[k].patched)
		}
		reloadState(deps)
		v2PushDiff(deps, typ, human, store.DiffResult[T]{Updates: rows}, in.Silent)
	}

	return v2PatchResponse(deps, typ, human, in, uids, updated, unchanged, refused), nil
}

// v2PatchResponse lays the outcomes out in uids order within each list.
func v2PatchResponse[Req any, T any](deps *TrackingDeps, typ v2TrackingType[Req, T], human *store.HumanLite, in *v2PatchInput,
	uids []int64, updated []v2PatchCandidate[T], unchanged map[int]T, refused map[int]v2PatchRefusal) *v2PatchOutput[Req] {
	out := &v2PatchOutput[Req]{}
	out.Body.DryRun = in.DryRun
	out.Body.Updated = []v2UpdatedRuleEnvelope[Req]{}
	out.Body.Refused = []v2PatchRefusal{}

	for _, c := range updated {
		env := buildRuleEnvelopes(deps, typ, human, []T{c.patched}, in.IncludeDescriptions)[0]
		prev := buildRuleEnvelopes(deps, typ, human, []T{c.stored}, in.IncludeDescriptions)[0]
		out.Body.Updated = append(out.Body.Updated, v2UpdatedRuleEnvelope[Req]{v2RuleEnvelope: env, Previous: &prev})
	}
	var same []T
	for pos := range uids {
		if row, ok := unchanged[pos]; ok {
			same = append(same, row)
		}
		if r, ok := refused[pos]; ok {
			out.Body.Refused = append(out.Body.Refused, r)
		}
	}
	out.Body.Unchanged = buildRuleEnvelopes(deps, typ, human, same, in.IncludeDescriptions)
	return out
}
