package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
)

func pprofStatus(t *testing.T, cfg config.ProcessorConfig, secret string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	mountPprof(r, cfg)
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	if secret != "" {
		req.Header.Set("X-Poracle-Secret", secret)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

// /debug/pprof used to be mounted unconditionally and outside the API
// secret (#265). It is now off by default and secret-protected when on.
func TestPprofDisabledByDefault(t *testing.T) {
	if got := pprofStatus(t, config.ProcessorConfig{APISecret: "s"}, "s"); got != http.StatusNotFound {
		t.Errorf("default config: status %d, want 404", got)
	}
}

func TestPprofRequiresSecretWhenEnabled(t *testing.T) {
	cfg := config.ProcessorConfig{EnablePprof: true, APISecret: "s"}
	if got := pprofStatus(t, cfg, ""); got == http.StatusOK {
		t.Errorf("no secret: status %d, want rejection", got)
	}
	if got := pprofStatus(t, cfg, "s"); got != http.StatusOK {
		t.Errorf("with secret: status %d, want 200", got)
	}
}
