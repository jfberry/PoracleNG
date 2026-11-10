package humanlock

import (
	"sync"
	"testing"
	"time"
)

func TestLockSerialisesSameID(t *testing.T) {
	inside := 0
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := Lock("u1")
			defer unlock()
			inside++
			if inside != 1 {
				t.Errorf("inside = %d, want 1", inside)
			}
			time.Sleep(time.Millisecond)
			inside--
		}()
	}
	wg.Wait()
}

func TestLockDifferentIDsDoNotBlock(t *testing.T) {
	unlockA := Lock("a")
	defer unlockA()
	done := make(chan struct{})
	go func() {
		Lock("b")()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Lock(b) blocked while a was held")
	}
}

func TestLockEvictsWhenIdle(t *testing.T) {
	Lock("x")()
	Lock("y")()
	if n := held(); n != 0 {
		t.Fatalf("held() = %d, want 0", n)
	}
}

func TestLockEmptyIDIsNoop(t *testing.T) {
	u1 := Lock("")
	u2 := Lock("")
	if n := held(); n != 0 {
		t.Fatalf("held() = %d, want 0", n)
	}
	u1()
	u2()
}
