package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestShinyRatesAndPlayerAccountsConfig(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(`
[shiny_rates]
url = "https://rates.example"
token = "t"
[player_accounts]
self_service = true
`, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ShinyRates.URL != "https://rates.example" || cfg.ShinyRates.Token != "t" || !cfg.PlayerAccounts.SelfService {
		t.Fatalf("decoded = %+v %+v", cfg.ShinyRates, cfg.PlayerAccounts)
	}
}
