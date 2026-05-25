package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
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
