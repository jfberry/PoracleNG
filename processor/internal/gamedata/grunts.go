package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Grunt represents a Team Rocket grunt type parsed from the classic.json format.
type Grunt struct {
	ID         int
	Template   string                   // e.g. "CHARACTER_GRASS_GRUNT_MALE"
	Gender     int                      // 1=male, 2=female
	Boss       bool                     // true for leaders (Giovanni, executives)
	TypeID     int                      // pokemon type ID (12=Grass, 7=Bug, etc.) — 0 if untyped
	Active     bool                     // whether this grunt type is currently active
	Team       [3][]GruntEncounterEntry // team[0]=slot 1, [1]=slot 2, [2]=slot 3
	Rewards    []int                    // reward-eligible slot indices (e.g. [0,1,2])
	CategoryID int                      // derived: character_category_{id} translation key
}

// GruntEncounterEntry holds a pokemon in a grunt encounter slot.
type GruntEncounterEntry struct {
	ID     int `json:"id"`
	FormID int `json:"formId"`
}

// HasRewardSlot returns true if the given slot index is reward-eligible.
func (g *Grunt) HasRewardSlot(slot int) bool {
	return slices.Contains(g.Rewards, slot)
}

// CategoryKey returns the i18n translation key for the grunt category.
func (g *Grunt) CategoryKey() string {
	return fmt.Sprintf("character_category_%d", g.CategoryID)
}

// TypeKey returns the i18n translation key for the grunt's pokemon type.
// Returns "" if the grunt has no type (e.g. Giovanni).
func (g *Grunt) TypeKey() string {
	if g.TypeID == 0 {
		return ""
	}
	return fmt.Sprintf("poke_type_%d", g.TypeID)
}

// TypeNameFromTemplate extracts the grunt type name from the character template string.
// Used as a fallback when TypeID is 0 (Metal, Darkness, Mixed grunts).
// Returns lowercased name matching what the !invasion command stores in the DB.
//
// Pokemon GO uses "Metal" internally for what players know as the Steel type,
// so we normalise METAL → "steel" — this lets users track Metal grunts using
// !invasion steel (matching PoracleJS behaviour).
func TypeNameFromTemplate(template string) string {
	// Special cases that don't have a standard pokemon type ID
	switch {
	case strings.Contains(template, "METAL"):
		return "steel"
	case strings.Contains(template, "DARKNESS"):
		return "darkness"
	case strings.Contains(template, "GRUNTB"):
		return "gruntb"
	case template == "CHARACTER_GRUNT_MALE" || template == "CHARACTER_GRUNT_FEMALE":
		return "mixed"
	}
	// Typed grunts: CHARACTER_{TYPE}_GRUNT_{GENDER} → extract type
	// e.g. CHARACTER_ELECTRIC_GRUNT_FEMALE → electric
	t := strings.TrimPrefix(template, "CHARACTER_")
	if idx := strings.Index(t, "_GRUNT"); idx > 0 {
		return strings.ToLower(t[:idx])
	}
	return strings.ToLower(template)
}

// categoryFromTemplate derives the category ID from the character template string.
func categoryFromTemplate(template string) int {
	switch {
	case template == "CHARACTER_GIOVANNI":
		return 6
	case strings.HasPrefix(template, "CHARACTER_EXECUTIVE_ARLO"):
		return 3
	case strings.HasPrefix(template, "CHARACTER_EXECUTIVE_CLIFF"):
		return 4
	case strings.HasPrefix(template, "CHARACTER_EXECUTIVE_SIERRA"):
		return 5
	case strings.Contains(template, "GRUNT"):
		return 2
	case template == "CHARACTER_BLANCHE",
		template == "CHARACTER_CANDELA",
		template == "CHARACTER_SPARK":
		return 1
	default:
		return 0
	}
}

// LoadGrunts parses the classic.json invasions format.
func LoadGrunts(path string) (map[int]*Grunt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw map[string]rawClassicGrunt
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	grunts := make(map[int]*Grunt, len(raw))
	for key, g := range raw {
		id, err := strconv.Atoi(key)
		if err != nil {
			continue
		}

		grunt := &Grunt{
			ID:         id,
			Template:   g.Character.Template,
			Gender:     g.Character.Gender,
			Boss:       g.Character.Boss,
			TypeID:     g.Character.Type.ID,
			Active:     g.Active,
			Rewards:    g.Lineup.Rewards,
			CategoryID: categoryFromTemplate(g.Character.Template),
		}

		// Parse team slots (up to 3)
		for i, slot := range g.Lineup.Team {
			if i >= 3 {
				break
			}
			entries := make([]GruntEncounterEntry, len(slot))
			for j, enc := range slot {
				entries[j] = GruntEncounterEntry{
					ID:     enc.ID,
					FormID: enc.Form,
				}
			}
			grunt.Team[i] = entries
		}

		grunts[id] = grunt
	}

	return grunts, nil
}

type rawClassicGrunt struct {
	Active    bool `json:"active"`
	Character struct {
		Template string `json:"template"`
		Gender   int    `json:"gender"`
		Boss     bool   `json:"boss"`
		Type     struct {
			ID   int    `json:"id"`
			Type string `json:"type"`
		} `json:"type"`
	} `json:"character"`
	Lineup struct {
		Rewards []int            `json:"rewards"`
		Team    [][]rawEncounter `json:"team"`
	} `json:"lineup"`
}

type rawEncounter struct {
	ID       int    `json:"id"`
	Form     int    `json:"form"`
	Template string `json:"template"`
}
