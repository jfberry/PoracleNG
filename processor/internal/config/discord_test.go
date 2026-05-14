package config

import "testing"

// TestDiscordGatewayToken_FallbackBehaviour pins the load-bearing
// invariant that the gateway-side bot uses CommandToken when set and
// falls back to the first delivery token otherwise. Drives the
// single-bot vs split-bot startup path in cmd/processor/main.go.
func TestDiscordGatewayToken_FallbackBehaviour(t *testing.T) {
	cases := []struct {
		name         string
		commandToken string
		token        any
		want         string
	}{
		{
			name:         "single-bot: command_token empty, single delivery token",
			commandToken: "",
			token:        "delivery-tok",
			want:         "delivery-tok",
		},
		{
			name:         "single-bot: command_token empty, multi delivery tokens",
			commandToken: "",
			token:        []any{"first-delivery", "second-delivery"},
			want:         "first-delivery",
		},
		{
			name:         "split-bot: command_token set wins over delivery token",
			commandToken: "command-tok",
			token:        "delivery-tok",
			want:         "command-tok",
		},
		{
			name:         "split-bot: command_token set wins even with multi delivery tokens",
			commandToken: "command-tok",
			token:        []any{"first-delivery", "second-delivery"},
			want:         "command-tok",
		},
		{
			name:         "nothing configured: empty string (callers treat as disabled)",
			commandToken: "",
			token:        nil,
			want:         "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DiscordConfig{CommandToken: c.commandToken, Token: c.token}
			if got := cfg.DiscordGatewayToken(); got != c.want {
				t.Errorf("DiscordGatewayToken() = %q, want %q", got, c.want)
			}
		})
	}
}
