package geocoding

import (
	"reflect"
	"testing"
)

func TestPhotonComponentsPromotesNameByType(t *testing.T) {
	props := photonProperties{
		Type:        "city",
		Name:        "Berlin",
		CountryCode: "de",
		Country:     "Germany",
	}

	components := photonComponents(props)

	if got := components["city"]; got != "Berlin" {
		t.Fatalf("expected promoted city to be Berlin, got %q", got)
	}
	if got := components["country_code"]; got != "de" {
		t.Fatalf("expected country_code to mirror countrycode, got %q", got)
	}
}

func TestPhotonComponentsPromotesRoadFromNameType(t *testing.T) {
	props := photonProperties{
		Type: "street",
		Name: "Unter den Linden",
	}

	components := photonComponents(props)

	if got := components["street"]; got != "Unter den Linden" {
		t.Fatalf("expected street from name/type promotion, got %q", got)
	}
	if got := components["road"]; got != "Unter den Linden" {
		t.Fatalf("expected road alias from name/type promotion, got %q", got)
	}
}

func TestPhotonComponentsDoesNotPromoteHouseFromNameType(t *testing.T) {
	props := photonProperties{
		Type: "house",
		Name: "I-Punkt Skateland",
	}

	components := photonComponents(props)

	if got := components["house"]; got != "" {
		t.Fatalf("expected house promotion to be skipped, got %q", got)
	}
}

func TestFormatCompactAddress(t *testing.T) {
	tests := []struct {
		name string
		addr Address
		want string
	}{
		{
			name: "suburb street city",
			addr: Address{Suburb: "Mitte", StreetName: "Friedrichstrasse", StreetNumber: "42", City: "Berlin"},
			want: "Mitte: Friedrichstrasse 42, Berlin",
		},
		{
			name: "city street when no suburb",
			addr: Address{StreetName: "Main Street", StreetNumber: "1", City: "Springfield"},
			want: "Springfield: Main Street 1",
		},
		{
			name: "city only",
			addr: Address{City: "Munich"},
			want: "Munich:",
		},
		{
			name: "formatted address fallback",
			addr: Address{FormattedAddress: "Berlin Germany"},
			want: "Berlin Germany",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCompactAddress(tt.addr)
			if got != tt.want {
				t.Fatalf("FormatCompactAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPhotonStreetName(t *testing.T) {
	tests := []struct {
		name       string
		props      photonProperties
		components map[string]string
		compact    bool
		want       string
	}{
		{
			name:       "uses street when present",
			props:      photonProperties{Street: "Main Street", Name: "POI Name", Type: "other"},
			components: map[string]string{},
			compact:    true,
			want:       "Main Street",
		},
		{
			name:       "uses name for compact other with no street",
			props:      photonProperties{Type: "other", Name: "I-Punkt Skateland"},
			components: map[string]string{},
			compact:    true,
			want:       "I-Punkt Skateland",
		},
		{
			name:       "does not use name for non-compact",
			props:      photonProperties{Type: "other", Name: "I-Punkt Skateland"},
			components: map[string]string{},
			compact:    false,
			want:       "",
		},
		{
			name:       "does not use name for non-other type",
			props:      photonProperties{Type: "city", Name: "Berlin"},
			components: map[string]string{},
			compact:    true,
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := photonStreetName(tt.props, tt.components, tt.compact)
			if got != tt.want {
				t.Fatalf("photonStreetName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPhotonTypeAliasMapping(t *testing.T) {
	cases := map[string]string{
		"housenumber": "house_number",
		"street":      "road",
		"district":    "city_district",
		"countrycode": "country_code",
		"city":        "city",
	}

	for in, want := range cases {
		if got := photonToOCKey(in); got != want {
			t.Fatalf("photonToOCKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFilterNonEmpty(t *testing.T) {
	got := filterNonEmpty("", "  a  ", " ", "b")
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterNonEmpty() = %#v, want %#v", got, want)
	}
}
