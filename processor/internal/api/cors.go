package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware adds permissive CORS headers for the DTS editor.
// Only applied to /api/* routes which are already protected by the API secret.
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")

		// Reflect the requested headers back — avoids maintaining a static allowlist
		// that breaks when clients send additional headers.
		if reqHeaders := c.GetHeader("Access-Control-Request-Headers"); reqHeaders != "" {
			c.Header("Access-Control-Allow-Headers", reqHeaders)
		} else {
			c.Header("Access-Control-Allow-Headers", "Content-Type, X-Poracle-Secret")
		}

		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
