package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// TestV1_OverrideAreasStillSpaced: v1 is frozen. It keeps writing the spaced
// key form (v1NormalizeOverrideAreas), and its GET output — which comes from
// db.parseOverrideAreas on the SQL read path — shows a canonical
// "gent_centrum" row as "gent centrum" (guarded by
// db.TestParseOverrideAreas_V1StaysSpaced, as v1 GET reads SQL directly).
func TestV1_OverrideAreasStillSpaced(t *testing.T) {
	gin.SetMode(gin.TestMode)
	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User", Enabled: true, Language: "en", CurrentProfileNo: 1})
	lures := store.NewMockTrackingStore(store.LureGetUID, store.LureSetUID)
	fences := []geofence.Fence{humanSquareFence("Gent_Centrum", 50, 50, 1)}
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: fences, Geofence: geofence.NewSpatialIndex(fences)})
	deps := &TrackingDeps{
		Humans:       humans,
		StateMgr:     mgr,
		Tracking:     &store.TrackingStores{Lures: lures},
		Config:       &config.Config{},
		RowText:      &rowtext.Generator{DefaultTemplateName: "1"},
		Translations: i18n.NewBundle(),
	}
	r := gin.New()
	r.POST("/api/tracking/lure/:id", HandleCreateLure(deps))

	req := httptest.NewRequest(http.MethodPost, "/api/tracking/lure/u1?silent=true",
		strings.NewReader(`[{"lure_id":501,"override_areas":["Gent_Centrum"]}]`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("v1 POST: %d %s", w.Code, w.Body.String())
	}
	rows := lures.AllRows()
	if len(rows) != 1 || !reflect.DeepEqual(rows[0].OverrideAreas, []string{"gent centrum"}) {
		t.Fatalf("v1 stored override_areas = %+v, want [gent centrum]", rows)
	}
}
