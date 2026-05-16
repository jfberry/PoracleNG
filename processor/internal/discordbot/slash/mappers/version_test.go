package mappers

import "testing"

func TestVersionMapper(t *testing.T) {
	tokens, err := Version(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected empty, got %v", tokens)
	}
}
