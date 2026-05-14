package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// fakeChangeSender is a minimal Sender stub. Nothing in the change-dispatch
// path actually invokes it, but the MessageTracker constructor needs at least
// one entry in its senders map to register clean-deletion callbacks for any
// platform we might track under.
type fakeChangeSender struct{}

func (fakeChangeSender) Send(_ context.Context, _ *delivery.Job) (*delivery.SentMessage, error) {
	return &delivery.SentMessage{ID: "ignored"}, nil
}
func (fakeChangeSender) Delete(_ context.Context, _ string) error { return nil }
func (fakeChangeSender) Edit(_ context.Context, _ string, _ json.RawMessage, _ []byte) error {
	return nil
}
func (fakeChangeSender) Platform() string          { return "discord" }
func (fakeChangeSender) WaitForRateLimit(_ string) {}

// newTestDispatcher returns a Dispatcher backed by a freshly-allocated
// MessageTracker so tests can pre-populate reply-index entries via
// MessageTracker.Track. The dispatcher is not started — the test does not
// drive any actual deliveries.
func newTestDispatcher(t *testing.T) *delivery.Dispatcher {
	t.Helper()
	senders := map[string]delivery.Sender{"discord": fakeChangeSender{}}
	mt := delivery.NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(mt.Stop)
	return delivery.NewDispatcherWithSenders(senders, mt, 16, delivery.QueueConfig{})
}

// drainRenderJobs reads every RenderJob currently buffered in the channel into
// a slice and returns it. Closes nothing; intended for tests that send jobs
// onto a buffered channel and then assert their shape.
func drainRenderJobs(ch chan RenderJob) []RenderJob {
	var jobs []RenderJob
	for {
		select {
		case j := <-ch:
			jobs = append(jobs, j)
		default:
			return jobs
		}
	}
}

// minimalProcessor returns a ProcessorService stripped down to the pieces
// the change-render dispatcher actually touches: the dispatcher (for the
// MessageTracker), the render channel, the cfg (for default locale), and an
// enricher (a default Translations bundle is fine — BuildOriginalView treats
// nil GameData as "skip name resolution").
func minimalProcessor(t *testing.T) (*ProcessorService, chan RenderJob, *delivery.Dispatcher) {
	t.Helper()
	d := newTestDispatcher(t)
	ch := make(chan RenderJob, 4)
	ps := &ProcessorService{
		dispatcher: d,
		renderCh:   ch,
		enricher:   &enrichment.Enricher{},
		cfg:        &config.Config{General: config.GeneralConfig{Locale: "en"}},
	}
	return ps, ch, d
}

// Matched user receives `monster` (never monsterChanged) with
// ReplyKey set so the delivery queue links to any prior message.
func TestDispatchPokemonAlert_MatchedUserStillMatches(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)

	encounterID := "enc-still-matches"
	user := webhook.MatchedUser{ID: "user-A", Type: "discord:user"}

	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeForm,
		Old:         tracker.EncounterState{PokemonID: 25, Form: 0, CP: 800, ATK: 10, DEF: 10, STA: 10},
		New:         tracker.EncounterState{PokemonID: 25, Form: 65, CP: 850, ATK: 10, DEF: 10, STA: 10},
	}

	ps.dispatchPokemonAlert(pokemonDispatchInput{
		encounterID:   encounterID,
		change:        change,
		matched:       []webhook.MatchedUser{user},
		isEncountered: true,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 RenderJob, got %d", len(jobs))
	}
	j := jobs[0]
	if j.IsChange {
		t.Errorf("matched-still-matches user must receive `monster` (IsChange=false), got IsChange=true")
	}
	if j.ReplyKey != encounterID {
		t.Errorf("ReplyKey must always be set so reply linking happens at delivery time: got %q, want %q", j.ReplyKey, encounterID)
	}
	if j.OriginalView != nil {
		t.Errorf("OriginalView should NOT be populated for the monster-template branch, got %v", j.OriginalView)
	}
	if len(j.MatchedUsers) != 1 || j.MatchedUsers[0].ID != "user-A" {
		t.Errorf("MatchedUsers should be [user-A], got %v", j.MatchedUsers)
	}
}

