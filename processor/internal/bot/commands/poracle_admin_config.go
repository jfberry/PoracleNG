package commands

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
)

// paConfig implements !poracle-admin config — view the effective merged
// config with sensitive fields redacted.
//
// Subcommands:
//
//	(no arg) / help  — subgroup help (lists subcommands + format hint)
//	keys             — list top-level section names with key counts
//	<section>        — print just that section with redactions applied
//
// No-arg call (other than "help"): print the FULL config with redactions,
// chunked via bot.SplitTextReply if necessary.
var paConfig = &paSubgroup{
	run:  paConfigRun,
	help: paConfigHelp,
}

// redactedFields maps fully-qualified "section.field" dotted paths to true
// when the value must be replaced with "***". false entries are explicit
// non-redactions for documentation purposes.
//
// This is the single source of truth for redaction across the entire
// admin surface. Adding a new sensitive key is a one-line change here.
var redactedFields = map[string]bool{
	// Discord tokens
	"discord.token":         true,
	"discord.command_token": true,

	// Telegram tokens
	"telegram.token": true,

	// Database credentials
	"database.password":         true,
	"database.dsn":              true, // full DSN contains password
	"database.scanner.password": true,
	"database.scanner.dsn":      true,

	// API secrets
	"processor.api_secret": true,
	"alerter.api_secret":   true, // legacy backward-compat fallback

	// Geocoding / map keys
	"geocoding.geocoding_key":         true,
	"geocoding.static_key":            true,
	"geocoding.google_api_key":        true,      // legacy name kept for safety
	"geocoding.google_static_api_key": true,      // legacy name kept for safety
	"geocoding.mapbox_token":          true,      // legacy name kept for safety
	"geocoding.tileserver_token":      true,      // legacy name kept for safety
	"geocoding.shlink_url":            true,      // shlink instance URL can be private
	"geocoding.shlink_api_key":        true,

	// General shortlink (mirrors geocoding.shlink_*)
	"general.shortlink_provider_url": true,
	"general.shortlink_provider_key": true,

	// Geofence Koji bearer token
	"geofence.koji.bearer_token": true,

	// Weather AccuWeather API keys
	"weather.accuweather_api_keys": true,

	// AI provider key (if any — future-proof)
	"ai.api_key": true,

	// Admin IDs are explicitly NOT redacted — not secrets.
	"discord.admins":  false,
	"telegram.admins": false,
}

const redactedLabel = "***"

// configSectionOrder controls the display order when rendering the full config.
// Sections not in this list appear alphabetically after the listed ones.
var configSectionOrder = []string{
	"processor",
	"general",
	"database",
	"discord",
	"telegram",
	"geofence",
	"pvp",
	"weather",
	"tuning",
	"stats",
	"area_security",
	"alert_limits",
	"tracking",
	"summariser",
	"geocoding",
	"locale",
	"logging",
	"webhookLogging",
	"fallbacks",
	"reconciliation",
	"autocreate",
	"validation",
	"ai",
	"alerter",
}

// paConfigHelpText returns the subgroup usage listing.
// This is the explicit help text (shown by "!poracle-admin config help").
func paConfigHelpText(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.config.help.intro"))
	sb.WriteString("\n\n")
	sb.WriteString("  **keys** — ")
	sb.WriteString(tr.T("cmd.poracle_admin.config.keys.desc"))
	sb.WriteString("\n  **<section>** — ")
	sb.WriteString(tr.T("cmd.poracle_admin.config.section.desc"))
	sb.WriteString("\n  *(no arg)* — ")
	sb.WriteString(tr.T("cmd.poracle_admin.config.full.desc"))

	return []bot.Reply{{Text: sb.String()}}
}

// paConfigHelp is called by the top-level dispatcher when the operator types
// `!poracle-admin config` with no further arguments. For this subgroup the
// useful default is the FULL config dump — the operator almost certainly
// wants to see values, not a help menu they can read from the docs.
func paConfigHelp(ctx *bot.CommandContext) []bot.Reply {
	return paConfigFull(ctx)
}

func paConfigRun(ctx *bot.CommandContext, args []string) []bot.Reply {
	if len(args) == 0 {
		return paConfigFull(ctx)
	}

	sub := strings.ToLower(args[0])

	switch sub {
	case "help":
		return paConfigHelpText(ctx)
	case "keys":
		return paConfigKeys(ctx)
	default:
		return paConfigSection(ctx, sub)
	}
}

// paConfigKeys lists all top-level section names with their key counts.
func paConfigKeys(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	if ctx.Config == nil {
		return []bot.Reply{{Text: "Config not loaded"}}
	}

	sections := configSections(ctx.Config)

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.config.keys.header"))

	for _, sec := range sections {
		sb.WriteString("\n  ")
		sb.WriteString(tr.Tf("cmd.poracle_admin.config.keys.row", sec.name, fmt.Sprintf("%d", sec.keyCount)))
	}

	return []bot.Reply{{Text: sb.String()}}
}

