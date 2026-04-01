package api

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/uicons"
)

// TileDeps holds dependencies for tile generation endpoints.
type TileDeps struct {
	StaticMap *staticmap.Resolver
	StateMgr  *state.Manager
	ImgUicons *uicons.Uicons
	Weather   *tracker.WeatherTracker
}

func tileJSONOK(c *gin.Context, tileURL string) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "url": tileURL})
}

func tileJSONError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"status": "error", "message": msg})
}

// FindFence finds a fence by name (case-insensitive, underscore-normalized).
func FindFence(fences []geofence.Fence, name string) *geofence.Fence {
	normalized := strings.ToLower(strings.ReplaceAll(name, "_", " "))
	for i := range fences {
		if fences[i].NormalizedName == normalized {
			return &fences[i]
		}
	}
	return nil
}

// FencePaths returns all polygon paths for a fence (single or multipath).
func FencePaths(f *geofence.Fence) [][][2]float64 {
	if len(f.Multipath) > 0 {
		return f.Multipath
	}
	if len(f.Path) > 0 {
		return [][][2]float64{f.Path}
	}
	return nil
}

// FenceAutopositionPolygons converts fence paths to LatLon polygons for autoposition.
func FenceAutopositionPolygons(paths [][][2]float64) [][]staticmap.LatLon {
	polygons := make([][]staticmap.LatLon, len(paths))
	for i, path := range paths {
		polygon := make([]staticmap.LatLon, len(path))
		for j, p := range path {
			polygon[j] = staticmap.LatLon{Latitude: p[0], Longitude: p[1]}
		}
		polygons[i] = polygon
	}
	return polygons
}

// HandleGeofenceAreaMap returns a tile of a single geofence area polygon.
// GET /api/geofence/{area}/map
func HandleGeofenceAreaMap(deps TileDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.StaticMap == nil {
			tileJSONError(c, http.StatusServiceUnavailable, "static map provider not configured")
			return
		}

		areaName, _ := url.PathUnescape(c.Param("area"))
		if areaName == "" {
			tileJSONError(c, http.StatusBadRequest, "area parameter required")
			return
		}

		st := deps.StateMgr.Get()
		fence := FindFence(st.Fences, areaName)
		if fence == nil {
			tileJSONError(c, http.StatusNotFound, "area not found")
			return
		}

		paths := FencePaths(fence)
		if len(paths) == 0 {
			tileJSONError(c, http.StatusNotFound, "area has no polygon data")
			return
		}

		pos := staticmap.Autoposition(staticmap.AutopositionShape{
			Polygons: FenceAutopositionPolygons(paths),
		}, 500, 250, 1.25, 17.5)

		if pos == nil {
			tileJSONError(c, http.StatusInternalServerError, "autoposition failed")
			return
		}

		data := map[string]any{
			"zoom":      pos.Zoom,
			"latitude":  pos.Latitude,
			"longitude": pos.Longitude,
			"polygons":  paths,
		}

		tileURL := deps.StaticMap.GetPregeneratedTileURL("area", data, "staticMap")
		tileJSONOK(c, tileURL)
	}
}

// HandleDistanceMap returns a tile showing a distance circle.
// GET /api/geofence/distanceMap/{lat}/{lon}/{distance}
func HandleDistanceMap(deps TileDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.StaticMap == nil {
			tileJSONError(c, http.StatusServiceUnavailable, "static map provider not configured")
			return
		}

		lat, err1 := strconv.ParseFloat(c.Param("lat"), 64)
		lon, err2 := strconv.ParseFloat(c.Param("lon"), 64)
		distance, err3 := strconv.ParseFloat(c.Param("distance"), 64)
		if err1 != nil || err2 != nil || err3 != nil || distance < 0 {
			tileJSONError(c, http.StatusBadRequest, "invalid parameters")
			return
		}

		pos := staticmap.Autoposition(staticmap.AutopositionShape{
			Circles: []staticmap.Circle{{Latitude: lat, Longitude: lon, RadiusM: distance}},
		}, 500, 250, 1.25, 17.5)

		if pos == nil {
			tileJSONError(c, http.StatusInternalServerError, "autoposition failed")
			return
		}

		data := map[string]any{
			"zoom":      pos.Zoom,
			"latitude":  lat,
			"longitude": lon,
			"distance":  distance,
		}

		tileURL := deps.StaticMap.GetPregeneratedTileURL("distance", data, "staticMap")
		tileJSONOK(c, tileURL)
	}
}