// Prior-only user (matched at T1, no longer matches at T2) receives
// `monsterChanged` as a reply with OriginalView populated.
func TestDispatchPokemonAlert_PriorOnlyNoLongerMatches(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)

	encounterID := "enc-bad-news"
	// Clean=1 simulates inheritance from the T1 message so the
	// monsterChanged reply auto-deletes alongside the original.
	priorOnly := webhook.MatchedUser{ID: "user-bad-news", Type: "discord:user", Clean: 1}

	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeSpecies,
		Old:         tracker.EncounterState{PokemonID: 240, CP: 240, ATK: 15, DEF: 15, STA: 15}, // 100% Magmar
		New:         tracker.EncounterState{PokemonID: 218, CP: 100, ATK: 5, DEF: 5, STA: 5},    // Slugma the user doesn't track
	}

	ps.dispatchPokemonAlert(pokemonDispatchInput{
		encounterID:    encounterID,
		change:         change,
		matched:        nil, // nobody currently matches
		priorOnlyUsers: []webhook.MatchedUser{priorOnly},
		isEncountered:  true,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 RenderJob (monsterChanged for prior-only user), got %d", len(jobs))
	}
	j := jobs[0]
	if !j.IsChange {
		t.Errorf("prior-only no-longer-matching user must receive monsterChanged (IsChange=true), got false")
	}
	if j.ReplyKey != encounterID {
		t.Errorf("ReplyKey must be set so the bad-news message threads under the original: got %q, want %q", j.ReplyKey, encounterID)
	}
	if j.OriginalView == nil {
		t.Fatalf("OriginalView must be populated so {{original.X}} can show the previous identity")
	}
	if got := j.OriginalView["pokemonId"]; got != 240 {
		t.Errorf("OriginalView should reflect the prior species (Magmar=240), got %v", got)
	}
	if j.ChangeType != "species" {
		t.Errorf("ChangeType: got %q, want \"species\"", j.ChangeType)
	}
	if len(j.MatchedUsers) != 1 || j.MatchedUsers[0].ID != "user-bad-news" {
		t.Errorf("MatchedUsers should be [user-bad-news], got %v", j.MatchedUsers)
	}
	if j.MatchedUsers[0].Clean != 1 {
		t.Errorf("Clean must propagate from priorOnly fixture into the RenderJob's MatchedUser (else monsterChanged outlives the original): got %d, want 1", j.MatchedUsers[0].Clean)
	}
	// Template-facing fields: the collapsed bucket + localised label.
	// minimalProcessor has no Translations bundle, so T() returns the
	// key — that's the assertion target.
	lang := effectiveLanguage(priorOnly, ps.cfg.General.Locale)
	if got := j.PerLangEnrichment[lang]["changeType"]; got != "species" {
		t.Errorf("PerLangEnrichment[%s][changeType] = %v, want \"species\"", lang, got)
	}
	if got := j.PerLangEnrichment[lang]["changeTypeText"]; got != "change_type_text_species" {
		t.Errorf("PerLangEnrichment[%s][changeTypeText] = %v, want fallback key", lang, got)
	}
}

