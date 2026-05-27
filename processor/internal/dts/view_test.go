package dts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEscapeJSONString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`hello`, `hello`},
		{`say "hi"`, `say ''hi''`},
		{"line1\nline2", "line1 line2"},
		{"line1\r\nline2", "line1 line2"},
		{`path\to\file`, `path?to?file`},
		{`"quote" and\back\n`, `''quote'' and?back?n`}, // literal \n in source, not newline
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, escapeJSONString(tt.input), "input: %q", tt.input)
	}
}

func TestEscapeUserContentLayered_GeocodedFields(t *testing.T) {
	base := map[string]any{
		"addr":             `123 Main "St", Springfield`,
		"formattedAddress": "Line1\nLine2",
		"city":             `O'Connor's "place"`,
		"streetName":       `Rue de l\Étoile`,
		"country":          `Côte d"Ivoire`,
	}
	computed := map[string]any{}
	escapeUserContentLayered(computed, base)

	got, ok := computed["addr"].(string)
	if !ok || strings.Contains(got, `"`) {
		t.Errorf("addr not escaped: %q", got)
	}
	got, ok = computed["formattedAddress"].(string)
	if !ok || strings.Contains(got, "\n") {
		t.Errorf("formattedAddress newline not escaped: %q", got)
	}
	got, ok = computed["city"].(string)
	if !ok || strings.Contains(got, `"`) {
		t.Errorf("city not escaped: %q", got)
	}
	got, ok = computed["streetName"].(string)
	if !ok || strings.Contains(got, `\`) {
		t.Errorf("streetName backslash not escaped: %q", got)
	}
	got, ok = computed["country"].(string)
	if !ok || strings.Contains(got, `"`) {
		t.Errorf("country not escaped: %q", got)
	}
}
