package geocoding

import (
	"encoding/json"
	"fmt"
	"os"
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

	return &Cache{
		mem:  mem,
		disk: disk,
	}, nil
}

// CacheKey builds a cache key from lat/lon rounded to the given number of
// decimal places.
func CacheKey(lat, lon float64, detail int) string {
	format := fmt.Sprintf("%%.%df-%%.%df", detail, detail)
	return fmt.Sprintf(format, lat, lon)
}

// Get looks up an address by key. It checks memory first, then disk.
// On a disk hit the entry is promoted to memory.
func (c *Cache) Get(key string) (*Address, bool) {
	// Memory layer
	if item := c.mem.Get(key); item != nil {
		return item.Value(), true
	}

	// Disk layer
	data, err := c.disk.Get([]byte(key))
	if err != nil || data == nil {
		return nil, false
	}

	var addr Address
	if err := json.Unmarshal(data, &addr); err != nil {
		return nil, false
	}

	// Promote to memory
	c.mem.Set(key, &addr, ttlcache.DefaultTTL)
	return &addr, true
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

// Close stops the memory cache eviction loop and closes the disk database.
func (c *Cache) Close() error {
	c.mem.Stop()
	return c.disk.Close()
}
