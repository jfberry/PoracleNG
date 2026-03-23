package api

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"
)

// RequireSecret returns middleware that checks the x-poracle-secret header.
// If secret is empty, all requests are allowed (no auth configured).
func RequireSecret(secret string, next http.HandlerFunc) http.HandlerFunc {
	if secret == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Poracle-Secret") != secret {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"status": "authError", "reason": "incorrect or missing api secret"})
			return
		}
		next(w, r)
	}
}

// HandleReload returns a handler that triggers a state reload.
func HandleReload(reloadFn func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		log.Infof("Reload requested via API")
		if err := reloadFn(); err != nil {
			log.Errorf("Reload failed: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// WeatherExporter returns weather data for a specific cell.
type WeatherExporter interface {
	ExportCellWeather(cellID string) map[int64]int
}

// HandleWeather returns a handler that serves weather data for a cell.
func HandleWeather(weather WeatherExporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		cellID := r.URL.Query().Get("cell")
		if cellID == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "cell parameter required"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(weather.ExportCellWeather(cellID))
	}
}

// HandleStats returns a handler that serves the result of a stats function.
func HandleStats(fn func() any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fn())
	}
}

// HandleHealth returns a simple health check handler.
func HandleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	}
}
