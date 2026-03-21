package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Item represents a game item from the raw masterfile.
type Item struct {
	ItemID int
}

// LoadItems parses the raw masterfile items.json.
func LoadItems(path string) (map[int]*Item, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw map[string]struct {
		ItemID int `json:"itemId"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	items := make(map[int]*Item, len(raw))
	for key, entry := range raw {
		id, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		items[id] = &Item{ItemID: entry.ItemID}
	}

	return items, nil
}
