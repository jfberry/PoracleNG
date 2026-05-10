package tracker

import "encoding/json"

// StripPVP returns the input pokemon webhook bytes with the top-level "pvp"
// field removed. The encounter tracker stores prior-sighting bytes to power
// the {{original.X}} template namespace; PVP rankings are large nested
// arrays (great/ultra/little/master, several KB per webhook) and don't
// contribute anything to "what changed" templates, so they're stripped at
// storage time to keep the tracker's resident memory bounded.
//
// On any unmarshal/marshal failure the original bytes are returned unchanged
// — the caller still has a usable webhook, even if larger than ideal.
func StripPVP(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	if _, ok := m["pvp"]; !ok {
		// Nothing to strip — return the original bytes unchanged so callers
		// don't pay for a re-marshal on PVP-less webhooks (cell spawns,
		// non-encountered sightings).
		return raw
	}
	delete(m, "pvp")
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}
