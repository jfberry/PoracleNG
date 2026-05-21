package tracker

import (
	"testing"
	"time"
)

func TestRecentActivityRaidBosses(t *testing.T) {
	r := NewRecentActivity()
	r.RecordRaidBoss(150)
	r.RecordRaidBoss(151)
	r.RecordRaidBoss(150) // duplicate; should update timestamp

	active := r.ActiveRaidBosses()
	if len(active) != 2 {
		t.Fatalf("want 2 active bosses, got %d", len(active))
	}
}

func TestRecentActivityExpiry(t *testing.T) {
	r := NewRecentActivity()
	r.now = func() time.Time { return time.Unix(1000, 0) }
	r.RecordRaidBoss(150)
	r.now = func() time.Time { return time.Unix(1000+int64(7*time.Hour/time.Second), 0) }

	active := r.ActiveRaidBosses()
	if len(active) != 0 {
		t.Fatalf("want 0 after TTL expiry, got %d", len(active))
	}
}

func TestRecentActivityZeroIgnored(t *testing.T) {
	r := NewRecentActivity()
	r.RecordRaidBoss(0)
	if len(r.ActiveRaidBosses()) != 0 {
		t.Fatal("zero ID should not be recorded")
	}
}

func TestRecentActivityRaceSafe(t *testing.T) {
	r := NewRecentActivity()
	done := make(chan struct{})
	go func() {
		for i := 1; i <= 1000; i++ {
			r.RecordRaidBoss(i)
		}
		close(done)
	}()
	for range 100 {
		_ = r.ActiveRaidBosses()
	}
	<-done
}
