package buttonactions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/buttons"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
)

func TestRegistryDispatch(t *testing.T) {
	r := NewRegistry()
	called := false
	r.Register("test", func(_ context.Context, _ *snapshots.Snapshot, _ buttons.Def, _ string, _ Deps) (Response, error) {
		called = true
		return Response{Text: "ok"}, nil
	})

	def := buttons.Def{Action: "test", ID: "x", Label: "x"}
	resp, err := r.Dispatch(context.Background(), &snapshots.Snapshot{}, def, "u1", Deps{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !called {
		t.Errorf("handler not invoked")
	}
	if resp.Text != "ok" {
		t.Errorf("Text: got %q", resp.Text)
	}
}

func TestDispatchUnknownAction(t *testing.T) {
	r := NewRegistry()
	_, err := r.Dispatch(context.Background(), nil, buttons.Def{Action: "missing"}, "u1", Deps{})
	if !errors.Is(err, ErrUnknownAction) {
		t.Errorf("got %v, want ErrUnknownAction", err)
	}
}

func TestDispatchNoAction(t *testing.T) {
	r := NewRegistry()
	_, err := r.Dispatch(context.Background(), nil, buttons.Def{}, "u1", Deps{})
	if !errors.Is(err, ErrNoAction) {
		t.Errorf("got %v, want ErrNoAction", err)
	}
}

func TestRegisterPanicsOnNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on nil handler")
		}
	}()
	r := NewRegistry()
	r.Register("x", nil)
}

func TestHandleMute_Gym(t *testing.T) {
	store := mute.NewStore()
	snap := &snapshots.Snapshot{
		Target:     "u1",
		Enrichment: map[string]any{"gym_id": "gym-abc.16"},
	}
	def := buttons.Def{
		ID:     "mute_gym",
		Label:  "Mute gym",
		Action: buttons.ActionMute,
		Scope:  buttons.ScopeGym,
		Params: map[string]any{"duration_min": float64(30)}, // JSON unmarshal produces float64
	}

	resp, err := HandleMute(context.Background(), snap, def, "u1", Deps{MuteStore: store})
	if err != nil {
		t.Fatalf("HandleMute: %v", err)
	}
	if resp.Reaction != "🔇" {
		t.Errorf("Reaction: got %q", resp.Reaction)
	}
	if !store.Match("u1", mute.Event{GymID: "gym-abc.16"}, time.Now().Unix()) {
		t.Errorf("expected mute store to contain gym-abc.16")
	}
}

func TestHandleMute_Pokemon(t *testing.T) {
	store := mute.NewStore()
	snap := &snapshots.Snapshot{
		Target:     "u1",
		Enrichment: map[string]any{"pokemon_id": float64(25)},
	}
	def := buttons.Def{
		ID:     "mute_pokemon",
		Label:  "Mute pokemon",
		Action: buttons.ActionMute,
		Scope:  buttons.ScopePokemon,
	}

	resp, err := HandleMute(context.Background(), snap, def, "u1", Deps{MuteStore: store})
	if err != nil {
		t.Fatalf("HandleMute: %v", err)
	}
	if resp.Reaction != "🔇" {
		t.Errorf("Reaction: got %q", resp.Reaction)
	}
	if !store.Match("u1", mute.Event{PokemonID: 25}, time.Now().Unix()) {
		t.Errorf("expected mute store to contain pokemon 25")
	}
}

func TestHandleMute_Area(t *testing.T) {
	store := mute.NewStore()
	snap := &snapshots.Snapshot{
		Target:       "u1",
		MatchedAreas: []string{"Downtown"},
	}
	def := buttons.Def{
		ID:     "mute_area",
		Label:  "Mute area",
		Action: buttons.ActionMute,
		Scope:  buttons.ScopeArea,
	}
	if _, err := HandleMute(context.Background(), snap, def, "u1", Deps{MuteStore: store}); err != nil {
		t.Fatalf("HandleMute: %v", err)
	}
	if !store.Match("u1", mute.Event{Area: []string{"Downtown"}}, time.Now().Unix()) {
		t.Errorf("expected mute store to contain Downtown area")
	}
}

func TestHandleMute_Everything(t *testing.T) {
	store := mute.NewStore()
	snap := &snapshots.Snapshot{Target: "u1"}
	def := buttons.Def{
		ID:     "self_mute",
		Label:  "Quiet for an hour",
		Action: buttons.ActionMute,
		Scope:  buttons.ScopeEverything,
		Params: map[string]any{"duration_min": float64(60)},
	}
	if _, err := HandleMute(context.Background(), snap, def, "u1", Deps{MuteStore: store}); err != nil {
		t.Fatalf("HandleMute: %v", err)
	}
	// Everything mutes match any event.
	if !store.Match("u1", mute.Event{GymID: "anything"}, time.Now().Unix()) {
		t.Errorf("expected ScopeEverything to match arbitrary events")
	}
}

func TestHandleMute_MissingScopeValue(t *testing.T) {
	store := mute.NewStore()
	snap := &snapshots.Snapshot{Target: "u1", Enrichment: map[string]any{}}
	def := buttons.Def{Action: buttons.ActionMute, Scope: buttons.ScopeGym, ID: "x", Label: "y"}
	resp, err := HandleMute(context.Background(), snap, def, "u1", Deps{MuteStore: store})
	if err != nil {
		t.Fatalf("HandleMute: %v", err)
	}
	if resp.Reaction != "🙅" {
		t.Errorf("Reaction: got %q, want 🙅", resp.Reaction)
	}
}

func TestHandleMute_NilStore(t *testing.T) {
	def := buttons.Def{Action: buttons.ActionMute, Scope: buttons.ScopeGym, ID: "x", Label: "y"}
	_, err := HandleMute(context.Background(), &snapshots.Snapshot{}, def, "u1", Deps{})
	if err == nil {
		t.Errorf("expected error on nil MuteStore")
	}
}

func TestHandleMute_NilSnap(t *testing.T) {
	store := mute.NewStore()
	def := buttons.Def{Action: buttons.ActionMute, Scope: buttons.ScopeGym, ID: "x", Label: "y"}
	resp, err := HandleMute(context.Background(), nil, def, "u1", Deps{MuteStore: store})
	if err != nil {
		t.Fatalf("HandleMute: %v", err)
	}
	if resp.Reaction != "🙅" {
		t.Errorf("Reaction: got %q, want 🙅", resp.Reaction)
	}
}

func TestRegisterBuiltins(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	for _, name := range []string{
		buttons.ActionMute,
		buttons.ActionUnsubscribe,
		buttons.ActionRedeliver,
		buttons.ActionRender,
	} {
		if h := r.Lookup(name); h == nil {
			t.Errorf("RegisterBuiltins: missing handler for %q", name)
		}
	}
}

func TestDurationFromParams(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   time.Duration
	}{
		{"nil → default", nil, muteDurationDefault},
		{"missing → default", map[string]any{"unused": 5}, muteDurationDefault},
		{"int", map[string]any{"duration_min": 90}, 90 * time.Minute},
		{"float64 (json)", map[string]any{"duration_min": float64(45)}, 45 * time.Minute},
		{"string", map[string]any{"duration_min": "30"}, 30 * time.Minute},
		{"zero → default", map[string]any{"duration_min": 0}, muteDurationDefault},
		{"negative → default", map[string]any{"duration_min": -5}, muteDurationDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := muteDurationFromParams(tc.params); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
