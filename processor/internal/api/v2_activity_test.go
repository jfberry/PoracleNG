package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// activityBody is the decoded shape of GET /api/v2/activity.
type activityBody struct {
	WindowSecs      int   `json:"window_secs"`
	RaidBosses      []int `json:"raid_bosses"`
	MaxBattleBosses []int `json:"max_battle_bosses"`
	QuestPokemon    []int `json:"quest_pokemon"`
	QuestItems      []int `json:"quest_items"`
	QuestCandy      []int `json:"quest_candy"`
	QuestMega       []int `json:"quest_mega"`
	QuestXL         []int `json:"quest_xl"`
	InvasionGrunts  []int `json:"invasion_grunts"`
	Costumes        []struct {
		PokemonID int   `json:"pokemon_id"`
		IDs       []int `json:"ids"`
	} `json:"costumes"`
	Forms []struct {
		PokemonID int   `json:"pokemon_id"`
		IDs       []int `json:"ids"`
	} `json:"forms"`
	RaidCostumes []struct {
		PokemonID int   `json:"pokemon_id"`
		IDs       []int `json:"ids"`
	} `json:"raid_costumes"`
	RaidForms []struct {
		PokemonID int   `json:"pokemon_id"`
		IDs       []int `json:"ids"`
	} `json:"raid_forms"`
}

func getActivity(t *testing.T, ra *tracker.RecentActivity) activityBody {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")
	RegisterV2Activity(humaAPI, ra)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/activity", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v2/activity = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var got activityBody
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v; raw: %s", err, w.Body.String())
	}
	return got
}

func TestV2ActivityReturnsEveryCategory(t *testing.T) {
	ra := tracker.NewRecentActivity()
	ra.RecordRaidBoss(150)
	ra.RecordMaxBattleBoss(149)
	ra.RecordQuestPokemon(25)
	ra.RecordQuestItem(701)
	ra.RecordQuestCandy(147)
	ra.RecordQuestMega(6)
	ra.RecordQuestXL(143)
	ra.RecordInvasionGrunt(44)
	ra.RecordCostume(25, 12)
	ra.RecordForm(25, 46)
	ra.RecordRaidCostume(150, 12)
	ra.RecordRaidForm(150, 952)

	got := getActivity(t, ra)

	for name, ids := range map[string][]int{
		"raid_bosses":       got.RaidBosses,
		"max_battle_bosses": got.MaxBattleBosses,
		"quest_pokemon":     got.QuestPokemon,
		"quest_items":       got.QuestItems,
		"quest_candy":       got.QuestCandy,
		"quest_mega":        got.QuestMega,
		"quest_xl":          got.QuestXL,
		"invasion_grunts":   got.InvasionGrunts,
	} {
		if len(ids) != 1 {
			t.Errorf("%s = %v, want exactly 1 id", name, ids)
		}
	}
	if len(got.Costumes) != 1 || got.Costumes[0].PokemonID != 25 || len(got.Costumes[0].IDs) != 1 {
		t.Errorf("costumes = %+v, want one entry for pokemon 25", got.Costumes)
	}
	if len(got.Forms) != 1 || got.Forms[0].PokemonID != 25 {
		t.Errorf("forms = %+v, want one entry for pokemon 25", got.Forms)
	}
	if len(got.RaidCostumes) != 1 || got.RaidCostumes[0].PokemonID != 150 {
		t.Errorf("raid_costumes = %+v, want one entry for pokemon 150", got.RaidCostumes)
	}
	if len(got.RaidForms) != 1 || got.RaidForms[0].PokemonID != 150 {
		t.Errorf("raid_forms = %+v, want one entry for pokemon 150", got.RaidForms)
	}
}

func TestV2ActivityReportsRecencyWindow(t *testing.T) {
	got := getActivity(t, tracker.NewRecentActivity())
	if got.WindowSecs != 21600 {
		t.Errorf("window_secs = %d, want 21600 (6h)", got.WindowSecs)
	}
}

// Ordering must be stable so clients can cache and diff responses; Go map
// iteration order is randomised, so this fails without an explicit sort.
func TestV2ActivityOutputIsDeterministicallyOrdered(t *testing.T) {
	ra := tracker.NewRecentActivity()
	for _, id := range []int{383, 150, 484} {
		ra.RecordRaidBoss(id)
	}
	for _, p := range []int{133, 25, 94} {
		ra.RecordCostume(p, 12)
	}
	ra.RecordCostume(25, 3)

	got := getActivity(t, ra)

	if want := []int{150, 383, 484}; !equalInts(got.RaidBosses, want) {
		t.Errorf("raid_bosses = %v, want ascending %v", got.RaidBosses, want)
	}
	gotPokemon := make([]int, len(got.Costumes))
	for i, e := range got.Costumes {
		gotPokemon[i] = e.PokemonID
	}
	if want := []int{25, 94, 133}; !equalInts(gotPokemon, want) {
		t.Errorf("costume pokemon_ids = %v, want ascending %v", gotPokemon, want)
	}
	if want := []int{3, 12}; !equalInts(got.Costumes[0].IDs, want) {
		t.Errorf("costumes[25].ids = %v, want ascending %v", got.Costumes[0].IDs, want)
	}
}

// A processor built without a RecentActivity tracker must serve an empty
// payload rather than 500 or a body full of JSON nulls.
func TestV2ActivityNilTrackerServesEmptyArrays(t *testing.T) {
	got := getActivity(t, nil)

	if got.RaidBosses == nil || len(got.RaidBosses) != 0 {
		t.Errorf("raid_bosses = %v, want empty array not null", got.RaidBosses)
	}
	if got.Costumes == nil || len(got.Costumes) != 0 {
		t.Errorf("costumes = %v, want empty array not null", got.Costumes)
	}
	if got.WindowSecs != 21600 {
		t.Errorf("window_secs = %d, want 21600 even with no tracker", got.WindowSecs)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