// ChangeEncountered (CP 0 → >0) must NOT fire monsterChanged for
// prior-only users: their filter excluded the new state, sending a
// "stats revealed" follow-up would contradict their tracking rule.
func TestDispatchPokemonAlert_EncounteredChange_PriorOnly_NoJob(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)

	encounterID := "enc-iv-reveal"
	priorOnly := webhook.MatchedUser{ID: "user-iv-strict", Type: "discord:user"}

	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeEncountered,
		Old:         tracker.EncounterState{PokemonID: 25, CP: 0}, // unencountered
		New:         tracker.EncounterState{PokemonID: 25, CP: 500, ATK: 5, DEF: 5, STA: 5},
	}

	ps.dispatchPokemonAlert(pokemonDispatchInput{
		encounterID:    encounterID,
		change:         change,
		matched:        nil,
		priorOnlyUsers: []webhook.MatchedUser{priorOnly},
		isEncountered:  true,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 0 {
		t.Fatalf("expected 0 RenderJobs (ChangeEncountered skips monsterChanged), got %d: %v", len(jobs), jobs)
	}
}

// ChangeWeatherBoost maps to the "stats" bucket — same pokemon,
// different effective IVs / CP. This is the most common real-world
// trigger for monsterChanged in production.
func TestDispatchPokemonAlert_WeatherBoost_StatsBucket(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)

	encounterID := "enc-weather"
	priorOnly := webhook.MatchedUser{ID: "user-100iv-only", Type: "discord:user"}

	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeWeatherBoost,
		Old:         tracker.EncounterState{PokemonID: 25, CP: 800, Weather: 1, ATK: 15, DEF: 15, STA: 15},
		New:         tracker.EncounterState{PokemonID: 25, CP: 640, Weather: 0, ATK: 15, DEF: 15, STA: 15},
	}

	ps.dispatchPokemonAlert(pokemonDispatchInput{
		encounterID:    encounterID,
		change:         change,
		matched:        nil,
		priorOnlyUsers: []webhook.MatchedUser{priorOnly},
		isEncountered:  true,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 monsterChanged RenderJob, got %d", len(jobs))
	}
	j := jobs[0]
	if !j.IsChange {
		t.Errorf("weather-boost change must use monsterChanged (IsChange=true)")
	}
	lang := effectiveLanguage(priorOnly, ps.cfg.General.Locale)
	if got := j.PerLangEnrichment[lang]["changeType"]; got != "stats" {
		t.Errorf("PerLangEnrichment[%s][changeType] = %v, want \"stats\"", lang, got)
	}
	if got := j.PerLangEnrichment[lang]["changeTypeText"]; got != "change_type_text_stats" {
		t.Errorf("PerLangEnrichment[%s][changeTypeText] = %v, want fallback key", lang, got)
	}
}

// Mixed: matched (A, C) share a language and batch into one
// `monster` RenderJob; prior-only B gets a separate monsterChanged.
func TestDispatchPokemonAlert_MixedMatchedAndPriorOnly(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)

	encounterID := "enc-mixed"
	userA := webhook.MatchedUser{ID: "user-A", Type: "discord:user", Language: "en"}
	userB := webhook.MatchedUser{ID: "user-B", Type: "discord:user", Language: "en"}
	userC := webhook.MatchedUser{ID: "user-C", Type: "discord:user", Language: "en"}

	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeForm,
		Old:         tracker.EncounterState{PokemonID: 25, Form: 0, CP: 800, ATK: 10, DEF: 10, STA: 10},
		New:         tracker.EncounterState{PokemonID: 25, Form: 65, CP: 850, ATK: 10, DEF: 10, STA: 10},
	}

	ps.dispatchPokemonAlert(pokemonDispatchInput{
		encounterID:    encounterID,
		change:         change,
		matched:        []webhook.MatchedUser{userA, userC}, // A still matches; C is new
		priorOnlyUsers: []webhook.MatchedUser{userB},        // B no longer matches
		isEncountered:  true,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 RenderJobs (one monster batch + one monsterChanged), got %d", len(jobs))
	}

	var monsterJob, changedJob *RenderJob
	for i := range jobs {
		if jobs[i].IsChange {
			changedJob = &jobs[i]
		} else {
			monsterJob = &jobs[i]
		}
	}
	if monsterJob == nil || changedJob == nil {
		t.Fatalf("expected one monster job and one monsterChanged job, got: monsterJob=%v changedJob=%v", monsterJob, changedJob)
	}
	if len(monsterJob.MatchedUsers) != 2 {
		t.Errorf("monster job should batch userA + userC (same language), got %d users: %v", len(monsterJob.MatchedUsers), monsterJob.MatchedUsers)
	}
	if len(changedJob.MatchedUsers) != 1 || changedJob.MatchedUsers[0].ID != "user-B" {
		t.Errorf("monsterChanged job should target only user-B, got %v", changedJob.MatchedUsers)
	}
	if changedJob.OriginalView == nil {
		t.Errorf("monsterChanged job should have OriginalView populated")
	}
	if monsterJob.OriginalView != nil {
		t.Errorf("monster job should NOT have OriginalView populated, got %v", monsterJob.OriginalView)
	}
	// Both jobs must set ReplyKey — monster so the delivery queue
	// attaches reply metadata for A (who has a prior), monsterChanged
	// for B's reply, and the fresh-send for C will seed the
	// reply-index for future changes.
	if monsterJob.ReplyKey != encounterID || changedJob.ReplyKey != encounterID {
		t.Errorf("both jobs must carry ReplyKey=encounterID, got monster=%q changed=%q", monsterJob.ReplyKey, changedJob.ReplyKey)
	}
}

