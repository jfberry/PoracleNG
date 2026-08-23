package tracker

import (
	"hash/maphash"
	"strconv"
	"sync"
	"time"
)

// expiringSetShards is the number of independently-locked shards. Sweeping
// takes one shard's lock at a time, so a full sweep of a multi-million-entry
// set never blocks the webhook path for more than a fraction of the total
// scan.
const expiringSetShards = 16

// expiringSweepInterval is how often the background sweeper reclaims expired
// fingerprints. Reads expire lazily regardless, so this only bounds memory,
// never correctness.
const expiringSweepInterval = time.Minute

// expiringSet is a set of key fingerprints with per-entry expiry.
//
// It exists because storing dedup keys in a ttlcache cost ~265 B per entry —
// a boxed item, the retained key string, a container/list node and an
// expiry-heap entry — to remember a single bit. At scanner throughput the
// live set runs to millions of keys, which measured 1.42 GB (44.8% of live
// heap) in a production profile. Hashing the key to a uint64 and storing only
// its expiry brings that to ~25 B per entry.
//
// Keys are fingerprinted, not retained, so a 64-bit collision would read as a
// false duplicate and drop one alert. Against a live set of ~6M entries that
// is ~3.3e-13 per insert, or roughly one dropped alert every few decades at
// production insert rates.
type expiringSet struct {
	seed   maphash.Seed
	shards [expiringSetShards]expiringShard
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
}

type expiringShard struct {
	mu sync.Mutex
	// entries maps a key fingerprint to its expiry in unix nanoseconds.
	// Nanoseconds rather than seconds so a sub-second TTL is not truncated
	// into the past on write; the value is 8 bytes either way.
	entries map[uint64]int64
}

func newExpiringSet() *expiringSet {
	s := &expiringSet{
		seed: maphash.MakeSeed(),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	for i := range s.shards {
		s.shards[i].entries = make(map[uint64]int64)
	}
	go s.sweepLoop()
	return s
}

func (s *expiringSet) fingerprint(key string) uint64 {
	return maphash.String(s.seed, key)
}

func (s *expiringSet) shardFor(fp uint64) *expiringShard {
	return &s.shards[fp%expiringSetShards]
}

// keyWriter accumulates a dedup key's components directly into a hash, so the
// key never exists as a string. The old path built one with fmt.Sprintf per
// webhook purely for maphash to consume and discard.
//
// Components are zero-separated. The concatenation this replaced was
// ambiguous in principle — ("ab","c") and ("a","bc") produced the same key —
// which the separator removes.
type keyWriter struct{ h maphash.Hash }

func (k *keyWriter) Str(v string) *keyWriter {
	_, _ = k.h.WriteString(v)
	_ = k.h.WriteByte(0)
	return k
}

func (k *keyWriter) Int(v int64) *keyWriter {
	var buf [20]byte
	_, _ = k.h.Write(strconv.AppendInt(buf[:0], v, 10))
	_ = k.h.WriteByte(0)
	return k
}

func (k *keyWriter) Bool(v bool) *keyWriter {
	b := byte('F')
	if v {
		b = 'T'
	}
	_ = k.h.WriteByte(b)
	_ = k.h.WriteByte(0)
	return k
}

// newKey returns a keyWriter seeded for this set. Returned by value and used
// as a local so it never escapes: taking its address across a function
// boundary (for example by passing a build callback) makes escape analysis
// heap-allocate it, which is the allocation this whole path exists to avoid.
//
// maphash.Hash must not be copied after first use; this copy happens before
// any write.
func (s *expiringSet) newKey() keyWriter {
	var k keyWriter
	k.h.SetSeed(s.seed)
	return k
}

// CheckAndAdd reports whether the key was already present and unexpired, and
// records it with ttl when it was not.
//
// One hash and one lock acquisition, against the two each that a Has-miss
// followed by Add cost. It also closes the check-then-act window in the old
// pairing, where two workers handling the same encounter could both miss and
// both deliver.
func (s *expiringSet) CheckAndAdd(k *keyWriter, ttl time.Duration) bool {
	fp := k.h.Sum64()
	sh := s.shardFor(fp)
	now := time.Now()

	sh.mu.Lock()
	defer sh.mu.Unlock()

	if expiry, ok := sh.entries[fp]; ok && expiry > now.UnixNano() {
		return true
	}
	sh.entries[fp] = now.Add(ttl).UnixNano()
	return false
}

// Has reports whether key is present and unexpired. Entries past their expiry
// read as absent even when the sweeper has not reclaimed them yet.
func (s *expiringSet) Has(key string) bool {
	fp := s.fingerprint(key)
	sh := s.shardFor(fp)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	expiry, ok := sh.entries[fp]
	return ok && expiry > time.Now().UnixNano()
}

// Add records key for the given ttl, replacing any existing expiry.
func (s *expiringSet) Add(key string, ttl time.Duration) {
	fp := s.fingerprint(key)
	sh := s.shardFor(fp)

	expiry := time.Now().Add(ttl).UnixNano()

	sh.mu.Lock()
	sh.entries[fp] = expiry
	sh.mu.Unlock()
}

// Len returns the number of entries still held, expired or not.
func (s *expiringSet) Len() int {
	var n int
	for i := range s.shards {
		s.shards[i].mu.Lock()
		n += len(s.shards[i].entries)
		s.shards[i].mu.Unlock()
	}
	return n
}

// Close stops the background sweeper and waits for it to exit. Safe to call
// more than once.
//
// The wait matters: sweep holds a shard lock while it scans, so a Close that
// returned early would leave DuplicateCache.Close's step of the shutdown
// ordering non-quiescent. mute.Sweeper and snapshots.Sweeper use the same
// stop/done/once trio.
func (s *expiringSet) Close() {
	s.once.Do(func() { close(s.stop) })
	<-s.done
}

func (s *expiringSet) sweepLoop() {
	defer close(s.done)
	ticker := time.NewTicker(expiringSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.sweep(time.Now().UnixNano())
		}
	}
}

// sweep drops expired entries, taking one shard lock at a time. now is unix
// nanoseconds.
func (s *expiringSet) sweep(now int64) {
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for fp, expiry := range sh.entries {
			if expiry <= now {
				delete(sh.entries, fp)
			}
		}
		sh.mu.Unlock()
	}
}
