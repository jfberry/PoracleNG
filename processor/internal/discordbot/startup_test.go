package discordbot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
)

func TestOpenWithRetryRecoversAtOneMinuteIntervals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var attempts []time.Duration
		err := openWithRetry(context.Background(), func() error {
			attempts = append(attempts, time.Since(start))
			switch len(attempts) {
			case 1:
				return &net.DNSError{Name: "discord.com", IsTemporary: true}
			case 2:
				return &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusServiceUnavailable}}
			default:
				return nil
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []time.Duration{0, time.Minute, 2 * time.Minute}
		if !slices.Equal(attempts, want) {
			t.Fatalf("connection attempts at %v, want %v", attempts, want)
		}
	})
}

func TestOpenWithRetryImmediateSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		attempts := 0
		err := openWithRetry(context.Background(), func() error {
			attempts++
			return nil
		})
		if err != nil || attempts != 1 || time.Since(start) != 0 {
			t.Fatalf("err=%v, attempts=%d, elapsed=%s", err, attempts, time.Since(start))
		}
	})
}

func TestOpenWithRetryCancellation(t *testing.T) {
	t.Run("before first attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := openWithRetry(ctx, func() error {
			t.Fatal("must not connect after cancellation")
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	})
	t.Run("during retry wait", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			time.AfterFunc(10*time.Second, cancel)
			start := time.Now()
			attempts := 0
			err := openWithRetry(ctx, func() error {
				attempts++
				return io.EOF
			})
			if !errors.Is(err, context.Canceled) || attempts != 1 || time.Since(start) != 10*time.Second {
				t.Fatalf("err=%v, attempts=%d, elapsed=%s", err, attempts, time.Since(start))
			}
		})
	})
	t.Run("when connection completes", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := openWithRetry(ctx, func() error {
			cancel()
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	})
}

func TestOpenWithRetryStopsOnPermanentFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		start := time.Now()
		attempts := 0
		permanentErr := fmt.Errorf("gateway: %w", discordgo.ErrUnauthorized)
		err := openWithRetry(ctx, func() error {
			attempts++
			if attempts == 1 {
				return io.EOF
			}
			return permanentErr
		})
		if !errors.Is(err, permanentErr) || attempts != 2 || time.Since(start) != time.Minute {
			t.Fatalf("err=%v, attempts=%d, elapsed=%s", err, attempts, time.Since(start))
		}
	})
}

func TestRetryableGatewayError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"DNS not ready", &net.DNSError{Name: "discord.com", IsTemporary: true}, true},
		{"connection refused", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
		{"connection closed", io.EOF, true},
		{"rate limited", &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusTooManyRequests}}, true},
		{"server unavailable", &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusServiceUnavailable}}, true},
		{"REST error without response", &discordgo.RESTError{}, true},
		{"unauthorized", discordgo.ErrUnauthorized, false},
		{"REST unauthorized", &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusUnauthorized}}, false},
		{"REST forbidden", &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusForbidden}}, false},
		{"invalid shard", discordgo.ErrWSShardBounds, false},
		{"already connected", discordgo.ErrWSAlreadyOpen, false},
		{"gateway unknown error", &websocket.CloseError{Code: 4000}, true},
		{"gateway session expired", &websocket.CloseError{Code: 4009}, true},
	}
	for _, code := range []int{4004, 4010, 4011, 4012, 4013, 4014} {
		tests = append(tests, struct {
			name string
			err  error
			want bool
		}{fmt.Sprintf("non-reconnectable gateway code %d", code), &websocket.CloseError{Code: code}, false})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, err := range []error{tt.err, fmt.Errorf("connect: %w", tt.err)} {
				if got := retryableGatewayError(err); got != tt.want {
					t.Errorf("retryableGatewayError(%v) = %v, want %v", err, got, tt.want)
				}
			}
		})
	}
}
