// Package mute is the in-memory per-user alert-suppression store. A mute
// dropped here causes the matcher to filter the user out of the matched
// list for any alert whose properties match the muted scope.
//
// In-memory, lost on restart by design (#109): mutes are short-lived
// (typically minutes to hours), restarts are infrequent, and persistence
// would add DB schema + matcher join overhead with no real upside. Users
// re-apply via command or button if they want a mute to outlive a restart.
//
// See docs/buttons-and-snapshots/ and GitHub #109 for the full design.
package mute

import (
	"strings"
	"sync"
	"time"
)

// Scope values. Constants rather than free-form strings so the matcher,
// command parser, and tests stay in sync. The values are user-visible —
// they appear in command syntax (`!mute gym ...`) and `!tracked` output —
// so we treat them as part of the public API.
const (
	ScopeGym        = "gym"
	ScopePokemon    = "pokemon"
	ScopeArea       = "area"
	ScopePokestop   = "pokestop"
	ScopeStation    = "station"
	ScopeEverything = "everything"
	// ScopeTracking — reserved for tracking-UID mutes. The store accepts
	// entries with this scope (see Add); enforcement in the matcher is a
	// Phase 2.5 follow-up that requires MatchedUser to carry the rule UID.
	// Until then, command parsers should not produce ScopeTracking entries.
	ScopeTracking = "tracking"
)

// Entry is a single active mute. Held by Store keyed under HumanID; the
// matcher reads via Match.
type Entry struct {
	HumanID    string
	ScopeType  string // one of the Scope* constants
	ScopeValue string // gym id, pokemon dex id (as string), area name, etc. Empty for ScopeEverything.
	ExpiresAt  int64  // unix seconds; entries past this are skipped by Match and removed by Sweep
}

// Event is the property bag the matcher passes to Match. Each webhook
// handler builds this from the in-flight event before filtering matched
// users. Unset fields (zero values) are simply ignored — a fort_update
// event has no PokemonID, so a `pokemon` mute can't match it.
//
// The handler doesn't need to know which mute scopes exist; Match's logic
// is the single point of truth for what a scope means.
type Event struct {
	GymID      string
	PokemonID  int
	Area       []string // every matched area name
	PokestopID string
	StationID  string

	// MatchedRuleUID is the database UID of the tracking rule that
	// produced this match (per-MatchedUser). ScopeTracking mutes
	// compare against this — when a user has muted UID N, alerts whose
	// MatchedRuleUID is N get dropped.
	MatchedRuleUID int64
}

// Store is the in-memory mute repository. Safe for concurrent use; the
// underlying map is guarded by sync.RWMutex with reads on the hot path
// (matchers) and writes on the cold path (commands + sweep).
type Store struct {
	mu      sync.RWMutex
	entries map[string][]Entry // humanID → entries
}

// NewStore constructs an empty in-memory store.
func NewStore() *Store {
	return &Store{entries: make(map[string][]Entry)}
}

// Add inserts a mute entry. Existing entries with the same
// (HumanID, ScopeType, ScopeValue) are replaced — re-muting the same gym
// just extends the expiry rather than accumulating duplicates.
//
// Returns true when the call replaced an existing entry, false when it
// added a fresh one. Used by the command layer to decide between
// "muted X" and "re-muted X" confirmation messages.
func (s *Store) Add(e Entry) (replaced bool) {
	if e.HumanID == "" || e.ScopeType == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.entries[e.HumanID]
	for i, existing := range list {
		if existing.ScopeType == e.ScopeType && existing.ScopeValue == e.ScopeValue {
			list[i] = e
			s.entries[e.HumanID] = list
			return true
		}
	}
	s.entries[e.HumanID] = append(list, e)
	return false
}

// Remove deletes the entry matching (HumanID, ScopeType, ScopeValue).
// Returns true when an entry was removed, false when no match existed.
func (s *Store) Remove(humanID, scopeType, scopeValue string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.entries[humanID]
	for i, e := range list {
		if e.ScopeType == scopeType && e.ScopeValue == scopeValue {
			s.entries[humanID] = append(list[:i], list[i+1:]...)
			if len(s.entries[humanID]) == 0 {
				delete(s.entries, humanID)
			}
			return true
		}
	}
	return false
}

// RemoveAll drops every mute for a given user. Returns the count removed.
// `!unmute all` and `!unmute everything` both route here.
func (s *Store) RemoveAll(humanID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.entries[humanID])
	delete(s.entries, humanID)
	return n
}

