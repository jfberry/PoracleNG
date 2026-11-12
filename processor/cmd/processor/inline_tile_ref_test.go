package main

import (
	"encoding/json"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
)

// The inline-mode stand-in written into {{staticMap}} when no fallback is
// configured must be the reference the Discord sender's upload resolves.
func TestInlineImageRefMatchesDelivery(t *testing.T) {
	if staticmap.InlineImageRef != delivery.InlineImageRef {
		t.Fatalf("staticmap.InlineImageRef %q != delivery.InlineImageRef %q",
			staticmap.InlineImageRef, delivery.InlineImageRef)
	}
}

// With an empty [fallbacks] static_map, a message that renders {{staticMap}}
// gets the inline stand-in and must still be handed the tile bytes.
func TestTileBytesForMessage_InlineStandIn(t *testing.T) {
	msg := json.RawMessage(`{"embed":{"image":{"url":"` + staticmap.InlineImageRef + `"}}}`)
	if got := tileBytesForMessage(msg, []byte("PNG"), staticmap.InlineImageRef); got == nil {
		t.Fatal("tile bytes dropped for a message rendering the inline stand-in")
	}
}
