package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	raymond "github.com/mailgun/raymond/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
)

// openJSON is a freeform JSON request body. It preserves the raw bytes so the
// handler can unmarshal them with the exact same logic the legacy gin handler
// used (via c.ShouldBindJSON into a local struct). It implements
// huma.SchemaProvider to emit an OPEN schema (accept anything), because these
// endpoints take polymorphic / dynamically-keyed payloads that huma's default
// schema generation would otherwise reject.
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
	// Empty schema = no constraints; accepts object, array, scalar, anything.
	return &huma.Schema{}
}

// dtsRenderReader is the minimal template-store surface the on-demand render
// endpoint needs. *dts.TemplateStore satisfies it; the interface keeps the
// Register signature testable since TemplateStore has unexported fields.
type dtsRenderReader interface {
	Get(templateType, platform, templateID, language string) *raymond.Template
}

// dtsSaveWriter is the minimal surface the save-templates endpoint needs.
type dtsSaveWriter interface {
	SaveEntry(entry dts.DTSEntry) error
}

// dtsMetadataReader is the minimal surface the config/templates metadata read
// needs.
type dtsMetadataReader interface {
	TemplateMetadata(includeDescriptions bool) map[string]any
}

// dtsSendTestReader is the minimal surface the sendtest endpoint needs from the
// template store (partials) and renderer (shortener).
type dtsSendTestReader interface {
	Partials() map[string]string
}

// dtsShortener is the minimal surface needed to resolve <S< >S> markers; a nil
// renderer is tolerated (markers left untouched).
type dtsShortener interface {
	Shortener() *dts.ShlinkShortener
}

// dtsDispatcher is the minimal delivery surface the sendtest endpoint needs.
// *delivery.Dispatcher satisfies it.
type dtsDispatcher interface {
	Dispatch(job *delivery.Job)
}

// templateConfigInput carries the optional includeDescriptions query param.
type templateConfigInput struct {
	IncludeDescriptions string `query:"includeDescriptions"`
}

