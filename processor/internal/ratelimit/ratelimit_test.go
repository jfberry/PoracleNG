package ratelimit

import (
	"testing"
	"time"
)

func TestUnderLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 3, ChannelLimit: 5})
	defer l.Close()

	for i := 0; i < 3; i++ {
		r := l.Check("user1", "discord:user")
		if !r.Allowed {
			t.Fatalf("message %d should be allowed", i+1)
		}
		if r.JustBreached || r.Banned {
			t.Fatalf("message %d should not be breached/banned", i+1)
		}
	}
}

func TestAtLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 3, ChannelLimit: 5})
	defer l.Close()

	// Send exactly 3 (the limit)
	for i := 0; i < 3; i++ {
		r := l.Check("user1", "discord:user")
		if !r.Allowed {
			t.Fatalf("message %d should be allowed", i+1)
		}
	}
}

func TestOverLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 3, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Send 3 allowed
	for i := 0; i < 3; i++ {
		l.Check("user1", "discord:user")
	}

	// 4th message = first over limit
	r := l.Check("user1", "discord:user")
	if r.Allowed {
		t.Fatal("4th message should not be allowed")
	}
	if !r.JustBreached {
		t.Fatal("4th message should have JustBreached=true")
	}
	if r.Banned {
		t.Fatal("should not be banned after one violation")
	}

	// 5th message = still over, but not JustBreached
	r = l.Check("user1", "discord:user")
	if r.Allowed {
		t.Fatal("5th message should not be allowed")
	}
	if r.JustBreached {
		t.Fatal("5th message should not have JustBreached=true")
	}
}

func TestChannelLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 3, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	for i := 0; i < 5; i++ {
		r := l.Check("chan1", "discord:channel")
		if !r.Allowed {
			t.Fatalf("channel message %d should be allowed", i+1)
		}
	}

	r := l.Check("chan1", "discord:channel")
	if r.Allowed {
		t.Fatal("6th channel message should not be allowed")
	}
	if !r.JustBreached {
		t.Fatal("6th channel message should have JustBreached=true")
	}
}

func TestWindowExpiry(t *testing.T) {
	l := New(Config{TimingPeriod: 1, DMLimit: 2, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Fill the window
	l.Check("user1", "discord:user")
	l.Check("user1", "discord:user")
	r := l.Check("user1", "discord:user")
	if r.Allowed {
		t.Fatal("should be over limit")
	}

	// Wait for window to expire
	time.Sleep(1100 * time.Millisecond)

	r = l.Check("user1", "discord:user")
	if !r.Allowed {
		t.Fatal("should be allowed after window expiry")
	}
}

func TestBannedAfterThreshold(t *testing.T) {
	l := New(Config{TimingPeriod: 1, DMLimit: 1, ChannelLimit: 5, MaxLimitsBeforeStop: 3})
	defer l.Close()

	for violation := 0; violation < 3; violation++ {
		// Fill window + breach
		l.Check("user1", "discord:user")
		r := l.Check("user1", "discord:user")
		if !r.JustBreached {
			t.Fatalf("violation %d: should be JustBreached", violation+1)
		}

		if violation < 2 {
			if r.Banned {
				t.Fatalf("violation %d: should not be banned yet", violation+1)
			}
		} else {
			if !r.Banned {
				t.Fatal("violation 3: should be banned")
			}
		}

		// Wait for rate limit window to reset
		time.Sleep(1100 * time.Millisecond)
	}
}

func TestOverrides(t *testing.T) {
	l := New(Config{
		TimingPeriod:        60,
		DMLimit:             2,
		ChannelLimit:        5,
		MaxLimitsBeforeStop: 10,
		Overrides:           map[string]int{"vip_user": 100},
	})
	defer l.Close()

	// VIP user should use override limit
	for i := 0; i < 100; i++ {
		r := l.Check("vip_user", "discord:user")
		if !r.Allowed {
			t.Fatalf("VIP message %d should be allowed (limit 100)", i+1)
		}
	}

	r := l.Check("vip_user", "discord:user")
	if r.Allowed {
		t.Fatal("101st VIP message should not be allowed")
	}

	// Normal user is still limited to 2
	l.Check("normal_user", "discord:user")
	l.Check("normal_user", "discord:user")
	r = l.Check("normal_user", "discord:user")
	if r.Allowed {
		t.Fatal("3rd normal user message should not be allowed")
	}
}

func TestTelegramUserType(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Telegram user gets DM limit
	l.Check("tguser", "telegram:user")
	l.Check("tguser", "telegram:user")
	r := l.Check("tguser", "telegram:user")
	if r.Allowed {
		t.Fatal("telegram user should hit DM limit of 2")
	}
}

func TestTelegramChannelType(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Telegram channel gets channel limit
	for i := 0; i < 5; i++ {
		r := l.Check("tgchan", "telegram:channel")
		if !r.Allowed {
			t.Fatalf("telegram channel message %d should be allowed", i+1)
		}
	}
	r := l.Check("tgchan", "telegram:channel")
	if r.Allowed {
		t.Fatal("6th telegram channel message should not be allowed")
	}
}

func TestWebhookType(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Webhook type gets channel limit
	for i := 0; i < 5; i++ {
		r := l.Check("wh1", "webhook")
		if !r.Allowed {
			t.Fatalf("webhook message %d should be allowed", i+1)
		}
	}
	r := l.Check("wh1", "webhook")
	if r.Allowed {
		t.Fatal("6th webhook message should not be allowed")
	}
}

func TestResetSeconds(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 5, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	r := l.Check("user1", "discord:user")
	if r.ResetSeconds < 55 || r.ResetSeconds > 60 {
		t.Fatalf("ResetSeconds should be ~60, got %d", r.ResetSeconds)
	}
}

func TestResultLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 5, ChannelLimit: 10, MaxLimitsBeforeStop: 10})
	defer l.Close()

	r := l.Check("user1", "discord:user")
	if r.Limit != 5 {
		t.Fatalf("DM user limit should be 5, got %d", r.Limit)
	}

	r = l.Check("chan1", "discord:channel")
	if r.Limit != 10 {
		t.Fatalf("channel limit should be 10, got %d", r.Limit)
	}
}
