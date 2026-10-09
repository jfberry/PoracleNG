package config

import (
	"strconv"
	"strings"
	"testing"
)

func TestTelegramRateLimitDefaultsForExistingConfig(t *testing.T) {
	cfg, err := loadFromReader(t, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Tuning.TelegramGlobalRatePerSecond != 29 {
		t.Errorf("telegram_global_rate_per_second=%d, want 29", cfg.Tuning.TelegramGlobalRatePerSecond)
	}
	if cfg.Tuning.TelegramGlobalBurst != 5 {
		t.Errorf("telegram_global_burst=%d, want 5", cfg.Tuning.TelegramGlobalBurst)
	}
}

func TestTelegramRateLimitConfigLoadsOverrides(t *testing.T) {
	cfg, err := loadFromReader(t, "[tuning]\ntelegram_global_rate_per_second = 7\ntelegram_global_burst = 2")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Tuning.TelegramGlobalRatePerSecond != 7 {
		t.Errorf("telegram_global_rate_per_second=%d, want 7", cfg.Tuning.TelegramGlobalRatePerSecond)
	}
	if cfg.Tuning.TelegramGlobalBurst != 2 {
		t.Errorf("telegram_global_burst=%d, want 2", cfg.Tuning.TelegramGlobalBurst)
	}
}

func TestTelegramRateLimitConfigRejectsNonPositiveValues(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value int
	}{
		{name: "global rate zero", field: "telegram_global_rate_per_second", value: 0},
		{name: "global rate negative", field: "telegram_global_rate_per_second", value: -1},
		{name: "global burst zero", field: "telegram_global_burst", value: 0},
		{name: "global burst negative", field: "telegram_global_burst", value: -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadFromReader(t, "[tuning]\n"+tc.field+" = "+strconv.Itoa(tc.value))
			if err == nil {
				t.Fatalf("Load succeeded with %s=%d; want validation error", tc.field, tc.value)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("error %q does not name invalid field %q", err, tc.field)
			}
		})
	}
}
