package geofence

import (
	"encoding/json"
	"os"
	"strings"
)

// Fence represents a geofence area.
type Fence struct {
	Name             string         `json:"name"`
	ID               int            `json:"id"`
	Color            string         `json:"color"`
	Path             [][2]float64   `json:"path,omitempty"`
	Multipath        [][][2]float64 `json:"multipath,omitempty"`
	Group            string         `json:"group"`
	Description      string         `json:"description"`
	UserSelectable   bool           `json:"userSelectable"`
	DisplayInMatches bool           `json:"displayInMatches"`
}

// GeoJSON types for parsing FeatureCollection format.
type geoJSONCollection struct {
	Type     string           `json:"type"`
	Features []geoJSONFeature `json:"features"`
}

type geoJSONFeature struct {
	Type       string            `json:"type"`
	Geometry   geoJSONGeometry   `json:"geometry"`
	Properties geoJSONProperties `json:"properties"`
}

type geoJSONGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

type geoJSONProperties struct {
	Name             string `json:"name"`
	Color            string `json:"color"`
	Group            string `json:"group"`
	Description      string `json:"description"`
	UserSelectable   *bool  `json:"userSelectable"`
	DisplayInMatches *bool  `json:"displayInMatches"`
}

// LoadGeofenceFile loads a geofence file (Poracle JSON or GeoJSON format).
func LoadGeofenceFile(path string) ([]Fence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Strip JSON comments (// and /* */)
	cleaned := stripJSONComments(data)

	// Try GeoJSON first
	var collection geoJSONCollection
	if err := json.Unmarshal(cleaned, &collection); err == nil && collection.Type == "FeatureCollection" {
		return parseGeoJSON(collection), nil
	}

	// Try Poracle native format
	var fences []Fence
	if err := json.Unmarshal(cleaned, &fences); err != nil {
		return nil, err
	}
	// Set defaults
	for i := range fences {
		fences[i].UserSelectable = true
		fences[i].DisplayInMatches = true
	}
	return fences, nil
}

func parseGeoJSON(collection geoJSONCollection) []Fence {
	var fences []Fence
	for i, feature := range collection.Features {
		if feature.Type != "Feature" {
			continue
		}
		props := feature.Properties
		name := props.Name
		if name == "" {
			name = "geofence" + string(rune('0'+i))
		}
		userSel := true
		if props.UserSelectable != nil {
			userSel = *props.UserSelectable
		}
		dispMatch := true
		if props.DisplayInMatches != nil {
			dispMatch = *props.DisplayInMatches
		}

		fence := Fence{
			Name:             name,
			ID:               i,
			Color:            props.Color,
			Group:            props.Group,
			Description:      props.Description,
			UserSelectable:   userSel,
			DisplayInMatches: dispMatch,
		}

		switch feature.Geometry.Type {
		case "Polygon":
			var coords [][][2]float64
			if err := json.Unmarshal(feature.Geometry.Coordinates, &coords); err == nil && len(coords) > 0 {
				// GeoJSON coords are [lon, lat], convert to [lat, lon]
				path := make([][2]float64, len(coords[0]))
				for j, c := range coords[0] {
					path[j] = [2]float64{c[1], c[0]}
				}
				fence.Path = path
			}
		case "MultiPolygon":
			var coords [][][][2]float64
			if err := json.Unmarshal(feature.Geometry.Coordinates, &coords); err == nil {
				for _, poly := range coords {
					if len(poly) > 0 {
						path := make([][2]float64, len(poly[0]))
						for j, c := range poly[0] {
							path[j] = [2]float64{c[1], c[0]}
						}
						fence.Multipath = append(fence.Multipath, path)
					}
				}
			}
		}
		fences = append(fences, fence)
	}
	return fences
}

// stripJSONComments removes // and /* */ comments from JSON bytes.
func stripJSONComments(data []byte) []byte {
	s := string(data)
	var result strings.Builder
	result.Grow(len(s))
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '/' {
			// Line comment - skip to newline
			for i < len(s) && s[i] != '\n' {
				i++
			}
		} else if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			// Block comment - skip to */
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			}
		} else if s[i] == '"' {
			// String literal - copy as-is
			result.WriteByte(s[i])
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					result.WriteByte(s[i])
					i++
				}
				result.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				result.WriteByte(s[i])
				i++
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return []byte(result.String())
}
