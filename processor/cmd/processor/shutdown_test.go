package main

import (
	"sync"
	"testing"
	"time"
)

func TestWaitTimeoutReturnsWhenDone(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		time.Sleep(10 * time.Millisecond)
		wg.Done()
	}()

	if !waitTimeout(&wg, time.Second) {
		t.Fatal("waitTimeout = false, want true once the WaitGroup finishes")
	}
}

func TestWaitTimeoutGivesUp(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Done()

	start := time.Now()
	if waitTimeout(&wg, 50*time.Millisecond) {
		t.Fatal("waitTimeout = true, want false while the WaitGroup is still busy")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("waitTimeout took %v, want about 50ms", elapsed)
	}
}
