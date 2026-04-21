package enrichment

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func TestConsolidateUsers(t *testing.T) {
	users := []webhook.MatchedUser{
		{ID: "u1", Name: "Alice", Language: "en", PVPRankingWorst: 10, PVPRankingLeague: 1500, PVPRankingCap: 50},
		{ID: "u1", Name: "Alice", Language: "en", PVPRankingWorst: 20, PVPRankingLeague: 2500, PVPRankingCap: 50},
		{ID: "u2", Name: "Bob", Language: "de", PVPRankingWorst: 4096}, // no PVP filter
		{ID: "u3", Name: "Carol", Language: "en", PVPRankingWorst: 5, PVPRankingLeague: 500, PVPRankingCap: 0},
	}

	result := consolidateUsers(users)

	if len(result) != 3 {
		t.Fatalf("expected 3 consolidated users, got %d", len(result))
	}

	// u1 should have 2 filters merged
	if result[0].ID != "u1" {
		t.Errorf("first user ID = %q, want u1", result[0].ID)
	}
	if len(result[0].Filters) != 2 {
		t.Errorf("u1 filters = %d, want 2", len(result[0].Filters))
	}
	if result[0].Filters[0].League != 1500 || result[0].Filters[1].League != 2500 {
		t.Errorf("u1 filter leagues = %d,%d, want 1500,2500", result[0].Filters[0].League, result[0].Filters[1].League)
	}

	// u2 should have 0 filters (PVPRankingWorst == 4096)
	if result[1].ID != "u2" {
		t.Errorf("second user ID = %q, want u2", result[1].ID)
	}
	if len(result[1].Filters) != 0 {
		t.Errorf("u2 filters = %d, want 0", len(result[1].Filters))
	}

	// u3 should have 1 filter
	if len(result[2].Filters) != 1 {
		t.Errorf("u3 filters = %d, want 1", len(result[2].Filters))
	}
}

// TestConsolidateUsersNonPVPMatch verifies that a non-PVP tracking rule (league
// unset) does NOT register as a PVP filter, even if PVPRankingWorst happens to
// be 0 (the Go zero value the DB may have stored because non-PVP INSERTs pass
// the struct's zero value rather than the column default of 4096). Before the
// fix this set userHasPvpTracks=true for every basic IV match.
func TestConsolidateUsersNonPVPMatch(t *testing.T) {
	cases := []struct {
		name string
		user webhook.MatchedUser
	}{
		{
			name: "basic IV match with league=0 worst=0 (legacy Go zero-value INSERTs)",
			user: webhook.MatchedUser{ID: "u1", PVPRankingLeague: 0, PVPRankingWorst: 0},
		},
		{
			name: "basic IV match with league=0 worst=4096 (DB default, JS-style)",
			user: webhook.MatchedUser{ID: "u1", PVPRankingLeague: 0, PVPRankingWorst: 4096},
		},
		{
			name: "PVP rule with explicit worst=4096 (== any rank, not a real filter)",
			user: webhook.MatchedUser{ID: "u1", PVPRankingLeague: 1500, PVPRankingWorst: 4096},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := consolidateUsers([]webhook.MatchedUser{tc.user})
			if len(got) != 1 {
				t.Fatalf("expected 1 consolidated user, got %d", len(got))
			}
			if len(got[0].Filters) != 0 {
				t.Errorf("expected 0 filters, got %d (Filters=%+v)", len(got[0].Filters), got[0].Filters)
			}
		})
	}
}

// TestConsolidateUsersMixedMatches covers the real multi-rule case: one user
// matched by a basic IV rule AND a PVP rule. Only the PVP rule should become
// a filter entry; userHasPvpTracks should be true because at least one real
// PVP rule matched.
func TestConsolidateUsersMixedMatches(t *testing.T) {
	users := []webhook.MatchedUser{
		// Basic IV match — league=0, worst=0 after Go zero-value INSERT
		{ID: "u1", PVPRankingLeague: 0, PVPRankingWorst: 0},
		// Real great-league PVP match
		{ID: "u1", PVPRankingLeague: 1500, PVPRankingWorst: 10, PVPRankingCap: 50},
	}
	got := consolidateUsers(users)
	if len(got) != 1 {
		t.Fatalf("expected 1 consolidated user, got %d", len(got))
	}
	if len(got[0].Filters) != 1 {
		t.Fatalf("expected 1 filter (only the PVP rule), got %d", len(got[0].Filters))
	}
	if got[0].Filters[0].League != 1500 || got[0].Filters[0].Worst != 10 {
		t.Errorf("filter mismatch: %+v", got[0].Filters[0])
	}
}

