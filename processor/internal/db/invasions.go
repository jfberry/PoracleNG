package db

import (
	"encoding/json"
	"strings"

	"github.com/jmoiron/sqlx"
)

// InvasionTracking represents a row from the invasion table.
type InvasionTracking struct {
	UID                   int64    `db:"uid"`
	ID                    string   `db:"id"`
	ProfileNo             int      `db:"profile_no"`
	Ping                  string   `db:"ping"`
	Clean                 int      `db:"clean"`
	Distance              int      `db:"distance"`
	Template              string   `db:"template"`
	Gender                int      `db:"gender"`
	GruntType             string   `db:"grunt_type"`
	OverrideLocationLabel string   `db:"override_location_label"`
	OverrideAreasRaw      string   `db:"override_areas"`
	OverrideAreas         []string `db:"-"`
}

// LoadInvasions loads all invasion trackings from the database.
//
// In-memory normalisation: rows with grunt_type='metal' (legacy
// PoracleJS spelling, still emitted by some third-party CRUD tools)
// are translated to 'steel' so they match the webhook-side classifier
// in gamedata.TypeNameFromTemplate (which returns 'steel' for the
// METAL template). The DB row is left untouched — only the in-memory
// snapshot used by the matcher is rewritten. Drop this once the
// known offending third-party tools have caught up.
func LoadInvasions(db *sqlx.DB) ([]*InvasionTracking, error) {
	var invasions []InvasionTracking
	err := db.Select(&invasions,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, gender, grunt_type,
		        COALESCE(override_location_label, '') AS override_location_label,
		        COALESCE(override_areas, '') AS override_areas
		 FROM invasion`)
	if err != nil {
		return nil, err
	}

	result := make([]*InvasionTracking, len(invasions))
	for i := range invasions {
		invasions[i].GruntType = normaliseInvasionGruntType(invasions[i].GruntType)
		if invasions[i].OverrideAreasRaw != "" {
			_ = json.Unmarshal([]byte(invasions[i].OverrideAreasRaw), &invasions[i].OverrideAreas)
		}
		result[i] = &invasions[i]
	}
	return result, nil
}

// normaliseInvasionGruntType rewrites legacy/third-party grunt_type
// values to the canonical name the webhook-side classifier produces.
// Returns the input unchanged when nothing needs rewriting.
func normaliseInvasionGruntType(s string) string {
	if strings.EqualFold(s, "metal") {
		return "steel"
	}
	return s
}
