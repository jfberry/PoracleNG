package api

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2_locations.go is the strict /api/v2 saved-locations CRUD surface. Each op is
// a thin wrapper around the exact business logic of the frozen v1 handlers in
// locations.go — reusing store.HumanStore (ListLocations / GetLocation /
// AddLocation / DeleteLocation / CountLocationReferences) and reloadState. The
// differences are strict typed huma inputs (additionalProperties:false, required
// enforcement, unknown query rejection) and problem+json errors.
//
// New in v2: PUT /v2/humans/{id}/locations/{label} updates a saved location's
// coords (no v1 equivalent), backed by the new store.UpdateLocation method.
//
// Success-shape convention:
//   - GET endpoints return the resource ({locations:...} / {location:...}).
//   - mutation POST/PUT/DELETE return {status:"ok"}; the DELETE-referenced case
//     preserves v1's 409 referencing-rules payload.

// --- shared shapes ----------------------------------------------------------

// v2LocationItem is one saved location in a v2 response.
type v2LocationItem struct {
	Label     string  `json:"label" doc:"Saved-location label (unique per human, matched case-insensitively); referenced by tracking rules via override_location_label."`
	Latitude  float64 `json:"latitude" doc:"Saved-location latitude."`
	Longitude float64 `json:"longitude" doc:"Saved-location longitude."`
}

// v2DefaultLocation is the human's default (humans-table) lat/lon.
type v2DefaultLocation struct {
	Latitude  float64 `json:"latitude" doc:"Default-location latitude from the humans table."`
	Longitude float64 `json:"longitude" doc:"Default-location longitude from the humans table."`
}

// v2LocationsOutput is the GET list response: named saved locations + an
// optional default. Mirrors v1's locationsPayload, wrapped in {locations}.
type v2LocationsOutput struct {
	Body struct {
		Locations struct {
			Default *v2DefaultLocation `json:"default,omitempty"`
			Named   []v2LocationItem   `json:"named"`
		} `json:"locations"`
	}
}

// v2LocationOutput is the GET one response: {location:{...}}.
type v2LocationOutput struct {
	Body struct {
		Location v2LocationItem `json:"location"`
	}
}

// v2LocationLabelInput addresses one location by {id} + {label}.
type v2LocationLabelInput struct {
	ID    string `path:"id" doc:"Human id (the owning user)"`
	Label string `path:"label" doc:"Saved-location label (case-insensitive)"`
}

// --- list -------------------------------------------------------------------

func registerV2LocationsList(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-list-human-locations", Method: "GET", Path: "/v2/humans/{id}/locations",
		Summary:     "List a human's saved locations",
		Description: "Returns every named saved location plus the human's default (humans-table) lat/lon when set.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*v2LocationsOutput, error) {
		locs, err := deps.Humans.ListLocations(in.ID)
		if err != nil {
			log.Errorf("v2 locations: list %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		out := &v2LocationsOutput{}
		out.Body.Locations.Named = make([]v2LocationItem, 0, len(locs))
		for _, l := range locs {
			out.Body.Locations.Named = append(out.Body.Locations.Named, v2LocationItem{
				Label:     l.Label,
				Latitude:  l.Latitude,
				Longitude: l.Longitude,
			})
		}
		human, _ := deps.Humans.Get(in.ID)
		if human != nil && (human.Latitude != 0 || human.Longitude != 0) {
			out.Body.Locations.Default = &v2DefaultLocation{
				Latitude:  human.Latitude,
				Longitude: human.Longitude,
			}
		}
		return out, nil
	})
}

// --- get one ----------------------------------------------------------------

func registerV2LocationsGet(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-get-human-location", Method: "GET", Path: "/v2/humans/{id}/locations/{label}",
		Summary:     "Fetch one saved location",
		Description: "Returns the saved location matched by case-insensitive label. 404 if the label does not exist.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2LocationLabelInput) (*v2LocationOutput, error) {
		loc, err := deps.Humans.GetLocation(in.ID, in.Label)
		if err != nil {
			log.Errorf("v2 locations: get %s/%s: %s", in.ID, in.Label, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if loc == nil {
			return nil, huma.Error404NotFound("location not found")
		}
		out := &v2LocationOutput{}
		out.Body.Location = v2LocationItem{
			Label:     loc.Label,
			Latitude:  loc.Latitude,
			Longitude: loc.Longitude,
		}
		return out, nil
	})
}

// --- create -----------------------------------------------------------------

// v2AddLocationBody is the strict create body. v1 accepted a single object or an
// array and a deferred `place` field; v2 takes exactly one {label,lat,lon}.
type v2AddLocationBody struct {
	Label     string  `json:"label" required:"true" doc:"Unique saved-location label"`
	Latitude  float64 `json:"lat" required:"true" doc:"Latitude"`
	Longitude float64 `json:"lon" required:"true" doc:"Longitude"`
}

type v2AddLocationInput struct {
	ID   string `path:"id" doc:"Human id (the owning user)"`
	Body v2AddLocationBody
}

