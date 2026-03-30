package api

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/delivery"
)

// HandleDeliverMessages returns a handler that accepts pre-rendered delivery jobs
// and dispatches them to the delivery system. This is used by the alerter's
// broadcast command and other command-generated messages.
func HandleDeliverMessages(dispatcher *delivery.Dispatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if dispatcher == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": "delivery dispatcher not configured",
			})
			return
		}

		var jobs []delivery.Job
		if err := json.NewDecoder(r.Body).Decode(&jobs); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": "invalid JSON: " + err.Error(),
			})
			return
		}

		queued := 0
		for i := range jobs {
			if jobs[i].Target == "" || jobs[i].Type == "" {
				continue
			}
			dispatcher.Dispatch(&jobs[i])
			queued++
		}

		log.Debugf("Accepted %d delivery jobs via API", queued)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"queued": queued,
		})
	}
}
