package tracker

import (
	"sync/atomic"
	"time"
)

// testClock is a race-free injectable clock. Tests that hand a clock to a
// constructor which starts background goroutines cannot use a plain captured
// variable: advancing it would race the goroutine's read.
type testClock struct{ unix atomic.Int64 }

func newTestClock(unix int64) *testClock {
	c := &testClock{}
	c.unix.Store(unix)
	return c
}

func (c *testClock) now() time.Time { return time.Unix(c.unix.Load(), 0) }

func (c *testClock) advance(d time.Duration) { c.unix.Add(int64(d / time.Second)) }
