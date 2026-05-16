package scanner

import "testing"

// NewGolbat and NewRDM must return the Scanner INTERFACE (not a
// concrete pointer) so the error path produces a true nil interface
// for callers. Returning a concrete *GolbatScanner / *RDMScanner
// would assign a typed-nil into the caller's Scanner variable, which
// compares != nil but panics on the first method call (nil receiver
// derefs s.db). The dial-failure traceback was
//
//   scanner.(*GolbatScanner).GetStopData(0x0, ...)
//     scanner/golbat.go:50 +0x14a
//
// — the 0x0 receiver is the smoking gun. This test guards against a
// future signature regression that would re-introduce the panic.
func TestConstructors_ErrorReturnsNilInterface(t *testing.T) {
	cases := []struct {
		name string
		fn   func() (Scanner, error)
	}{
		{"NewGolbat", func() (Scanner, error) { return NewGolbat("invalid-dsn") }},
		{"NewRDM", func() (Scanner, error) { return NewRDM("invalid-dsn") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := c.fn()
			if err == nil {
				t.Fatalf("%s with invalid DSN should error", c.name)
			}
			if s != nil {
				t.Errorf("%s on error must return a true nil interface (got typed-nil %T = %v) — downstream `== nil` gates will fail and methods will panic", c.name, s, s)
			}
		})
	}
}
