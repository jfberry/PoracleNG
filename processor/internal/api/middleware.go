package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/metrics"
)

// RequireSecretGin returns a Gin middleware that validates X-Poracle-Secret.
// If apiSecret is empty, all requests are allowed.
func RequireSecretGin(apiSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if apiSecret == "" {
			c.Next()
			return
		}
		if c.GetHeader("X-Poracle-Secret") != apiSecret {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"status": "authError",
				"reason": "incorrect or missing api secret",
			})
			return
		}
		c.Next()
	}
}

// RequestLogger returns a Gin middleware that logs API requests and records
// per-route Prometheus metrics (replacing the old InstrumentAPI wrapper).
// It uses c.FullPath() for the route label so path parameters are collapsed.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)

		path := c.Request.URL.Path

		// Skip logging for high-volume webhook receiver endpoint
		if path == "/" {
			return
		}

		log.Infof("API: %s %s %d %s",
			c.Request.Method, path,
			c.Writer.Status(), duration.Round(time.Millisecond))

		// Record Prometheus metrics using the route template (e.g. /api/tracking/pokemon/:id)
		// so that path parameters don't create unbounded label cardinality.
		endpoint := c.FullPath()
		if endpoint == "" {
			endpoint = c.Request.URL.Path // fallback for unmatched routes
		}
		method := c.Request.Method
		status := "ok"
		if c.Writer.Status() >= 400 {
			status = "error"
		}
		metrics.APIRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
		metrics.APIRequestsTotal.WithLabelValues(method, endpoint, status).Inc()
	}
}