// HandleLocationMap returns a tile showing a location pin.
// GET /api/geofence/locationMap/{lat}/{lon}
func HandleLocationMap(deps TileDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.StaticMap == nil {
			tileJSONError(c, http.StatusServiceUnavailable, "static map provider not configured")
			return
		}

		lat, err1 := strconv.ParseFloat(c.Param("lat"), 64)
		lon, err2 := strconv.ParseFloat(c.Param("lon"), 64)
		if err1 != nil || err2 != nil {
			tileJSONError(c, http.StatusBadRequest, "invalid parameters")
			return
		}

		data := map[string]any{
			"latitude":  lat,
			"longitude": lon,
		}

		tileURL := deps.StaticMap.GetPregeneratedTileURL("location", data, "staticMap")
		tileJSONOK(c, tileURL)
	}
}

// HandleOverviewMap returns a tile showing multiple geofence areas with rainbow colors.
// POST /api/geofence/overviewMap  body: {"areas": ["area1", "area2"]}
func HandleOverviewMap(deps TileDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.StaticMap == nil {
			tileJSONError(c, http.StatusServiceUnavailable, "static map provider not configured")
			return
		}

		var body struct {
			Areas []string `json:"areas"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || len(body.Areas) == 0 {
			tileJSONError(c, http.StatusBadRequest, "areas array required")
			return
		}

		st := deps.StateMgr.Get()

		// Find matching fences preserving order
		var fences []*geofence.Fence
		for _, name := range body.Areas {
			if f := FindFence(st.Fences, name); f != nil && len(FencePaths(f)) > 0 {
				fences = append(fences, f)
			}
		}
		if len(fences) == 0 {
			tileJSONError(c, http.StatusNotFound, "no matching areas found")
			return
		}

		// Build polygons for autoposition (flatten all paths from all fences)
		var autoPolygons [][]staticmap.LatLon
		for _, f := range fences {
			autoPolygons = append(autoPolygons, FenceAutopositionPolygons(FencePaths(f))...)
		}

		pos := staticmap.Autoposition(staticmap.AutopositionShape{
			Polygons: autoPolygons,
		}, 1024, 768, 1.25, 17.5)

		if pos == nil {
			tileJSONError(c, http.StatusInternalServerError, "autoposition failed")
			return
		}

		// Build flat list of colored polygons — multipath fences get multiple entries with the same color
		var tilePolygons []map[string]any
		for i, f := range fences {
			color := Rainbow(len(fences), i)
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

		tileURL := deps.StaticMap.GetPregeneratedTileURL("areaoverview", data, "staticMap")
		tileJSONOK(c, tileURL)
	}
}

// HandleWeatherMap returns a tile showing a weather S2 cell.
// GET /api/geofence/weatherMap/{lat}/{lon}
func HandleWeatherMap(deps TileDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.StaticMap == nil {
			tileJSONError(c, http.StatusServiceUnavailable, "static map provider not configured")
			return
		}

		lat, err1 := strconv.ParseFloat(c.Param("lat"), 64)
		lon, err2 := strconv.ParseFloat(c.Param("lon"), 64)
		if err1 != nil || err2 != nil {
			tileJSONError(c, http.StatusBadRequest, "invalid parameters")
			return
		}

		// Get weather condition from query param or look up from tracker
		weatherID := 0
		if qw := c.Query("weather"); qw != "" {
			weatherID, _ = strconv.Atoi(qw)
		}
		if weatherID == 0 && deps.Weather != nil {
			cellID := tracker.GetWeatherCellID(lat, lon)
			weatherID = deps.Weather.GetCurrentWeatherInCell(cellID)
		}

		// S2 cell center and corners at level 10
		centerLat, centerLon := geo.GetCellCenter(lat, lon, 10)
		coords := geo.GetCellCoordsSlice(lat, lon, 10)

		data := map[string]any{
			"latitude":           centerLat,
			"longitude":          centerLon,
			"coords":             coords,
			"gameplay_condition": weatherID,
		}

		// Add weather icon if available
		if deps.ImgUicons != nil && weatherID > 0 {
			data["imgUrl"] = deps.ImgUicons.WeatherIcon(weatherID)
		}

		tileURL := deps.StaticMap.GetPregeneratedTileURL("weather", data, "staticMap")
		tileJSONOK(c, tileURL)
	}
}

// HandleGeofenceAll returns all geofence data.
// GET /api/geofence/all
func HandleGeofenceAll(stateMgr *state.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		st := stateMgr.Get()
		c.JSON(http.StatusOK, gin.H{
			"status":   "ok",
			"geofence": st.Fences,
		})
	}
}

// HandleGeofenceHash returns MD5 hashes of each geofence path.
// GET /api/geofence/all/hash
func HandleGeofenceHash(stateMgr *state.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		st := stateMgr.Get()
		areas := make(map[string]string, len(st.Fences))
		for _, f := range st.Fences {
			pathJSON, _ := json.Marshal(f.Path)
			areas[f.Name] = fmt.Sprintf("%x", md5.Sum(pathJSON))
		}
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"areas":  areas,
		})
	}
}

// HandleGeofenceGeoJSON returns geofences as a GeoJSON FeatureCollection.
// GET /api/geofence/all/geojson
func HandleGeofenceGeoJSON(stateMgr *state.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		st := stateMgr.Get()

		features := make([]map[string]any, 0, len(st.Fences))
		for _, f := range st.Fences {
			properties := map[string]any{
				"name":             f.Name,
				"color":            f.Color,
				"id":               f.ID,
				"group":            f.Group,
				"description":      f.Description,
				"userSelectable":   f.UserSelectable,
				"displayInMatches": f.DisplayInMatches,
			}

			var geomType string
			var coordinates any

			if len(f.Multipath) > 0 {
				geomType = "MultiPolygon"
				// GeoJSON MultiPolygon: [ [ [ring] ], [ [ring] ], ... ]
				multiCoords := make([][][][2]float64, len(f.Multipath))
				for i, subpath := range f.Multipath {
					ring := make([][2]float64, len(subpath))
					for j, coord := range subpath {
						ring[j] = [2]float64{coord[1], coord[0]} // GeoJSON is [lon, lat]
					}
					if len(ring) > 0 && ring[len(ring)-1] != ring[0] {
						ring = append(ring, ring[0])
					}
					multiCoords[i] = [][][2]float64{ring}
				}
				coordinates = multiCoords
			} else {
				geomType = "Polygon"
				ring := make([][2]float64, len(f.Path))
				for i, coord := range f.Path {
					ring[i] = [2]float64{coord[1], coord[0]} // GeoJSON is [lon, lat]
				}
				if len(ring) > 0 && ring[len(ring)-1] != ring[0] {
					ring = append(ring, ring[0])
				}
				coordinates = [][][2]float64{ring}
			}

			features = append(features, map[string]any{
				"type":       "Feature",
				"properties": properties,
				"geometry": map[string]any{
					"type":        geomType,
					"coordinates": coordinates,
				},
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"geoJSON": map[string]any{
				"type":     "FeatureCollection",
				"features": features,
			},
		})
	}
}

// rainbow generates evenly-spaced vibrant colours for distinguishing areas.
// Ported from the JS geofenceTileGenerator.
func Rainbow(numSteps, step int) string {
	h := float64(step) / float64(numSteps)
	i := int(h * 6)
	f := h*6 - float64(i)
	q := 1 - f

	var r, g, b float64
	switch i % 6 {
	case 0:
		r, g, b = 1, f, 0
	case 1:
		r, g, b = q, 1, 0
	case 2:
		r, g, b = 0, 1, f
	case 3:
		r, g, b = 0, q, 1
	case 4:
		r, g, b = f, 0, 1
	case 5:
		r, g, b = 1, 0, q
	}

	return fmt.Sprintf("#%02x%02x%02x",
		int(math.Round(r*255)),
		int(math.Round(g*255)),
		int(math.Round(b*255)))
}
