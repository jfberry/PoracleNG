package api

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"
)

// ReloadFunc is called when a reload is requested.
type ReloadFunc func() error

// Handler provides API endpoints for the processor.
type Handler struct {
	reloadFn ReloadFunc
}

// NewHandler creates a new API handler.
func NewHandler(reloadFn ReloadFunc) *Handler {
	return &Handler{
		reloadFn: reloadFn,
	}
}

// RegisterRoutes adds API routes to the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/reload", h.handleReload)
	mux.HandleFunc("/health", h.handleHealth)
}

func (h *Handler) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	log.Infof("Reload requested via API")
	if err := h.reloadFn(); err != nil {
		log.Errorf("Reload failed: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}
