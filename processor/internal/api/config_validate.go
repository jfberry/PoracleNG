package api

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// ValidationIssue describes a problem with a single config field value.
// Severity is "error" (save will be rejected) or "warning" (save proceeds
// but the editor should display the issue).
type ValidationIssue struct {
	Field    string `json:"field"`    // dotted path, e.g. "geofence.paths[0]"
	Severity string `json:"severity"` // "error" or "warning"
	Message  string `json:"message"`
}

// HandleConfigValidate runs the same validation pass as POST /api/config/values
// but doesn't save anything. Used by the editor to live-preview validation
// state (path existence checks, colour format, array length, etc.) before
// the user submits.
//
// POST /api/config/validate
func HandleConfigValidate(deps ConfigDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var updates map[string]any
		if err := c.ShouldBindJSON(&updates); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid request body: " + err.Error()})
			return
		}

		issues := validateConfigValues(updates, deps.ConfigDir)
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"issues": issues,
		})
	}
}

// validateConfigValues runs all per-field validators and returns the list
// of issues. Empty list means everything is OK.
func validateConfigValues(updates map[string]any, configDir string) []ValidationIssue {
	var issues []ValidationIssue
	schemaIndex := buildSchemaIndex()

	for sectionName, sectionVal := range updates {
		sectionMap, ok := sectionVal.(map[string]any)
		if !ok {
			continue
		}
		section, ok := schemaIndex[sectionName]
		if !ok {
			continue // unknown sections handled by validateUpdates
		}

		for fieldName, value := range sectionMap {
			field, ok := section[fieldName]
			if !ok {
				continue // unknown fields handled by validateUpdates
			}
			path := sectionName + "." + fieldName
			issues = append(issues, validateField(path, field, value, configDir)...)
		}
	}

	return issues
}

// validateField runs all checks applicable to a single field value.
func validateField(path string, field *ConfigFieldDef, value any, configDir string) []ValidationIssue {
	var issues []ValidationIssue

	// Array length checks
	if field.MinLength > 0 || field.MaxLength > 0 {
		if arr, ok := value.([]any); ok {
			n := len(arr)
			if field.MinLength > 0 && n < field.MinLength {
				issues = append(issues, ValidationIssue{
					Field: path, Severity: "error",
					Message: fmt.Sprintf("requires at least %d entries (got %d)", field.MinLength, n),
				})
			}
			if field.MaxLength > 0 && n > field.MaxLength {
				issues = append(issues, ValidationIssue{
					Field: path, Severity: "error",
					Message: fmt.Sprintf("allows at most %d entries (got %d)", field.MaxLength, n),
				})
			}
		}
	}

	// Type-specific checks
	switch field.Type {
	case "color[]":
		if arr, ok := value.([]any); ok {
			for i, v := range arr {
				if s, ok := v.(string); ok {
					if !isValidHexColor(s) {
						issues = append(issues, ValidationIssue{
							Field: fmt.Sprintf("%s[%d]", path, i), Severity: "error",
							Message: fmt.Sprintf("not a valid hex colour (expected #RGB or #RRGGBB): %q", s),
						})
					}
				}
			}
		}
	}

	// Geofence path validation — special-cased for the one field that needs it
	if path == "geofence.paths" {
		if arr, ok := value.([]any); ok {
			for i, v := range arr {
				s, ok := v.(string)
				if !ok {
					continue
				}
				if issue := validateGeofencePath(s, configDir); issue != nil {
					issue.Field = fmt.Sprintf("%s[%d]", path, i)
					issues = append(issues, *issue)
				}
			}
		}
	}

	return issues
}

// hexColorRegex matches #RGB and #RRGGBB.
var hexColorRegex = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

func isValidHexColor(s string) bool {
	return hexColorRegex.MatchString(s)
}

// validateGeofencePath checks that a geofence path is one of:
//   - an http(s):// URL (existence is not checked — that would be slow
//     and the URL might be reachable from the server but not from here)
//   - a relative path under configDir/geofences that exists on disk
//
// Absolute paths and paths that escape the config dir are flagged as errors.
// A relative path that doesn't exist yet is flagged as a warning (the user
// might be configuring a fence they haven't created yet).
func validateGeofencePath(p, configDir string) *ValidationIssue {
	if p == "" {
		return &ValidationIssue{Severity: "error", Message: "empty path"}
	}

	// HTTP(S) URLs — accept without checking
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		if _, err := url.ParseRequestURI(p); err != nil {
			return &ValidationIssue{Severity: "error", Message: "invalid URL: " + err.Error()}
		}
		return nil
	}

	// Reject absolute paths and parent escapes
	if filepath.IsAbs(p) {
		return &ValidationIssue{Severity: "error", Message: "absolute paths not allowed; use a path relative to the config directory"}
	}
	cleaned := filepath.Clean(p)
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") {
		return &ValidationIssue{Severity: "error", Message: "path escapes the config directory"}
	}

	// Check existence relative to configDir
	full := filepath.Join(configDir, cleaned)
	rel, err := filepath.Rel(configDir, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return &ValidationIssue{Severity: "error", Message: "path escapes the config directory"}
	}
	if _, err := os.Stat(full); os.IsNotExist(err) {
		return &ValidationIssue{Severity: "warning", Message: "file does not exist (yet) at " + full}
	} else if err != nil {
		return &ValidationIssue{Severity: "error", Message: err.Error()}
	}

	return nil
}

// schemaIndex is sectionName → fieldName → *ConfigFieldDef.
type schemaSectionIndex map[string]*ConfigFieldDef
type schemaIndex map[string]schemaSectionIndex

// buildSchemaIndex builds a fast lookup of section/field metadata.
func buildSchemaIndex() schemaIndex {
	idx := make(schemaIndex)
	for i := range configSchema {
		section := &configSchema[i]
		fields := make(schemaSectionIndex, len(section.Fields))
		for j := range section.Fields {
			f := &section.Fields[j]
			fields[f.Name] = f
		}
		idx[section.Name] = fields
	}
	return idx
}
