package webhook

import (
	"encoding/json"
	"testing"
)

// recordingProcessor counts which Process* method routePokestop dispatches to.
type recordingProcessor struct{ called map[string]int }

func newRecordingProcessor() *recordingProcessor {
	return &recordingProcessor{called: map[string]int{}}
}
func (p *recordingProcessor) rec(name string) error { p.called[name]++; return nil }

func (p *recordingProcessor) ProcessPokemon(json.RawMessage) error    { return p.rec("pokemon") }
func (p *recordingProcessor) ProcessRaid(json.RawMessage) error       { return p.rec("raid") }
func (p *recordingProcessor) ProcessWeather(json.RawMessage) error    { return p.rec("weather") }
func (p *recordingProcessor) ProcessInvasion(json.RawMessage) error   { return p.rec("invasion") }
func (p *recordingProcessor) ProcessQuest(json.RawMessage) error      { return p.rec("quest") }
func (p *recordingProcessor) ProcessLure(json.RawMessage) error       { return p.rec("lure") }
func (p *recordingProcessor) ProcessGym(json.RawMessage) error        { return p.rec("gym") }
func (p *recordingProcessor) ProcessNest(json.RawMessage) error       { return p.rec("nest") }
func (p *recordingProcessor) ProcessFortUpdate(json.RawMessage) error { return p.rec("fort") }
func (p *recordingProcessor) ProcessMaxbattle(json.RawMessage) error  { return p.rec("maxbattle") }
func (p *recordingProcessor) ProcessShowcase(json.RawMessage) error   { return p.rec("showcase") }

func routeStop(t *testing.T, body string) *recordingProcessor {
	t.Helper()
	p := newRecordingProcessor()
	h := NewHandler(p, nil)
	if err := h.routePokestop(json.RawMessage(body)); err != nil {
		t.Fatalf("routePokestop: %v", err)
	}
	return p
}

// A showcase-only pokéstop webhook must dispatch to ProcessShowcase and must NOT
// fall through the legacy fallback into ProcessInvasion.
func TestRoutePokestop_ShowcaseOnly(t *testing.T) {
	p := routeStop(t, `{"pokestop_id":"s","showcase_expiry":1784084400}`)
	if p.called["showcase"] != 1 {
		t.Errorf("expected ProcessShowcase once, got %d", p.called["showcase"])
	}
	if p.called["invasion"] != 0 {
		t.Errorf("showcase-only stop must NOT fall through to ProcessInvasion, got %d", p.called["invasion"])
	}
	if p.called["lure"] != 0 {
		t.Errorf("no lure expected, got %d", p.called["lure"])
	}
}

// A snapshot carrying both a lure and a showcase dispatches to both, and still
// must not reach the invasion fallback.
func TestRoutePokestop_LureAndShowcase(t *testing.T) {
	p := routeStop(t, `{"pokestop_id":"s","lure_expiration":1784084400,"lure_id":501,"showcase_expiry":1784084400}`)
	if p.called["lure"] != 1 || p.called["showcase"] != 1 {
		t.Errorf("expected both lure and showcase, got lure=%d showcase=%d", p.called["lure"], p.called["showcase"])
	}
	if p.called["invasion"] != 0 {
		t.Errorf("must not fall through to invasion, got %d", p.called["invasion"])
	}
}

// A real invasion (grunt) still routes to ProcessInvasion, not showcase.
func TestRoutePokestop_InvasionUnaffected(t *testing.T) {
	p := routeStop(t, `{"pokestop_id":"s","incident_grunt_type":20}`)
	if p.called["invasion"] != 1 {
		t.Errorf("expected ProcessInvasion, got %d", p.called["invasion"])
	}
	if p.called["showcase"] != 0 {
		t.Errorf("no showcase expected, got %d", p.called["showcase"])
	}
}
