package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/dtsmap"
)

// realFallbacksDir locates the repo's bundled fallbacks/ directory relative
// to this test file's package (processor/internal/api), mirroring the
// repoRoot() helper in cmd/processor/enrich_test.go: three levels up from
// this package lands at the repo root, where fallbacks/ lives (see
// CLAUDE.md's directory layout).
func realFallbacksDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "fallbacks")
	if _, err := os.Stat(filepath.Join(dir, "testdata.json")); err != nil {
		t.Skipf("real fallbacks/testdata.json not found at %s: %v", dir, err)
	}
	return dir
}

// dtsTypeAllowEmpty lists DTS type names that are genuinely expected to
// return zero entries against the bundled fallbacks/testdata.json — either
// because no sample webhook exists for that source (nest), or because the
// name isn't wired into dtsmap.TypeMap() at all and so is never iterated
// below (greeting: it's a dts_fields.go-only entry with no dtsmap.Source,
// listed here for documentation parity with the task instructions rather
// than because the loop would ever visit it).
var dtsTypeAllowEmpty = map[string]bool{
	"nest":     true, // no "nest" webhook sample in fallbacks/testdata.json
	"greeting": true, // not a dtsmap.TypeMap() entry; documented, not iterated
}

// TestHumaDTSTestdata_RealFixture_AllDTSTypesResolve is a regression guard
// against a future testdata.json "type" rename (or a dtsmap WebhookType
// typo, like the fort-update/fort_update mismatch this test was written to
// catch) silently breaking ?dtsType= filtering for the editor. It loads the
// REAL bundled fallbacks/testdata.json through the same loadTestdata code
// path the endpoint uses, then asserts every DTS type name in
// dtsmap.TypeMap() resolves to at least one testdata entry, except the
// explicit dtsTypeAllowEmpty set.
func TestHumaDTSTestdata_RealFixture_AllDTSTypesResolve(t *testing.T) {
	fallbackDir := realFallbacksDir(t)

	r, api := newDTSTestAPI(t)
	RegisterDTSTestdata(api, t.TempDir(), fallbackDir)

	get := func(dtsType string) []any {
		t.Helper()
		q := url.Values{"dtsType": {dtsType}}
		req := httptest.NewRequest(http.MethodGet, "/api/dts/testdata?"+q.Encode(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("dtsType=%s: status = %d, body: %s", dtsType, w.Code, w.Body.String())
		}
		body := decodeBody(t, w)
		td, _ := body["testdata"].([]any)
		return td
	}

	for name := range dtsmap.TypeMap() {
		t.Run(fmt.Sprintf("dtsType=%s", name), func(t *testing.T) {
			td := get(name)
			if dtsTypeAllowEmpty[name] {
				return
			}
			if len(td) == 0 {
				t.Errorf("dtsType=%s returned zero testdata entries against the real fallbacks/testdata.json; either the fixture needs a sample or dtsmap's WebhookType for %q no longer matches the testdata \"type\" field", name, name)
			}
		})
	}
}

// TestFortUpdateDTSTypeResolvesToUnderscoreWebhookType pins down Fix 1
// directly: the "fort-update" DTS type name (the editor's TemplateType
// spelling, per internal/api/dts_fields.go's fieldsByType key) must resolve
// to WebhookType "fort_update" — the literal testdata.json/wire spelling —
// same as the "fort_update" identity entry. Before the fix,
// dtsmap.Alias("fort-update").WebhookType was the hyphenated
// "fort-update", which matches no testdata entry.
func TestFortUpdateDTSTypeResolvesToUnderscoreWebhookType(t *testing.T) {
	hyphen, ok := dtsmap.Alias("fort-update")
	if !ok {
		t.Fatal(`dtsmap.Alias("fort-update") not found`)
	}
	if hyphen.WebhookType != "fort_update" {
		t.Errorf(`dtsmap.Alias("fort-update").WebhookType = %q, want "fort_update"`, hyphen.WebhookType)
	}

	underscore, ok := dtsmap.Alias("fort_update")
	if !ok {
		t.Fatal(`dtsmap.Alias("fort_update") not found`)
	}
	if underscore.WebhookType != "fort_update" {
		t.Errorf(`dtsmap.Alias("fort_update").WebhookType = %q, want "fort_update"`, underscore.WebhookType)
	}
}

// TestHumaDTSTestdata_DtsTypeFortUpdate_RealFixture is the endpoint-level
// counterpart of the Alias test above: against the REAL bundled
// fallbacks/testdata.json, ?dtsType=fort-update must return the 7
// fort_update-typed entries, each tagged dtsType=fort-update.
func TestHumaDTSTestdata_DtsTypeFortUpdate_RealFixture(t *testing.T) {
	fallbackDir := realFallbacksDir(t)

	r, api := newDTSTestAPI(t)
	RegisterDTSTestdata(api, t.TempDir(), fallbackDir)

	req := httptest.NewRequest(http.MethodGet, "/api/dts/testdata?dtsType=fort-update", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	td, _ := body["testdata"].([]any)
	if len(td) != 7 {
		t.Fatalf("dtsType=fort-update returned %d entries, want 7 (the fort_update-typed testdata entries): %v", len(td), td)
	}
	for i, e := range td {
		m := e.(map[string]any)
		if m["type"] != "fort_update" {
			t.Errorf("testdata[%d].type = %v, want fort_update (webhook type preserved)", i, m["type"])
		}
		if m["dtsType"] != "fort-update" {
			t.Errorf("testdata[%d].dtsType = %v, want fort-update", i, m["dtsType"])
		}
	}
}
