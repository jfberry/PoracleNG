package geofence

import (
	"testing"
)

// Simple square polygon for basic PIP tests
var square = [][2]float64{
	{0.0, 0.0},
	{0.0, 10.0},
	{10.0, 10.0},
	{10.0, 0.0},
}

func TestPIPInsideSquare(t *testing.T) {
	if !PointInPolygon(5.0, 5.0, square) {
		t.Error("Center of square should be inside")
	}
}

func TestPIPOutsideSquare(t *testing.T) {
	if PointInPolygon(15.0, 5.0, square) {
		t.Error("Point outside square should not be inside")
	}
}

func TestPIPOnEdge(t *testing.T) {
	// Edge behavior is implementation-defined for ray-casting;
	// just verify it doesn't panic
	_ = PointInPolygon(0.0, 5.0, square)
	_ = PointInPolygon(5.0, 0.0, square)
}

func TestPIPTooFewPoints(t *testing.T) {
	line := [][2]float64{{0.0, 0.0}, {1.0, 1.0}}
	if PointInPolygon(0.5, 0.5, line) {
		t.Error("Polygon with < 3 points should always return false")
	}
}

func TestPIPEmptyPolygon(t *testing.T) {
	if PointInPolygon(0.0, 0.0, nil) {
		t.Error("Nil polygon should return false")
	}
	if PointInPolygon(0.0, 0.0, [][2]float64{}) {
		t.Error("Empty polygon should return false")
	}
}

func TestPIPTriangle(t *testing.T) {
	triangle := [][2]float64{
		{0.0, 0.0},
		{0.0, 10.0},
		{10.0, 5.0},
	}

	if !PointInPolygon(3.0, 5.0, triangle) {
		t.Error("Point inside triangle should be inside")
	}
	if PointInPolygon(9.0, 1.0, triangle) {
		t.Error("Point outside triangle should not be inside")
	}
}

func TestPIPConcavePolygon(t *testing.T) {
	// L-shaped polygon
	lShape := [][2]float64{
		{0.0, 0.0},
		{0.0, 10.0},
		{5.0, 10.0},
		{5.0, 5.0},
		{10.0, 5.0},
		{10.0, 0.0},
	}

	// Inside the L
	if !PointInPolygon(2.0, 2.0, lShape) {
		t.Error("Point in base of L should be inside")
	}
	if !PointInPolygon(2.0, 8.0, lShape) {
		t.Error("Point in vertical part of L should be inside")
	}
	// In the concave cutout
	if PointInPolygon(7.0, 8.0, lShape) {
		t.Error("Point in concave cutout should be outside")
	}
}

// Real Canterbury fence from geofence.json
var canterbury = [][2]float64{
	{51.3128980839251, 1.0079984211496},
	{51.3150440308209, 1.1333112263253},
	{51.2295578271973, 1.1484174274972},
	{51.2394462717341, 1.0145215534738},
}

// Real UKC fence from geofence.json
var ukc = [][2]float64{
	{51.294283219014, 1.0543049227321},
	{51.3001867254262, 1.0488546740139},
	{51.3015014939356, 1.073187674197},
	{51.2961616897663, 1.0771358858669},
	{51.290606560982, 1.065806234988},
}

// Real Faversham fence from geofence.json
var faversham = [][2]float64{
	{51.313276408389, 0.8415363318773},
	{51.3316208635379, 0.8496044165941},
	{51.3334441803482, 0.8748386390062},
	{51.3310845800883, 0.8945796973558},
	{51.3222886365392, 0.9038494117112},
	{51.3123107075019, 0.9067676551195},
	{51.3017939821503, 0.9009311683031},
	{51.3051746209927, 0.8698604590746},
}

