package tracker

import (
	"sync"
	"time"
)

// RecentActivityTTL is the recency window: an id stays "active" for this long
// after it was last seen. Exported so the activity API can report the window it
// is serving rather than clients hardcoding it.
const RecentActivityTTL = 6 * time.Hour

// RecentActivity tracks recently-seen IDs across several webhook-derived
// categories (raid bosses, max battle bosses, quest rewards, invasion grunts).
// Entries expire after recentActivityTTL and are pruned lazily on read.
// Intended for slash command autocomplete to prioritise currently-active entities.
type RecentActivity struct {
	mu                    sync.Mutex
	raidBosses            map[int]time.Time
	maxBattleBosses       map[int]time.Time
	questPokemon          map[int]time.Time
	questItems            map[int]time.Time
	questCandy            map[int]time.Time
	questMega             map[int]time.Time
	questXL               map[int]time.Time
	invasionGrunts        map[int]time.Time
	costumesByPokemon     map[int]map[int]time.Time
	raidCostumesByPokemon map[int]map[int]time.Time
	raidFormsByPokemon    map[int]map[int]time.Time
	formsByPokemon        map[int]map[int]time.Time
	now                   func() time.Time
}

// NewRecentActivity creates an empty RecentActivity tracker.
func NewRecentActivity() *RecentActivity {
	return &RecentActivity{
		raidBosses:            make(map[int]time.Time),
		maxBattleBosses:       make(map[int]time.Time),
		questPokemon:          make(map[int]time.Time),
		questItems:            make(map[int]time.Time),
		questCandy:            make(map[int]time.Time),
		questMega:             make(map[int]time.Time),
		questXL:               make(map[int]time.Time),
		invasionGrunts:        make(map[int]time.Time),
		costumesByPokemon:     make(map[int]map[int]time.Time),
		raidCostumesByPokemon: make(map[int]map[int]time.Time),
		raidFormsByPokemon:    make(map[int]map[int]time.Time),
		formsByPokemon:        make(map[int]map[int]time.Time),
		now:                   time.Now,
	}
}

func (r *RecentActivity) RecordRaidBoss(id int)      { r.record(r.raidBosses, id) }
func (r *RecentActivity) RecordMaxBattleBoss(id int) { r.record(r.maxBattleBosses, id) }
func (r *RecentActivity) RecordQuestPokemon(id int)  { r.record(r.questPokemon, id) }
func (r *RecentActivity) RecordQuestItem(id int)     { r.record(r.questItems, id) }
func (r *RecentActivity) RecordQuestCandy(id int)    { r.record(r.questCandy, id) }
func (r *RecentActivity) RecordQuestMega(id int)     { r.record(r.questMega, id) }
func (r *RecentActivity) RecordQuestXL(id int)       { r.record(r.questXL, id) }
func (r *RecentActivity) RecordInvasionGrunt(id int) { r.record(r.invasionGrunts, id) }

func (r *RecentActivity) ActiveRaidBosses() []int      { return r.active(r.raidBosses) }
func (r *RecentActivity) ActiveMaxBattleBosses() []int { return r.active(r.maxBattleBosses) }
func (r *RecentActivity) ActiveQuestPokemon() []int    { return r.active(r.questPokemon) }
func (r *RecentActivity) ActiveQuestItems() []int      { return r.active(r.questItems) }
func (r *RecentActivity) ActiveQuestCandy() []int      { return r.active(r.questCandy) }
func (r *RecentActivity) ActiveQuestMega() []int       { return r.active(r.questMega) }
func (r *RecentActivity) ActiveQuestXL() []int         { return r.active(r.questXL) }
func (r *RecentActivity) ActiveInvasionGrunts() []int  { return r.active(r.invasionGrunts) }

// RecordCostume marks costume as recently seen on pokemonID.
func (r *RecentActivity) RecordCostume(pokemonID, costume int) {
	if pokemonID <= 0 || costume <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	inner := r.costumesByPokemon[pokemonID]
	if inner == nil {
		inner = make(map[int]time.Time)
		r.costumesByPokemon[pokemonID] = inner
	}
	inner[costume] = r.now()
}

// RecentCostumes returns the recency-windowed list of costume IDs recently
// seen on pokemonID.
func (r *RecentActivity) RecentCostumes(pokemonID int) []int {
	r.mu.Lock()
	inner := r.costumesByPokemon[pokemonID]
	r.mu.Unlock()
	if inner == nil {
		return nil
	}
	return r.active(inner) // reuse the existing recency window logic
}

