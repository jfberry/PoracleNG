package geocoding

import (
	"strings"
	"sync"

	"github.com/mailgun/raymond/v2"
)

var helpersOnce sync.Once

// registerAddressHelpers registers the raymond helpers address_format
// templates can use. Called lazily from CompileAddressTemplate; the once
// guard protects against raymond's panic on re-registering the same name.
func registerAddressHelpers() {
	helpersOnce.Do(func() {
		// {{coalesce a b c}} — first non-empty argument.
		raymond.RegisterHelper("coalesce", func(args ...any) string {
			for _, a := range args {
				if s, ok := a.(string); ok && strings.TrimSpace(s) != "" {
					return s
				}
			}
			return ""
		})

		// {{compactAddress}} — PoracleJS-equivalent compact layout
		// ("Mitte: Friedrichstrasse 42, Berlin"). Lives here rather
		// than as a config flag so operators opt in via template.
		raymond.RegisterHelper("compactAddress", func(options *raymond.Options) string {
			return formatCompactAddress(extractAddressFromContext(options))
		})
	})
}

// extractAddressFromContext pulls the fields compactAddress needs out of
// the raymond context. Kept deliberately minimal — only reads what the
// helper uses, not the whole Address.
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

// formatCompactAddress — vendored from ccev's PR as the implementation
// behind {{compactAddress}}. Handles the suburb==city dedup and the
// all-empty-components fallback to FormattedAddress, which stock
// Handlebars can't express cleanly.
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
