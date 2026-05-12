package commands

import (
	"reflect"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// allDays is the standard day-prefix map used by every test. The bot
// builds this from translations at runtime; tests skip i18n.
var allDays = map[string][]int{
	"mon": {1}, "tue": {2}, "wed": {3}, "thu": {4},
	"fri": {5}, "sat": {6}, "sun": {7},
	"weekday": {1, 2, 3, 4, 5},
	"weekend": {6, 7},
	"every":   {1, 2, 3, 4, 5, 6, 7},
}

func TestParseSettime_SingleFire(t *testing.T) {
	cases := []struct {
		in   string
		want []db.ActiveHourEntry
	}{
		{
			in:   "mon7:30",
			want: []db.ActiveHourEntry{{Day: 1, Hours: 7, Mins: 30}},
		},
		{
			in:   "mon:7:30",
			want: []db.ActiveHourEntry{{Day: 1, Hours: 7, Mins: 30}},
		},
		{
			in:   "mon07:30",
			want: []db.ActiveHourEntry{{Day: 1, Hours: 7, Mins: 30}},
		},
		{
			in:   "every8",
			want: []db.ActiveHourEntry{{Day: 1, Hours: 8}, {Day: 2, Hours: 8}, {Day: 3, Hours: 8}, {Day: 4, Hours: 8}, {Day: 5, Hours: 8}, {Day: 6, Hours: 8}, {Day: 7, Hours: 8}},
		},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseSettimeArg(c.in, allDays)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestParseSettime_Range(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []db.ActiveHourEntry
	}{
		{
			name: "weekday:9-17/2 → 5 days, range 9-17 step 2",
			in:   "weekday:9-17/2",
			want: []db.ActiveHourEntry{
				{Day: 1, Hours: 9, EndHours: 17, Step: 2},
				{Day: 2, Hours: 9, EndHours: 17, Step: 2},
				{Day: 3, Hours: 9, EndHours: 17, Step: 2},
				{Day: 4, Hours: 9, EndHours: 17, Step: 2},
				{Day: 5, Hours: 9, EndHours: 17, Step: 2},
			},
		},
		{
			name: "no prefix → every day",
			in:   "9-17/2",
			want: []db.ActiveHourEntry{
				{Day: 1, Hours: 9, EndHours: 17, Step: 2},
				{Day: 2, Hours: 9, EndHours: 17, Step: 2},
				{Day: 3, Hours: 9, EndHours: 17, Step: 2},
				{Day: 4, Hours: 9, EndHours: 17, Step: 2},
				{Day: 5, Hours: 9, EndHours: 17, Step: 2},
				{Day: 6, Hours: 9, EndHours: 17, Step: 2},
				{Day: 7, Hours: 9, EndHours: 17, Step: 2},
			},
		},
		{
			name: "no step → default 1 (hourly)",
			in:   "mon:9-12",
			want: []db.ActiveHourEntry{{Day: 1, Hours: 9, EndHours: 12, Step: 1}},
		},
		{
			name: "explicit minutes",
			in:   "mon:09:30-17:00/2",
			want: []db.ActiveHourEntry{{Day: 1, Hours: 9, Mins: 30, EndHours: 17, EndMins: 0, Step: 2}},
		},
		{
			name: "no colon between prefix and range",
			in:   "mon9-17/3",
			want: []db.ActiveHourEntry{{Day: 1, Hours: 9, EndHours: 17, Step: 3}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseSettimeArg(c.in, allDays)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestParseSettime_RangeErrors(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		errIs  string // substring expected in error
	}{
		{"cross-midnight rejected", "9-5/2", "cross-midnight"},
		{"equal start/end rejected", "9-9/2", "after start"},
		{"step zero rejected", "9-17/0", "1..23"},
		{"step too large rejected", "9-17/24", "1..23"},
		{"hours out of range", "9-25/2", "out of range"},
		{"minutes out of range", "9:60-17/2", "out of range"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseSettimeArg(c.in, allDays)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.errIs)
			}
			if !strings.Contains(err.Error(), c.errIs) {
				t.Errorf("error %q does not contain %q", err.Error(), c.errIs)
			}
		})
	}
}

func TestParseSettime_UnknownTokenReturnsNilNoError(t *testing.T) {
	// Junk that matches neither form should return (nil, nil) so the
	// caller silently skips it (preserves the existing settime UX
	// where extra junk is ignored).
	got, err := ParseSettimeArg("not-a-time-token", allDays)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil entries for unknown token, got %+v", got)
	}
}
