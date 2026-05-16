package mappers

import "testing"

func TestLookupVersion(t *testing.T) {
	fn := Lookup("version")
	if fn == nil {
		t.Fatal("nil mapper for /version")
	}
	tokens, _ := fn(nil)
	if len(tokens) != 0 {
		t.Error("version should return empty")
	}
}

func TestLookupUnknownReturnsNil(t *testing.T) {
	if Lookup("does-not-exist") != nil {
		t.Error("expected nil for unknown")
	}
}
