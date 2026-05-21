package dts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/buttons"
)

// encodeTOML produces the wire-format TOML bytes for a list of DTS
// entries. Used by the config editor save path (Option C from #110):
// the editor speaks JSON internally; this is the bridge that writes
// back to TOML for entries whose sourceFormat is "toml".
//
// Hand-rolled rather than going through BurntSushi/toml because that
// encoder writes every string as a single-line `key = "..."` with
// escaped \n — fine for short scalars, awful for multi-line template
// bodies and inline response templates. Operators saving a 20-line
// Handlebars template get back a single unreadable line of escapes.
// We emit those as TOML triple-quoted multi-line strings instead so
// the saved file stays as editable as the hand-authored original.
//
// Comments and operator-chosen key ordering are NOT preserved. Editors
// that depend on those should treat each save as a re-emit; the
// rewrite-backup mechanism captures the prior file so operators can
// recover hand-authored layouts if a round-trip clobbers them.
func encodeTOML(entries []DTSEntry) ([]byte, error) {
	var buf bytes.Buffer
	for i, e := range entries {
		if i > 0 {
			buf.WriteByte('\n')
		}
		if err := writeEntry(&buf, &e); err != nil {
			return nil, fmt.Errorf("encode entry %d: %w", i, err)
		}
	}
	return buf.Bytes(), nil
}

// writeEntry emits a single [[entry]] block with all scalar fields in a
// stable, human-friendly order, then any [[entry.buttons]] sub-blocks.
func writeEntry(buf *bytes.Buffer, e *DTSEntry) error {
	buf.WriteString("[[entry]]\n")

	// Identity first — the four-tuple that keys the entry.
	writeStringKV(buf, "id", string(e.ID))
	writeStringKV(buf, "type", e.Type)
	writeStringKV(buf, "platform", e.Platform)
	writeStringKV(buf, "language", e.Language)

	// Optional flags.
	if e.Default {
		buf.WriteString("default = true\n")
	}
	if e.Hidden {
		buf.WriteString("hidden = true\n")
	}

	// Metadata.
	if e.Name != "" {
		writeStringKV(buf, "name", e.Name)
	}
	if e.Description != "" {
		writeStringKV(buf, "description", e.Description)
	}

	// Body — template OR templateFile, never both.
	if e.TemplateFile != "" {
		writeStringKV(buf, "templateFile", e.TemplateFile)
	} else if e.Template != nil {
		if err := writeTemplateKV(buf, "template", e.Template); err != nil {
			return err
		}
	}

	// Buttons as [[entry.buttons]] sub-tables.
	for i := range e.Buttons {
		buf.WriteString("\n")
		if err := writeButton(buf, &e.Buttons[i]); err != nil {
			return fmt.Errorf("button %d (%s): %w", i, e.Buttons[i].ID, err)
		}
	}
	return nil
}

// writeButton emits a [[entry.buttons]] sub-table with sensible field
// ordering and multi-line strings for response_template_inline bodies.
func writeButton(buf *bytes.Buffer, b *buttons.Def) error {
	buf.WriteString("  [[entry.buttons]]\n")
	writeStringKV2(buf, "id", b.ID)
	writeStringKV2(buf, "label", b.Label)
	if b.Style != "" {
		writeStringKV2(buf, "style", b.Style)
	}
	if b.Action != "" {
		writeStringKV2(buf, "action", b.Action)
	}
	if b.Scope != "" {
		writeStringKV2(buf, "scope", b.Scope)
	}
	if b.ResponseTemplateID != "" {
		writeStringKV2(buf, "response_template_id", b.ResponseTemplateID)
	}
	if b.ResponseText != "" {
		writeStringKV2Multiline(buf, "response_text", b.ResponseText)
	}
	if b.ResponseTemplateInline != nil {
		if err := writeTemplateKV2(buf, "response_template_inline", b.ResponseTemplateInline); err != nil {
			return err
		}
	}
	if len(b.AppliesTo) > 0 {
		buf.WriteString("  applies_to = ")
		writeStringArray(buf, b.AppliesTo)
		buf.WriteByte('\n')
	}
	if b.ShowIf != "" {
		writeStringKV2Multiline(buf, "show_if", b.ShowIf)
	}
	if b.VisibleTo != "" {
		writeStringKV2(buf, "visible_to", b.VisibleTo)
	}
	if len(b.Params) > 0 {
		buf.WriteString("  params = ")
		if err := writeInlineTable(buf, b.Params); err != nil {
			return err
		}
		buf.WriteByte('\n')
	}
	return nil
}

// writeTemplateKV emits the template field. Strings with newlines get
// triple-quoted blocks; objects get JSON-marshalled and emitted as
// triple-quoted blocks too (matches what the operator typed when they
// authored the entry as a TOML """...""" body containing JSON).
func writeTemplateKV(buf *bytes.Buffer, key string, v any) error {
	switch t := v.(type) {
	case string:
		if strings.Contains(t, "\n") {
			writeMultilineKV(buf, key, t, "")
		} else {
			writeStringKV(buf, key, t)
		}
		return nil
	case []any:
		// Array of strings (operator authored a description-style array
		// in their template). Join with newlines and emit multi-line —
		// matches the loader's interpretation.
		var sb strings.Builder
		for i, e := range t {
			if i > 0 {
				sb.WriteByte('\n')
			}
			s, ok := e.(string)
			if !ok {
				return fmt.Errorf("template array element %d is %T, not string", i, e)
			}
			sb.WriteString(s)
		}
		writeMultilineKV(buf, key, sb.String(), "")
		return nil
	default:
		// Object — JSON-marshal with indentation for readability.
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal template object: %w", err)
		}
		writeMultilineKV(buf, key, string(raw), "")
		return nil
	}
}

