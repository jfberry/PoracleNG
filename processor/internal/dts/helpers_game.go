package dts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	raymond "github.com/mailgun/raymond/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

var gameHelpersOnce sync.Once

// RegisterGameHelpers registers Handlebars helpers that depend on game data,
// translations, and emoji. Helpers read user language/platform from the
// template's private data frame (@language, @platform, @altLanguage).
//
// Safe to call multiple times; registration happens only once.
func RegisterGameHelpers(gd *gamedata.GameData, bundle *i18n.Bundle, emoji *EmojiLookup, configDir string) {
	gameHelpersOnce.Do(func() {
		registerPokemonHelpers(gd, bundle, emoji)
		registerMoveHelpers(gd, bundle, emoji)
		registerMiscGameHelpers(gd, bundle, emoji, configDir)
	})
}

// ---------------------------------------------------------------------------
// Data frame accessors
// ---------------------------------------------------------------------------

func getLang(options *raymond.Options) string {
	if v, ok := options.DataFrame().Get("language").(string); ok && v != "" {
		return v
	}
	return "en"
}

func getAltLang(options *raymond.Options) string {
	if v, ok := options.DataFrame().Get("altLanguage").(string); ok && v != "" {
		return v
	}
	return "en"
}

func getPlatform(options *raymond.Options) string {
	if v, ok := options.DataFrame().Get("platform").(string); ok && v != "" {
		return v
	}
	return "discord"
}

// ---------------------------------------------------------------------------
// Pokemon helpers
// ---------------------------------------------------------------------------

