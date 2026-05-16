package mappers

import "testing"

func TestTrackedMapper(t *testing.T) {
	tokens, err := Tracked(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected empty, got %v", tokens)
	}
}

func TestLookupTracked(t *testing.T) {
	if Lookup("tracked") == nil {
		t.Fatal("nil mapper for /tracked")
	}
}
