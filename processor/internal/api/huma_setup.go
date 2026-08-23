package api

import (
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
)

// NewHumaAPI builds a huma API bound to the authenticated /api group, declares
// the X-Poracle-Secret security scheme, and serves the OpenAPI spec + docs UI
// at PUBLIC top-level paths (no secret). Errors use huma's default RFC 9457
// problem+json model.
func NewHumaAPI(r *gin.Engine, apiGroup *gin.RouterGroup, version string) huma.API {
	cfg := huma.DefaultConfig("PoracleNG API", version)

	// DefaultConfig registers a SchemaLinkTransformer via CreateHooks that
	// injects a "$schema" field into every response body at runtime. This
	// breaks byte-compatibility with existing clients (PoracleWeb, ReactMap)
	// that expect exactly {"status":"ok",...} on success bodies. Clear the
	// hooks before NewWithGroup runs them so the transformer is
	// never installed. The OpenAPI document itself is unaffected — the
	// transformer only mutates live response bodies, not the spec.
	cfg.CreateHooks = nil

	// Disable huma's built-in mounts; we serve our own public copies on r.
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"poracleSecret": {Type: "apiKey", In: "header", Name: "X-Poracle-Secret"},
	}

	humaAPI := humagin.NewWithGroup(r, apiGroup, cfg)

	// Public spec + docs (top-level, outside /api, so RequireSecretGin never runs).
	r.GET("/openapi.json", func(c *gin.Context) {
		b, err := humaAPI.OpenAPI().MarshalJSON()
		if err != nil {
			c.Data(http.StatusInternalServerError, "text/plain", fmt.Appendf(nil, "openapi marshal: %v", err))
			return
		}
		c.Data(http.StatusOK, "application/json", b)
	})
	r.GET("/docs", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html", []byte(docsHTML))
	})
	return humaAPI
}

// docsHTML is a minimal Stoplight Elements page pointed at /openapi.json.
const docsHTML = `<!doctype html><html><head><meta charset="utf-8">
<title>PoracleNG API</title>
<script src="https://unpkg.com/@stoplight/elements/web-components.min.js"></script>
<link rel="stylesheet" href="https://unpkg.com/@stoplight/elements/styles.min.css">
</head><body><elements-api apiDescriptionUrl="/openapi.json" router="hash" layout="sidebar"/></body></html>`
