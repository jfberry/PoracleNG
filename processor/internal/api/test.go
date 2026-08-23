package api

import (
	"encoding/json"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// TestRequest is the payload for POST /api/test.
type TestRequest struct {
	Type    string          `json:"type"`    // webhook type: pokemon, raid, invasion, etc.
	Webhook json.RawMessage `json:"webhook"` // the raw webhook message
	Target  testTargetJSON  `json:"target"`  // who to send the result to
}

// testTargetJSON is the JSON-tagged version of bot.TestTarget for HTTP API use.
type testTargetJSON struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"` // discord:user, telegram:user, etc.
	Language  string  `json:"language"`
	Template  string  `json:"template"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func (t testTargetJSON) toBotTarget() bot.TestTarget {
	return bot.TestTarget{
		ID:        t.ID,
		Name:      t.Name,
		Type:      t.Type,
		Language:  t.Language,
		Template:  t.Template,
		Latitude:  t.Latitude,
		Longitude: t.Longitude,
	}
}
