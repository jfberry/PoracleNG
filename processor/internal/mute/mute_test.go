package mute

import (
	"sync"
	"testing"
	"time"
)

func TestAddRemoveMatch(t *testing.T) {
	s := NewStore()
	now := time.Now().Unix()

	// Empty store matches nothing.
	if s.Match("u1", Event{GymID: "gym1"}, now) {
		t.Errorf("empty store: Match returned true")
	}

	// Add a gym mute, confirm Match.
	s.Add(Entry{HumanID: "u1", ScopeType: ScopeGym, ScopeValue: "gym1", ExpiresAt: now + 60})
	if !s.Match("u1", Event{GymID: "gym1"}, now) {
		t.Errorf("after gym mute add: Match returned false")
	}
	// Different gym doesn't match.
	if s.Match("u1", Event{GymID: "gym2"}, now) {
		t.Errorf("different gym: Match returned true")
	}
	// Different user doesn't see u1's mute.
	if s.Match("u2", Event{GymID: "gym1"}, now) {
		t.Errorf("different user: Match returned true")
	}

	// Remove it.
	if !s.Remove("u1", ScopeGym, "gym1") {
		t.Errorf("Remove: got false, want true")
	}
	if s.Match("u1", Event{GymID: "gym1"}, now) {
		t.Errorf("after Remove: still matches")
	}
}

func TestAddReplacesDuplicate(t *testing.T) {
	s := NewStore()
	now := time.Now().Unix()

	added := s.Add(Entry{HumanID: "u1", ScopeType: ScopeGym, ScopeValue: "gym1", ExpiresAt: now + 60})
	if added {
		t.Errorf("first Add: replaced=%v, want false", added)
	}
	replaced := s.Add(Entry{HumanID: "u1", ScopeType: ScopeGym, ScopeValue: "gym1", ExpiresAt: now + 3600})
	if !replaced {
		t.Errorf("second Add: replaced=%v, want true", replaced)
	}
	list := s.List("u1")
	if len(list) != 1 {
		t.Errorf("expected 1 entry after replace, got %d", len(list))
	}
	if list[0].ExpiresAt != now+3600 {
		t.Errorf("expected new ExpiresAt after replace, got %d", list[0].ExpiresAt)
	}
}

