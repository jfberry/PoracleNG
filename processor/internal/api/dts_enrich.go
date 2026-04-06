package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// EnrichService is the interface the handler needs from ProcessorService.
// This avoids importing the main package.
type EnrichService interface {
	EnrichWebhook(webhookType string, raw json.RawMessage, language, platform string) (map[string]any, error)
}

// HandleDTSEnrich runs a webhook through the enrichment pipeline and returns the variable map.
// POST /api/dts/enrich
func HandleDTSEnrich(svc EnrichService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Type     string          `json:"type"`
			Webhook  json.RawMessage `json:"webhook"`
			Language string          `json:"language"`
			Platform string          `json:"platform"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid request body: " + err.Error()})
			return
		}

		if req.Type == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "type is required"})
			return
		}
		if len(req.Webhook) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "webhook is required"})
			return
		}
		if req.Language == "" {
			req.Language = "en"
		}
		if req.Platform == "" {
			req.Platform = "discord"
		}

		variables, err := svc.EnrichWebhook(req.Type, req.Webhook, req.Language, req.Platform)
		if err != nil {
			log.Errorf("dts enrich: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok", "variables": variables})
	}
}
