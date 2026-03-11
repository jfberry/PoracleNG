package geofence

// PointInPolygon tests if point (lat, lon) is inside polygon using ray-casting.
// polygon is [][]float64 where each entry is [lat, lon].
func PointInPolygon(lat, lon float64, polygon [][2]float64) bool {
	n := len(polygon)
	if n < 3 {
		return false
	}
	inside := false
	j := n - 1
	for i := 0; i < n; i++ {
		yi, xi := polygon[i][0], polygon[i][1]
		yj, xj := polygon[j][0], polygon[j][1]

		if ((yi > lat) != (yj > lat)) &&
			(lon < (xj-xi)*(lat-yi)/(yj-yi)+xi) {
			inside = !inside
		}
		j = i
	}
	return inside
}
