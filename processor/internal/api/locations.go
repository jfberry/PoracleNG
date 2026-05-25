package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/store"
)

type locationRow struct {
	Label     string  `json:"label"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type defaultLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type locationsPayload struct {
	Default *defaultLocation `json:"default,omitempty"`
	Named   []locationRow    `json:"named"`
}

// HandleListLocations returns the GET /api/humans/{id}/locations handler.
// Returns the user's default (lat/lon from humans table) and all named saved locations.
func HandleListLocations(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		locs, err := deps.Humans.ListLocations(id)
		if err != nil {
			log.Errorf("api locations list: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		out := locationsPayload{Named: make([]locationRow, 0, len(locs))}
		for _, l := range locs {
			out.Named = append(out.Named, locationRow{
				Label:     l.Label,
				Latitude:  l.Latitude,
				Longitude: l.Longitude,
			})
		}
		human, _ := deps.Humans.Get(id)
		if human != nil && (human.Latitude != 0 || human.Longitude != 0) {
			out.Default = &defaultLocation{
				Latitude:  human.Latitude,
				Longitude: human.Longitude,
			}
		}
		trackingJSONOK(c, map[string]any{"locations": out})
	}
}

// HandleGetLocation returns the GET /api/humans/{id}/locations/{label} handler.
// Label matching is case-insensitive (delegated to the store).
func HandleGetLocation(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		label := c.Param("label")
		loc, err := deps.Humans.GetLocation(id, label)
		if err != nil {
			log.Errorf("api locations get: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if loc == nil {
			trackingJSONError(c, http.StatusNotFound, "location not found")
			return
		}
		trackingJSONOK(c, map[string]any{
			"label":     loc.Label,
			"latitude":  loc.Latitude,
			"longitude": loc.Longitude,
		})
	}
}

type addLocationRequest struct {
	Label     string  `json:"label"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Place     string  `json:"place,omitempty"`
}

type addLocationResult struct {
	Label string `json:"label"`
	Error string `json:"error,omitempty"`
}

// HandleDeleteLocation returns the POST /api/humans/{id}/locations/{label}/delete handler.
// Returns 409 with referencing_rules when tracking rules still reference the location.
func HandleDeleteLocation(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		label := c.Param("label")

		refs, err := deps.Humans.CountLocationReferences(id, label)
		if err != nil {
			log.Errorf("api locations delete count refs: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if len(refs) > 0 {
			c.JSON(http.StatusConflict, map[string]any{
				"status":            "error",
				"error":             "location is referenced by tracking rules",
				"referencing_rules": refs,
			})
			return
		}
		if err := deps.Humans.DeleteLocation(id, label); err != nil {
			log.Errorf("api locations delete: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		reloadState(deps)
		trackingJSONOK(c, nil)
	}
}

// HandleAddLocation returns the POST /api/humans/{id}/locations/add handler.
// Accepts a single addLocationRequest object or a JSON array of them.
// Returns per-row results so partial-success batches surface individual errors
// (e.g. duplicate labels) without aborting the whole request.
func HandleAddLocation(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		rawBody, err := readBody(c)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, err.Error())
			return
		}

		var reqs []addLocationRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &reqs); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single addLocationRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
			reqs = []addLocationRequest{single}
		}

		results := make([]addLocationResult, 0, len(reqs))
		for _, r := range reqs {
			if r.Label == "" {
				results = append(results, addLocationResult{Label: r.Label, Error: "label required"})
				continue
			}
			if r.Place != "" {
				// Server-side place geocoding is deferred — v1 requires latitude+longitude.
				results = append(results, addLocationResult{Label: r.Label, Error: "place geocoding not yet supported via API; send latitude+longitude"})
				continue
			}
			if _, err := deps.Humans.AddLocation(store.UserLocation{ID: id, Label: r.Label, Latitude: r.Latitude, Longitude: r.Longitude}); err != nil {
				results = append(results, addLocationResult{Label: r.Label, Error: err.Error()})
				continue
			}
			results = append(results, addLocationResult{Label: r.Label})
		}
		reloadState(deps)
		trackingJSONOK(c, map[string]any{"results": results})
	}
}
