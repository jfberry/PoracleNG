package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	raymond "github.com/mailgun/raymond/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/dts"
)

// HandleTemplateConfig returns DTS template metadata for PoracleWeb.
// GET /api/config/templates
// Optional query parameter: ?includeDescriptions=true adds name/description fields.
func HandleTemplateConfig(ts *dts.TemplateStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		includeDescriptions := c.Query("includeDescriptions") == "true"
		metadata := ts.TemplateMetadata(includeDescriptions)

		resp := map[string]any{"status": "ok"}
		for k, v := range metadata {
			resp[k] = v
		}
		c.JSON(http.StatusOK, resp)
	}
}

// HandleDTSRender renders a DTS template on demand.
// POST /api/dts/render
//
// Request body:
//
//	{"type": "help", "id": "track", "platform": "discord", "language": "en", "view": {"prefix": "!"}}
//
// Response:
//
//	{"status": "ok", "message": {...rendered template object...}}
func HandleDTSRender(ts *dts.TemplateStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Type     string         `json:"type"`
			ID       string         `json:"id"`
			Platform string         `json:"platform"`
			Language string         `json:"language"`
			View     map[string]any `json:"view"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "invalid request body: " + err.Error()})
			return
		}

		tmpl := ts.Get(req.Type, req.Platform, req.ID, req.Language)
		if tmpl == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"status": "error",
				"error":  fmt.Sprintf("no template found for %s/%s/%s/%s", req.Type, req.Platform, req.ID, req.Language),
			})
			return
		}

		view := req.View
		if view == nil {
			view = make(map[string]any)
		}

		df := raymond.NewDataFrame()
		df.Set("language", req.Language)
		df.Set("platform", req.Platform)

		rendered, err := tmpl.ExecWith(view, df)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "template render failed: " + err.Error()})
			return
		}

		// Parse the rendered JSON string into an object
		var message any
		if err := json.Unmarshal([]byte(rendered), &message); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "rendered template is not valid JSON: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": message})
	}
}

// HandleDTSGetTemplates returns DTS template entries with full content.
// GET /api/dts/templates?type=monster&platform=discord&language=en&id=1
func HandleDTSGetTemplates(ts *dts.TemplateStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		entries := ts.FilteredEntries(
			c.Query("type"),
			c.Query("platform"),
			c.Query("language"),
			c.Query("id"),
		)
		c.JSON(http.StatusOK, gin.H{"status": "ok", "templates": entries})
	}
}

// HandleDTSSaveTemplates accepts an array of DTS entries and saves them.
// POST /api/dts/templates
func HandleDTSSaveTemplates(ts *dts.TemplateStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var entries []dts.DTSEntry
		if err := c.ShouldBindJSON(&entries); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid request body: " + err.Error()})
			return
		}

		updated, inserted := ts.UpdateEntries(entries)

		if err := ts.SaveToFile(); err != nil {
			log.Errorf("dts: save failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "save failed: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":   "ok",
			"updated":  updated,
			"inserted": inserted,
		})
	}
}

// HandleDTSDeleteTemplate deletes a DTS template entry by its key fields.
// DELETE /api/dts/templates?type=monster&platform=discord&language=en&id=1
func HandleDTSDeleteTemplate(ts *dts.TemplateStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		filterType := c.Query("type")
		filterPlatform := c.Query("platform")
		filterLanguage := c.Query("language")
		filterID := c.Query("id")

		if filterType == "" || filterPlatform == "" || filterID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "type, platform, and id query parameters are required"})
			return
		}

		deleted := ts.DeleteEntry(filterType, filterPlatform, filterLanguage, filterID)
		if !deleted {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "template not found"})
			return
		}

		if err := ts.SaveToFile(); err != nil {
			log.Errorf("dts: save failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "save failed: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

// HandleDTSPartials returns Handlebars partials for the DTS editor.
// GET /api/dts/partials
func HandleDTSPartials(ts *dts.TemplateStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "partials": ts.Partials()})
	}
}
