package buttonactions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/buttons"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
	"github.com/pokemon/poracleng/processor/internal/store"
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

// A tappable has no pokemon_id: the pokemon mute covers the first reward
// with a pokemon, the same one the webhook handler's mute.Event carries —
// here the candy, after an item reward. Rewards arrive as []any after the
// snapshot's JSON round-trip.
func TestHandleMute_PokemonTappable(t *testing.T) {
	for name, rewards := range map[string]any{
		"stored": []any{
			map[string]any{"type": "item", "itemId": float64(1301), "pokemonId": float64(0)},
			map[string]any{"type": "pokemon_candy", "pokemonId": float64(41)},
		},
		"in memory": []map[string]any{
			{"type": "item", "itemId": 1301, "pokemonId": 0},
			{"type": "pokemon_candy", "pokemonId": 41},
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := mute.NewStore()
			snap := &snapshots.Snapshot{
				Target:     "u1",
				AlertType:  "tappable",
				Enrichment: map[string]any{"rewardPokemonId": float64(0), "rewards": rewards},
			}
			def := buttons.Def{ID: "mute_pokemon", Label: "Mute", Action: buttons.ActionMute, Scope: buttons.ScopePokemon}
			if _, err := HandleMute(context.Background(), snap, def, "u1", Deps{MuteStore: store}); err != nil {
				t.Fatalf("HandleMute: %v", err)
			}
			if !store.Match("u1", mute.Event{PokemonID: 41}, time.Now().Unix()) {
				t.Errorf("expected mute store to contain pokemon 41")
			}
		})
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

// recordingStore records which per-type store received DeleteByUIDs.
// Embeds the interface so only the method under test needs a body.
type recordingStore[T any] struct {
	store.TrackingStore[T]
	name string
	hits *[]string
}

func (r recordingStore[T]) DeleteByUIDs(_ string, _ []int64) error {
	*r.hits = append(*r.hits, r.name)
	return nil
}

func recordingTrackingStores(hits *[]string) *store.TrackingStores {
	return &store.TrackingStores{
		Monsters:   recordingStore[db.MonsterTrackingAPI]{name: "monsters", hits: hits},
		Raids:      recordingStore[db.RaidTrackingAPI]{name: "raids", hits: hits},
		Eggs:       recordingStore[db.EggTrackingAPI]{name: "eggs", hits: hits},
		Quests:     recordingStore[db.QuestTrackingAPI]{name: "quests", hits: hits},
		Invasions:  recordingStore[db.InvasionTrackingAPI]{name: "invasions", hits: hits},
		Lures:      recordingStore[db.LureTrackingAPI]{name: "lures", hits: hits},
		Nests:      recordingStore[db.NestTrackingAPI]{name: "nests", hits: hits},
		Gyms:       recordingStore[db.GymTrackingAPI]{name: "gyms", hits: hits},
		Forts:      recordingStore[db.FortTrackingAPI]{name: "forts", hits: hits},
		Maxbattles: recordingStore[db.MaxbattleTrackingAPI]{name: "maxbattles", hits: hits},
		Tappables:  recordingStore[db.TappableTrackingAPI]{name: "tappables", hits: hits},
	}
}

// TestUnsubscribeRoutesEveryRenderedAlertType covers every RenderJob.AlertType
// the webhook handlers in cmd/processor actually emit (and therefore every
// value a snapshot can carry). A handler using a name the switch doesn't know
// makes the unsubscribe button fail with "unknown alert type" — which is how
// "pokemon" and "fort-update" were broken.
func TestUnsubscribeRoutesEveryRenderedAlertType(t *testing.T) {
	cases := map[string]string{
		"pokemon":     "monsters",   // pokemon.go
		"raid":        "raids",      // raid.go
		"egg":         "eggs",       // raid.go
		"quest":       "quests",     // quest.go
		"invasion":    "invasions",  // invasion.go
		"incident":    "invasions",  // invasion.go, showcase.go
		"lure":        "lures",      // lure.go
		"nest":        "nests",      // nest.go
		"gym":         "gyms",       // gym.go
		"fort-update": "forts",      // fort.go
		"maxbattle":   "maxbattles", // maxbattle.go
		"tappable":    "tappables",  // tappable.go
	}
	for alertType, want := range cases {
		t.Run(alertType, func(t *testing.T) {
			var hits []string
			n, err := deleteTrackingByAlertType(alertType, "u1", []int64{7}, Deps{Tracking: recordingTrackingStores(&hits)})
			if err != nil {
				t.Fatalf("deleteTrackingByAlertType(%q): %v", alertType, err)
			}
			if n != 1 || len(hits) != 1 || hits[0] != want {
				t.Errorf("deleteTrackingByAlertType(%q): n=%d hits=%v, want 1 delete on %s", alertType, n, hits, want)
			}
		})
	}
}

func TestHandleMute_TrackingTyped(t *testing.T) {
	ms := mute.NewStore()
	snap := &snapshots.Snapshot{Target: "u1", AlertType: "raid", TrackingUIDs: []int64{60, 61}}
	def := buttons.Def{ID: "m", Label: "Mute", Action: "mute", Scope: buttons.ScopeTracking}
	resp, err := HandleMute(context.Background(), snap, def, "u1", Deps{MuteStore: ms})
	if err != nil {
		t.Fatalf("HandleMute: %v", err)
	}
	if resp.Reaction != "🔇" {
		t.Fatalf("reaction = %q (%s), want mute", resp.Reaction, resp.Text)
	}
	list := ms.List("u1")
	if len(list) != 2 {
		t.Fatalf("entries = %+v, want two", list)
	}
	for i, uid := range []string{"60", "61"} {
		if e := list[i]; e.ScopeType != mute.ScopeTracking || e.RuleTable != "raid" || e.ScopeValue != uid {
			t.Errorf("entry %d = %+v, want tracking/raid/%s", i, e, uid)
		}
	}
}

func TestAlertTypeTable(t *testing.T) {
	cases := map[string]string{
		"pokemon": "pokemon", "monster": "pokemon", "monsterChanged": "pokemon",
		"raid": "raid", "egg": "egg", "quest": "quest",
		"invasion": "invasion", "incident": "invasion",
		"pokestop": "lure", "lure": "lure", "nest": "nest", "gym": "gym",
		"fort-update": "fort", "fort_update": "fort",
		"max_battle": "maxbattle", "maxbattle": "maxbattle",
		"tappable": "tappable",
	}
	for in, want := range cases {
		if got, ok := alertTypeTable(in); !ok || got != want {
			t.Errorf("alertTypeTable(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := alertTypeTable("nope"); ok {
		t.Errorf("unknown alert type must not resolve")
	}
}
