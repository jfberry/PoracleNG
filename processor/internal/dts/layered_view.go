package dts

import (
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// LayeredView implements raymond.FieldResolver for zero-copy template context.
// Instead of merging multiple maps into a flat map, it checks each layer in
// priority order when raymond resolves a field. This eliminates map allocation
// and key copying on the hot path.
//
// Layer priority (first match wins):
//  1. perUser enrichment (PVP display, distance — pokemon only)
//  2. emoji (resolved per platform)
//  3. perLang enrichment (translated names, types, moves)
//  4. computed fields (tthh/tthm/tths, areas, time, id, megaName)
//  5. aliases (field name mappings)
//  6. base enrichment (universal: weather, maps, icons, geocoding)
//  7. dtsDictionary (user-defined config overrides)
type LayeredView struct {
	base      map[string]any
	perLang   map[string]any
	perUser   map[string]any
	emoji     map[string]string // resolved emoji key → string
	aliases   map[string]string // alias name → source field name
	computed  map[string]any    // pre-computed fields (tthh, areas, etc.)
	webhook   map[string]any    // raw webhook fields (lowest priority, for custom DTS)
	dtsDict   map[string]any
}

// NewLayeredView creates a view from enrichment layers without copying.
// webhookFields is the parsed raw webhook JSON (lowest priority layer,
// supports custom DTS templates referencing obscure webhook fields).
func NewLayeredView(
	vb *ViewBuilder,
	base map[string]any,
	perLang map[string]any,
	perUser map[string]any,
	webhookFields map[string]any,
	platform string,
	areas []webhook.MatchedArea,
) *LayeredView {
	lv := &LayeredView{
		base:    base,
		perLang: perLang,
		perUser: perUser,
		webhook: webhookFields,
		dtsDict: vb.dtsDictionary,
	}

	// Resolve emoji (small map, only populated keys)
	lv.emoji = vb.resolveEmojiMap(base, perLang, platform)

	// Build computed fields (small map — needs resolved emoji for genderData)
	lv.computed = buildComputedFields(base, perLang, lv.emoji, areas)

	// Resolve emoji arrays into computed (they're []string, not simple strings)
	for _, m := range arrayEmojiKeys {
		var raw interface{}
		if perLang != nil {
			raw = perLang[m.keyField]
		}
		if raw == nil {
			raw = base[m.keyField]
		}
		if resolved := vb.resolveEmojiArray(raw, platform); resolved != nil {
			lv.computed[m.outputField] = resolved
		}
	}
	// Special: typeEmojiKeys → "emoji" array
	var typeRaw interface{}
	if perLang != nil {
		typeRaw = perLang["typeEmojiKeys"]
	}
	if typeRaw == nil {
		typeRaw = base["typeEmojiKeys"]
	}
	if resolved := vb.resolveEmojiArray(typeRaw, platform); resolved != nil {
		lv.computed["emoji"] = resolved
	}

	// Build alias reverse lookup
	lv.aliases = buildAliasLookup()

	return lv
}

// GetField implements raymond.FieldResolver.
// Checks layers in priority order, returns first match.
func (lv *LayeredView) GetField(name string) (interface{}, bool) {
	// 1. Per-user enrichment (PVP, distance)
	if lv.perUser != nil {
		if v, ok := lv.perUser[name]; ok {
			return v, true
		}
	}

	// 2. Emoji (resolved per platform)
	if v, ok := lv.emoji[name]; ok {
		return v, true
	}

	// 3. Per-language enrichment (translations)
	if lv.perLang != nil {
		if v, ok := lv.perLang[name]; ok {
			return v, true
		}
	}

	// 4. Computed fields (tthh, areas, time, etc.)
	if v, ok := lv.computed[name]; ok {
		return v, true
	}

	// 5. Base enrichment
	if v, ok := lv.base[name]; ok {
		return v, true
	}

	// 6. Aliases (check if name is an alias, resolve source from all layers)
	if source, ok := lv.aliases[name]; ok {
		if v, found := lv.resolveSource(source); found {
			return v, true
		}
	}

	// 7. Raw webhook fields (lowest priority — supports custom DTS templates)
	if lv.webhook != nil {
		if v, ok := lv.webhook[name]; ok {
			return v, true
		}
	}

	// 8. DTS dictionary
	if lv.dtsDict != nil {
		if v, ok := lv.dtsDict[name]; ok {
			return v, true
		}
	}

	return nil, false
}

// resolveSource looks up a field by name across all non-alias layers.
func (lv *LayeredView) resolveSource(name string) (interface{}, bool) {
	if lv.perUser != nil {
		if v, ok := lv.perUser[name]; ok {
			return v, true
		}
	}
	if v, ok := lv.emoji[name]; ok {
		return v, true
	}
	if lv.perLang != nil {
		if v, ok := lv.perLang[name]; ok {
			return v, true
		}
	}
	if v, ok := lv.computed[name]; ok {
		return v, true
	}
	if v, ok := lv.base[name]; ok {
		return v, true
	}
	if lv.webhook != nil {
		if v, ok := lv.webhook[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// resolveEmojiMap builds a small map of resolved emoji strings from emoji keys.
func (vb *ViewBuilder) resolveEmojiMap(base, perLang map[string]any, platform string) map[string]string {
	if vb.emoji == nil {
		return nil
	}

	result := make(map[string]string, 16)

	// Resolve single emoji keys from both base and perLang
	for _, m := range singleEmojiKeys {
		key := ""
		if perLang != nil {
			if k, ok := perLang[m.keyField].(string); ok && k != "" {
				key = k
			}
		}
		if key == "" {
			if k, ok := base[m.keyField].(string); ok && k != "" {
				key = k
			}
		}
		if key != "" {
			result[m.outputField] = vb.emoji.Lookup(key, platform)
		}
	}

	// Resolve array emoji keys
	for _, m := range arrayEmojiKeys {
		var raw interface{}
		if perLang != nil {
			raw = perLang[m.keyField]
		}
		if raw == nil {
			raw = base[m.keyField]
		}
		if resolved := vb.resolveEmojiArray(raw, platform); resolved != nil {
			result[m.outputField] = strings.Join(resolved, "")
		}
	}

	// Special case: typeEmojiKeys → emoji ([]string) + emojiString
	var typeRaw interface{}
	if perLang != nil {
		typeRaw = perLang["typeEmojiKeys"]
	}
	if typeRaw == nil {
		typeRaw = base["typeEmojiKeys"]
	}
	if resolved := vb.resolveEmojiArray(typeRaw, platform); resolved != nil {
		result["emojiString"] = strings.Join(resolved, "")
	}

	return result
}

// buildComputedFields creates the small set of derived fields.
func buildComputedFields(base, perLang map[string]any, emoji map[string]string, areas []webhook.MatchedArea) map[string]any {
	m := make(map[string]any, 16)

	// id = pokemon_id
	if v, ok := base["pokemon_id"]; ok {
		m["id"] = v
	}

	// time = disappearTime
	if v, ok := base["disappearTime"]; ok {
		m["time"] = v
	}

	// TTH components
	if tthRaw, ok := base["tth"]; ok {
		switch tth := tthRaw.(type) {
		case geo.TTH:
			m["tthd"] = tth.Days
			m["tthh"] = tth.Hours
			m["tthm"] = tth.Minutes
			m["tths"] = tth.Seconds
		case *geo.TTH:
			if tth != nil {
				m["tthd"] = tth.Days
				m["tthh"] = tth.Hours
				m["tthm"] = tth.Minutes
				m["tths"] = tth.Seconds
			}
		case map[string]any:
			if v, ok := tth["days"]; ok {
				m["tthd"] = v
			}
			if v, ok := tth["hours"]; ok {
				m["tthh"] = v
			}
			if v, ok := tth["minutes"]; ok {
				m["tthm"] = v
			}
			if v, ok := tth["seconds"]; ok {
				m["tths"] = v
			}
		}
	}

	// megaName (raid enrichment sets this directly)
	if v, ok := perLang["megaName"]; ok {
		m["megaName"] = v
	} else if v, ok := base["megaName"]; ok {
		m["megaName"] = v
	}

	// Current time
	now := time.Now().UTC()
	m["now"] = now.Format(time.RFC3339)
	m["nowISO"] = now.Format("2006-01-02T15:04:05.000Z")

	// Areas
	var areaNames []string
	for _, a := range areas {
		if a.DisplayInMatches {
			areaNames = append(areaNames, a.Name)
		}
	}
	m["areas"] = strings.Join(areaNames, ", ")

	// genderData (assembled from perLang name + resolved emoji)
	genderName := ""
	if perLang != nil {
		if n, ok := perLang["genderName"].(string); ok {
			genderName = n
		}
	}
	genderEmoji := emoji["genderEmoji"] // from resolved emoji map
	if genderName != "" || genderEmoji != "" {
		m["genderData"] = map[string]any{
			"name":  genderName,
			"emoji": genderEmoji,
		}
	}

	return m
}

// buildAliasLookup returns a map of alias → source field name.
// Built once, shared across all views.
var aliasLookupCache map[string]string

func buildAliasLookup() map[string]string {
	if aliasLookupCache != nil {
		return aliasLookupCache
	}
	m := make(map[string]string, len(aliasMapping))
	for _, a := range aliasMapping {
		m[a.alias] = a.source
	}
	aliasLookupCache = m
	return m
}
