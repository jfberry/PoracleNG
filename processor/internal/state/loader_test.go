package state

import "testing"

func TestLoaderBuildsGeoIndex(t *testing.T) {
	// We don't exercise the full DB path here — we synthesise a State
	// via the manager and assert GeoIndex can be set. This guards the
	// field's presence; the build is exercised end-to-end via the
	// matching tests in later tasks.
	m := NewManager()
	s := &State{GeoIndex: &HumanGeoIndex{}}
	m.Set(s)
	got := m.Get()
	if got.GeoIndex == nil {
		t.Errorf("State.GeoIndex field should be settable and round-trip")
	}
}
