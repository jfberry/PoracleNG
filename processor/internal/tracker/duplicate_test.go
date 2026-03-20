package tracker

import (
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

	// Different pokemon - not duplicate
	isDup, isFirst = dc.CheckRaid("gym1", end, 151, nil)
	if isDup {
		t.Error("Expected different pokemon to not be duplicate")
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

	// Same RSVPs — duplicate
	isDup, isFirst = dc.CheckRaid("gym2", end, 100, rsvps1)
	if !isDup {
		t.Error("Expected same RSVPs to be duplicate")
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

	// Changed maybe_count — not duplicate
	rsvps3 := []RaidRSVP{{Timeslot: ts, GoingCount: 5, MaybeCount: 3}}
	isDup, isFirst = dc.CheckRaid("gym2", end, 100, rsvps3)
	if isDup {
		t.Error("Expected changed maybe_count to not be duplicate")
	}

	// New timeslot added — not duplicate
	rsvps4 := []RaidRSVP{
		{Timeslot: ts, GoingCount: 5, MaybeCount: 3},
		{Timeslot: ts + 3600000, GoingCount: 1, MaybeCount: 0},
	}
	isDup, isFirst = dc.CheckRaid("gym2", end, 100, rsvps4)
	if isDup {
		t.Error("Expected new timeslot to not be duplicate")
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

func TestGymBattleCooldown(t *testing.T) {
	dc := NewDuplicateCache()
	defer dc.Close()

	// No cooldown initially
	if dc.GymInBattleCooldown("gym1", false) {
		t.Error("Expected no cooldown when not in battle and no prior entry")
	}

	// Start battle — sets cooldown, returns true
	if !dc.GymInBattleCooldown("gym1", true) {
		t.Error("Expected cooldown to be active after battle starts")
	}

	// Check again without battle — still in cooldown
	if !dc.GymInBattleCooldown("gym1", false) {
		t.Error("Expected cooldown to persist")
	}

	// Different gym — no cooldown
	if dc.GymInBattleCooldown("gym2", false) {
		t.Error("Expected different gym to have no cooldown")
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
