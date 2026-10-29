package webhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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
func (p *recordingProcessor) ProcessTappable(json.RawMessage) error   { return p.rec("tappable") }

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

// postBatch runs a webhook batch through the gin handler.
func postBatch(t *testing.T, body string) *recordingProcessor {
	t.Helper()
	gin.SetMode(gin.TestMode)
	p := newRecordingProcessor()
	h := NewHandler(p, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	h.handle(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	return p
}

func TestHandle_TappableRoutesToProcessTappable(t *testing.T) {
	p := postBatch(t, `[{"type":"tappable","message":{"encounter_id":"1","rewards":[{"pokemon_candy":41,"count":6}]}},
		{"type":"max_battle","message":{"id":"s"}}]`)
	if p.called["tappable"] != 1 {
		t.Errorf("ProcessTappable calls = %d, want 1", p.called["tappable"])
	}
	if p.called["maxbattle"] != 1 {
		t.Errorf("ProcessMaxbattle calls = %d, want 1", p.called["maxbattle"])
	}
	if len(p.called) != 2 {
		t.Errorf("unexpected dispatches: %v", p.called)
	}
}

// A real Golbat tappable payload decodes with its nullable fields intact.
func TestTappableWebhook_Decode(t *testing.T) {
	raw := `{"encounter_id":"16082073574809335024","spawnpoint_id":"476daa23b5b","pokestop_id":"None","pokestop_name":null,
		"latitude":48.16015080292662,"longitude":16.416816693472395,"type":"TAPPABLE_TYPE_BOX",
		"reward_type":"pokemon_candy","item_id":null,"reward_pokemon_id":41,"count":6,
		"rewards":[{"pokemon_candy":41,"count":6}],
		"disappear_time":1793220544,"disappear_time_verified":true,"updated":1793219372}`
	var tw TappableWebhook
	if err := json.Unmarshal([]byte(raw), &tw); err != nil {
		t.Fatal(err)
	}
	if tw.EncounterID != "16082073574809335024" {
		t.Errorf("encounter_id = %q", tw.EncounterID)
	}
	if tw.HasPokestop() {
		t.Error(`pokestop_id "None" must not count as a pokestop`)
	}
	if tw.PokestopName != nil || tw.ItemID != nil {
		t.Errorf("null fields decoded non-nil: name=%v item=%v", tw.PokestopName, tw.ItemID)
	}
	if tw.RewardType == nil || *tw.RewardType != "pokemon_candy" || tw.RewardPokemonID == nil || *tw.RewardPokemonID != 41 || tw.Count == nil || *tw.Count != 6 {
		t.Errorf("reward columns = %v %v %v", tw.RewardType, tw.RewardPokemonID, tw.Count)
	}
	if string(tw.Rewards) != `[{"pokemon_candy":41,"count":6}]` {
		t.Errorf("rewards = %s", tw.Rewards)
	}
	if tw.DisappearTime != 1793220544 || !tw.DisappearTimeVerified || tw.Updated != 1793219372 {
		t.Errorf("times = %d %v %d", tw.DisappearTime, tw.DisappearTimeVerified, tw.Updated)
	}
}
