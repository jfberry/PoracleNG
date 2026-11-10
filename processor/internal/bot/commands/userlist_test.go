package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// A Telegram community admin may run !userlist, but only sees humans in
// the communities they administer (PoracleJS); IsCommunityAdmin was never
// set, so the command was refused (#286).
func TestUserlist_CommunityAdminSeesOwnCommunities(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.IsAdmin = false
	ctx.IsCommunityAdmin = true
	ctx.CommunityAdminOf = []string{"north"}
	mock.AddHuman(&store.Human{ID: "n1", Type: "telegram:user", Name: "Norm", Enabled: true, CommunityMembership: []string{"north"}})
	mock.AddHuman(&store.Human{ID: "s1", Type: "telegram:user", Name: "Sally", Enabled: true, CommunityMembership: []string{"south"}})

	replies := (&UserlistCommand{}).Run(ctx, nil)
	text := ""
	for _, r := range replies {
		text += r.Text
	}
	if !strings.Contains(text, "Norm") || strings.Contains(text, "Sally") || strings.Contains(text, "TestUser") {
		t.Errorf("community admin should see only north members: %q", text)
	}
}
