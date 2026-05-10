package tracker

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStripPVP_RemovesPVPField(t *testing.T) {
	in := []byte(`{"pokemon_id":25,"cp":1500,"pvp":{"great":[{"rank":1,"percentage":1.0}],"ultra":[{"rank":2}]}}`)
	out := StripPVP(in)
	if strings.Contains(string(out), "\"pvp\"") {
		t.Errorf("pvp field not removed: %s", out)
	}
	// Other fields preserved.
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output not valid json: %v", err)
	}
	if m["pokemon_id"] != float64(25) {
		t.Errorf("pokemon_id lost: got %v", m["pokemon_id"])
	}
	if m["cp"] != float64(1500) {
		t.Errorf("cp lost: got %v", m["cp"])
	}
}

func TestStripPVP_NoPVPUnchanged(t *testing.T) {
	in := []byte(`{"pokemon_id":25,"cp":1500}`)
	out := StripPVP(in)
	// On a PVP-less input we expect to skip re-marshalling — the bytes
	// should be identical to the input.
	if string(out) != string(in) {
		t.Errorf("PVP-less webhook should round-trip unchanged.\n  in:  %s\n  out: %s", in, out)
	}
}

func TestStripPVP_MalformedReturnedUnchanged(t *testing.T) {
	in := []byte(`{not json`)
	out := StripPVP(in)
	if string(out) != string(in) {
		t.Errorf("malformed input should be returned unchanged.\n  in:  %s\n  out: %s", in, out)
	}
}

func TestStripPVP_EmptyReturnedUnchanged(t *testing.T) {
	out := StripPVP(nil)
	if out != nil {
		t.Errorf("nil input should be returned unchanged, got %v", out)
	}
	out = StripPVP([]byte{})
	if len(out) != 0 {
		t.Errorf("empty input should be returned unchanged, got %v", out)
	}
}

// TestStripPVP_ShrinksLargePayload confirms the strip is materially smaller —
// a realistic-shaped PVP block (10 entries per league, 4 leagues) collapses
// to a couple of small fields. Not a strict size assertion; the production
// goal is "much smaller", not a specific number.
func TestStripPVP_ShrinksLargePayload(t *testing.T) {
	// Build a PVP block that's clearly large — 20 entries per league with
	// realistic field shapes from Golbat.
	type entry struct {
		Rank        int     `json:"rank"`
		Percentage  float64 `json:"percentage"`
		Cap         int     `json:"cap"`
		Capped      bool    `json:"capped"`
		Pokemon     int     `json:"pokemon"`
		Form        int     `json:"form"`
		Level       float64 `json:"level"`
		CP          int     `json:"cp"`
		Competition float64 `json:"competition_rank_percentage"`
	}
	mkLeague := func(n int) []entry {
		out := make([]entry, n)
		for i := range out {
			out[i] = entry{Rank: i + 1, Percentage: 0.95, Cap: 50, Pokemon: 25, Level: 30.5, CP: 1499}
		}
		return out
	}
	body := map[string]any{
		"pokemon_id": 25,
		"cp":         1500,
		"latitude":   52.5,
		"longitude":  13.4,
		"pvp": map[string]any{
			"great":  mkLeague(20),
			"ultra":  mkLeague(20),
			"little": mkLeague(20),
			"master": mkLeague(20),
		},
	}
	raw, _ := json.Marshal(body)
	stripped := StripPVP(raw)
	if len(stripped) >= len(raw)/2 {
		t.Errorf("strip should materially shrink a PVP-laden webhook: in=%d out=%d", len(raw), len(stripped))
	}
}
