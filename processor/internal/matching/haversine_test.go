package matching

import (
	"math"
	"testing"
)

func TestHaversineDistance(t *testing.T) {
	tests := []struct {
		name     string
		lat1     float64
		lon1     float64
		lat2     float64
		lon2     float64
		expected int
	}{
		{
			name: "London to Paris",
			lat1: 51.5074, lon1: -0.1278,
			lat2: 48.8566, lon2: 2.3522,
			expected: 343556, // ~343.5 km
		},
		{
			name: "Same point",
			lat1: 51.5074, lon1: -0.1278,
			lat2: 51.5074, lon2: -0.1278,
			expected: 0,
		},
		{
			name: "Short distance ~1km",
			lat1: 51.5074, lon1: -0.1278,
			lat2: 51.5164, lon2: -0.1278,
			expected: 1001, // ~1km north
		},
		{
			name: "Antipodal points",
			lat1: 0, lon1: 0,
			lat2: 0, lon2: 180,
			expected: int(math.Ceil(math.Pi * earthRadiusMetres)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HaversineDistance(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			// Allow 1% tolerance for known distances
			diff := math.Abs(float64(got-tt.expected)) / math.Max(float64(tt.expected), 1)
			if diff > 0.01 && got != tt.expected {
				t.Errorf("HaversineDistance(%f,%f,%f,%f) = %d, want ~%d (diff %.2f%%)", tt.lat1, tt.lon1, tt.lat2, tt.lon2, got, tt.expected, diff*100)
			}
		})
	}
}
