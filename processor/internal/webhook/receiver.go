package webhook

import (
	"encoding/json"
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"
)

// Handler processes incoming Golbat webhooks.
type Handler struct {
	processor Processor
}

// Processor processes individual webhook messages.
type Processor interface {
	ProcessPokemon(raw json.RawMessage) error
	ProcessRaid(raw json.RawMessage) error
	ProcessWeather(raw json.RawMessage) error
}

// NewHandler creates a new webhook handler.
func NewHandler(processor Processor) *Handler {
	return &Handler{
		processor: processor,
	}
}

// ServeHTTP handles POST / with Golbat webhook payload.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Errorf("Failed to read webhook body: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var hooks []InboundWebhook
	if err := json.Unmarshal(body, &hooks); err != nil {
		log.Errorf("Failed to parse webhooks: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	for _, hook := range hooks {
		switch hook.Type {
		case "pokemon":
			if err := h.processor.ProcessPokemon(hook.Message); err != nil {
				log.Errorf("Failed to process pokemon: %s", err)
			}
		case "raid":
			if err := h.processor.ProcessRaid(hook.Message); err != nil {
				log.Errorf("Failed to process raid: %s", err)
			}
		case "weather":
			if err := h.processor.ProcessWeather(hook.Message); err != nil {
				log.Errorf("Failed to process weather: %s", err)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}
