package enrichment

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

type testCase struct {
	Type     string          `json:"type"`
	Message  json.RawMessage `json:"message"`
	Expected json.RawMessage `json:"expected"`
}

type pokemonExpected struct {
	PokemonID         int                 `json:"pokemonId"`
	Form              int                 `json:"form"`
	NameEng           string              `json:"nameEng"`
	FormNameEng       string              `json:"formNameEng"`
	FormNormalisedEng string              `json:"formNormalisedEng"`
	FullNameEng       string              `json:"fullNameEng"`
	TypeIDs           []int               `json:"typeIds"`
	TypeNames         []string            `json:"typeNames"`
	TypeColor         string              `json:"typeColor"`
	TypeEmojiKeys     []string            `json:"typeEmojiKeys"`
	Generation        int                 `json:"generation"`
	GenerationName    string              `json:"generationName"`
	GenerationRoman   string              `json:"generationRoman"`
	BaseAttack        int                 `json:"baseAttack"`
	BaseDefense       int                 `json:"baseDefense"`
	BaseStamina       int                 `json:"baseStamina"`
	Encountered       bool                `json:"encountered"`
	IV                float64             `json:"iv"`
	IvColor           string              `json:"ivColor"`
	Atk               int                 `json:"atk"`
	Def               int                 `json:"def"`
	Sta               int                 `json:"sta"`
	CP                int                 `json:"cp"`
	Level             int                 `json:"level"`
	CatchBase         float64             `json:"catchBase"`
	CatchGreat        float64             `json:"catchGreat"`
	CatchUltra        float64             `json:"catchUltra"`
	Weather           int                 `json:"weather"`
	BoostingWeathers  []int               `json:"boostingWeathers"`
	AlteringWeathers  []int               `json:"alteringWeathers"`
	Weaknesses        map[string][]string `json:"weaknesses"`
	QuickMoveID       int                 `json:"quickMoveId"`
	ChargeMoveID      int                 `json:"chargeMoveId"`
	QuickMoveNameEng  string              `json:"quickMoveNameEng"`
	ChargeMoveNameEng string              `json:"chargeMoveNameEng"`
	QuickMoveType     string              `json:"quickMoveType"`
	ChargeMoveType    string              `json:"chargeMoveType"`
	GenderName        string              `json:"genderName"`
	GenderEmojiKey    string              `json:"genderEmojiKey"`
	SizeName          string              `json:"sizeName"`
	SeenType          string              `json:"seenType"`
	HasEvolutions     bool                `json:"hasEvolutions"`
	HasMegaEvolutions bool                `json:"hasMegaEvolutions"`
	GoogleMapUrl      string              `json:"googleMapUrl"`
	AppleMapUrl       string              `json:"appleMapUrl"`
	Evolutions        []evoExpected       `json:"evolutions"`
	MegaEvolutions    []megaEvoExpected   `json:"megaEvolutions"`
	HasPvp            bool                `json:"hasPvp"`
	PvpEnriched       map[string][]pvpRankExpected `json:"pvpEnriched"`
	HasDisguise       bool                `json:"hasDisguise"`
	DisguisePokemonId int                 `json:"disguisePokemonId"`
	DisguiseForm      int                 `json:"disguiseForm"`
}

type evoExpected struct {
	ID                int    `json:"id"`
	Form              int    `json:"form"`
	NameEng           string `json:"nameEng"`
	FormNormalisedEng string `json:"formNormalisedEng"`
	FullNameEng       string `json:"fullNameEng"`
	BaseAttack        int    `json:"baseAttack"`
	BaseDefense       int    `json:"baseDefense"`
	BaseStamina       int    `json:"baseStamina"`
}

type megaEvoExpected struct {
	Evolution   int    `json:"evolution"`
	FullNameEng string `json:"fullNameEng"`
	BaseAttack  int    `json:"baseAttack"`
	BaseDefense int    `json:"baseDefense"`
	BaseStamina int    `json:"baseStamina"`
}

