package dts

import (
	"fmt"
	"maps"
	"strings"
	"sync"
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
	base     map[string]any
	perLang  map[string]any
	perUser  map[string]any
	emoji    map[string]string // resolved emoji key → string
	aliases  map[string]string // alias name → source field name
	computed map[string]any    // pre-computed fields (tthh, areas, etc.)
	webhook  map[string]any    // raw webhook fields (lowest priority, for custom DTS)
	dtsDict  map[string]any
}

// NewLayeredView creates a view from enrichment layers without copying.
// webhookFields is the parsed raw webhook JSON (lowest priority layer,
// supports custom DTS templates referencing obscure webhook fields).
func NewLayeredView(
	vb *ViewBuilder,
	templateType string,
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
	lv.computed = buildComputedFields(templateType, base, perLang, lv.emoji, areas)

	// Resolve emoji arrays into computed (they're []string, not simple strings)
	for _, m := range arrayEmojiKeys {
		var raw any
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
	var typeRaw any
	if perLang != nil {
		typeRaw = perLang["typeEmojiKeys"]
	}
	if typeRaw == nil {
		typeRaw = base["typeEmojiKeys"]
	}
	if resolved := vb.resolveEmojiArray(typeRaw, platform); resolved != nil {
		lv.computed["emoji"] = resolved
	}

	// Resolve emoji keys inside weaknessList entries (per-platform)
	resolveWeaknessEmojis(vb.emoji, perLang, platform)

	// Compose weatherChange text from forecast fields (needs resolved emoji + translated names)
	composeWeatherChange(lv.computed, base, perLang, vb.emoji, platform)

	// Escape user-generated content (pokestop/gym names) into computed layer
	escapeUserContentLayered(lv.computed, base, webhookFields)

	// Build alias lookup for this template type
	lv.aliases = buildAliasLookup(templateType)

	return lv
}

// GetField implements raymond.FieldResolver.
// Checks layers in priority order, returns first match.
func (lv *LayeredView) GetField(name string) (any, bool) {
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
func (lv *LayeredView) resolveSource(name string) (any, bool) {
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
		var raw any
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
	var typeRaw any
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

// resolveWeaknessEmojis resolves emoji keys inside weaknessList entries.
// Each type entry gets "emoji" resolved, and each category gets "typeEmoji" as a joined string.
// This must happen during view construction because it requires the platform for the emoji lookup.
func resolveWeaknessEmojis(emojiLookup *EmojiLookup, perLang map[string]any, platform string) {
	if perLang == nil || emojiLookup == nil {
		return
	}
	raw, ok := perLang["weaknessList"]
	if !ok {
		return
	}
	weaknessList, ok := raw.([]map[string]any)
	if !ok {
		return
	}

	for _, cat := range weaknessList {
		types, _ := cat["types"].([]map[string]any)
		var typeEmojis []string
		for _, entry := range types {
			if key, ok := entry["emojiKey"].(string); ok && key != "" {
				resolved := emojiLookup.Lookup(key, platform)
				entry["emoji"] = resolved
				typeEmojis = append(typeEmojis, resolved)
			}
		}
		cat["typeEmoji"] = strings.Join(typeEmojis, "")
	}
}

// buildComputedFields creates the small set of derived fields.
func buildComputedFields(templateType string, base, perLang map[string]any, emoji map[string]string, areas []webhook.MatchedArea) map[string]any {
	m := make(map[string]any, 16)

	// id = pokemon_id
	if v, ok := base["pokemon_id"]; ok {
		m["id"] = v
	}

	// time: for eggs = hatchTime (when egg hatches), for everything else = disappearTime
	if templateType == "egg" {
		if v, ok := base["hatchTime"]; ok {
			m["time"] = v
		}
	} else {
		if v, ok := base["disappearTime"]; ok {
			m["time"] = v
		}
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
	// matched: lowercase area names for {{#each matched}}{{map 'arealist' this}}
	matched := make([]string, len(areaNames))
	for i, name := range areaNames {
		matched[i] = strings.ToLower(name)
	}
	m["matched"] = matched

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

// buildAliasLookup returns a map of alias → source field name for a given template type.
// Cached per type since the alias tables are static. Uses sync.Map for safe concurrent access
// from render worker goroutines.
var aliasLookupCache sync.Map

func buildAliasLookup(templateType string) map[string]string {
	if cached, ok := aliasLookupCache.Load(templateType); ok {
		return cached.(map[string]string)
	}
	m := make(map[string]string, len(commonAliases)+10)
	for _, a := range commonAliases {
		m[a.alias] = a.source
	}
	if typeSpecific, ok := typeAliases[templateType]; ok {
		for _, a := range typeSpecific {
			m[a.alias] = a.source
		}
	}
	aliasLookupCache.Store(templateType, m)
	return m
}

// composeWeatherChange builds the weatherChange text from forecast fields.
// Format matches JS: "⚠️ {Possible weather change at} {time} : {currentEmoji} {current} ➡️ {nextEmoji} {next}"
// When weatherCurrent is unknown: "⚠️ {Possible weather change at} {time} : ➡️ {nextEmoji} {next}"
func composeWeatherChange(computed map[string]any, base, perLang map[string]any, emoji *EmojiLookup, platform string) {
	// weatherNext is only set when the base enrichment determines the weather
	// change actually affects the pokemon's boost status. weatherForecastNext
	// is the raw forecast and may be the same as current — don't use it here.
	weatherNext, _ := lookupField(base, perLang, "weatherNext").(int)
	if weatherNext == 0 {
		return
	}

	weatherChangeTime, _ := lookupField(base, perLang, "weatherChangeTime").(string)
	if weatherChangeTime == "" {
		return
	}

	// Get translated weather change prefix from per-language enrichment
	prefix, _ := lookupField(base, perLang, "weatherChangePossibleAt").(string)
	if prefix == "" {
		prefix = "Possible weather change at"
	}

	nextName, _ := lookupField(base, perLang, "weatherNextName").(string)
	nextEmojiKey, _ := lookupField(base, perLang, "weatherNextEmojiKey").(string)
	nextEmoji := ""
	if nextEmojiKey != "" && emoji != nil {
		nextEmoji = emoji.Lookup(nextEmojiKey, platform)
	}

	weatherCurrent, _ := lookupField(base, perLang, "weatherCurrent").(int)
	if weatherCurrent == 0 {
		// Unknown current weather
		currentName, _ := lookupField(base, perLang, "weatherCurrentUnknown").(string)
		if currentName == "" {
			currentName = "unknown"
		}
		computed["weatherCurrentName"] = currentName
		computed["weatherCurrentEmoji"] = "❓"
		computed["weatherChange"] = fmt.Sprintf("⚠️ %s %s : ➡️ %s %s", prefix, weatherChangeTime, nextName, nextEmoji)
	} else {
		currentName, _ := lookupField(base, perLang, "weatherCurrentName").(string)
		currentEmojiKey, _ := lookupField(base, perLang, "weatherCurrentEmojiKey").(string)
		currentEmoji := ""
		if currentEmojiKey != "" && emoji != nil {
			currentEmoji = emoji.Lookup(currentEmojiKey, platform)
		}
		computed["weatherChange"] = fmt.Sprintf("⚠️ %s %s : %s %s ➡️ %s %s", prefix, weatherChangeTime, currentName, currentEmoji, nextName, nextEmoji)
	}
}

// Flatten merges all layers into a single flat map for API responses.
// This produces the same field resolution as GetField but as a materialized map.
func (lv *LayeredView) Flatten() map[string]any {
	// Start with lowest priority, overlay higher
	result := make(map[string]any, 128)

	// 8. DTS dictionary
	maps.Copy(result, lv.dtsDict)
	// 7. Raw webhook
	maps.Copy(result, lv.webhook)
	// 5. Base enrichment
	maps.Copy(result, lv.base)
	// 4. Computed fields
	maps.Copy(result, lv.computed)
	// 3. Per-language
	maps.Copy(result, lv.perLang)
	// 2. Emoji
	for k, v := range lv.emoji {
		result[k] = v
	}
	// 1. Per-user
	maps.Copy(result, lv.perUser)

	// 6. Aliases — resolve each alias from the result (not from a separate layer)
	for alias, source := range lv.aliases {
		if _, already := result[alias]; already {
			continue // direct field takes precedence over alias
		}
		if v, ok := result[source]; ok {
			result[alias] = v
		}
	}

	return result
}

// lookupField checks perLang first, then base, for a field value.
func lookupField(base, perLang map[string]any, key string) any {
	if perLang != nil {
		if v, ok := perLang[key]; ok {
			return v
		}
	}
	if base != nil {
		if v, ok := base[key]; ok {
			return v
		}
	}
	return nil
}
