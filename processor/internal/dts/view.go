package dts

import (
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// ViewBuilder constructs the template view (map[string]any) by merging
// enrichment layers, resolving emoji keys, and adding backward-compatible aliases.
type ViewBuilder struct {
	emoji         *EmojiLookup
	dtsDictionary map[string]any
}

// NewViewBuilder creates a ViewBuilder with the given emoji lookup and DTS dictionary.
// Both parameters may be nil for simple use cases.
func NewViewBuilder(emoji *EmojiLookup, dtsDictionary map[string]any) *ViewBuilder {
	return &ViewBuilder{
		emoji:         emoji,
		dtsDictionary: dtsDictionary,
	}
}

// BuildPokemonView constructs the full template view for a pokemon alert.
// It merges enrichment layers (base < perLang < perUser), resolves emoji keys,
// applies the DTS dictionary, adds backward-compatible aliases, and computes
// derived fields.
func (vb *ViewBuilder) BuildPokemonView(
	base map[string]any,
	perLang map[string]any,
	perUser map[string]any,
	platform string,
	areas []webhook.MatchedArea,
) map[string]any {
	// 1. Merge layers: base < perLang < perUser
	view := make(map[string]any, len(base)+len(perLang)+len(perUser))
	mergeMaps(view, base)
	mergeMaps(view, perLang)
	mergeMaps(view, perUser)

	// 2. Resolve emoji keys
	vb.resolveEmoji(view, platform)

	// 3. Merge DTS dictionary (user-defined key-value pairs)
	mergeMaps(view, vb.dtsDictionary)

	// 4. Add backward-compatible aliases
	addAliases(view)

	// 5. Add computed fields
	addComputedFields(view, areas)

	// 6. Escape user content
	escapeUserContent(view)

	return view
}

// mergeMaps copies all entries from src into dst. Later calls overwrite earlier keys.
func mergeMaps(dst, src map[string]any) {
	for k, v := range src {
		dst[k] = v
	}
}

// emojiKeyMapping maps enrichment emoji key fields to their resolved output fields.
var emojiKeyMapping = []struct {
	keyField    string
	outputField string
}{
	{"genderEmojiKey", "genderEmoji"},
	{"quickMoveTypeEmojiKey", "quickMoveEmoji"},
	{"chargeMoveTypeEmojiKey", "chargeMoveEmoji"},
	{"boostWeatherEmojiKey", "boostWeatherEmoji"},
	{"gameWeatherEmojiKey", "gameWeatherEmoji"},
	{"bearingEmojiKey", "bearingEmoji"},
	{"shinyPossibleEmojiKey", "shinyPossibleEmoji"},
}

// resolveEmoji converts emoji key fields to resolved emoji strings using the platform.
func (vb *ViewBuilder) resolveEmoji(view map[string]any, platform string) {
	if vb.emoji == nil {
		return
	}

	for _, m := range emojiKeyMapping {
		if key, ok := view[m.keyField].(string); ok && key != "" {
			view[m.outputField] = vb.emoji.Lookup(key, platform)
		}
	}

	// typeEmojiKeys ([]string or []any) → emoji ([]string) + emojiString (joined)
	if raw, ok := view["typeEmojiKeys"]; ok {
		var keys []string
		switch v := raw.(type) {
		case []string:
			keys = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					keys = append(keys, s)
				}
			}
		}
		var resolved []string
		for _, key := range keys {
			resolved = append(resolved, vb.emoji.Lookup(key, platform))
		}
		view["emoji"] = resolved
		view["emojiString"] = strings.Join(resolved, "")
	}
}

// aliasMapping maps alias names to their source fields.
var aliasMapping = []struct {
	alias  string
	source string
}{
	{"formname", "formName"},
	{"mapurl", "googleMapUrl"},
	{"applemap", "appleMapUrl"},
	{"ivcolor", "ivColor"},
	{"distime", "disappearTime"},
	{"individual_attack", "atk"},
	{"individual_defense", "def"},
	{"individual_stamina", "sta"},
}

// addAliases adds backward-compatible field aliases to the view.
func addAliases(view map[string]any) {
	for _, a := range aliasMapping {
		if v, ok := view[a.source]; ok {
			view[a.alias] = v
		}
	}
}

// addComputedFields adds derived fields to the view.
func addComputedFields(view map[string]any, areas []webhook.MatchedArea) {
	// id = pokemon_id
	if v, ok := view["pokemon_id"]; ok {
		view["id"] = v
	}

	// time = disappearTime
	if v, ok := view["disappearTime"]; ok {
		view["time"] = v
	}

	// Extract tth components — handle both geo.TTH struct and map[string]any
	if tthRaw, ok := view["tth"]; ok {
		switch tth := tthRaw.(type) {
		case geo.TTH:
			view["tthd"] = tth.Days
			view["tthh"] = tth.Hours
			view["tthm"] = tth.Minutes
			view["tths"] = tth.Seconds
		case *geo.TTH:
			if tth != nil {
				view["tthd"] = tth.Days
				view["tthh"] = tth.Hours
				view["tthm"] = tth.Minutes
				view["tths"] = tth.Seconds
			}
		case map[string]any:
			if v, ok := tth["days"]; ok {
				view["tthd"] = v
			}
			if v, ok := tth["hours"]; ok {
				view["tthh"] = v
			}
			if v, ok := tth["minutes"]; ok {
				view["tthm"] = v
			}
			if v, ok := tth["seconds"]; ok {
				view["tths"] = v
			}
		}
	}

	// Current time
	now := time.Now().UTC()
	view["now"] = now.Format(time.RFC3339)
	view["nowISO"] = now.Format("2006-01-02T15:04:05.000Z")

	// Areas: join names where DisplayInMatches is true
	var areaNames []string
	for _, a := range areas {
		if a.DisplayInMatches {
			areaNames = append(areaNames, a.Name)
		}
	}
	view["areas"] = strings.Join(areaNames, ", ")
}

// escapeUserContent sanitizes fields that may contain user-generated text
// to prevent JSON injection or formatting issues.
func escapeUserContent(view map[string]any) {
	for _, field := range []string{"pokestop_name", "pokestop_url"} {
		if v, ok := view[field].(string); ok {
			view[field] = escapeJSONString(v)
		}
	}
}

// escapeJSONString replaces characters that could break JSON or message formatting.
func escapeJSONString(s string) string {
	s = strings.ReplaceAll(s, `\`, "?")
	s = strings.ReplaceAll(s, `"`, "''")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
