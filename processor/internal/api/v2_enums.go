package api

import (
	"fmt"
	"sort"
	"strings"
)

// v2 string-enum toolkit.
//
// v2 tracking fields that are small, stable, human-named categories (team,
// gender, fort_type, rsvp_changes) are modelled as STRING enums on the wire,
// translated down to the stored integer/string the engine already uses. This
// is the strict counterpart to v1's flex coercion: only the documented enum
// strings are accepted; anything else is a 422 (enforced by huma's generated
// `enum:[...]` schema). The name↔int maps below stay the single source of
// truth for storage translation during the per-type fan-out.
//
// Game-master dictionary values (pokemon_id, form, move, reward_type, lure_id,
// pvp_ranking_league as a CP cap, invasion ids, incident display_type) stay
// INT and are NOT modelled here — their sets grow with the game.

// stringEnum describes a fixed string↔int enumeration. The zero value is not
// usable; build instances via newStringEnum so the reverse map and the sorted
// string list (for the huma schema) are derived once.
type stringEnum struct {
	name     string         // field name, for error messages
	toInt    map[string]int // canonical string → stored int
	fromInt  map[int]string // stored int → canonical string
	ordered  []string       // string values in stored-int order (for enum schema + docs)
	defValue string         // documented default string
}

// newStringEnum builds a stringEnum from a stored-int → string map. The huma
// schema enum list is ordered by the stored int so the documentation reads
// naturally (e.g. harmony, mystic, valor, instinct, any). defValue is the
// documented default for the field (must be one of the strings).
func newStringEnum(name string, fromInt map[int]string, defValue string) stringEnum {
	toInt := make(map[string]int, len(fromInt))
	keys := make([]int, 0, len(fromInt))
	for k := range fromInt {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	ordered := make([]string, 0, len(fromInt))
	for _, k := range keys {
		s := fromInt[k]
		toInt[s] = k
		ordered = append(ordered, s)
	}
	// Fail fast on a bad default: these enums are package-level vars built at
	// init, so a fan-out author's typo in defValue would otherwise silently make
	// resolveStored/fromStored return a wrong/zero value at runtime. Panic at
	// startup instead.
	if _, ok := toInt[defValue]; !ok {
		panic(fmt.Sprintf("newStringEnum(%q): default %q is not one of %v", name, defValue, ordered))
	}
	return stringEnum{
		name:     name,
		toInt:    toInt,
		fromInt:  fromInt,
		ordered:  ordered,
		defValue: defValue,
	}
}

// values returns the accepted enum strings (stored-int order). Used to populate
// the huma `enum:[...]` schema constraint and the field documentation.
func (e stringEnum) values() []string { return e.ordered }

// toStored translates a canonical enum string to its stored integer. ok is
// false for any string outside the enum (defence-in-depth; huma's schema
// validation rejects unknown strings before the handler runs).
func (e stringEnum) toStored(s string) (int, bool) {
	v, ok := e.toInt[s]
	return v, ok
}

// fromStored translates a stored integer back to its canonical enum string,
// falling back to the documented default for any unmapped value (mirrors the
// engine's clamping behaviour, e.g. an out-of-range team stored as the raw int).
func (e stringEnum) fromStored(v int) string {
	if s, ok := e.fromInt[v]; ok {
		return s
	}
	return e.defValue
}

// canonical validates a string against the enum and returns its canonical form
// (the exact spelling in the enum's name set) plus an ok bool. It is the helper
// for STRING-stored enums (e.g. fort_type, whose table column holds the
// canonical string itself, not an int): the fan-out Translate validates the
// incoming value and stores the returned canonical string directly, without
// detouring through the stored-int representation. Unknown strings return
// ("", false) so the caller can fall back to the documented default.
func (e stringEnum) canonical(s string) (string, bool) {
	if _, ok := e.toInt[s]; ok {
		return s, true
	}
	return "", false
}

// resolveStored is the handler-side helper: it turns an optional pointer enum
// string (nil ⇒ documented default) into the stored int. An empty/unknown
// string is treated as the default (huma validation makes the unknown case
// unreachable in practice, but keeping it total avoids a panic path).
func (e stringEnum) resolveStored(s *string) int {
	if s == nil || *s == "" {
		v, _ := e.toStored(e.defValue)
		return v
	}
	if v, ok := e.toStored(*s); ok {
		return v
	}
	v, _ := e.toStored(e.defValue)
	return v
}

// enumDoc renders a "one of: a|b|c (default: d)" fragment for field docs.
func (e stringEnum) enumDoc() string {
	var out strings.Builder
	out.WriteString("one of:")
	for i, s := range e.ordered {
		if i == 0 {
			out.WriteString(" " + s)
		} else {
			out.WriteString(" | " + s)
		}
	}
	return fmt.Sprintf("%s (default: %s)", out.String(), e.defValue)
}

// --- Concrete enums -------------------------------------------------------

// genderEnum is the pokemon gender filter: any(0) | male(1) | female(2) |
// genderless(3). (Invasion reuses a 0–2 subset during the fan-out; that variant
// will get its own enum with genderless omitted.)
var genderEnum = newStringEnum("gender", map[int]string{
	0: "any",
	1: "male",
	2: "female",
	3: "genderless",
}, "any")

// invasionGenderEnum is the invasion grunt gender filter: any(0) | male(1) |
// female(2). It DELIBERATELY omits genderless (which the pokemon genderEnum
// carries) — a grunt is always gendered, so 3 is not a valid stored value here.
var invasionGenderEnum = newStringEnum("gender", map[int]string{
	0: "any",
	1: "male",
	2: "female",
}, "any")

// teamEnum (raid/egg/gym): harmony(0) | mystic(1) | valor(2) | instinct(3) | any(4).
// Slotted in here for the fan-out; pokemon doesn't use it.
var teamEnum = newStringEnum("team", map[int]string{
	0: "harmony",
	1: "mystic",
	2: "valor",
	3: "instinct",
	4: "any",
}, "any")

// rsvpChangesEnum (raid/egg): none(0) | rsvp(1) | rsvp_only(2).
var rsvpChangesEnum = newStringEnum("rsvp_changes", map[int]string{
	0: "none",
	1: "rsvp",
	2: "rsvp_only",
}, "none")

// fortTypeEnum (fort): pokestop | gym | everything. Stored as the string itself
// (not an int) — the fan-out fort translation will use fromStored/toStored only
// for validation, then store the canonical string. The int keys here are an
// arbitrary stable ordering for the schema, not a stored representation.
var fortTypeEnum = newStringEnum("fort_type", map[int]string{
	0: "pokestop",
	1: "gym",
	2: "everything",
}, "everything")
