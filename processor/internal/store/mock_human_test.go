package store

import (
	"slices"
	"testing"
)

// Compile-time check that MockHumanStore implements HumanStore.
var _ HumanStore = (*MockHumanStore)(nil)

func TestMockHumanStore_CreateAndGet(t *testing.T) {
	m := NewMockHumanStore()

	h := &Human{ID: "123", Type: "discord:user", Name: "Alice", Enabled: true}
	if err := m.Create(h); err != nil {
		t.Fatal(err)
	}

	got, err := m.Get("123")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected human, got nil")
	}
	if got.Name != "Alice" {
		t.Errorf("expected Alice, got %s", got.Name)
	}

	// Get returns a copy, not a reference to the stored record.
	got.Name = "Bob"
	got2, _ := m.Get("123")
	if got2.Name != "Alice" {
		t.Error("mock should return copies, not references")
	}
}

func TestMockHumanStore_GetNotFound(t *testing.T) {
	m := NewMockHumanStore()
	got, err := m.Get("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent human")
	}
}

func TestMockHumanStore_Delete(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123", Name: "Alice"})
	m.SeedProfile(Profile{ID: "123", ProfileNo: 1})

	if err := m.Delete("123"); err != nil {
		t.Fatal(err)
	}

	got, _ := m.Get("123")
	if got != nil {
		t.Error("expected nil after delete")
	}

	profiles, _ := m.GetProfiles("123")
	if len(profiles) != 0 {
		t.Error("expected profiles deleted")
	}
}

func TestMockHumanStore_SetEnabled(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123", Enabled: false})

	m.SetEnabled("123", true)
	got, _ := m.Get("123")
	if !got.Enabled {
		t.Error("expected enabled=true")
	}
}

func TestMockHumanStore_SetLocation(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123", CurrentProfileNo: 1})

	m.SetLocation("123", 1, 51.5, -0.1)
	got, _ := m.Get("123")
	if got.Latitude != 51.5 || got.Longitude != -0.1 {
		t.Errorf("expected (51.5, -0.1), got (%f, %f)", got.Latitude, got.Longitude)
	}
}

func TestMockHumanStore_SetArea(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123", CurrentProfileNo: 1})

	m.SetArea("123", 1, []string{"downtown", "park"})
	got, _ := m.Get("123")
	if len(got.Area) != 2 {
		t.Fatalf("expected 2 areas, got %d", len(got.Area))
	}
}

func TestMockHumanStore_SetCommunity(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123"})

	m.SetCommunity("123", []string{"TeamA"}, []string{"zone1"})
	got, _ := m.Get("123")
	if len(got.CommunityMembership) != 1 || got.CommunityMembership[0] != "TeamA" {
		t.Errorf("expected TeamA community, got %v", got.CommunityMembership)
	}
	if len(got.AreaRestriction) != 1 || got.AreaRestriction[0] != "zone1" {
		t.Errorf("expected zone1 restriction, got %v", got.AreaRestriction)
	}
}

func TestMockHumanStore_ListByType(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "1", Type: "discord:user"})
	m.AddHuman(&Human{ID: "2", Type: "discord:user"})
	m.AddHuman(&Human{ID: "3", Type: "telegram:user"})

	list, _ := m.ListByType("discord:user")
	if len(list) != 2 {
		t.Fatalf("expected 2 discord users, got %d", len(list))
	}
}

func TestMockHumanStore_LookupWebhookByName(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "http://hook.url", Type: "webhook", Name: "MyHook"})

	id, _ := m.LookupWebhookByName("MyHook")
	if id != "http://hook.url" {
		t.Errorf("expected webhook URL, got %s", id)
	}

	id, _ = m.LookupWebhookByName("NonExistent")
	if id != "" {
		t.Error("expected empty for nonexistent webhook")
	}
}

func TestMockHumanStore_SwitchProfile(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123", CurrentProfileNo: 1, Area: []string{"a"}})
	m.SeedProfile(Profile{ID: "123", ProfileNo: 1, Area: []string{"a"}})
	m.SeedProfile(Profile{ID: "123", ProfileNo: 2, Area: []string{"b", "c"}, Latitude: 10, Longitude: 20})

	found, _ := m.SwitchProfile("123", 2)
	if !found {
		t.Fatal("expected profile found")
	}
	got, _ := m.Get("123")
	if got.CurrentProfileNo != 2 {
		t.Errorf("expected profile 2, got %d", got.CurrentProfileNo)
	}
	if len(got.Area) != 2 {
		t.Errorf("expected 2 areas from profile 2, got %d", len(got.Area))
	}

	found, _ = m.SwitchProfile("123", 99)
	if found {
		t.Error("expected not found for nonexistent profile")
	}
}

func TestMockHumanStore_CallTracking(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123"})

	m.Get("123")
	m.SetEnabled("123", true)
	m.SetLanguage("123", "de")

	if len(m.Calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(m.Calls))
	}
	if m.Calls[0] != "Get" || m.Calls[1] != "SetEnabled" || m.Calls[2] != "SetLanguage" {
		t.Errorf("unexpected call sequence: %v", m.Calls)
	}
}

