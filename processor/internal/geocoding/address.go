package geocoding

import (
	"fmt"
	"strings"

	"github.com/mailgun/raymond/v2"
)

// AddressTemplate wraps a pre-compiled Handlebars template so we parse
// address_format once at Geocoder construction rather than per render.
type AddressTemplate struct {
	tmpl *raymond.Template
}

// CompileAddressTemplate parses the user-supplied address_format. An empty
// src returns a nil template — Render then falls back to FormattedAddress.
// Address helpers ({{coalesce}}, {{compactAddress}}) register lazily here
// so init order doesn't matter.
func CompileAddressTemplate(src string) (*AddressTemplate, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, nil
	}
	registerAddressHelpers()
	t, err := raymond.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("address_format: %w", err)
	}
	return &AddressTemplate{tmpl: t}, nil
}

// Render executes the compiled template against addr's fields, falling back
// to addr.FormattedAddress for empty/nil template and for runtime errors
// (so operators don't see an empty addr if a template blows up in prod).
func (t *AddressTemplate) Render(addr Address) string {
	if t == nil || t.tmpl == nil {
		return addr.FormattedAddress
	}
	result, err := t.tmpl.Exec(addressFields(addr))
	if err != nil {
		return addr.FormattedAddress
	}
	return strings.TrimSpace(result)
}

// addressFields exposes addr as the key/value pairs operators reference in
// their templates. Kept in sync with the Address struct's json tags.
func addressFields(addr Address) map[string]string {
	return map[string]string{
		"formattedAddress": addr.FormattedAddress,
		"displayName":      addr.DisplayName,
		"country":          addr.Country,
		"countryCode":      addr.CountryCode,
		"state":            addr.State,
		"county":           addr.County,
		"city":             addr.City,
		"zipcode":          addr.Zipcode,
		"streetName":       addr.StreetName,
		"streetNumber":     addr.StreetNumber,
		"neighbourhood":    addr.Neighbourhood,
		"suburb":           addr.Suburb,
		"town":             addr.Town,
		"village":          addr.Village,
		"flag":             addr.Flag,
	}
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
	addr.DisplayName = escapeString(addr.DisplayName)
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

func escapeString(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, `"`, "''")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, `\`, "?")
	return s
}
