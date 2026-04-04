package dts

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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
	raymond.RegisterHelper("pokemonName", func(id interface{}, options *raymond.Options) interface{} {
		pid := int(toFloat(id))
		lang := getLang(options)
		key := fmt.Sprintf("poke_%d", pid)
		name := bundle.For(lang).T(key)
		if name == key {
			return fmt.Sprintf("%d", pid)
		}
		return name
	})

	raymond.RegisterHelper("pokemonNameEng", func(id interface{}) interface{} {
		pid := int(toFloat(id))
		key := fmt.Sprintf("poke_%d", pid)
		name := bundle.For("en").T(key)
		if name == key {
			return fmt.Sprintf("%d", pid)
		}
		return name
	})

	raymond.RegisterHelper("pokemonNameAlt", func(id interface{}, options *raymond.Options) interface{} {
		pid := int(toFloat(id))
		lang := getAltLang(options)
		key := fmt.Sprintf("poke_%d", pid)
		name := bundle.For(lang).T(key)
		if name == key {
			return fmt.Sprintf("%d", pid)
		}
		return name
	})

	raymond.RegisterHelper("pokemonForm", func(formID interface{}, options *raymond.Options) interface{} {
		fid := int(toFloat(formID))
		return bundle.For(getLang(options)).T(fmt.Sprintf("form_%d", fid))
	})

	raymond.RegisterHelper("pokemonFormEng", func(formID interface{}) interface{} {
		fid := int(toFloat(formID))
		return bundle.For("en").T(fmt.Sprintf("form_%d", fid))
	})

	raymond.RegisterHelper("pokemonFormAlt", func(formID interface{}, options *raymond.Options) interface{} {
		fid := int(toFloat(formID))
		return bundle.For(getAltLang(options)).T(fmt.Sprintf("form_%d", fid))
	})

	// pokemon — block helper providing rich pokemon context.
	// Usage: {{#pokemon id}}...{{/pokemon}} or {{#pokemon id formId}}...{{/pokemon}}
	raymond.RegisterHelper("pokemon", func(id, form interface{}, options *raymond.Options) interface{} {
		pid := int(toFloat(id))
		formID := int(toFloat(form))

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
		baseStats := map[string]interface{}{
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

		ctx := map[string]interface{}{
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
		return options.FnWith(ctx)
	})

	raymond.RegisterHelper("pokemonBaseStats", func(id, form interface{}) interface{} {
		if gd == nil {
			return map[string]interface{}{"baseAttack": 0, "baseDefense": 0, "baseStamina": 0}
		}
		mon := gd.GetMonster(int(toFloat(id)), int(toFloat(form)))
		if mon == nil {
			return map[string]interface{}{"baseAttack": 0, "baseDefense": 0, "baseStamina": 0}
		}
		return map[string]interface{}{
			"baseAttack":  mon.Attack,
			"baseDefense": mon.Defense,
			"baseStamina": mon.Stamina,
		}
	})

	raymond.RegisterHelper("calculateCp", func(baseStatsOrID interface{}, args ...interface{}) interface{} {
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
		case map[string]interface{}:
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
		cp := int(math.Floor(attack * math.Sqrt(defense) * math.Sqrt(stamina) / 10.0))
		if cp < 10 {
			cp = 10
		}
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

func registerMoveHelpers(gd *gamedata.GameData, bundle *i18n.Bundle, emoji *EmojiLookup) {
	raymond.RegisterHelper("moveName", func(moveID interface{}, options *raymond.Options) interface{} {
		return bundle.For(getLang(options)).T(fmt.Sprintf("move_%d", int(toFloat(moveID))))
	})

	raymond.RegisterHelper("moveNameEng", func(moveID interface{}) interface{} {
		return bundle.For("en").T(fmt.Sprintf("move_%d", int(toFloat(moveID))))
	})

	raymond.RegisterHelper("moveNameAlt", func(moveID interface{}, options *raymond.Options) interface{} {
		return bundle.For(getAltLang(options)).T(fmt.Sprintf("move_%d", int(toFloat(moveID))))
	})

	raymond.RegisterHelper("moveType", func(moveID interface{}, options *raymond.Options) interface{} {
		if gd == nil {
			return ""
		}
		move := gd.GetMove(int(toFloat(moveID)))
		if move == nil {
			return ""
		}
		return bundle.For(getLang(options)).T(fmt.Sprintf("poke_type_%d", move.TypeID))
	})

	raymond.RegisterHelper("moveTypeEng", func(moveID interface{}) interface{} {
		if gd == nil {
			return ""
		}
		move := gd.GetMove(int(toFloat(moveID)))
		if move == nil {
			return ""
		}
		return bundle.For("en").T(fmt.Sprintf("poke_type_%d", move.TypeID))
	})

	raymond.RegisterHelper("moveTypeAlt", func(moveID interface{}, options *raymond.Options) interface{} {
		if gd == nil {
			return ""
		}
		move := gd.GetMove(int(toFloat(moveID)))
		if move == nil {
			return ""
		}
		return bundle.For(getAltLang(options)).T(fmt.Sprintf("poke_type_%d", move.TypeID))
	})

	// moveEmoji, moveEmojiEng, moveEmojiAlt — all resolve by type emoji key + platform
	moveEmojiFunc := func(moveID interface{}, options *raymond.Options) interface{} {
		if gd == nil {
			return ""
		}
		move := gd.GetMove(int(toFloat(moveID)))
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
	raymond.RegisterHelper("getEmoji", func(key interface{}, options *raymond.Options) interface{} {
		return emoji.Lookup(toString(key), getPlatform(options))
	})

	raymond.RegisterHelper("translateAlt", func(text interface{}, options *raymond.Options) interface{} {
		return bundle.For(getAltLang(options)).T(toString(text))
	})

	raymond.RegisterHelper("getPowerUpCost", func(startLevel, endLevel interface{}, options *raymond.Options) interface{} {
		start := toFloat(startLevel)
		end := toFloat(endLevel)
		stardust, candy, xlCandy := calculatePowerUpCost(gd, start, end)
		result := map[string]interface{}{
			"stardust": stardust,
			"candy":    candy,
			"xlCandy":  xlCandy,
		}
		// Block mode: {{#getPowerUpCost ...}}...{{/getPowerUpCost}}
		rendered := options.FnWith(result)
		if rendered != "" {
			return rendered
		}
		// Inline mode: {{getPowerUpCost ...}}
		return fmt.Sprintf("%d stardust, %d candy, %d XL candy", stardust, candy, xlCandy)
	})

	customMaps := loadCustomMaps(configDir)

	raymond.RegisterHelper("map", func(mapName, value interface{}, options *raymond.Options) interface{} {
		return lookupCustomMap(customMaps, toString(mapName), toString(value), "", getLang(options))
	})

	raymond.RegisterHelper("map2", func(mapName, value, value2 interface{}, options *raymond.Options) interface{} {
		return lookupCustomMap(customMaps, toString(mapName), toString(value), toString(value2), getLang(options))
	})
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

type customMapStore struct {
	mu   sync.RWMutex
	maps map[string]map[string]string
}

func loadCustomMaps(configDir string) *customMapStore {
	store := &customMapStore{maps: make(map[string]map[string]string)}
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
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(mapsDir, e.Name()))
		if err != nil {
			log.Warnf("dts: read custom map %s: %v", e.Name(), err)
			continue
		}

		// Support both formats:
		// 1. PoracleJS format: {"name": "arealist", "map": {"key": "value"}}
		// 2. Flat format: {"key": "value"}
		var wrapper struct {
			Name string            `json:"name"`
			Map  map[string]string `json:"map"`
		}
		if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Map != nil {
			store.maps[name] = wrapper.Map
			log.Debugf("dts: loaded custom map %s (%d entries, PoracleJS format)", name, len(wrapper.Map))
			continue
		}

		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			log.Warnf("dts: parse custom map %s: %v", e.Name(), err)
			continue
		}
		store.maps[name] = m
		log.Debugf("dts: loaded custom map %s (%d entries)", name, len(m))
	}
	return store
}

func lookupCustomMap(store *customMapStore, mapName, value, fallbackValue, lang string) string {
	if store == nil {
		return value
	}
	store.mu.RLock()
	defer store.mu.RUnlock()

	// Try language-specific map first
	langKey := mapName + "." + lang
	if m, ok := store.maps[langKey]; ok {
		if v, ok := m[value]; ok {
			return v
		}
		if fallbackValue != "" {
			if v, ok := m[fallbackValue]; ok {
				return v
			}
		}
	}

	// Try base map
	if m, ok := store.maps[mapName]; ok {
		if v, ok := m[value]; ok {
			return v
		}
		if fallbackValue != "" {
			if v, ok := m[fallbackValue]; ok {
				return v
			}
		}
	}

	return value
}
