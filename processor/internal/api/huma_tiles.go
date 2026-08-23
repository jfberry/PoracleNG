package api

import (
	"context"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// stateManagerFences adapts a *state.Manager to FenceStateGetter, reading the
// current snapshot's Fences on each call.
type stateManagerFences struct{ mgr *state.Manager }

func (s stateManagerFences) Fences() []geofence.Fence { return s.mgr.Get().Fences }

// NewHumaTileDeps builds the testable HumaTileDeps from the concrete TileDeps
// constructed in main.go. Nil concrete pointers are kept as genuine nil
// interfaces so the handlers' `!= nil` guards still fire (avoids typed-nil
// interfaces calling methods on nil pointers).
func NewHumaTileDeps(d TileDeps) HumaTileDeps {
	out := HumaTileDeps{
		StaticMap: d.StaticMap,
		StateMgr:  stateManagerFences{mgr: d.StateMgr},
	}
	if d.ImgUicons != nil {
		out.ImgUicons = d.ImgUicons
	}
	if d.Weather != nil {
		out.Weather = d.Weather
	}
	return out
}

// TileURLGenerator produces a pregenerated tile URL for a given map type/data.
// *staticmap.Resolver satisfies it; a minimal interface keeps the tile Register
// funcs testable with a stub.
type TileURLGenerator interface {
	GetPregeneratedTileURL(maptype string, data map[string]any, staticMapType string) string
}

// WeatherProvider looks up the current weather condition in an S2 cell.
// *tracker.WeatherTracker satisfies it.
type WeatherProvider interface {
	GetCurrentWeatherInCell(cellID string) int
}

// WeatherIconProvider resolves a weather icon URL. *uicons.Uicons satisfies it.
type WeatherIconProvider interface {
	WeatherIcon(weatherID int) string
}

// HumaTileDeps is the testable dependency set for the huma tile-URL endpoints.
// It mirrors the relevant fields of TileDeps via narrow interfaces so the
// Register funcs can be exercised with stubs in tests.
type HumaTileDeps struct {
	StaticMap TileURLGenerator
	StateMgr  FenceStateGetter
	ImgUicons WeatherIconProvider
	Weather   WeatherProvider
}

// FenceStateGetter exposes the current geofence list. *state.Manager's Get()
// returns *state.State whose .Fences field is read; a tiny adapter keeps the
// Register funcs decoupled from the concrete manager for testing.
type FenceStateGetter interface {
	Fences() []geofence.Fence
}

// tileURLOutput is the {status, url} success body shared by all tile endpoints,
// preserved byte-for-byte from the legacy gin handlers.
type tileURLOutput struct {
	Body struct {
		Status string `json:"status"`
		URL    string `json:"url"`
	}
}

// tileURLResult builds the typed {status, url} output, mapping an empty URL to
// the same 500 the legacy tileJSONOK produced.
func tileURLResult(tileURL string) (*tileURLOutput, error) {
	if tileURL == "" {
		return nil, huma.Error500InternalServerError("tile generation failed — check static map provider configuration and processor logs for 'staticmap:' warnings")
	}
	out := &tileURLOutput{}
	out.Body.Status = "ok"
	out.Body.URL = tileURL
	return out, nil
}

// staticMapNil reports whether the static map generator is unconfigured,
// guarding against both interface-nil and a typed-nil *staticmap.Resolver.
func staticMapNil(g TileURLGenerator) bool {
	if g == nil {
		return true
	}
	if r, ok := g.(*staticmap.Resolver); ok && r == nil {
		return true
	}
	return false
}

// RegisterTileEndpoints registers all five geofence tile-URL endpoints on the
// shared huma instance, in place at the same /api/geofence/... paths. Each
// returns {status, url}; errors are problem+json.
func RegisterTileEndpoints(api huma.API, deps HumaTileDeps) {
	RegisterWeatherMap(api, deps)
	RegisterLocationMap(api, deps)
	RegisterDistanceMap(api, deps)
	RegisterOverviewMap(api, deps)
	RegisterGeofenceAreaMap(api, deps)
}

// latLonInput carries the lat/lon float path params shared by several tile ops.
type latLonInput struct {
	Lat float64 `path:"lat"`
	Lon float64 `path:"lon"`
}

// weatherMapInput is latLonInput plus the optional weather query override.
type weatherMapInput struct {
	Lat     float64 `path:"lat"`
	Lon     float64 `path:"lon"`
	Weather string  `query:"weather"`
}

// RegisterWeatherMap registers GET /api/geofence/weatherMap/{lat}/{lon}.
func RegisterWeatherMap(api huma.API, deps HumaTileDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-geofence-weather-map", Method: "GET", Path: "/geofence/weatherMap/{lat}/{lon}",
		Summary: "Weather S2 cell tile", Tags: []string{"tiles"},
		Description: "Generates a static-map tile showing the S2 weather cell covering the location (current weather unless overridden by the `weather` query). Returns {status, url} — the tileserver URL of the generated image. 503 when no static map provider is configured.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *weatherMapInput) (*tileURLOutput, error) {
		if staticMapNil(deps.StaticMap) {
			return nil, huma.Error503ServiceUnavailable("static map provider not configured")
		}

		// Weather condition from query param or look up from tracker.
		weatherID := 0
		if in.Weather != "" {
			weatherID, _ = strconv.Atoi(in.Weather)
		}
		if weatherID == 0 && deps.Weather != nil {
			cellID := tracker.GetWeatherCellID(in.Lat, in.Lon)
			weatherID = deps.Weather.GetCurrentWeatherInCell(cellID)
		}

		centerLat, centerLon := geo.GetCellCenter(in.Lat, in.Lon, 10)
		coords := geo.GetCellCoordsSlice(in.Lat, in.Lon, 10)

		data := map[string]any{
			"latitude":           centerLat,
			"longitude":          centerLon,
			"coords":             coords,
			"gameplay_condition": weatherID,
		}
		if deps.ImgUicons != nil && weatherID > 0 {
			data["imgUrl"] = deps.ImgUicons.WeatherIcon(weatherID)
		}

		return tileURLResult(deps.StaticMap.GetPregeneratedTileURL("weather", data, "staticMap"))
	})
}