func registerV2LocationsAdd(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-add-human-location", Method: "POST", Path: "/v2/humans/{id}/locations",
		Summary:     "Create a saved location",
		Description: "Creates one named saved location. 422 if the label is empty; 409 if the label already exists. Triggers a state reload.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2AddLocationInput) (*statusOKOutput, error) {
		if in.Body.Label == "" {
			return nil, huma.Error422UnprocessableEntity("label is required")
		}
		_, err := deps.Humans.AddLocation(store.UserLocation{
			ID:        in.ID,
			Label:     in.Body.Label,
			Latitude:  in.Body.Latitude,
			Longitude: in.Body.Longitude,
		})
		if err != nil {
			if errors.Is(err, store.ErrDuplicateLocation) {
				return nil, huma.Error409Conflict("location label already exists")
			}
			log.Errorf("v2 locations: add %s/%s: %s", in.ID, in.Body.Label, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- update (NEW — no v1 equivalent) ----------------------------------------

// v2UpdateLocationBody carries the new coords for an existing label.
type v2UpdateLocationBody struct {
	Latitude  float64 `json:"lat" required:"true" doc:"New latitude"`
	Longitude float64 `json:"lon" required:"true" doc:"New longitude"`
}

type v2UpdateLocationInput struct {
	ID    string `path:"id" doc:"Human id (the owning user)"`
	Label string `path:"label" doc:"Saved-location label (case-insensitive)"`
	Body  v2UpdateLocationBody
}

func registerV2LocationsUpdate(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-update-human-location", Method: "PUT", Path: "/v2/humans/{id}/locations/{label}",
		Summary:     "Update a saved location's coords",
		Description: "Overwrites the lat/lon of an existing saved location (matched case-insensitively by label). 404 if the label does not exist. Triggers a state reload. This capability is new in v2 (v1 had add/delete only).",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2UpdateLocationInput) (*statusOKOutput, error) {
		// Verify the label exists first so a missing label is a clean 404 rather
		// than an opaque store error.
		loc, err := deps.Humans.GetLocation(in.ID, in.Label)
		if err != nil {
			log.Errorf("v2 locations: update lookup %s/%s: %s", in.ID, in.Label, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if loc == nil {
			return nil, huma.Error404NotFound("location not found")
		}
		if err := deps.Humans.UpdateLocation(in.ID, in.Label, in.Body.Latitude, in.Body.Longitude); err != nil {
			if errors.Is(err, store.ErrLocationNotFound) {
				return nil, huma.Error404NotFound("location not found")
			}
			log.Errorf("v2 locations: update %s/%s: %s", in.ID, in.Label, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// --- delete -----------------------------------------------------------------

// v2RefRule is the v2 DTO for a tracking rule that still references a
// to-be-deleted saved location. It maps store.ReferencingRule (which has no
// json tags — and must not gain any, since v1's frozen HandleDeleteLocation
// serializes it Go-cased) into snake_case fields for the v2 surface.
type v2RefRule struct {
	Type string `json:"type"`
	UID  int64  `json:"uid"`
}

// v2LocationReferencedError is the 409 body for a delete blocked by referencing
// rules. It embeds huma.ErrorModel so it inherits RFC 9457 problem+json: the
// promoted ContentType() yields application/problem+json, GetStatus() returns
// the numeric Status, and the marshaled body carries title/status/detail. The
// ReferencingRules extension field conveys which rules block the delete.
type v2LocationReferencedError struct {
	huma.ErrorModel
	ReferencingRules []v2RefRule `json:"referencing_rules"`
}

func newLocationRefsConflict(refs []store.ReferencingRule) *v2LocationReferencedError {
	mapped := make([]v2RefRule, 0, len(refs))
	for _, r := range refs {
		mapped = append(mapped, v2RefRule{Type: r.Type, UID: r.UID})
	}
	return &v2LocationReferencedError{
		ErrorModel: huma.ErrorModel{
			Status: 409,
			Title:  "Conflict",
			Detail: "location is referenced by one or more tracking rules",
		},
		ReferencingRules: mapped,
	}
}

func registerV2LocationsDelete(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-delete-human-location", Method: "DELETE", Path: "/v2/humans/{id}/locations/{label}",
		Summary:     "Delete a saved location",
		Description: "Deletes the named saved location (case-insensitive). 409 with the referencing rules when a tracking rule's override_location_label still points at it. Triggers a state reload.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2LocationLabelInput) (*statusOKOutput, error) {
		refs, err := deps.Humans.CountLocationReferences(in.ID, in.Label)
		if err != nil {
			log.Errorf("v2 locations: delete count refs %s/%s: %s", in.ID, in.Label, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if len(refs) > 0 {
			// Preserve v1's 409 referencing-rules payload verbatim.
			return nil, newLocationRefsConflict(refs)
		}
		if err := deps.Humans.DeleteLocation(in.ID, in.Label); err != nil {
			log.Errorf("v2 locations: delete %s/%s: %s", in.ID, in.Label, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}

// RegisterV2Locations registers the strict v2 saved-locations CRUD endpoints
// (list, get, add, update, delete) under /api/v2.
func RegisterV2Locations(humaAPI huma.API, deps *TrackingDeps) {
	tag := []string{"v2-locations"}
	sec := []map[string][]string{{"poracleSecret": {}}}

	registerV2LocationsList(humaAPI, deps, tag, sec)
	registerV2LocationsGet(humaAPI, deps, tag, sec)
	registerV2LocationsAdd(humaAPI, deps, tag, sec)
	registerV2LocationsUpdate(humaAPI, deps, tag, sec)
	registerV2LocationsDelete(humaAPI, deps, tag, sec)
}
