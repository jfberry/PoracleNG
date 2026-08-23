package tracker

import (
	"fmt"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// DuplicateCache provides deduplication for webhooks.
type DuplicateCache struct {
	// seen holds the plain "have we handled this?" keys, which dominate the
	// entry count at scanner throughput. It stores key fingerprints rather
	// than boxed cache items — see expiringSet.
	seen *expiringSet
	// raidCache keeps real values (prior RSVP state) rather than a bit, and
	// its cardinality is bounded by active raids, so it stays a ttlcache.
	raidCache *ttlcache.Cache[string, *RaidCacheResult]
}

// NewDuplicateCache creates a new duplicate detection cache.
func NewDuplicateCache() *DuplicateCache {
	raidCache := ttlcache.New[string, *RaidCacheResult](
		ttlcache.WithTTL[string, *RaidCacheResult](90*time.Minute),
		ttlcache.WithDisableTouchOnHit[string, *RaidCacheResult](),
	)
	go raidCache.Start()
	return &DuplicateCache{seen: newExpiringSet(), raidCache: raidCache}
}

// Close stops all cache eviction goroutines.
func (dc *DuplicateCache) Close() {
	dc.seen.Close()
	dc.raidCache.Stop()
}

// CheckPokemon returns true if this pokemon was already seen (duplicate).
// Key: {encounter_id}:{verified}:{cp}
func (dc *DuplicateCache) CheckPokemon(encounterID string, verified bool, cp int, disappearTime int64) bool {
	k := dc.seen.newKey()
	k.Str(encounterID).Bool(verified).Int(int64(cp))

	// TTL based on disappear time
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

	return dc.seen.CheckAndAdd(&k, ttl)
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
// CheckShowcase deduplicates showcase fires. Identity is the stop + contest end
// time + a rank-1 fingerprint: Golbat only fires the pokéstop webhook on rank-1
// movement, so including the fingerprint lets each meaningful leaderboard change
// through (to reach the edit path) while collapsing repeats of the same state.
func (dc *DuplicateCache) CheckShowcase(pokestopID string, showcaseExpiry int64, rank1Fingerprint string) bool {
	k := dc.seen.newKey()
	k.Str(pokestopID).Str("SC").Int(showcaseExpiry).Str(rank1Fingerprint)

	now := time.Now().Unix()
	remaining := showcaseExpiry - now + 300
	if remaining <= 0 {
		remaining = 60
	}
	return dc.seen.CheckAndAdd(&k, time.Duration(remaining)*time.Second)
}

func (dc *DuplicateCache) CheckInvasion(pokestopID string, expiration int64) bool {
	k := dc.seen.newKey()
	k.Str(pokestopID).Str("I").Int(expiration)

	now := time.Now().Unix()
	remaining := expiration - now + 300
	if remaining <= 0 {
		remaining = 60
	}
	return dc.seen.CheckAndAdd(&k, time.Duration(remaining)*time.Second)
}

// CheckQuest returns true if this quest was already seen (duplicate).
// Key: {pokestop_id}_{rewards_hash}
func (dc *DuplicateCache) CheckQuest(pokestopID string, rewardsKey string) bool {
	k := dc.seen.newKey()
	k.Str(pokestopID).Str(rewardsKey)

	return dc.seen.CheckAndAdd(&k, 90*time.Minute)
}

// CheckLure returns true if this lure was already seen (duplicate).
// Key: {pokestop_id}L{lure_expiration}
func (dc *DuplicateCache) CheckLure(pokestopID string, expiration int64) bool {
	k := dc.seen.newKey()
	k.Str(pokestopID).Str("L").Int(expiration)

	now := time.Now().Unix()
	remaining := expiration - now + 300
	if remaining <= 0 {
		remaining = 60
	}
	return dc.seen.CheckAndAdd(&k, time.Duration(remaining)*time.Second)
}

// CheckMaxbattle returns true if this maxbattle was already seen (duplicate).
// Key: {station_id}M{battle_end}{pokemon_id}
func (dc *DuplicateCache) CheckMaxbattle(stationID string, battleEnd int64, pokemonID int) bool {
	k := dc.seen.newKey()
	k.Str(stationID).Str("M").Int(battleEnd).Int(int64(pokemonID))

	now := time.Now().Unix()
	remaining := battleEnd - now + 300
	if remaining <= 0 {
		remaining = 60
	}
	return dc.seen.CheckAndAdd(&k, time.Duration(remaining)*time.Second)
}

// CheckNest returns true if this nest was already seen (duplicate).
// Key: {nest_id}_{pokemon_id}_{reset_time}
func (dc *DuplicateCache) CheckNest(nestID int64, pokemonID int, resetTime int64) bool {
	k := dc.seen.newKey()
	k.Int(nestID).Int(int64(pokemonID)).Int(resetTime)

	// 14 days from reset_time
	now := time.Now().Unix()
	remaining := resetTime + 14*24*3600 - now
	if remaining <= 0 {
		remaining = 3600
	}
	return dc.seen.CheckAndAdd(&k, time.Duration(remaining)*time.Second)
}
