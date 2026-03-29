package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	raymond "github.com/mailgun/raymond/v2"

	"github.com/pokemon/poracleng/processor/internal/dts"
)

// HandleTemplateConfig returns DTS template metadata for PoracleWeb.
// GET /api/config/templates
// Optional query parameter: ?includeDescriptions=true adds name/description fields.
func HandleTemplateConfig(ts *dts.TemplateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		includeDescriptions := r.URL.Query().Get("includeDescriptions") == "true"
		metadata := ts.TemplateMetadata(includeDescriptions)

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"status": "ok"}
		for k, v := range metadata {
			resp[k] = v
		}
		json.NewEncoder(w).Encode(resp)
	}
}

// HandleDTSRender renders a DTS template on demand.
// POST /api/dts/render
//
// Request body:
//
//	{"type": "help", "id": "track", "platform": "discord", "language": "en", "view": {"prefix": "!"}}
//
// Response:
//
//	{"status": "ok", "message": {...rendered template object...}}
func HandleDTSRender(ts *dts.TemplateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Type     string         `json:"type"`
			ID       string         `json:"id"`
			Platform string         `json:"platform"`
			Language string         `json:"language"`
			View     map[string]any `json:"view"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "invalid request body: " + err.Error()})
			return
		}

		tmpl := ts.Get(req.Type, req.Platform, req.ID, req.Language)
		if tmpl == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "error",
				"error":  fmt.Sprintf("no template found for %s/%s/%s/%s", req.Type, req.Platform, req.ID, req.Language),
			})
			return
		}

		view := req.View
		if view == nil {
			view = make(map[string]any)
		}

		df := raymond.NewDataFrame()
		df.Set("language", req.Language)
		df.Set("platform", req.Platform)

		rendered, err := tmpl.ExecWith(view, df)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "template render failed: " + err.Error()})
			return
		}

		// Parse the rendered JSON string into an object
		var message any
		if err := json.Unmarshal([]byte(rendered), &message); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "rendered template is not valid JSON: " + err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": message})
	}
}
