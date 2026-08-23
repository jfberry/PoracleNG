package api

import (
	"fmt"
	"math"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
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
