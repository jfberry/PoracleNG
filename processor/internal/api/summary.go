package api

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// SummaryDeps groups the dependencies for the /api/summaries endpoints.
// Kept narrow on purpose so tests can swap in mocks without dragging the
// full TrackingDeps surface in.
type SummaryDeps struct {
	// Schedules backs Get/Set/Delete/ListByType. nil disables CRUD —
	// handlers respond 503 so callers know the feature is off rather
	// than 404 (which would be misleading).
	Schedules store.SummaryScheduleStore
	// Dispatch is invoked synchronously by the trigger endpoint to flush
	// the buffer for (humanID, alertType). nil disables the trigger
	// endpoint with a 503.
	Dispatch func(humanID, alertType string)
	// ReloadFunc is called after Set / Delete so an in-flight scheduler
	// tick picks up the change without waiting for the next periodic
	// reload. nil is tolerated.
	ReloadFunc func()
}

// summarySetRequest is the POST body shape. We accept either a stringified
// JSON value or an arbitrary structure; both flow through json.Marshal so
// the schedule store always sees a canonical JSON-encoded string.
type summarySetRequest struct {
	ActiveHours any `json:"active_hours"`
}

// summaryScheduleResponse is the JSON shape returned by GET endpoints.
// We keep it stable independent of the SummarySchedule struct so adding
// internal fields doesn't leak through to API consumers.
type summaryScheduleResponse struct {
	ID          string          `json:"id"`
	AlertType   string          `json:"alert_type"`
	ActiveHours json.RawMessage `json:"active_hours"`
}

func toSummaryResponse(s *store.SummarySchedule) summaryScheduleResponse {
	hours := json.RawMessage(s.ActiveHours)
	if len(hours) == 0 {
		hours = json.RawMessage("[]")
	}
	return summaryScheduleResponse{
		ID:          s.ID,
		AlertType:   s.AlertType,
		ActiveHours: hours,
	}
}

