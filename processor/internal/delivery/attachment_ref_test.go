package delivery

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureDiscord records the payload JSON (from a JSON body or the
// payload_json multipart part) and whether a file part was sent.
type captureDiscord struct {
	payload map[string]any
	hasFile bool
}

func (c *captureDiscord) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		var raw []byte
		if strings.HasPrefix(mt, "multipart/") {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err != nil {
					break
				}
				b, _ := io.ReadAll(p)
				if p.FormName() == "payload_json" {
					raw = b
				} else {
					c.hasFile = true
				}
			}
		} else {
			raw, _ = io.ReadAll(r.Body)
		}
		if err := json.Unmarshal(raw, &c.payload); err != nil {
			t.Errorf("bad payload %q: %v", raw, err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"m1"}`)) //nolint:errcheck
	}
}

func firstEmbedImage(payload map[string]any) (string, bool) {
	embeds, _ := payload["embeds"].([]any)
	if len(embeds) == 0 {
		return "", false
	}
	embed, _ := embeds[0].(map[string]any)
	image, ok := embed["image"].(map[string]any)
	if !ok {
		return "", false
	}
	url, _ := image["url"].(string)
	return url, true
}

const attachmentRefMsg = `{"embeds":[{"title":"t","image":{"url":"attachment://map.png"}}]}`

// The inline-mode stand-in with tile bytes is uploaded: a file part plus an
// embed image pointing at it.
func TestDiscordSend_AttachmentRefWithBytesUploads(t *testing.T) {
	for _, typ := range []string{"discord:channel", "webhook"} {
		t.Run(typ, func(t *testing.T) {
			c := &captureDiscord{}
			server := httptest.NewServer(c.handler(t))
			defer server.Close()
			ds := newTestDiscordSender(server.URL)
			ds.uploadImages = true
			target := "chan"
			if typ == "webhook" {
				target = server.URL + "/api/webhooks/1/abc"
			}
			_, err := ds.Send(context.Background(), &Job{Target: target, Type: typ,
				Message: json.RawMessage(attachmentRefMsg), StaticMapData: []byte{0x89, 'P', 'N', 'G'}})
			if err != nil {
				t.Fatalf("send: %v", err)
			}
			if !c.hasFile {
				t.Error("tile bytes were not uploaded")
			}
			if url, _ := firstEmbedImage(c.payload); url != InlineImageRef {
				t.Errorf("embed image = %q, want %q", url, InlineImageRef)
			}
		})
	}
}

// Without bytes, an attachment:// image can't resolve: it is dropped, not
// sent dangling and not "downloaded".
func TestDiscordSend_AttachmentRefWithoutBytesDropsImage(t *testing.T) {
	for _, typ := range []string{"discord:channel", "webhook"} {
		t.Run(typ, func(t *testing.T) {
			c := &captureDiscord{}
			server := httptest.NewServer(c.handler(t))
			defer server.Close()
			ds := newTestDiscordSender(server.URL)
			ds.uploadImages = true
			target := "chan"
			if typ == "webhook" {
				target = server.URL + "/api/webhooks/1/abc"
			}
			_, err := ds.Send(context.Background(), &Job{Target: target, Type: typ,
				Message: json.RawMessage(attachmentRefMsg)})
			if err != nil {
				t.Fatalf("send: %v", err)
			}
			if c.hasFile {
				t.Error("unexpected file part")
			}
			if url, ok := firstEmbedImage(c.payload); ok {
				t.Errorf("embed image %q should have been dropped", url)
			}
			if c.payload["embeds"] == nil {
				t.Error("embed itself should be kept")
			}
		})
	}
}

func TestDiscordEdit_AttachmentRef(t *testing.T) {
	for _, withBytes := range []bool{true, false} {
		c := &captureDiscord{}
		server := httptest.NewServer(c.handler(t))
		ds := newTestDiscordSender(server.URL)
		ds.uploadImages = true
		var data []byte
		if withBytes {
			data = []byte{0x89, 'P', 'N', 'G'}
		}
		if err := ds.Edit(context.Background(), "chan:m1", json.RawMessage(attachmentRefMsg), data); err != nil {
			t.Fatalf("edit (bytes=%v): %v", withBytes, err)
		}
		url, ok := firstEmbedImage(c.payload)
		if withBytes && (!c.hasFile || url != InlineImageRef) {
			t.Errorf("with bytes: hasFile=%v image=%q", c.hasFile, url)
		}
		if !withBytes && (c.hasFile || ok) {
			t.Errorf("without bytes: hasFile=%v image=%q (want dropped)", c.hasFile, url)
		}
		server.Close()
	}
}

// When not extracting (upload off, interaction responses) nothing can be
// uploaded, so attachment:// images are dropped; ordinary URLs are kept.
func TestNormalize_DropsAttachmentImageWhenNotExtracting(t *testing.T) {
	out, _, err := NormalizeAndExtractImage(json.RawMessage(attachmentRefMsg), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	_ = json.Unmarshal(out, &payload)
	if url, ok := firstEmbedImage(payload); ok {
		t.Errorf("attachment image %q kept", url)
	}

	out, imageURL, err := NormalizeAndExtractImage(json.RawMessage(attachmentRefMsg), true)
	if err != nil {
		t.Fatal(err)
	}
	if imageURL != InlineImageRef {
		t.Errorf("extracting: imageURL = %q, want %q", imageURL, InlineImageRef)
	}
	_ = json.Unmarshal(out, &payload)
	if _, ok := firstEmbedImage(payload); !ok {
		t.Error("extracting: image should be kept for the sender")
	}

	plain := `{"embeds":[{"title":"t","image":{"url":"https://x/y.png"}}]}`
	out, _, _ = NormalizeAndExtractImage(json.RawMessage(plain), false)
	_ = json.Unmarshal(out, &payload)
	if url, _ := firstEmbedImage(payload); url != "https://x/y.png" {
		t.Errorf("plain image changed to %q", url)
	}
}
