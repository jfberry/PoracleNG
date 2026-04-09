package dts

import (
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
