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
// It creates a dedicated client to avoid mutating the shared client's timeout.
func DownloadImage(client *http.Client, url string) ([]byte, error) {
	// Use a separate client with short timeout for image downloads.
	// Reuse the transport for connection pooling.
	var transport http.RoundTripper
	if client != nil {
		transport = client.Transport
	}
	imgClient := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	resp, err := imgClient.Get(url) //nolint:noctx
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

// NormalizeAndExtractImage normalizes a Discord message JSON payload and optionally
// extracts the embed image URL — all in a single JSON parse. Returns the normalized
// JSON and the image URL (empty if none or extractImage is false).
//
// Normalization:
//   - Converts singular "embed" to "embeds" array
//   - Coerces string color values in embeds to integers
//   - Sanitizes embeds against Discord's validation rules (drops empty
//     fields, replaces blank field names/values with zero-width space,
//     truncates over-long strings, removes empty image/footer/author
//     objects, drops embeds that would render as completely empty)
func NormalizeAndExtractImage(raw json.RawMessage, extractImage bool) (json.RawMessage, string, error) {
	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, "", fmt.Errorf("parsing discord message: %w", err)
	}

	// Convert singular embed to embeds array.
	if embed, ok := msg["embed"]; ok {
		if _, hasEmbeds := msg["embeds"]; !hasEmbeds {
			msg["embeds"] = []any{embed}
		}
		delete(msg, "embed")
	}

	// Sanitize and extract image URL.
	var imageURL string
	if embeds, ok := msg["embeds"].([]any); ok {
		// Cap at 10 embeds (Discord max).
		if len(embeds) > maxDiscordEmbeds {
			embeds = embeds[:maxDiscordEmbeds]
		}
		var sanitized []any
		for i, e := range embeds {
			embed, ok := e.(map[string]any)
			if !ok {
				continue
			}
			coerceEmbedColor(embed)
			if sanitizeDiscordEmbed(embed) {
				sanitized = append(sanitized, embed)
			}
			// Extract image URL from first embed (same parse pass).
			if extractImage && i == 0 {
				if image, ok := embed["image"].(map[string]any); ok {
					imageURL, _ = image["url"].(string)
				}
			}
		}
		if len(sanitized) > 0 {
			msg["embeds"] = sanitized
		} else {
			delete(msg, "embeds")
		}
	}

	// Truncate top-level content; drop content if empty.
	if content, ok := msg["content"].(string); ok {
		content = truncateRunes(content, maxDiscordContent)
		if strings.TrimSpace(content) == "" {
			delete(msg, "content")
		} else {
			msg["content"] = content
		}
	}

	result, err := marshalNoEscape(msg)
	if err != nil {
		return nil, "", err
	}
	return result, imageURL, nil
}

// Discord embed limits — see https://discord.com/developers/docs/resources/channel#embed-object-embed-limits
const (
	maxDiscordContent   = 2000
	maxDiscordEmbeds    = 10
	maxEmbedTitle       = 256
	maxEmbedDescription = 4096
	maxEmbedFields      = 25
	maxEmbedFieldName   = 256
	maxEmbedFieldValue  = 1024
	maxEmbedFooterText  = 2048
	maxEmbedAuthorName  = 256
	maxEmbedTotalChars  = 6000
	zeroWidthSpace      = "\u200B"
)

// sanitizeDiscordEmbed enforces Discord's embed validation rules in place.
// Returns true if the embed is non-empty (should be kept), false if it
// should be dropped from the embeds array.
//
// Rules:
//   - field name AND value both empty → drop the field entirely
//   - field name empty, value has content → name = zero-width space
//   - field value empty, name has content → value = zero-width space
//   - truncate name (256), value (1024), title (256), description (4096),
//     footer.text (2048), author.name (256)
//   - drop empty image/thumbnail/footer/author objects
//   - drop "null" / "undefined" string literals (DTS rendering bugs)
//   - cap fields at 25
//   - if total characters exceed 6000, progressively trim long descriptions
//     and field values to fit
//   - if the embed has no title, description, image, fields, footer, or
//     author after sanitisation → return false (drop it)
func sanitizeDiscordEmbed(embed map[string]any) bool {
	// Strip "null"/"undefined" string literals from common string fields.
	stripNullLiteralString(embed, "title")
	stripNullLiteralString(embed, "description")
	stripNullLiteralString(embed, "url")

	// Truncate top-level strings.
	if v, ok := embed["title"].(string); ok {
		embed["title"] = truncateRunes(strings.TrimSpace(v), maxEmbedTitle)
		if embed["title"] == "" {
			delete(embed, "title")
		}
	}
	if v, ok := embed["description"].(string); ok {
		embed["description"] = truncateRunes(v, maxEmbedDescription)
		if strings.TrimSpace(embed["description"].(string)) == "" {
			delete(embed, "description")
		}
	}
	if v, ok := embed["url"].(string); ok && strings.TrimSpace(v) == "" {
		delete(embed, "url")
	}

	// image / thumbnail — drop if url is empty/whitespace
	dropEmptyURLObject(embed, "image")
	dropEmptyURLObject(embed, "thumbnail")

	// footer — drop if no text after trim
	if footer, ok := embed["footer"].(map[string]any); ok {
		if text, ok := footer["text"].(string); ok {
			text = truncateRunes(strings.TrimSpace(text), maxEmbedFooterText)
			if text == "" {
				delete(embed, "footer")
			} else {
				footer["text"] = text
			}
		} else {
			delete(embed, "footer")
		}
	}

	// author — drop if no name after trim
	if author, ok := embed["author"].(map[string]any); ok {
		if name, ok := author["name"].(string); ok {
			name = truncateRunes(strings.TrimSpace(name), maxEmbedAuthorName)
			if name == "" {
				delete(embed, "author")
			} else {
				author["name"] = name
			}
		} else {
			delete(embed, "author")
		}
	}

	// fields — drop empty ones, swap blank name/value with zero-width space, cap at 25
	if rawFields, ok := embed["fields"].([]any); ok {
		var keep []any
		for _, f := range rawFields {
			field, ok := f.(map[string]any)
			if !ok {
				continue
			}
			name, _ := field["name"].(string)
			value, _ := field["value"].(string)
			name = strings.TrimSpace(name)
			value = strings.TrimSpace(value)

			// Both empty → drop entirely
			if name == "" && value == "" {
				continue
			}
			// One side empty → swap with zero-width space (Discord requires both non-empty)
			if name == "" {
				name = zeroWidthSpace
			}
			if value == "" {
				value = zeroWidthSpace
			}

			// Truncate
			if len(name) > 0 && name != zeroWidthSpace {
				name = truncateRunes(name, maxEmbedFieldName)
			}
			if len(value) > 0 && value != zeroWidthSpace {
				value = truncateRunes(value, maxEmbedFieldValue)
			}

			field["name"] = name
			field["value"] = value
			keep = append(keep, field)
			if len(keep) >= maxEmbedFields {
				break
			}
		}
		if len(keep) > 0 {
			embed["fields"] = keep
		} else {
			delete(embed, "fields")
		}
	}

	// Total character budget — Discord rejects if > 6000
	enforceEmbedCharBudget(embed)

	// Drop the embed if it has nothing renderable
	return embedHasContent(embed)
}

