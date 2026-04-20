package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
)

func TestFlattenTileserverSettings(t *testing.T) {
	tr := true
	fa := false
	in := map[string]config.TileserverConfig{
		"default": {
			Type: "staticMap", Width: 500, Height: 250, Zoom: 15,
			Pregenerate: &tr, IncludeStops: &fa,
		},
		"monster": {
			Type: "multiStaticMap", Width: 600, Height: 300, Zoom: 16,
			Pregenerate: &tr, IncludeStops: &tr, TTL: 3600,
		},
	}
	got := flattenTileserverSettings(in)
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	// Keys are sorted: default first, then monster.
	if got[0]["maptype"] != "default" {
		t.Errorf("row 0 maptype: got %v", got[0]["maptype"])
	}
	if got[0]["include_stops"] != false {
		t.Errorf("default include_stops: got %#v", got[0]["include_stops"])
	}
	if got[1]["maptype"] != "monster" {
		t.Errorf("row 1 maptype: got %v", got[1]["maptype"])
	}
	if got[1]["include_stops"] != true {
		t.Errorf("monster include_stops: got %#v", got[1]["include_stops"])
	}
	if got[1]["ttl"] != 3600 {
		t.Errorf("monster ttl: got %#v", got[1]["ttl"])
	}
}

func TestFlattenTileserverSettingsNilBool(t *testing.T) {
	in := map[string]config.TileserverConfig{
		"default": {Type: "staticMap", Width: 500},
	}
	got := flattenTileserverSettings(in)
	// nil *bool must round-trip as JSON null (Go untyped nil), not false —
	// mergeOpts uses the nil/non-nil distinction to decide whether a
	// per-maptype entry overrides the default layer.
	if got[0]["include_stops"] != nil {
		t.Errorf("nil IncludeStops should render as nil, got %#v", got[0]["include_stops"])
	}
	if got[0]["pregenerate"] != nil {
		t.Errorf("nil Pregenerate should render as nil, got %#v", got[0]["pregenerate"])
	}
	// And must actually marshal to JSON null, not "null" string or omitted.
	buf, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `"include_stops":null`; !strings.Contains(string(buf), want) {
		t.Errorf("expected %q in JSON, got %s", want, string(buf))
	}
}

func TestNestTileserverRows(t *testing.T) {
	in := []any{
		map[string]any{
			"maptype":       "default",
			"type":          "staticMap",
			"width":         500,
			"height":        250,
			"zoom":          15,
			"pregenerate":   true,
			"include_stops": false,
		},
		map[string]any{
			"maptype":       "monster",
			"type":          "multiStaticMap",
			"include_stops": true,
		},
	}
	out := nestTileserverRows(in)
	if len(out) != 2 {
		t.Fatalf("want 2 keys, got %d", len(out))
	}
	def, ok := out["default"].(map[string]any)
	if !ok {
		t.Fatalf("default entry missing or wrong type: %#v", out["default"])
	}
	if _, ok := def["maptype"]; ok {
		t.Errorf("maptype should be removed from nested entry")
	}
	if def["type"] != "staticMap" {
		t.Errorf("default.type: got %#v", def["type"])
	}
	mon := out["monster"].(map[string]any)
	if mon["include_stops"] != true {
		t.Errorf("monster.include_stops: got %#v", mon["include_stops"])
	}
}

func TestNestTileserverRowsSkipsBlankKey(t *testing.T) {
	in := []any{
		map[string]any{"maptype": "", "type": "staticMap"},
		map[string]any{"maptype": "  ", "type": "staticMap"},
		map[string]any{"type": "staticMap"}, // no maptype at all
	}
	out := nestTileserverRows(in)
	if len(out) != 0 {
		t.Errorf("rows with blank/missing maptype should be dropped, got %#v", out)
	}
}

