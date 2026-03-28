package ai

import "testing"

func TestExtractCommands(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			"clean command",
			"!track pikachu iv100",
			"!track pikachu iv100",
		},
		{
			"multiple commands",
			"!track pikachu iv100\n!track eevee iv100",
			"!track pikachu iv100\n!track eevee iv100",
		},
		{
			"command with explanation",
			"Here is the command:\n!track pikachu iv100\nThis tracks pikachu.",
			"!track pikachu iv100",
		},
		{
			"markdown code fence",
			"```\n!track pikachu iv100\n```",
			"!track pikachu iv100",
		},
		{
			"error response",
			"ERROR: cannot determine pokemon name",
			"ERROR: cannot determine pokemon name",
		},
		{
			"no commands at all",
			"I don't understand your request.",
			"I don't understand your request.",
		},
		{
			"commands mixed with numbering",
			"1. !track pikachu iv100\n2. !track eevee iv100",
			"!track pikachu iv100\n!track eevee iv100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCommands(tt.input)
			if got != tt.expected {
				t.Errorf("extractCommands(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
