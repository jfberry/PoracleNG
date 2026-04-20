package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// HandleReload returns a Gin handler that triggers a state reload.
func HandleReload(reloadFn func() error) gin.HandlerFunc {
	return func(c *gin.Context) {
		log.Infof("Reload requested via API")
		if err := reloadFn(); err != nil {
			log.Errorf("Reload failed: %s", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

// WeatherExporter returns weather data for a specific cell.
type WeatherExporter interface {
	ExportCellWeather(cellID string) map[int64]int
}

// HandleWeather returns a Gin handler that serves weather data for a cell.
func HandleWeather(weather WeatherExporter) gin.HandlerFunc {
	return func(c *gin.Context) {
		cellID := c.Query("cell")
		if cellID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cell parameter required"})
			return
		}

		c.JSON(http.StatusOK, weather.ExportCellWeather(cellID))
	}
}

// HandleStats returns a Gin handler that serves the result of a stats function.
func HandleStats(fn func() any) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, fn())
	}
}

// HandleHealth returns a simple Gin health check handler.
func HandleHealth() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	}
}
