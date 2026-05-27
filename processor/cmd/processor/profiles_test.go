package main

import (
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func TestMatchesTimeWindow(t *testing.T) {
	tests := []struct {
		name         string
		entry        db.ActiveHourEntry
		nowDow       int
		yesterdayDow int
		nowHour      int
		nowMin       int
		want         bool
	}{
		// Same hour, within 10 minutes
		{
			name:   "exact match at scheduled time",
			entry:  db.ActiveHourEntry{Day: 1, Hours: 9, Mins: 0},
			nowDow: 1, yesterdayDow: 7, nowHour: 9, nowMin: 0,
			want: true,
		},
		{
			name:   "within 10 minutes of scheduled time",
			entry:  db.ActiveHourEntry{Day: 1, Hours: 9, Mins: 0},
			nowDow: 1, yesterdayDow: 7, nowHour: 9, nowMin: 9,
			want: true,
		},
		{
			name:   "outside 10 minute window",
			entry:  db.ActiveHourEntry{Day: 1, Hours: 9, Mins: 0},
			nowDow: 1, yesterdayDow: 7, nowHour: 9, nowMin: 10,
			want: false,
		},
		{
			name:   "wrong day",
			entry:  db.ActiveHourEntry{Day: 2, Hours: 9, Mins: 0},
			nowDow: 1, yesterdayDow: 7, nowHour: 9, nowMin: 0,
			want: false,
		},

		// Hour boundary: schedule at XX:50+, now at XX+1:00-09
		{
			name:   "hour boundary - schedule at 9:55, now at 10:05",
			entry:  db.ActiveHourEntry{Day: 1, Hours: 9, Mins: 55},
			nowDow: 1, yesterdayDow: 7, nowHour: 10, nowMin: 5,
			want: true,
		},
		{
			name:   "hour boundary - schedule at 9:55, now at 10:00",
			entry:  db.ActiveHourEntry{Day: 1, Hours: 9, Mins: 55},
			nowDow: 1, yesterdayDow: 7, nowHour: 10, nowMin: 0,
			want: true,
		},
		{
			name:   "hour boundary - not triggered at 10:10",
			entry:  db.ActiveHourEntry{Day: 1, Hours: 9, Mins: 55},
			nowDow: 1, yesterdayDow: 7, nowHour: 10, nowMin: 10,
			want: false,
		},
		{
			name:   "hour boundary - mins not > 50",
			entry:  db.ActiveHourEntry{Day: 1, Hours: 9, Mins: 40},
			nowDow: 1, yesterdayDow: 7, nowHour: 10, nowMin: 5,
			want: false,
		},

		// Midnight boundary: schedule Sunday 23:55, now Monday 00:05
		{
			name:   "midnight boundary - Sunday 23:55 matches Monday 00:05",
			entry:  db.ActiveHourEntry{Day: 7, Hours: 23, Mins: 55},
			nowDow: 1, yesterdayDow: 7, nowHour: 0, nowMin: 5,
			want: true,
		},
		{
			name:   "midnight boundary - no match at 00:10",
			entry:  db.ActiveHourEntry{Day: 7, Hours: 23, Mins: 55},
			nowDow: 1, yesterdayDow: 7, nowHour: 0, nowMin: 10,
			want: false,
		},
		{
			name:   "midnight boundary - wrong yesterday",
			entry:  db.ActiveHourEntry{Day: 6, Hours: 23, Mins: 55},
			nowDow: 1, yesterdayDow: 7, nowHour: 0, nowMin: 5,
			want: false,
		},

		// Same hour, scheduled at :50, now at :55
		{
			name:   "same hour late - schedule at 14:50, now 14:55",
			entry:  db.ActiveHourEntry{Day: 3, Hours: 14, Mins: 50},
			nowDow: 3, yesterdayDow: 2, nowHour: 14, nowMin: 55,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesTimeWindow(tt.entry, tt.nowDow, tt.yesterdayDow, tt.nowHour, tt.nowMin)
			if got != tt.want {
				t.Errorf("matchesTimeWindow(%+v, dow=%d, yesterday=%d, hour=%d, min=%d) = %v, want %v",
					tt.entry, tt.nowDow, tt.yesterdayDow, tt.nowHour, tt.nowMin, got, tt.want)
			}
		})
	}
}

// TestMatchesTimeWindow_RangeEntry exercises the range+step path that
// the schedule parser produces from e.g. "weekday:9-17/2" — a single
// ActiveHourEntry with Step > 0 that expands at scheduler-tick time
// into multiple fire points (9, 11, 13, 15, 17 in this case). The
// existing TestMatchesTimeWindow only covers single-fire entries
// (one Fires() pair), so this test pins the multi-pair iteration.
func TestMatchesTimeWindow_RangeEntry(t *testing.T) {
	// "weekday:9-17/2" expressed as a single entry on Day 1 (Mon).
	// Fires at 09:00, 11:00, 13:00, 15:00, 17:00.
	entry := db.ActiveHourEntry{Day: 1, Hours: 9, EndHours: 17, Step: 2}

	tests := []struct {
		name                                  string
		nowDow, yesterdayDow, nowHour, nowMin int
		want                                  bool
	}{
		// On a fire-point, first 10 minutes — match.
		{"at first fire 09:00", 1, 7, 9, 0, true},
		{"at first fire 09:09", 1, 7, 9, 9, true},
		{"at middle fire 13:05", 1, 7, 13, 5, true},
		{"at last fire 17:00", 1, 7, 17, 0, true},
		// Outside the 10-min grace of any fire — miss.
		{"between fires 10:00 (no 10:00 fire)", 1, 7, 10, 0, false},
		{"10 minutes past first fire 09:10", 1, 7, 9, 10, false},
		{"before first fire 08:00", 1, 7, 8, 0, false},
		{"after last fire 18:00", 1, 7, 18, 0, false},
		// Cross-hour grace: 12:05 matches if 11:55 was a fire (it isn't).
		{"12:05 (11:55 not a fire)", 1, 7, 12, 5, false},
		// Wrong day — miss even on a valid fire-point hour.
		{"wrong day", 2, 1, 13, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesTimeWindow(entry, tt.nowDow, tt.yesterdayDow, tt.nowHour, tt.nowMin)
			if got != tt.want {
				t.Errorf("matchesTimeWindow(range 9-17/2, dow=%d, hour=%d, min=%d) = %v, want %v",
					tt.nowDow, tt.nowHour, tt.nowMin, got, tt.want)
			}
		})
	}
}

