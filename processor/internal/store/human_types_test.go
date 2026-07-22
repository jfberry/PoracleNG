package store

import "testing"

func TestValidateHumanType(t *testing.T) {
	valid := []string{"discord:user", "discord:channel", "discord:thread",
		"telegram:user", "telegram:group", "telegram:channel", "telegram:topic",
		"webhook", "api:user", "api:channel"}
	for _, ty := range valid {
		if err := ValidateHumanType(ty); err != nil {
			t.Errorf("ValidateHumanType(%q) = %v, want nil", ty, err)
		}
	}
	for _, ty := range []string{"api:users", "discord", "", "slack:user"} {
		if err := ValidateHumanType(ty); err == nil {
			t.Errorf("ValidateHumanType(%q) = nil, want error", ty)
		}
	}
}

func TestValidAPIDestinationID(t *testing.T) {
	for _, id := range []string{"u-42", "user.42", "abc_123~x"} {
		if !ValidAPIDestinationID(id) {
			t.Errorf("ValidAPIDestinationID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"has:colon", "has space", "", string(make([]byte, 129))} {
		if ValidAPIDestinationID(id) {
			t.Errorf("ValidAPIDestinationID(%q) = true, want false", id)
		}
	}
}
