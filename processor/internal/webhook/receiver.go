package webhook

import (
	"encoding/json"
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/metrics"
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
	ProcessInvasion(raw json.RawMessage) error
	ProcessQuest(raw json.RawMessage) error
	ProcessLure(raw json.RawMessage) error
	ProcessGym(raw json.RawMessage) error
	ProcessNest(raw json.RawMessage) error
	ProcessFortUpdate(raw json.RawMessage) error
	ProcessMaxbattle(raw json.RawMessage) error
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

	metrics.WebhookBatchSize.Observe(float64(len(hooks)))

	for _, hook := range hooks {
		// Log each individual webhook as one line: {"type":"pokemon","message":{...}}
		if h.webhookLogger != nil {
			if line, err := json.Marshal(hook); err == nil {
				h.webhookLogger.Write(line)
				h.webhookLogger.Write([]byte("\n"))
			}
		}

		metrics.WebhooksReceived.WithLabelValues(hook.Type).Inc()
		metrics.IntervalWebhooks.Add(1)

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
		case "invasion":
			if err := h.processor.ProcessInvasion(hook.Message); err != nil {
				log.Errorf("Failed to process invasion: %s", err)
			}
		case "pokestop":
			// Pokestop webhooks may contain invasion or lure data - route based on fields
			if err := h.routePokestop(hook.Message); err != nil {
				log.Errorf("Failed to process pokestop: %s", err)
			}
		case "quest":
			if err := h.processor.ProcessQuest(hook.Message); err != nil {
				log.Errorf("Failed to process quest: %s", err)
			}
		case "gym", "gym_details":
			if err := h.processor.ProcessGym(hook.Message); err != nil {
				log.Errorf("Failed to process gym: %s", err)
			}
		case "nest":
			if err := h.processor.ProcessNest(hook.Message); err != nil {
				log.Errorf("Failed to process nest: %s", err)
			}
		case "fort_update":
			if err := h.processor.ProcessFortUpdate(hook.Message); err != nil {
				log.Errorf("Failed to process fort_update: %s", err)
			}
		case "max_battle":
			if err := h.processor.ProcessMaxbattle(hook.Message); err != nil {
				log.Errorf("Failed to process max_battle: %s", err)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// routePokestop inspects a pokestop webhook to determine if it's an invasion or lure.
func (h *Handler) routePokestop(raw json.RawMessage) error {
	// Peek at fields to determine type
	var peek struct {
		LureExpiration     int64 `json:"lure_expiration"`
		IncidentExpiration int64 `json:"incident_expiration"`
		IncidentGruntType  int   `json:"incident_grunt_type"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return err
	}

	// A pokestop can have both a lure and an invasion — process both
	if peek.LureExpiration > 0 {
		if err := h.processor.ProcessLure(raw); err != nil {
			return err
		}
	}
	if peek.IncidentExpiration > 0 || peek.IncidentGruntType > 0 {
		return h.processor.ProcessInvasion(raw)
	}

	// If neither lure nor invasion was detected, try as invasion (legacy fallback)
	if peek.LureExpiration <= 0 {
		return h.processor.ProcessInvasion(raw)
	}
	return nil
}
