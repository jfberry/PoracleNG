package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// TypeInfo holds the defensive type matchup data from the raw masterfile.
type TypeInfo struct {
	TypeID      int
	Name        string // set from util.json or form name resolution
	Color       string // set from util.json types section
	Emoji       string // emoji key, set from util.json types section
	Weaknesses  []int  // type IDs super effective against this type (defensively weak to)
	Resistances []int  // type IDs this type resists (takes less damage from)
	Immunes     []int  // type IDs this type is immune to (double resist in PoGo)
}

// rawTypeInfo is the raw masterfile format for types.json.
type rawTypeInfo struct {
	TypeID      int   `json:"typeId"`
	TypeName    string `json:"typeName"`
	Weaknesses  []int `json:"weaknesses"`
	Resistances []int `json:"resistances"`
	Immunes     []int `json:"immunes"`
}

// LoadTypes parses the raw masterfile types.json file.
func LoadTypes(path string) (map[int]*TypeInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw map[string]rawTypeInfo
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	types := make(map[int]*TypeInfo, len(raw))
	for key, entry := range raw {
		id, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		if id == 0 {
			continue // skip "None" type
		}
		types[id] = &TypeInfo{
			TypeID:      id,
			Name:        entry.TypeName,
			Weaknesses:  entry.Weaknesses,
			Resistances: entry.Resistances,
			Immunes:     entry.Immunes,
		}
	}

	return types, nil
}
