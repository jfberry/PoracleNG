package api

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/community"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2_humans.go is the strict /api/v2 humans surface. Each op is a thin wrapper
// around the exact business logic, validation, and reload behaviour of the
// frozen v1 handlers in humans.go — reusing store.HumanStore,
// community.FilterAreas, and reloadState. The differences are strict typed huma
// inputs (additionalProperties:false, required enforcement, unknown query
// rejection), problem+json errors, and the v2 human shape (v2Human) in place of
// v1's legacy HumanResponse.
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

// v2HumanResource is the GET /v2/humans/{id} (and create) response.
// blocked_alerts is a read-only derived field on this resource — it is never
// settable through any v2 humans endpoint.
type v2HumanResource struct {
	Body struct {
		Human *v2Human `json:"human"`
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
	Area         []string `json:"area,omitempty" doc:"Initial selected area names (default none)"`
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

		// Store each area as its fence's own spelling, deduped by key keeping
		// the first occurrence. (Names are not validated here.)
		var fences []geofence.Fence
		if deps.StateMgr != nil {
			if st := deps.StateMgr.Get(); st != nil {
				fences = st.Fences
			}
		}
		var areas []string
		seenArea := make(map[string]bool, len(b.Area))
		for _, a := range b.Area {
			key := geofence.NormalizeAreaKey(a)
			if seenArea[key] {
				continue
			}
			seenArea[key] = true
			areas = append(areas, geofence.CanonicalAreaName(fences, a))
		}

		human := &store.Human{
			ID:               b.ID,
			Name:             b.Name,
			Type:             b.Type,
			Enabled:          true,
			Area:             areas,
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
		out.Body.Human = humanToV2(human)
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
		out.Body.Human = humanToV2(human)
		return out, nil
	})
}

// --- areas (available) -----------------------------------------------------

