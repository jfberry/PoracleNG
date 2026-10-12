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
// (disposable DB; all tables are dropped). Skipped unless PORACLENG_TEST_DSN is set.
func TestV1Monster_PersonalSQL(t *testing.T) {
	dsn := os.Getenv("PORACLENG_TEST_DSN")
	if dsn == "" {
		t.Skip("PORACLENG_TEST_DSN not set")
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Ping(); err != nil {
		t.Skipf("cannot ping test DB: %v", err)
	}
	rows, err := conn.Query("SHOW TABLES")
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	_, _ = conn.Exec("SET FOREIGN_KEY_CHECKS = 0")
	for _, tb := range tables {
		_, _ = conn.Exec(fmt.Sprintf("DROP TABLE `%s`", tb))
	}
	_, _ = conn.Exec("SET FOREIGN_KEY_CHECKS = 1")
	if err := db.AdoptExistingDatabase(conn); err != nil {
		t.Fatal(err)
	}
	db.DropForeignKeys(conn)
	if err := db.RunMigrations(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("INSERT INTO humans (id, type, name, area, community_membership) VALUES ('u1','discord:user','U','[]','[]')"); err != nil {
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
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/u1", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("POST %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	stored := func() (personal, minIV int, uid int64) {
		t.Helper()
		if err := conn.QueryRow("SELECT uid, personal, min_iv FROM monsters WHERE id='u1'").Scan(&uid, &personal, &minIV); err != nil {
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
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tracking/pokemon/u1", nil))
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
