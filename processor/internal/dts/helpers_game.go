package dts

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
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

		cpm := getCPMultiplier(level)
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

func registerMiscGameHelpers(_ *gamedata.GameData, bundle *i18n.Bundle, emoji *EmojiLookup, configDir string) {
	raymond.RegisterHelper("getEmoji", func(key interface{}, options *raymond.Options) interface{} {
		return emoji.Lookup(toString(key), getPlatform(options))
	})

	raymond.RegisterHelper("translateAlt", func(text interface{}, options *raymond.Options) interface{} {
		return bundle.For(getAltLang(options)).T(toString(text))
	})

	raymond.RegisterHelper("getPowerUpCost", func(startLevel, endLevel interface{}, options *raymond.Options) interface{} {
		start := toFloat(startLevel)
		end := toFloat(endLevel)
		stardust, candy, xlCandy := calculatePowerUpCost(start, end)
		result := map[string]interface{}{
			"stardust": stardust,
			"candy":    candy,
			"xlCandy":  xlCandy,
		}
		block := options.Fn()
		if block != "" {
			return options.FnWith(result)
		}
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
// CP Multiplier table
// ---------------------------------------------------------------------------

// cpMultipliers is indexed by (level - 1) * 2, covering levels 1 to 51 at 0.5 increments.
// Source: Pokemon GO game master / https://pokemongo.fandom.com/wiki/CP_multiplier
var cpMultipliers = [...]float64{
	0.0940000,  // 1
	0.1351374,  // 1.5
	0.1663979,  // 2
	0.1926509,  // 2.5
	0.2157325,  // 3
	0.2365727,  // 3.5
	0.2557201,  // 4
	0.2735304,  // 4.5
	0.2902499,  // 5
	0.3060574,  // 5.5
	0.3210876,  // 6
	0.3354450,  // 6.5
	0.3492127,  // 7
	0.3624578,  // 7.5
	0.3752356,  // 8
	0.3875924,  // 8.5
	0.3995673,  // 9
	0.4111936,  // 9.5
	0.4225000,  // 10
	0.4329264,  // 10.5
	0.4431076,  // 11
	0.4530600,  // 11.5
	0.4627984,  // 12
	0.4723361,  // 12.5
	0.4816850,  // 13
	0.4908558,  // 13.5
	0.4998584,  // 14
	0.5087018,  // 14.5
	0.5173940,  // 15
	0.5259425,  // 15.5
	0.5343543,  // 16
	0.5426357,  // 16.5
	0.5507927,  // 17
	0.5588306,  // 17.5
	0.5667545,  // 18
	0.5745691,  // 18.5
	0.5822789,  // 19
	0.5898879,  // 19.5
	0.5974000,  // 20
	0.6048237,  // 20.5
	0.6121573,  // 21
	0.6194041,  // 21.5
	0.6265671,  // 22
	0.6336491,  // 22.5
	0.6406530,  // 23
	0.6475810,  // 23.5
	0.6544356,  // 24
	0.6612193,  // 24.5
	0.6679340,  // 25
	0.6745819,  // 25.5
	0.6811649,  // 26
	0.6876849,  // 26.5
	0.6941437,  // 27
	0.7005429,  // 27.5
	0.7068842,  // 28
	0.7131691,  // 28.5
	0.7193991,  // 29
	0.7255756,  // 29.5
	0.7317000,  // 30
	0.7347410,  // 30.5
	0.7377695,  // 31
	0.7407856,  // 31.5
	0.7437894,  // 32
	0.7467812,  // 32.5
	0.7497610,  // 33
	0.7527291,  // 33.5
	0.7556855,  // 34
	0.7586304,  // 34.5
	0.7615638,  // 35
	0.7644861,  // 35.5
	0.7673972,  // 36
	0.7702973,  // 36.5
	0.7731865,  // 37
	0.7760650,  // 37.5
	0.7789328,  // 38
	0.7817901,  // 38.5
	0.7846370,  // 39
	0.7874736,  // 39.5
	0.7903000,  // 40
	0.7931164,  // 40.5
	0.7953000,  // 41
	0.7978000,  // 41.5
	0.8003000,  // 42
	0.8028000,  // 42.5
	0.8053000,  // 43
	0.8078000,  // 43.5
	0.8103000,  // 44
	0.8128000,  // 44.5
	0.8153000,  // 45
	0.8178000,  // 45.5
	0.8203000,  // 46
	0.8228000,  // 46.5
	0.8253000,  // 47
	0.8278000,  // 47.5
	0.8303000,  // 48
	0.8328000,  // 48.5
	0.8353000,  // 49
	0.8378000,  // 49.5
	0.8403000,  // 50
	0.8428000,  // 50.5
	0.8453000,  // 51
}

func getCPMultiplier(level float64) float64 {
	idx := int((level - 1) * 2)
	if idx < 0 || idx >= len(cpMultipliers) {
		return 0.7903 // default to level 40
	}
	return cpMultipliers[idx]
}

// ---------------------------------------------------------------------------
// Power-up cost table
// ---------------------------------------------------------------------------

type powerUpCostEntry struct {
	stardust int
	candy    int
	xlCandy  int
}

var powerUpCosts = buildPowerUpCosts()

func buildPowerUpCosts() map[float64]powerUpCostEntry {
	costs := make(map[float64]powerUpCostEntry)

	// Levels 1-10 (half levels included): stardust + candy (no XL)
	dustArr := []int{
		200, 200, 400, 400, 600, 600, 800, 800, 1000, 1000,
		1300, 1300, 1600, 1600, 1900, 1900, 2200, 2200, 2500, 2500,
		3000, 3000, 3500, 3500, 4000, 4000, 4500, 4500, 5000, 5000,
		6000, 6000, 7000, 7000, 8000, 8000, 9000, 9000, 10000, 10000,
		10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000,
		10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000,
		10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000,
		10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000,
	}
	candyArr := []int{
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
		3, 3, 3, 3, 3, 3, 4, 4, 4, 4,
		6, 6, 8, 8, 10, 10, 12, 12, 15, 15,
		15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
		15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
		15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
		15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
	}

	for i := 0; i < len(dustArr); i++ {
		level := 1.0 + float64(i)*0.5
		costs[level] = powerUpCostEntry{
			stardust: dustArr[i],
			candy:    candyArr[i],
		}
	}

	// XL candy costs for levels 41-50.5
	xlDust := []int{
		10000, 10000, 10000, 10000, 11000, 11000, 12000, 12000, 12500, 12500,
		13000, 13000, 14000, 14000, 15000, 15000, 16000, 16000, 17000, 17000,
	}
	xlCandyArr := []int{
		10, 10, 10, 10, 12, 12, 12, 12, 15, 15,
		15, 15, 17, 17, 17, 17, 20, 20, 20, 20,
	}
	for i := 0; i < len(xlDust); i++ {
		level := 41.0 + float64(i)*0.5
		costs[level] = powerUpCostEntry{
			stardust: xlDust[i],
			candy:    0,
			xlCandy:  xlCandyArr[i],
		}
	}

	return costs
}

func calculatePowerUpCost(startLevel, endLevel float64) (stardust, candy, xlCandy int) {
	level := startLevel
	for level < endLevel {
		if entry, ok := powerUpCosts[level]; ok {
			stardust += entry.stardust
			candy += entry.candy
			xlCandy += entry.xlCandy
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
	entries, err := os.ReadDir(mapsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("dts: read customMaps dir: %v", err)
		}
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
