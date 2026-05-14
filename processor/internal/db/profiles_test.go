package db

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestActiveHourEntry_FiresSingle covers the single-fire shape: one
// pair regardless of Step / End being zero.
func TestActiveHourEntry_FiresSingle(t *testing.T) {
	e := ActiveHourEntry{Day: 1, Hours: 7, Mins: 30}
	got := e.Fires()
	want := [][2]int{{7, 30}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Fires() = %v, want %v", got, want)
	}
	if e.IsRange() {
		t.Errorf("IsRange() = true, want false (Step=0)")
	}
}

// TestActiveHourEntry_FiresRange covers the range+step shape: start,
// start+step*h, …, end-inclusive.
func TestActiveHourEntry_FiresRange(t *testing.T) {
	cases := []struct {
		name string
		in   ActiveHourEntry
		want [][2]int
	}{
		{
			name: "9-17/2 → 9, 11, 13, 15, 17",
			in:   ActiveHourEntry{Hours: 9, Mins: 0, EndHours: 17, EndMins: 0, Step: 2},
			want: [][2]int{{9, 0}, {11, 0}, {13, 0}, {15, 0}, {17, 0}},
		},
		{
			name: "9:30-17/2 → 9:30, 11:30, 13:30, 15:30 (17:30 past end)",
			in:   ActiveHourEntry{Hours: 9, Mins: 30, EndHours: 17, EndMins: 0, Step: 2},
			want: [][2]int{{9, 30}, {11, 30}, {13, 30}, {15, 30}},
		},
		{
			name: "9-17/1 (hourly) → 9..17",
			in:   ActiveHourEntry{Hours: 9, EndHours: 17, Step: 1},
			want: [][2]int{{9, 0}, {10, 0}, {11, 0}, {12, 0}, {13, 0}, {14, 0}, {15, 0}, {16, 0}, {17, 0}},
		},
		{
			name: "9-17/3 → 9, 12, 15 (18 past end)",
			in:   ActiveHourEntry{Hours: 9, EndHours: 17, Step: 3},
			want: [][2]int{{9, 0}, {12, 0}, {15, 0}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.in.Fires()
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Fires() = %v, want %v", got, c.want)
			}
			if !c.in.IsRange() {
				t.Error("IsRange() = false, want true")
			}
		})
	}
}

// TestActiveHourEntry_UnmarshalLegacyAndNew confirms the JSON
// unmarshaller accepts both the legacy string-encoded fields (which
// old data on disk uses) and the new int-encoded fields.
func TestActiveHourEntry_UnmarshalLegacyAndNew(t *testing.T) {
	cases := []struct {
		name string
		json string
		want ActiveHourEntry
	}{
		{
			name: "legacy strings",
			json: `{"day":1,"hours":"07","mins":"30"}`,
			want: ActiveHourEntry{Day: 1, Hours: 7, Mins: 30},
		},
		{
			name: "new ints",
			json: `{"day":1,"hours":7,"mins":30}`,
			want: ActiveHourEntry{Day: 1, Hours: 7, Mins: 30},
		},
		{
			name: "range form",
			json: `{"day":1,"hours":9,"mins":0,"end_hours":17,"end_mins":0,"step":2}`,
			want: ActiveHourEntry{Day: 1, Hours: 9, Mins: 0, EndHours: 17, EndMins: 0, Step: 2},
		},
		{
			name: "range with string end + step",
			json: `{"day":1,"hours":"09","mins":"30","end_hours":"17","end_mins":"00","step":"2"}`,
			want: ActiveHourEntry{Day: 1, Hours: 9, Mins: 30, EndHours: 17, EndMins: 0, Step: 2},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got ActiveHourEntry
			if err := json.Unmarshal([]byte(c.json), &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
