package tracker

import (
	"sync"
	"time"
)

const recentActivityTTL = 6 * time.Hour

// RecentActivity tracks recently-seen IDs across several webhook-derived
// categories (raid bosses, max battle bosses, quest rewards, invasion grunts).
// Entries expire after recentActivityTTL and are pruned lazily on read.
// Intended for slash command autocomplete to prioritise currently-active entities.
type RecentActivity struct {
	mu              sync.Mutex
	raidBosses      map[int]time.Time
	maxBattleBosses map[int]time.Time
	questPokemon    map[int]time.Time
	questItems      map[int]time.Time
	questCandy      map[int]time.Time
	questMega       map[int]time.Time
	questXL         map[int]time.Time
	invasionGrunts  map[int]time.Time
	now             func() time.Time
}

// NewRecentActivity creates an empty RecentActivity tracker.
func NewRecentActivity() *RecentActivity {
	return &RecentActivity{
		raidBosses:      make(map[int]time.Time),
		maxBattleBosses: make(map[int]time.Time),
		questPokemon:    make(map[int]time.Time),
		questItems:      make(map[int]time.Time),
		questCandy:      make(map[int]time.Time),
		questMega:       make(map[int]time.Time),
		questXL:         make(map[int]time.Time),
		invasionGrunts:  make(map[int]time.Time),
		now:             time.Now,
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
	cutoff := r.now().Add(-recentActivityTTL)
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
