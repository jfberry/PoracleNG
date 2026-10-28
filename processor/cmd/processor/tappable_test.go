package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

func tappableFixture(t *testing.T) *ProcessorService {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ps := &ProcessorService{
		cfg:             &config.Config{General: config.GeneralConfig{Locale: "en"}},
		stateMgr:        state.NewManager(),
		ctx:             ctx,
		workerPool:      make(chan struct{}, 4),
		duplicates:      tracker.NewDuplicateCache(),
		enricher:        &enrichment.Enricher{},
		tappableMatcher: &matching.TappableMatcher{},
	}
	t.Cleanup(ps.duplicates.Close)
	return ps
}

func tappableRaw(t *testing.T, encounterID string, disappear int64) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"encounter_id":   encounterID,
		"spawnpoint_id":  "None",
		"pokestop_id":    "None",
		"latitude":       48.1,
		"longitude":      16.4,
		"type":           "TAPPABLE_TYPE_BOX",
		"rewards":        []map[string]any{{"pokemon_candy": 41, "count": 6}},
		"disappear_time": disappear,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The handler drops a tappable whose known disappear_time is already past
// before it reaches dedup; a live one (or an unknown expiry, 0) is processed
// and recorded in the duplicate cache.
func TestProcessTappable_DropsExpired(t *testing.T) {
	ps := tappableFixture(t)
	now := time.Now().Unix()
	sig := matching.TappableRewardSignature([]matching.TappableRewardData{{Key: "pokemon_candy", PokemonID: 41, Amount: 6}})

	for _, tc := range []struct {
		id         string
		disappear  int64
		wantMarked bool
	}{
		{"expired", now - 5, false},
		{"live", now + 600, true},
		{"unknown", 0, true},
	} {
		if err := ps.ProcessTappable(tappableRaw(t, tc.id, tc.disappear)); err != nil {
			t.Fatalf("%s: %v", tc.id, err)
		}
		ps.wg.Wait()
		// CheckTappable reports true when the handler already recorded it.
		if got := ps.duplicates.CheckTappable(tc.id, sig, tc.disappear); got != tc.wantMarked {
			t.Errorf("%s: recorded in dedup = %v, want %v", tc.id, got, tc.wantMarked)
		}
	}
}

func TestProcessTappable_DisableFlag(t *testing.T) {
	ps := tappableFixture(t)
	ps.cfg.General.DisableTappable = true
	sig := matching.TappableRewardSignature([]matching.TappableRewardData{{Key: "pokemon_candy", PokemonID: 41, Amount: 6}})
	if err := ps.ProcessTappable(tappableRaw(t, "off", 0)); err != nil {
		t.Fatal(err)
	}
	ps.wg.Wait()
	if ps.duplicates.CheckTappable("off", sig, 0) {
		t.Error("disable_tappable must drop the webhook before dedup")
	}
}
