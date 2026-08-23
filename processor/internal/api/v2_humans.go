package api

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/community"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2_humans.go is the strict /api/v2 humans surface. Each op is a thin wrapper
// around the exact business logic, validation, and reload behaviour of the
// frozen v1 handlers in humans.go — reusing store.HumanStore, the humanToResponse
// DTO, community.FilterAreas, and reloadState. The only differences are strict
// typed huma inputs (additionalProperties:false, required enforcement, unknown
// query rejection) and problem+json errors.
//
// Success-shape convention:
//   - GET endpoints return the resource (human / areas / check-location result).
//   - mutation POSTs return {status:"ok"} (simple, consistent across the batch).
//
// Roles, locations CRUD, and profiles are SEPARATE later batches.

// --- shared input/output --------------------------------------------------

// v2HumanIDInput is the input for ops addressed only by {id} (no body, no query).
type v2HumanIDInput struct {
	ID string `path:"id" doc:"Human id (the owning user)"`
}

// Pure-action mutations on this surface return the shared statusOKOutput
// ({"status":"ok"}); see okStatus in huma_system.go.

// v2HumanResource is the GET /v2/humans/{id} response: the legacy human resource
// (humanToResponse). blocked_alerts is a read-only derived field on this
// resource — it is never settable through any v2 humans endpoint.
type v2HumanResource struct {
	Body struct {
		Human *HumanResponse `json:"human"`
	}
}

// resolveFullHuman looks up the full human record (parsed JSON columns) for the
// id, returning an huma 404 when missing and 500 on a store error.
func resolveFullHuman(deps *TrackingDeps, id string) (*store.Human, error) {
	human, err := deps.Humans.Get(id)
	if err != nil {
		log.Errorf("v2 humans: lookup %s: %s", id, err)
		return nil, huma.Error500InternalServerError("database error")
	}
	if human == nil {
		return nil, huma.Error404NotFound("human not found")
	}
	return human, nil
}

// --- create ----------------------------------------------------------------

// v2CreateHumanBody is the strict body for POST /v2/humans. Mirrors v1's
// createHumanRequest fields. community accepts a string or a list of strings;
// v2 keeps it strict as []string (the cleaner of v1's two accepted shapes).
type v2CreateHumanBody struct {
	ID           string   `json:"id" required:"true" doc:"Platform user/channel/webhook id (required)"`
	Name         string   `json:"name" required:"true" doc:"Display name (required)"`
	Type         string   `json:"type,omitempty" doc:"Human type, e.g. discord:user (default discord:user)"`
	Enabled      *bool    `json:"enabled,omitempty" doc:"Initial enabled state (default true)"`
	Area         string   `json:"area,omitempty" doc:"JSON-array string of initial area names (default empty)"`
	Latitude     float64  `json:"latitude,omitempty" doc:"Default latitude (default 0)"`
	Longitude    float64  `json:"longitude,omitempty" doc:"Default longitude (default 0)"`
	AdminDisable *bool    `json:"admin_disable,omitempty" doc:"Set the admin_disable flag (default false)"`
	Language     string   `json:"language,omitempty" doc:"Locale; empty = the configured default locale"`
	Community    []string `json:"community,omitempty" doc:"Community membership names (drives area restriction)"`
	ProfileName  string   `json:"profile_name,omitempty" doc:"Name for the default profile (default \"Default\")"`
	Notes        string   `json:"notes,omitempty" doc:"Freeform admin notes"`
}

type v2CreateHumanInput struct {
	Body v2CreateHumanBody
}

