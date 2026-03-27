package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Move represents a pokemon move from the raw masterfile.
type Move struct {
	MoveID   int
	TypeID   int  // numeric type ID
	Fast     bool // true = quick move, false = charged move
	Power    int  // PvE power
	PvPPower int
}

// rawMove is the raw masterfile format for moves.
type rawMove struct {
	MoveID   int  `json:"moveId"`
	MoveName string `json:"moveName"`
	Type     int  `json:"type"`
	Fast     bool `json:"fast"`
	Power    int  `json:"power"`
	PvPPower int  `json:"pvpPower"`
}

// LoadMoves parses the raw masterfile moves.json file.
func LoadMoves(path string) (map[int]*Move, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw map[string]rawMove
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	moves := make(map[int]*Move, len(raw))
	for key, entry := range raw {
		id, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		moves[id] = &Move{
			MoveID:   id,
			TypeID:   entry.Type,
			Fast:     entry.Fast,
			Power:    entry.Power,
			PvPPower: entry.PvPPower,
		}
	}

	return moves, nil
}