// RecordRaidCostume marks costume as recently seen on a raid boss pokemonID.
func (r *RecentActivity) RecordRaidCostume(pokemonID, costume int) {
	if pokemonID <= 0 || costume <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	inner := r.raidCostumesByPokemon[pokemonID]
	if inner == nil {
		inner = make(map[int]time.Time)
		r.raidCostumesByPokemon[pokemonID] = inner
	}
	inner[costume] = r.now()
}

// RecentRaidCostumes returns the recency-windowed costume IDs recently seen on
// raid boss pokemonID.
func (r *RecentActivity) RecentRaidCostumes(pokemonID int) []int {
	r.mu.Lock()
	inner := r.raidCostumesByPokemon[pokemonID]
	r.mu.Unlock()
	if inner == nil {
		return nil
	}
	return r.active(inner)
}

// RecordRaidForm marks form as recently seen on a raid boss pokemonID.
func (r *RecentActivity) RecordRaidForm(pokemonID, form int) {
	if pokemonID <= 0 || form <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	inner := r.raidFormsByPokemon[pokemonID]
	if inner == nil {
		inner = make(map[int]time.Time)
		r.raidFormsByPokemon[pokemonID] = inner
	}
	inner[form] = r.now()
}

// RecentRaidForms returns the recency-windowed form IDs recently seen on raid
// boss pokemonID.
func (r *RecentActivity) RecentRaidForms(pokemonID int) []int {
	r.mu.Lock()
	inner := r.raidFormsByPokemon[pokemonID]
	r.mu.Unlock()
	if inner == nil {
		return nil
	}
	return r.active(inner)
}

// RecordForm marks form as recently seen on pokemonID. Form 0 (the "any form"
// placeholder) is ignored — it is never a trackable value.
func (r *RecentActivity) RecordForm(pokemonID, form int) {
	if pokemonID <= 0 || form <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	inner := r.formsByPokemon[pokemonID]
	if inner == nil {
		inner = make(map[int]time.Time)
		r.formsByPokemon[pokemonID] = inner
	}
	inner[form] = r.now()
}

// RecentForms returns the recency-windowed list of form IDs recently seen on
// pokemonID.
func (r *RecentActivity) RecentForms(pokemonID int) []int {
	r.mu.Lock()
	inner := r.formsByPokemon[pokemonID]
	r.mu.Unlock()
	if inner == nil {
		return nil
	}
	return r.active(inner) // reuse the existing recency window logic
}

// allByPokemon returns the recency-windowed ids for every pokemon in m,
// pruning expired entries and dropping any pokemon left with none. Takes the
// lock once for the whole walk; callers must NOT already hold r.mu (sync.Mutex
// is not reentrant, and this is why it does not simply loop over the
// Recent*(pokemonID) accessors, which lock internally via active()).
func (r *RecentActivity) allByPokemon(m map[int]map[int]time.Time) map[int][]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := r.now().Add(-RecentActivityTTL)
	out := make(map[int][]int, len(m))
	for pokemonID, inner := range m {
		ids := make([]int, 0, len(inner))
		for id, ts := range inner {
			if ts.Before(cutoff) {
				delete(inner, id)
				continue
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			// Every entry for this pokemon aged out; drop the empty inner map
			// so the outer map does not accumulate them.
			delete(m, pokemonID)
			continue
		}
		out[pokemonID] = ids
	}
	return out
}

// AllCostumes / AllForms / AllRaidCostumes / AllRaidForms are the whole-map
// counterparts of the Recent*(pokemonID) accessors, for callers that need every
// pokemon at once rather than one lookup at a time (the activity API).
func (r *RecentActivity) AllCostumes() map[int][]int { return r.allByPokemon(r.costumesByPokemon) }
func (r *RecentActivity) AllForms() map[int][]int    { return r.allByPokemon(r.formsByPokemon) }
func (r *RecentActivity) AllRaidCostumes() map[int][]int {
	return r.allByPokemon(r.raidCostumesByPokemon)
}
func (r *RecentActivity) AllRaidForms() map[int][]int { return r.allByPokemon(r.raidFormsByPokemon) }

func (r *RecentActivity) record(m map[int]time.Time, id int) {
	if id <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	m[id] = r.now()
}

func (r *RecentActivity) active(m map[int]time.Time) []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := r.now().Add(-RecentActivityTTL)
	ids := make([]int, 0, len(m))
	for id, ts := range m {
		if ts.Before(cutoff) {
			delete(m, id)
			continue
		}
		ids = append(ids, id)
	}
	return ids
}
