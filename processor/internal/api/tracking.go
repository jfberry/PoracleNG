package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/mute"
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
	AreaLogic    *bot.AreaLogic             // nil-safe: area validation skipped when nil
	Summaries    store.SummaryScheduleStore // nil-safe: snapshot summaries omitted when nil
	Mutes        *mute.Store                // in-memory mute store (v2 mutes endpoints + snapshot mutes; nil-safe in snapshot, required by RegisterV2Mutes)
	ReloadFunc   func()                     // triggers debounced state reload (from ProcessorService.triggerReload)

	// v2SnapshotProviders is the per-instance registry of type-erased snapshot
	// providers, appended by registerV2Tracking when each type registers. The
	// full-snapshot endpoint (GET /v2/humans/{id}/tracking) iterates it to build
	// the "tracking" map. Instance-scoped (not a package global): a freshly built
	// TrackingDeps starts empty, so tests never leak providers across instances.
	v2SnapshotProviders []v2SnapshotProvider
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
	// Diagnostic: log the exact JSON every tracking mutation receives. This is
	// the chokepoint for all tracking POST handlers (gym, raid, monster, ...),
	// so enabling debug logging captures the raw payloads third-party clients
	// like ReactMap send — useful for diffing what the UI sends against what the
	// handler parses (e.g. a field sent under an unexpected key). Grep "tracking
	// POST body". Debug level: off in normal operation.
	log.Debugf("tracking POST body: %s %s %s", c.Request.Method, c.Request.URL.Path, string(data))
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
	human *store.Human
	// permitted is the set of areas this human may scope a rule to, keyed by
	// geofence.NormalizeAreaKey (lowercased, underscores as spaces) so either
	// spelling of a fence name is judged the same, whichever form is stored.
	//
	// Nil means "could not determine" — no fences loaded — and the check is
	// skipped rather than rejecting everything.
	permitted map[string]bool
	// limitExempt lifts [tracking] max_distance and max_areas: the human is
	// an admin or not a user (bot.LimitExempt), or a trusted v2 caller has
	// already authorised the write.
	limitExempt bool
}

// normalizeAreaKey keys an area name for permission comparison
// (geofence.NormalizeAreaKey).
func normalizeAreaKey(s string) string {
	return geofence.NormalizeAreaKey(s)
}

// newOverrideContext resolves the human and the set of areas they may scope a
// rule to. It is called once per handler invocation, outside the per-row loop,
// so that a batch POST of N rows only issues one Get query. Returns an error
// message + HTTP status on failure.
//
// The permitted set used to come from AreaLogic alone, and AreaLogic is nil in
// production (main.go never sets it), so the set was always nil and the
// override_areas check never ran on any tracking write — v1 or v2, all 11
// types. Falling back to the live state's fences is what makes it real; this
// mirrors what v2_mutes.go already does for area-scoped mutes (#211).
func newOverrideContext(deps *TrackingDeps, humanID string, trusted bool) (overrideContext, string, int) {
	if deps.Humans == nil {
		return overrideContext{limitExempt: trusted}, "", 0
	}
	human, err := deps.Humans.Get(humanID)
	if err != nil {
		return overrideContext{}, "database error", http.StatusInternalServerError
	}
	if human == nil {
		return overrideContext{}, "user not found", http.StatusNotFound
	}
	admin := isAdmin(deps, humanID)
	exempt := trusted || bot.LimitExempt(admin, human.Type)

	var fences []geofence.Fence
	switch {
	case deps.AreaLogic != nil:
		fences = deps.AreaLogic.Fences()
	case deps.StateMgr != nil && deps.StateMgr.Get() != nil:
		// Production path: AreaLogic is nil in main.go (#211).
		fences = deps.StateMgr.Get().Fences
	case deps.StateMgr != nil:
		// State not loaded yet: fail closed (empty set rejects every
		// override_areas value) rather than skipping the check.
		return overrideContext{human: human, permitted: map[string]bool{}, limitExempt: exempt}, "", 0
	default:
		return overrideContext{human: human, limitExempt: exempt}, "", 0
	}
	names := bot.PermittedAreas(fences, deps.Config, human, admin, trusted)

	permitted := make(map[string]bool, len(names))
	for _, n := range names {
		permitted[normalizeAreaKey(n)] = true
	}
	return overrideContext{human: human, permitted: permitted, limitExempt: exempt}, "", 0
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
			if !oc.permitted[normalizeAreaKey(a)] {
				return "area not permitted: " + a, http.StatusBadRequest
			}
		}
	}

	return "", 0
}

