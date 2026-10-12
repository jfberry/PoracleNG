package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func newV1PersonalHarness(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.MonsterTrackingAPI]) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mock := store.NewMockHumanStore()
	mock.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User", Enabled: true, Language: "en", CurrentProfileNo: 1})
	ms := store.NewMockTrackingStore(store.MonsterGetUID, store.MonsterSetUID)
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Util:     &gamedata.UtilData{},
	}
	deps := &TrackingDeps{
		Humans:       mock,
		Tracking:     &store.TrackingStores{Monsters: ms},
		Config:       &config.Config{},
		RowText:      &rowtext.Generator{DefaultTemplateName: "1", GD: gd},
		Translations: i18n.NewBundle(),
	}
	r := gin.New()
	r.POST("/api/tracking/pokemon/:id", HandleCreateMonster(deps))
	r.GET("/api/tracking/pokemon/:id", HandleGetMonster(deps))
	return r, ms
}

func v1Post(t *testing.T, r http.Handler, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/u1", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST %s: %d %s", body, w.Code, w.Body.String())
	}
}

func TestV1Monster_PersonalCreateAndMask(t *testing.T) {
	r, ms := newV1PersonalHarness(t)
	v1Post(t, r, `{"pokemon_id":25,"personal":1}`)
	v1Post(t, r, `{"pokemon_id":26,"personal":7}`)
	for _, row := range ms.AllRows() {
		want := 1
		if row.PokemonID == 26 {
			want = 3
		}
		if row.Personal != want {
			t.Fatalf("pokemon %d personal=%d want %d", row.PokemonID, row.Personal, want)
		}
	}
}

// The explicit-uid update path writes through raw SQL (db.UpdateMonsterByUID,
// no mock seam), so the keep-on-omit logic is tested via its helper.
func TestV1Monster_KeepStoredPersonal(t *testing.T) {
	tracked := []db.MonsterTrackingAPI{{UID: 5, Personal: 1}, {UID: 6, Personal: 3}}
	updates := []db.MonsterTrackingAPI{
		{UID: 5, MinIV: 90},   // omitted -> keep 1
		{UID: 6, Personal: 0}, // explicit 0 -> not in keep set, stays 0
		{UID: 99, MinIV: 10},  // omitted but not stored -> 0
	}
	keepStoredPersonal(updates, map[int64]bool{5: true, 99: true}, tracked)
	if updates[0].Personal != 1 || updates[0].MinIV != 90 {
		t.Fatalf("uid5: %+v", updates[0])
	}
	if updates[1].Personal != 0 {
		t.Fatalf("uid6 explicit clear lost: %+v", updates[1])
	}
	if updates[2].Personal != 0 {
		t.Fatalf("uid99: %+v", updates[2])
	}
}

// HandleGetMonster reads through db.SelectMonstersByIDProfile (no mock seam), so
// assert the wire shape it serialises: monsterWithDesc embeds
// db.MonsterTrackingAPI, whose Personal field carries the json tag "personal".
func TestV1Monster_PersonalInGet(t *testing.T) {
	b, err := json.Marshal(db.MonsterTrackingAPI{PokemonID: 25, Personal: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"personal":1`) {
		t.Fatalf("row JSON missing personal: %s", b)
	}
}
