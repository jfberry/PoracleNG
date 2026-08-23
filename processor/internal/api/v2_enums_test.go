package api

import (
	"strings"
	"testing"
)

// TestStringEnum_NameIntRoundTrip exercises the strict string-enum toolkit that
// the per-type fan-out reuses (team, gender, rsvp_changes, fort_type). gender is
// the one already wired into pokemon; the others are validated here so the
// toolkit is proven before 10 more types depend on it.
func TestStringEnum_NameIntRoundTrip(t *testing.T) {
	cases := []struct {
		e        stringEnum
		str      string
		wantInt  int
		wantBack string
	}{
		{genderEnum, "female", 2, "female"},
		{genderEnum, "genderless", 3, "genderless"},
		{teamEnum, "valor", 2, "valor"},
		{teamEnum, "any", 4, "any"},
		{rsvpChangesEnum, "rsvp_only", 2, "rsvp_only"},
		{fortTypeEnum, "gym", 1, "gym"},
	}
	for _, c := range cases {
		got, ok := c.e.toStored(c.str)
		if !ok || got != c.wantInt {
			t.Errorf("%s.toStored(%q) = %d,%v want %d,true", c.e.name, c.str, got, ok, c.wantInt)
		}
		if back := c.e.fromStored(c.wantInt); back != c.wantBack {
			t.Errorf("%s.fromStored(%d) = %q want %q", c.e.name, c.wantInt, back, c.wantBack)
		}
	}
}

// TestStringEnum_UnknownAndDefaults verifies the total/safe fallbacks the
// handler relies on (unknown string ⇒ default; unmapped int ⇒ default).
func TestStringEnum_UnknownAndDefaults(t *testing.T) {
	if _, ok := genderEnum.toStored("alien"); ok {
		t.Fatal("unknown gender string must not resolve")
	}
	// Unmapped stored int falls back to the documented default string.
	if got := teamEnum.fromStored(99); got != "any" {
		t.Fatalf("teamEnum.fromStored(99) = %q want any (default)", got)
	}
	// resolveStored: nil ⇒ default int.
	if got := genderEnum.resolveStored(nil); got != 0 {
		t.Fatalf("genderEnum.resolveStored(nil) = %d want 0 (any)", got)
	}
	male := "male"
	if got := genderEnum.resolveStored(&male); got != 1 {
		t.Fatalf("genderEnum.resolveStored(male) = %d want 1", got)
	}
}

// TestStringEnum_Canonical exercises the string-stored helper used by enums
// whose table column holds the canonical string itself (fort_type).
func TestStringEnum_Canonical(t *testing.T) {
	if got, ok := fortTypeEnum.canonical("pokestop"); !ok || got != "pokestop" {
		t.Errorf("fortTypeEnum.canonical(pokestop) = %q,%v want pokestop,true", got, ok)
	}
	if got, ok := fortTypeEnum.canonical("everything"); !ok || got != "everything" {
		t.Errorf("fortTypeEnum.canonical(everything) = %q,%v want everything,true", got, ok)
	}
	if got, ok := fortTypeEnum.canonical("portal"); ok || got != "" {
		t.Errorf("fortTypeEnum.canonical(portal) = %q,%v want \"\",false", got, ok)
	}
}

// TestNewStringEnum_GoodAndBadDefault verifies a well-formed enum constructs
// fine and a bad default panics at construction (fan-out typo caught at startup).
func TestNewStringEnum_GoodAndBadDefault(t *testing.T) {
	// Well-formed: must not panic.
	e := newStringEnum("ok", map[int]string{0: "a", 1: "b"}, "b")
	if got, ok := e.toStored("b"); !ok || got != 1 {
		t.Fatalf("well-formed enum toStored(b) = %d,%v want 1,true", got, ok)
	}

	// Bad default: must panic.
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("newStringEnum with bad default did not panic")
		}
	}()
	newStringEnum("bad", map[int]string{0: "a", 1: "b"}, "c")
}

// TestStringEnum_SchemaHelpers exercises the values()/enumDoc() schema/doc
// helpers the fan-out uses to populate huma enum constraints and field docs.
func TestStringEnum_SchemaHelpers(t *testing.T) {
	vals := genderEnum.values()
	want := []string{"any", "male", "female", "genderless"}
	if len(vals) != len(want) {
		t.Fatalf("gender values length = %d want %d", len(vals), len(want))
	}
	for i := range want {
		if vals[i] != want[i] {
			t.Fatalf("gender values[%d] = %q want %q (stored-int order)", i, vals[i], want[i])
		}
	}
	doc := genderEnum.enumDoc()
	if !strings.Contains(doc, "any | male | female | genderless") || !strings.Contains(doc, "default: any") {
		t.Fatalf("enumDoc unexpected: %q", doc)
	}
}