// dropEmptyURLObject removes embed[key] if it's an object whose "url" is
// missing/empty/whitespace.
func dropEmptyURLObject(embed map[string]any, key string) {
	obj, ok := embed[key].(map[string]any)
	if !ok {
		return
	}
	url, _ := obj["url"].(string)
	if strings.TrimSpace(url) == "" || strings.EqualFold(url, "null") || strings.EqualFold(url, "undefined") {
		delete(embed, key)
	}
}

// stripNullLiteralString removes a string field if its value is "null" /
// "undefined" / "<nil>" — common DTS rendering bugs where a missing variable
// gets stringified.
func stripNullLiteralString(embed map[string]any, key string) {
	v, ok := embed[key].(string)
	if !ok {
		return
	}
	trimmed := strings.TrimSpace(v)
	if trimmed == "null" || trimmed == "undefined" || trimmed == "<nil>" {
		delete(embed, key)
	}
}

// embedHasContent returns true if the embed has any renderable component.
// Accepts standard renderable keys (title/description/image/thumbnail/fields/
// footer/author) but also any other non-empty key — Discord accepts embeds
// with just a colour, for example.
func embedHasContent(embed map[string]any) bool {
	for _, v := range embed {
		if v == nil {
			continue
		}
		if s, isStr := v.(string); isStr && s == "" {
			continue
		}
		if arr, isArr := v.([]any); isArr && len(arr) == 0 {
			continue
		}
		if m, isMap := v.(map[string]any); isMap && len(m) == 0 {
			continue
		}
		return true
	}
	return false
}

// enforceEmbedCharBudget trims field values and the description to keep the
// total embed character count under maxEmbedTotalChars. Discord rejects
// embeds whose combined characters exceed 6000.
func enforceEmbedCharBudget(embed map[string]any) {
	total := embedCharCount(embed)
	if total <= maxEmbedTotalChars {
		return
	}

	// First pass: trim long field values to 256 chars
	if fields, ok := embed["fields"].([]any); ok {
		for _, f := range fields {
			if total <= maxEmbedTotalChars {
				return
			}
			field, ok := f.(map[string]any)
			if !ok {
				continue
			}
			val, _ := field["value"].(string)
			if len(val) > 256 {
				newVal := truncateRunes(val, 256)
				total -= len(val) - len(newVal)
				field["value"] = newVal
			}
		}
	}
	// Second pass: trim description
	if total > maxEmbedTotalChars {
		if desc, ok := embed["description"].(string); ok {
			overflow := total - maxEmbedTotalChars
			newLen := max(len(desc)-overflow, 0)
			embed["description"] = truncateRunes(desc, newLen)
		}
	}
}

// embedCharCount sums the character counts of fields that count towards
// Discord's 6000-char per-embed limit.
func embedCharCount(embed map[string]any) int {
	total := 0
	if v, ok := embed["title"].(string); ok {
		total += len([]rune(v))
	}
	if v, ok := embed["description"].(string); ok {
		total += len([]rune(v))
	}
	if footer, ok := embed["footer"].(map[string]any); ok {
		if v, ok := footer["text"].(string); ok {
			total += len([]rune(v))
		}
	}
	if author, ok := embed["author"].(map[string]any); ok {
		if v, ok := author["name"].(string); ok {
			total += len([]rune(v))
		}
	}
	if fields, ok := embed["fields"].([]any); ok {
		for _, f := range fields {
			if field, ok := f.(map[string]any); ok {
				if v, ok := field["name"].(string); ok {
					total += len([]rune(v))
				}
				if v, ok := field["value"].(string); ok {
					total += len([]rune(v))
				}
			}
		}
	}
	return total
}

// truncateRunes returns s truncated to at most max runes. If truncation
// happens, the last 3 runes are replaced with "..." (or the whole truncated
// string is "..." if max < 3).
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max < 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
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

	if str == "" {
		delete(embed, "color")
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
	} else {
		// Unparseable — remove to avoid Discord 400 error.
		delete(embed, "color")
	}
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

// ReplaceEmbedImageURL sets embeds[0].image.url to "attachment://map.png".
// This is the only function that needs a second JSON parse — called only when
// an image was successfully downloaded and needs to be replaced with an attachment.
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
