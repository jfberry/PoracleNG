package commands

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/geocoding"
)

func TestCache_HelpNoArgs(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	// "cache" with no further args → group help
	replies := cmd.Run(ctx, []string{"cache"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	for _, want := range []string{"stats", "clear"} {
		if !containsStr(text, want) {
			t.Errorf("cache help missing subcommand %q, got:\n%s", want, text)
		}
	}
}

func TestCache_Stats_Renders(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.GeocoderStats = func() geocoding.CacheStats {
		return geocoding.CacheStats{
			MemoryEntries: 1234,
			DiskEntries:   45678,
			HitsMemory:    10000,
			HitsDisk:      2500,
			Misses:        3200,
		}
	}

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"cache", "stats"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text

	for _, want := range []string{"1234", "45678", "10000", "2500", "3200"} {
		if !containsStr(text, want) {
			t.Errorf("stats reply missing value %q, got:\n%s", want, text)
		}
	}
}

func TestCache_Stats_ZeroDivisionGuard(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.GeocoderStats = func() geocoding.CacheStats {
		return geocoding.CacheStats{
			MemoryEntries: 0,
			DiskEntries:   0,
			HitsMemory:    0,
			HitsDisk:      0,
			Misses:        0,
		}
	}

	cmd := &PoracleAdminCommand{}
	// Must not panic; hit rate should be 0.
	replies := cmd.Run(ctx, []string{"cache", "stats"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	// Hit rate is 0% when no data.
	if !containsStr(text, "0") {
		t.Errorf("stats reply should contain '0' for zero counts, got:\n%s", text)
	}
}

func TestCache_ClearGeocoder_Success(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.GeocoderClear = func() int { return 42 }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"cache", "clear", "geocoder"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "42") {
		t.Errorf("clear geocoder reply missing count '42', got:\n%s", text)
	}
}

func TestCache_ClearWithoutSpecifier(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"cache", "clear"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	// Should mention "geocoder" in the usage hint.
	if !containsStr(text, "geocoder") {
		t.Errorf("usage hint should mention 'geocoder', got:\n%s", text)
	}
}

func TestCache_UnknownSub(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"cache", "bogus"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if text == "" {
		t.Error("unknown-sub reply must be non-empty")
	}
	if !containsStr(text, "cache") {
		t.Errorf("unknown-sub reply should mention 'cache', got:\n%s", text)
	}
}

func TestCache_NotConfigured_Stats(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.GeocoderStats = nil // not configured

	cmd := &PoracleAdminCommand{}
	// Must not panic.
	replies := cmd.Run(ctx, []string{"cache", "stats"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if text == "" {
		t.Error("nil GeocoderStats should return non-empty reply")
	}
	// Should say "not configured" (or equivalent).
	if !containsStr(text, "not configured") && !containsStr(text, "nicht konfiguriert") {
		t.Errorf("expected 'not configured' message, got:\n%s", text)
	}
}

func TestCache_NotConfigured_Clear(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.GeocoderClear = nil // not configured

	cmd := &PoracleAdminCommand{}
	// Must not panic.
	replies := cmd.Run(ctx, []string{"cache", "clear", "geocoder"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if text == "" {
		t.Error("nil GeocoderClear should return non-empty reply")
	}
	if !containsStr(text, "not configured") && !containsStr(text, "nicht konfiguriert") {
		t.Errorf("expected 'not configured' message, got:\n%s", text)
	}
}