func registerPokemonHelpers(gd *gamedata.GameData, bundle *i18n.Bundle, emoji *EmojiLookup) {
	raymond.RegisterHelper("pokemonName", func(id any, options *raymond.Options) any {
		return gamedata.PokemonName(bundle.For(getLang(options)), int(toFloat(id)))
	})

	raymond.RegisterHelper("pokemonNameEng", func(id any) any {
		return gamedata.PokemonName(bundle.For("en"), int(toFloat(id)))
	})

	raymond.RegisterHelper("pokemonNameAlt", func(id any, options *raymond.Options) any {
		return gamedata.PokemonName(bundle.For(getAltLang(options)), int(toFloat(id)))
	})

	raymond.RegisterHelper("pokemonForm", func(formID any, options *raymond.Options) any {
		return gamedata.FormName(bundle.For(getLang(options)), int(toFloat(formID)))
	})

	raymond.RegisterHelper("pokemonFormEng", func(formID any) any {
		return gamedata.FormName(bundle.For("en"), int(toFloat(formID)))
	})

	raymond.RegisterHelper("pokemonFormAlt", func(formID any, options *raymond.Options) any {
		return gamedata.FormName(bundle.For(getAltLang(options)), int(toFloat(formID)))
	})

	// pokemon — block helper providing rich pokemon context.
	// Usage: {{#pokemon id}}...{{/pokemon}} or {{#pokemon id formId}}...{{/pokemon}}
	// Options-only so the form is optional: a fixed (id, form, options)
	// signature makes raymond panic when the form is omitted.
	raymond.RegisterHelper("pokemon", func(options *raymond.Options) any {
		var pid, formID int
		if params := options.Params(); len(params) > 0 {
			pid = int(toFloat(params[0]))
			if len(params) > 1 {
				formID = int(toFloat(params[1]))
			}
		}

		lang := getLang(options)
		platform := getPlatform(options)
		tr := bundle.For(lang)
		trEn := bundle.For("en")

		name := tr.T(fmt.Sprintf("poke_%d", pid))
		nameEng := trEn.T(fmt.Sprintf("poke_%d", pid))

		formName := ""
		formNameEng := ""
		if formID > 0 {
			formName = tr.T(fmt.Sprintf("form_%d", formID))
			formNameEng = trEn.T(fmt.Sprintf("form_%d", formID))
		}

		fullName := buildFullName(name, formName, formNameEng)
		fullNameEng := buildFullName(nameEng, formNameEng, formNameEng)

		var typeNames, typeNamesEng, typeEmoji []string
		baseStats := map[string]any{
			"baseAttack": 0, "baseDefense": 0, "baseStamina": 0,
		}
		hasEvolutions := false

		if gd != nil {
			if mon := gd.GetMonster(pid, formID); mon != nil {
				baseStats["baseAttack"] = mon.Attack
				baseStats["baseDefense"] = mon.Defense
				baseStats["baseStamina"] = mon.Stamina
				hasEvolutions = len(mon.Evolutions) > 0 || len(mon.TempEvolutions) > 0

				for _, tid := range mon.Types {
					typeNames = append(typeNames, tr.T(fmt.Sprintf("poke_type_%d", tid)))
					typeNamesEng = append(typeNamesEng, trEn.T(fmt.Sprintf("poke_type_%d", tid)))
					if ti, ok := gd.Types[tid]; ok {
						typeEmoji = append(typeEmoji, emoji.Lookup(ti.Emoji, platform))
					}
				}
			}
		}

		ctx := map[string]any{
			"name":          name,
			"nameEng":       nameEng,
			"formName":      formName,
			"formNameEng":   formNameEng,
			"fullName":      fullName,
			"fullNameEng":   fullNameEng,
			"typeName":      typeNames,
			"typeNameEng":   typeNamesEng,
			"typeEmoji":     typeEmoji,
			"baseStats":     baseStats,
			"hasEvolutions": hasEvolutions,
		}
		if !options.IsBlock() {
			return fullName
		}
		return options.FnWith(ctx)
	})

	raymond.RegisterHelper("pokemonBaseStats", func(id, form any) any {
		if gd == nil {
			return map[string]any{"baseAttack": 0, "baseDefense": 0, "baseStamina": 0}
		}
		mon := gd.GetMonster(int(toFloat(id)), int(toFloat(form)))
		if mon == nil {
			return map[string]any{"baseAttack": 0, "baseDefense": 0, "baseStamina": 0}
		}
		return map[string]any{
			"baseAttack":  mon.Attack,
			"baseDefense": mon.Defense,
			"baseStamina": mon.Stamina,
		}
	})

	raymond.RegisterHelper("calculateCp", func(baseStatsOrID any, args ...any) any {
		var baseAtk, baseDef, baseSta int
		var level float64
		var ivAtk, ivDef, ivSta int

		switch v := baseStatsOrID.(type) {
		case map[string]int:
			// Enrichment stores baseStats as map[string]int
			if len(args) < 4 {
				return 10
			}
			baseAtk = v["baseAttack"]
			baseDef = v["baseDefense"]
			baseSta = v["baseStamina"]
			level = toFloat(args[0])
			ivAtk = int(toFloat(args[1]))
			ivDef = int(toFloat(args[2]))
			ivSta = int(toFloat(args[3]))
		case map[string]any:
			if len(args) < 4 {
				return 10
			}
			baseAtk = int(toFloat(v["baseAttack"]))
			baseDef = int(toFloat(v["baseDefense"]))
			baseSta = int(toFloat(v["baseStamina"]))
			level = toFloat(args[0])
			ivAtk = int(toFloat(args[1]))
			ivDef = int(toFloat(args[2]))
			ivSta = int(toFloat(args[3]))
		default:
			if len(args) < 5 {
				return 10
			}
			pid := int(toFloat(baseStatsOrID))
			fid := int(toFloat(args[0]))
			if gd != nil {
				if mon := gd.GetMonster(pid, fid); mon != nil {
					baseAtk = mon.Attack
					baseDef = mon.Defense
					baseSta = mon.Stamina
				}
			}
			level = toFloat(args[1])
			ivAtk = int(toFloat(args[2]))
			ivDef = int(toFloat(args[3]))
			ivSta = int(toFloat(args[4]))
		}

		cpm := getCPMultiplier(gd, level)
		attack := float64(baseAtk+ivAtk) * cpm
		defense := float64(baseDef+ivDef) * cpm
		stamina := float64(baseSta+ivSta) * cpm
		cp := max(int(math.Floor(attack*math.Sqrt(defense)*math.Sqrt(stamina)/10.0)), 10)
		return cp
	})
}

