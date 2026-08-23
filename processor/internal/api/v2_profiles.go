package api

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2_profiles.go is the strict /api/v2 profiles surface (a sub-resource of
// human). Each op is a thin wrapper around the exact business logic, validation,
// and reload behaviour of the frozen v1 handlers in profiles.go / humans.go —
// reusing store.HumanStore (GetProfiles / SwitchProfile / AddProfile /
// UpdateProfileHours / DeleteProfile / CopyProfile) and the profilesToResponse
// DTO. The differences are strict typed huma inputs (additionalProperties:false,
// required enforcement, unknown query rejection), the STRICT typed active_hours
// schema (v2ActiveHourEntry), and problem+json errors.
//
// Success-shape convention:
//   - GET endpoints return the resource ({profiles:[...]}).
//   - mutation POST/PATCH/DELETE return {status:"ok"}.

// --- strict typed active_hours schema (v2-design §2b) -----------------------

// v2ActiveHourEntry is the STRICT v2 representation of a single active-hours
// entry — distinct from the lenient db.ActiveHourEntry (which coerces strings,
// zero-pads, etc.) used by the in-place v1/P2d endpoints. It enforces hard
// numeric bounds via huma minimum/maximum tags (NO string coercion) and a
// cross-field rule validated in code (see validateV2ActiveHours).
//
// The JSON field names here are deliberately identical to db.ActiveHourEntry's
// json tags (day / hours / mins / step / end_hours / end_mins), so a validated
// []v2ActiveHourEntry marshals directly into a string that db.ParseActiveHours
// round-trips through the scheduler without any field remapping.
//
// This type is reusable: a future v2 summaries-schedule endpoint shares the same
// shape and the same validateV2ActiveHours guard.
type v2ActiveHourEntry struct {
	Day      int  `json:"day" minimum:"0" maximum:"6" doc:"Day of week (0=Sunday … 6=Saturday)"`
	Hours    int  `json:"hours" minimum:"0" maximum:"23" doc:"Start hour (0–23)"`
	Mins     int  `json:"mins" minimum:"0" maximum:"59" doc:"Start minute (0–59)"`
	Step     int  `json:"step,omitempty" minimum:"0" doc:"Step in hours; >0 makes this a range entry, 0/absent = single-fire"`
	EndHours *int `json:"end_hours,omitempty" minimum:"0" maximum:"23" doc:"End hour (0–23); required when step>0"`
	EndMins  *int `json:"end_mins,omitempty" minimum:"0" maximum:"59" doc:"End minute (0–59); required when step>0"`
}

// validateV2ActiveHours runs the cross-field rules huma's per-field
// minimum/maximum tags cannot express:
//
//   - step>0  ⇒ end_hours AND end_mins must both be present, and the end time
//     must be >= the start time (NO cross-midnight).
//   - step==0/absent ⇒ single-fire; end_* are ignored.
//
// Returns an huma 422 problem+json on any violation so callers can return it
// directly.
func validateV2ActiveHours(entries []v2ActiveHourEntry) error {
	for _, e := range entries {
		if e.Step > 0 {
			if e.EndHours == nil || e.EndMins == nil {
				return huma.Error422UnprocessableEntity(
					"active_hours entry with step>0 requires end_hours and end_mins")
			}
			startMin := e.Hours*60 + e.Mins
			endMin := *e.EndHours*60 + *e.EndMins
			if endMin < startMin {
				return huma.Error422UnprocessableEntity(
					"active_hours range end must be >= start (cross-midnight not allowed)")
			}
		}
	}
	return nil
}

// marshalV2ActiveHours validates and serialises a []v2ActiveHourEntry into the
// JSON string the store persists. An empty / nil slice serialises to "[]",
// which db.ParseActiveHours treats as "no schedule" (clears it).
//
// The marshalled bytes use db.ActiveHourEntry-compatible field names (the json
// tags on v2ActiveHourEntry match exactly), so the stored string round-trips
// through db.ParseActiveHours and the scheduler. For range entries the EndHours
// /EndMins pointers are non-nil (guaranteed by validateV2ActiveHours), so they
// always serialise; single-fire entries omit them.
func marshalV2ActiveHours(entries []v2ActiveHourEntry) (string, error) {
	if err := validateV2ActiveHours(entries); err != nil {
		return "", err
	}
	if entries == nil {
		entries = []v2ActiveHourEntry{}
	}
	b, err := json.Marshal(entries)
	if err != nil {
		// Should be unreachable for this shape; surface as 500.
		return "", huma.Error500InternalServerError("failed to encode active_hours")
	}
	return string(b), nil
}

// --- list -------------------------------------------------------------------

// v2ProfileResponse is the STRICT v2 representation of a single profile. Unlike
// the legacy ProfileResponse (which returns active_hours as the raw JSON STRING
// stored in the DB), the v2 shape exposes active_hours as a proper typed JSON
// ARRAY of v2ActiveHourEntry — symmetric with the strict v2 active_hours INPUT
// on PATCH/add. An absent / empty schedule serialises to [] (never a string,
// never null).
//
// area is kept as the JSON-encoded string the store persists, matching the
// existing v2 wire shape; only active_hours changes from string → array here.
type v2ProfileResponse struct {
	UID         int                 `json:"uid" doc:"Database row id of the profile."`
	ID          string              `json:"id" doc:"Owning human id."`
	ProfileNo   int                 `json:"profile_no" doc:"Profile number within the human (1 = default profile); referenced by tracking rules and profile switch."`
	Name        string              `json:"name" doc:"Profile display name."`
	Area        string              `json:"area" doc:"Profile geofence-area override as a JSON-encoded string array (legacy encoding retained on this field) — \"[]\" when the profile has no area override."`
	Latitude    float64             `json:"latitude" doc:"Profile location-override latitude. 0 when the profile has no location override."`
	Longitude   float64             `json:"longitude" doc:"Profile location-override longitude. 0 when the profile has no location override."`
	ActiveHours []v2ActiveHourEntry `json:"active_hours" doc:"Auto-switch schedule as a typed array. Always [] when unscheduled (never null, never a string)."`
}

// v2ProfilesOutput is the GET list response: {profiles:[...]} with active_hours
// as a typed array.
type v2ProfilesOutput struct {
	Body struct {
		Profiles []v2ProfileResponse `json:"profiles"`
	}
}

// profileToV2Response adapts a store.Profile into the strict v2 wire shape,
// parsing the stored active_hours JSON string into a typed []v2ActiveHourEntry.
// A missing / unparseable schedule yields an empty (non-nil) slice so the field
// always marshals to [] rather than null.
func profileToV2Response(p store.Profile) v2ProfileResponse {
	return v2ProfileResponse{
		UID:         p.UID,
		ID:          p.ID,
		ProfileNo:   p.ProfileNo,
		Name:        p.Name,
		Area:        stringSliceToJSON(p.Area),
		Latitude:    p.Latitude,
		Longitude:   p.Longitude,
		ActiveHours: parseV2ActiveHours(p.ActiveHours),
	}
}

// parseV2ActiveHours decodes the stored active_hours JSON string into typed v2
// entries. Empty / placeholder / malformed schedules return an empty slice (not
// nil) so the JSON field is always [] rather than null. A parse error is logged
// and treated as "no schedule".
func parseV2ActiveHours(raw string) []v2ActiveHourEntry {
	out := []v2ActiveHourEntry{}
	entries, err := db.ParseActiveHours(raw)
	if err != nil {
		log.Warnf("v2 profiles: active_hours parse %q: %s", raw, err)
		return out
	}
	for _, e := range entries {
		v := v2ActiveHourEntry{
			Day:   e.Day,
			Hours: e.Hours,
			Mins:  e.Mins,
			Step:  e.Step,
		}
		// end_hours/end_mins are only meaningful for range entries (step>0);
		// surface them via pointers so single-fire entries omit them, mirroring
		// the strict v2 input shape.
		if e.Step > 0 {
			eh, em := e.EndHours, e.EndMins
			v.EndHours = &eh
			v.EndMins = &em
		}
		out = append(out, v)
	}
	return out
}

func registerV2ProfilesList(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-list-human-profiles", Method: "GET", Path: "/v2/humans/{id}/profiles",
		Summary: "List a human's profiles",
		Description: "Returns every profile for the human (profile_no, name, area, location, active_hours). active_hours is a typed JSON " +
			"array of schedule entries ([] when no schedule is set). 404 if the human does not exist.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*v2ProfilesOutput, error) {
		if _, err := resolveFullHuman(deps, in.ID); err != nil {
			return nil, err
		}
		profiles, err := deps.Humans.GetProfiles(in.ID)
		if err != nil {
			log.Errorf("v2 profiles: list %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		out := &v2ProfilesOutput{}
		out.Body.Profiles = make([]v2ProfileResponse, len(profiles))
		for i, p := range profiles {
			out.Body.Profiles[i] = profileToV2Response(p)
		}
		return out, nil
	})
}

// --- switch active profile --------------------------------------------------

type v2SwitchProfileBody struct {
	ProfileNo int `json:"profile_no" required:"true" doc:"Profile number to make active"`
}

type v2SwitchProfileInput struct {
	ID   string `path:"id" doc:"Human id (the owning user)"`
	Body v2SwitchProfileBody
}

func registerV2ProfileSwitch(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-switch-human-profile", Method: "POST", Path: "/v2/humans/{id}/profile",
		Summary: "Switch a human's active profile",
		Description: "Makes the given profile_no active (also copies its area/location onto the human). " +
			"404 if the human does not exist or the profile_no does not exist. Triggers a state reload.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2SwitchProfileInput) (*statusOKOutput, error) {
		if _, err := resolveFullHuman(deps, in.ID); err != nil {
			return nil, err
		}
		// Mirror v1 HandleSwitchProfile: SwitchProfile returns found=false when
		// the profile does not exist ⇒ 404.
		found, err := deps.Humans.SwitchProfile(in.ID, in.Body.ProfileNo)
		if err != nil {
			log.Errorf("v2 profiles: switch %s/%d: %s", in.ID, in.Body.ProfileNo, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if !found {
			return nil, huma.Error404NotFound("profile not found")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- add --------------------------------------------------------------------

// v2AddProfileBody is the strict create body. v1 accepted a single object or an
// array and a free-form active_hours (string or object); v2 takes exactly one
// {name, active_hours?:[]v2ActiveHourEntry}. active_hours absent / [] = no
// schedule.
type v2AddProfileBody struct {
	Name        string              `json:"name" required:"true" doc:"Profile name (required, non-empty)"`
	ActiveHours []v2ActiveHourEntry `json:"active_hours,omitempty" doc:"Optional active-hours schedule (absent or [] = no schedule)"`
}

type v2AddProfileInput struct {
	ID   string `path:"id" doc:"Human id (the owning user)"`
	Body v2AddProfileBody
}

func registerV2ProfileAdd(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-add-human-profile", Method: "POST", Path: "/v2/humans/{id}/profiles",
		Summary: "Create a profile",
		Description: "Creates a new profile (profile_no auto-assigned). active_hours, if provided, is validated against the strict " +
			"schema (numeric bounds + step/end cross-field, no cross-midnight) and stored as JSON. 422 if the name is empty or " +
			"active_hours is invalid; 404 if the human does not exist. Triggers a state reload.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2AddProfileInput) (*statusOKOutput, error) {
		if _, err := resolveFullHuman(deps, in.ID); err != nil {
			return nil, err
		}
		if in.Body.Name == "" {
			return nil, huma.Error422UnprocessableEntity("name is required")
		}
		activeHours, err := marshalV2ActiveHours(in.Body.ActiveHours)
		if err != nil {
			return nil, err
		}
		if err := deps.Humans.AddProfile(in.ID, in.Body.Name, activeHours); err != nil {
			log.Errorf("v2 profiles: add %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- update active_hours ----------------------------------------------------

type v2UpdateProfileBody struct {
	ActiveHours []v2ActiveHourEntry `json:"active_hours" doc:"New active-hours schedule ([] clears it). Validated against the strict schema."`
}

type v2UpdateProfileInput struct {
	ID        string `path:"id" doc:"Human id (the owning user)"`
	ProfileNo int    `path:"profile_no" doc:"Profile number to update"`
	Body      v2UpdateProfileBody
}

func registerV2ProfileUpdate(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-update-human-profile", Method: "PATCH", Path: "/v2/humans/{id}/profiles/{profile_no}",
		Summary: "Update a profile's active_hours",
		Description: "Replaces the profile's active-hours schedule. The schedule is validated against the strict schema (numeric bounds " +
			"+ step/end cross-field, no cross-midnight); an empty [] clears the schedule. 422 on an invalid schedule; 404 if the " +
			"human does not exist. Triggers a state reload.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2UpdateProfileInput) (*statusOKOutput, error) {
		if _, err := resolveFullHuman(deps, in.ID); err != nil {
			return nil, err
		}
		activeHours, err := marshalV2ActiveHours(in.Body.ActiveHours)
		if err != nil {
			return nil, err
		}
		if err := deps.Humans.UpdateProfileHours(in.ID, in.ProfileNo, activeHours); err != nil {
			log.Errorf("v2 profiles: update %s/%d: %s", in.ID, in.ProfileNo, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- delete -----------------------------------------------------------------

type v2DeleteProfileInput struct {
	ID        string `path:"id" doc:"Human id (the owning user)"`
	ProfileNo int    `path:"profile_no" doc:"Profile number to delete"`
}

func registerV2ProfileDelete(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-delete-human-profile", Method: "DELETE", Path: "/v2/humans/{id}/profiles/{profile_no}",
		Summary: "Delete a profile",
		Description: "Deletes the profile and its tracking data. The store reassigns the human's active profile when the deleted one " +
			"was active (mirrors v1). 404 if the human does not exist. Triggers a state reload.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2DeleteProfileInput) (*statusOKOutput, error) {
		if _, err := resolveFullHuman(deps, in.ID); err != nil {
			return nil, err
		}
		if err := deps.Humans.DeleteProfile(in.ID, in.ProfileNo); err != nil {
			log.Errorf("v2 profiles: delete %s/%d: %s", in.ID, in.ProfileNo, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- copy -------------------------------------------------------------------

// v2CopyProfileBody carries the source profile number; the destination is the
// {profile_no} path segment.
type v2CopyProfileBody struct {
	FromProfile int `json:"from_profile" required:"true" doc:"Source profile number to copy tracking rules FROM"`
}

type v2CopyProfileInput struct {
	ID        string `path:"id" doc:"Human id (the owning user)"`
	ProfileNo int    `path:"profile_no" doc:"Destination profile number to copy tracking rules INTO"`
	Body      v2CopyProfileBody
}

func registerV2ProfileCopy(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-copy-human-profile", Method: "POST", Path: "/v2/humans/{id}/profiles/{profile_no}/copy",
		Summary: "Copy tracking rules between profiles",
		Description: "Copies all tracking rules from from_profile (body) into the {profile_no} destination, replacing the destination's " +
			"existing rules (mirrors v1 CopyProfile). 404 if the human does not exist. Triggers a state reload.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2CopyProfileInput) (*statusOKOutput, error) {
		if _, err := resolveFullHuman(deps, in.ID); err != nil {
			return nil, err
		}
		if err := deps.Humans.CopyProfile(in.ID, in.Body.FromProfile, in.ProfileNo); err != nil {
			log.Errorf("v2 profiles: copy %s %d->%d: %s", in.ID, in.Body.FromProfile, in.ProfileNo, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// RegisterV2Profiles registers the strict v2 profile endpoints (a sub-resource
// of human): list, switch-active, add, update-active-hours, delete, copy.
func RegisterV2Profiles(humaAPI huma.API, deps *TrackingDeps) {
	tag := []string{"v2-profiles"}
	sec := []map[string][]string{{"poracleSecret": {}}}

	registerV2ProfilesList(humaAPI, deps, tag, sec)
	registerV2ProfileSwitch(humaAPI, deps, tag, sec)
	registerV2ProfileAdd(humaAPI, deps, tag, sec)
	registerV2ProfileUpdate(humaAPI, deps, tag, sec)
	registerV2ProfileDelete(humaAPI, deps, tag, sec)
	registerV2ProfileCopy(humaAPI, deps, tag, sec)
}
