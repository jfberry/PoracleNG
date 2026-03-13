package geo

import (
	"github.com/golang/geo/s2"
)

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