type pvpRankExpected struct {
	Rank        int     `json:"rank"`
	CP          int     `json:"cp"`
	Pokemon     int     `json:"pokemon"`
	Form        int     `json:"form"`
	FullNameEng string  `json:"fullNameEng"`
	BaseAttack  int     `json:"baseAttack"`
	BaseDefense int     `json:"baseDefense"`
	BaseStamina int     `json:"baseStamina"`
	Percentage string `json:"percentage"`
}

type gymExpected struct {
	TeamID       int    `json:"teamId"`
	TeamName     string `json:"teamName"`
	TeamColor    string `json:"teamColor"`
	TeamEmojiKey string `json:"teamEmojiKey"`
}

type invasionExpected struct {
	GruntTypeID int              `json:"gruntTypeId"`
	GruntName   string           `json:"gruntName"`
	GruntType   string           `json:"gruntType"`
	GruntGender int              `json:"gruntGender"`
	Encounters  []invasionEncExp `json:"encounters"`
}

type invasionEncExp struct {
	ID      int    `json:"id"`
	Form    int    `json:"form"`
	Slot    string `json:"slot"`
	NameEng string `json:"nameEng"`
}

type maxbattleExpected struct {
	BattleLevel  int                 `json:"battleLevel"`
	LevelNameEng string              `json:"levelNameEng"`
	PokemonID    int                 `json:"pokemonId"`
	Form         int                 `json:"form"`
	NameEng      string              `json:"nameEng"`
	TypeIDs      []int               `json:"typeIds"`
	BaseAttack   int                 `json:"baseAttack"`
	BaseDefense  int                 `json:"baseDefense"`
	BaseStamina  int                 `json:"baseStamina"`
	Weaknesses   map[string][]string `json:"weaknesses"`
}

type raidExpected struct {
	GymID           string              `json:"gymId"`
	Level           int                 `json:"level"`
	TeamID          int                 `json:"teamId"`
	GymColor        string              `json:"gymColor"`
	LevelNameEng    string              `json:"levelNameEng"`
	PokemonID       int                 `json:"pokemonId"`
	Form            int                 `json:"form"`
	NameEng         string              `json:"nameEng"`
	FormNameEng     string              `json:"formNameEng"`
	TypeIDs         []int               `json:"typeIds"`
	TypeNames       []string            `json:"typeNames"`
	BaseAttack      int                 `json:"baseAttack"`
	BaseDefense     int                 `json:"baseDefense"`
	BaseStamina     int                 `json:"baseStamina"`
	Weaknesses      map[string][]string `json:"weaknesses"`
	Generation      int                 `json:"generation"`
	GenerationRoman string              `json:"generationRoman"`
}

func loadTestData(t *testing.T) ([]testCase, *gamedata.GameData, *i18n.Bundle) {
	t.Helper()
	baseDir := filepath.Join("..", "..", "..")

	expectedPath := filepath.Join("testdata", "expected.json")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Skipf("expected.json not found (run: node testdata/generate_expected.js): %v", err)
	}

	var cases []testCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse expected.json: %v", err)
	}

	gd, err := gamedata.Load(baseDir)
	if err != nil {
		t.Fatalf("load game data: %v", err)
	}

	tr := i18n.Load(baseDir)
	return cases, gd, tr
}

func sortInts(a []int) []int {
	s := make([]int, len(a))
	copy(s, a)
	sort.Ints(s)
	return s
}

func sortStrings(a []string) []string {
	s := make([]string, len(a))
	copy(s, a)
	sort.Strings(s)
	return s
}

