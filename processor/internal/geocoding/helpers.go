package geocoding

import (
	"strings"
	"sync"

	"github.com/mailgun/raymond/v2"
)

var helpersOnce sync.Once

// registerAddressHelpers registers raymond helpers used by address_format
// templates. Called lazily from CompileAddressTemplate so operators don't
// need to know about init order; also means tests that touch address
// templates pick up the helpers automatically.
//
// Registration is process-global (raymond's model). Safe to call multiple
// times thanks to the once guard — re-registering the same helper name
// panics in raymond.
func registerAddressHelpers() {
	helpersOnce.Do(func() {
		// coalesce — return the first non-empty argument. Useful for
		// "suburb, else city" style openers and similar fallback chains.
		//
		//   {{coalesce suburb city}}               → "Mitte" or "Berlin"
		//   {{coalesce streetName road name}}      → whichever is set
		raymond.RegisterHelper("coalesce", func(args ...any) string {
			for _, a := range args {
				if s, ok := a.(string); ok && strings.TrimSpace(s) != "" {
					return s
				}
			}
			return ""
		})

		// compactAddress — drop-in equivalent for PoracleJS/ccev's
		// FormatCompactAddress. Operators migrating from that layout set
		//   address_format = "{{compactAddress}}"
		// and get the exact "Mitte: Friedrichstrasse 42, Berlin" shape,
		// including the de-duplication of city when suburb == city and
		// the all-empty-components fallback to FormattedAddress. Lives as
		// a helper rather than a config knob so the spec stays in Go
		// (testable) while the call site stays in the template where
		// operators already customise.
		raymond.RegisterHelper("compactAddress", func(options *raymond.Options) string {
			addr := extractAddressFromContext(options)
			return formatCompactAddress(addr)
		})
	})
}

// extractAddressFromContext reconstructs an Address from the per-field
// string map that Render passes to Exec. Only the fields compactAddress
// consults need to be populated — this keeps the helper independent of
// Address struct layout changes elsewhere.
func extractAddressFromContext(options *raymond.Options) Address {
	field := func(name string) string {
		v := options.Value(name)
		if s, ok := v.(string); ok {
			return s
		}
		return ""
	}
	return Address{
		Suburb:           field("suburb"),
		City:             field("city"),
		StreetName:       field("streetName"),
		StreetNumber:     field("streetNumber"),
		FormattedAddress: field("formattedAddress"),
	}
}

// formatCompactAddress builds the compact "suburb: street number, city"
// layout from ccev's PR. Retained here as the implementation behind the
// {{compactAddress}} raymond helper — operators who want this shape write
// address_format = "{{compactAddress}}" rather than hand-authoring the
// Handlebars equivalent (which runs into edge cases around suburb==city
// and fully-empty components).
func formatCompactAddress(addr Address) string {
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

	if addr.City != "" && !compactContains(parts, addr.City) {
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

// compactContains returns true if `s` appears in the slice after stripping
// any trailing punctuation (":" or ",") we may have already appended to a
// previous element.
func compactContains(ss []string, s string) bool {
	for _, v := range ss {
		if strings.TrimRight(v, ":,") == s {
			return true
		}
	}
	return false
}
