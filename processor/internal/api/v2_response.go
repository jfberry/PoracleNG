package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// v2_response.go holds the generic response output types for v2 tracking and the
// flattening marshaller that merges a strict Req rule object with its uid (and
// optional description) into one flat JSON object.

// MarshalJSON flattens the embedded strict Req rule fields and the uid (plus
// description when set) into a single object, so the wire shape is e.g.
// {"uid":7,"pokemon_id":149,"min_iv":95,...,"description":"..."} rather than a
// nested {"rule":{...}}. uid and description always win over any same-named Req
// field (Req types don't carry uid/description, so there is no real conflict).
//
// Wildcard projection (Part B of #138): a filter sitting at its wildcard/default
// is set to nil by the type's ToRule. The Req struct keeps `omitempty` so the
// REQUEST schema stays strict (those fields remain optional, not required), which
// means a nil pointer would normally be DROPPED from the marshalled rule. The
// response contract is instead "present-but-null" — so after flattening we add an
// explicit `null` for every optional Req field that omitempty dropped. This keeps
// request strictness (omittable) and the desired response shape (null) on the
// single shared struct.
func (e v2RuleEnvelope[Req]) MarshalJSON() ([]byte, error) {
	ruleBytes, err := json.Marshal(e.Rule)
	if err != nil {
		return nil, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(ruleBytes, &merged); err != nil {
		return nil, fmt.Errorf("v2 rule envelope: rule must marshal to an object: %w", err)
	}
	// Re-introduce wildcard/default fields that omitempty dropped, as explicit
	// null, so the response shows every filter (meaningful value or null).
	addNullForDroppedFields(reflect.TypeOf(e.Rule), merged)

	uidBytes, _ := json.Marshal(e.UID)
	merged["uid"] = uidBytes
	if e.Description != "" {
		descBytes, _ := json.Marshal(e.Description)
		merged["description"] = descBytes
	}
	if e.ProfileNo != nil {
		pnBytes, _ := json.Marshal(*e.ProfileNo)
		merged["profile_no"] = pnBytes
	}
	return json.Marshal(merged)
}

// jsonNull is the literal emitted for a wildcard/default field hidden by Part B.
var jsonNull = json.RawMessage("null")

// addNullForDroppedFields walks the Req struct's JSON-tagged fields and inserts an
// explicit null into merged for any wildcard-hidden field the omitempty marshal
// dropped. Only fields the struct marks as response-nullable participate:
//
//   - pointer fields tagged nullable:"true" (the wildcard-hidden optional filters)
//   - slice/map fields (huma already models these as nullable by default)
//
// Mode-defining fields that are mutually exclusive (e.g. invasion type_id /
// grunt_id / everything / boss) deliberately carry NO nullable tag: only the
// ACTIVE mode is emitted and the inactive ones stay absent (dropped, not null).
// Fields already present (a meaningful, non-default value) are left untouched.
func addNullForDroppedFields(t reflect.Type, merged map[string]json.RawMessage) {
	if t == nil || t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if _, ok := merged[name]; ok {
			continue // present with a meaningful value
		}
		switch f.Type.Kind() {
		case reflect.Pointer:
			// A pointer becomes null only when explicitly marked nullable. This
			// excludes the inactive invasion/incident mode fields (no nullable
			// tag), which must stay absent.
			if f.Tag.Get("nullable") == "true" {
				merged[name] = jsonNull
			}
		case reflect.Slice, reflect.Map:
			// Slices/maps are nullable by default in the response model.
			merged[name] = jsonNull
		}
	}
}

// Schema implements huma.SchemaProvider so the generated OpenAPI document
// reflects the FLATTENED wire shape (Req fields + uid + description), matching
// what MarshalJSON actually emits. We build a fresh object schema that copies
// Req's properties and adds uid/description — we MUST NOT mutate the cached Req
// schema returned by the registry (mutating a shared *Schema contaminates every
// other use of Req; see the huma-gotchas reference). allowRef=false gives the
// inline object schema whose Properties map we copy.
func (v2RuleEnvelope[Req]) Schema(r huma.Registry) *huma.Schema {
	var zero Req
	reqSchema := r.Schema(reflect.TypeOf(zero), false, "")

	props := make(map[string]*huma.Schema, len(reqSchema.Properties)+2)
	maps.Copy(props, reqSchema.Properties)
	props["uid"] = &huma.Schema{Type: huma.TypeInteger, Format: "int64", Description: "Tracking rule uid"}
	props["description"] = &huma.Schema{Type: huma.TypeString, Description: "Human-readable rule description (present only with ?include_descriptions=true)"}

	required := append([]string(nil), reqSchema.Required...)
	required = append(required, "uid")

	s := &huma.Schema{
		Type:                 huma.TypeObject,
		Properties:           props,
		Required:             required,
		AdditionalProperties: false,
	}
	s.PrecomputeMessages()
	return s
}

// v2ListOutput is the {rules:[...]} response (list + get-by-uid).
type v2ListOutput[Req any] struct {
	Body struct {
		Rules []v2RuleEnvelope[Req] `json:"rules"`
	}
}

// v2CreateOutput is the {created, updated, unchanged} response (POST + PUT).
type v2CreateOutput[Req any] struct {
	Body struct {
		Created   []v2RuleEnvelope[Req] `json:"created"`
		Updated   []v2RuleEnvelope[Req] `json:"updated"`
		Unchanged []v2RuleEnvelope[Req] `json:"unchanged"`
	}
}

// v2DeleteOutput is the {deleted:[...]} response (single + bulk delete).
type v2DeleteOutput[Req any] struct {
	Body struct {
		Deleted []v2RuleEnvelope[Req] `json:"deleted"`
	}
}