// TestConsolidateUsersTrackDistance verifies that the largest distance
// threshold across matching rules wins, so userDistanceTrack is true when
// any rule was distance-based.
func TestConsolidateUsersTrackDistance(t *testing.T) {
	cases := []struct {
		name       string
		users      []webhook.MatchedUser
		wantWinner int
	}{
		{
			name:       "single distance rule",
			users:      []webhook.MatchedUser{{ID: "u1", TrackDistance: 500}},
			wantWinner: 500,
		},
		{
			name:       "area rule only (no distance)",
			users:      []webhook.MatchedUser{{ID: "u1", TrackDistance: 0}},
			wantWinner: 0,
		},
		{
			name: "area + distance — distance wins",
			users: []webhook.MatchedUser{
				{ID: "u1", TrackDistance: 0},
				{ID: "u1", TrackDistance: 500},
			},
			wantWinner: 500,
		},
		{
			name: "two distance rules — larger wins",
			users: []webhook.MatchedUser{
				{ID: "u1", TrackDistance: 500},
				{ID: "u1", TrackDistance: 1000},
			},
			wantWinner: 1000,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := consolidateUsers(tc.users)
			if len(got) != 1 {
				t.Fatalf("expected 1 consolidated user, got %d", len(got))
			}
			if got[0].TrackDistance != tc.wantWinner {
				t.Errorf("TrackDistance = %d, want %d", got[0].TrackDistance, tc.wantWinner)
			}
		})
	}
}

// TestPokemonPerUserDistanceTrack verifies the userDistanceTrack flag is
// correctly set on the per-user enrichment for pokemon matches.
func TestPokemonPerUserDistanceTrack(t *testing.T) {
	enricher := &Enricher{
		PVPDisplay: &PVPDisplayConfig{MaxRank: 10, GreatMinCP: 1400},
	}
	matched := []webhook.MatchedUser{
		{ID: "u_dist", Language: "en", TrackDistance: 500},
		{ID: "u_area", Language: "en", TrackDistance: 0},
	}
	result := enricher.PokemonPerUser(map[string]map[string]any{"en": {}}, matched)
	if result["u_dist"]["userDistanceTrack"] != true {
		t.Errorf("u_dist userDistanceTrack = %v, want true", result["u_dist"]["userDistanceTrack"])
	}
	if result["u_dist"]["userTrackDistance"] != 500 {
		t.Errorf("u_dist userTrackDistance = %v, want 500", result["u_dist"]["userTrackDistance"])
	}
	if result["u_area"]["userDistanceTrack"] != false {
		t.Errorf("u_area userDistanceTrack = %v, want false", result["u_area"]["userDistanceTrack"])
	}
	if result["u_area"]["userTrackDistance"] != 0 {
		t.Errorf("u_area userTrackDistance = %v, want 0", result["u_area"]["userTrackDistance"])
	}
}

func TestCreatePvpDisplay(t *testing.T) {
	enricher := &Enricher{
		PVPDisplay: &PVPDisplayConfig{
			MaxRank:       10,
			GreatMinCP:    1400,
			UltraMinCP:    2350,
			LittleMinCP:   450,
			FilterByTrack: false,
		},
	}

	entries := []map[string]any{
		{"rank": 1, "cp": 1498, "pokemon": 101, "form": 0, "fullName": "Electrode", "cap": 50, "capped": true},
		{"rank": 5, "cp": 1495, "pokemon": 100, "form": 0, "fullName": "Voltorb", "cap": 50, "capped": true},
		{"rank": 15, "cp": 1490, "pokemon": 26, "form": 0, "fullName": "Raichu", "cap": 50, "capped": true},   // exceeds maxRank
		{"rank": 3, "cp": 1300, "pokemon": 25, "form": 0, "fullName": "Pikachu", "cap": 50, "capped": true},    // below minCP
	}

	// No filters — all entries with rank <= 10 and cp >= 1400 should pass
	result := enricher.createPvpDisplay(1500, entries, 1400, nil)

	if len(result) != 2 {
		t.Fatalf("expected 2 display entries (rank 1 + rank 5), got %d", len(result))
	}
	if toInt(result[0]["rank"]) != 1 {
		t.Errorf("first entry rank = %d, want 1", toInt(result[0]["rank"]))
	}
	if toInt(result[1]["rank"]) != 5 {
		t.Errorf("second entry rank = %d, want 5", toInt(result[1]["rank"]))
	}
	// Without filters, passesFilter should be true
	if pf, _ := result[0]["passesFilter"].(bool); !pf {
		t.Error("entry without filters should have passesFilter=true")
	}
}

