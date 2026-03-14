package geo

import "strings"

// ConvertTimeFormat converts a human-readable time format string (e.g. "HH:mm:ss")
// to Go's reference time layout (e.g. "15:04:05").
func ConvertTimeFormat(format string) string {
	// Order matters: longer tokens must be replaced before shorter ones
	r := strings.NewReplacer(
		"YYYY", "2006",
		"YY", "06",
		"MM", "01",
		"DD", "02",
		"HH", "15",
		"hh", "03",
		"mm", "04",
		"ss", "05",
		"A", "PM",
		"a", "pm",
	)
	return r.Replace(format)
}
