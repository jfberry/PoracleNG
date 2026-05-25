package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// TrackingDeps holds shared dependencies for all tracking CRUD handlers.
type TrackingDeps struct {
	DB           *sqlx.DB
	Humans       store.HumanStore
	Tracking     *store.TrackingStores
	StateMgr     *state.Manager
	RowText      *rowtext.Generator
	Config       *config.Config
	Translations *i18n.Bundle
	Dispatcher   *delivery.Dispatcher
	AreaLogic    *bot.AreaLogic // nil-safe: area validation skipped when nil
	ReloadFunc   func()         // triggers debounced state reload (from ProcessorService.triggerReload)
}

// lookupHuman resolves the human from the {id} path parameter and the profile_no
// from the query string (falling back to the human's current_profile_no).
// Returns (nil, 0, nil) if the human is not found — caller should return an error response.
func lookupHuman(deps *TrackingDeps, c *gin.Context) (*store.HumanLite, int, error) {
	id := c.Param("id")
	if id == "" {
		return nil, 0, fmt.Errorf("missing id parameter")
	}

	human, err := deps.Humans.GetLite(id)
	if err != nil {
		return nil, 0, fmt.Errorf("lookup human: %w", err)
	}
	if human == nil {
		return nil, 0, nil
	}

	profileNo := human.CurrentProfileNo
	if pq := c.Query("profile_no"); pq != "" {
		if n, err := strconv.Atoi(pq); err == nil {
			profileNo = n
		}
	}

	return human, profileNo, nil
}

// reloadState triggers a debounced state reload via the centralized
// ProcessorService.triggerReload (shared with rate-limit disable, profile scheduler, etc.).
func reloadState(deps *TrackingDeps) {
	if deps.ReloadFunc != nil {
		deps.ReloadFunc()
	}
}

// sendConfirmation dispatches a confirmation message to the user via the delivery system.
func sendConfirmation(deps *TrackingDeps, human *store.HumanLite, message, language string) {
	if deps.Dispatcher == nil || message == "" {
		return
	}

	msgJSON, err := json.Marshal(map[string]string{"content": message})
	if err != nil {
		log.Errorf("Tracking API: marshal confirmation: %s", err)
		return
	}

	deps.Dispatcher.Dispatch(&delivery.Job{
		Target:       human.ID,
		Type:         human.Type,
		Name:         human.Name,
		Message:      msgJSON,
		TTH:          delivery.TTH{Hours: 1},
		LogReference: "WebApi",
	})
}

// isSilent returns true if the request has a silent or suppressMessage query param.
func isSilent(c *gin.Context) bool {
	return c.Query("silent") != "" || c.Query("suppressMessage") != ""
}

// trackingJSONOK writes a JSON response with status "ok" and any additional fields.
func trackingJSONOK(c *gin.Context, data map[string]any) {
	if data == nil {
		data = make(map[string]any)
	}
	data["status"] = "ok"
	c.JSON(http.StatusOK, data)
}

// trackingJSONError writes a JSON error response.
func trackingJSONError(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{
		"status":  "error",
		"message": message,
	})
}

// resolveLanguage returns the human's language or the configured default locale.
func resolveLanguage(deps *TrackingDeps, human *store.HumanLite) string {
	return human.LanguageOrDefault(deps.Config.General.Locale)
}

// translatorFor returns the translator for the given human's language.
func translatorFor(deps *TrackingDeps, human *store.HumanLite) *i18n.Translator {
	lang := resolveLanguage(deps, human)
	return deps.Translations.For(lang)
}

// readBody reads the raw request body from a gin context.
func readBody(c *gin.Context) ([]byte, error) {
	data, err := c.GetRawData()
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty request body")
	}
	return data, nil
}

// flexBool is a JSON type that accepts booleans (true/false) and numbers (0/1),
// coercing both to an integer value. This handles third-party clients like
// ReactMap that send boolean values where Poracle expects 0/1 integers.
type flexBool struct {
	value *int
}

func (f *flexBool) UnmarshalJSON(data []byte) error {
	s := string(data)
	switch s {
	case "null":
		f.value = nil
		return nil
	case "true":
		v := 1
		f.value = &v
		return nil
	case "false":
		v := 0
		f.value = &v
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		i, _ := strconv.Atoi(n.String())
		f.value = &i
		return nil
	}
	return fmt.Errorf("flexBool: cannot unmarshal %s", s)
}

func (f flexBool) intValue(defaultVal int) int {
	if f.value == nil {
		return defaultVal
	}
	return *f.value
}

func (f flexBool) isSet() bool {
	return f.value != nil
}

// flexInt is a JSON type that accepts numbers, booleans, and strings,
// coercing all to an integer value. Handles ReactMap sending booleans
// for fields like slot_changes, battle_changes.
type flexInt struct {
	value *int
}