func TestCreatePvpDisplayWithFilters(t *testing.T) {
	enricher := &Enricher{
		PVPDisplay: &PVPDisplayConfig{
			MaxRank:       10,
			GreatMinCP:    1400,
			FilterByTrack: true, // only show entries that match user's tracking
		},
	}

	entries := []map[string]any{
		{"rank": 1, "cp": 1498, "pokemon": 101, "form": 0, "fullName": "Electrode", "cap": 50, "capped": true},
		{"rank": 5, "cp": 1495, "pokemon": 100, "form": 0, "fullName": "Voltorb", "cap": 50, "capped": true},
	}

	// User tracks great league rank <= 3 — only rank 1 passes
	filters := []pvpFilter{
		{League: 1500, Worst: 3, Cap: 0},
	}

	result := enricher.createPvpDisplay(1500, entries, 1400, filters)

	if len(result) != 1 {
		t.Fatalf("expected 1 display entry (only rank 1 passes filter), got %d", len(result))
	}
	if toInt(result[0]["rank"]) != 1 {
		t.Errorf("entry rank = %d, want 1", toInt(result[0]["rank"]))
	}
	if mt, _ := result[0]["matchesUserTrack"].(bool); !mt {
		t.Error("entry should have matchesUserTrack=true")
	}
}

func TestCreatePvpDisplayFilterByTrackDisabled(t *testing.T) {
	enricher := &Enricher{
		PVPDisplay: &PVPDisplayConfig{
			MaxRank:       10,
			GreatMinCP:    1400,
			FilterByTrack: false, // show all, just mark which match
		},
	}

	entries := []map[string]any{
		{"rank": 1, "cp": 1498, "pokemon": 101, "form": 0, "fullName": "Electrode", "cap": 50, "capped": true},
		{"rank": 5, "cp": 1495, "pokemon": 100, "form": 0, "fullName": "Voltorb", "cap": 50, "capped": true},
	}

	filters := []pvpFilter{
		{League: 1500, Worst: 3, Cap: 0},
	}

	result := enricher.createPvpDisplay(1500, entries, 1400, filters)

	// Both should appear (filterByTrack=false) but only rank 1 matches user track
	if len(result) != 2 {
		t.Fatalf("expected 2 display entries (filterByTrack disabled), got %d", len(result))
	}
	if mt, _ := result[0]["matchesUserTrack"].(bool); !mt {
		t.Error("rank 1 should matchesUserTrack=true")
	}
	if mt, _ := result[1]["matchesUserTrack"].(bool); mt {
		t.Error("rank 5 should matchesUserTrack=false")
	}
}

func TestCreatePvpDisplayCapMatching(t *testing.T) {
	enricher := &Enricher{
		PVPDisplay: &PVPDisplayConfig{
			MaxRank:       100,
			GreatMinCP:    1400,
			FilterByTrack: true,
		},
	}

	entries := []map[string]any{
		{"rank": 1, "cp": 1498, "pokemon": 101, "form": 0, "fullName": "Electrode", "cap": 50, "capped": true},
		{"rank": 3, "cp": 1495, "pokemon": 100, "form": 0, "fullName": "Voltorb", "cap": 40, "capped": false},
	}

	// Filter: cap=50 should match capped entries (any cap >= 50) and exact cap=50
	filters := []pvpFilter{
		{League: 1500, Worst: 100, Cap: 50},
	}

	result := enricher.createPvpDisplay(1500, entries, 1400, filters)

	// rank 1 (capped=true): cap filter matches capped entries
	// rank 3 (capped=false, cap=40): cap=50 != cap=40, doesn't match
	if len(result) != 1 {
		t.Fatalf("expected 1 entry (only capped matches cap=50), got %d", len(result))
	}
}

