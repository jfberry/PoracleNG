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
