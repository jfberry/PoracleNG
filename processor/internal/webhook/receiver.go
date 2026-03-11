package webhook

import (
	"encoding/json"
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"
)

// Handler processes incoming Golbat webhooks.
type Handler struct {
	processor     Processor
	webhookLogger io.Writer
}

// Processor processes individual webhook messages.
type Processor interface {
	ProcessPokemon(raw json.RawMessage) error
	ProcessRaid(raw json.RawMessage) error
	ProcessWeather(raw json.RawMessage) error
}

// NewHandler creates a new webhook handler.
// webhookLogger is optional — if non-nil, raw webhook bodies are written to it.
func NewHandler(processor Processor, webhookLogger io.Writer) *Handler {
	return &Handler{
		processor:     processor,
		webhookLogger: webhookLogger,
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
		// Log each individual webhook as one line: {"type":"pokemon","message":{...}}
		if h.webhookLogger != nil {
			if line, err := json.Marshal(hook); err == nil {
				h.webhookLogger.Write(line)
				h.webhookLogger.Write([]byte("\n"))
			}
		}

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
