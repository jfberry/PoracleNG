package discordbot

import (
	"testing"
	"time"
)

func TestThreadKeepAliveDuration_RespectsConfig(t *testing.T) {
	if d := keepAliveTickerDuration(0); d != 0 {
		t.Errorf("0 → 0 (disabled), got %v", d)
	}
	if d := keepAliveTickerDuration(24); d != 24*time.Hour {
		t.Errorf("24 → 24h, got %v", d)
	}
	if d := keepAliveTickerDuration(200); d != 168*time.Hour {
		t.Errorf("200 → clamped to 168h, got %v", d)
	}
	if d := keepAliveTickerDuration(-5); d != 0 {
		t.Errorf("-5 → 0, got %v", d)
	}
}