// v2AreaInfo mirrors v1's areaInfoResponse element.
type v2AreaInfo struct {
	Name                   string `json:"name"`
	Group                  string `json:"group"`
	Description            string `json:"description"`
	UserSelectable         bool   `json:"userSelectable"`
	ExemptFromAreaSecurity bool   `json:"exemptFromAreaSecurity" doc:"The fence is outside the community system: area_security does not filter it. Does not imply userSelectable, and does not affect strict_locations delivery."`
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
		Description: "Returns the geofence areas available to this human. The listing is what a trusted caller may select: every fence for an unrestricted human, community-filtered otherwise, with exempt fences included under area_security. 404 if the human does not exist.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*v2AreasOutput, error) {
		human, err := resolveFullHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}

		st := deps.StateMgr.Get()
		listed := make(map[string]bool)
		for _, a := range bot.PermittedAreas(st.Fences, deps.Config, human, isAdmin(deps, in.ID), true) {
			listed[a] = true
		}
		out := &v2AreasOutput{}
		out.Body.Areas = []v2AreaInfo{}
		for _, f := range st.Fences {
			if listed[strings.ToLower(f.Name)] {
				out.Body.Areas = append(out.Body.Areas, v2AreaInfo{
					Name:                   f.Name,
					Group:                  f.Group,
					Description:            f.Description,
					UserSelectable:         f.UserSelectable,
					ExemptFromAreaSecurity: f.ExemptFromAreaSecurity,
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
		Description: "Validated against the configured available_languages (case-insensitive) when the operator has restricted them; " +
			"otherwise against the set of languages this server actually has translations for, so unrestricted does not mean " +
			"unvalidated. NOTE: the code is stored lowercased, so a client that sends \"DE\" reads back \"de\" — compare " +
			"case-insensitively. Triggers a state reload. 404 if the human does not exist; 422 if the language is empty or unavailable.",
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
		} else if deps.Translations != nil {
			// Unrestricted: still validate against what the server can actually
			// translate into. Storing a code with no translations produces a
			// user whose alerts silently fall back for every string (#216).
			// An empty bundle means "cannot tell" — accept rather than reject
			// everything.
			if loaded := deps.Translations.Locales(); len(loaded) > 0 {
				known := false
				for _, code := range loaded {
					if strings.EqualFold(code, language) {
						known = true
						break
					}
				}
				if !known {
					return nil, huma.Error422UnprocessableEntity(
						"unknown language: this server has no translations for " + language)
				}
			}
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
	Lat float64 `json:"latitude" required:"true" doc:"Latitude"`
	Lon float64 `json:"longitude" required:"true" doc:"Longitude"`
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
		Description: "latitude/longitude are taken from the BODY (v1 took them from the path). When area_security is enabled and the human has an " +
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

type v2ClearLocationInput struct {
	ID      string `path:"id" doc:"Human id (the owning user)"`
	Profile int    `query:"profile" default:"-1" doc:"Profile number; defaults to the human's active profile"`
}

// registerV2HumanClearLocation registers DELETE /v2/humans/{id}/location.
//
// POST {latitude:0,longitude:0} stores the same thing, but a human with an area
// restriction gets a 403 because 0,0 is inside none of their fences — and
// clearing a location should never need a fence.
func registerV2HumanClearLocation(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-clear-human-location", Method: "DELETE", Path: "/v2/humans/{id}/location",
		Summary: "Clear a human's location",
		Description: "Resets the location to 0,0 (unset), the same targeting as POST location. Not subject to the area-restriction " +
			"check. Rules with a distance > 0 stay as they are and will not match until a location is set again; reset their " +
			"distance if area matching is wanted. Triggers a state reload. 404 if the human does not exist.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2ClearLocationInput) (*statusOKOutput, error) {
		human, err := resolveFullHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}

		profileNo := human.CurrentProfileNo
		if in.Profile != profileSentinel {
			profileNo = in.Profile
		}

		if err := deps.Humans.SetLocation(in.ID, profileNo, 0, 0); err != nil {
			log.Errorf("v2 humans: clear location %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- check-location --------------------------------------------------------

type v2CheckLocationInput struct {
	ID  string  `path:"id" doc:"Human id (the owning user)"`
	Lat float64 `query:"latitude" required:"true" doc:"Latitude to check"`
	Lon float64 `query:"longitude" required:"true" doc:"Longitude to check"`
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
	Areas []string `json:"areas" required:"true" doc:"Requested area names. Matched case-insensitively against the fences this human may select; duplicates are collapsed and anything unmatched comes back in the rejected list rather than being dropped silently."`
	// The API secret is what distinguishes a server-side client from an end
	// user, so a caller that holds it may assert that a request is already
	// authorised on the user's behalf.
	//
	// This lifts the userSelectable filter ONLY. An unknown fence name is
	// still rejected, so a typo cannot become a stored area that matches
	// nothing; and when area_security is enabled the community restriction
	// still applies, so a trusted call cannot hand a user another community's
	// areas. Only admin lifts that. See #215, #228.
	Trusted bool `json:"trusted,omitempty" doc:"Set true to select fences that are not userSelectable — for a server-side client that has already authorised the change (e.g. the user drew the fence). Honoured because the caller holds the API secret. Unknown fence names are still rejected."`
}

// v2SetAreasOutput reports the outcome of a set-areas call. v1 answered 200
// with no body detail while quietly discarding names, so a caller could not
// tell a fully-applied request from a partly-ignored one (#215).
type v2SetAreasOutput struct {
	Body struct {
		Areas    []string `json:"areas" doc:"The areas now stored, lowercased and deduped"`
		Rejected []string `json:"rejected" doc:"Requested names that were NOT stored — unknown fences, or fences this human may not select. Empty when everything applied."`
	}
}

type v2SetAreasInput struct {
	ID string `path:"id" doc:"Human id (the owning user)"`
	// Profile targeting mirrors the tracking endpoints and set-location.
	// Without it set-areas wrote the active profile only, so dropping an area
	// from EVERY profile — what a client does when a user deletes a fence they
	// had drawn — had no API form at all.
	Profile     int  `query:"profile" default:"-1" doc:"Profile number to write; defaults to the human's active profile. Mutually exclusive with all_profiles."`
	AllProfiles bool `query:"all_profiles" doc:"Replace the areas on EVERY profile the human owns with the submitted list. This is a whole-list replace, not a per-name removal: profiles that currently hold different areas all end up with this list. To remove one area from every profile, use DELETE /api/v2/humans/{id}/areas?name=…&all_profiles=true. Mutually exclusive with profile."`
	Body        v2SetAreasBody
}

func registerV2HumanSetAreas(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-set-human-areas", Method: "POST", Path: "/v2/humans/{id}/areas",
		Summary: "Set a human's selected areas",
		Description: "Stores the requested areas as the matching fences' names (lowercased; underscores and spaces are " +
			"interchangeable in the request), deduped, after matching them against the fences this human may " +
			"select (userSelectable for non-admins, community-filtered when area_security is enabled). Names that do not match " +
			"come back in `rejected` rather than being discarded silently. Set `trusted: true` to select non-userSelectable " +
			"fences on behalf of a user whose client has already authorised the change; unknown names are still rejected. " +
			"Writes the human's active profile by default; `?profile=` targets another and `?all_profiles=true` replaces every " +
			"profile's areas with the submitted list (a whole-list replace; to drop one name everywhere use DELETE …/areas?name=). " +
			"The two are mutually exclusive. " +
			"Triggers a state reload. 404 if the human does not exist.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2SetAreasInput) (*v2SetAreasOutput, error) {
		human, err := resolveFullHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}

		if in.AllProfiles && in.Profile != profileSentinel {
			return nil, huma.Error422UnprocessableEntity("profile and all_profiles are mutually exclusive")
		}

		allowedSet := settableAreaSet(deps, human, isAdmin(deps, in.ID), in.Body.Trusted)

		// Resolve each name to its fence's own spelling first, so either
		// underscore/space spelling of a fence is accepted and the fence name
		// is what gets stored.
		var fences []geofence.Fence
		if deps.StateMgr != nil {
			if st := deps.StateMgr.Get(); st != nil {
				fences = st.Fences
			}
		}
		seen := make(map[string]bool)
		newAreas := []string{}
		rejected := []string{}
		for _, raw := range in.Body.Areas {
			a := geofence.CanonicalAreaName(fences, raw)
			if !allowedSet[a] {
				rejected = append(rejected, raw)
				continue
			}
			if seen[a] {
				continue
			}
			seen[a] = true
			newAreas = append(newAreas, a)
		}

		targets, err := v2AreaTargets(deps, human, in.AllProfiles, in.Profile)
		if err != nil {
			return nil, err
		}

		for _, profileNo := range targets {
			if err := deps.Humans.SetArea(in.ID, profileNo, newAreas); err != nil {
				log.Errorf("v2 humans: set areas %s/%d: %s", in.ID, profileNo, err)
				return nil, huma.Error500InternalServerError("database error")
			}
		}
		reloadState(deps)

		out := &v2SetAreasOutput{}
		out.Body.Areas = newAreas
		out.Body.Rejected = rejected
		return out, nil
	})
}

// --- remove one area -------------------------------------------------------

type v2RemoveAreaInput struct {
	ID          string `path:"id" doc:"Human id (the owning user)"`
	Name        string `query:"name" required:"true" minLength:"1" doc:"Area name to remove. Matched case-insensitively, with underscores and spaces treated alike. Need not be a current fence."`
	Profile     int    `query:"profile" default:"-1" doc:"Profile to remove it from; defaults to the active profile. Mutually exclusive with all_profiles."`
	AllProfiles bool   `query:"all_profiles" doc:"Remove it from every profile the human owns, leaving each profile's other areas intact. Mutually exclusive with profile."`
	DryRun      bool   `query:"dry_run" doc:"Report what would change without writing anything or reloading state."`
}

type v2RemoveAreaOutput struct {
	Body struct {
		Name        string             `json:"name" doc:"The name as requested"`
		RemovedFrom []int              `json:"removed_from" doc:"Profile numbers whose area list held the name and no longer does. Empty when no targeted profile held it."`
		Broadened   map[string][]int64 `json:"broadened" doc:"Rules whose only override area was this name, keyed by tracking type (pokemon, raid, egg, quest, invasion, lure, nest, gym, fort, maxbattle, tappable). Their override is cleared, so they now match across all of the human's areas instead of one. Empty when no rule was affected."`
		DryRun      bool               `json:"dry_run" doc:"True when nothing was written"`
	}
}

// registerV2HumanRemoveArea registers DELETE /v2/humans/{id}/areas (#252, #253).
//
// POST …/areas is a whole-list replace, so dropping one name from profiles
// that each hold different sets needed a read-modify-write per profile. This
// removes one name from each targeted profile's own list AND from
// override_areas on the human's rules in those profiles. No permission or
// existence check: a removal can never widen what the human is alerted for
// except by clearing a rule's last override (reported as `broadened`), and the
// fence is usually gone already. No DB transaction: the per-human lock covers
// concurrency and the operation is idempotent.
func registerV2HumanRemoveArea(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-remove-human-area", Method: "DELETE", Path: "/v2/humans/{id}/areas",
		Summary: "Remove one area from a human's profiles and rule overrides",
		Description: "Removes one area name from the active profile, `?profile=N`, or every profile with `?all_profiles=true`, " +
			"leaving each profile's other areas untouched, and removes it from `override_areas` on the human's rules in " +
			"those profiles. A rule whose only override area was this name loses its override and so now matches across " +
			"all of the human's areas; those rules are listed in `broadened`. Names match case-insensitively with " +
			"underscores and spaces alike. Removing a name nothing holds is a 200 with empty results. " +
			"`?dry_run=true` reports the same result without writing or reloading. " +
			"Triggers a state reload when something changed. 404 if the human does not exist.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2RemoveAreaInput) (*v2RemoveAreaOutput, error) {
		human, err := resolveFullHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}
		if in.AllProfiles && in.Profile != profileSentinel {
			return nil, huma.Error422UnprocessableEntity("profile and all_profiles are mutually exclusive")
		}

		targets, err := v2AreaTargets(deps, human, in.AllProfiles, in.Profile)
		if err != nil {
			return nil, err
		}

		removedFrom, areasChanged, err := deps.Humans.RemoveAreaFromProfiles(in.ID, in.Name, targets, in.DryRun)
		if err != nil {
			log.Errorf("v2 humans: remove area %q from %s: %s", in.Name, in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		broadened, overridesChanged, err := deps.Humans.EditOverrideAreas(in.ID, targets, in.Name, "", in.DryRun)
		if err != nil {
			log.Errorf("v2 humans: remove area %q overrides for %s: %s", in.Name, in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if !in.DryRun && (areasChanged || overridesChanged) {
			reloadState(deps)
		}

		out := &v2RemoveAreaOutput{}
		out.Body.Name = in.Name
		out.Body.RemovedFrom = removedFrom
		if out.Body.RemovedFrom == nil {
			out.Body.RemovedFrom = []int{}
		}
		out.Body.Broadened = broadened
		if out.Body.Broadened == nil {
			out.Body.Broadened = map[string][]int64{}
		}
		out.Body.DryRun = in.DryRun
		return out, nil
	})
}

// --- rename one area -------------------------------------------------------

type v2RenameAreaInput struct {
	ID          string `path:"id" doc:"Human id (the owning user)"`
	From        string `query:"from" required:"true" minLength:"1" doc:"Area name to rename. Matched case-insensitively, with underscores and spaces treated alike. Need not be a current fence."`
	To          string `query:"to" required:"true" minLength:"1" doc:"New area name. Must resolve to a loaded fence this human may hold; stored as that fence's lowercased name."`
	Profile     int    `query:"profile" default:"-1" doc:"Profile to rename in; defaults to the active profile. Mutually exclusive with all_profiles."`
	AllProfiles bool   `query:"all_profiles" doc:"Rename in every profile the human owns. Mutually exclusive with profile."`
	Trusted     bool   `query:"trusted" doc:"Allow a non-userSelectable fence as the target, for a client that has already authorised the change. Unknown names are still rejected."`
	DryRun      bool   `query:"dry_run" doc:"Report what would change without writing anything or reloading state."`
}

type v2RenameAreaOutput struct {
	Body struct {
		From      string `json:"from" doc:"The from name as requested"`
		To        string `json:"to" doc:"The canonical stored form of the new name (the fence's lowercased name)"`
		RenamedIn []int  `json:"renamed_in" doc:"Profile numbers whose area list changed. Empty when nothing held the old name (including on a repeat call)."`
		DryRun    bool   `json:"dry_run" doc:"True when nothing was written"`
	}
}

// registerV2HumanRenameArea registers PATCH /v2/humans/{id}/areas (#259).
// Renames one name in each targeted profile, humans.area (current profile) and
// override_areas on the human's rules, so a renamed fence does not leave the
// human with stale references. The target is permission-checked before any
// write; no transaction (per-human lock + idempotent).
func registerV2HumanRenameArea(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-rename-human-area", Method: "PATCH", Path: "/v2/humans/{id}/areas",
		Summary: "Rename one area in a human's profiles and rule overrides",
		Description: "Replaces area name `from` with `to` in the active profile, `?profile=N`, or every profile with " +
			"`?all_profiles=true`, and in `override_areas` on the human's rules in those profiles. `to` must resolve to a " +
			"loaded fence the human may hold (same rules as set-areas, `?trusted=true` included), otherwise 422 and nothing " +
			"changes; `from` needs no check. The stored spelling is the fence's lowercased name, and a list already holding " +
			"`to` in any spelling ends up with it exactly once. Names match case-insensitively with underscores and spaces " +
			"alike. Repeating the call is a 200 with empty `renamed_in`. `?dry_run=true` reports without writing or " +
			"reloading. Triggers a state reload when something changed. 404 if the human does not exist.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2RenameAreaInput) (*v2RenameAreaOutput, error) {
		human, err := resolveFullHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}
		if in.AllProfiles && in.Profile != profileSentinel {
			return nil, huma.Error422UnprocessableEntity("profile and all_profiles are mutually exclusive")
		}

		var fences []geofence.Fence
		if deps.StateMgr != nil {
			if st := deps.StateMgr.Get(); st != nil {
				fences = st.Fences
			}
		}
		to := geofence.CanonicalAreaName(fences, in.To)
		if !settableAreaSet(deps, human, isAdmin(deps, in.ID), in.Trusted)[to] {
			return nil, huma.Error422UnprocessableEntity("area not permitted: " + in.To)
		}

		targets, err := v2AreaTargets(deps, human, in.AllProfiles, in.Profile)
		if err != nil {
			return nil, err
		}

		renamedIn, areasChanged, err := deps.Humans.RenameAreaInProfiles(in.ID, in.From, to, targets, in.DryRun)
		if err != nil {
			log.Errorf("v2 humans: rename area %q for %s: %s", in.From, in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		_, overridesChanged, err := deps.Humans.EditOverrideAreas(in.ID, targets, in.From, to, in.DryRun)
		if err != nil {
			log.Errorf("v2 humans: rename area %q overrides for %s: %s", in.From, in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if !in.DryRun && (areasChanged || overridesChanged) {
			reloadState(deps)
		}

		out := &v2RenameAreaOutput{}
		out.Body.From = in.From
		out.Body.To = to
		out.Body.RenamedIn = renamedIn
		if out.Body.RenamedIn == nil {
			out.Body.RenamedIn = []int{}
		}
		out.Body.DryRun = in.DryRun
		return out, nil
	})
}

// --- shared helpers --------------------------------------------------------

// settableAreaSet keys bot.PermittedAreas for the set-areas path by the
// lowercased fence name, which is what set-areas stores; requested names are
// resolved to that form with geofence.CanonicalAreaName before the lookup.
// (The per-rule override validation keys the same policy with
// normalizeAreaKey.)
func settableAreaSet(deps *TrackingDeps, human *store.Human, admin, trusted bool) map[string]bool {
	set := make(map[string]bool)
	if deps.StateMgr == nil {
		return set
	}
	st := deps.StateMgr.Get()
	if st == nil {
		return set
	}
	for _, a := range bot.PermittedAreas(st.Fences, deps.Config, human, admin, trusted) {
		set[a] = true
	}
	return set
}

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
	registerV2HumansList(humaAPI, deps, tag, sec)
	registerV2HumanDelete(humaAPI, deps, tag, sec)
	registerV2HumanGet(humaAPI, deps, tag, sec)
	registerV2HumanAreasGet(humaAPI, deps, tag, sec)
	registerV2HumanEnable(humaAPI, deps, tag, sec)
	registerV2HumanDisable(humaAPI, deps, tag, sec)
	registerV2HumanAdminDisable(humaAPI, deps, tag, sec)
	registerV2HumanLanguage(humaAPI, deps, tag, sec)
	registerV2HumanSetLocation(humaAPI, deps, tag, sec)
	registerV2HumanClearLocation(humaAPI, deps, tag, sec)
	registerV2HumanCheckLocation(humaAPI, deps, tag, sec)
	registerV2HumanSetAreas(humaAPI, deps, tag, sec)
	registerV2HumanRemoveArea(humaAPI, deps, tag, sec)
	registerV2HumanRenameArea(humaAPI, deps, tag, sec)
}

// v2AreaTargets resolves the profile numbers an area endpoint writes: the
// current profile by default, ?profile= when given, or every profile with
// ?all_profiles=true. A human with no profiles rows (e.g. migrated from
// PoracleJS) falls back to the current profile, so all_profiles still covers
// humans.area and that profile's override_areas instead of writing nothing.
func v2AreaTargets(deps *TrackingDeps, human *store.Human, allProfiles bool, profile int) ([]int, error) {
	switch {
	case allProfiles:
		profiles, err := deps.Humans.GetProfiles(human.ID)
		if err != nil {
			log.Errorf("v2 humans: list profiles for %s: %s", human.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if len(profiles) == 0 {
			return []int{human.CurrentProfileNo}, nil
		}
		targets := make([]int, 0, len(profiles))
		for _, p := range profiles {
			targets = append(targets, p.ProfileNo)
		}
		return targets, nil
	case profile != profileSentinel:
		return []int{profile}, nil
	}
	return []int{human.CurrentProfileNo}, nil
}
