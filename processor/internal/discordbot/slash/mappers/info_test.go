package mappers

import "testing"

func TestInfoMapper(t *testing.T) {
	tokens, err := Info(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected empty, got %v", tokens)
	}
}

func TestLookupInfo(t *testing.T) {
	if Lookup("info") == nil {
		t.Fatal("nil mapper for /info")
	}
}
