package api

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/ai"
	"github.com/pokemon/poracleng/processor/internal/nlp"
)

type aiRequest struct {
	Message string `json:"message"`
}

type aiResponse struct {
	Status  string     `json:"status"`
	Command string     `json:"command,omitempty"`
	Error   string     `json:"error,omitempty"`
	Message string     `json:"message,omitempty"`
	Options []aiOption `json:"options,omitempty"`
}

type aiOption struct {
	Label   string `json:"label"`
	Command string `json:"command"`
}

// HandleAI returns a handler for POST /api/ai/translate.
// It tries the NLP parser first. If NLP returns "ok" or "ambiguous", that result
// is returned immediately. If NLP fails and fallbackEnabled is true with a
// configured AI client, the AI model is used as a fallback.
func HandleAI(parser *nlp.Parser, aiClient *ai.Client, fallbackEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if parser == nil && aiClient == nil {
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

		// Try NLP parser first
		if parser != nil {
			log.Infof("[NLP] Translating: %q", req.Message)
			result := parser.Parse(req.Message)

			if result.Status == "ok" || result.Status == "ambiguous" {
				log.Infof("[NLP] Result (%s): %q", result.Status, result.Command)
				resp := aiResponse{
					Status:  result.Status,
					Command: result.Command,
					Message: result.Message,
				}
				for _, opt := range result.Options {
					resp.Options = append(resp.Options, aiOption{
						Label:   opt.Label,
						Command: opt.Command,
					})
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
				return
			}

			// NLP failed — fall through to AI if enabled
			log.Infof("[NLP] Parse failed: %s", result.Error)
		}

		// AI fallback
		if !fallbackEnabled || aiClient == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(aiResponse{
				Status: "error",
				Error:  "could not parse command",
			})
			return
		}

		log.Infof("[AI] Fallback translating: %q", req.Message)

		command, err := aiClient.TranslateCommand(req.Message)
		if err != nil {
			log.Errorf("[AI] Translation failed: %s", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(aiResponse{Status: "error", Error: err.Error()})
			return
		}

		log.Infof("[AI] Fallback result: %q", command)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(aiResponse{Status: "ok", Command: command})
	}
}
