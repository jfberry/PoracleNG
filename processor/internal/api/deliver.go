package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/delivery"
)

// HandleDeliverMessages returns a handler that accepts pre-rendered delivery jobs
// and dispatches them to the delivery system. This is used by the alerter's
// broadcast command and other command-generated messages.
func HandleDeliverMessages(dispatcher *delivery.Dispatcher) gin.HandlerFunc {
	return func(c *gin.Context) {
		if dispatcher == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":  "error",
				"message": "delivery dispatcher not configured",
			})
			return
		}

		var jobs []delivery.Job
		if err := c.ShouldBindJSON(&jobs); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": "invalid JSON: " + err.Error(),
			})
			return
		}

		queued := 0
		for i := range jobs {
			if jobs[i].Target == "" || jobs[i].Type == "" {
				continue
			}
			dispatcher.Dispatch(&jobs[i])
			queued++
		}

		log.Debugf("Accepted %d delivery jobs via API", queued)

		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"queued": queued,
		})
	}
}