func TestCreatePvpDisplayLeagueFilter(t *testing.T) {
	enricher := &Enricher{
		PVPDisplay: &PVPDisplayConfig{
			MaxRank:       100,
			GreatMinCP:    1400,
			UltraMinCP:    2350,
			FilterByTrack: true,
		},
	}

	entries := []map[string]any{
		{"rank": 1, "cp": 1498, "pokemon": 101, "form": 0, "fullName": "Electrode", "cap": 50, "capped": true},
	}

	// Filter for ultra (2500) — shouldn't match great (1500) entries
	filters := []pvpFilter{
		{League: 2500, Worst: 100, Cap: 0},
	}

	result := enricher.createPvpDisplay(1500, entries, 1400, filters)
	if result != nil {
		t.Errorf("expected nil (ultra filter shouldn't match great league), got %d entries", len(result))
	}

	// Filter with league=0 matches any league
	filters = []pvpFilter{
		{League: 0, Worst: 100, Cap: 0},
	}

	result = enricher.createPvpDisplay(1500, entries, 1400, filters)
	if len(result) != 1 {
		t.Fatalf("expected 1 (league=0 matches any), got %d", len(result))
	}
}

func TestCreatePvpDisplayNilEntries(t *testing.T) {
	enricher := &Enricher{
		PVPDisplay: &PVPDisplayConfig{MaxRank: 10, GreatMinCP: 1400},
	}

	result := enricher.createPvpDisplay(1500, nil, 1400, nil)
	if result != nil {
		t.Error("expected nil for nil entries")
	}

	result = enricher.createPvpDisplay(1500, "not a slice", 1400, nil)
	if result != nil {
		t.Error("expected nil for wrong type")
	}
}

func TestCalculateBestInfo(t *testing.T) {
	ranks := []map[string]any{
		{"rank": 5, "fullName": "Voltorb"},
		{"rank": 1, "fullName": "Electrode"},
		{"rank": 1, "fullName": "Electrode Hisuian"},
		{"rank": 3, "fullName": "Pikachu"},
	}

	best := calculateBestInfo(ranks)
	if best == nil {
		t.Fatal("expected non-nil best info")
	}
	if toInt(best["rank"]) != 1 {
		t.Errorf("best rank = %d, want 1", toInt(best["rank"]))
	}
	bestList, ok := best["list"].([]map[string]any)
	if !ok || len(bestList) != 2 {
		t.Fatalf("best list length = %d, want 2", len(bestList))
	}
	name, _ := best["name"].(string)
	if name != "Electrode, Electrode Hisuian" {
		t.Errorf("best name = %q, want %q", name, "Electrode, Electrode Hisuian")
	}
}

func TestCalculateBestInfoNil(t *testing.T) {
	if calculateBestInfo(nil) != nil {
		t.Error("expected nil for nil input")
	}
	if calculateBestInfo([]map[string]any{}) != nil {
		t.Error("expected nil for empty input")
	}
}

