package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// HandleMasterdataMonsters returns a handler for GET /api/masterdata/monsters.
// It builds the poracle-v2 format that PoracleWeb expects from the processor's
// raw masterfile data and translations.
func HandleMasterdataMonsters(gd *gamedata.GameData, translations *i18n.Bundle) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		locale := r.URL.Query().Get("locale")
		if locale == "" {
			locale = "en"
		}
		tr := translations.For(locale)

		// Collect pokemon names (from form-0 entries).
		nameMap := make(map[int]string)
		for key := range gd.Monsters {
			if _, ok := nameMap[key.ID]; !ok {
				nameMap[key.ID] = tr.T(fmt.Sprintf("poke_%d", key.ID))
			}
		}

		// Build the result keyed by "pokemonID_formID" matching poracle-v2 format.
		result := make(map[string]*poracle2Monster, len(gd.Monsters))
		for key, mon := range gd.Monsters {
			types := make([]poracle2TypeEntry, len(mon.Types))
			for i, tid := range mon.Types {
				types[i] = poracle2TypeEntry{
					ID:   tid,
					Name: tr.T(fmt.Sprintf("poke_type_%d", tid)),
				}
			}

			formName := ""
			if key.Form != 0 {
				formName = tr.T(fmt.Sprintf("form_%d", key.Form))
				// If translation returns the key itself, fall back to empty.
				if formName == fmt.Sprintf("form_%d", key.Form) {
					formName = ""
				}
			}

			evolutions := make([]poracle2Evo, len(mon.Evolutions))
			for i, evo := range mon.Evolutions {
				evolutions[i] = poracle2Evo{
					EvoID:     evo.PokemonID,
					ID:        evo.FormID,
					CandyCost: evo.CandyCost,
				}
			}

			mapKey := strconv.Itoa(key.ID) + "_" + strconv.Itoa(key.Form)
			result[mapKey] = &poracle2Monster{
				Name:  nameMap[key.ID],
				ID:    key.ID,
				Types: types,
				Form: poracle2FormEntry{
					Name: formName,
					ID:   key.Form,
				},
				Stats: poracle2Stats{
					BaseAttack:  mon.Attack,
					BaseDefense: mon.Defense,
					BaseStamina: mon.Stamina,
				},
				Evolutions: evolutions,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

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

// HandleMasterdataGrunts returns a handler for GET /api/masterdata/grunts.
// It builds the poracle-v2 format that PoracleWeb expects from the processor's
// classic.json grunt data.
func HandleMasterdataGrunts(gd *gamedata.GameData) http.HandlerFunc {
	// Build the response once since game data is loaded at startup.
	result := buildGruntsResponse(gd)
	body, _ := json.Marshal(result)

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}

// buildGruntsResponse converts processor Grunt data to the poracle-v2 grunts.json format.
func buildGruntsResponse(gd *gamedata.GameData) map[string]*poracle2Grunt {
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
