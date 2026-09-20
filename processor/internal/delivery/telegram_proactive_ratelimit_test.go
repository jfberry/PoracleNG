package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func mustTelegramSendRateLimiter(t *testing.T, cfg TelegramRateLimitConfig) *TelegramSendRateLimiter {
	t.Helper()
	limiter, err := NewTelegramSendRateLimiter(cfg)
	if err != nil {
		t.Fatalf("NewTelegramSendRateLimiter: %v", err)
	}
	return limiter
}

func TestTelegramGlobalLimiterSustainedRate(t *testing.T) {
	limiter := mustTelegramSendRateLimiter(t, TelegramRateLimitConfig{
		GlobalRatePerSecond: 20,
		GlobalBurst:         1,
	})

	started := time.Now()
	for range 5 {
		if err := limiter.Wait(context.Background()); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
	// Five sends with burst=1 need four refills at 50ms each. Leave
	// scheduler headroom while still proving sustained pacing is active.
	if elapsed := time.Since(started); elapsed < 175*time.Millisecond {
		t.Fatalf("5 permits completed in %v; want at least 175ms at 20 sends/s with burst=1", elapsed)
	}
}

func TestTelegramDifferentDestinationsShareGlobalLimiter(t *testing.T) {
	server, sender, _ := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		return okResponse(1)
	})
	defer server.Close()

	if err := sender.SetRateLimits(TelegramRateLimitConfig{
		GlobalRatePerSecond: 2,
		GlobalBurst:         1,
	}); err != nil {
		t.Fatalf("SetRateLimits: %v", err)
	}

	if _, err := sender.sendMessage(context.Background(), "chat-a", 0, "a", "HTML", false, "", ""); err != nil {
		t.Fatalf("first send: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if _, err := sender.sendMessage(ctx, "chat-b", 0, "b", "HTML", false, "", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("different destination bypassed global limiter: err=%v, want deadline exceeded", err)
	}
}

func TestTelegramMultiMessageJobConsumesOneTokenPerSendCall(t *testing.T) {
	server, sender, calls := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		return okResponse(1)
	})
	defer server.Close()

	if err := sender.SetRateLimits(TelegramRateLimitConfig{
		GlobalRatePerSecond: 20,
		GlobalBurst:         1,
	}); err != nil {
		t.Fatalf("SetRateLimits: %v", err)
	}

	started := time.Now()
	_, err := sender.Send(context.Background(), &Job{
		Target: "123",
		Type:   "telegram:user",
		Message: json.RawMessage(`{
			"sticker":"sticker-file-id",
			"content":"hello",
			"send_order":["sticker","text"]
		}`),
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := len(*calls); got != 2 {
		t.Fatalf("wire calls=%d, want 2", got)
	}
	if elapsed := time.Since(started); elapsed < 35*time.Millisecond {
		t.Fatalf("two message calls completed in %v; second call appears not to have consumed its own global token", elapsed)
	}
}

func TestTelegramGlobalRateLimitWaitDoesNotHoldSemaphore(t *testing.T) {
	server, sender, _ := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		return okResponse(1)
	})
	defer server.Close()

	sender.SetConcurrency(1)
	if err := sender.SetRateLimits(TelegramRateLimitConfig{
		GlobalRatePerSecond: 2,
		GlobalBurst:         1,
	}); err != nil {
		t.Fatalf("SetRateLimits: %v", err)
	}

	// Consume the only send token without occupying the HTTP semaphore.
	if err := sender.apiLimiter.Wait(context.Background()); err != nil {
		t.Fatalf("priming limiter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := sender.sendMessage(ctx, "chat-a", 0, "a", "HTML", false, "", "")
		done <- err
	}()

	// Give the goroutine time to enter the global-token wait. It must not have
	// occupied the sole HTTP slot while doing so.
	time.Sleep(20 * time.Millisecond)
	if got := len(sender.sem); got != 0 {
		t.Fatalf("HTTP semaphore occupied while waiting for global send token: len=%d", got)
	}
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("send error=%v, want deadline exceeded", err)
	}
}

func TestTelegramGlobalRateLimitWaitRespectsContextCancellation(t *testing.T) {
	limiter := mustTelegramSendRateLimiter(t, TelegramRateLimitConfig{
		GlobalRatePerSecond: 1,
		GlobalBurst:         1,
	})
	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("priming Wait: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := limiter.Wait(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait error=%v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("context cancellation took %v; limiter wait did not abort promptly", elapsed)
	}
}

