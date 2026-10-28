package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/geofence"
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

// tappableMatchingFixture is tappableFixture with alert_minimum_time at its
// default (120s), a render channel, and one human tracking all candy, so a
// tappable that survives the handler's checks is enqueued for render.
func tappableMatchingFixture(t *testing.T) *ProcessorService {
	t.Helper()
	ps := tappableFixture(t)
	ps.cfg.General.AlertMinimumTime = 120
	ps.cfg.Discord.Enabled = true
	ps.renderCh = make(chan RenderJob, 4)
	fences := []geofence.Fence{{
		Name: "TestArea", DisplayInMatches: true,
		Path: [][2]float64{{48.0, 16.0}, {49.0, 16.0}, {49.0, 17.0}, {48.0, 17.0}},
	}}
	ps.stateMgr.Set(&state.State{
		Humans: map[string]*db.Human{"u1": {
			ID: "u1", Type: "discord:user", Enabled: true, Area: []string{"testarea"},
			Latitude: 48.1, Longitude: 16.4, CurrentProfileNo: 1,
		}},
		Tappables: []*db.TappableTracking{{UID: 1, ID: "u1", ProfileNo: 1, RewardType: matching.TappableRewardCandy}},
		Geofence:  geofence.NewSpatialIndex(fences),
		Fences:    fences,
	})
	return ps
}

func tappableRawExpiry(t *testing.T, encounterID string, disappear int64, verified bool, pokestop string) json.RawMessage {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(tappableRaw(t, encounterID, disappear), &m); err != nil {
		t.Fatal(err)
	}
	m["disappear_time_verified"] = verified
	m["pokestop_id"] = pokestop
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A fort tappable's expiry is Golbat's tap+120s guess (unverified), so with
// the default alert_minimum_time of 120 it must still be rendered — and the
// render must skip the minimum-time check. A verified expiry under the
// minimum is dropped in the handler, before matching.
func TestProcessTappable_AlertMinimumTime(t *testing.T) {
	now := time.Now().Unix()
	for _, tc := range []struct {
		name     string
		left     int64
		verified bool
		pokestop string
		wantJob  bool
		wantSkip bool
	}{
		{"stop unverified 110s", 110, false, "stop1", true, true},
		{"spawn verified 60s", 60, true, "None", false, false},
		{"spawn verified 600s", 600, true, "None", true, false},
		{"unknown expiry", -now, true, "None", true, false}, // disappear_time 0
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := tappableMatchingFixture(t)
			if err := ps.ProcessTappable(tappableRawExpiry(t, tc.name, now+tc.left, tc.verified, tc.pokestop)); err != nil {
				t.Fatal(err)
			}
			ps.wg.Wait()
			select {
			case job := <-ps.renderCh:
				if !tc.wantJob {
					t.Fatal("job enqueued, want the handler to drop it")
				}
				if job.SkipMinAlertTime != tc.wantSkip {
					t.Errorf("SkipMinAlertTime = %v, want %v", job.SkipMinAlertTime, tc.wantSkip)
				}
			default:
				if tc.wantJob {
					t.Fatal("no job enqueued")
				}
			}
		})
	}
}
