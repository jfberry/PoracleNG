package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// TestRequest is the payload for POST /api/test.
type TestRequest struct {
	Type    string          `json:"type"`    // webhook type: pokemon, raid, invasion, etc.
	Webhook json.RawMessage `json:"webhook"` // the raw webhook message
	Target  TestTarget      `json:"target"`  // who to send the result to
}

// TestTarget specifies the user/channel to deliver the test alert to.
type TestTarget struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`      // discord:user, telegram:user, etc.
	Language  string  `json:"language"`
	Template  string  `json:"template"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// TestProcessor processes a single test webhook through the enrichment pipeline.
type TestProcessor interface {
	ProcessTest(webhookType string, raw json.RawMessage, target TestTarget) error
}

// HandleTest returns a handler for POST /api/test.
// The test endpoint accepts a webhook + target, runs it through the normal
// enrichment pipeline (skipping matching/dedup), and sends the enriched
// result for the specified target user via the render pipeline.
func HandleTest(proc TestProcessor) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
			return
		}

		var req TestRequest
		if err := json.Unmarshal(body, &req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
			return
		}

		if req.Type == "" || req.Webhook == nil || req.Target.ID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "type, webhook, and target.id are required"})
			return
		}

		log.Infof("[Test] Processing %s test for %s %s", req.Type, req.Target.Type, req.Target.ID)

		if err := proc.ProcessTest(req.Type, req.Webhook, req.Target); err != nil {
			log.Errorf("[Test] Failed to process %s test: %s", req.Type, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}
