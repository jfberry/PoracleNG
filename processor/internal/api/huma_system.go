package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

// statusOKOutput is THE shared success body for pure action endpoints — those
// that perform a side effect but have no resource to return: {"status":"ok"}.
//
// Success-response convention (see docs/v2-api-design.md):
//   - Pure action endpoints (no resource): {"status":"ok"} — this type, the
//     single shared StatusOK schema referenced by every action op (reloads,
//     summary delete/trigger, dts/templates delete, dts/sendtest,
//     deliverMessages-style acks, and all v2 humans/locations/profiles action
//     mutations: enable/disable/language/areas/location/role/profile switch/
//     delete/copy, etc.).
//   - Resource/data reads: return the typed resource/data directly.
//   - v2 tracking mutations: return the diff ({created,updated,unchanged} /
//     {deleted}).
//
// Responses that carry the status plus extra fields (e.g. {status,backup},
// {status,queued}, {status,saved}, {status,url}) are NOT this type — they keep
// their own typed structs.
//
// Use okStatus() to build the {"status":"ok"} value.
type statusOKOutput struct {
	Body struct {
		Status string `json:"status" doc:"Always \"ok\" on success"`
	}
}

// okStatus builds the canonical {"status":"ok"} action-success body.
func okStatus() *statusOKOutput {
	out := &statusOKOutput{}
	out.Body.Status = "ok"
	return out
}

// RegisterReload registers a reload-style op that returns {"status":"ok"} on
// success (preserving the legacy success body) or a problem+json error on
// failure, for the given method/path on the shared huma API. The summary and
// description are supplied per call site so each reload op documents exactly
// what it reloads (state-only vs. geofence-inclusive vs. DTS templates).
func RegisterReload(api huma.API, opID, method, path, summary, description string, fn func() error) {
	huma.Register(api, huma.Operation{
		OperationID: opID, Method: method, Path: path,
		Summary: summary, Description: description, Tags: []string{"reload"},
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*statusOKOutput, error) {
		if err := fn(); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return okStatus(), nil
	})
}
