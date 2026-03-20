package tracker

import (
	"fmt"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// DuplicateCache provides deduplication for webhooks.
type DuplicateCache struct {
	cache     *ttlcache.Cache[string, bool]
	raidCache *ttlcache.Cache[string, *RaidCacheResult]
	gymBattle *ttlcache.Cache[string, bool] // battle cooldown (5 min TTL)
}

// NewDuplicateCache creates a new duplicate detection cache.
func NewDuplicateCache() *DuplicateCache {
	cache := ttlcache.New[string, bool](
		ttlcache.WithTTL[string, bool](90*time.Minute),
		ttlcache.WithDisableTouchOnHit[string, bool](),
	)
	raidCache := ttlcache.New[string, *RaidCacheResult](
		ttlcache.WithTTL[string, *RaidCacheResult](90*time.Minute),
		ttlcache.WithDisableTouchOnHit[string, *RaidCacheResult](),
	)
	gymBattle := ttlcache.New[string, bool](
		ttlcache.WithTTL[string, bool](5*time.Minute),
		ttlcache.WithDisableTouchOnHit[string, bool](),
	)
	go cache.Start()
	go raidCache.Start()
	go gymBattle.Start()
	return &DuplicateCache{cache: cache, raidCache: raidCache, gymBattle: gymBattle}
}

// Close stops all cache eviction goroutines.
func (dc *DuplicateCache) Close() {
	dc.cache.Stop()
	dc.raidCache.Stop()
	dc.gymBattle.Stop()
}

// CheckPokemon returns true if this pokemon was already seen (duplicate).
// Key: {encounter_id}:{verified}:{cp}
func (dc *DuplicateCache) CheckPokemon(encounterID string, verified bool, cp int, disappearTime int64) bool {
	verifiedStr := "F"
	if verified {
		verifiedStr = "T"
	}
	key := fmt.Sprintf("%s%s%d", encounterID, verifiedStr, cp)

	if dc.cache.Get(key) != nil {
		return true // duplicate
	}

	// Set with TTL based on disappear time
	now := time.Now().Unix()
	var ttl time.Duration
	if !verified || disappearTime == 0 {
		ttl = 60 * time.Minute
	} else {
		remaining := disappearTime - now + 300
		if remaining <= 0 {
			remaining = 60
		}
		ttl = time.Duration(remaining) * time.Second
	}

	dc.cache.Set(key, true, ttl)
	return false
}

// RaidCacheResult holds info about a previously-seen raid.
type RaidCacheResult struct {
	RSVPs []RaidRSVP
}

// RaidRSVP mirrors the RSVP struct for comparison.
type RaidRSVP struct {
	Timeslot   int64
	GoingCount int
	MaybeCount int
}

// CheckRaid returns (isDuplicate, isFirstNotification) for a raid webhook.
// Key: {gym_id}:{end}:{pokemon_id}
// On first sight: stores RSVPs and returns (false, true).
// On re-notification: compares RSVPs. If changed, updates cache and returns (false, false).
// If unchanged: returns (true, false) — true duplicate.
func (dc *DuplicateCache) CheckRaid(gymID string, end int64, pokemonID int, rsvps []RaidRSVP) (bool, bool) {
	key := fmt.Sprintf("%s:%d:%d", gymID, end, pokemonID)

	existing := dc.raidCache.Get(key)
	if existing == nil {
		// First time seeing this raid
		dc.raidCache.Set(key, &RaidCacheResult{RSVPs: rsvps}, 90*time.Minute)
		return false, true
	}

	prev := existing.Value()

	if rsvpChanged(prev.RSVPs, rsvps) {
		// RSVP data changed — update cache, allow re-notification
		dc.raidCache.Set(key, &RaidCacheResult{RSVPs: rsvps}, 90*time.Minute)
		return false, false
	}

	// No RSVP change — true duplicate
	return true, false
}

// rsvpChanged compares old and new RSVP slices. Returns true if there's any
// difference in timeslot count, going_count, or maybe_count.
// Mirrors the original JS logic: only compares timeslots present in the new data.
func rsvpChanged(oldRSVPs, newRSVPs []RaidRSVP) bool {
	if len(newRSVPs) > len(oldRSVPs) {
		return true
	}

	for _, nr := range newRSVPs {
		found := false
		for _, or := range oldRSVPs {
			if nr.Timeslot == or.Timeslot {
				found = true
				if nr.GoingCount != or.GoingCount || nr.MaybeCount != or.MaybeCount {
					return true
				}
				break
			}
		}
		if !found {
			// New timeslot not in old data
			return true
		}
	}
	return false
}

// CheckInvasion returns true if this invasion was already seen (duplicate).
// Key: {pokestop_id}I{incident_expiration}
func (dc *DuplicateCache) CheckInvasion(pokestopID string, expiration int64) bool {
	key := fmt.Sprintf("%sI%d", pokestopID, expiration)

	if dc.cache.Get(key) != nil {
		return true
	}

	now := time.Now().Unix()
	remaining := expiration - now + 300
	if remaining <= 0 {
		remaining = 60
	}
	dc.cache.Set(key, true, time.Duration(remaining)*time.Second)
	return false
}

// CheckQuest returns true if this quest was already seen (duplicate).
// Key: {pokestop_id}_{rewards_hash}
func (dc *DuplicateCache) CheckQuest(pokestopID string, rewardsKey string) bool {
	key := fmt.Sprintf("%s_%s", pokestopID, rewardsKey)

	if dc.cache.Get(key) != nil {
		return true
	}

	dc.cache.Set(key, true, 90*time.Minute)
	return false
}

// CheckLure returns true if this lure was already seen (duplicate).
// Key: {pokestop_id}L{lure_expiration}
func (dc *DuplicateCache) CheckLure(pokestopID string, expiration int64) bool {
	key := fmt.Sprintf("%sL%d", pokestopID, expiration)

	if dc.cache.Get(key) != nil {
		return true
	}

	now := time.Now().Unix()
	remaining := expiration - now + 300
	if remaining <= 0 {
		remaining = 60
	}
	dc.cache.Set(key, true, time.Duration(remaining)*time.Second)
	return false
}

// CheckMaxbattle returns true if this maxbattle was already seen (duplicate).
// Key: {station_id}M{battle_end}{pokemon_id}
func (dc *DuplicateCache) CheckMaxbattle(stationID string, battleEnd int64, pokemonID int) bool {
	key := fmt.Sprintf("%sM%d%d", stationID, battleEnd, pokemonID)

	if dc.cache.Get(key) != nil {
		return true
	}

	now := time.Now().Unix()
	remaining := battleEnd - now + 300
	if remaining <= 0 {
		remaining = 60
	}
	dc.cache.Set(key, true, time.Duration(remaining)*time.Second)
	return false
}

// CheckNest returns true if this nest was already seen (duplicate).
// Key: {nest_id}_{pokemon_id}_{reset_time}
func (dc *DuplicateCache) CheckNest(nestID int64, pokemonID int, resetTime int64) bool {
	key := fmt.Sprintf("%d_%d_%d", nestID, pokemonID, resetTime)

	if dc.cache.Get(key) != nil {
		return true
	}

	// 14 days from reset_time
	now := time.Now().Unix()
	remaining := resetTime + 14*24*3600 - now
	if remaining <= 0 {
		remaining = 3600
	}
	dc.cache.Set(key, true, time.Duration(remaining)*time.Second)
	return false
}

// GymInBattleCooldown checks if a gym is within the 5-minute battle cooldown.
// If inBattle is true, starts/refreshes the cooldown.
// Returns true if the gym is still in the cooldown period.
func (dc *DuplicateCache) GymInBattleCooldown(gymID string, inBattle bool) bool {
	if inBattle {
		dc.gymBattle.Set(gymID, true, 5*time.Minute)
	}
	return dc.gymBattle.Get(gymID) != nil
}
