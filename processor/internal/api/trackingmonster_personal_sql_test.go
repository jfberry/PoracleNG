package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// TestV1Monster_PersonalSQL drives the real v1 handlers against a real MySQL
// (non-destructive: migrations are idempotent, only rows with a unique test id are created and removed; skips unless the DB name contains "test"). Skipped unless PORACLENG_TEST_DSN is set.
func TestV1Monster_PersonalSQL(t *testing.T) {
	dsn := os.Getenv("PORACLENG_TEST_DSN")
	if dsn == "" {
		t.Skip("PORACLENG_TEST_DSN not set")
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() }) // registered first, so it runs after the row cleanup below
	if err := conn.Ping(); err != nil {
		t.Skipf("cannot ping test DB: %v", err)
	}
	var dbName string
	if err := conn.QueryRow("SELECT DATABASE()").Scan(&dbName); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dbName, "test") {
		t.Skipf("refusing to run against database %q: name must contain \"test\"", dbName)
	}
	// Same idempotent bring-up the processor does at startup; non-destructive.
	if err := db.AdoptExistingDatabase(conn); err != nil {
		t.Fatal(err)
	}
	db.DropForeignKeys(conn)
	if err := db.RunMigrations(conn); err != nil {
		t.Fatal(err)
	}
	hid := fmt.Sprintf("test-personal-%d", time.Now().UnixNano())
	cleanup := func() {
		_, _ = conn.Exec("DELETE FROM monsters WHERE id=?", hid)
		_, _ = conn.Exec("DELETE FROM profiles WHERE id=?", hid)
		_, _ = conn.Exec("DELETE FROM humans WHERE id=?", hid)
	}
	t.Cleanup(cleanup)
	if _, err := conn.Exec("INSERT INTO humans (id, type, name, area, community_membership) VALUES (?,'discord:user','U','[]','[]')", hid); err != nil {
		t.Fatal(err)
	}

	dbx := sqlx.NewDb(conn, "mysql")
	gd := &gamedata.GameData{Monsters: map[gamedata.MonsterKey]*gamedata.Monster{}, Util: &gamedata.UtilData{}}
	deps := &TrackingDeps{
		DB:           dbx,
		Humans:       store.NewSQLHumanStore(dbx),
		Tracking:     store.NewTrackingStores(dbx),
		Config:       &config.Config{},
		RowText:      &rowtext.Generator{DefaultTemplateName: "1", GD: gd},
		Translations: i18n.NewBundle(),
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/tracking/pokemon/:id", HandleCreateMonster(deps))
	r.GET("/api/tracking/pokemon/:id", HandleGetMonster(deps))

	post := func(body string) {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/"+hid, strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("POST %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	stored := func() (personal, minIV int, uid int64) {
		t.Helper()
		if err := conn.QueryRow("SELECT uid, personal, min_iv FROM monsters WHERE id=?", hid).Scan(&uid, &personal, &minIV); err != nil {
			t.Fatal(err)
		}
		return
	}

	post(`{"pokemon_id":25,"personal":1}`)
	p, _, uid := stored()
	if p != 1 {
		t.Fatalf("create: personal=%d", p)
	}

	post(fmt.Sprintf(`{"uid":%d,"pokemon_id":25,"min_iv":90}`, uid))
	if p, iv, _ := stored(); p != 1 || iv != 90 {
		t.Fatalf("omitted personal: personal=%d min_iv=%d, want 1/90", p, iv)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tracking/pokemon/"+hid, nil))
	var got struct {
		Pokemon []map[string]any `json:"pokemon"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Pokemon) != 1 || got.Pokemon[0]["personal"] != float64(1) {
		t.Fatalf("GET personal: err=%v body=%s", err, w.Body.String())
	}

	post(fmt.Sprintf(`{"uid":%d,"pokemon_id":25,"min_iv":90,"personal":0}`, uid))
	if p, _, _ := stored(); p != 0 {
		t.Fatalf("explicit 0: personal=%d", p)
	}
}