// TestDispatchPokemonAlert_NoMatchedNoPrior_NoOp: defensive guard.
// When both lists are empty the dispatcher should emit nothing —
// even though `change` is non-nil, there's no one to notify.
func TestDispatchPokemonAlert_NoMatchedNoPrior_NoOp(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)

	change := &tracker.EncounterChange{
		EncounterID: "enc-empty",
		Type:        tracker.ChangeForm,
	}
	ps.dispatchPokemonAlert(pokemonDispatchInput{
		encounterID: "enc-empty",
		change:      change,
		// matched and priorOnlyUsers both nil
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 0 {
		t.Fatalf("expected 0 RenderJobs (nobody to notify), got %d", len(jobs))
	}
}

// TestInitialPokemonRender_SetsReplyKey pins the invariant that the
// initial-sighting RenderJob enqueued by ProcessPokemon carries
// ReplyKey = pokemon.EncounterID. Without that, (encounterID, target)
// never enters MessageTracker.replies and the next change-event has
// no prior message to thread under. ProcessPokemon needs a full
// ProcessorService to invoke directly, so the assertion grep-checks
// pokemon.go for the literal field assignment.
func TestInitialPokemonRender_SetsReplyKey(t *testing.T) {
	src, err := os.ReadFile("pokemon.go")
	if err != nil {
		t.Fatalf("read pokemon.go: %v", err)
	}
	// gofmt may insert alignment whitespace between `ReplyKey:` and the
	// value, so collapse runs of whitespace before substring-matching.
	normalized := strings.Join(strings.Fields(string(src)), " ")
	if !strings.Contains(normalized, "ReplyKey: pokemon.EncounterID") {
		t.Fatalf("pokemon.go must set ReplyKey: pokemon.EncounterID on the initial-sighting RenderJob")
	}
}

// stubWeatherProvider is a no-op WeatherProvider for tests that need the
// regular pokemon enrichment path to run without a real weather tracker.
type stubWeatherProvider struct{}

func (stubWeatherProvider) GetCurrentWeatherInCell(_ string) int { return 0 }
func (stubWeatherProvider) GetWeatherForecast(_ string) tracker.WeatherForecast {
	return tracker.WeatherForecast{}
}

// TestDispatchPokemonAlert_PerLanguageOriginalFromBytes covers the
// rich-OriginalView path for the monsterChanged branch: when the
// EncounterChange carries the prior webhook bytes, the dispatcher
// re-runs base enrichment to populate {{original.X}} with the same
// field set as a regular monster render (not just the hand-picked
// subset). Verified on a prior-only recipient since that's the
// only path that emits a monsterChanged job under the unified rule.
func TestDispatchPokemonAlert_PerLanguageOriginalFromBytes(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)
	ps.enricher.WeatherProvider = stubWeatherProvider{}

	encounterID := "enc-prior-bytes"
	priorOnly := webhook.MatchedUser{ID: "user-A", Type: "discord:user", Language: "en"}

	atk, def, sta := 10, 11, 12
	prior := &webhook.PokemonWebhook{
		PokemonID:         25,
		Form:              0,
		CP:                900,
		IndividualAttack:  &atk,
		IndividualDefense: &def,
		IndividualStamina: &sta,
		Weather:           1,
		Latitude:          52.5,
		Longitude:         13.4,
		DisappearTime:     9999999999,
	}
	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeForm,
		Old:         tracker.EncounterState{PokemonID: 25, Form: 0, CP: 900, ATK: 10, DEF: 11, STA: 12, Weather: 1},
		New:         tracker.EncounterState{PokemonID: 25, Form: 65, CP: 950, ATK: 10, DEF: 11, STA: 12, Weather: 1},
		OldWebhook:  prior,
	}

	ps.dispatchPokemonAlert(pokemonDispatchInput{
		encounterID:    encounterID,
		change:         change,
		priorOnlyUsers: []webhook.MatchedUser{priorOnly},
		isEncountered:  true,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 RenderJob (monsterChanged), got %d", len(jobs))
	}
	j := jobs[0]
	if !j.IsChange {
		t.Errorf("prior-only path must set IsChange=true (monsterChanged), got false")
	}
	if j.OriginalView == nil {
		t.Fatalf("OriginalView should be populated from prior webhook bytes, got nil")
	}
	if _, ok := j.OriginalView["seenType"]; !ok {
		t.Errorf("OriginalView from bytes should expose seenType (regular enrichment), got: %v", j.OriginalView)
	}
	if got := j.OriginalView["pokemon_id"]; got != 25 {
		t.Errorf("OriginalView should reflect prior pokemon_id=25, got %v", got)
	}
}