// buildFullName constructs "Name (Form)" but skips form if it's "Normal".
func buildFullName(name, formName, formNameEng string) string {
	if formName == "" {
		return name
	}
	if strings.EqualFold(formNameEng, "Normal") {
		return name
	}
	return name + " (" + formName + ")"
}

// ---------------------------------------------------------------------------
// Move helpers
// ---------------------------------------------------------------------------

// lookupMove resolves a Handlebars-supplied move id (any of int / float64
// / numeric string) to a *gamedata.Move, returning nil when gamedata is
// absent or the id is unknown. Shared by the moveType / moveEmoji
// helpers — both want the same "no data → empty string" semantics.
func lookupMove(gd *gamedata.GameData, moveID any) *gamedata.Move {
	if gd == nil {
		return nil
	}
	return gd.GetMove(int(toFloat(moveID)))
}

func registerMoveHelpers(gd *gamedata.GameData, bundle *i18n.Bundle, emoji *EmojiLookup) {
	raymond.RegisterHelper("moveName", func(moveID any, options *raymond.Options) any {
		return gamedata.MoveName(bundle.For(getLang(options)), int(toFloat(moveID)))
	})

	raymond.RegisterHelper("moveNameEng", func(moveID any) any {
		return gamedata.MoveName(bundle.For("en"), int(toFloat(moveID)))
	})

	raymond.RegisterHelper("moveNameAlt", func(moveID any, options *raymond.Options) any {
		return gamedata.MoveName(bundle.For(getAltLang(options)), int(toFloat(moveID)))
	})

	raymond.RegisterHelper("moveType", func(moveID any, options *raymond.Options) any {
		if move := lookupMove(gd, moveID); move != nil {
			return gamedata.TypeName(bundle.For(getLang(options)), move.TypeID)
		}
		return ""
	})

	raymond.RegisterHelper("moveTypeEng", func(moveID any) any {
		if move := lookupMove(gd, moveID); move != nil {
			return gamedata.TypeName(bundle.For("en"), move.TypeID)
		}
		return ""
	})

	raymond.RegisterHelper("moveTypeAlt", func(moveID any, options *raymond.Options) any {
		if move := lookupMove(gd, moveID); move != nil {
			return gamedata.TypeName(bundle.For(getAltLang(options)), move.TypeID)
		}
		return ""
	})

	// moveEmoji, moveEmojiEng, moveEmojiAlt — all resolve by type emoji key + platform
	moveEmojiFunc := func(moveID any, options *raymond.Options) any {
		move := lookupMove(gd, moveID)
		if move == nil {
			return ""
		}
		if ti, ok := gd.Types[move.TypeID]; ok {
			return emoji.Lookup(ti.Emoji, getPlatform(options))
		}
		return ""
	}

	raymond.RegisterHelper("moveEmoji", moveEmojiFunc)
	raymond.RegisterHelper("moveEmojiEng", moveEmojiFunc)
	raymond.RegisterHelper("moveEmojiAlt", moveEmojiFunc)
}

// ---------------------------------------------------------------------------
// Miscellaneous game helpers
// ---------------------------------------------------------------------------

