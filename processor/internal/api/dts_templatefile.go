package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/dts"
)

// HandleDTSTemplateFileWrite updates the raw content of a templateFile entry.
// The file path is resolved from the template's key fields — no client-supplied
// paths are used, preventing path traversal. Readonly entries are rejected.
//
// PUT /api/dts/templates/file?type=fort-update&platform=discord&id=1&language=en
// Body: {"content": "raw handlebars text"}
func HandleDTSTemplateFileWrite(ts *dts.TemplateStore, configDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		entry := ts.GetEntry(c.Query("type"), c.Query("platform"), c.Query("language"), c.Query("id"))
		if entry == nil {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "template not found"})
			return
		}
		if entry.TemplateFile == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "template uses inline JSON, not a templateFile"})
			return
		}
		if entry.Readonly {
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "template is readonly (bundled default)"})
			return
		}

		var req struct {
			Content string `json:"content"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid request body"})
			return
		}

		path := filepath.Join(configDir, entry.TemplateFile)
		// Safety: ensure resolved path stays under configDir
		absPath, _ := filepath.Abs(path)
		absConfig, _ := filepath.Abs(configDir)
		if !strings.HasPrefix(absPath, absConfig+string(filepath.Separator)) {
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "invalid template file path"})
			return
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "create directory: " + err.Error()})
			return
		}

		if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "write file: " + err.Error()})
			return
		}

		ts.ClearCache()

		log.Infof("dts: updated template file %s via API", entry.TemplateFile)
		c.JSON(http.StatusOK, gin.H{"status": "ok", "templateFile": entry.TemplateFile})
	}
}
