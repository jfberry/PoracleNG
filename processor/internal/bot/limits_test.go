package bot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func TestLimitExempt(t *testing.T) {
	cases := []struct {
		admin bool
		typ   string
		want  bool
	}{
		{false, "discord:user", false},
		{false, "telegram:user", false},
		{true, "discord:user", true},
		{false, "discord:channel", true},
		{false, "discord:thread", true},
		{false, "webhook", true},
		{false, "telegram:group", true},
	}
	for _, c := range cases {
		if got := LimitExempt(c.admin, c.typ); got != c.want {
			t.Errorf("LimitExempt(%v, %q) = %v, want %v", c.admin, c.typ, got, c.want)
		}
	}
}

func TestMaxDistanceFor(t *testing.T) {
	cfg := &config.Config{Tracking: config.TrackingConfig{MaxDistance: 500}}
	if got := MaxDistanceFor(cfg, false); got != 500 {
		t.Errorf("not exempt = %d, want 500", got)
	}
	if got := MaxDistanceFor(cfg, true); got != 0 {
		t.Errorf("exempt = %d, want 0 (no cap)", got)
	}
	if got := MaxDistanceFor(nil, false); got != 0 {
		t.Errorf("nil config = %d, want 0", got)
	}
}

func TestAreaLimitBlocks(t *testing.T) {
	cfg := &config.Config{Tracking: config.TrackingConfig{MaxAreas: 2}}
	cases := []struct {
		name     string
		cfg      *config.Config
		exempt   bool
		areas    []string
		previous []string
		want     bool
	}{
		{"no limit", &config.Config{}, false, []string{"a", "b", "c"}, nil, false},
		{"at limit", cfg, false, []string{"a", "b"}, nil, false},
		{"over limit", cfg, false, []string{"a", "b", "c"}, nil, true},
		{"exempt", cfg, true, []string{"a", "b", "c"}, nil, false},
		{"duplicates count once", cfg, false, []string{"a", "b", "B", "a"}, nil, false},
		{"spellings count once", cfg, false, []string{"new york", "new_york", "b"}, nil, false},
		{"resubmit previous", cfg, false, []string{"a", "b", "c"}, []string{"c", "b", "a"}, false},
		{"shrink previous", cfg, false, []string{"a", "b", "c"}, []string{"a", "b", "c", "d"}, false},
		{"cannot add to previous", cfg, false, []string{"a", "b", "c", "e"}, []string{"a", "b", "c", "d"}, true},
	}
	for _, c := range cases {
		if got := AreaLimitBlocks(c.cfg, c.exempt, c.areas, c.previous); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestOverrideAreaLimitBreached(t *testing.T) {
	cfg := &config.Config{Tracking: config.TrackingConfig{MaxAreas: 1}}
	lure := func(uid int64, lureID int, areas ...string) db.LureTrackingAPI {
		return db.LureTrackingAPI{UID: uid, ID: "u1", ProfileNo: 1, LureID: lureID, OverrideAreas: areas}
	}
	stored := []db.LureTrackingAPI{lure(7, 501, "a", "b")}
	breached := func(candidates ...db.LureTrackingAPI) bool {
		return OverrideAreaLimitBreached(cfg, false, stored, candidates, store.LureGetUID, store.LureSetUID)
	}

	if breached(lure(0, 502, "a")) {
		t.Error("a new rule within the limit was refused")
	}
	if !breached(lure(0, 502, "a", "b")) {
		t.Error("a new rule over the limit (copying a stored list) was accepted")
	}
	if breached(lure(0, 501, "a", "b")) {
		t.Error("resubmitting the stored over-limit rule was refused")
	}
	if !OverrideAreaLimitBreached(cfg, false, nil, []db.LureTrackingAPI{lure(0, 501, "a", "b")},
		store.LureGetUID, store.LureSetUID) {
		t.Error("with no stored rule the same list is a new rule and must be refused")
	}
	if OverrideAreaLimitBreached(cfg, true, nil, []db.LureTrackingAPI{lure(0, 502, "a", "b")},
		store.LureGetUID, store.LureSetUID) {
		t.Error("exempt write refused")
	}
}

func TestOverrideAreasOf(t *testing.T) {
	row := db.EggTrackingAPI{OverrideAreas: []string{"x"}}
	if got := OverrideAreasOf(&row); len(got) != 1 || got[0] != "x" {
		t.Errorf("pointer: got %v", got)
	}
	if got := OverrideAreasOf(row); len(got) != 1 {
		t.Errorf("value: got %v", got)
	}
	if got := OverrideAreasOf(42); got != nil {
		t.Errorf("non-struct: got %v", got)
	}
}
