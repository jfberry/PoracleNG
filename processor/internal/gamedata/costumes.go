package gamedata

import "fmt"

// CostumeInfo is one entry from resources/rawdata/costumes.json.
type CostumeInfo struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Proto    string `json:"proto"`
	NoEvolve bool   `json:"noEvolve"`
}

// CostumeTranslationKey returns "costume_{id}" for a costume ID.
func CostumeTranslationKey(id int) string {
	return fmt.Sprintf("costume_%d", id)
}