// overrideLimitMsg applies [tracking] max_areas to a tracking write's
// override_areas, judged on the diff it is about to apply
// (bot.OverrideAreaLimitBreached): new rules must be within the limit, an
// updated rule may only keep a subset of its stored list. Call it before
// writing, with the rows the write diffs. "" means the write may go ahead.
func overrideLimitMsg[T any](deps *TrackingDeps, oc overrideContext, existing, candidates []T,
	getUID func(*T) int64, setUID func(*T, int64)) string {
	if !bot.OverrideAreaLimitBreached(deps.Config, oc.limitExempt, existing, candidates, getUID, setUID) {
		return ""
	}
	return fmt.Sprintf("override_areas has more than the maximum of %d areas", deps.Config.Tracking.MaxAreas)
}

// earthCircumference (metres) caps distance when the operator sets no
// [tracking] max_distance (or the write is exempt) — no radius can usefully
// exceed it.
const earthCircumference = 40000000

// clampDistance caps a rule's radius at [tracking] max_distance, or at the
// earth's circumference when there is no limit or exempt (oc.limitExempt) is
// set, as the bot's enforceDistance does. Clamped rather than rejected so a
// stored rule above a since-lowered limit still round-trips through GET →
// POST/PUT instead of failing the whole body. Used by v1 and v2 tracking
// writes.
func clampDistance(deps *TrackingDeps, exempt bool, distance int) int {
	limit := earthCircumference
	if deps != nil {
		if m := bot.MaxDistanceFor(deps.Config, exempt); m > 0 {
			limit = m
		}
	}
	return min(distance, limit)
}

// v1NormalizeOverrideAreas is the frozen v1 write form: an empty slice becomes
// nil (so "override_areas":[] round-trips identically to a NULL column for the
// diff) and each name becomes its comparison key (lowercase, underscores as
// spaces). Only the v1 tracking<Type>.go handlers use it; v2 and the bot store
// the fence's own spelling (canonicalOverrideAreas). Either form matches,
// because the matcher state loaders key what they load.
func v1NormalizeOverrideAreas(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, a := range in {
		out[i] = geofence.NormalizeAreaKey(a)
	}
	return out
}

// trackingFences returns the loaded fences used to resolve area names: the
// AreaLogic's when set (tests), else the live state's (production, #211).
func trackingFences(deps *TrackingDeps) []geofence.Fence {
	if deps.AreaLogic != nil {
		return deps.AreaLogic.Fences()
	}
	if deps.StateMgr != nil {
		if st := deps.StateMgr.Get(); st != nil {
			return st.Fences
		}
	}
	return nil
}

// canonicalOverrideAreas is the v2 write form of override_areas: each name
// becomes the loaded fence's own spelling (geofence.CanonicalAreaName), and
// names that are the same area under another spelling are dropped, keeping
// first-seen order. Empty input gives nil, the NULL column.
func canonicalOverrideAreas(deps *TrackingDeps, in []string) []string {
	if len(in) == 0 {
		return nil
	}
	fences := trackingFences(deps)
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, a := range in {
		key := geofence.NormalizeAreaKey(a)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, geofence.CanonicalAreaName(fences, a))
	}
	return out
}

// canonicalizeRowOverrideAreas rewrites row's OverrideAreas field (if it has
// one) to the canonical fence spelling, so v2 reads show fence names even for
// legacy rows stored spaced. The SQL read path (parseOverrideAreas) returns
// the spaced key form, which v1, !tracked and rowtext keep using.
func canonicalizeRowOverrideAreas[T any](deps *TrackingDeps, row *T) {
	f := reflect.ValueOf(row).Elem().FieldByName("OverrideAreas")
	if !f.IsValid() || !f.CanSet() {
		return
	}
	areas, ok := f.Interface().([]string)
	if !ok || len(areas) == 0 {
		return
	}
	f.Set(reflect.ValueOf(canonicalOverrideAreas(deps, areas)))
}

// DiffTracking compares two tracking structs using `diff` struct tags.
// This is a convenience wrapper around db.DiffTracking for use within the api package
// and by external callers that import api.
func DiffTracking(existing, toInsert any) (noMatch, isDuplicate bool, existingUID int64, isUpdate bool) {
	return db.DiffTracking(existing, toInsert)
}
