package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/config"
)

// huma_autocreate_golden_test.go is the golden OpenAPI spec test for the six
// autocreate ops that live in package cmd/processor (registerAutocreateHuma +
// registerAutocreateTemplatesHuma). These ops can't be covered by the api-package
// golden (internal/api/huma_openapi_golden_test.go) because they depend on
// *discordbot.Bot, and api → discordbot → bot/commands → api is an import cycle:
// the register funcs must therefore stay here. This golden is the autocreate
// counterpart to the api-package golden — together they guard the WHOLE huma
// surface the production server mounts (cmd/processor/main.go).
//
// It builds a huma API, registers the two autocreate funcs with a zero-value cfg
// and a nil bot (handlers are never invoked during spec generation, so the nil
// bot is fine), marshals the generated OpenAPI 3.1 document, canonicalises it to
// stable JSON, and compares it byte-for-byte to the committed golden at
// testdata/autocreate-openapi.golden.json.
//
// Regenerate the golden after an intentional spec change:
//
//	UPDATE_GOLDEN=1 go test ./cmd/processor/ -run TestAutocreateOpenAPIGolden

const autocreateGoldenRelPath = "testdata/autocreate-openapi.golden.json"

// shouldUpdateAutocreateGolden mirrors the api-package UPDATE_GOLDEN=1 mechanism.
func shouldUpdateAutocreateGolden() bool {
	return os.Getenv("UPDATE_GOLDEN") == "1"
}

// buildAutocreateHumaSpec builds a fresh huma API with the six autocreate ops
// registered and returns the canonicalised OpenAPI JSON. The deps mirror
// main.go's registerAutocreateHuma / registerAutocreateTemplatesHuma calls with
// stub values — registration only builds handler closures.
func buildAutocreateHumaSpec(t *testing.T) []byte {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := api.NewHumaAPI(r, r.Group("/api"), "test")
	registerAutocreateHuma(humaAPI, &config.Config{}, nil)
	registerAutocreateTemplatesHuma(humaAPI, &config.Config{})

	raw, err := humaAPI.OpenAPI().MarshalJSON()
	if err != nil {
		t.Fatalf("marshal OpenAPI: %v", err)
	}
	return canonicalAutocreateOpenAPIJSON(t, raw)
}

// canonicalAutocreateOpenAPIJSON re-encodes JSON with Go's encoding/json, which
// sorts object keys and applies a stable 2-space indent, neutralising any
// map-ordering non-determinism in huma's marshaling so the golden is stable.
func canonicalAutocreateOpenAPIJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal for canonicalisation: %v", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("re-encode canonical JSON: %v", err)
	}
	return buf.Bytes()
}

func TestAutocreateOpenAPIGolden(t *testing.T) {
	got := buildAutocreateHumaSpec(t)
	goldenPath := filepath.FromSlash(autocreateGoldenRelPath)

	if shouldUpdateAutocreateGolden() {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote golden %s (%d bytes)", goldenPath, len(got))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with UPDATE_GOLDEN=1 to generate)", goldenPath, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("autocreate OpenAPI spec drifted from golden %s.\n"+
			"If this change is intentional, regenerate with:\n"+
			"    UPDATE_GOLDEN=1 go test ./cmd/processor/ -run TestAutocreateOpenAPIGolden\n"+
			"then inspect the diff before committing.\n%s",
			goldenPath, firstAutocreateDiffHint(want, got))
	}
}

// TestAutocreateOpenAPIGoldenDeterministic guards against non-deterministic
// marshaling: two independent builds must produce byte-identical canonical specs.
func TestAutocreateOpenAPIGoldenDeterministic(t *testing.T) {
	a := buildAutocreateHumaSpec(t)
	b := buildAutocreateHumaSpec(t)
	if !bytes.Equal(a, b) {
		t.Fatalf("autocreate OpenAPI marshaling is non-deterministic across builds:\n%s", firstAutocreateDiffHint(a, b))
	}
}

// firstAutocreateDiffHint returns a short, line-oriented hint pointing at the
// first differing line so a failing diff is readable without dumping the whole
// spec.
func firstAutocreateDiffHint(want, got []byte) string {
	wl := bytes.Split(want, []byte("\n"))
	gl := bytes.Split(got, []byte("\n"))
	n := min(len(gl), len(wl))
	for i := range n {
		if !bytes.Equal(wl[i], gl[i]) {
			return "first diff at line " + strconv.Itoa(i+1) + ":\n  - want: " + string(wl[i]) + "\n  + got:  " + string(gl[i])
		}
	}
	if len(wl) != len(gl) {
		return "specs differ in length: want " + strconv.Itoa(len(wl)) + " lines, got " + strconv.Itoa(len(gl)) + " lines"
	}
	return "(no line-level diff found)"
}
