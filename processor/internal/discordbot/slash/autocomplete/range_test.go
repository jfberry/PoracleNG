package autocomplete

import "testing"

func TestIVRange_EmptyReturnsAllSuggestions(t *testing.T) {
	out := IVRange("")
	if len(out) != len(ivSuggestions) {
		t.Fatalf("got %d, want %d", len(out), len(ivSuggestions))
	}
	for i, s := range ivSuggestions {
		if out[i].Name != s || out[i].Value != s {
			t.Errorf("entry %d: got %+v, want %q", i, out[i], s)
		}
	}
}

func TestIVRange_EchoesUserInputFirst(t *testing.T) {
	out := IVRange("87")
	if len(out) == 0 {
		t.Fatalf("expected at least 1 choice")
	}
	if out[0].Name != "87" || out[0].Value != "87" {
		t.Errorf("first choice = %+v, want literal echo '87'", out[0])
	}
}

func TestIVRange_PrefixFilter(t *testing.T) {
	out := IVRange("9")
	found := false
	for _, c := range out {
		if c.Name == "95" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("'95' missing from prefix '9' results: %+v", out)
	}
	// '100' should NOT appear when focused='9' since it doesn't start with '9'.
	for _, c := range out {
		if c.Name == "100" {
			t.Errorf("'100' should not appear for focused '9': %+v", out)
		}
	}
}

func TestIVRange_DedupesUserInputAgainstSuggestion(t *testing.T) {
	// Typing "100" should not produce two "100" entries.
	out := IVRange("100")
	count := 0
	for _, c := range out {
		if c.Name == "100" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected single '100' entry, got %d", count)
	}
}
