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
	ID                  string      `json:"id" doc:"Destination id: Discord user/channel/webhook id or Telegram chat id."`
	Type                string      `json:"type" doc:"Destination type, e.g. discord:user, discord:channel, discord:thread, webhook, telegram:user, telegram:channel, telegram:group."`
	Name                string      `json:"name" doc:"Display name (Discord username, channel name, webhook name, ...)."`
	Enabled             int         `json:"enabled" doc:"Legacy int flag: 1 = alerts enabled, 0 = stopped. (Kept numeric for legacy clients; v2 mutations use the enable/disable endpoints.)"`
	Area                string      `json:"area" doc:"Selected geofence areas as a JSON-encoded string array, e.g. \"[\\\"downtown\\\"]\" — clients must parse the string. \"[]\" when none."`
	Latitude            float64     `json:"latitude" doc:"Default location latitude. 0 when no location is set."`
	Longitude           float64     `json:"longitude" doc:"Default location longitude. 0 when no location is set."`
	Fails               int         `json:"fails" doc:"Consecutive delivery-failure count; the user is auto-disabled past the configured threshold."`
	LastChecked         null.Time   `json:"last_checked" doc:"When reconciliation last verified this destination. Null if never checked."`
	Language            null.String `json:"language" doc:"User locale code (e.g. \"de\"). Null when unset (falls back to the configured default locale)."`
	AdminDisable        int         `json:"admin_disable" doc:"Legacy int flag: 1 = administratively disabled (user cannot re-enable themselves), 0 = not."`
	DisabledDate        null.Time   `json:"disabled_date" doc:"When the destination was disabled. Null while enabled."`
	CurrentProfileNo    int         `json:"current_profile_no" doc:"Active profile number (matches profiles[].profile_no). 1 is the default profile."`
	CommunityMembership string      `json:"community_membership" doc:"Area-security community names as a JSON-encoded string array. \"[]\" when area security is off or the user has no communities."`
	AreaRestriction     null.String `json:"area_restriction" doc:"Area-security allowed-area names as a JSON-encoded string array. Null when unrestricted."`
	Notes               string      `json:"notes" doc:"Free-form operator notes."`
	BlockedAlerts       null.String `json:"blocked_alerts" doc:"Alert types this destination has blocked, as a JSON-encoded string array (e.g. \"[\\\"raid\\\"]\"). Null when none."`
}

// humanToResponse converts an internal *store.Human into the legacy API
// response shape. Returns nil if h is nil.
func humanToResponse(h *store.Human) *HumanResponse {
	if h == nil {
		return nil
	}
	resp := &HumanResponse{
		ID:                  h.ID,
		Type:                h.Type,
		Name:                h.Name,
		Enabled:             boolToAPIInt(h.Enabled),
		Area:                stringSliceToJSON(h.Area),
		Latitude:            h.Latitude,
		Longitude:           h.Longitude,
		Fails:               h.Fails,
		LastChecked:         h.LastChecked,
		AdminDisable:        boolToAPIInt(h.AdminDisable),
		DisabledDate:        h.DisabledDate,
		CurrentProfileNo:    h.CurrentProfileNo,
		CommunityMembership: stringSliceToJSON(h.CommunityMembership),
		Notes:               h.Notes,
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
