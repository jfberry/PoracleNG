package geocoding

import (
	"strings"
)

// FormatAddress applies a simple template to an Address, replacing
// {{fieldName}} placeholders with the corresponding field value.
// This matches the alerter's Handlebars addressFormat behaviour.
func FormatAddress(tmpl string, addr Address) string {
	if tmpl == "" {
		return addr.FormattedAddress
	}

	fields := map[string]string{
		"formattedAddress": addr.FormattedAddress,
		"country":          addr.Country,
		"countryCode":      addr.CountryCode,
		"state":            addr.State,
		"city":             addr.City,
		"zipcode":          addr.Zipcode,
		"streetName":       addr.StreetName,
		"streetNumber":     addr.StreetNumber,
		"neighbourhood":    addr.Neighbourhood,
		"county":           addr.County,
		"suburb":           addr.Suburb,
		"town":             addr.Town,
		"village":          addr.Village,
		"flag":             addr.Flag,
	}

	result := tmpl
	// Replace triple-braces FIRST to avoid partial matches
	// (e.g., {{{streetName}}} must not become {value} from {{streetName}} match)
	for field, value := range fields {
		result = strings.ReplaceAll(result, "{{{"+field+"}}}", value)
	}
	for field, value := range fields {
		result = strings.ReplaceAll(result, "{{"+field+"}}", value)
	}
	return strings.TrimSpace(result)
}

// CountryFlag returns the flag emoji for a two-letter country code.
// For example, "GB" returns the British flag emoji.
// Each letter is mapped to a Regional Indicator Symbol (U+1F1E6 + offset).
func CountryFlag(countryCode string) string {
	if len(countryCode) != 2 {
		return ""
	}
	cc := strings.ToUpper(countryCode)
	r1 := rune(cc[0]) - 'A' + 0x1F1E6
	r2 := rune(cc[1]) - 'A' + 0x1F1E6
	return string([]rune{r1, r2})
}

// EscapeAddress sanitises address string fields to prevent JSON/template
// injection issues. This matches the alerter's escapeJsonString behaviour.
func EscapeAddress(addr *Address) {
	addr.StreetName = escapeString(addr.StreetName)
	addr.StreetNumber = escapeString(addr.StreetNumber)
	addr.Addr = escapeString(addr.Addr)
	addr.FormattedAddress = escapeString(addr.FormattedAddress)
	addr.City = escapeString(addr.City)
	addr.State = escapeString(addr.State)
	addr.Country = escapeString(addr.Country)
	addr.Neighbourhood = escapeString(addr.Neighbourhood)
	addr.County = escapeString(addr.County)
	addr.Suburb = escapeString(addr.Suburb)
	addr.Town = escapeString(addr.Town)
	addr.Village = escapeString(addr.Village)
	addr.Zipcode = escapeString(addr.Zipcode)
}

// FormatCompactAddress builds a compact formatted address string from an Address.
//
// It follows the Photon compact logic used in PoracleJS/Python migration:
// - Start with suburb
// - If suburb is missing and city exists, start with city
// - Add street (optionally with house number)
// - Add city when it is not already part of the result
// - Add a colon after the first context part
// - If still empty, fall back to splitting FormattedAddress into words
func FormatCompactAddress(addr Address) string {
	var parts []string
	if addr.Suburb != "" {
		parts = append(parts, addr.Suburb)
	}

	if addr.Suburb == "" && addr.City != "" {
		parts = append(parts, addr.City)
	}

	colonsAt := len(parts) - 1

	street := strings.TrimSpace(addr.StreetName)
	if street != "" {
		if addr.StreetNumber != "" {
			street += " " + strings.TrimSpace(addr.StreetNumber)
		}
		parts = append(parts, street)
	}

	if addr.City != "" && !containsString(parts, addr.City) {
		if street != "" && len(parts) > 0 {
			parts[len(parts)-1] += ","
		}
		parts = append(parts, addr.City)
	}

	if colonsAt >= 0 && colonsAt < len(parts) {
		parts[colonsAt] += ":"
	}

	if len(parts) == 0 && strings.TrimSpace(addr.FormattedAddress) != "" {
		parts = strings.Fields(addr.FormattedAddress)
	}

	return strings.Join(parts, " ")
}

// containsString checks if a string slice contains a given string.
func containsString(ss []string, s string) bool {
	for _, v := range ss {
		// Strip trailing punctuation for comparison
		clean := strings.TrimRight(v, ":,")
		if clean == s {
			return true
		}
	}
	return false
}

func escapeString(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, `"`, "''")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, `\`, "?")
	return s
}
