package config

import "testing"

func TestBlankAPISecretExposed(t *testing.T) {
	cases := []struct {
		host, secret string
		want         bool
	}{
		{"127.0.0.1", "", false},
		{"127.0.0.2", "", false},
		{"::1", "", false},
		{"[::1]", "", false},
		{"localhost", "", false},
		{"LOCALHOST", "", false},
		{"0.0.0.0", "", true},
		{"", "", true}, // empty host binds all interfaces
		{"::", "", true},
		{"192.168.1.10", "", true},
		{"poracle.example.com", "", true},
		{"0.0.0.0", "s3cret", false},
		{"0.0.0.0", "   ", false}, // non-empty (if odd) secret still enforces auth
	}
	for _, tc := range cases {
		p := ProcessorConfig{Host: tc.host, APISecret: tc.secret}
		if got := p.BlankAPISecretExposed(); got != tc.want {
			t.Errorf("host=%q secret=%q: got %v, want %v", tc.host, tc.secret, got, tc.want)
		}
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for _, h := range []string{"127.0.0.1", "::1", "localhost", "[::1]"} {
		if !IsLoopbackHost(h) {
			t.Errorf("%q should be loopback", h)
		}
	}
	for _, h := range []string{"", "0.0.0.0", "::", "10.0.0.1", "example.com"} {
		if IsLoopbackHost(h) {
			t.Errorf("%q should not be loopback", h)
		}
	}
}
