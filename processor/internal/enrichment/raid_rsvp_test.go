package enrichment

import (
	"testing"
	"time"
)

func TestNormalizeTimeslotToSeconds(t *testing.T) {
	// A timestamp clearly in seconds (current era ≈ 1.7e9) must pass through
	// unchanged. A timestamp clearly in milliseconds (≈ 1.7e12) must be
	// divided by 1000.
	nowSec := time.Now().Unix()
	nowMs := time.Now().UnixMilli()

	cases := []struct {
		name string
		in   int64
		want int64
	}{
		{"seconds passthrough (now)", nowSec, nowSec},
		{"seconds passthrough (future raid timeslot)", nowSec + 3600, nowSec + 3600},
		{"milliseconds divided", nowMs, nowMs / 1000},
		{"milliseconds future", nowMs + 3600_000, (nowMs + 3600_000) / 1000},
		{"zero stays zero", 0, 0},
		// Boundary: exactly 1e12 should be treated as ms.
		{"boundary ms", 1_000_000_000_000, 1_000_000_000},
		// Just under 1e12 stays as-is (treated as seconds).
		{"just-under-boundary seconds", 999_999_999_999, 999_999_999_999},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeTimeslotToSeconds(tc.in); got != tc.want {
				t.Errorf("normalizeTimeslotToSeconds(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