// writeTemplateKV2 is the [[entry.buttons]]-indented variant of
// writeTemplateKV. Shares the same shape logic; differs in indentation.
func writeTemplateKV2(buf *bytes.Buffer, key string, v any) error {
	switch t := v.(type) {
	case string:
		if strings.Contains(t, "\n") {
			writeMultilineKV(buf, key, t, "  ")
		} else {
			writeStringKV2(buf, key, t)
		}
		return nil
	case []any:
		var sb strings.Builder
		for i, e := range t {
			if i > 0 {
				sb.WriteByte('\n')
			}
			s, ok := e.(string)
			if !ok {
				return fmt.Errorf("response_template_inline array element %d is %T, not string", i, e)
			}
			sb.WriteString(s)
		}
		writeMultilineKV(buf, key, sb.String(), "  ")
		return nil
	default:
		// Prefix is "" — the second arg to MarshalIndent is the per-line
		// prefix, and any prefix here would silently become trailing
		// whitespace in the parsed body on the next load (TOML preserves
		// triple-quoted content literally). The third arg sets the
		// nesting step.
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal response_template_inline object: %w", err)
		}
		writeMultilineKV(buf, key, string(raw), "  ")
		return nil
	}
}

// writeMultilineKV emits  key = """\n<body>\n"""  with optional indent
// before the key. The closing `"""` is deliberately written at column
// 0, not at the key's indent: per the TOML spec any whitespace before
// the closing fence is included in the string body. Matching the key
// indent would silently append trailing whitespace to every operator's
// template on every save round-trip. Embedded triple-quote sequences
// in the body are escaped (rare in practice).
func writeMultilineKV(buf *bytes.Buffer, key, body, indent string) {
	body = strings.ReplaceAll(body, `"""`, `\"\"\"`)
	body = strings.TrimRight(body, "\n")
	buf.WriteString(indent)
	buf.WriteString(key)
	buf.WriteString(" = \"\"\"\n")
	buf.WriteString(body)
	buf.WriteString("\n\"\"\"\n")
}

// writeStringKV emits a top-level scalar string key/value with no
// indent. Picks multi-line form automatically when the value contains
// a newline.
func writeStringKV(buf *bytes.Buffer, key, value string) {
	if strings.Contains(value, "\n") {
		writeMultilineKV(buf, key, value, "")
		return
	}
	buf.WriteString(key)
	buf.WriteString(" = ")
	writeBasicString(buf, value)
	buf.WriteByte('\n')
}

// writeStringKV2 is the [[entry.buttons]]-indented variant of
// writeStringKV. Buttons are nested one level inside the entry block.
func writeStringKV2(buf *bytes.Buffer, key, value string) {
	buf.WriteString("  ")
	buf.WriteString(key)
	buf.WriteString(" = ")
	writeBasicString(buf, value)
	buf.WriteByte('\n')
}

// writeStringKV2Multiline behaves like writeStringKV2 but switches to
// triple-quoted form when the value contains a newline.
func writeStringKV2Multiline(buf *bytes.Buffer, key, value string) {
	if strings.Contains(value, "\n") {
		writeMultilineKV(buf, key, value, "  ")
		return
	}
	writeStringKV2(buf, key, value)
}

// writeBasicString emits a TOML basic string (double-quoted, standard
// escapes). Used for all single-line strings.
func writeBasicString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			buf.WriteString(`\\`)
		case '"':
			buf.WriteString(`\"`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04X`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}

// writeStringArray emits a TOML array of strings on one line:
// ["a", "b", "c"].
func writeStringArray(buf *bytes.Buffer, items []string) {
	buf.WriteByte('[')
	for i, s := range items {
		if i > 0 {
			buf.WriteString(", ")
		}
		writeBasicString(buf, s)
	}
	buf.WriteByte(']')
}

// writeInlineTable emits a TOML inline table { k = v, ... } from a map.
// Used for button params, which the schema constrains to a flat
// key/value bag of scalars. Keys are sorted for stable output.
func writeInlineTable(buf *bytes.Buffer, m map[string]any) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(k)
		buf.WriteString(" = ")
		if err := writeScalar(buf, m[k]); err != nil {
			return fmt.Errorf("inline-table key %q: %w", k, err)
		}
	}
	buf.WriteByte('}')
	return nil
}

// writeScalar emits a single TOML value for any common scalar type the
// JSON unmarshal produces (string, bool, int, float64). Used by the
// inline-table emitter for button params.
func writeScalar(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case string:
		writeBasicString(buf, t)
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case int:
		fmt.Fprintf(buf, "%d", t)
	case int64:
		fmt.Fprintf(buf, "%d", t)
	case float64:
		// JSON numbers come in as float64 even when they're integers.
		// Print without trailing zeros when the value is a whole number.
		if t == float64(int64(t)) {
			fmt.Fprintf(buf, "%d", int64(t))
		} else {
			fmt.Fprintf(buf, "%g", t)
		}
	default:
		return fmt.Errorf("unsupported scalar type %T", v)
	}
	return nil
}
