package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func newV2AccountsTestAPI(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Mirrors main.go: encoded slashes in path params.
	r.UseRawPath = true
	r.UnescapePathValues = true
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	deps := &TrackingDeps{
		Humans:   humans,
		Accounts: store.NewMockPlayerAccountStore(),
		Config:   &config.Config{}, // player_accounts.self_service unset/false: must not block the API
	}
	RegisterV2Accounts(humaAPI, deps)
	RegisterV2TrackingSnapshot(humaAPI, deps)
	return r
}

func TestV2Accounts_ListEmptyAndUnknownHuman(t *testing.T) {
	r := newV2AccountsTestAPI(t)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/accounts", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"accounts":[]`) {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	if w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nobody/accounts", ""); w.Code != 404 {
		t.Fatalf("unknown human: %d", w.Code)
	}
}

func TestV2Accounts_AddListLabelDelete(t *testing.T) {
	r := newV2AccountsTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/accounts", `{"player_id":"User_ABC","label":"main"}`)
	if w.Code != 201 {
		t.Fatalf("add: %d %s", w.Code, w.Body.String())
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/accounts", "")
	if !strings.Contains(w.Body.String(), `"player_id":"User_ABC"`) || !strings.Contains(w.Body.String(), `"label":"main"`) || !strings.Contains(w.Body.String(), `"created_at"`) {
		t.Fatalf("list: %s", w.Body.String())
	}
	if w := v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/accounts/User_ABC", `{"label":"alt"}`); w.Code != 200 {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	if w := v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/accounts/nope", `{"label":"x"}`); w.Code != 404 {
		t.Fatalf("put missing: %d", w.Code)
	}
	if w := v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/accounts/User_ABC", `{"label":""}`); w.Code != 200 {
		t.Fatalf("clear: %d", w.Code)
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/accounts", "")
	if !strings.Contains(w.Body.String(), `"label":null`) {
		t.Fatalf("expected null label: %s", w.Body.String())
	}
	if w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/accounts/User_ABC", ""); w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/accounts/User_ABC", ""); w.Code != 404 {
		t.Fatalf("delete missing: %d", w.Code)
	}
}

func TestV2Accounts_Rejections(t *testing.T) {
	r := newV2AccountsTestAPI(t)
	post := func(body string) int {
		return v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/accounts", body).Code
	}
	if c := post(`{"player_id":"User_ABC","label":"main"}`); c != 201 {
		t.Fatalf("seed: %d", c)
	}
	if c := post(`{"player_id":"User_ABC"}`); c != 409 {
		t.Errorf("dup player: %d", c)
	}
	if c := post(`{"player_id":"x","label":"MAIN"}`); c != 409 {
		t.Errorf("dup label: %d", c)
	}
	if c := post(`{"player_id":""}`); c != 422 {
		t.Errorf("empty: %d", c)
	}
	if c := post(fmt.Sprintf(`{"player_id":"%s"}`, strings.Repeat("a", 257))); c != 422 {
		t.Errorf("long: %d", c)
	}
	for i := 1; i < 50; i++ {
		if c := post(fmt.Sprintf(`{"player_id":"p%d"}`, i)); c != 201 {
			t.Fatalf("fill %d: %d", i, c)
		}
	}
	if c := post(`{"player_id":"over"}`); c != 422 {
		t.Errorf("51st: %d", c)
	}
}

func TestV2Accounts_SlashInPlayerID(t *testing.T) {
	r := newV2AccountsTestAPI(t)
	if c := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/accounts", `{"player_id":"User/ABC"}`).Code; c != 201 {
		t.Fatalf("add: %d", c)
	}
	if c := v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/accounts/User%2FABC", `{"label":"s"}`).Code; c != 200 {
		t.Fatalf("put: %d", c)
	}
	if c := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/accounts/User%2FABC", "").Code; c != 200 {
		t.Fatalf("delete: %d", c)
	}
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/accounts", "")
	if strings.Contains(w.Body.String(), "User/ABC") {
		t.Fatalf("still present: %s", w.Body.String())
	}
}

func TestV2Accounts_InSnapshot(t *testing.T) {
	r := newV2AccountsTestAPI(t)
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/accounts", `{"player_id":"User_ABC"}`)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"accounts":[{`) {
		t.Fatalf("snapshot: %d %s", w.Code, w.Body.String())
	}
}
