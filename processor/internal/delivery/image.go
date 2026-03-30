package delivery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// DownloadImage fetches an image from the given URL with a 10-second timeout.
func DownloadImage(client *http.Client, url string) ([]byte, error) {
	if client == nil {
		client = &http.Client{}
	}
	origTimeout := client.Timeout
	client.Timeout = 10 * time.Second
	defer func() { client.Timeout = origTimeout }()

	resp, err := client.Get(url) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("image download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image download returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading image body: %w", err)
	}

	return data, nil
}

// NormalizeDiscordMessage normalizes a Discord message JSON payload:
//   - Converts singular "embed" to "embeds" array
//   - Coerces string color values in embeds to integers
func NormalizeDiscordMessage(raw json.RawMessage) (json.RawMessage, error) {
	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("parsing discord message: %w", err)
	}

	// Convert singular embed to embeds array.
	if embed, ok := msg["embed"]; ok {
		if _, hasEmbeds := msg["embeds"]; !hasEmbeds {
			msg["embeds"] = []any{embed}
		}
		delete(msg, "embed")
	}

	// Coerce color strings in each embed.
	if embeds, ok := msg["embeds"].([]any); ok {
		for _, e := range embeds {
			if embed, ok := e.(map[string]any); ok {
				coerceEmbedColor(embed)
			}
		}
	}

	return marshalNoEscape(msg)
}

// coerceEmbedColor converts a string "color" field to an integer in-place.
func coerceEmbedColor(embed map[string]any) {
	colorVal, ok := embed["color"]
	if !ok {
		return
	}

	str, ok := colorVal.(string)
	if !ok {
		// Already a number (json.Unmarshal gives float64), leave as-is.
		return
	}

	stripped := strings.TrimPrefix(str, "#")
	if isHexColor(stripped) {
		if n, err := strconv.ParseInt(stripped, 16, 64); err == nil {
			embed["color"] = n
			return
		}
	}

	// Try decimal.
	if n, err := strconv.ParseInt(str, 10, 64); err == nil {
		embed["color"] = n
	}
	// If nothing parsed, leave the original string.
}

// isHexColor returns true if s is exactly 6 hex characters.
func isHexColor(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ExtractEmbedImageURL returns embeds[0].image.url from a Discord message, or "".
func ExtractEmbedImageURL(raw json.RawMessage) string {
	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		return ""
	}

	embeds, ok := msg["embeds"].([]any)
	if !ok || len(embeds) == 0 {
		return ""
	}

	embed, ok := embeds[0].(map[string]any)
	if !ok {
		return ""
	}

	image, ok := embed["image"].(map[string]any)
	if !ok {
		return ""
	}

	url, _ := image["url"].(string)
	return url
}

// ReplaceEmbedImageURL sets embeds[0].image.url to "attachment://map.png".
// Returns the original payload if parsing or re-serialization fails.
func ReplaceEmbedImageURL(raw json.RawMessage) json.RawMessage {
	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		return raw
	}

	embeds, ok := msg["embeds"].([]any)
	if !ok || len(embeds) == 0 {
		return raw
	}

	embed, ok := embeds[0].(map[string]any)
	if !ok {
		return raw
	}

	image, ok := embed["image"].(map[string]any)
	if !ok {
		image = map[string]any{}
		embed["image"] = image
	}

	image["url"] = "attachment://map.png"

	result, err := marshalNoEscape(msg)
	if err != nil {
		return raw
	}
	return result
}

// BuildMultipartMessage creates a multipart form body containing the message JSON
// as "payload_json" and the image data as a file attachment.
// fieldName is "files[0]" for bot sends or "file" for webhook sends.
func BuildMultipartMessage(payload json.RawMessage, imageData []byte, fieldName string) (body *bytes.Buffer, contentType string, err error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	// Add payload_json part.
	jsonPart, err := writer.CreateFormField("payload_json")
	if err != nil {
		return nil, "", fmt.Errorf("creating payload_json field: %w", err)
	}
	if _, err := jsonPart.Write(payload); err != nil {
		return nil, "", fmt.Errorf("writing payload_json: %w", err)
	}

	// Add file part.
	filePart, err := createFilePart(writer, fieldName, "map.png", "image/png")
	if err != nil {
		return nil, "", fmt.Errorf("creating file part: %w", err)
	}
	if _, err := filePart.Write(imageData); err != nil {
		return nil, "", fmt.Errorf("writing image data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("closing multipart writer: %w", err)
	}

	return buf, writer.FormDataContentType(), nil
}

// createFilePart creates a multipart file part with explicit Content-Type.
func createFilePart(w *multipart.Writer, fieldName, filename, ct string) (io.Writer, error) {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, filename))
	h.Set("Content-Type", ct)
	return w.CreatePart(h)
}

// marshalNoEscape serializes v to JSON without HTML escaping.
func marshalNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encode appends a newline; trim it.
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return json.RawMessage(b), nil
}
