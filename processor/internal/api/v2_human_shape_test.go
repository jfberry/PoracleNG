package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2 returned humans in the legacy v1 shape (#294): flags as 0/1 and array
// columns as JSON-encoded strings. The v2 human uses real booleans and
// arrays; area_restriction stays nullable because null (unrestricted) and
// [] (no areas allowed) mean different things.
func TestV2Human_TypedShape(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	humans.AddHuman(&store.Human{
		ID: "u2", Type: "discord:user", Name: "Two", Enabled: true, AdminDisable: true,
		Area: []string{"alpha"}, CommunityMembership: []string{"north"},
		AreaRestriction: []string{}, BlockedAlerts: []string{"raid"},
	})

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Human map[string]json.RawMessage `json:"human"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"enabled":              `true`,
		"admin_disable":        `true`,
		"area":                 `["alpha"]`,
		"community_membership": `["north"]`,
		"area_restriction":     `[]`,
		"blocked_alerts":       `["raid"]`,
		"language":             `""`,
	}
	for k, v := range want {
		if got := string(body.Human[k]); got != v {
			t.Errorf("%s = %s, want %s", k, got, v)
		}
	}

	// Unset arrays: area/community/blocked are [], area_restriction is null.
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1", "")
	body.Human = nil
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		"area": `[]`, "community_membership": `[]`, "blocked_alerts": `[]`,
		"area_restriction": `null`, "language": `"en"`, "enabled": `true`,
	} {
		if got := string(body.Human[k]); got != v {
			t.Errorf("u1 %s = %s, want %s", k, got, v)
		}
	}
}
