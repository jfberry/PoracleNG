package api

import (
	"reflect"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
)

func TestFlattenCommunities(t *testing.T) {
	in := []config.CommunityConfig{{
		Name:          "Japan-Wakayama",
		AllowedAreas:  []string{"area-a", "area-b"},
		LocationFence: config.FlexStrings{"Japan"},
	}}
	in[0].Discord.Channels = []string{"chan-1"}
	in[0].Discord.UserRole = []string{"role-1", "role-2"}
	in[0].Telegram.Channels = []string{}
	in[0].Telegram.Admins = []string{"admin-1"}

	got := flattenCommunities(in)
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	row := got[0]
	checks := map[string]any{
		"name":              "Japan-Wakayama",
		"allowed_areas":     []string{"area-a", "area-b"},
		"location_fence":    []string{"Japan"},
		"discord_channels":  []string{"chan-1"},
		"discord_user_role": []string{"role-1", "role-2"},
		"telegram_channels": []string{},
		"telegram_admins":   []string{"admin-1"},
	}
	for k, want := range checks {
		if !reflect.DeepEqual(row[k], want) {
			t.Errorf("%s: want %#v, got %#v", k, want, row[k])
		}
	}
}

func TestNestCommunityRows(t *testing.T) {
	in := []any{
		map[string]any{
			"name":              "Japan-Wakayama",
			"allowed_areas":     []string{"area-a"},
			"location_fence":    []string{"Japan"},
			"discord_channels":  []string{"chan-1"},
			"discord_user_role": []string{"role-1"},
			"telegram_channels": []string{"tg-1"},
			"telegram_admins":   []string{"admin-1"},
		},
	}
	out := nestCommunityRows(in)
	if len(out) != 1 {
		t.Fatalf("want 1 row, got %d", len(out))
	}
	row, ok := out[0].(map[string]any)
	if !ok {
		t.Fatalf("row is not map: %T", out[0])
	}
	if _, ok := row["discord_channels"]; ok {
		t.Errorf("discord_channels should have been removed after nesting")
	}
	discord, ok := row["discord"].(map[string]any)
	if !ok {
		t.Fatalf("discord missing or wrong type: %#v", row["discord"])
	}
	if !reflect.DeepEqual(discord["channels"], []string{"chan-1"}) {
		t.Errorf("discord.channels: got %#v", discord["channels"])
	}
	if !reflect.DeepEqual(discord["user_role"], []string{"role-1"}) {
		t.Errorf("discord.user_role: got %#v", discord["user_role"])
	}
	telegram, ok := row["telegram"].(map[string]any)
	if !ok {
		t.Fatalf("telegram missing: %#v", row["telegram"])
	}
	if !reflect.DeepEqual(telegram["channels"], []string{"tg-1"}) {
		t.Errorf("telegram.channels: got %#v", telegram["channels"])
	}
	if !reflect.DeepEqual(telegram["admins"], []string{"admin-1"}) {
		t.Errorf("telegram.admins: got %#v", telegram["admins"])
	}
	// Untouched scalar should survive.
	if row["name"] != "Japan-Wakayama" {
		t.Errorf("name preserved: got %#v", row["name"])
	}
}

func TestValidateUpdatesRejectsUnknownRowField(t *testing.T) {
	updates := map[string]any{
		"area_security": map[string]any{
			"communities": []any{
				map[string]any{
					"name":         "x",
					"discord_typo": []string{"bad"},
				},
			},
		},
	}
	if err := validateUpdates(updates); err == nil {
		t.Fatalf("expected error for unknown row field")
	}
}

func TestValidateUpdatesAcceptsKnownRowFields(t *testing.T) {
	updates := map[string]any{
		"area_security": map[string]any{
			"communities": []any{
				map[string]any{
					"name":              "x",
					"discord_channels":  []string{"1"},
					"discord_user_role": []string{"2"},
				},
			},
		},
	}
	if err := validateUpdates(updates); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
