package api

import (
	"sort"
	"strconv"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// poracle2Monster matches the poracle-v2 monsters.json format that PoracleWeb expects.
type poracle2Monster struct {
	Name       string              `json:"name"`
	ID         int                 `json:"id"`
	Types      []poracle2TypeEntry `json:"types"`
	Form       poracle2FormEntry   `json:"form"`
	Stats      poracle2Stats       `json:"stats"`
	Evolutions []poracle2Evo       `json:"evolutions"`
}

type poracle2TypeEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type poracle2FormEntry struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

type poracle2Stats struct {
	BaseAttack  int `json:"baseAttack"`
	BaseDefense int `json:"baseDefense"`
	BaseStamina int `json:"baseStamina"`
}

type poracle2Evo struct {
	EvoID     int `json:"evoId"`
	ID        int `json:"id"`
	CandyCost int `json:"candyCost"`
}

// buildGruntsResponse converts processor Grunt data to the poracle-v2 grunts.json format.
func buildGruntsResponse(gd *gamedata.GameData) map[string]*poracle2Grunt {
	if gd == nil {
		return make(map[string]*poracle2Grunt)
	}
	result := make(map[string]*poracle2Grunt, len(gd.Grunts))

	// Sort IDs for deterministic output.
	ids := make([]int, 0, len(gd.Grunts))
	for id := range gd.Grunts {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	for _, id := range ids {
		g := gd.Grunts[id]

		// Derive the "type" string from the template. This matches the alerter's
		// grunts.json format where "type" is an English name like "Bug", "Mixed", etc.
		typeName := gamedata.TypeNameFromTemplate(g.Template)
		// Capitalize the first letter for display.
		if len(typeName) > 0 {
			typeName = strings.ToUpper(typeName[:1]) + typeName[1:]
		}

		// Derive the "grunt" category name from the template.
		gruntName := gruntCategoryName(g)

		// Build encounter lists in the poracle-v2 format.
		encounters := poracle2Encounters{
			First:  gruntSlotToPoracle(g.Team[0]),
			Second: gruntSlotToPoracle(g.Team[1]),
			Third:  gruntSlotToPoracle(g.Team[2]),
		}

		result[strconv.Itoa(id)] = &poracle2Grunt{
			Type:         typeName,
			Gender:       g.Gender,
			Grunt:        gruntName,
			FirstReward:  g.HasRewardSlot(0),
			SecondReward: g.HasRewardSlot(1),
			ThirdReward:  g.HasRewardSlot(2),
			Encounters:   encounters,
		}
	}

	return result
}

type poracle2Grunt struct {
	Type         string             `json:"type"`
	Gender       int                `json:"gender"`
	Grunt        string             `json:"grunt"`
	FirstReward  bool               `json:"firstReward"`
	SecondReward bool               `json:"secondReward"`
	ThirdReward  bool               `json:"thirdReward"`
	Encounters   poracle2Encounters `json:"encounters"`
}

type poracle2Encounters struct {
	First  []poracle2GruntPokemon `json:"first"`
	Second []poracle2GruntPokemon `json:"second"`
	Third  []poracle2GruntPokemon `json:"third"`
}

type poracle2GruntPokemon struct {
	ID   int `json:"id"`
	Form int `json:"form"`
}

func gruntSlotToPoracle(entries []gamedata.GruntEncounterEntry) []poracle2GruntPokemon {
	if len(entries) == 0 {
		return []poracle2GruntPokemon{}
	}
	result := make([]poracle2GruntPokemon, len(entries))
	for i, e := range entries {
		result[i] = poracle2GruntPokemon{ID: e.ID, Form: e.FormID}
	}
	return result
}

// gruntCategoryName derives the grunt category display name from the grunt data.
// This produces the "grunt" field in the poracle-v2 format (e.g. "Grunt", "Blanche", "Giovanni").
func gruntCategoryName(g *gamedata.Grunt) string {
	switch g.CategoryID {
	case 1: // Training leaders — extract name from template (CHARACTER_BLANCHE → Blanche)
		name := strings.TrimPrefix(g.Template, "CHARACTER_")
		if len(name) > 0 {
			return strings.ToUpper(name[:1]) + strings.ToLower(name[1:])
		}
		return name
	case 2:
		return "Grunt"
	case 3:
		return "Arlo"
	case 4:
		return "Cliff"
	case 5:
		return "Sierra"
	case 6:
		return "Giovanni"
	default:
		return "Unset"
	}
}
