package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
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

// trackPriorMessage seeds the MessageTracker with a tracked message indexed
// under (replyKey, target) so partitionByPriorMessage / LookupReply find it.
// The tracker only populates the reply index when the TrackedMessage carries a
// non-empty ReplyKey, so we set it here.
func trackPriorMessage(t *testing.T, d *delivery.Dispatcher, replyKey, target, sentID string) {
	t.Helper()
	d.MessageTracker().Track(
		"prior:"+replyKey+":"+target,
		&delivery.TrackedMessage{
			SentID:   sentID,
			Target:   target,
			Type:     "discord:user",
			ReplyKey: replyKey,
		},
		5*time.Minute,
	)
}

// TestPartitionByPriorMessage covers the pure helper.
func TestPartitionByPriorMessage(t *testing.T) {
	d := newTestDispatcher(t)
	tr := d.MessageTracker()

	encounterID := "enc-1"
	matched := []webhook.MatchedUser{
		{ID: "user-A", Type: "discord:user"},
		{ID: "user-B", Type: "discord:user"},
		{ID: "user-C", Type: "discord:user"},
	}

	// Seed prior messages for A and C only.
	trackPriorMessage(t, d, encounterID, "user-A", "msg-A")
	trackPriorMessage(t, d, encounterID, "user-C", "msg-C")

	withPrior, withoutPrior := partitionByPriorMessage(matched, encounterID, tr)

	if len(withPrior) != 2 {
		t.Fatalf("withPrior length: got %d, want 2 (%v)", len(withPrior), withPrior)
	}
	got := map[string]bool{withPrior[0].ID: true, withPrior[1].ID: true}
	if !got["user-A"] || !got["user-C"] {
		t.Errorf("withPrior should contain user-A and user-C, got %v", withPrior)
	}
	if len(withoutPrior) != 1 || withoutPrior[0].ID != "user-B" {
		t.Errorf("withoutPrior should contain only user-B, got %v", withoutPrior)
	}
}