// Profile 0 does not exist: every human starts on the default profile 1, and
// rules saved to a profile other than the active one never match.
func TestMockHumanStore_CreateDefaultsProfileNo(t *testing.T) {
	m := NewMockHumanStore()

	if err := m.Create(&Human{ID: "unset", Type: "discord:user"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Create(&Human{ID: "explicit", Type: "discord:user", CurrentProfileNo: 3}); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]int{"unset": 1, "explicit": 3} {
		got, err := m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.CurrentProfileNo != want {
			t.Errorf("%s: CurrentProfileNo = %d, want %d", id, got.CurrentProfileNo, want)
		}
	}
}

func TestMockHumanStore_StripUserAreas(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "u1", CurrentProfileNo: 1, Area: []string{"alpha", "beta", "gent_centrum"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha", "beta", "gent_centrum"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 2, Area: []string{"beta", "delta"}})

	removed, err := m.StripUserAreas("u1", map[string]bool{"beta": true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(removed, []string{"beta"}) {
		t.Errorf("removed = %v, want [beta]", removed)
	}
	h, _ := m.Get("u1")
	if !slices.Equal(h.Area, []string{"alpha", "gent_centrum"}) {
		t.Errorf("humans.area = %v", h.Area)
	}
	ps, _ := m.GetProfiles("u1")
	byNo := map[int][]string{}
	for _, p := range ps {
		byNo[p.ProfileNo] = p.Area
	}
	if !slices.Equal(byNo[1], []string{"alpha", "gent_centrum"}) || !slices.Equal(byNo[2], []string{"delta"}) {
		t.Errorf("profiles = %v, want 1:[alpha gent_centrum] 2:[delta]", byNo)
	}
	// A name absent from denied survives even though it is not a known fence.
	m.AddHuman(&Human{ID: "u2", Area: []string{"ghost", "beta"}})
	if _, err := m.StripUserAreas("u2", map[string]bool{"beta": true}); err != nil {
		t.Fatal(err)
	}
	if h2, _ := m.Get("u2"); !slices.Equal(h2.Area, []string{"ghost"}) {
		t.Errorf("u2 humans.area = %v, want [ghost]", h2.Area)
	}
	// Idempotent: nothing left to remove.
	if removed, _ := m.StripUserAreas("u1", map[string]bool{"beta": true}); len(removed) != 0 {
		t.Errorf("second prune removed %v, want nothing", removed)
	}
	// Everything pruned leaves an empty non-nil list, like SQL's [].
	if _, err := m.StripUserAreas("u1", map[string]bool{"alpha": true, "beta": true, "gent_centrum": true, "delta": true}); err != nil {
		t.Fatal(err)
	}
	h, _ = m.Get("u1")
	if h.Area == nil || len(h.Area) != 0 {
		t.Errorf("fully pruned humans.area = %#v, want empty non-nil", h.Area)
	}
}

func TestMockHumanStore_StripUserAreas_NoHuman(t *testing.T) {
	m := NewMockHumanStore()
	m.SeedProfile(Profile{ID: "ghost", ProfileNo: 1, Area: []string{"beta", "alpha"}})

	removed, err := m.StripUserAreas("ghost", map[string]bool{"beta": true})
	if err != nil {
		t.Fatal(err)
	}
	if removed != nil {
		t.Errorf("removed = %v, want nil", removed)
	}
	ps, _ := m.GetProfiles("ghost")
	if len(ps) != 1 || !slices.Equal(ps[0].Area, []string{"beta", "alpha"}) {
		t.Errorf("profiles = %+v, want unchanged", ps)
	}
}

func TestMockHumanStore_RemoveAreaFromProfiles(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "u1", CurrentProfileNo: 1, Area: []string{"alpha", "gent_centrum"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha", "gent_centrum"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 2, Area: []string{"gent_centrum", "beta"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 3, Area: []string{"beta"}})

	// Different spelling of the same fence.
	got, _, err := m.RemoveAreaFromProfiles("u1", "Gent Centrum", []int{1, 2, 3}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int{1, 2}) {
		t.Errorf("removedFrom = %v, want [1 2]", got)
	}
	h, _ := m.Get("u1")
	if !slices.Equal(h.Area, []string{"alpha"}) {
		t.Errorf("humans.area = %v, want [alpha]", h.Area)
	}
	ps, _ := m.GetProfiles("u1")
	want := map[int][]string{1: {"alpha"}, 2: {"beta"}, 3: {"beta"}}
	for _, p := range ps {
		if !slices.Equal(p.Area, want[p.ProfileNo]) {
			t.Errorf("profile %d = %v, want %v", p.ProfileNo, p.Area, want[p.ProfileNo])
		}
	}
}

// Targeting a non-current profile leaves the live humans.area alone.
func TestMockHumanStore_RemoveAreaFromProfiles_InactiveLeavesLive(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "u1", CurrentProfileNo: 1, Area: []string{"alpha"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 2, Area: []string{"alpha"}})
	if _, _, err := m.RemoveAreaFromProfiles("u1", "alpha", []int{2}, false); err != nil {
		t.Fatal(err)
	}
	if h, _ := m.Get("u1"); !slices.Equal(h.Area, []string{"alpha"}) {
		t.Errorf("live humans.area changed: %v", h.Area)
	}
}

func TestMockHumanStore_RemoveAreaFromProfiles_DryRun(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "u1", CurrentProfileNo: 1, Area: []string{"alpha"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha"}})
	got, ch, _ := m.RemoveAreaFromProfiles("u1", "alpha", []int{1}, true)
	if !ch {
		t.Error("dry run must report changed")
	}
	if !slices.Equal(got, []int{1}) {
		t.Errorf("dry run removedFrom = %v, want [1]", got)
	}
	if h, _ := m.Get("u1"); !slices.Equal(h.Area, []string{"alpha"}) {
		t.Errorf("dry run wrote humans.area: %v", h.Area)
	}
	if ps, _ := m.GetProfiles("u1"); !slices.Equal(ps[0].Area, []string{"alpha"}) {
		t.Errorf("dry run wrote profile: %v", ps[0].Area)
	}
}

func TestEditAreaList(t *testing.T) {
	// Removal keeps unrelated legacy spellings exactly as stored.
	out, changed := editAreaList([]string{"Gent Centrum", "other_one", "x"}, "gent_centrum", "")
	if !changed || !slices.Equal(out, []string{"other_one", "x"}) {
		t.Errorf("remove: %v %v", out, changed)
	}
	// Rename collapses duplicates by key.
	out, changed = editAreaList([]string{"a b", "c", "a_b"}, "a_b", "a_b2")
	if !changed || !slices.Equal(out, []string{"a_b2", "c"}) {
		t.Errorf("rename: %v %v", out, changed)
	}
	if _, changed = editAreaList([]string{"c"}, "zzz", ""); changed {
		t.Error("no match must be unchanged")
	}
}

func TestMockHumanStore_RenameAreaInProfiles(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "u1", CurrentProfileNo: 1, Area: []string{"private", "gent centrum"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 1, Area: []string{"private", "gent centrum"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 2, Area: []string{"alpha", "private"}})
	got, ch, err := m.RenameAreaInProfiles("u1", "Private", "gent_centrum", []int{1, 2}, false)
	if err != nil || !ch || !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("got=%v ch=%v err=%v", got, ch, err)
	}
	h, _ := m.Get("u1")
	if !slices.Equal(h.Area, []string{"gent_centrum"}) {
		t.Errorf("humans.area = %v", h.Area)
	}
	ps, _ := m.GetProfiles("u1")
	if !slices.Equal(ps[0].Area, []string{"gent_centrum"}) || !slices.Equal(ps[1].Area, []string{"alpha", "gent_centrum"}) {
		t.Errorf("profiles = %v / %v", ps[0].Area, ps[1].Area)
	}
}

// A current profile with no profiles row (legacy human) still has its
// humans.area written, so it is reported, in order and once, under dry_run too.
func TestMockHumanStore_AreaOps_CurrentProfileWithoutRowReported(t *testing.T) {
	for _, dry := range []bool{true, false} {
		m := NewMockHumanStore()
		m.AddHuman(&Human{ID: "u1", CurrentProfileNo: 2, Area: []string{"alpha", "private"}})
		m.SeedProfile(Profile{ID: "u1", ProfileNo: 3, Area: []string{"private"}})
		got, ch, err := m.RemoveAreaFromProfiles("u1", "private", []int{1, 2, 3}, dry)
		if err != nil || !ch || !slices.Equal(got, []int{2, 3}) {
			t.Errorf("dry=%v remove: got=%v ch=%v err=%v, want [2 3]", dry, got, ch, err)
		}

		m = NewMockHumanStore()
		m.AddHuman(&Human{ID: "u1", CurrentProfileNo: 2, Area: []string{"alpha", "private"}})
		m.SeedProfile(Profile{ID: "u1", ProfileNo: 3, Area: []string{"private"}})
		got, ch, err = m.RenameAreaInProfiles("u1", "private", "beta", []int{1, 2, 3}, dry)
		if err != nil || !ch || !slices.Equal(got, []int{2, 3}) {
			t.Errorf("dry=%v rename: got=%v ch=%v err=%v, want [2 3]", dry, got, ch, err)
		}
	}
	// A current profile whose row also changed is reported once.
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "u1", CurrentProfileNo: 1, Area: []string{"private"}})
	m.SeedProfile(Profile{ID: "u1", ProfileNo: 1, Area: []string{"private"}})
	if got, _, _ := m.RemoveAreaFromProfiles("u1", "private", []int{1}, false); !slices.Equal(got, []int{1}) {
		t.Errorf("remove with row: got=%v, want [1]", got)
	}
}
