package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// TrackingDeps holds shared dependencies for all tracking CRUD handlers.
type TrackingDeps struct {
	DB           *sqlx.DB
	Tracking     *store.TrackingStores
	StateMgr     *state.Manager
	RowText      *rowtext.Generator
	Config       *config.Config
	Translations *i18n.Bundle
	AlerterURL   string // e.g. "http://localhost:3031"
	APISecret    string // for X-Poracle-Secret on alerter calls
	ReloadFunc   func() // triggers debounced state reload (from ProcessorService.triggerReload)
}

// lookupHuman resolves the human from the {id} path parameter and the profile_no
// from the query string (falling back to the human's current_profile_no).
// Returns (nil, 0, nil) if the human is not found — caller should return an error response.
func lookupHuman(deps *TrackingDeps, r *http.Request) (*db.HumanAPI, int, error) {
	id := r.PathValue("id")
	if id == "" {
		return nil, 0, fmt.Errorf("missing id parameter")
	}

	human, err := db.SelectOneHuman(deps.DB, id)
	if err != nil {
		return nil, 0, fmt.Errorf("lookup human: %w", err)
	}
	if human == nil {
		return nil, 0, nil
	}

	profileNo := human.CurrentProfileNo
	if pq := r.URL.Query().Get("profile_no"); pq != "" {
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

// sendConfirmation posts a message to the alerter's /api/postMessage endpoint.
// It is non-blocking: the POST happens in a goroutine and errors are logged.
func sendConfirmation(deps *TrackingDeps, human *db.HumanAPI, message, language string) {
	if deps.AlerterURL == "" || message == "" {
		return
	}

	payload := []map[string]any{
		{
			"lat":     0,
			"lon":     0,
			"message": map[string]string{"content": message},
			"target":  human.ID,
			"type":    human.Type,
			"name":    human.Name,
			"tth":     map[string]int{"hours": 1, "minutes": 0, "seconds": 0},
			"clean":   false,
			"emoji":   "",
			"logReference": "WebApi",
			"language":     language,
		},
	}

	go func() {
		body, err := json.Marshal(payload)
		if err != nil {
			log.Errorf("Tracking API: marshal confirmation: %s", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		url := deps.AlerterURL + "/api/postMessage"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			log.Errorf("Tracking API: create confirmation request: %s", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if deps.APISecret != "" {
			req.Header.Set("X-Poracle-Secret", deps.APISecret)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Errorf("Tracking API: send confirmation to alerter: %s", err)
			return
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)

		if resp.StatusCode >= 300 {
			log.Warnf("Tracking API: alerter postMessage returned %d", resp.StatusCode)
		}
	}()
}

// readJSONBody decodes the JSON request body into v.
func readJSONBody(r *http.Request, v any) error {
	if r.Body == nil {
		return fmt.Errorf("empty request body")
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode JSON body: %w", err)
	}
	return nil
}

// isSilent returns true if the request has a silent or suppressMessage query param.
func isSilent(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("silent") != "" || q.Get("suppressMessage") != ""
}

// trackingJSONOK writes a JSON response with status "ok" and any additional fields.
func trackingJSONOK(w http.ResponseWriter, data map[string]any) {
	if data == nil {
		data = make(map[string]any)
	}
	data["status"] = "ok"
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// trackingJSONError writes a JSON error response.
func trackingJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"message": message,
	})
}

// resolveLanguage returns the human's language or the configured default locale.
func resolveLanguage(deps *TrackingDeps, human *db.HumanAPI) string {
	return human.LanguageOrDefault(deps.Config.General.Locale)
}

// translatorFor returns the translator for the given human's language.
func translatorFor(deps *TrackingDeps, human *db.HumanAPI) *i18n.Translator {
	lang := resolveLanguage(deps, human)
	return deps.Translations.For(lang)
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

// DiffTracking compares two tracking structs using `diff` struct tags.
// This is a convenience wrapper around db.DiffTracking for use within the api package
// and by external callers that import api.
func DiffTracking(existing, toInsert any) (noMatch, isDuplicate bool, existingUID int64, isUpdate bool) {
	return db.DiffTracking(existing, toInsert)
}
