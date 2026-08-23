package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
)

// pingOutput is the trivial typed body returned by both dual-path ops below.
type pingOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

// registerPing registers a trivial GET op returning {"ok":true} on the given
// huma API at the given path/operationID.
func registerPing(api huma.API, opID, path string) {
	huma.Register(api, huma.Operation{
		OperationID: opID,
		Method:      http.MethodGet,
		Path:        path,
	}, func(_ context.Context, _ *struct{}) (*pingOutput, error) {
		out := &pingOutput{}
		out.Body.OK = true
		return out, nil
	})
}

// TestSingleInstanceDualPathMount proves the locked architecture decision: one
// huma API instance (NewHumaAPI), mounted on the /api gin group, serves both an
// "in-place" op (/ping → /api/ping) and a "v2" op (/v2/ping → /api/v2/ping),
// and both ops appear in the single generated OpenAPI spec.
func TestSingleInstanceDualPathMount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	registerPing(humaAPI, "ping-inplace", "/ping")
	registerPing(humaAPI, "ping-v2", "/v2/ping")

	// Both paths must SERVE 200 with {"ok":true}.
	for _, url := range []string{"/api/ping", "/api/v2/ping"} {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200; body: %s", url, w.Code, w.Body.String())
		}
		var got struct {
			OK bool `json:"ok"`
		}
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatalf("GET %s decode body: %v", url, err)
		}
		if !got.OK {
			t.Errorf("GET %s body = %v, want {\"ok\":true}", url, got)
		}
	}

	// Both ops must appear in the SINGLE OpenAPI spec.
	b, err := humaAPI.OpenAPI().MarshalJSON()
	if err != nil {
		t.Fatalf("marshal OpenAPI: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("unmarshal OpenAPI: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI spec has no paths object; spec: %v", spec)
	}
	// huma records op paths relative to the gin group (/ping, /v2/ping), not
	// the mounted /api prefix.
	for _, want := range []string{"/ping", "/v2/ping"} {
		if _, present := paths[want]; !present {
			keys := make([]string, 0, len(paths))
			for k := range paths {
				keys = append(keys, k)
			}
			t.Errorf("OpenAPI paths missing %q; observed keys: %v", want, keys)
		}
	}
}
