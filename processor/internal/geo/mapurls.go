package geo

import "fmt"

// AppleMapURL returns an Apple Maps URL for the given coordinates.
func AppleMapURL(lat, lon float64) string {
	return fmt.Sprintf("https://maps.apple.com/place?coordinate=%f,%f", lat, lon)
}

// GoogleMapURL returns a Google Maps URL for the given coordinates.
func GoogleMapURL(lat, lon float64) string {
	return fmt.Sprintf("https://maps.google.com/maps?q=%f,%f", lat, lon)
}

// WazeMapURL returns a Waze navigation URL for the given coordinates.
func WazeMapURL(lat, lon float64) string {
	return fmt.Sprintf("https://www.waze.com/ul?ll=%f,%f&navigate=yes&zoom=17", lat, lon)
}

// FormatCoord formats a coordinate for URL use.
func FormatCoord(v float64) string {
	return fmt.Sprintf("%f", v)
}