// paConfigSection renders a single named section.
func paConfigSection(ctx *bot.CommandContext, section string) []bot.Reply {
	tr := ctx.Tr()

	if ctx.Config == nil {
		return []bot.Reply{{Text: "Config not loaded"}}
	}

	sections := configSections(ctx.Config)

	for _, sec := range sections {
		if sec.name == section {
			if sec.keyCount == 0 {
				return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.config.empty_section", section)}}
			}
			text := renderSection(sec.name, sec.value, "")
			if text == "" {
				return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.config.empty_section", section)}}
			}
			block := fmt.Sprintf("[%s]\n%s", sec.name, text)
			return bot.SplitTextReply(block)
		}
	}

	return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.config.unknown_section", section)}}
}

// paConfigFull renders the entire config, all sections, redacted.
// Called when no sub-argument is given (and it's not "help").
func paConfigFull(ctx *bot.CommandContext) []bot.Reply {
	if ctx.Config == nil {
		return []bot.Reply{{Text: "Config not loaded"}}
	}

	sections := configSections(ctx.Config)

	var sb strings.Builder
	for _, sec := range sections {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("[%s]", sec.name))
		body := renderSection(sec.name, sec.value, "")
		if body != "" {
			sb.WriteString("\n")
			sb.WriteString(body)
		}
	}

	return bot.SplitTextReply(sb.String())
}

// sectionEntry holds a parsed config section for display.
type sectionEntry struct {
	name     string
	value    reflect.Value
	keyCount int
}

// configSections returns all top-level Config sections in display order.
func configSections(cfg *config.Config) []sectionEntry {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()

	// Build a map of toml tag → field index for lookup.
	tagToIdx := make(map[string]int, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("toml")
		if tag != "" && tag != "-" {
			tagToIdx[tag] = i
		}
	}

	// Ordered list of sections.
	ordered := make([]sectionEntry, 0, t.NumField())

	seen := make(map[string]bool)
	// First: ordered sections.
	for _, name := range configSectionOrder {
		idx, ok := tagToIdx[name]
		if !ok {
			continue
		}
		fv := v.Field(idx)
		kc := countKeys(fv, name)
		ordered = append(ordered, sectionEntry{name: name, value: fv, keyCount: kc})
		seen[name] = true
	}

	// Then: any remaining fields not in the order list.
	extras := make([]sectionEntry, 0)
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("toml")
		if tag == "" || tag == "-" || seen[tag] {
			continue
		}
		fv := v.Field(i)
		kc := countKeys(fv, tag)
		extras = append(extras, sectionEntry{name: tag, value: fv, keyCount: kc})
	}
	sort.Slice(extras, func(i, j int) bool { return extras[i].name < extras[j].name })
	ordered = append(ordered, extras...)

	return ordered
}

// countKeys returns the number of leaf keys under a struct (or map) value.
// Used to display "discord: 12 keys".
func countKeys(v reflect.Value, sectionPath string) int {
	return len(collectLeaves(v, sectionPath))
}

type leaf struct {
	path  string
	value reflect.Value
}

// collectLeaves recursively walks v and returns (dotted path, reflect.Value)
// pairs for every exported leaf field.
func collectLeaves(v reflect.Value, path string) []leaf {
	// Dereference pointer.
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		var out []leaf
		for i := 0; i < t.NumField(); i++ {
			ft := t.Field(i)
			if !ft.IsExported() {
				continue
			}
			tag := ft.Tag.Get("toml")
			if tag == "-" {
				continue
			}
			fieldName := tag
			if fieldName == "" {
				fieldName = strings.ToLower(ft.Name)
			}
			childPath := fieldName
			if path != "" {
				childPath = path + "." + fieldName
			}
			out = append(out, collectLeaves(v.Field(i), childPath)...)
		}
		return out

	case reflect.Map:
		// Map is a leaf for display purposes (rendered as one value).
		return []leaf{{path: path, value: v}}

	case reflect.Slice:
		// Slices are leaves.
		return []leaf{{path: path, value: v}}

	default:
		// Scalar: string, bool, int, float, etc.
		return []leaf{{path: path, value: v}}
	}
}

// renderSection renders a struct as TOML-like "  key = value" lines.
// sectionPrefix is the dotted path so redaction checks work correctly.
func renderSection(sectionName string, v reflect.Value, indent string) string {
	var sb strings.Builder
	renderValue(&sb, v, sectionName, indent)
	return sb.String()
}

