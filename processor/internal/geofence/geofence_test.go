package geofence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGeofenceFilePoracleFormat(t *testing.T) {
	content := `[
		{
			"name": "TestArea",
			"color": "#FF0000",
			"path": [
				[51.0, 0.0],
				[52.0, 0.0],
				[52.0, 1.0],
				[51.0, 1.0]
			]
		},
		{
			"name": "OtherArea",
			"path": [
				[40.0, -1.0],
				[41.0, -1.0],
				[41.0, 0.0],
				[40.0, 0.0]
			]
		}
	]`

	path := writeTempFile(t, content, "poracle_*.json")
	fences, err := LoadGeofenceFile(path, "")
	if err != nil {
		t.Fatalf("LoadGeofenceFile failed: %v", err)
	}

	if len(fences) != 2 {
		t.Fatalf("Expected 2 fences, got %d", len(fences))
	}

	if fences[0].Name != "TestArea" {
		t.Errorf("Expected name 'TestArea', got %q", fences[0].Name)
	}
	if len(fences[0].Path) != 4 {
		t.Errorf("Expected 4 points, got %d", len(fences[0].Path))
	}
	// Poracle format sets defaults
	if !fences[0].UserSelectable {
		t.Error("UserSelectable should default to true")
	}
	if !fences[0].DisplayInMatches {
		t.Error("DisplayInMatches should default to true")
	}
}

func TestLoadGeofenceFilePorFormatExplicitFalse(t *testing.T) {
	content := `[
		{
			"name": "Hidden",
			"userSelectable": false,
			"displayInMatches": false,
			"path": [[51.0, 0.0], [51.0, 1.0], [52.0, 1.0], [52.0, 0.0]]
		}
	]`

	path := writeTempFile(t, content, "poracle_false_*.json")
	fences, err := LoadGeofenceFile(path, "")
	if err != nil {
		t.Fatalf("LoadGeofenceFile failed: %v", err)
	}
	if len(fences) != 1 {
		t.Fatalf("Expected 1 fence, got %d", len(fences))
	}
	if fences[0].UserSelectable {
		t.Error("UserSelectable should be false when explicitly set")
	}
	if fences[0].DisplayInMatches {
		t.Error("DisplayInMatches should be false when explicitly set")
	}
}

func TestLoadGeofenceFileGeoJSON(t *testing.T) {
	content := `{
		"type": "FeatureCollection",
		"features": [
			{
				"type": "Feature",
				"properties": {
					"name": "GeoArea",
					"color": "#00FF00",
					"group": "TestGroup"
				},
				"geometry": {
					"type": "Polygon",
					"coordinates": [
						[
							[0.0, 51.0],
							[1.0, 51.0],
							[1.0, 52.0],
							[0.0, 52.0],
							[0.0, 51.0]
						]
					]
				}
			}
		]
	}`

	path := writeTempFile(t, content, "geojson_*.json")
	fences, err := LoadGeofenceFile(path, "")
	if err != nil {
		t.Fatalf("LoadGeofenceFile failed: %v", err)
	}

	if len(fences) != 1 {
		t.Fatalf("Expected 1 fence, got %d", len(fences))
	}

	f := fences[0]
	if f.Name != "GeoArea" {
		t.Errorf("Expected name 'GeoArea', got %q", f.Name)
	}
	if f.Group != "TestGroup" {
		t.Errorf("Expected group 'TestGroup', got %q", f.Group)
	}
	// GeoJSON coords are [lon, lat] but should be converted to [lat, lon]
	if len(f.Path) < 4 {
		t.Fatalf("Expected at least 4 path points, got %d", len(f.Path))
	}
	if f.Path[0][0] != 51.0 || f.Path[0][1] != 0.0 {
		t.Errorf("Expected first point [51.0, 0.0] (lat, lon), got %v", f.Path[0])
	}
	if !f.UserSelectable {
		t.Error("UserSelectable should default to true")
	}
	if !f.DisplayInMatches {
		t.Error("DisplayInMatches should default to true")
	}
}