// TestDispatchPokemonAlert_FallbackWhenNoBytes confirms the
// pre-bytes-storage fallback path still works: a change with no
// OldWebhook (older tracker entry or test that didn't supply one)
// still produces a usable OriginalView via dts.BuildOriginalView's
// hand-picked subset.
func TestDispatchPokemonAlert_FallbackWhenNoBytes(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)

	encounterID := "enc-no-bytes"
	priorOnly := webhook.MatchedUser{ID: "user-B", Type: "discord:user"}

	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeForm,
		Old:         tracker.EncounterState{PokemonID: 25, Form: 0, CP: 900},
		New:         tracker.EncounterState{PokemonID: 25, Form: 65, CP: 950},
		// OldWebhook intentionally nil
	}

	ps.dispatchPokemonAlert(pokemonDispatchInput{
		encounterID:    encounterID,
		change:         change,
		priorOnlyUsers: []webhook.MatchedUser{priorOnly},
		isEncountered:  true,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 RenderJob, got %d", len(jobs))
	}
	j := jobs[0]
	if j.OriginalView == nil {
		t.Fatalf("OriginalView should fall back to BuildOriginalView, got nil")
	}
	// BuildOriginalView exposes pokemonId (camelCase), not pokemon_id —
	// asserting that distinguishes the fallback path.
	if got := j.OriginalView["pokemonId"]; got != 25 {
		t.Errorf("BuildOriginalView fallback should expose pokemonId=25, got %v", got)
	}
}

// TestTileGate_ConcurrentReadAfterCloseNoRace exercises the chan-close
// happens-before that makes the shared-Enrichment-map pattern safe under
// `go test -race`. A single goroutine writes "staticMap" into the shared
// map and sets gate.bytes, then closes ready. Multiple readers wait on
// ready and then read the map and the bytes. If the gate's chan-close
// barrier is ever removed, -race will fail this test.
func TestTileGate_ConcurrentReadAfterCloseNoRace(t *testing.T) {
	enrichmentMap := map[string]any{"name": "Pikachu"}
	gate := &tileGate{ready: make(chan struct{})}

	go func() {
		// Small delay simulates the tile wait. The map write must be
		// sequenced-before close(gate.ready) for the readers' post-receive
		// reads to be race-free.
		time.Sleep(5 * time.Millisecond)
		enrichmentMap["staticMap"] = "test-url"
		gate.bytes = []byte{1, 2, 3}
		close(gate.ready)
	}()

	const readers = 4
	var wg sync.WaitGroup
	urls := make(chan string, readers)
	imgs := make(chan []byte, readers)
	for range readers {
		wg.Go(func() {
			<-gate.ready
			urls <- enrichmentMap["staticMap"].(string)
			imgs <- gate.bytes
		})
	}
	wg.Wait()
	close(urls)
	close(imgs)

	for u := range urls {
		if u != "test-url" {
			t.Errorf("reader saw %q, want test-url", u)
		}
	}
	for b := range imgs {
		if !bytes.Equal(b, []byte{1, 2, 3}) {
			t.Errorf("reader saw bytes %v, want [1 2 3]", b)
		}
	}
}