// TestMatchesTimeWindow_RangeCrossHourGrace pins the cross-hour grace
// for a range entry whose step lands a fire-point at HH:55+ (e.g.
// "9:55-23:55/2" would put a fire at 9:55 that needs to still match
// at 10:00..10:09 via the "previous hour" grace).
func TestMatchesTimeWindow_RangeCrossHourGrace(t *testing.T) {
	// Entry with fire-points at 9:55, 11:55, 13:55, 15:55, 17:55.
	entry := db.ActiveHourEntry{Day: 1, Hours: 9, Mins: 55, EndHours: 17, EndMins: 55, Step: 2}

	// 10:05 (just after 9:55 fire) should match via "previous hour" grace.
	if !matchesTimeWindow(entry, 1, 7, 10, 5) {
		t.Error("expected match at 10:05 (cross-hour grace from 9:55 fire)")
	}
	// 10:15 is past the 10-minute grace window.
	if matchesTimeWindow(entry, 1, 7, 10, 15) {
		t.Error("expected miss at 10:15 (past 10-min grace)")
	}
}

func TestNextScheduleTime(t *testing.T) {
	loc := time.UTC
	mins := profileScheduleMinutes // {0, 10, 15, 20, 30, 40, 45, 50}

	tests := []struct {
		name    string
		now     time.Time
		wantMin int
		wantHr  int
	}{
		{
			name:    "before first slot",
			now:     time.Date(2026, 3, 21, 14, 3, 30, 0, loc),
			wantMin: 10,
			wantHr:  14,
		},
		{
			name:    "between :10 and :15",
			now:     time.Date(2026, 3, 21, 14, 12, 0, 0, loc),
			wantMin: 15,
			wantHr:  14,
		},
		{
			name:    "between :15 and :20",
			now:     time.Date(2026, 3, 21, 14, 17, 0, 0, loc),
			wantMin: 20,
			wantHr:  14,
		},
		{
			name:    "between :20 and :30",
			now:     time.Date(2026, 3, 21, 14, 25, 0, 0, loc),
			wantMin: 30,
			wantHr:  14,
		},
		{
			name:    "between :40 and :45",
			now:     time.Date(2026, 3, 21, 14, 42, 0, 0, loc),
			wantMin: 45,
			wantHr:  14,
		},
		{
			name:    "between :45 and :50",
			now:     time.Date(2026, 3, 21, 14, 48, 0, 0, loc),
			wantMin: 50,
			wantHr:  14,
		},
		{
			name:    "after last slot wraps to next hour",
			now:     time.Date(2026, 3, 21, 14, 55, 0, 0, loc),
			wantMin: 0,
			wantHr:  15,
		},
		{
			name:    "at :59 wraps to next hour",
			now:     time.Date(2026, 3, 21, 23, 59, 0, 0, loc),
			wantMin: 0,
			wantHr:  0, // midnight next day
		},
		{
			name:    "exactly on slot advances to next",
			now:     time.Date(2026, 3, 21, 14, 15, 0, 1, loc), // 1ns past :15
			wantMin: 20,
			wantHr:  14,
		},
		{
			name:    "exactly on :00 with zero nanoseconds",
			now:     time.Date(2026, 3, 21, 14, 0, 0, 0, loc),
			wantMin: 0,
			wantHr:  14,
		},
		{
			name:    "exactly on :50 with some seconds past",
			now:     time.Date(2026, 3, 21, 14, 50, 1, 0, loc),
			wantMin: 0,
			wantHr:  15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextScheduleTime(tt.now, mins)
			if got.Minute() != tt.wantMin || got.Hour() != tt.wantHr {
				t.Errorf("nextScheduleTime(%s) = %s (hour=%d min=%d), want hour=%d min=%d",
					tt.now.Format("15:04:05.000"), got.Format("15:04:05"),
					got.Hour(), got.Minute(), tt.wantHr, tt.wantMin)
			}
			if got.Second() != 0 || got.Nanosecond() != 0 {
				t.Errorf("nextScheduleTime should return zero seconds, got %s", got.Format("15:04:05.000000000"))
			}
			if !got.After(tt.now) && (!got.Equal(tt.now) || tt.now.Second() != 0 || tt.now.Nanosecond() != 0) {
				t.Errorf("nextScheduleTime(%s) = %s, should be after now",
					tt.now.Format("15:04:05.000"), got.Format("15:04:05"))
			}
		})
	}
}

func TestIsoDow(t *testing.T) {
	tests := []struct {
		wd   time.Weekday
		want int
	}{
		{time.Sunday, 7},
		{time.Monday, 1},
		{time.Tuesday, 2},
		{time.Wednesday, 3},
		{time.Thursday, 4},
		{time.Friday, 5},
		{time.Saturday, 6},
	}

	for _, tt := range tests {
		t.Run(tt.wd.String(), func(t *testing.T) {
			got := isoDow(tt.wd)
			if got != tt.want {
				t.Errorf("isoDow(%v) = %d, want %d", tt.wd, got, tt.want)
			}
		})
	}
}
