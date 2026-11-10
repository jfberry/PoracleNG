package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/humanlock"
)

// HumanLock serialises every non-read request addressed to one human
// (/…/:id/…) against other writers of that human (bot commands, reconcilers).
// Register it on the /api group before the huma and v1 route groups are
// created so it covers both. Handlers must not take the same lock again.
func HumanLock() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		id := c.Param("id")
		if id == "" {
			c.Next()
			return
		}
		unlock := humanlock.Lock(id)
		defer unlock()
		c.Next()
	}
}
