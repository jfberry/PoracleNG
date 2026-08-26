package api

import (
	"sort"
	"strconv"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
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

// buildGruntsResponse converts processor Grunt data to the poracle-v2 grunts.json
// format, with names in the requested locale.
//
// tr may be nil, in which case the English masterfile-derived names are used —
// the shape this endpoint served before it learned about locales.
func buildGruntsResponse(gd *gamedata.GameData, tr *i18n.Translator) map[string]*poracle2Grunt {
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

		// gruntType is the canonical stored string an invasion tracking rule
		// holds, and the one field every v2 invasion read emits (#209). Without
		// it a client cannot map a rule back to a display name.
		gruntType := gamedata.TypeNameFromTemplate(g.Template)

		// "type" is the distinguishing half of the display name: the localised
		// pokemon type for typed grunts, else the template-derived name. That
		// fallback is what keeps Blanche, Candela and Spark apart — they all
		// share category 1 ("Team Leader"), so the category alone cannot.
		// Mirrors enrichment's gruntTypeName chain in invasion.go.
		typeName := gruntDisplayType(g, gruntType, tr)

		// "grunt" is the category half: Grunt, Giovanni, Team Leader, ...
		gruntName := gruntCategoryName(g)
		if tr != nil {
			if localised := tr.T(g.CategoryKey()); localised != g.CategoryKey() && localised != "" {
				gruntName = localised
			}
		}

		// Build encounter lists in the poracle-v2 format.
		encounters := poracle2Encounters{
			First:  gruntSlotToPoracle(g.Team[0]),
			Second: gruntSlotToPoracle(g.Team[1]),
			Third:  gruntSlotToPoracle(g.Team[2]),
		}

		result[strconv.Itoa(id)] = &poracle2Grunt{
			GruntType:    gruntType,
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
	// GruntType is the canonical stored grunt_type string (lowercased,
	// template-derived) — the value an invasion tracking rule holds and the
	// one targeting field a v2 invasion read emits.
	GruntType    string             `json:"grunt_type"`
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

// gruntDisplayType resolves the distinguishing half of a grunt's display name:
// the localised pokemon type when the grunt has one, else the template-derived
// name title-cased. Mirrors the gruntTypeName chain in enrichment/invasion.go.
func gruntDisplayType(g *gamedata.Grunt, gruntType string, tr *i18n.Translator) string {
	if tr != nil {
		if key := g.TypeKey(); key != "" {
			if localised := tr.T(key); localised != key && localised != "" {
				return localised
			}
		}
	}
	if gruntType == "" {
		return ""
	}
	return strings.ToUpper(gruntType[:1]) + gruntType[1:]
}
