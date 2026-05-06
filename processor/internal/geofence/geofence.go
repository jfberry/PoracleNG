package geofence

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// Fence represents a geofence area.
// UserSelectable and DisplayInMatches default to true when absent from JSON.
// We use a custom UnmarshalJSON to distinguish absent (nil → true) from explicit false.
type Fence struct {
	Name             string         `json:"name"`
	NormalizedName   string         `json:"-"` // lowercased, underscores replaced with spaces (computed at load)
	ID               int            `json:"id"`
	Color            string         `json:"color"`
	Path             [][2]float64   `json:"path,omitempty"`
	Multipath        [][][2]float64 `json:"multipath,omitempty"`
	Group            string         `json:"group"`
	Description      string         `json:"description"`
	UserSelectable   bool           `json:"userSelectable"`
	DisplayInMatches bool           `json:"displayInMatches"`
	// Properties holds user-defined keys from the source's properties block
	// that aren't mapped to a named field above. Used by the bulk-autocreate
	// runner's filter/params Handlebars expressions. Populated by
	// parseGeoJSON; left nil for native-format files.
	Properties map[string]any `json:"-"`
}

// fenceJSON is used for unmarshalling with *bool to detect absent vs explicit false.
type fenceJSON struct {
	Name             string         `json:"name"`
	ID               int            `json:"id"`
	Color            string         `json:"color"`
	Path             [][2]float64   `json:"path,omitempty"`
	Multipath        [][][2]float64 `json:"multipath,omitempty"`
	Group            string         `json:"group"`
	Description      string         `json:"description"`
	UserSelectable   *bool          `json:"userSelectable"`
	DisplayInMatches *bool          `json:"displayInMatches"`
}

// UnmarshalJSON handles defaulting UserSelectable and DisplayInMatches to true when absent.
func (f *Fence) UnmarshalJSON(data []byte) error {
	var raw fenceJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.Name = raw.Name
	f.ID = raw.ID
	f.Color = raw.Color
	f.Path = raw.Path
	f.Multipath = raw.Multipath
	f.Group = raw.Group
	f.Description = raw.Description
	f.UserSelectable = raw.UserSelectable == nil || *raw.UserSelectable
	f.DisplayInMatches = raw.DisplayInMatches == nil || *raw.DisplayInMatches
	return nil
}

// GeoJSON types for parsing FeatureCollection format.
type geoJSONCollection struct {
	Type     string           `json:"type"`
	Features []geoJSONFeature `json:"features"`
}

type geoJSONFeature struct {
	Type       string          `json:"type"`
	Geometry   geoJSONGeometry `json:"geometry"`
	Properties json.RawMessage `json:"properties"`
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
// defaultName is the prefix for unnamed fences (e.g. "city" → city1, city2).
func LoadGeofenceFile(path, defaultName string) ([]Fence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Strip JSON comments (// and /* */)
	cleaned := stripJSONComments(data)

	// Try GeoJSON first
	var collection geoJSONCollection
	if err := json.Unmarshal(cleaned, &collection); err == nil && collection.Type == "FeatureCollection" {
		return parseGeoJSON(collection, defaultName), nil
	}

	// Try Poracle native format — UnmarshalJSON handles defaulting
	// UserSelectable and DisplayInMatches to true when absent.
	var fences []Fence
	if err := json.Unmarshal(cleaned, &fences); err != nil {
		return nil, err
	}
	// Assign default names to unnamed fences
	prefix := defaultName
	if prefix == "" {
		prefix = "city"
	}
	unnamed := 0
	for i := range fences {
		if fences[i].Name == "" {
			unnamed++
			fences[i].Name = prefix + strconv.Itoa(unnamed)
		}
	}
	return fences, nil
}

func parseGeoJSON(collection geoJSONCollection, defaultName string) []Fence {
	// Set of property keys promoted to named struct fields. Anything else
	// in the source's properties block ends up in Fence.Properties for use
	// by bulk-autocreate filter/params expressions.
	namedKeys := map[string]bool{
		"name":             true,
		"color":            true,
		"group":            true,
		"description":      true,
		"userSelectable":   true,
		"displayInMatches": true,
	}

	var fences []Fence
	for i, feature := range collection.Features {
		if feature.Type != "Feature" {
			continue
		}

		// Pass 1: decode the named-field subset.
		var props geoJSONProperties
		if len(feature.Properties) > 0 {
			if err := json.Unmarshal(feature.Properties, &props); err != nil {
				continue
			}
		}

		// Pass 2: decode every property as a generic map, then strip the
		// named keys so they don't shadow the struct fields.
		var extras map[string]any
		if len(feature.Properties) > 0 {
			if err := json.Unmarshal(feature.Properties, &extras); err != nil {
				extras = nil
			}
			for k := range namedKeys {
				delete(extras, k)
			}
			if len(extras) == 0 {
				extras = nil
			}
		}

		name := props.Name
		if name == "" {
			prefix := defaultName
			if prefix == "" {
				prefix = "city"
			}
			name = prefix + strconv.Itoa(i+1)
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
			Properties:       extras,
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
