package ocfmt

import (
	"strings"
	"testing"
)

// TestWorldwideParses ensures the embedded worldwide.yaml loads and at least
// populates the default entry — a regression here would turn every Format
// call into a no-op on startup.
func TestWorldwideParses(t *testing.T) {
	f, err := newFormatter()
	if err != nil {
		t.Fatalf("newFormatter: %v", err)
	}
	if f.defaultEntry == nil || f.defaultEntry.AddressTemplate == "" {
		t.Fatal("defaultEntry should be populated with a template")
	}
	if len(f.countries) < 50 {
		t.Errorf("expected many country entries, got %d", len(f.countries))
	}
}

func TestFormatUS(t *testing.T) {
	f := Global()
	got := f.Format(map[string]string{
		"house_number": "1600",
		"road":         "Pennsylvania Avenue NW",
		"city":         "Washington",
		"state":        "District of Columbia",
		"state_code":   "DC",
		"postcode":     "20500",
		"country":      "United States of America",
		"country_code": "US",
	})
	for _, want := range []string{"1600", "Pennsylvania Avenue NW", "Washington", "20500", "United States"} {
		if !strings.Contains(got, want) {
			t.Errorf("US format missing %q; got %q", want, got)
		}
	}
	// US places the state between city and postcode ("Washington, DC 20500").
	if !strings.Contains(got, "DC") && !strings.Contains(got, "District of Columbia") {
		t.Errorf("US format should include the state/state_code, got %q", got)
	}
}

func TestFormatDE(t *testing.T) {
	f := Global()
	got := f.Format(map[string]string{
		"house_number": "1",
		"road":         "Unter den Linden",
		"city":         "Berlin",
		"state":        "Berlin",
		"postcode":     "10117",
		"country":      "Germany",
		"country_code": "DE",
	})
	for _, want := range []string{"Unter den Linden", "10117", "Berlin", "Germany"} {
		if !strings.Contains(got, want) {
			t.Errorf("DE format missing %q; got %q", want, got)
		}
	}
	// German convention puts the house number AFTER the street.
	street := strings.Index(got, "Unter den Linden")
	number := strings.Index(got, "1")
	if street < 0 || number < 0 || number < street {
		t.Errorf("DE expected street before number in %q", got)
	}
}

func TestFormatMissingFields(t *testing.T) {
	f := Global()
	got := f.Format(map[string]string{
		"city":         "Paris",
		"country":      "France",
		"country_code": "FR",
	})
	if !strings.Contains(got, "Paris") {
		t.Errorf("should include city when street/postcode missing; got %q", got)
	}
	if !strings.Contains(got, "France") {
		t.Errorf("should include country; got %q", got)
	}
}

func TestFormatUnknownCountryFallsBack(t *testing.T) {
	f := Global()
	got := f.Format(map[string]string{
		"road":         "Sample Street",
		"house_number": "42",
		"city":         "Neverland",
		"country":      "Neverwhere",
		"country_code": "XX",
	})
	if got == "" {
		t.Fatal("unknown country code should still render via default template")
	}
	if !strings.Contains(got, "Sample Street") || !strings.Contains(got, "Neverland") {
		t.Errorf("default template should still emit street and city; got %q", got)
	}
}

func TestFormatNoCountryCode(t *testing.T) {
	f := Global()
	got := f.Format(map[string]string{
		"city":    "Someplace",
		"country": "Somewhere",
	})
	if got == "" {
		t.Fatal("missing country_code should fall through to the default template")
	}
}
