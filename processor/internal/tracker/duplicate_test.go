package tracker

import (
	"runtime"
	"strconv"
	"testing"
	"time"
)

func TestDuplicateCachePokemon(t *testing.T) {
	dc := NewDuplicateCache()
	defer dc.Close()

	disappear := time.Now().Unix() + 600

	// First time - not duplicate
	isDup := dc.CheckPokemon("enc1", true, 500, disappear)
	if isDup {
		t.Error("Expected first sighting to not be duplicate")
	}

	// Same key - duplicate
	isDup = dc.CheckPokemon("enc1", true, 500, disappear)
	if !isDup {
		t.Error("Expected second sighting to be duplicate")
	}

	// Different verified state - not duplicate
	isDup = dc.CheckPokemon("enc1", false, 500, disappear)
	if isDup {
		t.Error("Expected different verified state to not be duplicate")
	}

	// Different CP - not duplicate
	isDup = dc.CheckPokemon("enc1", true, 600, disappear)
	if isDup {
		t.Error("Expected different CP to not be duplicate")
	}
}

func TestDuplicateCacheShowcase(t *testing.T) {
	dc := NewDuplicateCache()
	defer dc.Close()

	expiry := time.Now().Unix() + 3600

	// First fire (empty leaderboard) — not a duplicate.
	if dc.CheckShowcase("stop1", expiry, "0:none") {
		t.Error("first showcase fire should not be a duplicate")
	}
	// Same rank-1 state — duplicate.
	if !dc.CheckShowcase("stop1", expiry, "0:none") {
		t.Error("identical showcase state should be a duplicate")
	}
	// Rank-1 changed (new fingerprint) — NOT a duplicate, so the edit path
	// sees the leaderboard update.
	if dc.CheckShowcase("stop1", expiry, "3:679@1032.5") {
		t.Error("changed rank-1 fingerprint should not be a duplicate")
	}
	// Different contest (new expiry) at the same stop — not a duplicate.
	if dc.CheckShowcase("stop1", expiry+7200, "0:none") {
		t.Error("new contest (different expiry) should not be a duplicate")
	}
}

func TestDuplicateCacheRaid(t *testing.T) {
	dc := NewDuplicateCache()
	defer dc.Close()

	end := time.Now().Unix() + 3600

	// First time
	isDup, isFirst := dc.CheckRaid("gym1", end, 150, nil)
	if isDup {
		t.Error("Expected first raid to not be duplicate")
	}
	if !isFirst {
		t.Error("Expected first notification to be true")
	}

	// Same key - duplicate
	isDup, isFirst = dc.CheckRaid("gym1", end, 150, nil)
	if !isDup {
		t.Error("Expected second raid to be duplicate")
	}
	if isFirst {
		t.Error("Expected first notification to be false")
	}

	// Different pokemon - not duplicate, first sighting (new cache key)
	isDup, isFirst = dc.CheckRaid("gym1", end, 151, nil)
	if isDup {
		t.Error("Expected different pokemon to not be duplicate")
	}
	if !isFirst {
		t.Error("Expected different pokemon to be first notification (new key)")
	}
}

func TestRaidRSVPChangeDetection(t *testing.T) {
	dc := NewDuplicateCache()
	defer dc.Close()

	end := time.Now().Unix() + 3600
	ts := int64(1700000000000)

	rsvps1 := []RaidRSVP{{Timeslot: ts, GoingCount: 3, MaybeCount: 1}}

	// First time with RSVPs
	isDup, isFirst := dc.CheckRaid("gym2", end, 100, rsvps1)
	if isDup || !isFirst {
		t.Error("Expected first raid to not be duplicate and be first notification")
	}

	// Same RSVPs — duplicate, not first
	isDup, isFirst = dc.CheckRaid("gym2", end, 100, rsvps1)
	if !isDup {
		t.Error("Expected same RSVPs to be duplicate")
	}
	if isFirst {
		t.Error("Expected duplicate to not be first notification")
	}

	// Changed going_count — not duplicate, not first
	rsvps2 := []RaidRSVP{{Timeslot: ts, GoingCount: 5, MaybeCount: 1}}
	isDup, isFirst = dc.CheckRaid("gym2", end, 100, rsvps2)
	if isDup {
		t.Error("Expected changed going_count to not be duplicate")
	}
	if isFirst {
		t.Error("Expected changed RSVP to not be first notification")
	}

	// Changed maybe_count — not duplicate, not first (re-notification with changes)
	rsvps3 := []RaidRSVP{{Timeslot: ts, GoingCount: 5, MaybeCount: 3}}
	isDup, isFirst = dc.CheckRaid("gym2", end, 100, rsvps3)
	if isDup {
		t.Error("Expected changed maybe_count to not be duplicate")
	}
	if isFirst {
		t.Error("Expected changed maybe_count to not be first notification")
	}

	// New timeslot added — not duplicate, not first (re-notification with changes)
	rsvps4 := []RaidRSVP{
		{Timeslot: ts, GoingCount: 5, MaybeCount: 3},
		{Timeslot: ts + 3600000, GoingCount: 1, MaybeCount: 0},
	}
	isDup, isFirst = dc.CheckRaid("gym2", end, 100, rsvps4)
	if isDup {
		t.Error("Expected new timeslot to not be duplicate")
	}
	if isFirst {
		t.Error("Expected new timeslot to not be first notification")
	}

	// Same as last — duplicate again
	isDup, _ = dc.CheckRaid("gym2", end, 100, rsvps4)
	if !isDup {
		t.Error("Expected identical RSVPs to be duplicate")
	}
}