func registerMiscGameHelpers(gd *gamedata.GameData, bundle *i18n.Bundle, emoji *EmojiLookup, configDir string) {
	raymond.RegisterHelper("getEmoji", func(key any, options *raymond.Options) any {
		return emoji.Lookup(toString(key), getPlatform(options))
	})

	raymond.RegisterHelper("translateAlt", func(text any, options *raymond.Options) any {
		return bundle.For(getAltLang(options)).T(toString(text))
	})

	raymond.RegisterHelper("getPowerUpCost", func(startLevel, endLevel any, options *raymond.Options) any {
		start := toFloat(startLevel)
		end := toFloat(endLevel)
		stardust, candy, xlCandy := calculatePowerUpCost(gd, start, end)
		result := map[string]any{
			"stardust": stardust,
			"candy":    candy,
			"xlCandy":  xlCandy,
		}
		// Block mode: {{#getPowerUpCost ...}}{{stardust}} dust{{/getPowerUpCost}}
		// Inline mode: {{getPowerUpCost ...}} → "X stardust, Y candy, Z XL candy"
		// FnWith returns empty for inline (raymond pops enclosing block during body eval)
		rendered := options.FnWith(result)
		if rendered != "" {
			return rendered
		}
		return fmt.Sprintf("%d stardust, %d candy, %d XL candy", stardust, candy, xlCandy)
	})

	customMaps := loadCustomMaps(configDir)
	activeCustomMaps.Store(customMaps)

	raymond.RegisterHelper("map", func(mapName, value any, options *raymond.Options) any {
		v, ok := lookupCustomMap(customMaps, toString(mapName), toString(value), "", getLang(options))
		return customMapResult(v, ok, options)
	})

	raymond.RegisterHelper("map2", func(mapName, value, value2 any, options *raymond.Options) any {
		v, ok := lookupCustomMap(customMaps, toString(mapName), toString(value), toString(value2), getLang(options))
		return customMapResult(v, ok, options)
	})
}

// customMapResult shapes a custom map lookup for the three ways {{map}} and
// {{map2}} are used, matching PoracleJS:
//   - block ({{#map 'name' key}}...{{/map}}): renders the body with the
//     looked-up value as context, so object values expose their fields
//     ({{genName}}). A missing key renders the {{else}} branch (if any).
//   - subexpression ({{#if (map 'name' key)}}): returns the raw value, so a
//     missing key is falsy and an object value is truthy.
//   - inline ({{map 'name' key}}): returns the value; missing keys and object
//     values render as empty.
func customMapResult(v any, found bool, options *raymond.Options) any {
	if options.IsSubExpression() {
		if !found {
			return nil
		}
		return v
	}
	if options.IsBlock() {
		if !found {
			return options.Inverse()
		}
		return options.FnWith(v)
	}
	if !found {
		return ""
	}
	switch v.(type) {
	case map[string]any, []any:
		return ""
	}
	return v
}

// ---------------------------------------------------------------------------
// getCPMultiplier returns the CP multiplier for a given level from util.json data.
func getCPMultiplier(gd *gamedata.GameData, level float64) float64 {
	if gd != nil && gd.Util != nil && gd.Util.CpMultipliers != nil {
		key := strconv.FormatFloat(level, 'f', -1, 64)
		if v, ok := gd.Util.CpMultipliers[key]; ok {
			return v
		}
	}
	return 0.7903 // fallback to level 40
}

// ---------------------------------------------------------------------------
// Power-up cost table
// ---------------------------------------------------------------------------

func calculatePowerUpCost(gd *gamedata.GameData, startLevel, endLevel float64) (stardust, candy, xlCandy int) {
	if gd == nil || gd.Util == nil || gd.Util.PowerUpCost == nil {
		return
	}
	level := startLevel
	for level < endLevel {
		key := strconv.FormatFloat(level, 'f', -1, 64)
		if entry, ok := gd.Util.PowerUpCost[key]; ok {
			stardust += entry.Stardust
			candy += entry.Candy
			xlCandy += entry.XLCandy
		}
		level += 0.5
	}
	return
}

// ---------------------------------------------------------------------------
// Custom maps
// ---------------------------------------------------------------------------

// activeCustomMaps is the store the map/map2 helpers were registered with,
// exposed through CustomMaps so the DTS editor can preview lookups.
var activeCustomMaps atomic.Pointer[customMapStore]

// CustomMaps returns the loaded custom maps keyed by lookup key — the map
// name, or "name.language" for a language variant. Callers must treat the
// inner maps as read-only. Empty (never nil) before helpers are registered.
func CustomMaps() map[string]map[string]any {
	return activeCustomMaps.Load().snapshot()
}

type customMapStore struct {
	mu   sync.RWMutex
	maps map[string]map[string]any // keyed by map name, or "name.language"
}

