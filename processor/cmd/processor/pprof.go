package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/config"
)

// mountPprof serves the net/http/pprof handlers (registered on
// http.DefaultServeMux by the blank import in main.go) under /debug/pprof
// when [processor] enable_pprof is set, behind the same API secret as /api.
// Disabled, the routes don't exist.
func mountPprof(r *gin.Engine, cfg config.ProcessorConfig) {
	if !cfg.EnablePprof {
		return
	}
	if cfg.APISecret == "" {
		log.Warn("[processor] enable_pprof is on with no api_secret: /debug/pprof is unauthenticated")
	}
	g := r.Group("/debug/pprof")
	g.Use(api.RequireSecretGin(cfg.APISecret))
	g.GET("/", gin.WrapF(http.DefaultServeMux.ServeHTTP))
	g.GET("/:name", gin.WrapF(http.DefaultServeMux.ServeHTTP))
}
