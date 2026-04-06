package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// TestDataEntry represents a single test scenario from testdata.json.
type TestDataEntry struct {
	Type     string          `json:"type"`
	Test     string          `json:"test"`
	Location string          `json:"location"`
	Webhook  json.RawMessage `json:"webhook"`
}

// HandleDTSTestdata returns test webhook scenarios for the DTS editor.
// GET /api/dts/testdata?type=pokemon
// Without type filter, returns all scenarios.
func HandleDTSTestdata(configDir, fallbackDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		filterType := c.Query("type")

		entries := loadTestdata(configDir, fallbackDir)
		if entries == nil {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "testdata.json not found"})
			return
		}

		if filterType != "" {
			var filtered []TestDataEntry
			for _, e := range entries {
				if e.Type == filterType {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok", "testdata": entries})
	}
}

// loadTestdata reads testdata.json, merging config (overrides) with fallback (defaults).
func loadTestdata(configDir, fallbackDir string) []TestDataEntry {
	// Load fallback first
	fallbackEntries := readTestdataFile(filepath.Join(fallbackDir, "testdata.json"))

	// Load config override
	configEntries := readTestdataFile(filepath.Join(configDir, "testdata.json"))

	if fallbackEntries == nil && configEntries == nil {
		return nil
	}

	if configEntries == nil {
		return fallbackEntries
	}
	if fallbackEntries == nil {
		return configEntries
	}

	// Merge: config entries override fallback entries by type+test key
	configKeys := make(map[string]TestDataEntry, len(configEntries))
	for _, e := range configEntries {
		configKeys[e.Type+"/"+e.Test] = e
	}

	merged := make([]TestDataEntry, 0, len(fallbackEntries)+len(configEntries))
	for _, e := range fallbackEntries {
		key := e.Type + "/" + e.Test
		if override, ok := configKeys[key]; ok {
			merged = append(merged, override)
			delete(configKeys, key)
		} else {
			merged = append(merged, e)
		}
	}
	// Append config-only entries not present in fallback
	for _, e := range configKeys {
		merged = append(merged, e)
	}

	return merged
}

func readTestdataFile(path string) []TestDataEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []TestDataEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Warnf("dts testdata: failed to parse %s: %v", path, err)
		return nil
	}
	return entries
}
