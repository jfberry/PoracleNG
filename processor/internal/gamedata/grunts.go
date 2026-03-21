package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Grunt represents a Team Rocket grunt type from the raw masterfile.
type Grunt struct {
	Type         string
	Gender       int
	Name         string
	Active       bool
	FirstReward  bool
	SecondReward bool
	ThirdReward  bool
	Encounters   []GruntEncounterEntry
}

// GruntEncounterEntry holds a pokemon in a grunt encounter slot.
type GruntEncounterEntry struct {
	ID       int    `json:"id"`
	FormID   int    `json:"formId"`
	Position string `json:"position"` // "first", "second", "third"
}

// EncountersByPosition returns encounters filtered by position.
func (g *Grunt) EncountersByPosition(position string) []GruntEncounterEntry {
	var result []GruntEncounterEntry
	for _, e := range g.Encounters {
		if e.Position == position {
			result = append(result, e)
		}
	}
	return result
}

// LoadGrunts parses the raw masterfile invasions.json.
func LoadGrunts(path string) (map[int]*Grunt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw map[string]rawGrunt
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	grunts := make(map[int]*Grunt, len(raw))
	for key, g := range raw {
		id, err := strconv.Atoi(key)
		if err != nil {
			continue
		}

		grunts[id] = &Grunt{
			Type:         g.Type,
			Gender:       g.Gender,
			Name:         g.Grunt,
			Active:       g.Active,
			FirstReward:  g.FirstReward,
			SecondReward: g.SecondReward,
			ThirdReward:  g.ThirdReward,
			Encounters:   g.Encounters,
		}
	}

	return grunts, nil
}

type rawGrunt struct {
	Type         string                `json:"type"`
	Gender       int                   `json:"gender"`
	Grunt        string                `json:"grunt"`
	Active       bool                  `json:"active"`
	FirstReward  bool                  `json:"firstReward"`
	SecondReward bool                  `json:"secondReward"`
	ThirdReward  bool                  `json:"thirdReward"`
	Encounters   []GruntEncounterEntry `json:"encounters"`
}