func TestScopeMatching(t *testing.T) {
	s := NewStore()
	now := time.Now().Unix()

	// Set up one of each scope.
	s.Add(Entry{HumanID: "u1", ScopeType: ScopeGym, ScopeValue: "g1", ExpiresAt: now + 60})
	s.Add(Entry{HumanID: "u1", ScopeType: ScopePokemon, ScopeValue: "25", ExpiresAt: now + 60})
	s.Add(Entry{HumanID: "u1", ScopeType: ScopeArea, ScopeValue: "Downtown", ExpiresAt: now + 60})
	s.Add(Entry{HumanID: "u1", ScopeType: ScopePokestop, ScopeValue: "ps1", ExpiresAt: now + 60})
	s.Add(Entry{HumanID: "u1", ScopeType: ScopeStation, ScopeValue: "st1", ExpiresAt: now + 60})
	s.Add(Entry{HumanID: "u2", ScopeType: ScopeEverything, ExpiresAt: now + 60})

	cases := []struct {
		name  string
		user  string
		ev    Event
		want  bool
	}{
		{"gym match", "u1", Event{GymID: "g1"}, true},
		{"gym no-match", "u1", Event{GymID: "other"}, false},
		{"pokemon match by ID", "u1", Event{PokemonID: 25}, true},
		{"pokemon no-match", "u1", Event{PokemonID: 24}, false},
		{"area match exact", "u1", Event{Area: []string{"Downtown"}}, true},
		{"area match case-insensitive", "u1", Event{Area: []string{"downtown"}}, true},
		{"area no-match", "u1", Event{Area: []string{"Suburbia"}}, false},
		{"area match one of many", "u1", Event{Area: []string{"Suburbia", "Downtown"}}, true},
		{"pokestop match", "u1", Event{PokestopID: "ps1"}, true},
		{"station match", "u1", Event{StationID: "st1"}, true},
		{"everything always matches", "u2", Event{GymID: "anything"}, true},
		{"everything matches empty event", "u2", Event{}, true},
		{"u1 doesn't see u2's everything", "u1", Event{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.Match(tc.user, tc.ev, now); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExpiredEntriesDontMatch(t *testing.T) {
	s := NewStore()
	now := time.Now().Unix()
	s.Add(Entry{HumanID: "u1", ScopeType: ScopeGym, ScopeValue: "g1", ExpiresAt: now - 1})
	if s.Match("u1", Event{GymID: "g1"}, now) {
		t.Errorf("expired entry: Match returned true")
	}
}

func TestSweepRemovesExpired(t *testing.T) {
	s := NewStore()
	now := time.Now().Unix()

	s.Add(Entry{HumanID: "u1", ScopeType: ScopeGym, ScopeValue: "g1", ExpiresAt: now + 60})    // alive
	s.Add(Entry{HumanID: "u1", ScopeType: ScopeGym, ScopeValue: "g2", ExpiresAt: now - 60})    // expired
	s.Add(Entry{HumanID: "u2", ScopeType: ScopePokemon, ScopeValue: "1", ExpiresAt: now - 60}) // expired (only entry for u2)

	removed := s.Sweep(now)
	if removed != 2 {
		t.Errorf("Sweep removed %d, want 2", removed)
	}
	if s.Count() != 1 {
		t.Errorf("Count after sweep: %d, want 1", s.Count())
	}
	if !s.Match("u1", Event{GymID: "g1"}, now) {
		t.Errorf("alive entry should still match after sweep")
	}
	if len(s.List("u2")) != 0 {
		t.Errorf("u2 should have no entries after sweep")
	}
}

func TestRemoveAll(t *testing.T) {
	s := NewStore()
	now := time.Now().Unix()
	s.Add(Entry{HumanID: "u1", ScopeType: ScopeGym, ScopeValue: "g1", ExpiresAt: now + 60})
	s.Add(Entry{HumanID: "u1", ScopeType: ScopePokemon, ScopeValue: "25", ExpiresAt: now + 60})
	s.Add(Entry{HumanID: "u1", ScopeType: ScopeArea, ScopeValue: "X", ExpiresAt: now + 60})

	n := s.RemoveAll("u1")
	if n != 3 {
		t.Errorf("RemoveAll: got %d, want 3", n)
	}
	if len(s.List("u1")) != 0 {
		t.Errorf("expected empty list after RemoveAll")
	}

	// Idempotent — removing again returns 0.
	if n := s.RemoveAll("u1"); n != 0 {
		t.Errorf("RemoveAll second call: got %d, want 0", n)
	}
}

func TestConcurrentReadsWrites(t *testing.T) {
	s := NewStore()
	now := time.Now().Unix()
	s.Add(Entry{HumanID: "u1", ScopeType: ScopeGym, ScopeValue: "g1", ExpiresAt: now + 60})

	const readers = 8
	const writers = 4
	const iters = 200

	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				_ = s.Match("u1", Event{GymID: "g1"}, time.Now().Unix())
			}
		}()
	}
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				s.Add(Entry{
					HumanID:    "u1",
					ScopeType:  ScopeGym,
					ScopeValue: "g1",
					ExpiresAt:  time.Now().Unix() + int64(60+id*10+j),
				})
			}
		}(i)
	}
	wg.Wait()
	// Sanity: still matches at the end.
	if !s.Match("u1", Event{GymID: "g1"}, time.Now().Unix()) {
		t.Errorf("after concurrent ops: Match returned false")
	}
}

func TestTrackingScopeMatchesRuleUID(t *testing.T) {
	s := NewStore()
	now := time.Now().Unix()
	s.Add(Entry{HumanID: "u1", ScopeType: ScopeTracking, ScopeValue: "45", ExpiresAt: now + 60})

	// Match when MatchedRuleUID equals the muted value.
	if !s.Match("u1", Event{MatchedRuleUID: 45}, now) {
		t.Errorf("ScopeTracking with MatchedRuleUID=45 should match mute on '45'")
	}
	// No match for a different UID.
	if s.Match("u1", Event{MatchedRuleUID: 46}, now) {
		t.Errorf("ScopeTracking with MatchedRuleUID=46 should not match mute on '45'")
	}
	// No match when MatchedRuleUID is unset (avoid accidental "0" trip).
	if s.Match("u1", Event{}, now) {
		t.Errorf("ScopeTracking with no MatchedRuleUID should not match")
	}
}

func TestRemainingAt(t *testing.T) {
	now := time.Now().Unix()
	e := Entry{ExpiresAt: now + 600}
	got := e.RemainingAt(now)
	if got < 599*time.Second || got > 600*time.Second {
		t.Errorf("RemainingAt: got %v, want ~600s", got)
	}
	// Expired entry returns negative.
	e = Entry{ExpiresAt: now - 60}
	if got := e.RemainingAt(now); got >= 0 {
		t.Errorf("RemainingAt for expired entry: got %v, want negative", got)
	}
	// Zero ExpiresAt means no expiry — RemainingAt returns 0.
	e = Entry{ExpiresAt: 0}
	if got := e.RemainingAt(now); got != 0 {
		t.Errorf("RemainingAt for ExpiresAt=0: got %v, want 0", got)
	}
}
