package geocoding

import "testing"

// TestCoalesceOnlyApproaches — how close do we get to compactAddress with
// only {{coalesce}} (no eq/ne/compactAddress helpers)? Documents which
// edge cases stock Handlebars plus coalesce can't reach.
func TestCoalesceOnlyApproaches(t *testing.T) {
	// With conditional-colon guard via (coalesce …) in an #if.
	const src = `{{#if (coalesce suburb city)}}{{coalesce suburb city}}:{{/if}}{{#if streetName}} {{streetName}}{{#if streetNumber}} {{streetNumber}}{{/if}}{{/if}}{{#if suburb}}{{#if city}}{{#if streetName}},{{/if}} {{city}}{{/if}}{{/if}}`

	tmpl, err := CompileAddressTemplate(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	cases := []struct {
		name string
		addr Address
		want string
	}{
		{"full", Address{Suburb: "Mitte", StreetName: "F", StreetNumber: "42", City: "Berlin"}, "Mitte: F 42, Berlin"},
		{"no suburb", Address{StreetName: "Main", StreetNumber: "1", City: "Springfield"}, "Springfield: Main 1"},
		{"city only", Address{City: "Munich"}, "Munich:"},
		{"suburb+city no street", Address{Suburb: "Mitte", City: "Berlin"}, "Mitte: Berlin"},
		{"suburb only", Address{Suburb: "Mitte"}, "Mitte:"},
		{"street only", Address{StreetName: "Main", StreetNumber: "1"}, "Main 1"},
		{"nothing", Address{}, ""},
		// These still diverge without eq or a FormattedAddress fallback:
		{"suburb==city (DIVERGES)", Address{Suburb: "Berlin", City: "Berlin"}, "Berlin: Berlin"}, // Go: "Berlin:"
		{"fall back (DIVERGES)", Address{FormattedAddress: "Somewhere"}, ""},                    // Go: "Somewhere"
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tmpl.Render(c.addr)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestCompactAddressHelperParity guards that {{compactAddress}} produces the
// same output as the original PoracleJS/ccev FormatCompactAddress function
// across every case we care about — including the edge cases a pure
// Handlebars template struggles with (suburb==city de-dup, fully-empty
// components falling back to FormattedAddress).
func TestCompactAddressHelperParity(t *testing.T) {
	tmpl, err := CompileAddressTemplate(`{{compactAddress}}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	cases := []struct {
		name string
		addr Address
		want string
	}{
		{"full: suburb + street + num + city", Address{Suburb: "Mitte", StreetName: "Friedrichstrasse", StreetNumber: "42", City: "Berlin"}, "Mitte: Friedrichstrasse 42, Berlin"},
		{"no suburb, street + num + city", Address{StreetName: "Main Street", StreetNumber: "1", City: "Springfield"}, "Springfield: Main Street 1"},
		{"city only", Address{City: "Munich"}, "Munich:"},
		{"suburb + city, no street", Address{Suburb: "Mitte", City: "Berlin"}, "Mitte: Berlin"},
		{"suburb == city (de-dup)", Address{Suburb: "Berlin", City: "Berlin"}, "Berlin:"},
		{"suburb + city + street, no number", Address{Suburb: "Mitte", StreetName: "Friedrichstrasse", City: "Berlin"}, "Mitte: Friedrichstrasse, Berlin"},
		{"suburb + street, no city", Address{Suburb: "Mitte", StreetName: "Main St"}, "Mitte: Main St"},
		{"nothing", Address{}, ""},
		{"street + number only", Address{StreetName: "Main St", StreetNumber: "1"}, "Main St 1"},
		{"suburb only", Address{Suburb: "Mitte"}, "Mitte:"},
		{"fall back to FormattedAddress when components empty", Address{FormattedAddress: "Somewhere Far Away"}, "Somewhere Far Away"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tmpl.Render(c.addr)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestCoalesceHelper — verifies the generic "first non-empty" helper works
// in an address_format context. Useful for operators writing their own
// templates: {{coalesce suburb city neighbourhood}} picks the best
// available opener without a nested if/else cascade.
func TestCoalesceHelper(t *testing.T) {
	tmpl, err := CompileAddressTemplate(`{{coalesce suburb city}}:{{#if streetName}} {{streetName}}{{/if}}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	cases := []struct {
		addr Address
		want string
	}{
		{Address{Suburb: "Mitte", City: "Berlin", StreetName: "X"}, "Mitte: X"},
		{Address{City: "Berlin", StreetName: "X"}, "Berlin: X"},
		{Address{Suburb: "Mitte"}, "Mitte:"},
		{Address{}, ":"}, // coalesce returns empty, trailing ':' per literal
	}

	for _, c := range cases {
		got := tmpl.Render(c.addr)
		if got != c.want {
			t.Errorf("addr=%+v: got %q want %q", c.addr, got, c.want)
		}
	}
}
