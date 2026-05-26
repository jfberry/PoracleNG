package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/dts"
)

// helpDTSWithTrack builds a TemplateStore that has a help/track template,
// so detailedHelpSuffix will fire for usageKey "msg.track.usage".
func helpDTSWithTrack(t *testing.T) *dts.TemplateStore {
	t.Helper()
	dir := t.TempDir()
	// Minimal DTS entry: type=help, id=track, platform=discord, language=en.
	// The template only needs to be valid JSON — content doesn't matter for
	// the suffix check (Get just tests for existence).
	entry := `[{
		"type": "help",
		"id": "track",
		"platform": "discord",
		"language": "en",
		"default": false,
		"template": {"embed": {"title": "Track help"}}
	}]`
	if err := os.WriteFile(dir+"/dts.json", []byte(entry), 0644); err != nil {
		t.Fatal(err)
	}
	ts, err := dts.LoadTemplates(dir, dir)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	return ts
}

// helpDTSEmpty builds a TemplateStore with no help templates at all.
func helpDTSEmpty(t *testing.T) *dts.TemplateStore {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/dts.json", []byte("[]"), 0644); err != nil {
		t.Fatal(err)
	}
	ts, err := dts.LoadTemplates(dir, dir)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	return ts
}

// TestUsageReply_AppendsHintWhenDTSHelpExists verifies that usageReply adds
// a "More detailed help" suffix when a help DTS template exists for the topic.
func TestUsageReply_AppendsHintWhenDTSHelpExists(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.DTS = helpDTSWithTrack(t)

	reply := usageReply(ctx, nil, "msg.track.usage")
	if reply == nil {
		t.Fatal("expected reply")
	}
	if !strings.Contains(reply.Text, "help") || !strings.Contains(reply.Text, "track") {
		t.Fatalf("expected detailed-help hint mentioning 'help' and 'track', got: %q", reply.Text)
	}
}

// TestUsageReply_NoHintWhenDTSHelpAbsent verifies that no hint is appended
// when the DTS is wired but has no help template for the topic.
func TestUsageReply_NoHintWhenDTSHelpAbsent(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.DTS = helpDTSEmpty(t)

	reply := usageReply(ctx, nil, "msg.nonexistent.usage")
	if reply == nil {
		t.Fatal("expected reply")
	}
	if strings.Contains(reply.Text, "More detailed help") {
		t.Fatalf("should not append hint when no DTS help exists, got: %q", reply.Text)
	}
}

// TestUsageReply_NoHintWhenDTSNil verifies that no hint is appended
// when ctx.DTS is nil (DTS system not configured).
func TestUsageReply_NoHintWhenDTSNil(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.DTS = nil

	reply := usageReply(ctx, nil, "msg.track.usage")
	if reply == nil {
		t.Fatal("expected reply")
	}
	if strings.Contains(reply.Text, "More detailed help") {
		t.Fatalf("should not append hint when DTS not configured, got: %q", reply.Text)
	}
}

// TestTopicFromUsageKey verifies topicFromUsageKey extraction for all edge cases.
func TestTopicFromUsageKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"msg.track.usage", "track"},
		{"msg.location.usage", "location"},
		{"msg.poracle_admin.usage", "poracle_admin"},
		{"cmd.track", ""},
		{"not.a.usage.key", ""},
		{"msg.usage", ""},
		{"msg..usage", ""},
	}
	for _, c := range cases {
		got := topicFromUsageKey(c.in)
		if got != c.want {
			t.Errorf("topicFromUsageKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