// renderValue appends key=value lines (or nested blocks) to sb.
// path is the fully-qualified dotted path for redaction lookup.
func renderValue(sb *strings.Builder, v reflect.Value, path string, indent string) {
	// Dereference pointer.
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			ft := t.Field(i)
			if !ft.IsExported() {
				continue
			}
			tag := ft.Tag.Get("toml")
			if tag == "-" {
				continue
			}
			fieldName := tag
			if fieldName == "" {
				fieldName = strings.ToLower(ft.Name)
			}
			childPath := path + "." + fieldName
			fv := v.Field(i)

			// If nested struct (not a special type), render as a sub-block.
			if isNestedStruct(fv) {
				sb.WriteString(indent)
				sb.WriteString(fmt.Sprintf("[%s]\n", childPath))
				renderValue(sb, fv, childPath, indent+"  ")
				continue
			}

			sb.WriteString(indent)
			sb.WriteString(fieldName)
			sb.WriteString(" = ")
			sb.WriteString(renderLeaf(childPath, fv))
			sb.WriteString("\n")
		}

	default:
		// Top-level call with a non-struct: just render it.
		sb.WriteString(renderLeaf(path, v))
		sb.WriteString("\n")
	}
}

// isNestedStruct returns true when v is a struct (or ptr-to-struct) that
// should be rendered as a sub-block rather than a single-line value.
// We treat all exported structs that have their own toml tags as sub-blocks,
// except that a struct with zero exported toml fields is treated as empty.
func isNestedStruct(v reflect.Value) bool {
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
		if !ft.IsExported() {
			continue
		}
		tag := ft.Tag.Get("toml")
		if tag != "" && tag != "-" {
			return true
		}
	}
	return false
}

// renderLeaf formats a scalar or collection leaf value for display.
// It checks redactedFields for the given path.
func renderLeaf(path string, v reflect.Value) string {
	// Redaction check.
	if shouldRedact(path) {
		return fmt.Sprintf("%q", redactedLabel)
	}

	// Dereference pointer.
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "null"
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.String:
		return fmt.Sprintf("%q", v.String())

	case reflect.Bool:
		if v.Bool() {
			return "true"
		}
		return "false"

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", v.Int())

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", v.Uint())

	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%g", v.Float())

	case reflect.Slice:
		if v.Len() == 0 {
			return "[]"
		}
		var parts []string
		for i := 0; i < v.Len(); i++ {
			parts = append(parts, renderLeafElem(path, v.Index(i)))
		}
		return "[" + strings.Join(parts, ", ") + "]"

	case reflect.Map:
		if v.IsNil() || v.Len() == 0 {
			return "{}"
		}
		keys := v.MapKeys()
		sortedKeys := make([]string, 0, len(keys))
		for _, k := range keys {
			sortedKeys = append(sortedKeys, fmt.Sprintf("%v", k.Interface()))
		}
		sort.Strings(sortedKeys)
		var parts []string
		for _, k := range sortedKeys {
			val := v.MapIndex(reflect.ValueOf(k))
			// Treat all map entries as opaque (no per-entry redaction).
			parts = append(parts, fmt.Sprintf("%q: %s", k, renderAny(val.Interface())))
		}
		return "{" + strings.Join(parts, ", ") + "}"

	case reflect.Interface:
		if v.IsNil() {
			return "null"
		}
		return renderAny(v.Elem().Interface())

	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}

// renderLeafElem renders a single slice element. Applies parent-path redaction.
func renderLeafElem(parentPath string, v reflect.Value) string {
	if shouldRedact(parentPath) {
		return fmt.Sprintf("%q", redactedLabel)
	}
	// Dereference interface.
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "null"
		}
		v = v.Elem()
	}
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "null"
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		return fmt.Sprintf("%q", v.String())
	case reflect.Bool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", v.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%g", v.Float())
	case reflect.Map:
		return renderAny(v.Interface())
	case reflect.Struct:
		return renderAny(v.Interface())
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}

// renderAny formats an arbitrary Go value compactly (for map values, slice
// elements of mixed type, and interface values).
func renderAny(v any) string {
	if v == nil {
		return "null"
	}
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%q", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return fmt.Sprintf("%v", val)
	case map[string]any:
		if len(val) == 0 {
			return "{}"
		}
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%q: %s", k, renderAny(val[k])))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		if len(val) == 0 {
			return "[]"
		}
		parts := make([]string, len(val))
		for i, elem := range val {
			parts[i] = renderAny(elem)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// shouldRedact returns true when the dotted path must be replaced with "***".
// Exact-match check; partial-path substrings do not trigger redaction unless
// the exact key is listed.
func shouldRedact(path string) bool {
	// Exact match first.
	if v, ok := redactedFields[path]; ok {
		return v
	}

	// Heuristic for unlisted keys: if the path contains any of these
	// substrings as a segment suffix, redact defensively.
	sensitive := []string{
		"token", "password", "secret", "api_key", "apikey",
		"dsn", "bearer_token", "api_keys",
	}
	lower := strings.ToLower(path)
	// Get the final segment.
	lastDot := strings.LastIndex(lower, ".")
	segment := lower
	if lastDot >= 0 {
		segment = lower[lastDot+1:]
	}
	for _, s := range sensitive {
		if segment == s {
			return true
		}
	}
	return false
}
