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
//  3. localOverrides (per-LayeredView resolved data, e.g. weaknessList with emoji)
//  4. perLang enrichment (translated names, types, moves)
//  5. computed fields (tthh/tthm/tths, areas, time, id, megaName)
//  6. aliases (field name mappings)
//  7. base enrichment (universal: weather, maps, icons, geocoding)
//  8. dtsDictionary (user-defined config overrides)
type LayeredView struct {
	base           map[string]any
	original       map[string]any // prior-sighting snapshot exposed as {{original.X}} (pokemon-changed)
	perLang        map[string]any
	perUser        map[string]any
	emoji          map[string]string // resolved emoji key → string
	localOverrides map[string]any    // per-LayeredView resolved fields (e.g. weaknessList with emoji filled in)
	aliases        map[string]string // alias name → source field name
	computed       map[string]any    // pre-computed fields (tthh, areas, etc.)
	webhook        map[string]any    // raw webhook fields (lowest priority, for custom DTS)
	dtsDict        map[string]any
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

	// Resolve emoji (small map, only populated keys). Pass perUser so
	// per-user keys like bearingEmojiKey (set by PokemonPerUser) resolve.
	lv.emoji = vb.resolveEmojiMap(base, perLang, perUser, platform)

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

	// Resolve emoji keys inside weaknessList entries (per-platform) and
	// build the flat {{weaknessEmoji}} string for templates that don't
	// iterate weaknessList.
	//
	// The resolved weaknessList (with per-entry "emoji" and per-category
	// "typeEmoji" fields) is stored in lv.localOverrides — a per-LayeredView
	// map checked BEFORE perLang. This keeps the shared perLang enrichment
	// map immutable: concurrent render workers fan out over the same
	// *Enrichment without racing on weakness map writes.
	lv.localOverrides = make(map[string]any, 2)
	resolveWeaknessEmojis(vb.emoji, perLang, platform, lv.computed, lv.localOverrides)

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
	// 0. Special-case: `original` exposes the prior-sighting snapshot as a
	// nested map so templates can write {{original.fullName}}, {{original.cp}},
	// etc. raymond recurses into the returned map. The short-circuit must
	// come before the layer cascade so it can't be shadowed by an "original"
	// key accidentally placed into base/perLang/etc.
	if name == "original" {
		if lv.original == nil {
			return nil, false
		}
		return lv.original, true
	}

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

	// 3. Local overrides (per-LayeredView resolved fields, e.g. weaknessList
	// with emoji filled in). Checked before perLang so the resolved copy
	// shadows the shared (un-resolved) perLang entry. The map is small and
	// allocated per LayeredView, keeping shared enrichment immutable.
	if v, ok := lv.localOverrides[name]; ok {
		return v, true
	}

	// 4. Per-language enrichment (translations)
	if lv.perLang != nil {
		if v, ok := lv.perLang[name]; ok {
			return v, true
		}
	}

	// 5. Computed fields (tthh, areas, time, etc.)
	if v, ok := lv.computed[name]; ok {
		return v, true
	}

	// 6. Base enrichment
	if v, ok := lv.base[name]; ok {
		return v, true
	}

	// 7. Aliases (check if name is an alias, resolve source from all layers)
	if source, ok := lv.aliases[name]; ok {
		if v, found := lv.resolveSource(source); found {
			return v, true
		}
	}

	// 8. Raw webhook fields (lowest priority — supports custom DTS templates)
	if lv.webhook != nil {
		if v, ok := lv.webhook[name]; ok {
			return v, true
		}
	}

	// 9. DTS dictionary
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
	if v, ok := lv.localOverrides[name]; ok {
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
// Checks perUser → perLang → base in that order so per-user keys like
// bearingEmojiKey (populated by PokemonPerUser) take precedence without
// shadowing more general per-language and base keys.
func (vb *ViewBuilder) resolveEmojiMap(base, perLang, perUser map[string]any, platform string) map[string]string {
	if vb.emoji == nil {
		return nil
	}

	result := make(map[string]string, 16)

	// Resolve single emoji keys from perUser → perLang → base
	for _, m := range singleEmojiKeys {
		key := ""
		if perUser != nil {
			if k, ok := perUser[m.keyField].(string); ok && k != "" {
				key = k
			}
		}
		if key == "" && perLang != nil {
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

	// Resolve array emoji keys (no per-user array keys today, but check
	// perUser first for symmetry with the single-key path).
	for _, m := range arrayEmojiKeys {
		var raw any
		if perUser != nil {
			raw = perUser[m.keyField]
		}
		if raw == nil && perLang != nil {
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
	if perUser != nil {
		typeRaw = perUser["typeEmojiKeys"]
	}
	if typeRaw == nil && perLang != nil {
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

// resolveWeaknessEmojis resolves emoji keys inside weaknessList entries
// and writes a flat {{weaknessEmoji}} string into the computed layer.
//
// Per category: each type entry gets "emoji" resolved (per-platform),
// and the category gets "typeEmoji" as a joined string. The flat
// weaknessEmoji is the space-separated concatenation of
// "{value}x{typeEmoji}" per category — the field many shipped templates
// reference directly without iterating weaknessList.
//
// Resolution must happen during view construction because it requires
// the platform for the emoji lookup.
//
// The resolved weaknessList is written to localOverrides, NOT back into
// perLang. The perLang map is shared across all render workers that
// process the same language group; mutating it in-place causes fatal
// "concurrent map writes" panics when two workers run concurrently (e.g.
// raid-rsvp's fullUsers + rsvpUsers partition). localOverrides is
// allocated fresh per LayeredView and is never shared.
func resolveWeaknessEmojis(emojiLookup *EmojiLookup, perLang map[string]any, platform string, computed, localOverrides map[string]any) {
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

	// Deep-copy the weakness list before adding emoji fields. The underlying
	// perLang maps are shared across render workers; mutating in-place races.
	// Building a local copy keeps all writes isolated to this LayeredView.
	localList := make([]map[string]any, 0, len(weaknessList))
	var flat strings.Builder
	for _, cat := range weaknessList {
		// Copy the category map so we can add "typeEmoji" without touching shared state.
		catCopy := make(map[string]any, len(cat)+1)
		for k, v := range cat {
			catCopy[k] = v
		}

		types, _ := cat["types"].([]map[string]any)
		var typeEmojis []string
		if len(types) > 0 {
			// Copy each type entry so we can add "emoji" without touching shared state.
			typesCopy := make([]map[string]any, 0, len(types))
			for _, entry := range types {
				entryCopy := make(map[string]any, len(entry)+1)
				for k, v := range entry {
					entryCopy[k] = v
				}
				if key, ok := entry["emojiKey"].(string); ok && key != "" {
					resolved := emojiLookup.Lookup(key, platform)
					entryCopy["emoji"] = resolved
					typeEmojis = append(typeEmojis, resolved)
				}
				typesCopy = append(typesCopy, entryCopy)
			}
			catCopy["types"] = typesCopy
		}

		joined := strings.Join(typeEmojis, "")
		catCopy["typeEmoji"] = joined
		if joined != "" {
			fmt.Fprintf(&flat, "%vx%s ", cat["value"], joined)
		}
		localList = append(localList, catCopy)
	}

	// Surface the resolved list in localOverrides (checked before perLang in
	// GetField), so templates referencing {{weaknessList}} get the copy with
	// emoji fields populated. The shared perLang["weaknessList"] is untouched.
	if localOverrides != nil {
		localOverrides["weaknessList"] = localList
	}
	if flat.Len() > 0 && computed != nil {
		computed["weaknessEmoji"] = flat.String()
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

	currentName, _ := lookupField(base, perLang, "weatherCurrentName").(string)
	weatherCurrent, _ := lookupField(base, perLang, "weatherCurrent").(int)
	if weatherCurrent == 0 {
		// De-boost case: per-language sets weatherCurrentName to translated
		// "unknown"; JS hardcodes ❓ for the emoji.
		computed["weatherCurrentEmoji"] = "❓"
		computed["weatherChange"] = fmt.Sprintf("⚠️ %s %s : ➡️ %s %s", prefix, weatherChangeTime, nextName, nextEmoji)
	} else {
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

	// 9. DTS dictionary
	maps.Copy(result, lv.dtsDict)
	// 8. Raw webhook
	maps.Copy(result, lv.webhook)
	// 6. Base enrichment
	maps.Copy(result, lv.base)
	// 5. Computed fields
	maps.Copy(result, lv.computed)
	// 4. Per-language
	maps.Copy(result, lv.perLang)
	// 3. Local overrides (resolved weaknessList etc. — shadows perLang)
	maps.Copy(result, lv.localOverrides)
	// 2. Emoji
	for k, v := range lv.emoji {
		result[k] = v
	}
	// 1. Per-user
	maps.Copy(result, lv.perUser)

	// 7. Aliases — resolve each alias from the result (not from a separate layer)
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