func TestLoadGeofenceFileGeoJSONMultiPolygon(t *testing.T) {
	content := `{
		"type": "FeatureCollection",
		"features": [
			{
				"type": "Feature",
				"properties": {
					"name": "MultiArea"
				},
				"geometry": {
					"type": "MultiPolygon",
					"coordinates": [
						[
							[[0.0, 51.0], [1.0, 51.0], [1.0, 52.0], [0.0, 52.0], [0.0, 51.0]]
						],
						[
							[[5.0, 51.0], [6.0, 51.0], [6.0, 52.0], [5.0, 52.0], [5.0, 51.0]]
						]
					]
				}
			}
		]
	}`

	path := writeTempFile(t, content, "multipolygon_*.json")
	fences, err := LoadGeofenceFile(path, "")
	if err != nil {
		t.Fatalf("LoadGeofenceFile failed: %v", err)
	}

	if len(fences) != 1 {
		t.Fatalf("Expected 1 fence, got %d", len(fences))
	}
	f := fences[0]
	if f.Name != "MultiArea" {
		t.Errorf("Expected name 'MultiArea', got %q", f.Name)
	}
	if len(f.Multipath) != 2 {
		t.Errorf("Expected 2 multipath polygons, got %d", len(f.Multipath))
	}
	// First polygon first point: [lon=0, lat=51] -> [lat=51, lon=0]
	if f.Multipath[0][0][0] != 51.0 || f.Multipath[0][0][1] != 0.0 {
		t.Errorf("Expected multipath[0][0] = [51.0, 0.0], got %v", f.Multipath[0][0])
	}
}

func TestLoadGeofenceFileGeoJSONProperties(t *testing.T) {
	falseVal := false
	_ = falseVal

	content := `{
		"type": "FeatureCollection",
		"features": [
			{
				"type": "Feature",
				"properties": {
					"name": "NoDisplay",
					"userSelectable": false,
					"displayInMatches": false
				},
				"geometry": {
					"type": "Polygon",
					"coordinates": [[[0,51],[1,51],[1,52],[0,52],[0,51]]]
				}
			}
		]
	}`

	path := writeTempFile(t, content, "props_*.json")
	fences, err := LoadGeofenceFile(path, "")
	if err != nil {
		t.Fatalf("LoadGeofenceFile failed: %v", err)
	}

	f := fences[0]
	if f.UserSelectable {
		t.Error("UserSelectable should be false")
	}
	if f.DisplayInMatches {
		t.Error("DisplayInMatches should be false")
	}
}

func TestLoadGeofenceFileGeoJSONNoName(t *testing.T) {
	content := `{
		"type": "FeatureCollection",
		"features": [
			{
				"type": "Feature",
				"properties": {},
				"geometry": {
					"type": "Polygon",
					"coordinates": [[[0,51],[1,51],[1,52],[0,52],[0,51]]]
				}
			}
		]
	}`

	path := writeTempFile(t, content, "noname_*.json")
	fences, err := LoadGeofenceFile(path, "")
	if err != nil {
		t.Fatalf("LoadGeofenceFile failed: %v", err)
	}

	if fences[0].Name == "" {
		t.Error("Nameless feature should get a default name")
	}
}

func TestLoadGeofenceFileWithComments(t *testing.T) {
	content := `[
		// This is a line comment
		{
			"name": "Commented",
			/* block comment */
			"path": [
				[51.0, 0.0],
				[52.0, 0.0],
				[52.0, 1.0]
			]
		}
	]`

	path := writeTempFile(t, content, "comments_*.json")
	fences, err := LoadGeofenceFile(path, "")
	if err != nil {
		t.Fatalf("LoadGeofenceFile with comments failed: %v", err)
	}
	if len(fences) != 1 || fences[0].Name != "Commented" {
		t.Errorf("Expected 1 fence named 'Commented', got %v", fences)
	}
}