func registerV2HumanCreate(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-create-human", Method: "POST", Path: "/v2/humans",
		Summary:     "Create a human",
		Description: "Creates a new user/channel/webhook with an optional community membership and a default profile. Returns the created human resource.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2CreateHumanInput) (*v2HumanResource, error) {
		b := in.Body
		// id + name required is enforced by huma; mirror v1's empty-string guard
		// for safety (huma required:"true" rejects absent, but not empty string).
		if b.ID == "" || b.Name == "" {
			return nil, huma.Error422UnprocessableEntity("id and name are required")
		}

		existing, err := deps.Humans.Get(b.ID)
		if err != nil {
			log.Errorf("v2 humans: create lookup: %s", err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if existing != nil {
			return nil, huma.Error409Conflict("human already exists")
		}

		human := &store.Human{
			ID:               b.ID,
			Name:             b.Name,
			Type:             b.Type,
			Enabled:          true,
			Area:             parseMembership(b.Area),
			Latitude:         b.Latitude,
			Longitude:        b.Longitude,
			CurrentProfileNo: 1,
			Notes:            b.Notes,
		}
		if human.Type == "" {
			human.Type = "discord:user"
		}
		if b.Enabled != nil && !*b.Enabled {
			human.Enabled = false
		}
		if b.AdminDisable != nil && *b.AdminDisable {
			human.AdminDisable = true
		}

		lang := b.Language
		if lang == "" {
			lang = deps.Config.General.Locale
			if lang == "" {
				lang = "en"
			}
		}
		human.Language = lang

		if len(b.Community) > 0 {
			human.CommunityMembership = b.Community
			st := deps.StateMgr.Get()
			allAreas := lowerFenceNames(st.Fences)
			human.AreaRestriction = community.FilterAreas(
				deps.Config.Area.Communities, b.Community, allAreas)
		}

		if err := deps.Humans.Create(human); err != nil {
			log.Errorf("v2 humans: create: %s", err)
			return nil, huma.Error500InternalServerError("database error")
		}

		profileName := b.ProfileName
		if profileName == "" {
			profileName = "Default"
		}
		if err := deps.Humans.CreateDefaultProfile(b.ID, profileName, human.Area, human.Latitude, human.Longitude); err != nil {
			log.Errorf("v2 humans: create default profile: %s", err)
			return nil, huma.Error500InternalServerError("database error")
		}

		reloadState(deps)
		out := &v2HumanResource{}
		out.Body.Human = humanToResponse(human)
		return out, nil
	})
}

// --- get one ---------------------------------------------------------------

func registerV2HumanGet(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-get-human", Method: "GET", Path: "/v2/humans/{id}",
		Summary:     "Fetch one human",
		Description: "Returns the full human resource. blocked_alerts is a read-only derived field. 404 if the human does not exist.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*v2HumanResource, error) {
		human, err := resolveFullHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}
		out := &v2HumanResource{}
		out.Body.Human = humanToResponse(human)
		return out, nil
	})
}

// --- areas (available) -----------------------------------------------------

// v2AreaInfo mirrors v1's areaInfoResponse element.
type v2AreaInfo struct {
	Name           string `json:"name"`
	Group          string `json:"group"`
	Description    string `json:"description"`
	UserSelectable bool   `json:"userSelectable"`
}

// v2AreasOutput is the {areas:[...]} response for available areas.
type v2AreasOutput struct {
	Body struct {
		Areas []v2AreaInfo `json:"areas"`
	}
}

func registerV2HumanAreasGet(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-get-human-areas", Method: "GET", Path: "/v2/humans/{id}/areas",
		Summary:     "List a human's available areas",
		Description: "Returns the geofence areas available to this human (community-filtered when area_security is enabled). 404 if the human does not exist.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*v2AreasOutput, error) {
		human, err := resolveFullHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}

		st := deps.StateMgr.Get()
		allAreas := lowerFenceNames(st.Fences)

		// Same area_restriction == nil semantics as v1 HandleGetHumanAreas: nil
		// restriction = full access; a set restriction triggers the
		// community-membership filter.
		allowedAreas := allAreas
		if deps.Config.Area.Enabled && !isAdmin(deps, in.ID) && human.AreaRestriction != nil {
			allowedAreas = community.FilterAreas(
				deps.Config.Area.Communities, human.CommunityMembership, allAreas)
		}

		allowedSet := make(map[string]bool, len(allowedAreas))
		for _, a := range allowedAreas {
			allowedSet[strings.ToLower(a)] = true
		}

		out := &v2AreasOutput{}
		out.Body.Areas = []v2AreaInfo{}
		for _, f := range st.Fences {
			if allowedSet[strings.ToLower(f.Name)] {
				out.Body.Areas = append(out.Body.Areas, v2AreaInfo{
					Name:           f.Name,
					Group:          f.Group,
					Description:    f.Description,
					UserSelectable: f.UserSelectable,
				})
			}
		}
		return out, nil
	})
}

