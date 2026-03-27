package api

import (
	"encoding/json"
	"net/http"

	"github.com/pokemon/poracleng/processor/internal/geocoding"
)

// HandleGeocode returns a handler for GET /api/geocode/forward?q=QUERY.
// It performs a forward geocode lookup and returns the results as JSON.
func HandleGeocode(geocoder *geocoding.Geocoder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query().Get("q")
		if query == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "q parameter required"})
			return
		}

		if geocoder == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "geocoder not configured"})
			return
		}

		results, err := geocoder.Forward(query)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}
