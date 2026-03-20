package geofence

import (
	"path/filepath"
	"strings"

	"github.com/tidwall/rtree"
)

// MatchedArea represents an area a point falls within.
type MatchedArea struct {
	Name             string
	Description      string
	DisplayInMatches bool
	Group            string
}

// SpatialIndex provides fast point-in-geofence lookups using an R-tree.
type SpatialIndex struct {
	tree   rtree.RTreeG[*fenceEntry]
	fences []Fence
}

type fenceEntry struct {
	fence *Fence
	path  [][2]float64 // the specific polygon path (for multipath fences, one entry per sub-polygon)
}

// NewSpatialIndex builds an R-tree from the given fences.
func NewSpatialIndex(fences []Fence) *SpatialIndex {
	si := &SpatialIndex{fences: fences}
	for i := range fences {
		f := &fences[i]
		f.NormalizedName = strings.ToLower(strings.ReplaceAll(f.Name, "_", " "))
		if len(f.Path) > 0 {
			minX, minY, maxX, maxY := boundingBox(f.Path)
			entry := &fenceEntry{fence: f, path: f.Path}
			si.tree.Insert([2]float64{minX, minY}, [2]float64{maxX, maxY}, entry)
		}
		for _, mp := range f.Multipath {
			if len(mp) > 0 {
				minX, minY, maxX, maxY := boundingBox(mp)
				entry := &fenceEntry{fence: f, path: mp}
				si.tree.Insert([2]float64{minX, minY}, [2]float64{maxX, maxY}, entry)
			}
		}
	}
	return si
}

// PointInAreas returns all geofence areas that contain the given point.
func (si *SpatialIndex) PointInAreas(lat, lon float64) []MatchedArea {
	var results []MatchedArea
	seen := make(map[string]bool)

	si.tree.Search(
		[2]float64{lat, lon},
		[2]float64{lat, lon},
		func(min, max [2]float64, entry *fenceEntry) bool {
			if !seen[entry.fence.Name] && PointInPolygon(lat, lon, entry.path) {
				seen[entry.fence.Name] = true
				results = append(results, MatchedArea{
					Name:             entry.fence.Name,
					Description:      entry.fence.Description,
					DisplayInMatches: entry.fence.DisplayInMatches,
					Group:            entry.fence.Group,
				})
			}
			return true // continue searching
		},
	)
	return results
}

// MatchedAreaNames returns a set of normalized area names for a point.
func (si *SpatialIndex) MatchedAreaNames(lat, lon float64) map[string]bool {
	names := make(map[string]bool)
	seen := make(map[string]bool)

	si.tree.Search(
		[2]float64{lat, lon},
		[2]float64{lat, lon},
		func(min, max [2]float64, entry *fenceEntry) bool {
			if !seen[entry.fence.Name] && PointInPolygon(lat, lon, entry.path) {
				seen[entry.fence.Name] = true
				names[entry.fence.NormalizedName] = true
			}
			return true
		},
	)
	return names
}

func boundingBox(path [][2]float64) (minX, minY, maxX, maxY float64) {
	minX = path[0][0]
	minY = path[0][1]
	maxX = path[0][0]
	maxY = path[0][1]
	for _, p := range path[1:] {
		if p[0] < minX {
			minX = p[0]
		}
		if p[0] > maxX {
			maxX = p[0]
		}
		if p[1] < minY {
			minY = p[1]
		}
		if p[1] > maxY {
			maxY = p[1]
		}
	}
	return
}

// LoadAllGeofences loads all geofence files from the given paths and builds a spatial index.
// HTTP/HTTPS URLs are resolved to their cached file paths in cacheDir.
func LoadAllGeofences(paths []string, cacheDir string) (*SpatialIndex, []Fence, error) {
	if cacheDir == "" {
		cacheDir = "config/.cache/geofences"
	}
	var allFences []Fence
	for _, p := range paths {
		filePath := p
		if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
			filePath = filepath.Join(cacheDir, sanitizeURL(p)+".json")
		}
		fences, err := LoadGeofenceFile(filePath)
		if err != nil {
			return nil, nil, err
		}
		allFences = append(allFences, fences...)
	}
	return NewSpatialIndex(allFences), allFences, nil
}