// RegisterLocationMap registers GET /api/geofence/locationMap/{lat}/{lon}.
func RegisterLocationMap(api huma.API, deps HumaTileDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-geofence-location-map", Method: "GET", Path: "/geofence/locationMap/{lat}/{lon}",
		Summary: "Location pin tile", Tags: []string{"tiles"},
		Description: "Generates a static-map tile with a pin at the given coordinates (used to confirm location settings). Returns {status, url} — the tileserver URL of the generated image. 503 when no static map provider is configured.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *latLonInput) (*tileURLOutput, error) {
		if staticMapNil(deps.StaticMap) {
			return nil, huma.Error503ServiceUnavailable("static map provider not configured")
		}
		data := map[string]any{
			"latitude":  in.Lat,
			"longitude": in.Lon,
		}
		return tileURLResult(deps.StaticMap.GetPregeneratedTileURL("location", data, "staticMap"))
	})
}

// distanceMapInput carries lat/lon plus the distance float path param.
type distanceMapInput struct {
	Lat      float64 `path:"lat"`
	Lon      float64 `path:"lon"`
	Distance float64 `path:"distance"`
}

// RegisterDistanceMap registers GET /api/geofence/distanceMap/{lat}/{lon}/{distance}.
func RegisterDistanceMap(api huma.API, deps HumaTileDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-geofence-distance-map", Method: "GET", Path: "/geofence/distanceMap/{lat}/{lon}/{distance}",
		Summary: "Distance circle tile", Tags: []string{"tiles"},
		Description: "Generates a static-map tile showing a circle of the given radius (metres) around the coordinates (used to visualise distance-based tracking). Returns {status, url} — the tileserver URL of the generated image. 503 when no static map provider is configured.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *distanceMapInput) (*tileURLOutput, error) {
		if staticMapNil(deps.StaticMap) {
			return nil, huma.Error503ServiceUnavailable("static map provider not configured")
		}
		if in.Distance < 0 {
			return nil, huma.Error400BadRequest("invalid parameters")
		}

		pos := staticmap.Autoposition(staticmap.AutopositionShape{
			Circles: []staticmap.Circle{{Latitude: in.Lat, Longitude: in.Lon, RadiusM: in.Distance}},
		}, 500, 250, 1.25, 17.5)
		if pos == nil {
			return nil, huma.Error500InternalServerError("autoposition failed")
		}

		data := map[string]any{
			"zoom":      pos.Zoom,
			"latitude":  in.Lat,
			"longitude": in.Lon,
			"distance":  in.Distance,
		}
		return tileURLResult(deps.StaticMap.GetPregeneratedTileURL("distance", data, "staticMap"))
	})
}