// TestPartitionByPriorMessage_NilTracker — when no tracker is provided (e.g.
// dispatcher not initialised), every user is treated as "no prior" so the
// fresh-message path covers them all.
func TestPartitionByPriorMessage_NilTracker(t *testing.T) {
	matched := []webhook.MatchedUser{
		{ID: "user-A", Type: "discord:user"},
		{ID: "user-B", Type: "discord:user"},
	}
	withPrior, withoutPrior := partitionByPriorMessage(matched, "enc-x", nil)
	if len(withPrior) != 0 {
		t.Errorf("withPrior should be empty when tracker is nil, got %v", withPrior)
	}
	if len(withoutPrior) != 2 {
		t.Errorf("withoutPrior should contain all matched users, got %v", withoutPrior)
	}
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

// TestDispatchPokemonChangeRender_PriorEncountered covers Case A:
// prior tracked + encounter event (CP 0 → >0) → one RenderJob with regular
// `monster` template (IsChange=false), ReplyKey set, OriginalView nil.
func TestDispatchPokemonChangeRender_PriorEncountered(t *testing.T) {
	ps, ch, d := minimalProcessor(t)

	encounterID := "enc-encountered"
	user := webhook.MatchedUser{ID: "user-A", Type: "discord:user"}
	trackPriorMessage(t, d, encounterID, user.ID, "msg-A")

	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeEncountered,
		Old:         tracker.EncounterState{PokemonID: 25, CP: 0},
		New:         tracker.EncounterState{PokemonID: 25, CP: 800, ATK: 10, DEF: 10, STA: 10},
	}

	ps.dispatchPokemonChangeRender(pokemonChangeRenderInput{
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
		t.Errorf("encounter event should NOT use IsChange (monsterChanged template), got IsChange=true")
	}
	if !j.IsPokemon {
		t.Errorf("RenderJob.IsPokemon should be true, got false")
	}
	if !j.IsEncountered {
		t.Errorf("RenderJob.IsEncountered should be true, got false")
	}
	if j.ReplyKey != encounterID {
		t.Errorf("ReplyKey: got %q, want %q", j.ReplyKey, encounterID)
	}
	if j.OriginalView != nil {
		t.Errorf("OriginalView should be nil for encounter event (CP 0→>0), got %v", j.OriginalView)
	}
	if j.ChangeType != "encountered" {
		t.Errorf("ChangeType: got %q, want \"encountered\"", j.ChangeType)
	}
	if len(j.MatchedUsers) != 1 || j.MatchedUsers[0].ID != "user-A" {
		t.Errorf("MatchedUsers should be [user-A], got %v", j.MatchedUsers)
	}
}

// TestDispatchPokemonChangeRender_PriorFormChange covers Case B:
// prior tracked + post-encounter form change → one RenderJob with
// IsChange=true (monsterChanged template), OriginalView populated.
func TestDispatchPokemonChangeRender_PriorFormChange(t *testing.T) {
	ps, ch, d := minimalProcessor(t)

	encounterID := "enc-form-change"
	user := webhook.MatchedUser{ID: "user-B", Type: "discord:user"}
	trackPriorMessage(t, d, encounterID, user.ID, "msg-B")

	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeForm,
		Old:         tracker.EncounterState{PokemonID: 25, Form: 0, CP: 800, ATK: 10, DEF: 10, STA: 10},
		New:         tracker.EncounterState{PokemonID: 25, Form: 65, CP: 850, ATK: 10, DEF: 10, STA: 10},
	}

	ps.dispatchPokemonChangeRender(pokemonChangeRenderInput{
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
	if !j.IsChange {
		t.Errorf("post-encounter change should set IsChange=true, got false")
	}
	if j.ReplyKey != encounterID {
		t.Errorf("ReplyKey: got %q, want %q", j.ReplyKey, encounterID)
	}
	if j.OriginalView == nil {
		t.Fatalf("OriginalView should be populated for post-encounter change, got nil")
	}
	// The original view should reflect the prior state's identity (form 0,
	// not 65).
	if got := j.OriginalView["formId"]; got != 0 {
		t.Errorf("OriginalView.formId: got %v, want 0", got)
	}
	if got := j.OriginalView["pokemonId"]; got != 25 {
		t.Errorf("OriginalView.pokemonId: got %v, want 25", got)
	}
	if j.ChangeType != "form" {
		t.Errorf("ChangeType: got %q, want \"form\"", j.ChangeType)
	}
}

// TestDispatchPokemonChangeRender_NoPriorChange covers Cases C/D:
// no prior + change → one RenderJob with IsChange=false (regular monster),
// no OriginalView, no ChangeType. The user still gets ReplyKey set so a
// future change can chain off this send.
func TestDispatchPokemonChangeRender_NoPriorChange(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)

	encounterID := "enc-no-prior"
	user := webhook.MatchedUser{ID: "user-C", Type: "discord:user"}

	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeForm,
		Old:         tracker.EncounterState{PokemonID: 25, Form: 0, CP: 800, ATK: 10, DEF: 10, STA: 10},
		New:         tracker.EncounterState{PokemonID: 25, Form: 65, CP: 850, ATK: 10, DEF: 10, STA: 10},
	}

	ps.dispatchPokemonChangeRender(pokemonChangeRenderInput{
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
		t.Errorf("no-prior user should NOT receive monsterChanged (IsChange=true), got IsChange=true")
	}
	if j.ReplyKey != encounterID {
		t.Errorf("ReplyKey: got %q, want %q (must be set so future changes can chain)", j.ReplyKey, encounterID)
	}
	if j.OriginalView != nil {
		t.Errorf("OriginalView should NOT be set when sending fresh, got %v", j.OriginalView)
	}
	if j.ChangeType != "" {
		t.Errorf("ChangeType should be empty for fresh-send branch, got %q", j.ChangeType)
	}
}

// TestDispatchPokemonChangeRender_MixedPriorAndFresh covers a mixed batch:
// some users tracked (form change → reply with monsterChanged) and others not
// (no prior → fresh monster). Should emit two RenderJobs partitioned
// accordingly, both carrying ReplyKey=encounterID for chain continuity.
func TestDispatchPokemonChangeRender_MixedPriorAndFresh(t *testing.T) {
	ps, ch, d := minimalProcessor(t)

	encounterID := "enc-mixed"
	priorUser := webhook.MatchedUser{ID: "user-prior", Type: "discord:user"}
	freshUser := webhook.MatchedUser{ID: "user-fresh", Type: "discord:user"}
	trackPriorMessage(t, d, encounterID, priorUser.ID, "msg-prior")

	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeForm,
		Old:         tracker.EncounterState{PokemonID: 25, Form: 0, CP: 800, ATK: 10, DEF: 10, STA: 10},
		New:         tracker.EncounterState{PokemonID: 25, Form: 65, CP: 850, ATK: 10, DEF: 10, STA: 10},
	}

	ps.dispatchPokemonChangeRender(pokemonChangeRenderInput{
		encounterID:   encounterID,
		change:        change,
		matched:       []webhook.MatchedUser{priorUser, freshUser},
		isEncountered: true,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 RenderJobs (one per partition), got %d", len(jobs))
	}

	var changeJob, freshJob *RenderJob
	for i := range jobs {
		if jobs[i].IsChange {
			changeJob = &jobs[i]
		} else {
			freshJob = &jobs[i]
		}
	}
	if changeJob == nil {
		t.Fatalf("expected one job with IsChange=true (monsterChanged), got none")
	}
	if freshJob == nil {
		t.Fatalf("expected one job with IsChange=false (fresh monster), got none")
	}
	if len(changeJob.MatchedUsers) != 1 || changeJob.MatchedUsers[0].ID != priorUser.ID {
		t.Errorf("change job should target only the prior user, got %v", changeJob.MatchedUsers)
	}
	if len(freshJob.MatchedUsers) != 1 || freshJob.MatchedUsers[0].ID != freshUser.ID {
		t.Errorf("fresh job should target only the new user, got %v", freshJob.MatchedUsers)
	}
	if changeJob.OriginalView == nil {
		t.Errorf("change job should have OriginalView populated")
	}
	if freshJob.OriginalView != nil {
		t.Errorf("fresh job should NOT have OriginalView populated, got %v", freshJob.OriginalView)
	}
}

// TestInitialPokemonRender_SetsReplyKey pins PR 5.2's guarantee: the
// initial-sighting RenderJob enqueued by ProcessPokemon carries
// ReplyKey = pokemon.EncounterID. Without that, the (encounterID, target)
// pair never makes it into MessageTracker.replyIndex, and a subsequent
// change-event handler has no prior message to look up.
//
// ProcessPokemon is not directly callable here (it requires a full
// ProcessorService — matcher, enricher, state). Instead the test reads
// pokemon.go and verifies the regular RenderJob constructor sets
// `ReplyKey: pokemon.EncounterID`. This is brittle on purpose — if anyone
// removes that line the test breaks, which is the whole point.
func TestInitialPokemonRender_SetsReplyKey(t *testing.T) {
	src, err := os.ReadFile("pokemon.go")
	if err != nil {
		t.Fatalf("read pokemon.go: %v", err)
	}
	if !strings.Contains(string(src), "ReplyKey: pokemon.EncounterID") {
		t.Fatalf("pokemon.go must set ReplyKey: pokemon.EncounterID on the initial-sighting RenderJob (PR 5.2). Without this, change-event handlers cannot find prior messages.")
	}
}
