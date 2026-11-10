package discordroles

import (
	"slices"
	"testing"
)

func TestMemberRoleIDsAppendsEveryone(t *testing.T) {
	roles := []string{"r1", "r2"}
	got := MemberRoleIDs("guild-1", roles)
	if want := []string{"r1", "r2", "guild-1"}; !slices.Equal(got, want) {
		t.Errorf("MemberRoleIDs=%v, want %v", got, want)
	}
	if want := []string{"r1", "r2"}; !slices.Equal(roles, want) {
		t.Errorf("input mutated to %v", roles)
	}
}

func TestMemberRoleIDsNoRoles(t *testing.T) {
	// A member with no explicit roles still holds @everyone.
	if got := MemberRoleIDs("guild-1", nil); !slices.Equal(got, []string{"guild-1"}) {
		t.Errorf("MemberRoleIDs(nil)=%v, want [guild-1]", got)
	}
}