func TestTelegramPermitCancellationWhileWaitingForSemaphoreDoesNotConsumeToken(t *testing.T) {
	limiter := mustTelegramSendRateLimiter(t, TelegramRateLimitConfig{
		GlobalRatePerSecond: 1,
		GlobalBurst:         1,
	})

	sem := make(chan struct{}, 1)
	sem <- struct{}{} // occupy the only wire slot
	gate := func(ctx context.Context) (func(), error) {
		select {
		case sem <- struct{}{}:
			return func() { <-sem }, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if release, err := limiter.WaitForPermit(ctx, gate); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("WaitForPermit error=%v, want deadline exceeded", err)
	}
	<-sem

	// A canceled gate wait must not consume the only send token. If it did,
	// this probe would have to wait about one second for a refill.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer probeCancel()
	if err := limiter.Wait(probeCtx); err != nil {
		t.Fatalf("canceled semaphore wait consumed a limiter token: %v", err)
	}
}

func TestTelegram429RetryStillUsesGlobalSendLimiter(t *testing.T) {
	attempt := 0
	server, sender, calls := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		attempt++
		if attempt == 1 {
			return http.StatusTooManyRequests, map[string]any{
				"ok":         false,
				"parameters": map[string]any{"retry_after": 1},
			}
		}
		return okResponse(77)
	})
	defer server.Close()

	if err := sender.SetRateLimits(TelegramRateLimitConfig{
		GlobalRatePerSecond: 1,
		GlobalBurst:         1,
	}); err != nil {
		t.Fatalf("SetRateLimits: %v", err)
	}

	result, err := sender.Send(context.Background(), &Job{
		Target:  "444",
		Type:    "telegram:user",
		Message: json.RawMessage(`{"content":"retry test"}`),
	})
	if err != nil {
		t.Fatalf("Send after 429: %v", err)
	}
	if result.ID != "444|text=77" {
		t.Fatalf("sent ID=%q, want 444|text=77", result.ID)
	}
	if got := len(*calls); got != 2 {
		t.Fatalf("wire calls=%d, want 2", got)
	}

	// The retry consumed the refilled token. Immediately asking for another
	// permit should therefore block rather than finding a full bucket.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := sender.apiLimiter.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("retry appears to have bypassed proactive limiting: probe err=%v", err)
	}
}

func TestTelegramEditsAndDeletesDoNotConsumeGlobalSendQuota(t *testing.T) {
	server, sender, calls := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		if method == "deleteMessage" {
			return http.StatusOK, map[string]any{"ok": true, "result": true}
		}
		return okResponse(1)
	})
	defer server.Close()

	if err := sender.SetRateLimits(TelegramRateLimitConfig{
		GlobalRatePerSecond: 1,
		GlobalBurst:         1,
	}); err != nil {
		t.Fatalf("SetRateLimits: %v", err)
	}
	// Exhaust the send bucket. Edit/delete must still go to the wire because
	// Telegram documents the global limit as a message/broadcast send limit.
	if err := sender.apiLimiter.Wait(context.Background()); err != nil {
		t.Fatalf("priming limiter: %v", err)
	}

	ctxEdit, cancelEdit := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelEdit()
	if err := sender.Edit(ctxEdit, "chat-a|text=1", json.RawMessage(`{"content":"edited","parse_mode":"HTML"}`), nil); err != nil {
		t.Fatalf("edit was incorrectly blocked by send limiter: %v", err)
	}
	ctxDelete, cancelDelete := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelDelete()
	if err := sender.Delete(ctxDelete, "chat-a|text=1"); err != nil {
		t.Fatalf("delete was incorrectly blocked by send limiter: %v", err)
	}

	ctxSend, cancelSend := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelSend()
	if _, err := sender.sendMessage(ctxSend, "chat-a", 0, "hello", "HTML", false, "", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("send unexpectedly bypassed exhausted global quota: err=%v", err)
	}

	gotMethods := make([]string, 0, len(*calls))
	for _, call := range *calls {
		gotMethods = append(gotMethods, call.Method)
	}
	want := []string{"editMessageText", "deleteMessage"}
	if len(gotMethods) != len(want) {
		t.Fatalf("methods=%v, want %v", gotMethods, want)
	}
	for i := range want {
		if gotMethods[i] != want[i] {
			t.Fatalf("methods=%v, want %v", gotMethods, want)
		}
	}
}

func TestTelegramLimiterDoesNotPrefetchTokensBehindSemaphore(t *testing.T) {
	var mu sync.Mutex
	var wireTimes []time.Time
	firstOnWire := make(chan struct{})
	releaseFirst := make(chan struct{})
	first := true

	server, sender, _ := setupTelegramServer(t, func(method string, body map[string]any) (int, any) {
		mu.Lock()
		wireTimes = append(wireTimes, time.Now())
		isFirst := first
		if first {
			first = false
		}
		mu.Unlock()

		if isFirst {
			close(firstOnWire)
			<-releaseFirst
		}
		return okResponse(1)
	})
	defer server.Close()

	sender.SetConcurrency(1)
	if err := sender.SetRateLimits(TelegramRateLimitConfig{
		GlobalRatePerSecond: 20, // one token every 50ms
		GlobalBurst:         1,
	}); err != nil {
		t.Fatalf("SetRateLimits: %v", err)
	}

	var wg sync.WaitGroup
	send := func(chatID string) {
		defer wg.Done()
		if _, err := sender.sendMessage(context.Background(), chatID, 0, "test", "HTML", false, "", ""); err != nil {
			t.Errorf("send to %s: %v", chatID, err)
		}
	}

	wg.Add(1)
	go send("chat-0")
	select {
	case <-firstOnWire:
	case <-time.After(time.Second):
		t.Fatal("first Telegram call never reached the wire")
	}

	// Keep the sole HTTP slot occupied long enough for an incorrect
	// implementation to pre-consume several future global tokens while queued.
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go send(fmt.Sprintf("chat-%d", i))
	}
	time.Sleep(225 * time.Millisecond)
	close(releaseFirst)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("queued Telegram calls did not complete")
	}

	mu.Lock()
	times := append([]time.Time(nil), wireTimes...)
	mu.Unlock()
	if len(times) != 4 {
		t.Fatalf("wire calls=%d, want 4", len(times))
	}
	for i := 2; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < 35*time.Millisecond {
			t.Fatalf("queued sends reached the wire only %v apart; limiter tokens were prefetched behind the semaphore", gap)
		}
	}
}
