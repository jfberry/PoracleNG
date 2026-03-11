package geofence

import (
	"sort"
	"testing"
)

// Build a SpatialIndex from the real Canterbury-area fences
func makeRealSpatialIndex() *SpatialIndex {
	fences := []Fence{
		{
			Name:             "Canterbury",
			DisplayInMatches: true,
			Path: [][2]float64{
				{51.3128980839251, 1.0079984211496},
				{51.3150440308209, 1.1333112263253},
				{51.2295578271973, 1.1484174274972},
				{51.2394462717341, 1.0145215534738},
			},
		},
		{
			Name:             "ukc",
			DisplayInMatches: true,
			Path: [][2]float64{
				{51.294283219014, 1.0543049227321},
				{51.3001867254262, 1.0488546740139},
				{51.3015014939356, 1.073187674197},
				{51.2961616897663, 1.0771358858669},
				{51.290606560982, 1.065806234988},
			},
		},
		{
			Name:             "Faversham",
			DisplayInMatches: true,
			Path: [][2]float64{
				{51.313276408389, 0.8415363318773},
				{51.3316208635379, 0.8496044165941},
				{51.3334441803482, 0.8748386390062},
				{51.3310845800883, 0.8945796973558},
				{51.3222886365392, 0.9038494117112},
				{51.3123107075019, 0.9067676551195},
				{51.3017939821503, 0.9009311683031},
				{51.3051746209927, 0.8698604590746},
			},
		},
		{
			Name:             "StDunstans",
			DisplayInMatches: true,
			Path: [][2]float64{
				{51.2901738413754, 1.0553879703472},
				{51.2934367775791, 1.0554522587103},
				{51.2933965062454, 1.0680849760776},
				{51.2884557427236, 1.079218652789},
				{51.2815612093296, 1.0831668644589},
				{51.2793431199283, 1.0822073020263},
				{51.2801889379506, 1.0748834025283},
				{51.2808735023069, 1.0690040803337},
				{51.283971001766, 1.0596037673285},
				{51.2856624361082, 1.0581340574082},
			},
		},
		{
			Name:             "Dover Road",
			DisplayInMatches: true,
			Path: [][2]float64{
				{51.2652736685765, 1.0826203710766},
				{51.2657839428006, 1.0780069543649},
				{51.2778619697755, 1.0807427716926},
				{51.2786943296571, 1.0896254437965},
				{51.2672651974037, 1.0955904814125},
			},
		},
	}
	return NewSpatialIndex(fences)
}

func areaNames(areas []MatchedArea) []string {
	names := make([]string, len(areas))
	for i, a := range areas {
		names[i] = a.Name
	}
	sort.Strings(names)
	return names
}

func TestSpatialIndexPointInAreas(t *testing.T) {
	si := makeRealSpatialIndex()

	tests := []struct {
		name     string
		lat      float64
		lon      float64
		expected []string // sorted area names
	}{
		// Real webhook: quest potion at UKC campus
		{
			"quest potion - Canterbury+ukc",
			51.297267, 1.069734,
			[]string{"Canterbury", "ukc"},
		},
		// Real webhook: quest squirtle near StDunstans
		{
			"quest squirtle - Canterbury+StDunstans",
			51.282747, 1.063537,
			[]string{"Canterbury", "StDunstans"},
		},
		// Real webhook: lure at UKC
		{
			"lure 501 - Canterbury+ukc",
			51.293981, 1.063606,
			[]string{"Canterbury", "ukc"},
		},
		// Real webhook: gym in Faversham
		{
			"gym king george - Faversham only",
			51.310916, 0.877440,
			[]string{"Faversham"},
		},
		// Real webhook: raid at Abbots Barton
		{
			"raid abbots barton - Canterbury+Dover Road",
			51.272513, 1.089742,
			[]string{"Canterbury", "Dover Road"},
		},
		// Real webhook: quest archen - Canterbury only (not UKC)
		{
			"quest archen - Canterbury only",
			51.302032, 1.054028,
			[]string{"Canterbury"},
		},
		// Real webhook: quest stardust - outside all fences
		{
			"quest stardust - outside all",
			51.311853, 1.193484,
			nil,
		},
		// Completely outside any fence
		{
			"London - nowhere near",
			51.5074, -0.1278,
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			areas := si.PointInAreas(tt.lat, tt.lon)
			got := areaNames(areas)

			expected := tt.expected
			if expected == nil {
				expected = []string{}
			}
			if got == nil {
				got = []string{}
			}

			if len(got) != len(expected) {
				t.Errorf("PointInAreas(%f, %f) = %v, want %v", tt.lat, tt.lon, got, expected)
				return
			}
			for i := range got {
				if got[i] != expected[i] {
					t.Errorf("PointInAreas(%f, %f) = %v, want %v", tt.lat, tt.lon, got, expected)
					return
				}
			}
		})
	}
}

