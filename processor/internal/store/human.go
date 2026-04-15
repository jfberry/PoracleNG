// Package store provides database-backed storage interfaces for humans and
// tracking data, decoupling command logic from SQL.
package store

import "github.com/guregu/null/v6"

// Human represents a complete human record with all columns.
// JSON fields (Area, CommunityMembership, AreaRestriction, BlockedAlerts)
// are stored as JSON arrays in the database but exposed here as Go slices.
type Human struct {
	ID                  string
	Type                string
	Name                string
	Enabled             bool
	Area                []string
	Latitude            float64
	Longitude           float64
	Fails               int
	LastChecked         null.Time
	Language            string // empty string = not set (uses config default)
	AdminDisable        bool
	DisabledDate        null.Time
	CurrentProfileNo    int
	CommunityMembership []string
	AreaRestriction     []string // nil = no restriction
	Notes               string
	BlockedAlerts       []string // nil = no blocked alerts
}

// HumanLite is a minimal projection of the humans table used on hot paths
// (tracking CRUD handlers) where only identity, language, and profile
// selection matter. Skips the JSON-column parsing that Get does for Area,
// CommunityMembership, AreaRestriction, and BlockedAlerts.
type HumanLite struct {
	ID               string
	Type             string
	Name             string
	Enabled          bool
	AdminDisable     bool
	Language         string // empty == not set; callers fall back to the configured default locale
	CurrentProfileNo int
}

// LanguageOrDefault returns h.Language if set, otherwise defaultLang.
func (h *HumanLite) LanguageOrDefault(defaultLang string) string {
	if h.Language != "" {
		return h.Language
	}
	return defaultLang
}

// Profile represents a row from the profiles table.
type Profile struct {
	UID         int
	ID          string
	ProfileNo   int
	Name        string
	Area        []string
	Latitude    float64
	Longitude   float64
	ActiveHours string
}

// HumanStore provides typed CRUD operations over the humans and profiles
// tables. JSON marshaling is handled internally — callers work with Go types.
type HumanStore interface {
	// --- Human CRUD ---

	// Get returns a human by ID, or nil if not found.
	Get(id string) (*Human, error)

	// GetLite returns identity + profile fields for a human by ID without
	// parsing any JSON columns. Cheaper than Get for hot-path handlers that
	// only need ID / CurrentProfileNo / enable state.
	GetLite(id string) (*HumanLite, error)

	// Create inserts a new human record.
	Create(h *Human) error

	// Delete removes a human and all their tracking data and profiles.
	Delete(id string) error

	// --- Field updates ---

	// SetEnabled sets the enabled flag (and optionally resets fails).
	SetEnabled(id string, enabled bool) error

	// SetEnabledWithFails sets enabled=1 and fails=0 atomically.
	SetEnabledWithFails(id string) error

	// SetAdminDisable sets the admin_disable flag. When disabling, sets
	// disabled_date to now. When enabling, clears disabled_date.
	SetAdminDisable(id string, disable bool) error

	// SetLocation updates latitude and longitude on both humans and the
	// active profile.
	SetLocation(id string, profileNo int, lat, lon float64) error

	// SetArea updates the area JSON on both humans and the active profile.
	SetArea(id string, profileNo int, areas []string) error

	// SetLanguage updates the language field.
	SetLanguage(id string, lang string) error

	// SetCommunity updates community_membership and area_restriction.
	SetCommunity(id string, communities []string, restrictions []string) error

	// SetBlockedAlerts updates the blocked_alerts field.
	SetBlockedAlerts(id string, alerts []string) error

	// SetName updates the display name.
	SetName(id string, name string) error

	// Update performs a dynamic update of the given fields. The fields map
	// keys are column names, values are the new values. This is used by
	// reconciliation and poracle command which build dynamic SET clauses.
	Update(id string, fields map[string]any) error

	// --- Queries ---

	// ListByType returns all humans matching the given type string
	// (e.g. "discord:user", "telegram:group").
	ListByType(typ string) ([]*Human, error)

	// ListByTypeEnabled returns humans matching the type that are not
	// admin-disabled.
	ListByTypeEnabled(typ string) ([]*Human, error)

	// ListByTypes returns humans matching any of the given types that are
	// not admin-disabled.
	ListByTypes(types []string) ([]*Human, error)

	// ListAll returns all humans (for admin userlist).
	ListAll() ([]*Human, error)

	// LookupWebhookByName finds a webhook human by name.
	LookupWebhookByName(name string) (string, error)

	// CountByName returns the number of humans with the given name.
	CountByName(name string) (int, error)

	// --- Profile operations ---

	// GetProfiles returns all profiles for a human.
	GetProfiles(id string) ([]Profile, error)

	// SwitchProfile switches the human's active profile. Returns false if
	// the profile does not exist.
	SwitchProfile(id string, profileNo int) (bool, error)

	// AddProfile creates a new profile, auto-assigning the next profile_no.
	AddProfile(id string, name string, activeHours string) error

	// DeleteProfile removes a profile and its tracking data. If the
	// deleted profile was current, switches to the lowest remaining.
	DeleteProfile(id string, profileNo int) error

	// CopyProfile copies all tracking data from one profile to another.
	CopyProfile(id string, fromProfile, toProfile int) error

	// CreateDefaultProfile creates profile_no=1 for a new human.
	CreateDefaultProfile(id, name string, areas []string, lat, lon float64) error

	// UpdateProfileHours updates the active_hours field on a profile.
	UpdateProfileHours(id string, profileNo int, activeHours string) error
}