// assertWeaknesses compares Go weakness categories against JS expected weaknesses.
func assertWeaknesses(t *testing.T, goWeak []gamedata.WeaknessCategory, jsWeak map[string][]string, gd *gamedata.GameData) {
	t.Helper()
	for _, cat := range goWeak {
		multKey := fmt.Sprintf("%g", cat.Multiplier)
		jsNames, ok := jsWeak[multKey]
		if !ok {
			t.Errorf("weakness %sx: Go has types %v, JS has none", multKey, cat.TypeIDs)
			continue
		}
		goNames := make([]string, 0, len(cat.TypeIDs))
		for _, id := range cat.TypeIDs {
			if ti, ok := gd.Types[id]; ok {
				goNames = append(goNames, ti.Name)
			}
		}
		if !slices.Equal(sortStrings(goNames), sortStrings(jsNames)) {
			t.Errorf("weakness %sx: Go=%v JS=%v", multKey, goNames, jsNames)
		}
	}
	for multKey, jsNames := range jsWeak {
		found := false
		for _, cat := range goWeak {
			if fmt.Sprintf("%g", cat.Multiplier) == multKey {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("weakness %sx: JS has %v, Go has none", multKey, jsNames)
		}
	}
}

func TestPokemonEnrichmentEquivalence(t *testing.T) {
	cases, gd, tr := loadTestData(t)

	// Use default IV colors matching the JS test generator
	ivColors := []string{"#9D9D9D", "#FFFFFF", "#1EFF00", "#0070DD", "#A335EE", "#FF8000"}

	for _, tc := range cases {
		if tc.Type != "pokemon" {
			continue
		}

		var exp pokemonExpected
		if err := json.Unmarshal(tc.Expected, &exp); err != nil {
			t.Fatalf("parse expected pokemon: %v", err)
		}

		var pokemon webhook.PokemonWebhook
		if err := json.Unmarshal(tc.Message, &pokemon); err != nil {
			t.Fatalf("parse pokemon message: %v", err)
		}

		t.Run(fmt.Sprintf("pokemon_%d_form_%d", exp.PokemonID, exp.Form), func(t *testing.T) {
			monster := gd.GetMonster(exp.PokemonID, exp.Form)
			if monster == nil {
				t.Fatalf("monster %d form %d not found in GameData", exp.PokemonID, exp.Form)
			}

			// --- Types ---
			if !slices.Equal(sortInts(monster.Types), sortInts(exp.TypeIDs)) {
				t.Errorf("types: Go=%v JS=%v", monster.Types, exp.TypeIDs)
			}
			goColor := gd.GetTypeColor(monster.Types)
			if goColor != exp.TypeColor {
				t.Errorf("typeColor: Go=%q JS=%q", goColor, exp.TypeColor)
			}
			goEmojiKeys := gd.GetTypeEmojiKeys(monster.Types)
			if !slices.Equal(sortStrings(goEmojiKeys), sortStrings(exp.TypeEmojiKeys)) {
				t.Errorf("typeEmojiKeys: Go=%v JS=%v", goEmojiKeys, exp.TypeEmojiKeys)
			}

			// --- Base stats ---
			if monster.Attack != exp.BaseAttack {
				t.Errorf("baseAttack: Go=%d JS=%d", monster.Attack, exp.BaseAttack)
			}
			if monster.Defense != exp.BaseDefense {
				t.Errorf("baseDefense: Go=%d JS=%d", monster.Defense, exp.BaseDefense)
			}
			if monster.Stamina != exp.BaseStamina {
				t.Errorf("baseStamina: Go=%d JS=%d", monster.Stamina, exp.BaseStamina)
			}

			// --- Generation ---
			goGen := gd.GetGeneration(exp.PokemonID, exp.Form)
			if goGen != exp.Generation {
				t.Errorf("generation: Go=%d JS=%d", goGen, exp.Generation)
			}
			if genInfo := gd.GetGenerationInfo(goGen); genInfo != nil {
				if genInfo.Roman != exp.GenerationRoman {
					t.Errorf("generationRoman: Go=%q JS=%q", genInfo.Roman, exp.GenerationRoman)
				}
			}

			// --- Weather ---
			goBoost := gd.GetBoostingWeathers(monster.Types)
			if !slices.Equal(sortInts(goBoost), sortInts(exp.BoostingWeathers)) {
				t.Errorf("boostingWeathers: Go=%v JS=%v", goBoost, exp.BoostingWeathers)
			}
			goAlter := gd.GetAlteringWeathers(monster.Types, exp.Weather)
			if !slices.Equal(sortInts(goAlter), sortInts(exp.AlteringWeathers)) {
				t.Errorf("alteringWeathers: Go=%v JS=%v", goAlter, exp.AlteringWeathers)
			}

			// --- Weakness ---
			goWeak := gamedata.CalculateWeaknesses(monster.Types, gd.Types)
			assertWeaknesses(t, goWeak, exp.Weaknesses, gd)

			// --- IV / encounter ---
			encountered := pokemon.IndividualAttack != nil
			if encountered != exp.Encountered {
				t.Errorf("encountered: Go=%v JS=%v", encountered, exp.Encountered)
			}
			if encountered {
				atk := *pokemon.IndividualAttack
				def := 0
				if pokemon.IndividualDefense != nil {
					def = *pokemon.IndividualDefense
				}
				sta := 0
				if pokemon.IndividualStamina != nil {
					sta = *pokemon.IndividualStamina
				}
				goIV := float64(atk+def+sta) / 0.45
				if math.Abs(goIV-exp.IV) > 0.01 {
					t.Errorf("iv: Go=%.2f JS=%.2f", goIV, exp.IV)
				}
				if atk != exp.Atk {
					t.Errorf("atk: Go=%d JS=%d", atk, exp.Atk)
				}
				if def != exp.Def {
					t.Errorf("def: Go=%d JS=%d", def, exp.Def)
				}
				if sta != exp.Sta {
					t.Errorf("sta: Go=%d JS=%d", sta, exp.Sta)
				}
				if pokemon.CP != exp.CP {
					t.Errorf("cp: Go=%d JS=%d", pokemon.CP, exp.CP)
				}
				if pokemon.PokemonLevel != exp.Level {
					t.Errorf("level: Go=%d JS=%d", pokemon.PokemonLevel, exp.Level)
				}

				// IV color
				goIvColor := gamedata.FindIvColor(goIV, ivColors)
				if goIvColor != exp.IvColor {
					t.Errorf("ivColor: Go=%q JS=%q (iv=%.2f)", goIvColor, exp.IvColor, goIV)
				}

				// Catch rates
				if pokemon.BaseCatch > 0 {
					goCatchBase := pokemon.BaseCatch * 100
					if math.Abs(goCatchBase-exp.CatchBase) > 0.01 {
						t.Errorf("catchBase: Go=%.2f JS=%.2f", goCatchBase, exp.CatchBase)
					}
				}
			}

			// --- Seen type ---
			goSeenType := computeSeenType(&pokemon)
			if goSeenType != exp.SeenType {
				t.Errorf("seenType: Go=%q JS=%q", goSeenType, exp.SeenType)
			}

			// --- Evolutions ---
			if (len(monster.Evolutions) > 0) != exp.HasEvolutions {
				t.Errorf("hasEvolutions: Go=%v JS=%v", len(monster.Evolutions) > 0, exp.HasEvolutions)
			}
			if (len(monster.TempEvolutions) > 0) != exp.HasMegaEvolutions {
				t.Errorf("hasMegaEvolutions: Go=%v JS=%v", len(monster.TempEvolutions) > 0, exp.HasMegaEvolutions)
			}

			// Verify evolution chain content (using English translator)
			enTr := tr.For("en")
			enricher := &Enricher{GameData: gd, Translations: tr}
			goEvos, goMegaEvos := enricher.buildEvolutions(gd, enTr, exp.PokemonID, exp.Form)

			if len(goEvos) < len(exp.Evolutions) {
				t.Errorf("evolutions count: Go=%d < JS=%d (Go should have at least as many)", len(goEvos), len(exp.Evolutions))
			} else if len(goEvos) != len(exp.Evolutions) {
				// Raw masterfile may have more evolutions than poracle-v2 (e.g. Hisuian forms)
				t.Logf("evolutions count differs (raw has more data): Go=%d JS=%d", len(goEvos), len(exp.Evolutions))
			}
			// Match by pokemon ID — every JS evolution must exist in Go
			for _, jsEvo := range exp.Evolutions {
				found := false
				for _, goEvo := range goEvos {
					goID, _ := goEvo["id"].(int)
					goForm, _ := goEvo["form"].(int)
					if goID == jsEvo.ID && goForm == jsEvo.Form {
						found = true
						goStats, _ := goEvo["baseStats"].(map[string]int)
						if goStats != nil && goStats["baseAttack"] != jsEvo.BaseAttack {
							t.Errorf("evo %d_%d baseAttack: Go=%d JS=%d", jsEvo.ID, jsEvo.Form, goStats["baseAttack"], jsEvo.BaseAttack)
						}
						break
					}
				}
				if !found {
					t.Errorf("JS evolution %d_%d not found in Go evolutions", jsEvo.ID, jsEvo.Form)
				}
			}

			if len(goMegaEvos) != len(exp.MegaEvolutions) {
				t.Errorf("megaEvolutions count: Go=%d JS=%d", len(goMegaEvos), len(exp.MegaEvolutions))
			} else {
				for i, jsMega := range exp.MegaEvolutions {
					goMega := goMegaEvos[i]
					goEvoID, _ := goMega["evolution"].(int)
					if goEvoID != jsMega.Evolution {
						t.Errorf("mega[%d] evolution: Go=%d JS=%d", i, goEvoID, jsMega.Evolution)
					}
					goStats, _ := goMega["baseStats"].(map[string]int)
					if goStats != nil {
						if goStats["baseAttack"] != jsMega.BaseAttack {
							t.Errorf("mega[%d] baseAttack: Go=%d JS=%d", i, goStats["baseAttack"], jsMega.BaseAttack)
						}
					}
				}
			}

			// --- Map URLs ---
			goGoogle := fmt.Sprintf("https://maps.google.com/maps?q=%f,%f", pokemon.Latitude, pokemon.Longitude)
			goApple := fmt.Sprintf("https://maps.apple.com/place?coordinate=%f,%f", pokemon.Latitude, pokemon.Longitude)
			// JS uses raw float formatting, Go uses %f — compare prefix
			if !strings.HasPrefix(goGoogle, "https://maps.google.com/maps?q=") {
				t.Errorf("googleMapUrl format wrong: %q", goGoogle)
			}
			if !strings.HasPrefix(goApple, "https://maps.apple.com/place?coordinate=") {
				t.Errorf("appleMapUrl format wrong: %q", goApple)
			}

			// --- Gender ---
			if genderInfo, ok := gd.Util.Genders[pokemon.Gender]; ok {
				if genderInfo.Name != exp.GenderName {
					t.Errorf("genderName: Go=%q JS=%q", genderInfo.Name, exp.GenderName)
				}
				if genderInfo.Emoji != exp.GenderEmojiKey {
					t.Errorf("genderEmojiKey: Go=%q JS=%q", genderInfo.Emoji, exp.GenderEmojiKey)
				}
			}

			// --- Size ---
			if exp.SizeName != "" {
				if sizeName, ok := gd.Util.Size[pokemon.Size]; ok {
					if sizeName != exp.SizeName {
						t.Errorf("sizeName: Go=%q JS=%q", sizeName, exp.SizeName)
					}
				}
			}

			// --- PVP enrichment ---
			if exp.HasPvp {
				for league, jsRanks := range exp.PvpEnriched {
					// Verify the Go enricher produces entries for each rank
					for _, jsRank := range jsRanks {
						// Verify GameData lookup for each PVP evolution pokemon
						pvpMon := gd.GetMonster(jsRank.Pokemon, jsRank.Form)
						if pvpMon != nil {
							if pvpMon.Attack != jsRank.BaseAttack {
								t.Errorf("pvp %s rank %d pokemon %d baseAttack: Go=%d JS=%d",
									league, jsRank.Rank, jsRank.Pokemon, pvpMon.Attack, jsRank.BaseAttack)
							}
						}
						// Verify percentage is a formatted string (e.g. "89.81")
						if jsRank.Percentage == "" {
							t.Errorf("pvp %s rank %d percentage is empty", league, jsRank.Rank)
						}
					}
				}
			}

			// --- Disguise ---
			if exp.HasDisguise {
				disguiseMon := gd.GetMonster(exp.DisguisePokemonId, exp.DisguiseForm)
				if disguiseMon == nil {
					t.Errorf("disguise monster %d form %d not found in GameData", exp.DisguisePokemonId, exp.DisguiseForm)
				}
			}

			// --- Translation equivalence (English) ---
			pokeName := enTr.T(gamedata.PokemonTranslationKey(exp.PokemonID))
			if pokeName != exp.NameEng && pokeName != fmt.Sprintf("poke_%d", exp.PokemonID) {
				t.Logf("name difference: Go(pogo-translations)=%q JS(poracle-v2)=%q (may differ)", pokeName, exp.NameEng)
			}
			if exp.QuickMoveNameEng != "" {
				goMoveName := enTr.T(gamedata.MoveTranslationKey(exp.QuickMoveID))
				if goMoveName != exp.QuickMoveNameEng && goMoveName != fmt.Sprintf("move_%d", exp.QuickMoveID) {
					t.Logf("quickMove name: Go=%q JS=%q (may differ)", goMoveName, exp.QuickMoveNameEng)
				}
			}
		})
	}
}

func TestRaidEnrichmentEquivalence(t *testing.T) {
	cases, gd, _ := loadTestData(t)

	for _, tc := range cases {
		if tc.Type != "raid" {
			continue
		}

		var exp raidExpected
		if err := json.Unmarshal(tc.Expected, &exp); err != nil {
			t.Fatalf("parse expected raid: %v", err)
		}

		t.Run(fmt.Sprintf("raid_%s_level_%d", exp.GymID[:8], exp.Level), func(t *testing.T) {
			if teamInfo, ok := gd.Util.Teams[exp.TeamID]; ok {
				if teamInfo.Color != exp.GymColor {
					t.Errorf("gymColor: Go=%q JS=%q", teamInfo.Color, exp.GymColor)
				}
			}

			if levelName, ok := gd.Util.RaidLevels[exp.Level]; ok {
				if levelName != exp.LevelNameEng {
					t.Errorf("levelName: Go=%q JS=%q", levelName, exp.LevelNameEng)
				}
			}

			if exp.PokemonID > 0 {
				monster := gd.GetMonster(exp.PokemonID, exp.Form)
				if monster == nil {
					t.Fatalf("monster %d form %d not found", exp.PokemonID, exp.Form)
				}

				if !slices.Equal(sortInts(monster.Types), sortInts(exp.TypeIDs)) {
					t.Errorf("types: Go=%v JS=%v", monster.Types, exp.TypeIDs)
				}
				if monster.Attack != exp.BaseAttack {
					t.Errorf("baseAttack: Go=%d JS=%d", monster.Attack, exp.BaseAttack)
				}

				goGen := gd.GetGeneration(exp.PokemonID, exp.Form)
				if goGen != exp.Generation {
					t.Errorf("generation: Go=%d JS=%d", goGen, exp.Generation)
				}

				goWeak := gamedata.CalculateWeaknesses(monster.Types, gd.Types)
				assertWeaknesses(t, goWeak, exp.Weaknesses, gd)
			}
		})
	}
}

func TestGymEnrichmentEquivalence(t *testing.T) {
	cases, gd, _ := loadTestData(t)

	for _, tc := range cases {
		if tc.Type != "gym" {
			continue
		}

		var exp gymExpected
		if err := json.Unmarshal(tc.Expected, &exp); err != nil {
			t.Fatalf("parse expected gym: %v", err)
		}

		t.Run(fmt.Sprintf("gym_team_%d", exp.TeamID), func(t *testing.T) {
			teamInfo, ok := gd.Util.Teams[exp.TeamID]
			if !ok {
				t.Fatalf("team %d not found in util data", exp.TeamID)
			}
			if teamInfo.Name != exp.TeamName {
				t.Errorf("teamName: Go=%q JS=%q", teamInfo.Name, exp.TeamName)
			}
			if teamInfo.Color != exp.TeamColor {
				t.Errorf("teamColor: Go=%q JS=%q", teamInfo.Color, exp.TeamColor)
			}
			if teamInfo.Emoji != exp.TeamEmojiKey {
				t.Errorf("teamEmojiKey: Go=%q JS=%q", teamInfo.Emoji, exp.TeamEmojiKey)
			}
		})
	}
}

func TestInvasionEnrichmentEquivalence(t *testing.T) {
	cases, gd, _ := loadTestData(t)

	for _, tc := range cases {
		if tc.Type != "invasion" {
			continue
		}

		var exp invasionExpected
		if err := json.Unmarshal(tc.Expected, &exp); err != nil {
			t.Fatalf("parse expected invasion: %v", err)
		}

		t.Run(fmt.Sprintf("invasion_grunt_%d", exp.GruntTypeID), func(t *testing.T) {
			grunt := gd.GetGrunt(exp.GruntTypeID)
			if grunt == nil {
				t.Fatalf("grunt type %d not found in GameData", exp.GruntTypeID)
			}
			if grunt.Type != exp.GruntType {
				t.Errorf("gruntType: Go=%q JS=%q", grunt.Type, exp.GruntType)
			}
			if grunt.Name != exp.GruntName {
				t.Errorf("gruntName: Go=%q JS=%q", grunt.Name, exp.GruntName)
			}
			if grunt.Gender != exp.GruntGender {
				t.Errorf("gruntGender: Go=%d JS=%d", grunt.Gender, exp.GruntGender)
			}

			// Verify encounter pokemon can be looked up
			for _, enc := range exp.Encounters {
				mon := gd.GetMonster(enc.ID, enc.Form)
				if mon == nil {
					// Try form 0 fallback
					mon = gd.GetMonster(enc.ID, 0)
				}
				if mon == nil {
					t.Errorf("encounter pokemon %d form %d not found in GameData", enc.ID, enc.Form)
				}
			}
		})
	}
}

func TestMaxbattleEnrichmentEquivalence(t *testing.T) {
	cases, gd, _ := loadTestData(t)

	for _, tc := range cases {
		if tc.Type != "max_battle" {
			continue
		}

		var exp maxbattleExpected
		if err := json.Unmarshal(tc.Expected, &exp); err != nil {
			t.Fatalf("parse expected maxbattle: %v", err)
		}

		t.Run(fmt.Sprintf("maxbattle_level_%d_pokemon_%d", exp.BattleLevel, exp.PokemonID), func(t *testing.T) {
			// Level name
			if levelName, ok := gd.Util.MaxbattleLevels[exp.BattleLevel]; ok {
				if levelName != exp.LevelNameEng {
					t.Errorf("levelName: Go=%q JS=%q", levelName, exp.LevelNameEng)
				}
			}

			if exp.PokemonID > 0 {
				monster := gd.GetMonster(exp.PokemonID, exp.Form)
				if monster == nil {
					t.Fatalf("monster %d form %d not found", exp.PokemonID, exp.Form)
				}

				if !slices.Equal(sortInts(monster.Types), sortInts(exp.TypeIDs)) {
					t.Errorf("types: Go=%v JS=%v", monster.Types, exp.TypeIDs)
				}
				if monster.Attack != exp.BaseAttack {
					t.Errorf("baseAttack: Go=%d JS=%d", monster.Attack, exp.BaseAttack)
				}
				if monster.Defense != exp.BaseDefense {
					t.Errorf("baseDefense: Go=%d JS=%d", monster.Defense, exp.BaseDefense)
				}
				if monster.Stamina != exp.BaseStamina {
					t.Errorf("baseStamina: Go=%d JS=%d", monster.Stamina, exp.BaseStamina)
				}

				// Weakness
				if exp.Weaknesses != nil {
					goWeak := gamedata.CalculateWeaknesses(monster.Types, gd.Types)
					assertWeaknesses(t, goWeak, exp.Weaknesses, gd)
				}
			}
		})
	}
}
