package geo

import (
	"github.com/golang/geo/s2"
)

// GetCellCenter returns the center lat/lon of the S2 cell at the given level
// that contains the given lat/lon point.
func GetCellCenter(lat, lon float64, level int) (float64, float64) {
	ll := s2.LatLngFromDegrees(lat, lon)
	cellID := s2.CellIDFromLatLng(ll).Parent(level)
	center := s2.LatLngFromPoint(cellID.Point())
	return center.Lat.Degrees(), center.Lng.Degrees()
}

// GetCellCoords returns the 4 corner vertices of the S2 cell at the given level
// that contains the given lat/lon point. Returns [[lat,lon], ...].
func GetCellCoords(lat, lon float64, level int) [4][2]float64 {
	ll := s2.LatLngFromDegrees(lat, lon)
	cellID := s2.CellIDFromLatLng(ll).Parent(level)
	cell := s2.CellFromCellID(cellID)

	var coords [4][2]float64
	for i := 0; i < 4; i++ {
		vertex := s2.LatLngFromPoint(cell.Vertex(i))
		coords[i] = [2]float64{vertex.Lat.Degrees(), vertex.Lng.Degrees()}
	}
	return coords
}

// GetCellCoordsSlice returns the same vertices as GetCellCoords but as a slice,
// so that JSON omitempty works correctly (nil slice omits, zero-value array does not).
func GetCellCoordsSlice(lat, lon float64, level int) [][2]float64 {
	arr := GetCellCoords(lat, lon, level)
	return arr[:]
}
