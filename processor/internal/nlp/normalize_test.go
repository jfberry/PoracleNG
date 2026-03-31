package nlp

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Track Shiny Pikachu", "shiny pikachu"},
		{"NOTIFY ME ABOUT perfect dragonite", "perfect dragonite"},
		{"I want level 5 raids nearby", "level 5 raids nearby"},
		{"track hundos for pikachu eevee and dragonite", "hundos pikachu eevee dragonite"},
		{"looking for pikachu with good IVs", "pikachu good ivs"},
		{"tell me about the legendary raids", "legendary raids"},
		// Extra edge cases
		{"", ""},
		{"pikachu", "pikachu"},
		{"can you please find pikachu", "find pikachu"},
		{"track pikachu, eevee, dragonite", "pikachu eevee dragonite"},
		{"PLEASE   show   me   bulbasaur", "show bulbasaur"},
		{"a an the", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Normalize(tt.input)
			if got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
