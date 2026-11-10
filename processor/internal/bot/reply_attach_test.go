package bot

import (
	"strings"
	"testing"
)

// lines builds a text of n newline-separated rows, each ~width chars, so we can
// drive SplitOrAttachReply across the message-count threshold deterministically.
func makeLines(n, width int) string {
	row := strings.Repeat("x", width)
	rows := make([]string, n)
	for i := range rows {
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

func TestSplitOrAttachReply_ShortStaysInline(t *testing.T) {
	// Small text → a single inline Reply with Text, no attachment.
	replies := SplitOrAttachReply("just a few\nshort lines", "tracked.txt", "see attached")
	if len(replies) != 1 {
		t.Fatalf("expected 1 inline reply, got %d", len(replies))
	}
	if replies[0].Attachment != nil {
		t.Fatalf("short text must not attach; got attachment %q", replies[0].Attachment.Filename)
	}
	if replies[0].Text == "" {
		t.Fatalf("inline reply should carry the text")
	}
}

func TestSplitOrAttachReply_AtThresholdStaysInline(t *testing.T) {
	// Exactly maxInlineReplyChunks (3) 2000-char messages → still inline, no attach.
	// Each line ~1900 chars so each line is its own chunk.
	text := makeLines(maxInlineReplyChunks, 1900)
	replies := SplitOrAttachReply(text, "tracked.txt", "see attached")
	if len(replies) != maxInlineReplyChunks {
		t.Fatalf("expected %d inline chunks, got %d", maxInlineReplyChunks, len(replies))
	}
	for i, r := range replies {
		if r.Attachment != nil {
			t.Fatalf("chunk %d should not attach at threshold", i)
		}
	}
}

func TestSplitOrAttachReply_OverThresholdAttaches(t *testing.T) {
	// More than maxInlineReplyChunks messages → one Reply with the full text
	// as an attachment and the supplied attach message inline.
	text := makeLines(maxInlineReplyChunks+2, 1900)
	replies := SplitOrAttachReply(text, "tracked.txt", "your list is long — see attached")
	if len(replies) != 1 {
		t.Fatalf("expected a single attachment reply, got %d replies", len(replies))
	}
	r := replies[0]
	if r.Attachment == nil {
		t.Fatalf("long text must attach")
	}
	if r.Attachment.Filename != "tracked.txt" {
		t.Fatalf("filename = %q, want tracked.txt", r.Attachment.Filename)
	}
	if string(r.Attachment.Content) != text {
		t.Fatalf("attachment must carry the full untruncated text (got %d bytes, want %d)",
			len(r.Attachment.Content), len(text))
	}
	if r.Text != "your list is long — see attached" {
		t.Fatalf("inline text should be the attach message, got %q", r.Text)
	}
}

func TestSplitCodeBlockReply_FencesEveryChunk(t *testing.T) {
	line := strings.Repeat("default_edit = x ", 10)
	text := strings.TrimSpace(strings.Repeat(line+"\n", 300))

	replies := SplitCodeBlockReply(text)
	if len(replies) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(replies))
	}
	for i, r := range replies {
		if !strings.HasPrefix(r.Text, "```\n") || !strings.HasSuffix(r.Text, "\n```") {
			t.Errorf("chunk %d not fenced: %q", i, r.Text[:20])
		}
		if len(r.Text) > 2000 {
			t.Errorf("chunk %d is %d chars, over the 2000 limit", i, len(r.Text))
		}
	}
}

func TestSplitCodeBlockReply_EscapesBackticks(t *testing.T) {
	replies := SplitCodeBlockReply("a = \"```\"")
	if got, want := replies[0].Text, "```\na = \"\\`\\`\\`\"\n```"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
