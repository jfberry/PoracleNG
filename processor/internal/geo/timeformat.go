package geo

import (
	"sort"
	"strings"
)

// Moment.js locale shortcut expansions.
// Only the locales commonly used by Poracle are included; add more as needed.
var momentLocales = map[string]map[string]string{
	"en-gb": {
		"LT":   "HH:mm",
		"LTS":  "HH:mm:ss",
		"L":    "DD/MM/YYYY",
		"LL":   "D MMMM YYYY",
		"LLL":  "D MMMM YYYY HH:mm",
		"LLLL": "dddd, D MMMM YYYY HH:mm",
	},
	"en-us": {
		"LT":   "h:mm A",
		"LTS":  "h:mm:ss A",
		"L":    "MM/DD/YYYY",
		"LL":   "MMMM D, YYYY",
		"LLL":  "MMMM D, YYYY h:mm A",
		"LLLL": "dddd, MMMM D, YYYY h:mm A",
	},
	"de": {
		"LT":   "HH:mm",
		"LTS":  "HH:mm:ss",
		"L":    "DD.MM.YYYY",
		"LL":   "D. MMMM YYYY",
		"LLL":  "D. MMMM YYYY HH:mm",
		"LLLL": "dddd, D. MMMM YYYY HH:mm",
	},
	"fr": {
		"LT":   "HH:mm",
		"LTS":  "HH:mm:ss",
		"L":    "DD/MM/YYYY",
		"LL":   "D MMMM YYYY",
		"LLL":  "D MMMM YYYY HH:mm",
		"LLLL": "dddd D MMMM YYYY HH:mm",
	},
}

// IsLocaleSupported returns true if the given locale has Moment.js shortcut mappings.
func IsLocaleSupported(locale string) bool {
	_, ok := momentLocales[strings.ToLower(locale)]
	return ok
}

// SupportedLocales returns the list of supported locale names.
func SupportedLocales() []string {
	locales := make([]string, 0, len(momentLocales))
	for k := range momentLocales {
		locales = append(locales, k)
	}
	sort.Strings(locales)
	return locales
}

// ConvertTimeFormat converts a Moment.js-style format string to a Go reference
// time layout. If the format is a Moment locale shortcut (e.g. "LTS", "L"),
// it is first expanded using the given locale before token replacement.
func ConvertTimeFormat(format, locale string) string {
	// Resolve Moment.js locale shortcuts
	locale = strings.ToLower(locale)
	if shortcuts, ok := momentLocales[locale]; ok {
		if expanded, ok := shortcuts[format]; ok {
			format = expanded
		}
	} else if shortcuts, ok := momentLocales["en-gb"]; ok {
		// Fall back to en-gb for unknown locales
		if expanded, ok := shortcuts[format]; ok {
			format = expanded
		}
	}

	// Order matters: longer tokens must be replaced before shorter ones
	r := strings.NewReplacer(
		"YYYY", "2006",
		"YY", "06",
		"MMMM", "January",
		"MMM", "Jan",
		"MM", "01",
		"dddd", "Monday",
		"ddd", "Mon",
		"DD", "02",
		"D", "2",
		"HH", "15",
		"hh", "03",
		"h", "3",
		"mm", "04",
		"ss", "05",
		"A", "PM",
		"a", "pm",
	)
	return r.Replace(format)
}
