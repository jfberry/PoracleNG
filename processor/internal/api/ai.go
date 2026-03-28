package api

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/ai"
)

type aiRequest struct {
	Message string `json:"message"`
}

type aiResponse struct {
	Status  string `json:"status"`
	Command string `json:"command,omitempty"`
	Error   string `json:"error,omitempty"`
}

// HandleAI returns a handler for POST /api/ai/translate.
func HandleAI(client *ai.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if client == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(aiResponse{
				Status: "error",
				Error:  "AI assistant is not configured. Set [ai] enabled=true in config.toml.",
			})
			return
		}

		var req aiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(aiResponse{Status: "error", Error: "invalid request"})
			return
		}

		if req.Message == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(aiResponse{Status: "error", Error: "message is required"})
			return
		}

		log.Infof("[AI] Translating: %q", req.Message)

		command, err := client.TranslateCommand(req.Message)
		if err != nil {
			log.Errorf("[AI] Translation failed: %s", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(aiResponse{Status: "error", Error: err.Error()})
			return
		}

		log.Infof("[AI] Result: %q", command)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(aiResponse{Status: "ok", Command: command})
	}
}