// Real Ashford fence from geofence.json
var ashford = [][2]float64{
	{51.126045, 0.839649},
	{51.127445, 0.837074},
	{51.129387, 0.839649},
	{51.136063, 0.838104},
	{51.136604, 0.829693},
	{51.139187, 0.827289},
	{51.143387, 0.824543},
	{51.14813, 0.845485},
	{51.153942, 0.839477},
	{51.160507, 0.844112},
	{51.165783, 0.850979},
	{51.17784, 0.875355},
	{51.175686, 0.885483},
	{51.171703, 0.891834},
	{51.17052, 0.903164},
	{51.167614, 0.904537},
	{51.163956, 0.898357},
	{51.159542, 0.909},
	{51.140697, 0.920501},
	{51.131542, 0.916553},
	{51.125507, 0.90179},
	{51.116833, 0.901737},
	{51.10805, 0.887542},
	{51.10536, 0.880848},
	{51.103523, 0.869175},
	{51.107838, 0.861965},
	{51.11215, 0.867973},
	{51.116566, 0.862652},
	{51.118935, 0.85424},
	{51.123676, 0.848919},
	{51.126045, 0.839649},
}

func TestPIPRealCanterbury(t *testing.T) {
	tests := []struct {
		name   string
		lat    float64
		lon    float64
		inside bool
	}{
		// Real webhook coordinates verified against Python PIP
		{"quest squirtle (StDunstans+Canterbury)", 51.282747, 1.063537, true},
		{"quest potion (UKC+Canterbury)", 51.297267, 1.069734, true},
		{"quest archen", 51.302032, 1.054028, true},
		{"quest pikachu (StDunstans+Canterbury)", 51.287301, 1.079881, true},
		{"raid abbots barton", 51.272513, 1.089742, true},
		{"quest kings football", 51.278471, 1.089237, true},
		{"quest gulbenkian (UKC+Canterbury)", 51.298203, 1.069457, true},
		{"lure 501 (UKC+Canterbury)", 51.293981, 1.063606, true},
		// Outside Canterbury
		{"gym king george (Faversham)", 51.310916, 0.877440, false},
		{"invasion (Ashford)", 51.120882, 0.863187, false},
		{"quest stardust (outside all)", 51.311853, 1.193484, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PointInPolygon(tt.lat, tt.lon, canterbury)
			if got != tt.inside {
				t.Errorf("PointInPolygon(%f, %f, Canterbury) = %v, want %v",
					tt.lat, tt.lon, got, tt.inside)
			}
		})
	}
}

func TestPIPRealUKC(t *testing.T) {
	tests := []struct {
		name   string
		lat    float64
		lon    float64
		inside bool
	}{
		{"quest potion", 51.297267, 1.069734, true},
		{"lure 501", 51.293981, 1.063606, true},
		{"quest gulbenkian", 51.298203, 1.069457, true},
		// Inside Canterbury but outside UKC
		{"quest squirtle (StDunstans)", 51.282747, 1.063537, false},
		{"quest archen", 51.302032, 1.054028, false},
		{"raid abbots barton (Dover Road)", 51.272513, 1.089742, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PointInPolygon(tt.lat, tt.lon, ukc)
			if got != tt.inside {
				t.Errorf("PointInPolygon(%f, %f, UKC) = %v, want %v",
					tt.lat, tt.lon, got, tt.inside)
			}
		})
	}
}

func TestPIPRealFaversham(t *testing.T) {
	if !PointInPolygon(51.310916, 0.877440, faversham) {
		t.Error("King George gym should be inside Faversham")
	}
	if PointInPolygon(51.282747, 1.063537, faversham) {
		t.Error("Canterbury point should not be inside Faversham")
	}
}

func TestPIPRealAshford(t *testing.T) {
	if !PointInPolygon(51.120882, 0.863187, ashford) {
		t.Error("Invasion point should be inside Ashford")
	}
	if !PointInPolygon(51.160499, 0.900055, ashford) {
		t.Error("Fort update point should be inside Ashford")
	}
	if !PointInPolygon(51.156041, 0.856433, ashford) {
		t.Error("Raid Repton point should be inside Ashford")
	}
	if PointInPolygon(51.282747, 1.063537, ashford) {
		t.Error("Canterbury point should not be inside Ashford")
	}
}

// Benchmark PIP with a complex polygon (31-point Ashford)
func BenchmarkPIPComplex(b *testing.B) {
	for i := 0; i < b.N; i++ {
		PointInPolygon(51.120882, 0.863187, ashford)
	}
}

func BenchmarkPIPSimple(b *testing.B) {
	for i := 0; i < b.N; i++ {
		PointInPolygon(51.297267, 1.069734, canterbury)
	}
}
