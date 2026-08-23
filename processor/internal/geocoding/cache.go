package geocoding

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/akrylysov/pogreb"
	"github.com/jellydator/ttlcache/v3"
	log "github.com/sirupsen/logrus"
)

// Cache is a two-layer geocoding cache: in-memory ttlcache backed by an
// on-disk pogreb database. On a miss in memory, the disk layer is checked
// and the result is promoted to memory.
type Cache struct {
	mem  *ttlcache.Cache[string, *Address]
	disk *pogreb.DB

	// memISect is a second in-memory layer for intersection strings. pogreb
	// is a single key-value store with no notion of tables, so intersections
	// share the same on-disk DB as addresses but under a distinct key
	// namespace (see IntersectionCacheKey) — effectively a second logical
	// table in one file. A separate mem layer keeps the address layer's
	// *Address typing clean.
	memISect *ttlcache.Cache[string, string]

	hitsMemory atomic.Uint64
	hitsDisk   atomic.Uint64
	misses     atomic.Uint64

	// Intersection-layer counters are kept separate so they don't pollute the
	// reverse-geocode cache-hit metrics operators tune cache_detail against.
	isectHitsMemory atomic.Uint64
	isectHitsDisk   atomic.Uint64
	isectMisses     atomic.Uint64
}

// CacheStats is a point-in-time snapshot of geocoder cache health.
type CacheStats struct {
	MemoryEntries int    // entries currently in the in-memory layer
	DiskEntries   int    // entries in the on-disk pogreb layer (address + intersection share one DB)
	HitsMemory    uint64 // total address memory-layer hits since process start
	HitsDisk      uint64 // total address disk-layer hits since process start
	Misses        uint64 // total address misses since process start

	IsectHitsMemory uint64 // intersection memory-layer hits since process start
	IsectHitsDisk   uint64 // intersection disk-layer hits since process start
	IsectMisses     uint64 // intersection misses since process start
}

// NewCache opens or creates a two-layer cache.
// diskPath is the directory for the pogreb database.
// memTTL controls how long entries live in memory.
// memMaxSize is the maximum number of in-memory entries (0 = unlimited).
func NewCache(diskPath string, memTTL time.Duration, memMaxSize int) (*Cache, error) {
	if err := os.MkdirAll(diskPath, 0o755); err != nil {
		return nil, fmt.Errorf("geocache: create dir %s: %w", diskPath, err)
	}

	disk, err := pogreb.Open(diskPath, nil)
	if err != nil {
		return nil, fmt.Errorf("geocache: open pogreb: %w", err)
	}

	opts := []ttlcache.Option[string, *Address]{
		ttlcache.WithTTL[string, *Address](memTTL),
	}
	if memMaxSize > 0 {
		opts = append(opts, ttlcache.WithCapacity[string, *Address](uint64(memMaxSize)))
	}
	mem := ttlcache.New(opts...)
	go mem.Start() // start eviction goroutine

	isectOpts := []ttlcache.Option[string, string]{ttlcache.WithTTL[string, string](memTTL)}
	if memMaxSize > 0 {
		isectOpts = append(isectOpts, ttlcache.WithCapacity[string, string](uint64(memMaxSize)))
	}
	memISect := ttlcache.New(isectOpts...)
	go memISect.Start()

	return &Cache{
		mem:      mem,
		memISect: memISect,
		disk:     disk,
	}, nil
}

// intersectionKeyPrefix namespaces intersection entries within the shared
// pogreb DB so they never collide with reverse-geocode address keys.
const intersectionKeyPrefix = "isect:"

// IntersectionCacheKey builds an intersection cache key from lat/lon rounded
// to the given number of decimal places. Intersections are coordinate-only
// (no language), prefixed to stay isolated from address keys in the shared DB.
func IntersectionCacheKey(lat, lon float64, detail int) string {
	return intersectionKeyPrefix + CacheKey(lat, lon, detail)
}

// CacheKey builds a cache key from lat/lon rounded to the given number of
// decimal places.
func CacheKey(lat, lon float64, detail int) string {
	format := fmt.Sprintf("%%.%df-%%.%df", detail, detail)
	return fmt.Sprintf(format, lat, lon)
}