// TestTileserverOverridesRoundTrip verifies that editor-shape rows can be
// nested, JSON-marshalled, and applied to a live Config via ApplyOverrides.
// This is the critical path: editor → overrides.json → in-memory config.
func TestTileserverOverridesRoundTrip(t *testing.T) {
	updates := map[string]any{
		"geocoding": map[string]any{
			"tileserver_settings": []any{
				map[string]any{
					"maptype":       "default",
					"type":          "staticMap",
					"width":         500,
					"height":        250,
					"zoom":          15,
					"pregenerate":   true,
					"include_stops": true,
					"ttl":           0,
				},
			},
		},
	}
	nestTableUpdates(updates)

	// The nested shape is what gets serialised to overrides.json.
	buf, err := json.Marshal(updates)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(buf, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cfg := &config.Config{}
	config.ApplyOverrides(cfg, parsed)

	entry, ok := cfg.Geocoding.TileserverSettings["default"]
	if !ok {
		t.Fatalf("default entry missing after ApplyOverrides; got %#v", cfg.Geocoding.TileserverSettings)
	}
	if entry.Type != "staticMap" {
		t.Errorf("Type: got %q", entry.Type)
	}
	if entry.Width != 500 || entry.Height != 250 || entry.Zoom != 15 {
		t.Errorf("dimensions: got %d x %d @ %d", entry.Width, entry.Height, entry.Zoom)
	}
	if entry.IncludeStops == nil || !*entry.IncludeStops {
		t.Errorf("IncludeStops: got %#v, want *bool(true)", entry.IncludeStops)
	}
	if entry.Pregenerate == nil || !*entry.Pregenerate {
		t.Errorf("Pregenerate: got %#v, want *bool(true)", entry.Pregenerate)
	}
}

// TestTileserverNilRoundTripPreservesDefault simulates the bad scenario: a
// "default" entry with include_stops=true and a "monster" entry whose
// include_stops was never set (nil *bool). The editor re-reads, shows the
// monster row with include_stops=null, and saves the whole batch back. After
// ApplyOverrides, monster.IncludeStops must stay nil so the default layer
// (true) still wins through mergeOpts in staticmap.go. If this collapsed
// nil→false anywhere, monster's overlay would be silently disabled.
func TestTileserverNilRoundTripPreservesDefault(t *testing.T) {
	tr := true
	live := map[string]config.TileserverConfig{
		"default": {Type: "multiStaticMap", Width: 500, Height: 250, Zoom: 15, Pregenerate: &tr, IncludeStops: &tr},
		"monster": {Type: "multiStaticMap", Width: 600}, // IncludeStops and Pregenerate are nil
	}

	rows := flattenTileserverSettings(live)

	// Round-trip through JSON as the editor would.
	buf, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal rows: %v", err)
	}
	var decoded []any
	if err := json.Unmarshal(buf, &decoded); err != nil {
		t.Fatalf("unmarshal rows: %v", err)
	}

	// Rebuild the save payload and apply it back to a fresh config.
	updates := map[string]any{
		"geocoding": map[string]any{"tileserver_settings": decoded},
	}
	nestTableUpdates(updates)

	cfg := &config.Config{}
	config.ApplyOverrides(cfg, updates)

	def, ok := cfg.Geocoding.TileserverSettings["default"]
	if !ok {
		t.Fatalf("default entry missing")
	}
	if def.IncludeStops == nil || !*def.IncludeStops {
		t.Fatalf("default.IncludeStops should stay *true, got %#v", def.IncludeStops)
	}
	mon, ok := cfg.Geocoding.TileserverSettings["monster"]
	if !ok {
		t.Fatalf("monster entry missing")
	}
	if mon.IncludeStops != nil {
		t.Errorf("monster.IncludeStops should stay nil (inherit from default), got %#v", mon.IncludeStops)
	}
	if mon.Pregenerate != nil {
		t.Errorf("monster.Pregenerate should stay nil, got %#v", mon.Pregenerate)
	}
}
