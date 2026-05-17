package logbuffer

import (
	"sync"
	"time"
)

// Entry is one captured log record.
type Entry struct {
	Time    time.Time
	Level   string // "WARN" or "ERROR"
	Message string
	Source  string // "file.go:42"; empty when not available
}

// Buffer keeps a fixed-size startup buffer and a fixed-size rolling
// buffer of WARN/ERROR log entries. Thread-safe.
//
// Before MarkStartupComplete is called, all Capture calls feed the
// startup buffer. The startup buffer is append-only and capped: once
// it reaches startupCap entries, subsequent entries are silently
// dropped (the earliest entries are the most interesting for
// diagnosing a misconfigured deployment).
//
// After MarkStartupComplete is called, all Capture calls feed the
// rolling buffer, which operates as a FIFO ring: when full it
// overwrites the oldest entry.
type Buffer struct {
	mu              sync.Mutex
	startup         []Entry // append-only; capped at startupCap
	rolling         []Entry // ring buffer; capped at rollingCap
	head            int     // index of the oldest entry in the ring
	count           int     // number of entries currently in the ring
	startupCap      int
	rollingCap      int
	startupComplete bool
}

// New returns a Buffer with the given caps. Suggested production values:
// startupCap=200, rollingCap=50.
func New(startupCap, rollingCap int) *Buffer {
	if startupCap < 1 {
		startupCap = 1
	}
	if rollingCap < 1 {
		rollingCap = 1
	}
	return &Buffer{
		startup:    make([]Entry, 0, startupCap),
		rolling:    make([]Entry, rollingCap),
		startupCap: startupCap,
		rollingCap: rollingCap,
	}
}

// Capture records one entry. Called by the log hook.
// Routes to the startup buffer until MarkStartupComplete is called,
// then routes to the rolling buffer.
func (b *Buffer) Capture(level, message, source string) {
	e := Entry{
		Time:    time.Now(),
		Level:   level,
		Message: message,
		Source:  source,
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.startupComplete {
		if len(b.startup) < b.startupCap {
			b.startup = append(b.startup, e)
		}
		// silently drop if startup buffer is full
		return
	}
	// Rolling ring: find the write slot
	slot := (b.head + b.count) % b.rollingCap
	b.rolling[slot] = e
	if b.count < b.rollingCap {
		b.count++
	} else {
		// ring is full: advance head to drop oldest
		b.head = (b.head + 1) % b.rollingCap
	}
}

// MarkStartupComplete freezes the startup buffer. Subsequent Capture
// calls go to the rolling buffer only.
func (b *Buffer) MarkStartupComplete() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.startupComplete = true
}

// Startup returns a copy of the startup buffer. Safe to iterate
// outside the buffer's lock.
func (b *Buffer) Startup() []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Entry, len(b.startup))
	copy(out, b.startup)
	return out
}

// Recent returns a copy of the rolling buffer in chronological order
// (oldest first). Returns nil if no rolling entries exist.
func (b *Buffer) Recent() []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.count == 0 {
		return nil
	}
	out := make([]Entry, b.count)
	for i := 0; i < b.count; i++ {
		out[i] = b.rolling[(b.head+i)%b.rollingCap]
	}
	return out
}

// ClearRecent empties the rolling buffer. The startup buffer is unaffected.
func (b *Buffer) ClearRecent() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head = 0
	b.count = 0
}