func TestMatchedAreaNames(t *testing.T) {
	si := makeRealSpatialIndex()

	// UKC campus point - should get lowercased names
	names := si.MatchedAreaNames(51.297267, 1.069734)

	sort.Strings(names)
	if len(names) != 2 {
		t.Fatalf("Expected 2 area names, got %d: %v", len(names), names)
	}
	if names[0] != "canterbury" {
		t.Errorf("Expected 'canterbury', got %q", names[0])
	}
	if names[1] != "ukc" {
		t.Errorf("Expected 'ukc', got %q", names[1])
	}
}

func TestMatchedAreaNamesUnderscoreReplacement(t *testing.T) {
	fences := []Fence{
		{
			Name:             "Dover_Road",
			DisplayInMatches: true,
			Path: [][2]float64{
				{50.0, -1.0}, {52.0, -1.0}, {52.0, 1.0}, {50.0, 1.0},
			},
		},
	}
	si := NewSpatialIndex(fences)

	names := si.MatchedAreaNames(51.0, 0.0)
	if len(names) != 1 || names[0] != "dover road" {
		t.Errorf("Expected 'dover road', got %v", names)
	}
}

func TestMatchedAreaNamesOutside(t *testing.T) {
	si := makeRealSpatialIndex()
	names := si.MatchedAreaNames(40.0, 0.0) // way outside
	if len(names) != 0 {
		t.Errorf("Expected 0 names outside all fences, got %v", names)
	}
}

func TestSpatialIndexDisplayInMatches(t *testing.T) {
	fences := []Fence{
		{
			Name:             "Visible",
			DisplayInMatches: true,
			Path:             [][2]float64{{50, -1}, {52, -1}, {52, 1}, {50, 1}},
		},
		{
			Name:             "Hidden",
			DisplayInMatches: false,
			Path:             [][2]float64{{50, -1}, {52, -1}, {52, 1}, {50, 1}},
		},
	}
	si := NewSpatialIndex(fences)

	areas := si.PointInAreas(51.0, 0.0)
	if len(areas) != 2 {
		t.Fatalf("Expected 2 areas, got %d", len(areas))
	}

	// Both should be returned, but with correct DisplayInMatches
	foundVisible := false
	foundHidden := false
	for _, a := range areas {
		if a.Name == "Visible" && a.DisplayInMatches {
			foundVisible = true
		}
		if a.Name == "Hidden" && !a.DisplayInMatches {
			foundHidden = true
		}
	}
	if !foundVisible {
		t.Error("Missing Visible area with DisplayInMatches=true")
	}
	if !foundHidden {
		t.Error("Missing Hidden area with DisplayInMatches=false")
	}
}

