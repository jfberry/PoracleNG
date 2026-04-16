package main

import (
	"bytes"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

func captureLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := log.StandardLogger().Out
	prevLevel := log.StandardLogger().Level
	log.SetOutput(&buf)
	log.SetLevel(log.InfoLevel)
	defer func() {
		log.SetOutput(prev)
		log.SetLevel(prevLevel)
	}()
	fn()
	return buf.String()
}

func TestValidateCommunityAreas(t *testing.T) {
	fences := []geofence.Fence{
		{Name: "Japan-Wakayama-Arida"},
		{Name: "Japan-Wakayama-Naka"},
		{Name: "Japan"},
	}
	communities := []config.CommunityConfig{{
		Name:          "Japan-Wakayama",
		AllowedAreas:  []string{"japan-wakayama-arida", "Japan-Wakayama-Typo", "Japan-Wakayama-Naka"},
		LocationFence: config.FlexStrings{"Japan", "missing-fence"},
	}}

	out := captureLogs(t, func() {
		validateCommunityAreas(fences, communities)
	})

	// logrus's text formatter wraps messages in msg="..." which backslash-
	// escapes embedded quotes, so the checks target the escaped form.
	// allowed_areas: 2 valid, 1 typo
	if !strings.Contains(out, `Japan-Wakayama-Typo`) || !strings.Contains(out, "NOT FOUND in loaded geofences") {
		t.Errorf("expected warning for typo'd allowed_areas, got:\n%s", out)
	}
	if !strings.Contains(out, "allowed_areas: 2/3 resolved") {
		t.Errorf("expected 2/3 summary, got:\n%s", out)
	}
	// location_fence: 1 valid, 1 missing
	if !strings.Contains(out, "Japan") || !strings.Contains(out, "✓") {
		t.Errorf("expected ✓ for valid location_fence, got:\n%s", out)
	}
	if !strings.Contains(out, `missing-fence`) {
		t.Errorf("expected warning for missing location_fence, got:\n%s", out)
	}
}

func TestValidateCommunityAreasEmpty(t *testing.T) {
	out := captureLogs(t, func() {
		validateCommunityAreas(nil, nil)
	})
	if out != "" {
		t.Errorf("expected no output for empty config, got:\n%s", out)
	}
}
