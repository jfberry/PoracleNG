package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// HandleReload returns a Gin handler that triggers a state reload.
func HandleReload(reloadFn func() error) gin.HandlerFunc {
	return func(c *gin.Context) {
		log.Infof("Reload requested via API")
		if err := reloadFn(); err != nil {
			log.Errorf("Reload failed: %s", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

// WeatherExporter returns weather data for a specific cell.
type WeatherExporter interface {
	ExportCellWeather(cellID string) map[int64]int
}

// HandleWeather returns a Gin handler that serves weather data for a cell.
func HandleWeather(weather WeatherExporter) gin.HandlerFunc {
	return func(c *gin.Context) {
		cellID := c.Query("cell")
		if cellID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cell parameter required"})
			return
		}

		c.JSON(http.StatusOK, weather.ExportCellWeather(cellID))
	}
}

// HandleStats returns a Gin handler that serves the result of a stats function.
func HandleStats(fn func() any) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, fn())
	}
}

// Capabilities is the static feature map this PoracleNG binary supports.
// Returned in the /health response so clients (config editor, web UI)
// can do explicit feature detection rather than probing endpoints or
// sniffing response shapes.
//
// Design rules:
//   - Booleans only. Limits, scopes, parameter lists etc. live in their
//     own feature-specific endpoints (e.g. /api/dts/actions). This map
//     answers "is the feature compiled in", not "how is it configured".
//   - Forward-compatible by construction. Clients default-false on
//     missing keys so adding a capability is non-breaking. Removing or
//     repurposing a key IS breaking — pick names that age well.
//   - Capability ≠ runtime enablement. Buttons depend on
//     [snapshots] enabled = true to actually fire at runtime; the
//     capability still reports true because the binary knows how to
//     handle them. The operator gates activation via config.
type Capabilities struct {
	// Buttons reports whether DTS entries can carry an interactive
	// buttons[] array. Implies the action registry endpoint exists.
	Buttons bool `json:"buttons"`

	// Snapshots reports whether the per-delivery snapshot store
	// implementation is present. Kept separate from Buttons even
	// though one implies the other today — feature evolution may
	// split them.
	Snapshots bool `json:"snapshots"`

	// Autocreate reports whether !autocreate and the bulk-channel-sync
	// machinery are present.
	Autocreate bool `json:"autocreate"`

	// TomlDts reports whether config/dts/*.toml files are loaded and
	// re-emitted with format preserved.
	TomlDts bool `json:"tomlDts"`

	// ButtonResponseObject reports whether response_template_inline
	// accepts a JSON object (not just a string). Lets editors send
	// Form-mode authored response templates without flattening.
	ButtonResponseObject bool `json:"buttonResponseObject"`
}

// HandleHealth returns a Gin handler that responds with a small health
// payload including the binary version and a capabilities map. The
// capabilities are static for the lifetime of the process — defined
// in BuildCapabilities at handler-construction time.
func HandleHealth() gin.HandlerFunc {
	caps := BuildCapabilities()
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":       "healthy",
			"version":      Version,
			"capabilities": caps,
		})
	}
}

// BuildCapabilities returns the feature map for this binary. Every key
// here is true on the current branch — if a future binary trims a
// feature, the corresponding key flips to false (or, after a long
// deprecation, gets removed entirely and clients default-false it).
func BuildCapabilities() Capabilities {
	return Capabilities{
		Buttons:              true,
		Snapshots:            true,
		Autocreate:           true,
		TomlDts:              true,
		ButtonResponseObject: true,
	}
}
