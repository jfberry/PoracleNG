package main

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// [discord] enabled = false only stopped the gateway; delivery was built
// from the token regardless, so Discord humans with tracking kept getting
// alerts (#292). A disabled platform's humans — Discord webhooks count as
// Discord, as in PoracleJS — are now dropped straight after matching.
func TestFilterBlockedDropsDisabledPlatforms(t *testing.T) {
	matched := func() []webhook.MatchedUser {
		return []webhook.MatchedUser{
			{ID: "1", Type: "discord:user"},
			{ID: "2", Type: "discord:channel"},
			{ID: "3", Type: "webhook"},
			{ID: "4", Type: "telegram:user"},
			{ID: "5", Type: "telegram:group"},
		}
	}
	ids := func(m []webhook.MatchedUser) string {
		s := ""
		for _, u := range m {
			s += u.ID
		}
		return s
	}

	cfg := &config.Config{}
	cfg.Discord.Enabled, cfg.Telegram.Enabled = false, true
	ps := &ProcessorService{cfg: cfg}
	if got := ids(ps.filterBlocked(matched())); got != "45" {
		t.Errorf("discord disabled: kept %q, want telegram only (45)", got)
	}

	cfg.Discord.Enabled, cfg.Telegram.Enabled = true, false
	if got := ids(ps.filterBlocked(matched())); got != "123" {
		t.Errorf("telegram disabled: kept %q, want discord only (123)", got)
	}

	cfg.Discord.Enabled, cfg.Telegram.Enabled = true, true
	if got := ids(ps.filterBlocked(matched())); got != "12345" {
		t.Errorf("both enabled: kept %q, want all", got)
	}
}

// A token with enabled = false now means no alerts for that platform, so
// startup warns about it.
func TestDisabledPlatformWarnings(t *testing.T) {
	cfg := &config.Config{}
	cfg.Discord.Enabled, cfg.Telegram.Enabled = true, false
	cfg.Telegram.Token = "tg-token"
	if w := disabledPlatformWarnings(cfg); len(w) != 1 || !strings.Contains(w[0], "[telegram]") {
		t.Errorf("telegram token + disabled: warnings %v", w)
	}
	cfg.Telegram.Token = ""
	if w := disabledPlatformWarnings(cfg); len(w) != 0 {
		t.Errorf("no token: warnings %v, want none", w)
	}
}