func TestRaidRSVPNilToSome(t *testing.T) {
	dc := NewDuplicateCache()
	defer dc.Close()

	end := time.Now().Unix() + 3600

	// First with no RSVPs
	isDup, _ := dc.CheckRaid("gym3", end, 100, nil)
	if isDup {
		t.Error("Expected first to not be duplicate")
	}

	// Same with no RSVPs — duplicate
	isDup, _ = dc.CheckRaid("gym3", end, 100, nil)
	if !isDup {
		t.Error("Expected nil→nil to be duplicate")
	}

	// Now RSVPs appear — not duplicate
	rsvps := []RaidRSVP{{Timeslot: 1700000000000, GoingCount: 1, MaybeCount: 0}}
	isDup, isFirst := dc.CheckRaid("gym3", end, 100, rsvps)
	if isDup {
		t.Error("Expected nil→some to not be duplicate")
	}
	if isFirst {
		t.Error("Expected nil→some to not be first notification")
	}
}

func TestRsvpChanged(t *testing.T) {
	tests := []struct {
		name    string
		old     []RaidRSVP
		new     []RaidRSVP
		changed bool
	}{
		{"nil to nil", nil, nil, false},
		{"nil to some", nil, []RaidRSVP{{Timeslot: 1, GoingCount: 1}}, true},
		{"some to nil", []RaidRSVP{{Timeslot: 1, GoingCount: 1}}, nil, false},
		{"same", []RaidRSVP{{Timeslot: 1, GoingCount: 1}}, []RaidRSVP{{Timeslot: 1, GoingCount: 1}}, false},
		{"going changed", []RaidRSVP{{Timeslot: 1, GoingCount: 1}}, []RaidRSVP{{Timeslot: 1, GoingCount: 2}}, true},
		{"maybe changed", []RaidRSVP{{Timeslot: 1, MaybeCount: 1}}, []RaidRSVP{{Timeslot: 1, MaybeCount: 2}}, true},
		{"new timeslot", []RaidRSVP{{Timeslot: 1}}, []RaidRSVP{{Timeslot: 1}, {Timeslot: 2}}, true},
		{"unknown timeslot", []RaidRSVP{{Timeslot: 1}}, []RaidRSVP{{Timeslot: 2}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rsvpChanged(tt.old, tt.new)
			if got != tt.changed {
				t.Errorf("rsvpChanged(%v, %v) = %v, want %v", tt.old, tt.new, got, tt.changed)
			}
		})
	}
}

// TestDuplicateCacheMemoryPerEntry pins the per-entry cost of the dedup set.
//
// The ttlcache this replaced spent ~233 B to remember a single bool: an Item
// struct, a retained key string, a container/list node and an expiry-heap
// entry. At scanner throughput the live set runs to millions of keys, which
// measured 1.42 GB in a production heap profile.
func TestDuplicateCacheMemoryPerEntry(t *testing.T) {
	dc := NewDuplicateCache()
	defer dc.Close()

	const entries = 500_000
	// Far enough out that nothing expires mid-test.
	disappear := time.Now().Unix() + 3600

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := range entries {
		dc.CheckPokemon(encounterIDForTest(i), true, 1500, disappear)
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if growth < 0 {
		growth = 0
	}
	perEntry := float64(growth) / entries

	// ttlcache measured ~233 B/entry; a hash set measures ~25 B. 60 B is well
	// clear of both, so this catches a regression back to boxed entries
	// without being brittle about map growth slack.
	const limitPerEntry = 60
	t.Logf("dedup cache: %.1f B/entry over %d entries (%.1f MB)", perEntry, entries, float64(growth)/(1<<20))
	if perEntry > limitPerEntry {
		t.Errorf("dedup cache used %.1f B/entry over %d entries (%.1f MB total), want <= %d B/entry",
			perEntry, entries, float64(growth)/(1<<20), limitPerEntry)
	}
}

// encounterIDForTest builds a golbat-shaped 19-digit encounter id.
func encounterIDForTest(i int) string {
	const base = 1_000_000_000_000_000_000
	return fmtInt(base + i)
}

func fmtInt(v int) string {
	return strconv.Itoa(v)
}