func TestSpatialIndexMultipath(t *testing.T) {
	// A fence with two separate polygons (multipath)
	fences := []Fence{
		{
			Name:             "TwoParts",
			DisplayInMatches: true,
			Multipath: [][][2]float64{
				// Part A: small square at (10,10)
				{{9, 9}, {9, 11}, {11, 11}, {11, 9}},
				// Part B: small square at (20,20)
				{{19, 19}, {19, 21}, {21, 21}, {21, 19}},
			},
		},
	}
	si := NewSpatialIndex(fences)

	// Inside Part A
	areas := si.PointInAreas(10.0, 10.0)
	if len(areas) != 1 || areas[0].Name != "TwoParts" {
		t.Errorf("Expected TwoParts for part A, got %v", areaNames(areas))
	}

	// Inside Part B
	areas = si.PointInAreas(20.0, 20.0)
	if len(areas) != 1 || areas[0].Name != "TwoParts" {
		t.Errorf("Expected TwoParts for part B, got %v", areaNames(areas))
	}

	// Between the two parts - should not match
	areas = si.PointInAreas(15.0, 15.0)
	if len(areas) != 0 {
		t.Errorf("Expected no match between parts, got %v", areaNames(areas))
	}
}

func TestSpatialIndexMultipathDedup(t *testing.T) {
	// Two overlapping sub-polygons in the same fence
	fences := []Fence{
		{
			Name:             "Overlap",
			DisplayInMatches: true,
			Multipath: [][][2]float64{
				{{0, 0}, {0, 10}, {10, 10}, {10, 0}},
				{{5, 5}, {5, 15}, {15, 15}, {15, 5}},
			},
		},
	}
	si := NewSpatialIndex(fences)

	// Point (7,7) is inside both sub-polygons
	areas := si.PointInAreas(7.0, 7.0)
	if len(areas) != 1 {
		t.Errorf("Expected 1 area (deduped), got %d", len(areas))
	}
}

func TestSpatialIndexEmpty(t *testing.T) {
	si := NewSpatialIndex(nil)
	areas := si.PointInAreas(51.0, 0.0)
	if len(areas) != 0 {
		t.Errorf("Empty index should return no areas, got %d", len(areas))
	}
}

func TestSpatialIndexGroupField(t *testing.T) {
	fences := []Fence{
		{
			Name:             "TestFence",
			Group:            "Kent",
			DisplayInMatches: true,
			Path:             [][2]float64{{50, -1}, {52, -1}, {52, 1}, {50, 1}},
		},
	}
	si := NewSpatialIndex(fences)

	areas := si.PointInAreas(51.0, 0.0)
	if len(areas) != 1 {
		t.Fatal("Expected 1 area")
	}
	if areas[0].Group != "Kent" {
		t.Errorf("Expected group 'Kent', got %q", areas[0].Group)
	}
}

func TestBoundingBox(t *testing.T) {
	path := [][2]float64{
		{51.2295578271973, 1.0079984211496},
		{51.3150440308209, 1.1333112263253},
		{51.3128980839251, 1.1484174274972},
		{51.2394462717341, 1.0145215534738},
	}

	minX, minY, maxX, maxY := boundingBox(path)

	if minX != 51.2295578271973 {
		t.Errorf("minX = %f, want 51.2295578271973", minX)
	}
	if maxX != 51.3150440308209 {
		t.Errorf("maxX = %f, want 51.3150440308209", maxX)
	}
	if minY != 1.0079984211496 {
		t.Errorf("minY = %f, want 1.0079984211496", minY)
	}
	if maxY != 1.1484174274972 {
		t.Errorf("maxY = %f, want 1.1484174274972", maxY)
	}
}

// Benchmark R-tree search with real fences
func BenchmarkSpatialIndexPointInAreas(b *testing.B) {
	si := makeRealSpatialIndex()
	for i := 0; i < b.N; i++ {
		si.PointInAreas(51.297267, 1.069734) // UKC campus
	}
}

func BenchmarkSpatialIndexMiss(b *testing.B) {
	si := makeRealSpatialIndex()
	for i := 0; i < b.N; i++ {
		si.PointInAreas(40.0, 0.0) // outside all
	}
}
