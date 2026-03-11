package tracker

import (
	"fmt"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// DuplicateCache provides deduplication for webhooks.
type DuplicateCache struct {
	cache *ttlcache.Cache[string, bool]
}

// NewDuplicateCache creates a new duplicate detection cache.
func NewDuplicateCache() *DuplicateCache {
	cache := ttlcache.New[string, bool](
		ttlcache.WithTTL[string, bool](90*time.Minute),
		ttlcache.WithDisableTouchOnHit[string, bool](),
	)
	go cache.Start()
	return &DuplicateCache{cache: cache}
}

// Close stops the cache eviction goroutine.
func (dc *DuplicateCache) Close() {
	dc.cache.Stop()
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
func (dc *DuplicateCache) CheckRaid(gymID string, end int64, pokemonID int, rsvps []RaidRSVP) (bool, bool) {
	key := fmt.Sprintf("%s:%d:%d", gymID, end, pokemonID)

	existing := dc.cache.Get(key)
	if existing == nil {
		// First time seeing this raid
		dc.cache.Set(key, true, 90*time.Minute)
		return false, true
	}

	// For now, allow re-notification (RSVP change detection is handled by alerter)
	// TODO: implement full RSVP comparison here
	return true, false
}