// TestDispatchPokemonAlert_TileGateOnEveryJob is a structural guard:
// when the dispatch input carries a TilePending and emits multiple
// RenderJobs (matched + priorOnly batch), every emitted job must
// reference the same tileGate. The gate ensures one goroutine writes
// the shared Enrichment map's staticMap field before any render
// worker reads it; if jobs in a batch ever reference different
// gates, sibling render workers would race on that map.
func TestDispatchPokemonAlert_TileGateOnEveryJob(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)

	encounterID := "enc-gate-wiring"
	matchedUser := webhook.MatchedUser{ID: "user-A", Type: "discord:user"}
	priorOnlyUser := webhook.MatchedUser{ID: "user-B", Type: "discord:user"}

	// Real TilePending in default (URL) mode with no target — Apply is a
	// no-op so we don't need a full enrichment/staticmap wiring. We send a
	// URL on Result so the gate goroutine returns promptly.
	pending := &staticmap.TilePending{
		Result:   make(chan string, 1),
		Deadline: time.Now().Add(2 * time.Second),
		Fallback: "fallback",
	}
	pending.Result <- "resolved"

	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeForm,
		Old:         tracker.EncounterState{PokemonID: 25, Form: 0, CP: 900},
		New:         tracker.EncounterState{PokemonID: 25, Form: 65, CP: 950},
	}

	ps.dispatchPokemonAlert(pokemonDispatchInput{
		encounterID:    encounterID,
		change:         change,
		matched:        []webhook.MatchedUser{matchedUser},
		priorOnlyUsers: []webhook.MatchedUser{priorOnlyUser},
		enrichment:     map[string]any{"name": "Pikachu"},
		tilePending:    pending,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 RenderJobs (matched + monsterChanged), got %d", len(jobs))
	}

	var sharedGate *tileGate
	for i, j := range jobs {
		if j.TileGate == nil {
			t.Errorf("job[%d] has TileGate=nil; every job in a TilePending-bearing dispatch must reference the gate", i)
			continue
		}
		if sharedGate == nil {
			sharedGate = j.TileGate
		} else if j.TileGate != sharedGate {
			t.Errorf("job[%d] references a different *tileGate than its sibling; all jobs in a dispatch must share the same gate so they observe one writer's map mutations", i)
		}
	}

	// Wait for the gate goroutine to complete so the test doesn't leak it.
	if sharedGate != nil {
		select {
		case <-sharedGate.ready:
		case <-time.After(2 * time.Second):
			t.Fatal("gate goroutine did not close ready within 2s")
		}
	}
}

// TestProcessPokemon_RespectsChangeTrackingFlag is a source-level guard:
// the pokemon webhook handler must consult ps.cfg.Tracking.PokemonChangeTracking
// before routing into the per-user dispatch path. When the flag is false,
// the legacy single-RenderJob path runs instead. Source-grep on purpose —
// ProcessPokemon needs a fully-constructed ProcessorService (matcher,
// enricher, state) which is impractical to assemble here.
func TestProcessPokemon_RespectsChangeTrackingFlag(t *testing.T) {
	src, err := os.ReadFile("pokemon.go")
	if err != nil {
		t.Fatalf("read pokemon.go: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "ps.cfg.Tracking.PokemonChangeTracking") {
		t.Fatalf("pokemon.go must reference ps.cfg.Tracking.PokemonChangeTracking. Without this, the [tracking] pokemon_change_tracking flag has no effect.")
	}
}