// List returns the current entries for a user. The slice is a snapshot —
// safe to iterate without holding any lock. Order is stable but not
// otherwise defined.
func (s *Store) List(humanID string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.entries[humanID]
	if len(src) == 0 {
		return nil
	}
	out := make([]Entry, len(src))
	copy(out, src)
	return out
}

// Match returns true when humanID has at least one active (non-expired)
// mute entry that fires for the given event. Called from filterMuted on
// every matched user per webhook event — keep it cheap. Read-locked only.
//
// Matching semantics per scope:
//   - ScopeEverything matches every event.
//   - ScopeGym matches when ev.GymID equals the entry's ScopeValue.
//   - ScopePokemon matches when ev.PokemonID's decimal representation
//     equals the entry's ScopeValue.
//   - ScopeArea matches when any name in ev.Area equals the entry's
//     ScopeValue (case-insensitive — area names in tracking commands are
//     resolved case-insensitively so mute commands follow suit).
//   - ScopePokestop matches when ev.PokestopID equals the entry's ScopeValue.
//   - ScopeStation matches when ev.StationID equals the entry's ScopeValue.
//   - ScopeTracking is currently a no-op (see the const comment above).
func (s *Store) Match(humanID string, ev Event, now int64) bool {
	s.mu.RLock()
	list := s.entries[humanID]
	if len(list) == 0 {
		s.mu.RUnlock()
		return false
	}
	// Copy under lock so the rest of the function doesn't hold the read
	// lock while doing scope comparisons — keeps writers from being
	// starved on heavy-mute users. For small slice sizes this is cheap.
	entries := make([]Entry, len(list))
	copy(entries, list)
	s.mu.RUnlock()

	for _, e := range entries {
		if e.ExpiresAt > 0 && e.ExpiresAt <= now {
			continue // expired; sweep will reap it
		}
		if matchScope(e, ev) {
			return true
		}
	}
	return false
}

// matchScope is the per-entry scope check used by Match. Extracted for
// clarity and testability — callers should use Match for the locked path.
func matchScope(e Entry, ev Event) bool {
	switch e.ScopeType {
	case ScopeEverything:
		return true
	case ScopeGym:
		return e.ScopeValue != "" && e.ScopeValue == ev.GymID
	case ScopePokemon:
		return e.ScopeValue != "" && ev.PokemonID > 0 && e.ScopeValue == itoa(ev.PokemonID)
	case ScopeArea:
		if e.ScopeValue == "" {
			return false
		}
		for _, name := range ev.Area {
			if strings.EqualFold(name, e.ScopeValue) {
				return true
			}
		}
		return false
	case ScopePokestop:
		return e.ScopeValue != "" && e.ScopeValue == ev.PokestopID
	case ScopeStation:
		return e.ScopeValue != "" && e.ScopeValue == ev.StationID
	case ScopeTracking:
		// Match when the muted rule UID equals the rule that produced
		// this match. Skip when either side is empty so a zero-UID
		// synthetic match doesn't accidentally trip a "0" mute entry.
		if e.ScopeValue == "" || ev.MatchedRuleUID == 0 {
			return false
		}
		return e.ScopeValue == itoa(int(ev.MatchedRuleUID))
	default:
		return false
	}
}

// Sweep removes all entries that expired at or before `now`. Returns the
// number removed. Runs from the background sweeper.
func (s *Store) Sweep(now int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for humanID, list := range s.entries {
		kept := list[:0]
		for _, e := range list {
			if e.ExpiresAt > 0 && e.ExpiresAt <= now {
				removed++
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == 0 {
			delete(s.entries, humanID)
		} else {
			s.entries[humanID] = kept
		}
	}
	return removed
}

// Count returns the total number of active entries across all users —
// the metric gauge reads this on each sweep tick.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, list := range s.entries {
		n += len(list)
	}
	return n
}

// itoa is a small allocation-free wrapper used in matchScope to compare
// pokemon IDs without bringing in strconv (saves a 16-byte alloc per
// match call in the hot path).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// SweepDuration returns the lifetime of an entry. Used by `!tracked`
// rendering to format "23m left" suffixes. Negative values indicate the
// entry has expired (sweep should reap it shortly).
func (e Entry) RemainingAt(now int64) time.Duration {
	if e.ExpiresAt == 0 {
		return 0
	}
	return time.Duration(e.ExpiresAt-now) * time.Second
}