func TestPokemonPerUser(t *testing.T) {
	enricher := &Enricher{
		PVPDisplay: &PVPDisplayConfig{
			MaxRank:       10,
			GreatMinCP:    1400,
			UltraMinCP:    2350,
			LittleMinCP:   450,
			FilterByTrack: false,
		},
	}

	langEnrichments := map[string]map[string]any{
		"en": {
			"pvpEnriched_great_league": []map[string]any{
				{"rank": 1, "cp": 1498, "pokemon": 101, "form": 0, "fullName": "Electrode", "cap": 50, "capped": true},
				{"rank": 4, "cp": 1137, "pokemon": 100, "form": 0, "fullName": "Voltorb", "cap": 50, "capped": true},
			},
			"pvpEnriched_ultra_league": []map[string]any{
				{"rank": 1, "cp": 2366, "pokemon": 101, "form": 0, "fullName": "Electrode", "cap": 50, "capped": true},
			},
		},
		"de": {
			"pvpEnriched_great_league": []map[string]any{
				{"rank": 1, "cp": 1498, "pokemon": 101, "form": 0, "fullName": "Lektrobal", "cap": 50, "capped": true},
			},
		},
	}

	matchedUsers := []webhook.MatchedUser{
		{ID: "u1", Name: "Alice", Language: "en", Distance: 500, Bearing: 45, CardinalDirection: "northeast",
			PVPRankingWorst: 5, PVPRankingLeague: 1500, PVPRankingCap: 0},
		{ID: "u2", Name: "Hans", Language: "de", Distance: 1200, Bearing: 180, CardinalDirection: "south",
			PVPRankingWorst: 4096},
	}

	result := enricher.PokemonPerUser(langEnrichments, matchedUsers)

	if len(result) != 2 {
		t.Fatalf("expected 2 per-user entries, got %d", len(result))
	}

	// Check u1 (English, has PVP filter)
	u1 := result["u1"]
	if u1 == nil {
		t.Fatal("u1 not found in result")
	}
	if u1["_lang"] != "en" {
		t.Errorf("u1 _lang = %q, want en", u1["_lang"])
	}
	if u1["distance"] != 500 {
		t.Errorf("u1 distance = %v, want 500", u1["distance"])
	}
	if u1["bearing"] != 45 {
		t.Errorf("u1 bearing = %v, want 45", u1["bearing"])
	}
	if u1["bearingEmojiKey"] != "northeast" {
		t.Errorf("u1 bearingEmojiKey = %v, want northeast", u1["bearingEmojiKey"])
	}
	if u1["pvpUserRanking"] != 5 {
		t.Errorf("u1 pvpUserRanking = %v, want 5", u1["pvpUserRanking"])
	}
	if u1["userHasPvpTracks"] != true {
		t.Error("u1 userHasPvpTracks should be true")
	}
	if u1["pvpAvailable"] != true {
		t.Error("u1 pvpAvailable should be true")
	}

	// u1 great PVP: rank 1 passes (cp 1498 >= 1400), rank 4 filtered out (cp 1137 < 1400)
	pvpGreat, ok := u1["pvpGreat"].([]map[string]any)
	if !ok || len(pvpGreat) != 1 {
		t.Fatalf("u1 pvpGreat length = %d, want 1 (Voltorb cp below minCP)", len(pvpGreat))
	}

	// u1 great best should be rank 1
	pvpGreatBest, ok := u1["pvpGreatBest"].(map[string]any)
	if !ok {
		t.Fatal("u1 pvpGreatBest is nil")
	}
	if toInt(pvpGreatBest["rank"]) != 1 {
		t.Errorf("u1 pvpGreatBest rank = %d, want 1", toInt(pvpGreatBest["rank"]))
	}

	// u1 ultra PVP
	pvpUltra, ok := u1["pvpUltra"].([]map[string]any)
	if !ok || len(pvpUltra) != 1 {
		t.Fatalf("u1 pvpUltra length = %d, want 1", len(pvpUltra))
	}

	// Check u2 (German, no PVP filter)
	u2 := result["u2"]
	if u2 == nil {
		t.Fatal("u2 not found in result")
	}
	if u2["_lang"] != "de" {
		t.Errorf("u2 _lang = %q, want de", u2["_lang"])
	}
	if u2["pvpUserRanking"] != 0 {
		t.Errorf("u2 pvpUserRanking = %v, want 0 (was 4096)", u2["pvpUserRanking"])
	}
	if u2["userHasPvpTracks"] != false {
		t.Error("u2 userHasPvpTracks should be false")
	}

	// u2 great PVP: should use German language enrichment
	u2Great, ok := u2["pvpGreat"].([]map[string]any)
	if !ok || len(u2Great) != 1 {
		t.Fatalf("u2 pvpGreat length = %d, want 1", len(u2Great))
	}
	if name, _ := u2Great[0]["fullName"].(string); name != "Lektrobal" {
		t.Errorf("u2 pvpGreat[0].fullName = %q, want Lektrobal", name)
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		input any
		want  int
	}{
		{42, 42},
		{int64(100), 100},
		{float64(3.14), 3},
		{nil, 0},
		{"hello", 0},
	}
	for _, tt := range tests {
		got := toInt(tt.input)
		if got != tt.want {
			t.Errorf("toInt(%v) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
