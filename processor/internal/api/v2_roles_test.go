package api

import (
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// newV2RolesTestAPI wires the strict v2 roles endpoints against a mock human
// store. The Discord session is always nil here: discordgo's *Session is a
// concrete type that cannot be faithfully stubbed in-process, so these tests
// cover every path that does NOT require a live gateway — 404 (unknown human),
// 503 (no session for a discord:user), the non-discord:user empty-result path,
// and the delegated-administration computation (which only needs config + an
// ID-based match, not a session). The happy-path role list/set against a live
// guild is exercised by integration against a real bot, not here.
func newV2RolesTestAPI(t *testing.T, cfg *config.Config) (*gin.Engine, *store.MockHumanStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{
		ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, CurrentProfileNo: 1,
	})
	humans.AddHuman(&store.Human{
		ID: "wh1", Type: "discord:webhook", Name: "Hook1", Enabled: true, CurrentProfileNo: 1,
	})

	if cfg == nil {
		cfg = &config.Config{}
	}
	deps := &RoleDeps{
		SessionFunc: func() *discordgo.Session { return nil },
		Config:      cfg,
		Humans:      humans,
	}
	RegisterV2Roles(humaAPI, deps)
	return r, humans
}

// --- list roles -------------------------------------------------------------

func TestV2Roles_List_UnknownHuman(t *testing.T) {
	r, _ := newV2RolesTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/roles", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Roles_List_NonDiscordUserEmpty(t *testing.T) {
	r, _ := newV2RolesTestAPI(t, nil)
	// A webhook human is not a discord:user — returns an empty guild list, 200.
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/wh1/roles", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	guilds, ok := v2DecodeBody(t, w)["guilds"].([]any)
	if !ok || len(guilds) != 0 {
		t.Fatalf("expected empty guilds, got %v", v2DecodeBody(t, w)["guilds"])
	}
}

func TestV2Roles_List_NoSession503(t *testing.T) {
	r, _ := newV2RolesTestAPI(t, nil)
	// discord:user but no live session => 503.
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/roles", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

// --- add / remove role ------------------------------------------------------

func TestV2Roles_Add_UnknownHuman(t *testing.T) {
	r, _ := newV2RolesTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/nope/roles/role1", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Roles_Add_NoSession503(t *testing.T) {
	r, _ := newV2RolesTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/roles/role1", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Roles_Remove_NoSession503(t *testing.T) {
	r, _ := newV2RolesTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/roles/role1", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Roles_Remove_NonDiscordUserEmpty(t *testing.T) {
	r, _ := newV2RolesTestAPI(t, nil)
	// Webhook human: empty result, 200, no session needed.
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/wh1/roles/role1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	result, ok := v2DecodeBody(t, w)["result"].([]any)
	if !ok || len(result) != 0 {
		t.Fatalf("expected empty result, got %v", v2DecodeBody(t, w)["result"])
	}
}

func TestV2Roles_Add_StrictRejectsUnknownQuery(t *testing.T) {
	r, _ := newV2RolesTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/roles/role1?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query, got %d: %s", w.Code, w.Body.String())
	}
}

// --- administration roles ---------------------------------------------------

// rolesAdminCfg builds a config with a Discord + Telegram token (so both
// administration branches run) and delegated-administration rules that match
// user "admin1" by ID — exercising the no-session computation path.
func rolesAdminCfg() *config.Config {
	cfg := &config.Config{}
	cfg.Discord.Token = "discord-token"
	cfg.Discord.Guilds = []string{"g1"}
	cfg.Discord.DelegatedAdministration = config.DelegatedAdminConfig{
		WebhookTracking: map[string][]string{"hookA": {"admin1"}},
		UserTracking:    []string{"admin1"},
	}
	cfg.Telegram.Token = "telegram-token"
	cfg.Telegram.DelegatedAdministration = config.TelegramDelegatedAdminConfig{
		UserTracking: []string{"admin1"},
	}
	return cfg
}

func TestV2Roles_AdminRoles_UnknownHuman(t *testing.T) {
	r, _ := newV2RolesTestAPI(t, rolesAdminCfg())
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/admin-roles", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Roles_AdminRoles_Computed(t *testing.T) {
	cfg := rolesAdminCfg()
	r, humans := newV2RolesTestAPI(t, cfg)
	humans.AddHuman(&store.Human{ID: "admin1", Type: "discord:user", Name: "Admin", CurrentProfileNo: 1})

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/admin1/admin-roles", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	admin, ok := v2DecodeBody(t, w)["admin"].(map[string]any)
	if !ok {
		t.Fatalf("missing admin object: %s", w.Body.String())
	}

	discord, ok := admin["discord"].(map[string]any)
	if !ok {
		t.Fatalf("missing discord admin roles: %v", admin)
	}
	webhooks, _ := discord["webhooks"].([]any)
	if len(webhooks) != 1 || webhooks[0] != "hookA" {
		t.Fatalf("expected webhook hookA, got %v", discord["webhooks"])
	}
	if discord["users"] != true {
		t.Fatalf("expected discord users=true, got %v", discord["users"])
	}

	telegram, ok := admin["telegram"].(map[string]any)
	if !ok {
		t.Fatalf("missing telegram admin roles: %v", admin)
	}
	if telegram["users"] != true {
		t.Fatalf("expected telegram users=true, got %v", telegram["users"])
	}
}

func TestV2Roles_AdminRoles_NoTokensEmpty(t *testing.T) {
	// No Discord/Telegram tokens => empty admin result (both branches skipped).
	r, _ := newV2RolesTestAPI(t, &config.Config{})
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/admin-roles", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	admin := v2DecodeBody(t, w)["admin"].(map[string]any)
	if _, present := admin["discord"]; present {
		t.Fatalf("expected no discord result without token, got %v", admin["discord"])
	}
	if _, present := admin["telegram"]; present {
		t.Fatalf("expected no telegram result without token, got %v", admin["telegram"])
	}
}
