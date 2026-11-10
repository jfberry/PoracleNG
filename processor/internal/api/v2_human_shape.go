package api

import (
	"github.com/guregu/null/v6"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2Human is the v2 representation of a human (user, channel, thread or
// webhook). Unlike the frozen v1 HumanResponse — which keeps PoracleWeb's
// legacy wire format of 0/1 flags and JSON-encoded string arrays — every
// field here has its natural type. Array columns are always arrays ([] when
// empty) except area_restriction, where null and [] mean different things.
type v2Human struct {
	ID                  string    `json:"id" doc:"Destination id: Discord user/channel/webhook id or Telegram chat id."`
	Type                string    `json:"type" doc:"Destination type, e.g. discord:user, discord:channel, discord:thread, webhook, telegram:user, telegram:channel, telegram:group."`
	Name                string    `json:"name" doc:"Display name (Discord username, channel name, webhook name, ...)."`
	Enabled             bool      `json:"enabled" doc:"Whether alerts are switched on (the !start/!stop flag)."`
	AdminDisable        bool      `json:"admin_disable" doc:"Whether an admin has disabled this destination; a disabled user cannot re-enable themselves."`
	Area                []string  `json:"area" doc:"Selected geofence area names. [] when none."`
	Latitude            float64   `json:"latitude" doc:"Default location latitude. 0 when no location is set."`
	Longitude           float64   `json:"longitude" doc:"Default location longitude. 0 when no location is set."`
	Language            string    `json:"language" doc:"Locale code (e.g. \"de\"). Empty when unset — the configured default locale applies."`
	CurrentProfileNo    int       `json:"current_profile_no" doc:"Active profile number. 1 is the default profile."`
	CommunityMembership []string  `json:"community_membership" doc:"Area-security community names. [] when area security is off or the destination has no communities."`
	AreaRestriction     []string  `json:"area_restriction" nullable:"true" doc:"Area-security allowed area names. null = unrestricted; [] = no areas allowed."`
	BlockedAlerts       []string  `json:"blocked_alerts" doc:"Alert types this destination has blocked (read-only; derived from command_security). [] when none."`
	Fails               int       `json:"fails" doc:"Consecutive delivery-failure count; the destination is auto-disabled past the configured threshold."`
	LastChecked         null.Time `json:"last_checked" doc:"When reconciliation last verified this destination. Null if never checked."`
	DisabledDate        null.Time `json:"disabled_date" doc:"When the destination was disabled. Null while enabled."`
	Notes               string    `json:"notes" doc:"Free-form operator notes."`
}

// humanToV2 converts a store human into the v2 shape. Returns nil for nil.
func humanToV2(h *store.Human) *v2Human {
	if h == nil {
		return nil
	}
	return &v2Human{
		ID:                  h.ID,
		Type:                h.Type,
		Name:                h.Name,
		Enabled:             h.Enabled,
		AdminDisable:        h.AdminDisable,
		Area:                nonNilStrings(h.Area),
		Latitude:            h.Latitude,
		Longitude:           h.Longitude,
		Language:            h.Language,
		CurrentProfileNo:    h.CurrentProfileNo,
		CommunityMembership: nonNilStrings(h.CommunityMembership),
		AreaRestriction:     h.AreaRestriction, // nil stays null: unrestricted
		BlockedAlerts:       nonNilStrings(h.BlockedAlerts),
		Fails:               h.Fails,
		LastChecked:         h.LastChecked,
		DisabledDate:        h.DisabledDate,
		Notes:               h.Notes,
	}
}

// nonNilStrings returns s, or an empty (non-nil) slice so it encodes as [].
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
