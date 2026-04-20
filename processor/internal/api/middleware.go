package api

import (
	"net"
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

// IPFilter returns a Gin middleware that enforces IP whitelist and blacklist.
// If whitelist is non-empty, only listed IPs are allowed.
// If blacklist is non-empty, listed IPs are rejected.
// Both lists are checked against the client IP (from Gin's ClientIP which
// respects trusted proxies / X-Forwarded-For).
func IPFilter(whitelist, blacklist []string) gin.HandlerFunc {
	if len(whitelist) == 0 && len(blacklist) == 0 {
		return func(c *gin.Context) { c.Next() }
	}

	whiteSet := make(map[string]bool, len(whitelist))
	for _, ip := range whitelist {
		whiteSet[normalizeIP(ip)] = true
	}
	blackSet := make(map[string]bool, len(blacklist))
	for _, ip := range blacklist {
		blackSet[normalizeIP(ip)] = true
	}

	return func(c *gin.Context) {
		clientIP := normalizeIP(c.ClientIP())

		if len(blackSet) > 0 && blackSet[clientIP] {
			log.Warnf("API: rejected blacklisted IP %s for %s %s", clientIP, c.Request.Method, c.Request.URL.Path)
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if len(whiteSet) > 0 && !whiteSet[clientIP] {
			log.Warnf("API: rejected non-whitelisted IP %s for %s %s", clientIP, c.Request.Method, c.Request.URL.Path)
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}

// normalizeIP strips port and zone from an IP address for consistent matching.
func normalizeIP(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
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
