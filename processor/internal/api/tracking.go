package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/state"
)

// TrackingDeps holds shared dependencies for all tracking CRUD handlers.
type TrackingDeps struct {
	DB           *sqlx.DB
	StateMgr     *state.Manager
	RowText      *rowtext.Generator
	Config       *config.Config
	Translations *i18n.Bundle
	AlerterURL   string // e.g. "http://localhost:3031"
	APISecret    string // for X-Poracle-Secret on alerter calls
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

// reloadDebouncer coalesces rapid state reload requests into a single reload.
// When multiple API mutations happen in quick succession (e.g. PoracleWeb adding
// 50 tracking rules), only one actual DB reload runs per debounce window.
var reloadDebouncer = struct {
	mu    sync.Mutex
	timer *time.Timer
}{}

// reloadState triggers a debounced state reload from the database.
// Multiple calls within 500ms are coalesced into a single reload.
func reloadState(deps *TrackingDeps) {
	reloadDebouncer.mu.Lock()
	defer reloadDebouncer.mu.Unlock()

	if reloadDebouncer.timer != nil {
		reloadDebouncer.timer.Stop()
	}
	reloadDebouncer.timer = time.AfterFunc(500*time.Millisecond, func() {
		if err := state.Load(deps.StateMgr, deps.DB); err != nil {
			log.Errorf("Tracking API: state reload failed: %s", err)
		}
	})
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

// diffTracking compares two tracking structs using `diff` struct tags.
//
// Tag values:
//   - diff:"-"      skip (uid, id, profile_no — always same or irrelevant)
//   - diff:"match"  match key — if different, rows aren't related (noMatch=true)
//   - diff:"update" updatable field — if ALL diffs are updatable, it's an update
//   - (no tag)      regular field — any diff here means new insert
//
// Both existing and toInsert must be pointers to the same struct type.
func diffTracking(existing, toInsert any) (noMatch, isDuplicate bool, existingUID int64, isUpdate bool) {
	ev := reflect.ValueOf(existing).Elem()
	iv := reflect.ValueOf(toInsert).Elem()
	et := ev.Type()

	var uid int64
	totalDiffs, nonUpdatableDiffs := 0, 0

	for i := 0; i < et.NumField(); i++ {
		field := et.Field(i)
		tag := field.Tag.Get("diff")

		switch tag {
		case "-":
			if field.Tag.Get("db") == "uid" {
				uid = ev.Field(i).Int()
			}
		case "match":
			if !reflect.DeepEqual(ev.Field(i).Interface(), iv.Field(i).Interface()) {
				return true, false, 0, false
			}
		case "update":
			if !reflect.DeepEqual(ev.Field(i).Interface(), iv.Field(i).Interface()) {
				totalDiffs++
			}
		default:
			if !reflect.DeepEqual(ev.Field(i).Interface(), iv.Field(i).Interface()) {
				totalDiffs++
				nonUpdatableDiffs++
			}
		}
	}

	if totalDiffs == 0 {
		return false, true, 0, false // duplicate — all fields match
	}
	if nonUpdatableDiffs == 0 {
		return false, false, uid, true // update — only updatable fields differ
	}
	return false, false, 0, false // new insert — non-updatable fields differ
}
