package telegrambot

import (
	"context"
	"errors"
	"testing"

	gotgbot "github.com/go-telegram/bot"
)

var errRateLimitProbe = errors.New("rate-limit probe")

type rejectingTelegramRateLimiter struct {
	calls int
}

func (l *rejectingTelegramRateLimiter) Wait(_ context.Context) error {
	l.calls++
	return errRateLimitProbe
}

func TestTopicSendHelpersUseSharedGlobalRateLimiter(t *testing.T) {
	limiter := &rejectingTelegramRateLimiter{}
	b := &Bot{rateLimiter: limiter}

	if _, err := b.sendTopicMessage(-100123, 77, "text"); !errors.Is(err, errRateLimitProbe) {
		t.Fatalf("sendTopicMessage error=%v, want rate-limit probe", err)
	}
	if err := b.sendMarkdownToTopic(-100123, 88, "text"); !errors.Is(err, errRateLimitProbe) {
		t.Fatalf("sendMarkdownToTopic error=%v, want rate-limit probe", err)
	}
	if err := b.sendPhotoURLToTopic(-100123, 99, "https://example.test/photo.jpg", "caption"); !errors.Is(err, errRateLimitProbe) {
		t.Fatalf("sendPhotoURLToTopic error=%v, want rate-limit probe", err)
	}
	if err := b.sendDocumentBytesToTopic(-100123, 111, "test.txt", []byte("data"), "caption"); !errors.Is(err, errRateLimitProbe) {
		t.Fatalf("sendDocumentBytesToTopic error=%v, want rate-limit probe", err)
	}

	if limiter.calls != 4 {
		t.Fatalf("rate limiter calls=%d, want 4", limiter.calls)
	}
}

func TestReconciliationSendUsesSharedGlobalRateLimiter(t *testing.T) {
	limiter := &rejectingTelegramRateLimiter{}
	r := &TelegramReconciliation{rateLimiter: limiter}

	_, err := r.sendMessage(context.Background(), &gotgbot.SendMessageParams{
		ChatID: int64(-100456),
		Text:   "test",
	})
	if !errors.Is(err, errRateLimitProbe) {
		t.Fatalf("sendMessage error=%v, want rate-limit probe", err)
	}
	if limiter.calls != 1 {
		t.Fatalf("rate limiter calls=%d, want 1", limiter.calls)
	}
}
