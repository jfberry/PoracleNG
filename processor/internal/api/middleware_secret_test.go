package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func secretTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api")
	g.Use(RequireSecretGin("s3cret"))
	g.Any("/*path", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/health", HandleHealth())
	return r
}

func TestRequireSecret_V1KeepsLegacyBody(t *testing.T) {
	r := secretTestRouter()
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/tracking/pokemon/u1"},
		{http.MethodPost, "/api/humans"},
		{http.MethodGet, "/api/humans/u1"},
		{http.MethodGet, "/api/profiles/u1"},
		{http.MethodGet, "/api/tracking"},
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: code %d", tc.path, rec.Code)
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if got["status"] != "authError" || got["reason"] != "incorrect or missing api secret" {
			t.Errorf("%s: body %s", tc.path, rec.Body.String())
		}
	}
}

func TestRequireSecret_HumaRoutesGetProblemJSON(t *testing.T) {
	r := secretTestRouter()
	for _, path := range []string{"/api/v2/humans/u1", "/api/v2/humans", "/api/config/poracleWeb", "/api/humansx", "/api/trackingfoo"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: code %d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
			t.Errorf("%s: content-type %q", path, ct)
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if got["status"] != float64(401) || got["title"] != "Unauthorized" || got["detail"] != "incorrect or missing api secret" {
			t.Errorf("%s: body %s", path, rec.Body.String())
		}
	}
}

func TestNoAPIHealthRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	apiGroup := r.Group("/api")
	apiGroup.Use(RequireSecretGin("s3cret"))
	NewHumaAPI(r, apiGroup, "test")
	r.GET("/health", HandleHealth())
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-Poracle-Secret", "s3cret")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/health: %d, want 404", rec.Code)
	}
}

func TestOpenAPI_DocumentsHealthAndServers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	apiGroup := r.Group("/api")
	humaAPI := NewHumaAPI(r, apiGroup, "test")
	registerAllHumaOpsForTest(humaAPI)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	var spec struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Servers) != 1 || spec.Servers[0].URL != "/api" {
		t.Errorf("servers: %+v", spec.Servers)
	}
	h, ok := spec.Paths["/health"]
	if !ok {
		t.Fatal("no /health path")
	}
	srv, _ := h["servers"].([]any)
	if len(srv) != 1 || srv[0].(map[string]any)["url"] != "/" {
		t.Errorf("health servers: %v", h["servers"])
	}
	get, _ := h["get"].(map[string]any)
	sec, present := get["security"].([]any)
	if !present || len(sec) != 0 {
		t.Errorf("health security: %v (present=%v)", get["security"], present)
	}
}
