package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/geocoding"
)

// HandleGeocode returns a handler for GET /api/geocode/forward?q=QUERY.
// It performs a forward geocode lookup and returns the results as JSON.
func HandleGeocode(geocoder *geocoding.Geocoder) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.Query("q")
		if query == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "q parameter required"})
			return
		}

		if geocoder == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "geocoder not configured"})
			return
		}

		results, err := geocoder.Forward(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, results)
	}
}