// overviewMapInput carries the POST body {areas: []string}.
type overviewMapInput struct {
	Body struct {
		Areas []string `json:"areas"`
	}
}

// RegisterOverviewMap registers POST /api/geofence/overviewMap.
func RegisterOverviewMap(api huma.API, deps HumaTileDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "post-geofence-overview-map", Method: "POST", Path: "/geofence/overviewMap",
		Summary: "Multi-area overview tile", Tags: []string{"tiles"},
		Description: "Generates a single static-map tile overlaying the polygons of the geofence areas named in the request body. Returns {status, url} — the tileserver URL of the generated image. 503 when no static map provider is configured.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *overviewMapInput) (*tileURLOutput, error) {
		if staticMapNil(deps.StaticMap) {
			return nil, huma.Error503ServiceUnavailable("static map provider not configured")
		}
		if len(in.Body.Areas) == 0 {
			return nil, huma.Error400BadRequest("areas array required")
		}

		fences := deps.StateMgr.Fences()

		// Find matching fences preserving order.
		var matched []*geofence.Fence
		for _, name := range in.Body.Areas {
			if f := FindFence(fences, name); f != nil && len(FencePaths(f)) > 0 {
				matched = append(matched, f)
			}
		}
		if len(matched) == 0 {
			return nil, huma.Error404NotFound("no matching areas found")
		}

		// Build polygons for autoposition (flatten all paths from all fences).
		var autoPolygons [][]staticmap.LatLon
		for _, f := range matched {
			autoPolygons = append(autoPolygons, FenceAutopositionPolygons(FencePaths(f))...)
		}

		pos := staticmap.Autoposition(staticmap.AutopositionShape{
			Polygons: autoPolygons,
		}, 1024, 768, 1.25, 17.5)
		if pos == nil {
			return nil, huma.Error500InternalServerError("autoposition failed")
		}

		// Build flat list of colored polygons — multipath fences get multiple
		// entries with the same color.
		var tilePolygons []map[string]any
		for i, f := range matched {
			color := Rainbow(len(matched), i)
			for _, path := range FencePaths(f) {
				tilePolygons = append(tilePolygons, map[string]any{
					"color": color,
					"path":  path,
				})
			}
		}

		data := map[string]any{
			"zoom":      pos.Zoom,
			"latitude":  pos.Latitude,
			"longitude": pos.Longitude,
			"fences":    tilePolygons,
		}
		return tileURLResult(deps.StaticMap.GetPregeneratedTileURL("areaoverview", data, "staticMap"))
	})
}

// areaMapInput carries the area string path param.
type areaMapInput struct {
	Area string `path:"area"`
}

// RegisterGeofenceAreaMap registers GET /api/geofence/{area}/map.
func RegisterGeofenceAreaMap(api huma.API, deps HumaTileDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-geofence-area-map", Method: "GET", Path: "/geofence/{area}/map",
		Summary: "Geofence area tile", Tags: []string{"tiles"},
		Description: "Generates a static-map tile of the named geofence area's polygon. Returns {status, url} — the tileserver URL of the generated image. 404 for an unknown area; 503 when no static map provider is configured.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *areaMapInput) (*tileURLOutput, error) {
		if staticMapNil(deps.StaticMap) {
			return nil, huma.Error503ServiceUnavailable("static map provider not configured")
		}
		if in.Area == "" {
			return nil, huma.Error400BadRequest("area parameter required")
		}

		fence := FindFence(deps.StateMgr.Fences(), in.Area)
		if fence == nil {
			return nil, huma.Error404NotFound("area not found")
		}

		paths := FencePaths(fence)
		if len(paths) == 0 {
			return nil, huma.Error404NotFound("area has no polygon data")
		}

		pos := staticmap.Autoposition(staticmap.AutopositionShape{
			Polygons: FenceAutopositionPolygons(paths),
		}, 500, 250, 1.25, 17.5)
		if pos == nil {
			return nil, huma.Error500InternalServerError("autoposition failed")
		}

		data := map[string]any{
			"zoom":      pos.Zoom,
			"latitude":  pos.Latitude,
			"longitude": pos.Longitude,
			"polygons":  paths,
			"coords":    paths[0], // backward compat: first area for legacy templates using "coords"
		}
		return tileURLResult(deps.StaticMap.GetPregeneratedTileURL("area", data, "staticMap"))
	})
}