// CacheKeyForLanguage builds a reverse geocode cache key. Blank language keeps
// the historical coordinate-only key; localized lookups include the language
// code so provider-localized address data stays isolated per user locale.
func CacheKeyForLanguage(lat, lon float64, detail int, language string) string {
	key := CacheKey(lat, lon, detail)
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		return key
	}
	return language + ":" + key
}

// Get looks up an address by key. It checks memory first, then disk.
// On a disk hit the entry is promoted to memory.
func (c *Cache) Get(key string) (*Address, bool) {
	// Memory layer
	if item := c.mem.Get(key); item != nil {
		c.hitsMemory.Add(1)
		return item.Value(), true
	}

	// Disk layer
	data, err := c.disk.Get([]byte(key))
	if err != nil || data == nil {
		c.misses.Add(1)
		return nil, false
	}

	var addr Address
	if err := json.Unmarshal(data, &addr); err != nil {
		c.misses.Add(1)
		return nil, false
	}

	// Promote to memory
	c.hitsDisk.Add(1)
	c.mem.Set(key, &addr, ttlcache.DefaultTTL)
	return &addr, true
}

// Stats returns a point-in-time snapshot of cache health metrics.
func (c *Cache) Stats() CacheStats {
	return CacheStats{
		MemoryEntries:   c.mem.Len() + c.memISect.Len(),
		DiskEntries:     int(c.disk.Count()),
		HitsMemory:      c.hitsMemory.Load(),
		HitsDisk:        c.hitsDisk.Load(),
		Misses:          c.misses.Load(),
		IsectHitsMemory: c.isectHitsMemory.Load(),
		IsectHitsDisk:   c.isectHitsDisk.Load(),
		IsectMisses:     c.isectMisses.Load(),
	}
}

// ClearMemory drops all entries from both in-memory layers (address +
// intersection). The disk layer is left untouched. Returns the number of
// entries that were removed.
func (c *Cache) ClearMemory() int {
	n := c.mem.Len() + c.memISect.Len()
	c.mem.DeleteAll()
	c.memISect.DeleteAll()
	return n
}

// Set writes an address to both memory and disk.
func (c *Cache) Set(key string, addr *Address) {
	c.mem.Set(key, addr, ttlcache.DefaultTTL)

	data, err := json.Marshal(addr)
	if err != nil {
		return
	}
	if err := c.disk.Put([]byte(key), data); err != nil {
		log.Debugf("geocache: disk write failed for %s: %s", key, err)
	}
}

// GetIntersection looks up a cached intersection string by key (built with
// IntersectionCacheKey). Memory first, then disk; a disk hit is promoted to
// memory. A cached empty string is a HIT (negative cache for "no intersection
// here") and is returned as ("", true), distinct from a never-stored miss.
func (c *Cache) GetIntersection(key string) (string, bool) {
	if item := c.memISect.Get(key); item != nil {
		c.isectHitsMemory.Add(1)
		return item.Value(), true
	}

	data, err := c.disk.Get([]byte(key))
	if err != nil || data == nil {
		c.isectMisses.Add(1)
		return "", false
	}

	// Values are JSON-encoded strings so an empty intersection serializes as
	// `""` (2 bytes) — never a zero-length value pogreb can't distinguish
	// from "missing".
	var val string
	if err := json.Unmarshal(data, &val); err != nil {
		c.isectMisses.Add(1)
		return "", false
	}

	c.isectHitsDisk.Add(1)
	c.memISect.Set(key, val, ttlcache.DefaultTTL)
	return val, true
}

// SetIntersection writes an intersection string to both memory and the shared
// disk DB. An empty value is stored deliberately (negative cache).
func (c *Cache) SetIntersection(key, value string) {
	c.memISect.Set(key, value, ttlcache.DefaultTTL)

	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	if err := c.disk.Put([]byte(key), data); err != nil {
		log.Debugf("geocache: intersection disk write failed for %s: %s", key, err)
	}
}

// Close stops the memory cache eviction loops and closes the disk database.
func (c *Cache) Close() error {
	c.mem.Stop()
	c.memISect.Stop()
	return c.disk.Close()
}