// RegisterTemplateConfig registers GET /api/config/templates, returning DTS
// template metadata for PoracleWeb. Replaces gin HandleTemplateConfig. The
// response is a dynamic-keyed map merged with {"status":"ok"} (freeform body).
func RegisterTemplateConfig(api huma.API, ts dtsMetadataReader) {
	huma.Register(api, huma.Operation{
		OperationID: "get-config-templates", Method: "GET", Path: "/config/templates",
		Summary:     "DTS template list",
		Description: "Returns dynamic-keyed DTS template metadata for PoracleWeb. The response shape is freeform (open body): a {\"status\":\"ok\"} envelope merged with a per-type metadata map.",
		Tags:        []string{"dts"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *templateConfigInput) (*anyBodyOutput, error) {
		metadata := ts.TemplateMetadata(in.IncludeDescriptions == "true")
		resp := map[string]any{"status": "ok"}
		maps.Copy(resp, metadata)
		return &anyBodyOutput{Body: resp}, nil
	})
}

// dtsRenderRequest documents the render request wrapper. It is the
// documentation-only shape for dtsRenderBody.Schema() (never bound to directly —
// dtsRenderBody captures raw bytes). The outer fields are NAMED and documented;
// `view` is openJSON so its schema stays OPEN. The scalar outer fields use
// openJSON too (their intended type is described in prose) so huma's pre-bind
// validation never rejects a wrong-typed scalar before the handler runs — this
// preserves the legacy lenient contract (a type mismatch surfaces as the
// handler's 400, not a huma 422).
type dtsRenderRequest struct {
	Type     openJSON `json:"type" doc:"Template type string, e.g. monster, raid, egg, quest"`
	ID       openJSON `json:"id" doc:"Template id string (the configured template name/number)"`
	Platform openJSON `json:"platform" doc:"Target platform string: discord or telegram"`
	Language openJSON `json:"language" doc:"Locale code string, e.g. en, de"`
	View     openJSON `json:"view" doc:"Arbitrary template-variable map (open shape)"`
}

// dtsRenderBody is the POST /api/dts/render request body. It captures the raw
// bytes verbatim (like openJSON) so the handler keeps the legacy lenient parsing
// and its own required-field checks. Its Schema() documents the
// {type,id,platform,language,view} wrapper — view OPEN — while staying permissive
// (no required, additionalProperties allowed) so huma's pre-bind validation never
// rejects a partial body before the handler runs.
type dtsRenderBody json.RawMessage

func (b *dtsRenderBody) UnmarshalJSON(data []byte) error {
	*b = append((*b)[:0], data...)
	return nil
}

func (b dtsRenderBody) MarshalJSON() ([]byte, error) {
	if len(b) == 0 {
		return []byte("null"), nil
	}
	return b, nil
}

// Schema documents the render request wrapper, loosened to be permissive.
func (dtsRenderBody) Schema(r huma.Registry) *huma.Schema {
	return openObjectSchema(r, reflect.TypeFor[dtsRenderRequest](),
		"DTS render request: {type,id,platform,language,view}. `view` is an OPEN template-variable map; the template is selected by type/id/platform/language.")
}

// dtsRenderInput carries the render request.
type dtsRenderInput struct {
	Body dtsRenderBody
}

// dtsRenderResponse is the typed body for POST /api/dts/render. The outer
// envelope is named; `message` stays OPEN (the rendered template parsed back to
// any JSON value). Field order is alphabetical (message, status) to match the
// legacy map[string]any byte order.
type dtsRenderResponse struct {
	Message any    `json:"message" doc:"The rendered template parsed back to JSON (any shape)"`
	Status  string `json:"status" doc:"Always \"ok\""`
}

// dtsRenderOutput is the typed huma output for POST /api/dts/render.
type dtsRenderOutput struct {
	Body dtsRenderResponse
}

// RegisterDTSRender registers POST /api/dts/render, rendering a DTS template on
// demand. Replaces gin HandleDTSRender. The request envelope is named/documented
// ({type,id,platform,language}) with an OPEN `view` map and stays permissive
// (extras tolerated). The response `message` is the parsed rendered template (any
// JSON value).
func RegisterDTSRender(api huma.API, ts dtsRenderReader) {
	huma.Register(api, huma.Operation{
		OperationID: "post-dts-render", Method: "POST", Path: "/dts/render",
		Summary:     "Render a DTS template",
		Description: "Renders a DTS template on demand. Request envelope ({type,id,platform,language}) is typed; `view` is an OPEN template-variable map. Response `message` is the rendered template parsed back to JSON (any shape).",
		Tags:        []string{"dts"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *dtsRenderInput) (*dtsRenderOutput, error) {
		var req struct {
			Type     string         `json:"type"`
			ID       string         `json:"id"`
			Platform string         `json:"platform"`
			Language string         `json:"language"`
			View     map[string]any `json:"view"`
		}
		if err := json.Unmarshal([]byte(in.Body), &req); err != nil {
			return nil, huma.Error400BadRequest("invalid request body: " + err.Error())
		}

		tmpl := ts.Get(req.Type, req.Platform, req.ID, req.Language)
		if tmpl == nil {
			return nil, huma.Error404NotFound(fmt.Sprintf("no template found for %s/%s/%s/%s", req.Type, req.Platform, req.ID, req.Language))
		}

		view := req.View
		if view == nil {
			view = make(map[string]any)
		}

		df := raymond.NewDataFrame()
		df.Set("language", req.Language)
		df.Set("platform", req.Platform)

		rendered, err := tmpl.ExecWith(view, df)
		if err != nil {
			return nil, huma.Error500InternalServerError("template render failed: " + err.Error())
		}

		var message any
		if err := json.Unmarshal([]byte(rendered), &message); err != nil {
			return nil, huma.Error500InternalServerError("rendered template is not valid JSON: " + err.Error())
		}

		return &dtsRenderOutput{Body: dtsRenderResponse{Message: message, Status: "ok"}}, nil
	})
}

// dtsSaveInput carries the freeform save request: an array of DTSEntry whose
// `template` field is polymorphic. Kept open so any valid entry shape parses.
type dtsSaveInput struct {
	Body openJSON
}

// dtsSaveOutput is the typed body for POST /api/dts/templates: {status, saved}
// where saved is the count of entries persisted.
type dtsSaveOutput struct {
	Body struct {
		Status string `json:"status"`
		Saved  int    `json:"saved"`
	}
}

// RegisterDTSSaveTemplates registers POST /api/dts/templates, saving an array
// of DTS entries. Replaces gin HandleDTSSaveTemplates. The request body is open
// ([]DTSEntry with a polymorphic `template` value). Each entry is saved to its
// own file; readonly entries are rejected (403).
func RegisterDTSSaveTemplates(api huma.API, ts dtsSaveWriter) {
	huma.Register(api, huma.Operation{
		OperationID: "post-dts-templates", Method: "POST", Path: "/dts/templates",
		Summary:     "Save DTS template entries",
		Description: "Saves an array of DTS entries. Request body is open: []DTSEntry where each entry's `template` field is polymorphic (string, object, or array). Readonly (bundled) entries are rejected.",
		Tags:        []string{"dts"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *dtsSaveInput) (*dtsSaveOutput, error) {
		var entries []dts.DTSEntry
		if err := json.Unmarshal(in.Body, &entries); err != nil {
			log.Warnf("dts save: invalid request body: %v", err)
			return nil, huma.Error400BadRequest("invalid request body: " + err.Error())
		}

		if len(entries) == 0 {
			log.Warnf("dts save: received empty entries array")
			return nil, huma.Error400BadRequest("no templates provided")
		}

		for i, entry := range entries {
			// Platform is required except for platform-agnostic types (e.g.
			// help), whose fallbacks carry an empty platform. Allowing "" for
			// those lets an override keep the fallback's key and shadow it,
			// instead of surfacing as a duplicate alongside the fallback.
			if entry.Type == "" || (entry.Platform == "" && !dts.IsPlatformAgnosticType(entry.Type)) {
				msg := fmt.Sprintf("entry %d missing required fields (type=%q, platform=%q, id=%q)", i, entry.Type, entry.Platform, entry.ID)
				log.Warnf("dts save: %s", msg)
				return nil, huma.Error400BadRequest(msg)
			}
		}

		saved := 0
		for _, entry := range entries {
			if err := ts.SaveEntry(entry); err != nil {
				log.Warnf("dts save: failed to save %s/%s/%s/%s: %v", entry.Type, entry.Platform, entry.ID, entry.Language, err)
				return nil, huma.Error403Forbidden(err.Error())
			}
			saved++
		}

		log.Infof("dts save: saved %d template(s) via API", saved)
		out := &dtsSaveOutput{}
		out.Body.Status = "ok"
		out.Body.Saved = saved
		return out, nil
	})
}

// dtsEnrichRequest documents the enrich request wrapper (documentation-only
// shape for dtsEnrichBody.Schema(); never bound to directly). The outer fields
// are NAMED and documented; `webhook` is openJSON so its schema stays OPEN. The
// scalar outer fields use openJSON too (intended type described in prose) so
// huma never rejects a wrong-typed scalar before the handler — preserving the
// legacy lenient 400 contract.
type dtsEnrichRequest struct {
	Type     openJSON `json:"type" doc:"Webhook type string: pokemon, raid, invasion, quest, pokestop, gym, nest, fort-update, max-battle"`
	Webhook  openJSON `json:"webhook" doc:"Raw webhook message payload (open shape; depends on the type field)"`
	Language openJSON `json:"language" doc:"Locale code string (defaults to en)"`
	Platform openJSON `json:"platform" doc:"Target platform string (defaults to discord)"`
}

// dtsEnrichBody is the POST /api/dts/enrich request body. It captures the raw
// bytes verbatim so the handler keeps the legacy lenient parsing and its own
// required-field checks. Its Schema() documents the {type,language,platform,
// webhook} wrapper — webhook OPEN — while staying permissive.
type dtsEnrichBody json.RawMessage

func (b *dtsEnrichBody) UnmarshalJSON(data []byte) error {
	*b = append((*b)[:0], data...)
	return nil
}

func (b dtsEnrichBody) MarshalJSON() ([]byte, error) {
	if len(b) == 0 {
		return []byte("null"), nil
	}
	return b, nil
}

// Schema documents the enrich request wrapper, loosened to be permissive.
func (dtsEnrichBody) Schema(r huma.Registry) *huma.Schema {
	return openObjectSchema(r, reflect.TypeFor[dtsEnrichRequest](),
		"DTS enrich request: {type,language,platform,webhook}. `webhook` is an OPEN payload; required fields (type, webhook) are enforced by the handler.")
}

// dtsEnrichInput carries the enrich request.
type dtsEnrichInput struct {
	Body dtsEnrichBody
}

// dtsEnrichResponse is the typed body for POST /api/dts/enrich. The outer
// envelope is named; `variables` stays OPEN (the enriched variable map, any
// shape). Field order is alphabetical (status, variables) to match the legacy
// map[string]any byte order.
type dtsEnrichResponse struct {
	Status    string `json:"status" doc:"Always \"ok\""`
	Variables any    `json:"variables" doc:"The enriched template-variable map (any shape)"`
}

// dtsEnrichOutput is the typed huma output for POST /api/dts/enrich.
type dtsEnrichOutput struct {
	Body dtsEnrichResponse
}

// RegisterDTSEnrich registers POST /api/dts/enrich, running a webhook through
// the enrichment pipeline. Replaces gin HandleDTSEnrich. The request envelope is
// named/documented ({type,language,platform}) with an OPEN `webhook` field and
// stays permissive. The response `variables` is the enriched variable map (any
// shape).
func RegisterDTSEnrich(api huma.API, svc EnrichService) {
	huma.Register(api, huma.Operation{
		OperationID: "post-dts-enrich", Method: "POST", Path: "/dts/enrich",
		Summary:     "Run webhook through enrichment pipeline",
		Description: "Runs a webhook through the enrichment pipeline. Request envelope ({type,language,platform}) is typed; `webhook` is an OPEN payload. Response `variables` is the enriched variable map (any shape).",
		Tags:        []string{"dts"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *dtsEnrichInput) (*dtsEnrichOutput, error) {
		var req struct {
			Type     string          `json:"type"`
			Webhook  json.RawMessage `json:"webhook"`
			Language string          `json:"language"`
			Platform string          `json:"platform"`
		}
		if err := json.Unmarshal([]byte(in.Body), &req); err != nil {
			return nil, huma.Error400BadRequest("invalid request body: " + err.Error())
		}

		if req.Type == "" {
			return nil, huma.Error400BadRequest("type is required")
		}
		if len(req.Webhook) == 0 {
			return nil, huma.Error400BadRequest("webhook is required")
		}
		if req.Language == "" {
			req.Language = "en"
		}
		if req.Platform == "" {
			req.Platform = "discord"
		}

		variables, err := svc.EnrichWebhook(req.Type, req.Webhook, req.Language, req.Platform)
		if err != nil {
			log.Errorf("dts enrich: %v", err)
			return nil, huma.Error500InternalServerError(err.Error())
		}

		return &dtsEnrichOutput{Body: dtsEnrichResponse{Status: "ok", Variables: variables}}, nil
	})
}

// dtsSendTestTarget is the target sub-object for the sendtest request. Its
// scalar fields use openJSON (intended type described in prose) so a wrong-typed
// scalar never trips huma's pre-bind validation — preserving the legacy lenient
// 400 contract.
type dtsSendTestTarget struct {
	ID   openJSON `json:"id" doc:"Destination ID string (required; type defaults to discord:user)"`
	Type openJSON `json:"type" doc:"Destination type string, e.g. discord:user, discord:channel, telegram:user"`
}

// dtsSendTestRequest documents the sendtest request wrapper (documentation-only
// shape for dtsSendTestBody.Schema(); never bound to directly). The outer fields
// are NAMED and documented; `template` and `variables` stay OPEN. Scalar outer
// fields use openJSON too so huma never rejects a wrong-typed scalar before the
// handler — preserving the legacy lenient 400 contract.
type dtsSendTestRequest struct {
	Template  openJSON          `json:"template" doc:"Polymorphic template value (string or object) — open shape"`
	Variables openJSON          `json:"variables" doc:"Arbitrary template-variable map — open shape"`
	Target    dtsSendTestTarget `json:"target" doc:"Delivery destination (id required; type defaults to discord:user)"`
	Language  openJSON          `json:"language" doc:"Locale code string (defaults to en)"`
	Platform  openJSON          `json:"platform" doc:"Target platform string (defaults to discord)"`
}

// dtsSendTestBody is the POST /api/dts/sendtest request body. It captures the
// raw bytes verbatim so the handler keeps the legacy lenient parsing and its own
// required-field checks. Its Schema() documents the {template,variables,target,
// language,platform} wrapper — template and variables OPEN — while staying
// permissive.
type dtsSendTestBody json.RawMessage

func (b *dtsSendTestBody) UnmarshalJSON(data []byte) error {
	*b = append((*b)[:0], data...)
	return nil
}

func (b dtsSendTestBody) MarshalJSON() ([]byte, error) {
	if len(b) == 0 {
		return []byte("null"), nil
	}
	return b, nil
}

// Schema documents the sendtest request wrapper, loosened to be permissive.
func (dtsSendTestBody) Schema(r huma.Registry) *huma.Schema {
	return openObjectSchema(r, reflect.TypeFor[dtsSendTestRequest](),
		"DTS sendtest request: {template,variables,target,language,platform}. `template` and `variables` are OPEN; required fields (template, target.id) are enforced by the handler.")
}

// dtsSendTestInput carries the sendtest request.
type dtsSendTestInput struct {
	Body dtsSendTestBody
}

// dtsSendTestOutput is the typed body for POST /api/dts/sendtest: {status,
// message} where message is the fixed string "sent" on success.
type dtsSendTestOutput struct {
	Body struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
}

// RegisterDTSSendTest registers POST /api/dts/sendtest, rendering a template
// with provided variables and delivering it to a target. Replaces gin
// HandleDTSSendTest. The request envelope ({target,language,platform}) is typed;
// `template` and `variables` stay OPEN, and the body stays permissive.
func RegisterDTSSendTest(api huma.API, dispatcher dtsDispatcher, ts dtsSendTestReader, renderer dtsShortener) {
	huma.Register(api, huma.Operation{
		OperationID: "post-dts-sendtest", Method: "POST", Path: "/dts/sendtest",
		Summary:     "Render and deliver a test message",
		Description: "Renders a template with provided variables and delivers it to a target. Request envelope ({target,language,platform}) is typed; `template` (string or object) and `variables` are OPEN.",
		Tags:        []string{"dts"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *dtsSendTestInput) (*dtsSendTestOutput, error) {
		if dispatcher == nil {
			return nil, huma.Error503ServiceUnavailable("delivery dispatcher not configured")
		}

		var req struct {
			Template  any            `json:"template"`
			Variables map[string]any `json:"variables"`
			Target    struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"target"`
			Language string `json:"language"`
			Platform string `json:"platform"`
		}
		if err := json.Unmarshal([]byte(in.Body), &req); err != nil {
			return nil, huma.Error400BadRequest("invalid request body: " + err.Error())
		}

		if req.Template == nil {
			return nil, huma.Error400BadRequest("template is required")
		}
		if req.Target.ID == "" {
			return nil, huma.Error400BadRequest("target.id is required")
		}
		if req.Target.Type == "" {
			req.Target.Type = "discord:user"
		}
		if req.Language == "" {
			req.Language = "en"
		}
		if req.Platform == "" {
			req.Platform = "discord"
		}

		// JSON-stringify the template object with SetEscapeHTML(false) to preserve
		// <, >, & in Handlebars expressions like {{#compare x '<' 100}}.
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(req.Template); err != nil {
			return nil, huma.Error400BadRequest("invalid template: " + err.Error())
		}
		templateStr := strings.TrimSpace(buf.String())

		compiled, err := raymond.Parse(templateStr)
		if err != nil {
			log.Warnf("dts sendtest: template compile error: %v\nTemplate: %s", err, templateStr)
			return nil, huma.Error400BadRequest("template compile error: " + err.Error())
		}

		// Register partials so {{> partialName}} works.
		partials := ts.Partials()
		if len(partials) > 0 {
			compiled.RegisterPartials(partials)
		}

		df := raymond.NewDataFrame()
		df.Set("language", req.Language)
		df.Set("platform", req.Platform)

		rendered, err := compiled.ExecWith(req.Variables, df)
		if err != nil {
			log.Warnf("dts sendtest: render error: %v", err)
			return nil, huma.Error500InternalServerError("render error: " + err.Error())
		}

		// Process <S< ... >S> shortlink markers the same way the live renderer does.
		if renderer != nil {
			rendered = dts.ShortenMarkers(rendered, renderer.Shortener())
		}

		var message json.RawMessage
		if err := json.Unmarshal([]byte(rendered), &message); err != nil {
			log.Warnf("dts sendtest: rendered template is not valid JSON: %v\nRendered: %s", err, rendered)
			return nil, huma.Error500InternalServerError("rendered template is not valid JSON: " + err.Error())
		}

		job := &delivery.Job{
			Target:       req.Target.ID,
			Type:         req.Target.Type,
			Message:      message,
			Name:         "DTS Editor Test",
			LogReference: "dts-editor",
		}
		dispatcher.Dispatch(job)

		out := &dtsSendTestOutput{}
		out.Body.Status = "ok"
		out.Body.Message = "sent"
		return out, nil
	})
}

// RegisterDTSWrites registers the in-block DTS write/render endpoints that must
// stay gated behind dtsRenderer != nil. The sendtest dispatcher may be nil
// (handled at request time with a 503).
func RegisterDTSWrites(api huma.API, ts interface {
	dtsRenderReader
	dtsSaveWriter
	dtsMetadataReader
	dtsSendTestReader
}, enrich EnrichService, dispatcher dtsDispatcher, renderer dtsShortener) {
	RegisterTemplateConfig(api, ts)
	RegisterDTSRender(api, ts)
	RegisterDTSSaveTemplates(api, ts)
	RegisterDTSEnrich(api, enrich)
	RegisterDTSSendTest(api, dispatcher, ts, renderer)
}

// compile-time assertions that the concrete types satisfy the write interfaces.
var (
	_ dtsRenderReader   = (*dts.TemplateStore)(nil)
	_ dtsSaveWriter     = (*dts.TemplateStore)(nil)
	_ dtsMetadataReader = (*dts.TemplateStore)(nil)
	_ dtsSendTestReader = (*dts.TemplateStore)(nil)
	_ dtsShortener      = (*dts.Renderer)(nil)
	_ dtsDispatcher     = (*delivery.Dispatcher)(nil)
)