// snapshot returns a copy of the outer map. The inner maps are shared; they
// are never modified after loading.
func (s *customMapStore) snapshot() map[string]map[string]any {
	if s == nil {
		return map[string]map[string]any{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]map[string]any, len(s.maps))
	maps.Copy(out, s.maps)
	return out
}

// customMapDef is one map definition in the PoracleJS customMaps format.
type customMapDef struct {
	Name     string         `json:"name"`
	Language string         `json:"language"`
	Map      map[string]any `json:"map"`
}

// loadCustomMaps reads config/customMaps/*.json. Each file may hold:
//  1. a PoracleJS definition: {"name": "arealist", "language": "de", "map": {...}}
//  2. an array of PoracleJS definitions (several maps in one file)
//  3. a flat map {"key": "value"}, named after the file ("arealist.de.json"
//     is the German variant of "arealist")
//
// Map values may be strings, numbers or objects; object values are used with
// block syntax: {{#map 'generation' pokemonId}}{{genName}}{{/map}}.
func loadCustomMaps(configDir string) *customMapStore {
	store := &customMapStore{maps: make(map[string]map[string]any)}
	if configDir == "" {
		return store
	}
	mapsDir := filepath.Join(configDir, "customMaps")
	if err := os.MkdirAll(mapsDir, 0755); err != nil {
		log.Warnf("dts: create customMaps dir: %v", err)
		return store
	}
	entries, err := os.ReadDir(mapsDir)
	if err != nil {
		log.Warnf("dts: read customMaps dir: %v", err)
		return store
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(mapsDir, e.Name()))
		if err != nil {
			log.Warnf("dts: read custom map %s: %v", e.Name(), err)
			continue
		}
		defs, err := parseCustomMapFile(data, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		if err != nil {
			log.Warnf("dts: parse custom map %s: %v", e.Name(), err)
			continue
		}
		for _, d := range defs {
			key := d.Name
			if d.Language != "" {
				key += "." + d.Language
			}
			store.maps[key] = d.Map
			log.Debugf("dts: loaded custom map %s from %s (%d entries)", key, e.Name(), len(d.Map))
		}
	}
	return store
}

// parseCustomMapFile decodes one customMaps file into its map definitions.
// fileName (without extension) names a flat-format map.
func parseCustomMapFile(data []byte, fileName string) ([]customMapDef, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var defs []customMapDef
		if err := json.Unmarshal(trimmed, &defs); err != nil {
			return nil, err
		}
		out := defs[:0]
		for _, d := range defs {
			if d.Name == "" || d.Map == nil {
				log.Warnf("dts: custom map file %s.json: skipping entry without name/map", fileName)
				continue
			}
			out = append(out, d)
		}
		return out, nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, err
	}
	if _, ok := raw["map"]; ok {
		var d customMapDef
		if err := json.Unmarshal(trimmed, &d); err != nil {
			return nil, err
		}
		if d.Map != nil {
			if d.Name == "" {
				d.Name = fileName
			}
			return []customMapDef{d}, nil
		}
	}

	var flat map[string]any
	if err := json.Unmarshal(trimmed, &flat); err != nil {
		return nil, err
	}
	d := customMapDef{Name: fileName}
	if base, lang, ok := strings.Cut(fileName, "."); ok {
		d.Name, d.Language = base, lang
	}
	d.Map = flat
	return []customMapDef{d}, nil
}

// lookupCustomMap returns the value for value (or fallbackValue) in the named
// map, preferring the user's language variant. found is false when neither key
// exists, so templates can treat a miss as empty/falsy like PoracleJS did.
func lookupCustomMap(store *customMapStore, mapName, value, fallbackValue, lang string) (any, bool) {
	if store == nil {
		return nil, false
	}
	store.mu.RLock()
	defer store.mu.RUnlock()

	for _, name := range []string{mapName + "." + lang, mapName} {
		m, ok := store.maps[name]
		if !ok {
			continue
		}
		if v, ok := m[value]; ok {
			return v, true
		}
		if fallbackValue != "" {
			if v, ok := m[fallbackValue]; ok {
				return v, true
			}
		}
	}
	return nil, false
}
