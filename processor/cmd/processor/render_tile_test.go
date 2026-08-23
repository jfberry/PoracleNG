package main

import (
	"encoding/json"
	"testing"
)

// TestTileBytesForMessage covers the "wrong image uploaded" bug: the batch's
// inline tile bytes must only be attached to a message whose embed image IS the
// resolved staticMap tile URL. Templates that render a static image URL or a
// hand-built tileserver URL must keep their own image (bytes returned nil so
// delivery downloads the message's real image).
func TestTileBytesForMessage(t *testing.T) {
	const tileURL = "https://tiles.example.com/multistaticmap/pregenerated/abc123"
	tileBytes := []byte("PNGDATA")

	msg := func(imageURL string) json.RawMessage {
		return json.RawMessage(`{"embed":{"image":{"url":"` + imageURL + `"}}}`)
	}

	tests := []struct {
		name      string
		message   json.RawMessage
		tileBytes []byte
		tileURL   string
		wantBytes bool
	}{
		{
			name:      "template uses {{staticMap}} — image is the tile → attach bytes",
			message:   msg(tileURL),
			tileBytes: tileBytes,
			tileURL:   tileURL,
			wantBytes: true,
		},
		{
			name:      "static image URL → keep it, no tile bytes",
			message:   msg("https://i.ibb.co/Tty3NHn/demo-en.png"),
			tileBytes: tileBytes,
			tileURL:   tileURL,
			wantBytes: false,
		},
		{
			name:      "hand-built tileserver URL (trips UsesTile) → keep it, no tile bytes",
			message:   msg("https://cache.darkcode.sx/staticmap/poracle-monster?imgUrl=x&latitude=1&longitude=2&style=klokantech-basic"),
			tileBytes: tileBytes,
			tileURL:   tileURL,
			wantBytes: false,
		},
		{
			name:      "embeds-array form is handled too",
			message:   json.RawMessage(`{"embeds":[{"image":{"url":"https://i.ibb.co/other.png"}}]}`),
			tileBytes: tileBytes,
			tileURL:   tileURL,
			wantBytes: false,
		},
		{
			name:      "no tile bytes → passthrough nil",
			message:   msg(tileURL),
			tileBytes: nil,
			tileURL:   tileURL,
			wantBytes: false,
		},
		{
			name:      "no tile URL (skip mode) → passthrough bytes unchanged",
			message:   msg("https://i.ibb.co/anything.png"),
			tileBytes: tileBytes,
			tileURL:   "",
			wantBytes: true,
		},
		{
			name:      "message with no embed image → no bytes (nothing to replace)",
			message:   json.RawMessage(`{"content":"hi"}`),
			tileBytes: tileBytes,
			tileURL:   tileURL,
			wantBytes: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tileBytesForMessage(tc.message, tc.tileBytes, tc.tileURL)
			if tc.wantBytes && len(got) == 0 {
				t.Errorf("expected tile bytes to be attached, got none")
			}
			if !tc.wantBytes && len(got) != 0 {
				t.Errorf("expected no tile bytes, got %d bytes", len(got))
			}
		})
	}
}
