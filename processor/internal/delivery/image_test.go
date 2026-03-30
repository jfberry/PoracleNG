package delivery

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeEmbedToEmbeds(t *testing.T) {
	input := json.RawMessage(`{"content":"hello","embed":{"title":"test","color":123}}`)
	result, err := NormalizeDiscordMessage(input)
	if err != nil {
		t.Fatal(err)
	}

	var msg map[string]any
	if err := json.Unmarshal(result, &msg); err != nil {
		t.Fatal(err)
	}

	if _, ok := msg["embed"]; ok {
		t.Error("embed key should be removed")
	}

	embeds, ok := msg["embeds"].([]any)
	if !ok || len(embeds) != 1 {
		t.Fatalf("expected embeds array of length 1, got %v", msg["embeds"])
	}

	embed := embeds[0].(map[string]any)
	if embed["title"] != "test" {
		t.Errorf("expected title 'test', got %v", embed["title"])
	}
}

func TestNormalizeColorHex(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  float64
	}{
		{"hash prefix", `{"embeds":[{"color":"#A040A0"}]}`, 10502304},
		{"bare hex", `{"embeds":[{"color":"A040A0"}]}`, 10502304},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := NormalizeDiscordMessage(json.RawMessage(tc.input))
			if err != nil {
				t.Fatal(err)
			}

			var msg map[string]any
			if err := json.Unmarshal(result, &msg); err != nil {
				t.Fatal(err)
			}

			embeds := msg["embeds"].([]any)
			embed := embeds[0].(map[string]any)
			// json.Unmarshal gives float64 for numbers.
			color, ok := embed["color"].(float64)
			if !ok {
				t.Fatalf("color is not a number: %T %v", embed["color"], embed["color"])
			}
			if color != tc.want {
				t.Errorf("expected %v, got %v", tc.want, color)
			}
		})
	}
}

func TestNormalizeColorDecimal(t *testing.T) {
	input := json.RawMessage(`{"embeds":[{"color":"1216493"}]}`)
	result, err := NormalizeDiscordMessage(input)
	if err != nil {
		t.Fatal(err)
	}

	var msg map[string]any
	if err := json.Unmarshal(result, &msg); err != nil {
		t.Fatal(err)
	}

	embeds := msg["embeds"].([]any)
	embed := embeds[0].(map[string]any)
	color, ok := embed["color"].(float64)
	if !ok {
		t.Fatalf("color is not a number: %T %v", embed["color"], embed["color"])
	}
	if color != 1216493 {
		t.Errorf("expected 1216493, got %v", color)
	}
}

func TestNormalizeColorAlreadyInt(t *testing.T) {
	input := json.RawMessage(`{"embeds":[{"color":1216493}]}`)
	result, err := NormalizeDiscordMessage(input)
	if err != nil {
		t.Fatal(err)
	}

	var msg map[string]any
	if err := json.Unmarshal(result, &msg); err != nil {
		t.Fatal(err)
	}

	embeds := msg["embeds"].([]any)
	embed := embeds[0].(map[string]any)
	color, ok := embed["color"].(float64)
	if !ok {
		t.Fatalf("color is not a number: %T %v", embed["color"], embed["color"])
	}
	if color != 1216493 {
		t.Errorf("expected 1216493, got %v", color)
	}
}

func TestExtractEmbedImageURL(t *testing.T) {
	input := json.RawMessage(`{"embeds":[{"image":{"url":"https://example.com/tile.png"}}]}`)
	url := ExtractEmbedImageURL(input)
	if url != "https://example.com/tile.png" {
		t.Errorf("expected https://example.com/tile.png, got %q", url)
	}
}

func TestExtractEmbedImageURLMissing(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"no embeds", `{"content":"hello"}`},
		{"empty embeds", `{"embeds":[]}`},
		{"no image", `{"embeds":[{"title":"test"}]}`},
		{"no url in image", `{"embeds":[{"image":{}}]}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := ExtractEmbedImageURL(json.RawMessage(tc.input))
			if url != "" {
				t.Errorf("expected empty string, got %q", url)
			}
		})
	}
}

func TestReplaceEmbedImageURL(t *testing.T) {
	input := json.RawMessage(`{"embeds":[{"image":{"url":"https://example.com/tile.png"},"title":"test"}]}`)
	result := ReplaceEmbedImageURL(input)

	var msg map[string]any
	if err := json.Unmarshal(result, &msg); err != nil {
		t.Fatal(err)
	}

	embeds := msg["embeds"].([]any)
	embed := embeds[0].(map[string]any)
	image := embed["image"].(map[string]any)
	url := image["url"].(string)
	if url != "attachment://map.png" {
		t.Errorf("expected attachment://map.png, got %q", url)
	}

	// Verify other fields preserved.
	if embed["title"] != "test" {
		t.Errorf("expected title preserved, got %v", embed["title"])
	}
}

func TestBuildMultipart(t *testing.T) {
	payload := json.RawMessage(`{"content":"hello","embeds":[{"image":{"url":"attachment://map.png"}}]}`)
	imageData := []byte{0x89, 0x50, 0x4e, 0x47} // PNG magic bytes

	body, contentType, err := BuildMultipartMessage(payload, imageData, "files[0]")
	if err != nil {
		t.Fatal(err)
	}

	// Parse the content type to get the boundary.
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		t.Fatalf("expected multipart content type, got %s", mediaType)
	}

	reader := multipart.NewReader(body, params["boundary"])

	// First part: payload_json.
	part1, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if part1.FormName() != "payload_json" {
		t.Errorf("expected payload_json, got %s", part1.FormName())
	}
	jsonBytes, _ := io.ReadAll(part1)
	if string(jsonBytes) != string(payload) {
		t.Errorf("payload mismatch: %s", jsonBytes)
	}

	// Second part: file.
	part2, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if part2.FormName() != "files[0]" {
		t.Errorf("expected files[0], got %s", part2.FormName())
	}
	if part2.FileName() != "map.png" {
		t.Errorf("expected map.png, got %s", part2.FileName())
	}
	fileBytes, _ := io.ReadAll(part2)
	if len(fileBytes) != 4 {
		t.Errorf("expected 4 bytes, got %d", len(fileBytes))
	}

	// No more parts.
	_, err = reader.NextPart()
	if err != io.EOF {
		t.Error("expected only 2 parts")
	}
}

func TestDownloadImage(t *testing.T) {
	expected := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(expected) //nolint:errcheck
	}))
	defer srv.Close()

	data, err := DownloadImage(srv.Client(), srv.URL+"/tile.png")
	if err != nil {
		t.Fatal(err)
	}

	if len(data) != len(expected) {
		t.Fatalf("expected %d bytes, got %d", len(expected), len(data))
	}
	for i, b := range data {
		if b != expected[i] {
			t.Errorf("byte %d: expected %02x, got %02x", i, expected[i], b)
		}
	}
}

func TestDownloadImageError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := DownloadImage(srv.Client(), srv.URL+"/missing.png")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}
