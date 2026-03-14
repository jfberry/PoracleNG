package matching

import "math"

const earthRadiusMetres = 6371e3

// HaversineDistance returns the distance in metres between two lat/lon points,
// rounded up to the nearest metre. Direct port of controller.js:getDistance.
func HaversineDistance(lat1, lon1, lat2, lon2 float64) int {
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	Δφ := (lat2 - lat1) * math.Pi / 180
	Δλ := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*
			math.Sin(Δλ/2)*math.Sin(Δλ/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	d := earthRadiusMetres * c
	return int(math.Ceil(d))
}

// Bearing returns the initial bearing in degrees (0-360) from point 1 to point 2.
// Port of controller.js:getBearing.
func Bearing(lat1, lon1, lat2, lon2 float64) float64 {
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	λ1 := lon1 * math.Pi / 180
	λ2 := lon2 * math.Pi / 180

	y := math.Sin(λ2-λ1) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(λ2-λ1)
	θ := math.Atan2(y, x)
	brng := math.Mod(θ*180/math.Pi+360, 360)
	return brng
}

// CardinalDirection returns a compass direction name for a bearing in degrees.
// Port of controller.js:getBearingEmoji — matches the JS mapping exactly.
func CardinalDirection(brng float64) string {
	if brng < 22.5 {
		return "north"
	}
	if brng < 67.5 {
		return "northwest"
	}
	if brng < 112.5 {
		return "west"
	}
	if brng < 157.5 {
		return "southwest"
	}
	if brng < 202.5 {
		return "south"
	}
	if brng < 247.5 {
		return "southeast"
	}
	if brng < 292.5 {
		return "east"
	}
	if brng < 337.5 {
		return "northeast"
	}
	return "north"
}