func (f *flexInt) UnmarshalJSON(data []byte) error {
	s := string(data)
	switch s {
	case "null":
		f.value = nil
		return nil
	case "true":
		v := 1
		f.value = &v
		return nil
	case "false":
		v := 0
		f.value = &v
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		i, _ := strconv.Atoi(n.String())
		f.value = &i
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		i, _ := strconv.Atoi(str)
		f.value = &i
		return nil
	}
	return fmt.Errorf("flexInt: cannot unmarshal %s", s)
}

func (f flexInt) intValue(defaultVal int) int {
	if f.value == nil {
		return defaultVal
	}
	return *f.value
}

func (f flexInt) isSet() bool {
	return f.value != nil
}

// overrideContext holds per-target data pre-fetched once above the per-row
// validation loop so that batch POSTs of N rules don't issue N×Get queries.
// Build it with newOverrideContext before the loop, then pass it into
// validateOverrideFields for every row.
type overrideContext struct {
	human     *store.Human    // nil when AreaLogic is nil (no permission check needed)
	permitted map[string]bool // lowercase area set; nil when area security disabled
}

// newOverrideContext fetches the full human record once and pre-builds the
// permitted-area set. It is called once per handler invocation, outside the
// per-row loop, so that a batch POST of N rows only issues one Get query.
// Returns an error message + HTTP status on failure.
func newOverrideContext(deps *TrackingDeps, humanID string) (overrideContext, string, int) {
	if deps.AreaLogic == nil {
		return overrideContext{}, "", 0
	}
	human, err := deps.Humans.Get(humanID)
	if err != nil {
		return overrideContext{}, "database error", http.StatusInternalServerError
	}
	if human == nil {
		return overrideContext{}, "user not found", http.StatusNotFound
	}
	admin := isAdmin(deps, humanID)
	available := deps.AreaLogic.GetAvailableAreas(human.CommunityMembership, admin)
	permitted := make(map[string]bool, len(available))
	for _, a := range available {
		permitted[strings.ToLower(a.Name)] = true
	}
	return overrideContext{human: human, permitted: permitted}, "", 0
}

// validateOverrideFields checks the four mutually-exclusive override
// rules from the spec, mirroring the bot-side parseOverride logic.
// Returns ("", 0) when valid; returns (errorMsg, httpStatus) otherwise.
//
// The four rules are:
//  1. location: AND area: → mutually exclusive
//  2. area: AND distance > 0 → mutually exclusive
//  3. location: AND distance == 0 → location requires distance
//  4. Otherwise valid — validate label exists and each area is permitted.
//
// oc is the per-target context built once by newOverrideContext before the
// per-row loop. Passing the same oc for every row in a batch avoids
// re-fetching the human and rebuilding the permitted-area set per row.
func validateOverrideFields(
	deps *TrackingDeps,
	oc overrideContext,
	humanID string,
	overrideLabel string,
	overrideAreas []string,
	distance int,
) (string, int) {
	hasLocation := overrideLabel != ""
	hasAreas := len(overrideAreas) > 0

	// Rule 1: location: and area: are mutually exclusive.
	if hasLocation && hasAreas {
		return "override_location_label and override_areas are mutually exclusive", http.StatusBadRequest
	}
	// Rule 2: area: and distance > 0 are mutually exclusive.
	if hasAreas && distance > 0 {
		return "override_areas and distance are mutually exclusive", http.StatusBadRequest
	}
	// Rule 3: location: requires distance > 0.
	if hasLocation && distance == 0 {
		return "override_location_label requires distance > 0", http.StatusBadRequest
	}

	if hasLocation && deps.Humans != nil {
		loc, err := deps.Humans.GetLocation(humanID, overrideLabel)
		if err != nil {
			return "database error", http.StatusInternalServerError
		}
		if loc == nil {
			return "unknown location label: " + overrideLabel, http.StatusBadRequest
		}
	}

	if hasAreas && oc.permitted != nil {
		for _, a := range overrideAreas {
			if !oc.permitted[strings.ToLower(a)] {
				return "area not permitted: " + a, http.StatusBadRequest
			}
		}
	}

	return "", 0
}

// normalizeOverrideAreas converts an empty slice to nil (so an incoming
// "override_areas":[] round-trips identically to a NULL DB column for diff
// purposes) and normalizes each remaining area name to lowercase with
// underscores replaced by spaces. This matches the convention used by
// human.Area (parseAndNormalizeAreas) and the geofence NormalizedName keys,
// so area comparisons in the matcher's areaOverlap will always find a match
// regardless of the original case or underscore usage in the request body.
func normalizeOverrideAreas(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, a := range in {
		out[i] = strings.ToLower(strings.ReplaceAll(a, "_", " "))
	}
	return out
}

// DiffTracking compares two tracking structs using `diff` struct tags.
// This is a convenience wrapper around db.DiffTracking for use within the api package
// and by external callers that import api.
func DiffTracking(existing, toInsert any) (noMatch, isDuplicate bool, existingUID int64, isUpdate bool) {
	return db.DiffTracking(existing, toInsert)
}
