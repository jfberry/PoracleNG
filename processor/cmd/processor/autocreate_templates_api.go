package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/discordbot"
)

// channelTemplatesEnums is the static metadata the editor needs to render
// dropdowns + permission-flag pickers. Returned by GET .../schema.
type channelTemplatesEnums struct {
	ChannelTypes     []string                    `json:"channelTypes"`
	ControlTypes     []string                    `json:"controlTypes"`
	ButtonStyles     []string                    `json:"buttonStyles"`
	PermissionFlags  []discordbot.PermissionFlag `json:"permissionFlags"`
	PlaceholderHelp  map[string]string           `json:"placeholderHelp"`
	BackupNamePrefix string                      `json:"backupNamePrefix"`
}

// handleGetChannelTemplates implements GET /api/autocreate/templates.
// Returns the live channelTemplate.json contents as a typed array.
// A missing file yields {"status":"ok","templates":[]}.
func handleGetChannelTemplates(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := discordbot.LoadChannelTemplatesRaw(cfg.BaseDir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		// Round-trip via json.RawMessage so the response carries a real
		// JSON array (not a base64-encoded string) regardless of the
		// editor's deserialiser.
		c.Data(http.StatusOK, "application/json", buildOKEnvelope("templates", raw))
	}
}

// channelTemplatesPostRequest is the body shared by POST .../templates and
// POST .../templates/validate. The Templates field is treated as a raw
// JSON array so unknown fields survive a write through the API (any
// future field the bot adds can be set in the editor before the bot
// supports decoding it).
type channelTemplatesPostRequest struct {
	Templates json.RawMessage `json:"templates"`
}

// handlePostChannelTemplates implements POST /api/autocreate/templates —
// validate + write. On success returns the backup filename so the
// operator can roll back if the change was a mistake.
func handlePostChannelTemplates(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		req, raw, ok := readTemplatesBody(c)
		if !ok {
			return
		}
		_ = req // body shape verified

		if errs := discordbot.ValidateChannelTemplatesRaw(raw); hasBlockingErrors(errs) {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "errors": errs})
			return
		}
		backup, err := discordbot.SaveChannelTemplatesRaw(cfg.BaseDir, raw)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":   "ok",
			"backup":   backup,
			"warnings": nonBlocking(discordbot.ValidateChannelTemplatesRaw(raw)),
		})
	}
}

// handleValidateChannelTemplates implements POST /api/autocreate/templates/validate.
// Same body as POST .../templates but never writes — useful for "lint as
// you type" in the editor.
func handleValidateChannelTemplates() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, raw, ok := readTemplatesBody(c)
		if !ok {
			return
		}
		errs := discordbot.ValidateChannelTemplatesRaw(raw)
		if hasBlockingErrors(errs) {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "errors": errs})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "warnings": nonBlocking(errs)})
	}
}

// handleDeleteChannelTemplate implements DELETE /api/autocreate/templates/:name.
func handleDeleteChannelTemplate(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "template name is required"})
			return
		}
		backup, err := discordbot.DeleteChannelTemplate(cfg.BaseDir, name)
		if errors.Is(err, os.ErrNotExist) {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "template not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "backup": backup})
	}
}

// handleGetChannelTemplatesSchema implements GET /api/autocreate/templates/schema.
// Static metadata the editor uses to render dropdowns + permission flags.
func handleGetChannelTemplatesSchema() gin.HandlerFunc {
	return func(c *gin.Context) {
		out := channelTemplatesEnums{
			ChannelTypes:    []string{"text", "voice"},
			ControlTypes:    []string{"", "bot", "webhook"},
			ButtonStyles:    []string{"primary", "secondary", "success", "danger"},
			PermissionFlags: discordbot.PermissionFlagsList(),
			PlaceholderHelp: map[string]string{
				"interactive": "{N} indexes args[N+1] from !autocreate <template> <args...> — note the off-by-one (args[0] is the template name itself).",
				"bulk":        "[[autocreate.rules]] params[] elements render per-fence then become positional args in order. Whitespace inside a rendered element splits it into multiple args; \"quoted segments\" stay as one.",
			},
			BackupNamePrefix: "channelTemplate.json.bak.",
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "schema": out})
	}
}

// readTemplatesBody validates and extracts the templates raw JSON from a
// POST body shaped {"templates":[...]}. Writes the error response and
// returns ok=false on failure.
func readTemplatesBody(c *gin.Context) (channelTemplatesPostRequest, []byte, bool) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "read body: " + err.Error()})
		return channelTemplatesPostRequest{}, nil, false
	}
	var req channelTemplatesPostRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "parse body: " + err.Error()})
		return channelTemplatesPostRequest{}, nil, false
	}
	if len(req.Templates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "templates field is required"})
		return channelTemplatesPostRequest{}, nil, false
	}
	return req, []byte(req.Templates), true
}

func hasBlockingErrors(errs []discordbot.TemplateValidationError) bool {
	for _, e := range errs {
		if e.Severity == "error" {
			return true
		}
	}
	return false
}

func nonBlocking(errs []discordbot.TemplateValidationError) []discordbot.TemplateValidationError {
	out := errs[:0]
	for _, e := range errs {
		if e.Severity != "error" {
			out = append(out, e)
		}
	}
	return out
}

// buildOKEnvelope wraps a raw JSON value under {"status":"ok","<key>":<raw>}.
// Used so the GET endpoint can return the live file's array as actual
// JSON (not as a base64 string the way gin's c.JSON would render []byte).
func buildOKEnvelope(key string, raw []byte) []byte {
	var buf []byte
	buf = append(buf, `{"status":"ok","`...)
	buf = append(buf, key...)
	buf = append(buf, `":`...)
	buf = append(buf, raw...)
	buf = append(buf, '}')
	return buf
}
