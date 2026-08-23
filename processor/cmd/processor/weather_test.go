package main

import (
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// weatherAlertCleanUntil must prefer max(ActivePokemons.DisappearTime)
// so weather alerts auto-delete with the pokemon they mention, not
// with the user's cell-wide care window (which is the max across every
// pokemon ever registered and never shrinks). Falls back to CaresUntil
// when no active-pokemon data is available (show_altered_pokemon off).
func TestWeatherAlertCleanUntil(t *testing.T) {
	cases := []struct {
		name string
		user webhook.MatchedUser
		want int64
	}{
		{
			name: "no active pokemon → falls back to CaresUntil",
			user: webhook.MatchedUser{CaresUntil: 1700000000},
			want: 1700000000,
		},
		{
			name: "single active pokemon → uses its DisappearTime",
			user: webhook.MatchedUser{
				CaresUntil:     2000000000, // longer-lived pokemon registered earlier, already despawned
				ActivePokemons: []webhook.ActivePokemonEntry{{DisappearTime: 1700000000}},
			},
			want: 1700000000,
		},
		{
			name: "multiple active pokemon → max(DisappearTime)",
			user: webhook.MatchedUser{
				CaresUntil: 3000000000, // stale, ignored when ActivePokemons present
				ActivePokemons: []webhook.ActivePokemonEntry{
					{DisappearTime: 1700000000},
					{DisappearTime: 1700001800}, // 30min later
					{DisappearTime: 1700000900},
				},
			},
			want: 1700001800,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := weatherAlertCleanUntil(c.user)
			if got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

// TestDispatchWeatherChange_SharedTileUsesSingleGate is the regression test for
// the weather-change shared-tile bug: when the base weather tile is shared
// across users (show_altered_pokemon_static_map off), every user's RenderJob
// must resolve the tile through ONE shared tileGate. Wrapping the shared
// *staticmap.TilePending in a gate per user meant its size-1 Result channel
// delivered the URL to exactly one goroutine, so one message got the real tile
// and the rest fell back to the fallback (pokemon) image. Clean users must also
// share the base enrichment map (carrying their per-user TTH via
// OverrideCleanTTH), not a pre-resolution copy that loses the tile.
func TestDispatchWeatherChange_SharedTileUsesSingleGate(t *testing.T) {
	ps, _, _ := minimalProcessor(t)
	ch := make(chan RenderJob, 16)
	ps.renderCh = ch

	// A shared base weather tile: one non-nil pending with a past deadline so
	// the gate goroutine resolves immediately (Apply is a no-op — no target).
	baseTilePending := &staticmap.TilePending{
		Result:    make(chan string, 1),
		ResultImg: make(chan []byte, 1),
		Deadline:  time.Now(),
	}
	baseEnrichment := map[string]any{"weatherName": "Sunny"}

	now := time.Now().Unix()
	matched := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Language: "en"},
		{ID: "u2", Type: "discord:user", Language: "en", Clean: 1, CaresUntil: now + 3600},
		{ID: "u3", Type: "discord:user", Language: "en"},
		{ID: "u4", Type: "discord:user", Language: "en"},
	}

	ps.dispatchWeatherChange(weatherChangeDispatchInput{
		s2CellID:        "cell-1",
		oldCondition:    5,
		newCondition:    3,
		baseEnrichment:  baseEnrichment,
		baseTilePending: baseTilePending,
		matched:         matched,
		now:             now,
		minAlert:        0,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != len(matched) {
		t.Fatalf("expected %d render jobs, got %d", len(matched), len(jobs))
	}

	// THE FIX: one shared gate across all shared-tile jobs.
	gates := map[*tileGate]int{}
	for _, j := range jobs {
		if j.TileGate == nil {
			t.Fatalf("shared-tile job for %s has a nil TileGate", j.MatchedUsers[0].ID)
		}
		gates[j.TileGate]++
	}
	if len(gates) != 1 {
		t.Errorf("shared base tile must use ONE gate across all %d users; got %d distinct gates (per-user gates on a shared pending is the bug)", len(jobs), len(gates))
	}

	// Every user (clean included) shares the base enrichment map — proven by a
	// post-dispatch mutation being visible in every job — and clean users carry
	// their per-user clean-deletion TTH via OverrideCleanTTH.
	baseEnrichment["sharedMarker"] = "yes"
	for _, j := range jobs {
		uid := j.MatchedUsers[0].ID
		if j.Enrichment["sharedMarker"] != "yes" {
			t.Errorf("job for %s does not share baseEnrichment (got a copy) — clean users must share so they still get the resolved tile", uid)
		}
		if uid == "u2" {
			if j.OverrideCleanTTH != now+3600 {
				t.Errorf("clean user u2 OverrideCleanTTH = %d, want %d", j.OverrideCleanTTH, now+3600)
			}
		} else if j.OverrideCleanTTH != 0 {
			t.Errorf("non-clean user %s OverrideCleanTTH = %d, want 0", uid, j.OverrideCleanTTH)
		}
	}
}
