package api

import (
	"encoding/json"
)

// EnrichService is the interface the enrich handler needs from ProcessorService.
// This avoids importing the main package.
type EnrichService interface {
	EnrichWebhook(webhookType string, raw json.RawMessage, language, platform string) (map[string]any, error)
}
