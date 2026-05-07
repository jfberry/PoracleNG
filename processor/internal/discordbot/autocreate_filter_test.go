package discordbot

import (
	"reflect"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

func init() {
	// Ensure DTS helpers (eq, ne, and, or, not, ...) are registered before
	// the filter tests run. The renderer registers them on first use; we
	// force that here by touching its public API.
	dts.RegisterHelpers()
}

func TestRenderFilter_EmptyMatchesAll(t *testing.T) {
	f := geofence.Fence{Name: "Gent_centrum"}
	ok, err := renderFilter("", f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error("empty filter should match")
	}
}

func TestRenderFilter_TruthyProperty(t *testing.T) {
	f := geofence.Fence{
		Name:       "Gent_centrum",
		Properties: map[string]any{"beserver": true},
	}
	ok, err := renderFilter("{{beserver}}", f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error("expected truthy match for beserver=true")
	}

	f.Properties["beserver"] = false
	ok, err = renderFilter("{{beserver}}", f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Error("beserver=false should not match")
	}
}

func TestRenderFilter_EqHelper(t *testing.T) {
	f := geofence.Fence{
		Name:       "Gent_centrum",
		Properties: map[string]any{"server": "uk"},
	}
	ok, err := renderFilter(`{{eq server "uk"}}`, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error(`expected {{eq server "uk"}} to match`)
	}

	ok, err = renderFilter(`{{eq server "ie"}}`, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Error(`{{eq server "ie"}} should not match a uk fence`)
	}
}

func TestRenderFilter_NamedFieldsAvailable(t *testing.T) {
	f := geofence.Fence{Name: "Gent_centrum", Group: "Belgium"}
	ok, err := renderFilter(`{{eq group "Belgium"}}`, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error(`group should be available alongside Properties`)
	}
}

func TestRenderFilter_NamedFieldShadowsProperty(t *testing.T) {
	// Edge case: if Properties happened to contain "name", the named
	// field must still win.
	f := geofence.Fence{
		Name:       "Real_name",
		Properties: map[string]any{"name": "Decoy"},
	}
	ok, err := renderFilter(`{{eq name "Real_name"}}`, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error("named Name field should win over Properties[\"name\"]")
	}
}

func TestRenderFilter_FalseStringNotTruthy(t *testing.T) {
	// Truthiness rule: rendered string is truthy unless trimmed value is
	// "", "false", or "0".
	f := geofence.Fence{Properties: map[string]any{"v": "false"}}
	ok, _ := renderFilter("{{v}}", f)
	if ok {
		t.Error(`literal "false" should not be truthy`)
	}
	f.Properties["v"] = "0"
	ok, _ = renderFilter("{{v}}", f)
	if ok {
		t.Error(`literal "0" should not be truthy`)
	}
	f.Properties["v"] = "  "
	ok, _ = renderFilter("{{v}}", f)
	if ok {
		t.Error("whitespace should not be truthy")
	}
}

func TestRenderParams_Positional(t *testing.T) {
	f := geofence.Fence{
		Name:  "Gent_centrum",
		Group: "Belgium",
	}
	out, err := renderParams([]string{"{{group}}", "{{name}}"}, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"Belgium", "Gent_centrum"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

func TestRenderParams_RendersAgainstProperties(t *testing.T) {
	f := geofence.Fence{
		Name:       "Gent_centrum",
		Properties: map[string]any{"region": "VL"},
	}
	out, err := renderParams([]string{"{{region}}-{{name}}"}, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out[0] != "VL-Gent_centrum" {
		t.Errorf("got %q, want VL-Gent_centrum", out[0])
	}
}