// --- enable / disable ------------------------------------------------------

func registerV2HumanEnable(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-enable-human", Method: "POST", Path: "/v2/humans/{id}/enable",
		Summary:     "Enable a human",
		Description: "Sets enabled=true and triggers a state reload. 404 if the human does not exist.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*statusOKOutput, error) {
		if _, err := resolveFullHuman(deps, in.ID); err != nil {
			return nil, err
		}
		if err := deps.Humans.SetEnabled(in.ID, true); err != nil {
			log.Errorf("v2 humans: enable %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

func registerV2HumanDisable(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-disable-human", Method: "POST", Path: "/v2/humans/{id}/disable",
		Summary:     "Disable a human",
		Description: "Sets enabled=false and triggers a state reload. 404 if the human does not exist.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*statusOKOutput, error) {
		if _, err := resolveFullHuman(deps, in.ID); err != nil {
			return nil, err
		}
		if err := deps.Humans.SetEnabled(in.ID, false); err != nil {
			log.Errorf("v2 humans: disable %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- admin-disable ---------------------------------------------------------

type v2AdminDisableBody struct {
	Disabled bool `json:"disabled" required:"true" doc:"true sets admin_disable=1, false clears it"`
}

type v2AdminDisableInput struct {
	ID   string `path:"id" doc:"Human id (the owning user)"`
	Body v2AdminDisableBody
}

func registerV2HumanAdminDisable(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-admin-disable-human", Method: "POST", Path: "/v2/humans/{id}/admin-disable",
		Summary: "Set a human's admin_disable flag",
		Description: "Flag-only toggle of admin_disable (does NOT clear disabled_date or reset enabled/fails, matching v1). " +
			"Triggers a state reload. 404 if the human does not exist.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2AdminDisableInput) (*statusOKOutput, error) {
		if _, err := resolveFullHuman(deps, in.ID); err != nil {
			return nil, err
		}
		// Flag-only semantics: mirror v1 HandleAdminDisabled — update just the
		// admin_disable column via Update, NOT SetAdminDisable (which also
		// clears disabled_date and resets enabled/fails).
		adminDisable := 0
		if in.Body.Disabled {
			adminDisable = 1
		}
		if err := deps.Humans.Update(in.ID, map[string]any{"admin_disable": adminDisable}); err != nil {
			log.Errorf("v2 humans: admin-disable %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- language --------------------------------------------------------------

type v2LanguageBody struct {
	Language string `json:"language" required:"true" doc:"Locale code; validated against the configured available languages when restricted"`
}

type v2LanguageInput struct {
	ID   string `path:"id" doc:"Human id (the owning user)"`
	Body v2LanguageBody
}

func registerV2HumanLanguage(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-set-human-language", Method: "POST", Path: "/v2/humans/{id}/language",
		Summary: "Set a human's language",
		Description: "Validated against the configured available_languages (case-insensitive) when restricted; otherwise any code is accepted. " +
			"Triggers a state reload. 404 if the human does not exist; 422 if the language is empty or unavailable.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2LanguageInput) (*statusOKOutput, error) {
		if _, err := resolveFullHuman(deps, in.ID); err != nil {
			return nil, err
		}

		language := strings.ToLower(strings.TrimSpace(in.Body.Language))
		if language == "" {
			return nil, huma.Error422UnprocessableEntity("language is required")
		}
		// Mirror v1 HandleSetLanguage: when available languages are configured,
		// the requested code must match one (case-insensitive); the stored value
		// preserves the configured casing.
		if deps.Config != nil && len(deps.Config.General.AvailableLanguages) > 0 {
			matched := ""
			for code := range deps.Config.General.AvailableLanguages {
				if strings.ToLower(code) == language {
					matched = code
					break
				}
			}
			if matched == "" {
				return nil, huma.Error422UnprocessableEntity("language is not available")
			}
			language = matched
		}

		if err := deps.Humans.SetLanguage(in.ID, language); err != nil {
			log.Errorf("v2 humans: set language %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- location --------------------------------------------------------------

type v2LocationBody struct {
	Lat float64 `json:"lat" required:"true" doc:"Latitude"`
	Lon float64 `json:"lon" required:"true" doc:"Longitude"`
}

type v2SetLocationInput struct {
	ID      string `path:"id" doc:"Human id (the owning user)"`
	Profile int    `query:"profile" default:"-1" doc:"Profile number; defaults to the human's active profile"`
	Body    v2LocationBody
}

func registerV2HumanSetLocation(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-set-human-location", Method: "POST", Path: "/v2/humans/{id}/location",
		Summary: "Set a human's location",
		Description: "lat/lon are taken from the BODY (v1 took them from the path). When area_security is enabled and the human has an " +
			"area restriction, the location must fall inside an allowed fence (403 otherwise). Triggers a state reload. 404 if the human does not exist.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2SetLocationInput) (*statusOKOutput, error) {
		human, err := resolveFullHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}

		lat := in.Body.Lat
		lon := in.Body.Lon

		// Mirror v1 HandleSetLocation: validate against area restrictions when enabled.
		if deps.Config.Area.Enabled && len(human.AreaRestriction) > 0 {
			st := deps.StateMgr.Get()
			matched := st.Geofence.MatchedAreaNames(lat, lon)
			permitted := false
			for _, fence := range human.AreaRestriction {
				if matched[strings.ToLower(fence)] {
					permitted = true
					break
				}
			}
			if !permitted {
				return nil, huma.Error403Forbidden("location not permitted")
			}
		}

		profileNo := human.CurrentProfileNo
		if in.Profile != profileSentinel {
			profileNo = in.Profile
		}

		if err := deps.Humans.SetLocation(in.ID, profileNo, lat, lon); err != nil {
			log.Errorf("v2 humans: set location %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- check-location --------------------------------------------------------

type v2CheckLocationInput struct {
	ID  string  `path:"id" doc:"Human id (the owning user)"`
	Lat float64 `query:"lat" required:"true" doc:"Latitude to check"`
	Lon float64 `query:"lon" required:"true" doc:"Longitude to check"`
}

// v2CheckLocationOutput mirrors v1's {locationOk:bool} result shape.
type v2CheckLocationOutput struct {
	Body struct {
		LocationOk bool `json:"locationOk" doc:"Whether the location is inside the human's allowed areas"`
	}
}

func registerV2HumanCheckLocation(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-check-human-location", Method: "GET", Path: "/v2/humans/{id}/check-location",
		Summary: "Check whether a location is allowed for a human",
		Description: "Returns {locationOk}. When area_security is disabled, always true (matching v1). Otherwise true only if the " +
			"location falls inside one of the human's area-restriction fences. 404 if the human does not exist.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2CheckLocationInput) (*v2CheckLocationOutput, error) {
		human, err := resolveFullHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}

		out := &v2CheckLocationOutput{}
		// Mirror v1 HandleCheckLocation: area_security disabled ⇒ always ok.
		if !deps.Config.Area.Enabled {
			out.Body.LocationOk = true
			return out, nil
		}

		st := deps.StateMgr.Get()
		matched := st.Geofence.MatchedAreaNames(in.Lat, in.Lon)
		for _, fence := range human.AreaRestriction {
			if matched[strings.ToLower(fence)] {
				out.Body.LocationOk = true
				break
			}
		}
		return out, nil
	})
}

// --- set areas -------------------------------------------------------------

type v2SetAreasBody struct {
	Areas []string `json:"areas" required:"true" doc:"Requested area names; intersected with the human's allowed areas (invalid/duplicate entries dropped)"`
}

type v2SetAreasInput struct {
	ID   string `path:"id" doc:"Human id (the owning user)"`
	Body v2SetAreasBody
}

func registerV2HumanSetAreas(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-set-human-areas", Method: "POST", Path: "/v2/humans/{id}/areas",
		Summary: "Set a human's selected areas",
		Description: "Intersects the requested areas with what the human is allowed to select (userSelectable for non-admins, " +
			"community-filtered when area_security is enabled), dedups, lowercases, and stores the result. Triggers a state reload. " +
			"404 if the human does not exist.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2SetAreasInput) (*statusOKOutput, error) {
		human, err := resolveFullHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}

		// Mirror v1 HandleSetAreas: lowercase requested, build allowed set,
		// intersect+dedup, store.
		requested := make([]string, len(in.Body.Areas))
		for i, a := range in.Body.Areas {
			requested[i] = strings.ToLower(a)
		}

		st := deps.StateMgr.Get()
		admin := isAdmin(deps, in.ID)

		var allowedAreas []string
		for _, f := range st.Fences {
			if !admin && !f.UserSelectable {
				continue
			}
			allowedAreas = append(allowedAreas, strings.ToLower(f.Name))
		}
		if deps.Config.Area.Enabled && !admin && human.AreaRestriction != nil {
			allowedAreas = community.FilterAreas(
				deps.Config.Area.Communities, human.CommunityMembership, allowedAreas)
		}

		allowedSet := make(map[string]bool, len(allowedAreas))
		for _, a := range allowedAreas {
			allowedSet[strings.ToLower(a)] = true
		}

		seen := make(map[string]bool)
		newAreas := []string{}
		for _, a := range requested {
			if allowedSet[a] && !seen[a] {
				seen[a] = true
				newAreas = append(newAreas, a)
			}
		}

		if err := deps.Humans.SetArea(in.ID, human.CurrentProfileNo, newAreas); err != nil {
			log.Errorf("v2 humans: set areas %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- shared helpers --------------------------------------------------------

// lowerFenceNames returns the lowercased names of every fence (the "all areas"
// baseline both the available-areas and set-areas paths start from).
func lowerFenceNames(fences []geofence.Fence) []string {
	out := make([]string, len(fences))
	for i, f := range fences {
		out[i] = strings.ToLower(f.Name)
	}
	return out
}

// RegisterV2Humans registers the strict v2 humans endpoints (core + status +
// location + areas). Roles, locations CRUD, and profiles are later batches.
func RegisterV2Humans(humaAPI huma.API, deps *TrackingDeps) {
	tag := []string{"v2-humans"}
	sec := []map[string][]string{{"poracleSecret": {}}}

	registerV2HumanCreate(humaAPI, deps, tag, sec)
	registerV2HumanGet(humaAPI, deps, tag, sec)
	registerV2HumanAreasGet(humaAPI, deps, tag, sec)
	registerV2HumanEnable(humaAPI, deps, tag, sec)
	registerV2HumanDisable(humaAPI, deps, tag, sec)
	registerV2HumanAdminDisable(humaAPI, deps, tag, sec)
	registerV2HumanLanguage(humaAPI, deps, tag, sec)
	registerV2HumanSetLocation(humaAPI, deps, tag, sec)
	registerV2HumanCheckLocation(humaAPI, deps, tag, sec)
	registerV2HumanSetAreas(humaAPI, deps, tag, sec)
}
