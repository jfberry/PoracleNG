package api

import (
	"encoding/json"

	"github.com/guregu/null/v6"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// HumanResponse is the JSON shape returned by /api/humans/* and tracking
// endpoints that include a "human" field. It intentionally mirrors the legacy
// db.HumanFull JSON layout so existing clients (PoracleWeb, curl scripts)
// continue to receive the same wire format after the internal migration to
// store.HumanStore.
//
// Key shape decisions:
//   - Enabled / AdminDisable are int (0/1), not bool — clients treat them numerically.
//   - Area / CommunityMembership are JSON-encoded strings, not arrays — clients parse them.
//   - AreaRestriction / BlockedAlerts / Language are null.String — omitted as null when unset.
//
// Build with humanToResponse().
type HumanResponse struct {
	ID                  string      `json:"id"`
	Type                string      `json:"type"`
	Name                string      `json:"name"`
	Enabled             int         `json:"enabled"`
	Area                string      `json:"area"`
	Latitude            float64     `json:"latitude"`
	Longitude           float64     `json:"longitude"`
	Fails               int         `json:"fails"`
	LastChecked         null.Time   `json:"last_checked"`
	Language            null.String `json:"language"`
	AdminDisable        int         `json:"admin_disable"`
	DisabledDate        null.Time   `json:"disabled_date"`
	CurrentProfileNo    int         `json:"current_profile_no"`
	CommunityMembership string      `json:"community_membership"`
	AreaRestriction     null.String `json:"area_restriction"`
	Notes               string      `json:"notes"`
	BlockedAlerts       null.String `json:"blocked_alerts"`
}

// humanToResponse converts an internal *store.Human into the legacy API
// response shape. Returns nil if h is nil.
func humanToResponse(h *store.Human) *HumanResponse {
	if h == nil {
		return nil
	}
	resp := &HumanResponse{
		ID:               h.ID,
		Type:             h.Type,
		Name:             h.Name,
		Enabled:          boolToAPIInt(h.Enabled),
		Area:             stringSliceToJSON(h.Area),
		Latitude:         h.Latitude,
		Longitude:        h.Longitude,
		Fails:            h.Fails,
		LastChecked:      h.LastChecked,
		AdminDisable:     boolToAPIInt(h.AdminDisable),
		DisabledDate:     h.DisabledDate,
		CurrentProfileNo: h.CurrentProfileNo,
		CommunityMembership: stringSliceToJSON(h.CommunityMembership),
		Notes:            h.Notes,
	}
	if h.Language != "" {
		resp.Language = null.StringFrom(h.Language)
	}
	if h.AreaRestriction != nil {
		resp.AreaRestriction = null.StringFrom(stringSliceToJSON(h.AreaRestriction))
	}
	if h.BlockedAlerts != nil {
		resp.BlockedAlerts = null.StringFrom(stringSliceToJSON(h.BlockedAlerts))
	}
	return resp
}

// boolToAPIInt maps a bool to the legacy int flag encoding (1 = true, 0 = false).
func boolToAPIInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// stringSliceToJSON marshals a []string to a JSON-array string. Returns "[]"
// for a nil/empty slice to match the legacy shape (clients expect a string,
// not null, for the Area / CommunityMembership fields).
func stringSliceToJSON(s []string) string {
	if len(s) == 0 {
		return "[]"
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "[]"
	}
	return string(b)
}
