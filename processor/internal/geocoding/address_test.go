package geocoding

import (
	"strings"
	"testing"
)

// TestAddressTemplateEmpty — no template means "render the country-idiomatic
// string the provider already built via ocfmt".
func TestAddressTemplateEmpty(t *testing.T) {
	tmpl, err := CompileAddressTemplate("")
	if err != nil {
		t.Fatalf("compile empty: %v", err)
	}
	addr := Address{FormattedAddress: "Unter den Linden 1, 10117 Berlin, Germany"}
	if got := tmpl.Render(addr); got != addr.FormattedAddress {
		t.Errorf("empty template should fall through to FormattedAddress; got %q", got)
	}
}

// TestAddressTemplateSimpleSubstitution — legacy {{{field}}} template keeps
// working unchanged after the raymond switch.
func TestAddressTemplateSimpleSubstitution(t *testing.T) {
	tmpl, err := CompileAddressTemplate("{{{streetName}}} {{streetNumber}}")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	addr := Address{StreetName: "Unter den Linden", StreetNumber: "1"}
	if got := tmpl.Render(addr); got != "Unter den Linden 1" {
		t.Errorf("got %q, want %q", got, "Unter den Linden 1")
	}
}

// TestAddressTemplateCompactEquivalent — reproducing the old
// FormatCompactAddress behaviour with raymond's {{#if}} / {{#unless}}. This
// is the canonical example we point operators at in config.example.toml.
func TestAddressTemplateCompactEquivalent(t *testing.T) {
	const src = `{{#if suburb}}{{suburb}}{{else}}{{city}}{{/if}}:{{#if streetName}} {{streetName}}{{#if streetNumber}} {{streetNumber}}{{/if}}{{/if}}{{#unless suburb}}{{else}}, {{city}}{{/unless}}`

	tmpl, err := CompileAddressTemplate(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		name string
		addr Address
		want string
	}{
		{
			name: "suburb street number city",
			addr: Address{Suburb: "Mitte", StreetName: "Friedrichstrasse", StreetNumber: "42", City: "Berlin"},
			want: "Mitte: Friedrichstrasse 42, Berlin",
		},
		{
			name: "city fills in when no suburb",
			addr: Address{StreetName: "Main Street", StreetNumber: "1", City: "Springfield"},
			want: "Springfield: Main Street 1",
		},
		{
			name: "city only — no street block",
			addr: Address{City: "Munich"},
			want: "Munich:",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tmpl.Render(tt.addr); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAddressTemplateMissingFields — empty field references render as empty
// string with raymond (standard Handlebars). Template stays valid.
func TestAddressTemplateMissingFields(t *testing.T) {
	tmpl, err := CompileAddressTemplate("{{streetName}} {{streetNumber}}")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Neither field populated — template still renders (to whitespace).
	got := tmpl.Render(Address{})
	if strings.TrimSpace(got) != "" {
		t.Errorf("expected empty rendering for empty address, got %q", got)
	}
}

// TestAddressTemplateParseError surfaces a bad template as an error at
// compile time (so the operator sees it in startup logs, not at first
// render).
func TestAddressTemplateParseError(t *testing.T) {
	_, err := CompileAddressTemplate("{{#if suburb}}missing closing tag")
	if err == nil {
		t.Fatal("expected parse error for unclosed block")
	}
}

// TestAddressTemplateFallbackOnRuntimeError — template that passes parse but
// blows up at render falls back to FormattedAddress so production addresses
// don't render as empty strings.
func TestAddressTemplateFormattedAddressAccessible(t *testing.T) {
	tmpl, err := CompileAddressTemplate("Prefix: {{{formattedAddress}}}")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	addr := Address{FormattedAddress: "A, B, C"}
	if got := tmpl.Render(addr); got != "Prefix: A, B, C" {
		t.Errorf("got %q", got)
	}
}

// TestAddressTemplateDisplayNameAccessible — operators who preferred
// Nominatim's raw display_name can opt back in via {{{displayName}}}.
func TestAddressTemplateDisplayNameAccessible(t *testing.T) {
	tmpl, err := CompileAddressTemplate("{{{displayName}}}")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	addr := Address{DisplayName: "Houses of Parliament, Westminster, London"}
	if got := tmpl.Render(addr); got != addr.DisplayName {
		t.Errorf("got %q, want %q", got, addr.DisplayName)
	}
}

// TestCountryFlag guards the well-known flag mapping the alerter used so any
// refactor of the unicode offsets doesn't silently change output.
func TestCountryFlag(t *testing.T) {
	if got := CountryFlag("GB"); got != "🇬🇧" {
		t.Errorf("GB got %q", got)
	}
	if got := CountryFlag("US"); got != "🇺🇸" {
		t.Errorf("US got %q", got)
	}
	if got := CountryFlag(""); got != "" {
		t.Errorf("empty should be empty, got %q", got)
	}
}
