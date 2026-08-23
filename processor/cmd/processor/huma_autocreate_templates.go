package main

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/discordbot"
)

// openJSON is a freeform JSON request body. It preserves the raw bytes so the
// handler can unmarshal them with the exact same logic the legacy gin handler
// used. It implements huma.SchemaProvider to emit an OPEN schema (accept
// anything), because the channel-template endpoints take polymorphic /
// dynamically-shaped payloads that huma's default schema generation would
// otherwise reject. Mirrors api.openJSON (unexported there).
type openJSON json.RawMessage

// UnmarshalJSON captures the raw bytes verbatim.
func (o *openJSON) UnmarshalJSON(data []byte) error {
	*o = append((*o)[:0], data...)
	return nil
}

// MarshalJSON returns the captured bytes (or null when empty).
func (o openJSON) MarshalJSON() ([]byte, error) {
	if len(o) == 0 {
		return []byte("null"), nil
	}
	return o, nil
}

// Schema returns an empty (fully open) schema so huma accepts any JSON value.
func (openJSON) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{}
}

// templatesPostInput carries the freeform channel-templates body. The whole
// body is open: it is shaped {"templates":[...]} but the templates array is
// polymorphic, so the gin handler unmarshalled it raw and we do the same.
type templatesPostInput struct {
	Body openJSON
}

// registerAutocreateTemplatesHuma registers the three channel-template ops on
// the shared huma instance, in place — same paths, same success JSON as the
// legacy gin handlers; error paths surface as problem+json. Bodies are open
// (raw JSON) so unknown fields survive a write through the API.
func registerAutocreateTemplatesHuma(humaAPI huma.API, cfg *config.Config) {
	// GET /api/autocreate/templates — live channelTemplate.json contents.
	huma.Register(humaAPI, huma.Operation{
		OperationID: "get-autocreate-templates", Method: "GET", Path: "/autocreate/templates",
		Summary: "Get channel templates", Tags: []string{"autocreate"},
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*autocreateBodyOutput, error) {
		raw, err := discordbot.LoadChannelTemplatesRaw(cfg.BaseDir)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		// Round-trip via json.RawMessage so the response carries a real JSON
		// array (not a base64-encoded string) regardless of the marshaller.
		var templates any
		if err := json.Unmarshal(raw, &templates); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return &autocreateBodyOutput{Body: map[string]any{"status": "ok", "templates": templates}}, nil
	})

	// POST /api/autocreate/templates — validate + write. On success returns
	// the backup filename so the operator can roll back.
	huma.Register(humaAPI, huma.Operation{
		OperationID: "post-autocreate-templates", Method: "POST", Path: "/autocreate/templates",
		Summary: "Save channel templates", Tags: []string{"autocreate"},
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *templatesPostInput) (*autocreateBodyOutput, error) {
		raw, err := extractTemplatesRaw(in.Body)
		if err != nil {
			return nil, err
		}
		if errs := discordbot.ValidateChannelTemplatesRaw(raw); hasBlockingErrors(errs) {
			return nil, blockingValidationError(errs)
		}
		backup, err := discordbot.SaveChannelTemplatesRaw(cfg.BaseDir, raw)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return &autocreateBodyOutput{Body: map[string]any{
			"status":   "ok",
			"backup":   backup,
			"warnings": nonBlocking(discordbot.ValidateChannelTemplatesRaw(raw)),
		}}, nil
	})

	// POST /api/autocreate/templates/validate — dry-run validation, never writes.
	huma.Register(humaAPI, huma.Operation{
		OperationID: "post-autocreate-templates-validate", Method: "POST", Path: "/autocreate/templates/validate",
		Summary: "Validate channel templates", Tags: []string{"autocreate"},
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *templatesPostInput) (*autocreateBodyOutput, error) {
		raw, err := extractTemplatesRaw(in.Body)
		if err != nil {
			return nil, err
		}
		errs := discordbot.ValidateChannelTemplatesRaw(raw)
		if hasBlockingErrors(errs) {
			return nil, blockingValidationError(errs)
		}
		return &autocreateBodyOutput{Body: map[string]any{"status": "ok", "warnings": nonBlocking(errs)}}, nil
	})
}

// extractTemplatesRaw parses the {"templates":[...]} envelope from an open body
// and returns the raw templates JSON. Mirrors the legacy readTemplatesBody:
// parse errors and a missing templates field surface as problem+json 400s.
func extractTemplatesRaw(body openJSON) ([]byte, error) {
	var req struct {
		Templates json.RawMessage `json:"templates"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, huma.Error400BadRequest("parse body: " + err.Error())
	}
	if len(req.Templates) == 0 {
		return nil, huma.Error400BadRequest("templates field is required")
	}
	return []byte(req.Templates), nil
}

// blockingValidationError builds a problem+json 400 whose Errors detail carries
// the per-template validation errors, preserving the legacy {"errors":[...]}
// information in the structured huma error response.
func blockingValidationError(errs []discordbot.TemplateValidationError) error {
	details := make([]error, 0, len(errs))
	for _, e := range errs {
		details = append(details, &huma.ErrorDetail{Message: e.Message})
	}
	return huma.Error400BadRequest("channel template validation failed", details...)
}