func TestLoadGeofenceFileNotFound(t *testing.T) {
	_, err := LoadGeofenceFile("/nonexistent/file.json", "")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestLoadGeofenceFileInvalidJSON(t *testing.T) {
	path := writeTempFile(t, "not valid json {{{", "invalid_*.json")
	_, err := LoadGeofenceFile(path, "")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// Test loading the real geofence.json file
func TestLoadRealGeofenceFile(t *testing.T) {
	realPath := "../../../../alerter/config/geofence.json"
	if _, err := os.Stat(realPath); os.IsNotExist(err) {
		t.Skip("Real geofence file not found, skipping")
	}

	fences, err := LoadGeofenceFile(realPath, "")
	if err != nil {
		t.Fatalf("Failed to load real geofence: %v", err)
	}

	if len(fences) < 10 {
		t.Errorf("Expected at least 10 fences in real file, got %d", len(fences))
	}

	// Check Canterbury is present and has the right shape
	var canterbury *Fence
	for i := range fences {
		if fences[i].Name == "Canterbury" {
			canterbury = &fences[i]
			break
		}
	}
	if canterbury == nil {
		t.Fatal("Canterbury fence not found in real file")
	}
	if len(canterbury.Path) != 4 {
		t.Errorf("Canterbury should have 4 points, got %d", len(canterbury.Path))
	}

	// Build spatial index and verify real coordinates
	si := NewSpatialIndex(fences)

	// UKC campus should be in Canterbury + ukc
	areas := si.PointInAreas(51.297267, 1.069734)
	names := make(map[string]bool)
	for _, a := range areas {
		names[a.Name] = true
	}
	if !names["Canterbury"] {
		t.Error("UKC point should be in Canterbury")
	}
	if !names["ukc"] {
		t.Error("UKC point should be in ukc")
	}

	// Faversham gym
	areas = si.PointInAreas(51.310916, 0.877440)
	names = make(map[string]bool)
	for _, a := range areas {
		names[a.Name] = true
	}
	if !names["Faversham"] {
		t.Error("King George gym should be in Faversham")
	}
	if names["Canterbury"] {
		t.Error("King George gym should not be in Canterbury")
	}
}

func TestStripJSONComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			"line comment",
			`{"a": 1} // comment`,
			`{"a": 1} `,
		},
		{
			"block comment",
			`{"a": /* comment */ 1}`,
			`{"a":  1}`,
		},
		{
			"comment in string preserved",
			`{"a": "// not a comment"}`,
			`{"a": "// not a comment"}`,
		},
		{
			"no comments",
			`{"a": 1}`,
			`{"a": 1}`,
		},
		{
			"escaped quote in string",
			`{"a": "he said \"hello\""}`,
			`{"a": "he said \"hello\""}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(stripJSONComments([]byte(tt.input)))
			if got != tt.expected {
				t.Errorf("stripJSONComments(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLoadAllGeofences(t *testing.T) {
	file1 := writeTempFile(t, `[{"name": "A", "path": [[51,0],[52,0],[52,1],[51,1]]}]`, "fence1_*.json")
	file2 := writeTempFile(t, `[{"name": "B", "path": [[40,0],[41,0],[41,1],[40,1]]}]`, "fence2_*.json")

	si, fences, err := LoadAllGeofences([]string{file1, file2}, "", "")
	if err != nil {
		t.Fatalf("LoadAllGeofences failed: %v", err)
	}

	if len(fences) != 2 {
		t.Errorf("Expected 2 fences, got %d", len(fences))
	}

	// Point in fence A
	areas := si.PointInAreas(51.5, 0.5)
	if len(areas) != 1 || areas[0].Name != "A" {
		t.Errorf("Expected area A, got %v", areaNames(areas))
	}

	// Point in fence B
	areas = si.PointInAreas(40.5, 0.5)
	if len(areas) != 1 || areas[0].Name != "B" {
		t.Errorf("Expected area B, got %v", areaNames(areas))
	}
}

func TestLoadAllGeofencesFileNotFound(t *testing.T) {
	_, _, err := LoadAllGeofences([]string{"/nonexistent.json"}, "", "")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func writeTempFile(t *testing.T, content, pattern string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, pattern)
	// Replace glob pattern with a fixed name
	path = filepath.Join(dir, "test.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