// HandleSummaryListForUser returns GET /api/summaries/{id}.
//
// We don't have a "list every alert_type for one user" query on the
// store interface today; iterating known alert types covers the
// foreseeable surface (only `quest` is wired to a renderer) without
// adding a new store method. As more alert types gain summary support,
// add them to this slice.
func HandleSummaryListForUser(deps *SummaryDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.Schedules == nil {
			summaryFeatureDisabled(c)
			return
		}
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		out := make([]summaryScheduleResponse, 0)
		for _, alertType := range knownSummaryAlertTypes {
			s, err := deps.Schedules.Get(id, alertType)
			if err != nil {
				log.Errorf("Summary API: get %s/%s: %v", id, alertType, err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			if s == nil {
				continue
			}
			out = append(out, toSummaryResponse(s))
		}
		trackingJSONOK(c, map[string]any{"schedules": out})
	}
}

// HandleSummaryGet returns GET /api/summaries/{id}/{alertType}. Missing
// rows return 404 with a status payload so clients can distinguish
// "no schedule" from a transport error.
func HandleSummaryGet(deps *SummaryDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.Schedules == nil {
			summaryFeatureDisabled(c)
			return
		}
		id := c.Param("id")
		alertType := c.Param("alertType")
		if id == "" || alertType == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing path parameter")
			return
		}
		if !isKnownSummaryAlertType(alertType) {
			trackingJSONError(c, http.StatusBadRequest, "unknown alert type (currently only \"quest\" is supported for summary scheduling)")
			return
		}

		s, err := deps.Schedules.Get(id, alertType)
		if err != nil {
			log.Errorf("Summary API: get %s/%s: %v", id, alertType, err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if s == nil {
			trackingJSONError(c, http.StatusNotFound, "schedule not found")
			return
		}

		trackingJSONOK(c, map[string]any{"schedule": toSummaryResponse(s)})
	}
}

// HandleSummarySet returns POST /api/summaries/{id}/{alertType}. The
// body must contain `active_hours` either as a JSON value or pre-encoded
// string; we re-encode to ensure storage is always a canonical JSON
// string.
func HandleSummarySet(deps *SummaryDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.Schedules == nil {
			summaryFeatureDisabled(c)
			return
		}
		id := c.Param("id")
		alertType := c.Param("alertType")
		if id == "" || alertType == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing path parameter")
			return
		}
		if !isKnownSummaryAlertType(alertType) {
			trackingJSONError(c, http.StatusBadRequest, "unknown alert type (currently only \"quest\" is supported for summary scheduling)")
			return
		}

		rawBody, err := readBody(c)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, err.Error())
			return
		}

		var req summarySetRequest
		if err := json.Unmarshal(rawBody, &req); err != nil {
			trackingJSONError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.ActiveHours == nil {
			trackingJSONError(c, http.StatusBadRequest, "active_hours must be specified")
			return
		}

		var hoursJSON string
		switch v := req.ActiveHours.(type) {
		case string:
			hoursJSON = v
		default:
			b, err := json.Marshal(v)
			if err != nil {
				trackingJSONError(c, http.StatusBadRequest, "active_hours could not be serialised")
				return
			}
			hoursJSON = string(b)
		}

		// Validate the JSON shape end-to-end through the same parser the
		// scheduler uses. Without this gate a client could PUT
		// `"active_hours": "not-json"` (the string branch above just
		// stores the value verbatim), then ParseActiveHours on the next
		// state reload would fail and the user's schedule would be
		// silently dropped. ParseActiveHours treats `""` / `"[]"` / `"{}"`
		// as "no schedule" (returns nil, nil) — allow those through so
		// clients can clear a schedule by writing an empty array.
		if _, perr := db.ParseActiveHours(hoursJSON); perr != nil {
			trackingJSONError(c, http.StatusBadRequest, "active_hours is not valid JSON for an ActiveHourEntry array")
			return
		}

		if err := deps.Schedules.Set(id, alertType, hoursJSON); err != nil {
			log.Errorf("Summary API: set %s/%s: %v", id, alertType, err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if deps.ReloadFunc != nil {
			deps.ReloadFunc()
		}
		trackingJSONOK(c, nil)
	}
}

// HandleSummaryDelete returns DELETE /api/summaries/{id}/{alertType}.
// Deleting a missing row is a no-op (200 with status=ok) so idempotent
// clean-up scripts don't have to special-case 404.
func HandleSummaryDelete(deps *SummaryDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.Schedules == nil {
			summaryFeatureDisabled(c)
			return
		}
		id := c.Param("id")
		alertType := c.Param("alertType")
		if id == "" || alertType == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing path parameter")
			return
		}
		if !isKnownSummaryAlertType(alertType) {
			trackingJSONError(c, http.StatusBadRequest, "unknown alert type (currently only \"quest\" is supported for summary scheduling)")
			return
		}

		if err := deps.Schedules.Delete(id, alertType); err != nil {
			log.Errorf("Summary API: delete %s/%s: %v", id, alertType, err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if deps.ReloadFunc != nil {
			deps.ReloadFunc()
		}
		trackingJSONOK(c, nil)
	}
}

// HandleSummaryTrigger returns POST /api/summaries/{id}/{alertType}/trigger.
// Calls Dispatch synchronously so the response reflects whether the
// dispatcher was wired (it's a no-op if not).
func HandleSummaryTrigger(deps *SummaryDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.Dispatch == nil {
			summaryFeatureDisabled(c)
			return
		}
		id := c.Param("id")
		alertType := c.Param("alertType")
		if id == "" || alertType == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing path parameter")
			return
		}
		if !isKnownSummaryAlertType(alertType) {
			trackingJSONError(c, http.StatusBadRequest, "unknown alert type (currently only \"quest\" is supported for summary scheduling)")
			return
		}

		deps.Dispatch(id, alertType)
		trackingJSONOK(c, nil)
	}
}

// summaryFeatureDisabled writes a 503 with a clear payload so callers
// can distinguish "feature off" from "endpoint missing". Discoverability
// over HTTP-pure correctness here.
func summaryFeatureDisabled(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"status":  "error",
		"message": "summary feature is disabled (set tracking.quest_summary_enabled = true)",
	})
}

// knownSummaryAlertTypes lists the alert types that have a summary
// renderer wired in DispatchQuestSummary. The list-for-user endpoint
// iterates these so we don't need a new "list by id" store method.
var knownSummaryAlertTypes = []string{"quest"}

// isKnownSummaryAlertType is the membership test used by every handler
// that accepts an alertType path parameter. Returning a 400 for unknown
// values lets clients building tooling get a clear signal instead of a
// silent 200-with-no-effect (DispatchQuestSummary itself no-ops on
// unknown alert types).
func isKnownSummaryAlertType(t string) bool {
	return slices.Contains(knownSummaryAlertTypes, t)
}
