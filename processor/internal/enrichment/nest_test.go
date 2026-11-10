package enrichment

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// nestTilePayload runs a nest through enrichment in inline tile mode against a
// fake tileserver and returns the JSON body POSTed to /staticmap/poracle-nest.
func nestTilePayload(t *testing.T, nest *webhook.NestWebhook) map[string]any {
	t.Helper()
	var (
		mu   sync.Mutex
		body []byte
		path string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body, path = b, r.URL.Path
		mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("png"))
	}))
	defer srv.Close()

	sm := staticmap.New(staticmap.Config{Provider: "tileservercache", ProviderURL: srv.URL})
	defer sm.Close()
	e := &Enricher{StaticMap: sm, TimeLayout: "15:04:05", DateLayout: "2006-01-02"}

	_, pending := e.Nest(nest, TileModeInline)
	if pending == nil {
		t.Fatal("expected a pending inline tile")
	}
	select {
	case <-pending.ResultImg:
	case <-time.After(5 * time.Second):
		t.Fatal("tile request never completed")
	}

	mu.Lock()
	defer mu.Unlock()
	if path != "/staticmap/poracle-nest" {
		t.Fatalf("tile POST path = %q, want /staticmap/poracle-nest", path)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("tile body is not JSON: %v (%s)", err, body)
	}
	return payload
}

// The shipped poracle-nest tileserver templates emit map_latitude,
// map_longitude and zoom unquoted, draw poly_path as the nest outline and put
// the marker at latitude/longitude. Missing map fields made every nest tile
// invalid JSON, so the tileserver rejected it and alerts got the fallback map.
func TestNestStaticMapPayload_WithPolygon(t *testing.T) {
	nest := &webhook.NestWebhook{
		NestID: 1, Lat: 51.5, Lon: -0.12, PokemonID: 25,
		PolyPath:  `[[[51.501,-0.121],[51.502,-0.118],[51.499,-0.117],[51.498,-0.122]]]`,
		ResetTime: time.Now().Unix(),
	}
	p := nestTilePayload(t, nest)

	for _, k := range []string{"map_latitude", "map_longitude", "zoom"} {
		if _, ok := p[k].(float64); !ok {
			t.Errorf("%s missing from tile payload: %#v", k, p[k])
		}
	}
	if z := p["zoom"].(float64); z > 16 {
		t.Errorf("zoom = %v, want capped at 16", z)
	}
	// Marker stays on the nest, not the autopositioned centre.
	if p["latitude"] != 51.5 || p["longitude"] != -0.12 {
		t.Errorf("marker = %v,%v, want the nest's own 51.5,-0.12", p["latitude"], p["longitude"])
	}
	if p["map_latitude"] == 51.5 && p["map_longitude"] == -0.12 {
		t.Error("map centre equals the nest point; expected the polygon's autopositioned centre")
	}
	poly, ok := p["poly_path"].([]any)
	if !ok || len(poly) != 1 {
		t.Fatalf("poly_path = %#v, want one polygon", p["poly_path"])
	}
	ring, ok := poly[0].([]any)
	if !ok || len(ring) != 4 {
		t.Fatalf("polygon = %#v, want 4 points", poly[0])
	}
	if pt, ok := ring[0].([]any); !ok || len(pt) != 2 || pt[0] != 51.501 || pt[1] != -0.121 {
		t.Errorf("first point = %#v, want [51.501 -0.121]", ring[0])
	}
}

// Without a usable poly_path the map fields still have to be present, or the
// template is invalid JSON; poly_path is left out so the outline block is skipped.
func TestNestStaticMapPayload_WithoutPolygon(t *testing.T) {
	for name, polyPath := range map[string]string{"empty": "", "unparseable": "not json"} {
		t.Run(name, func(t *testing.T) {
			p := nestTilePayload(t, &webhook.NestWebhook{
				NestID: 2, Lat: 48.85, Lon: 2.35, PokemonID: 1, PolyPath: polyPath,
				ResetTime: time.Now().Unix(),
			})
			if p["map_latitude"] != 48.85 || p["map_longitude"] != 2.35 {
				t.Errorf("map centre = %v,%v, want the nest point", p["map_latitude"], p["map_longitude"])
			}
			if p["zoom"] != float64(16) {
				t.Errorf("zoom = %v, want default 16", p["zoom"])
			}
			if _, ok := p["poly_path"]; ok {
				t.Errorf("poly_path should be absent, got %#v", p["poly_path"])
			}
		})
	}
}

// Non-pregenerate tiles put the fields in the URL query, which can't carry
// the nested polygon array; poly_path is pregenerate/POST only.
func TestStaticMapFieldsForType_NestPolyPathPregenOnly(t *testing.T) {
	keys, pregen := staticMapFieldsForType("nest")
	has := func(list []string, k string) bool {
		for _, v := range list {
			if v == k {
				return true
			}
		}
		return false
	}
	for _, k := range []string{"map_latitude", "map_longitude", "zoom"} {
		if !has(keys, k) || !has(pregen, k) {
			t.Errorf("%s must be in both field lists", k)
		}
	}
	if has(keys, "poly_path") {
		t.Error("poly_path must not be in the URL (non-pregenerate) field list")
	}
	if !has(pregen, "poly_path") {
		t.Error("poly_path must be in the pregenerate field list")
	}
}
