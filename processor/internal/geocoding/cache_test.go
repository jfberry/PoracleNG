package geocoding

import (
	"encoding/json"
	"testing"
	"time"
)

// newTestCache creates a Cache backed by a temporary pogreb directory.
// The caller is responsible for calling Close() when done.
func newTestCache(t *testing.T) *Cache {
	t.Helper()
	dir := t.TempDir()
	c, err := NewCache(dir, 5*time.Minute, 0)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

var testAddr = &Address{City: "Berlin", StreetName: "Hauptstrasse", StreetNumber: "1"}

func TestCacheStats_EmptyOnFresh(t *testing.T) {
	c := newTestCache(t)
	s := c.Stats()
	if s.MemoryEntries != 0 {
		t.Errorf("MemoryEntries: got %d, want 0", s.MemoryEntries)
	}
	if s.DiskEntries != 0 {
		t.Errorf("DiskEntries: got %d, want 0", s.DiskEntries)
	}
	if s.HitsMemory != 0 {
		t.Errorf("HitsMemory: got %d, want 0", s.HitsMemory)
	}
	if s.HitsDisk != 0 {
		t.Errorf("HitsDisk: got %d, want 0", s.HitsDisk)
	}
	if s.Misses != 0 {
		t.Errorf("Misses: got %d, want 0", s.Misses)
	}
}

func TestCacheStats_HitsMemoryIncrementsCorrectly(t *testing.T) {
	c := newTestCache(t)
	c.Set("key1", testAddr)

	// Get should be served from memory (Set populates both layers).
	addr, ok := c.Get("key1")
	if !ok || addr == nil {
		t.Fatal("Get returned nothing after Set")
	}

	s := c.Stats()
	if s.HitsMemory != 1 {
		t.Errorf("HitsMemory: got %d, want 1", s.HitsMemory)
	}
	if s.HitsDisk != 0 {
		t.Errorf("HitsDisk: got %d, want 0 (should have been a memory hit)", s.HitsDisk)
	}
	if s.Misses != 0 {
		t.Errorf("Misses: got %d, want 0", s.Misses)
	}
}

func TestCacheStats_MissIncrements(t *testing.T) {
	c := newTestCache(t)

	_, ok := c.Get("nonexistent-key")
	if ok {
		t.Fatal("expected miss, got hit")
	}

	s := c.Stats()
	if s.Misses != 1 {
		t.Errorf("Misses: got %d, want 1", s.Misses)
	}
	if s.HitsMemory != 0 {
		t.Errorf("HitsMemory: got %d, want 0", s.HitsMemory)
	}
	if s.HitsDisk != 0 {
		t.Errorf("HitsDisk: got %d, want 0", s.HitsDisk)
	}
}

// TestCacheStats_DiskHitIncrements tests that a lookup which misses memory
// but hits the disk layer increments HitsDisk. We write directly to the
// pogreb disk layer (bypassing Cache.Set so memory is not populated), then
// call Get. This test is in package geocoding (white-box) so it can access
// the unexported c.disk field.
func TestCacheStats_DiskHitIncrements(t *testing.T) {
	c := newTestCache(t)

	// Write directly to the disk layer without populating memory.
	data, err := json.Marshal(testAddr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := c.disk.Put([]byte("disk-only-key"), data); err != nil {
		t.Fatalf("disk.Put: %v", err)
	}

	// Memory should be empty; disk should have 1 entry.
	if s := c.Stats(); s.MemoryEntries != 0 {
		t.Fatalf("expected empty memory before Get, got %d entries", s.MemoryEntries)
	}

	addr, ok := c.Get("disk-only-key")
	if !ok || addr == nil {
		t.Fatal("Get returned nothing for disk-only entry")
	}

	s := c.Stats()
	if s.HitsDisk != 1 {
		t.Errorf("HitsDisk: got %d, want 1", s.HitsDisk)
	}
	if s.HitsMemory != 0 {
		t.Errorf("HitsMemory: got %d, want 0", s.HitsMemory)
	}
	if s.Misses != 0 {
		t.Errorf("Misses: got %d, want 0", s.Misses)
	}
	// Entry should also have been promoted to memory.
	if s.MemoryEntries != 1 {
		t.Errorf("MemoryEntries after disk hit: got %d, want 1 (promote-to-memory expected)", s.MemoryEntries)
	}
}

func TestCache_Intersection_RoundTrip(t *testing.T) {
	c := newTestCache(t)
	key := IntersectionCacheKey(52.5, 13.4, 4)

	// Miss before anything is stored.
	if _, ok := c.GetIntersection(key); ok {
		t.Fatal("expected intersection miss on fresh cache")
	}

	c.SetIntersection(key, "Main St & 2nd Ave")
	got, ok := c.GetIntersection(key)
	if !ok || got != "Main St & 2nd Ave" {
		t.Fatalf("GetIntersection after Set: got (%q, %v), want (%q, true)", got, ok, "Main St & 2nd Ave")
	}
}

// A negative result ("no intersection here") is a stable fact and must be
// cached so we don't re-query GeoNames forever for rural stops. The empty
// string must round-trip as a HIT, distinct from a never-stored MISS.
func TestCache_Intersection_NegativeCaches(t *testing.T) {
	c := newTestCache(t)
	key := IntersectionCacheKey(0, 0, 4)

	c.SetIntersection(key, "")
	got, ok := c.GetIntersection(key)
	if !ok {
		t.Fatal("cached empty intersection should be a HIT, not a miss")
	}
	if got != "" {
		t.Fatalf("GetIntersection: got %q, want empty string", got)
	}
}

// Intersection entries share the pogreb DB with addresses but live under a
// distinct key namespace, so an address key and an intersection key derived
// from the same coordinates never collide.
func TestCache_Intersection_NamespaceIsolation(t *testing.T) {
	c := newTestCache(t)
	addrKey := CacheKey(52.5, 13.4, 4)
	isectKey := IntersectionCacheKey(52.5, 13.4, 4)
	if addrKey == isectKey {
		t.Fatalf("intersection key %q must differ from address key %q", isectKey, addrKey)
	}

	c.Set(addrKey, testAddr)
	c.SetIntersection(isectKey, "A & B")

	if _, ok := c.GetIntersection(addrKey); ok {
		t.Error("address key should not resolve as an intersection")
	}
	if a, ok := c.Get(addrKey); !ok || a.City != "Berlin" {
		t.Error("address lookup broken by intersection write")
	}
}

func TestCache_ClearMemory_Empties(t *testing.T) {
	c := newTestCache(t)

	c.Set("a", testAddr)
	c.Set("b", testAddr)
	c.Set("c", testAddr)

	if s := c.Stats(); s.MemoryEntries != 3 {
		t.Fatalf("expected 3 memory entries before ClearMemory, got %d", s.MemoryEntries)
	}

	dropped := c.ClearMemory()
	if dropped != 3 {
		t.Errorf("ClearMemory returned %d, want 3", dropped)
	}

	s := c.Stats()
	if s.MemoryEntries != 0 {
		t.Errorf("MemoryEntries after ClearMemory: got %d, want 0", s.MemoryEntries)
	}
	// Disk layer should be untouched.
	if s.DiskEntries != 3 {
		t.Errorf("DiskEntries after ClearMemory: got %d, want 3 (disk untouched)", s.DiskEntries)
	}
}
