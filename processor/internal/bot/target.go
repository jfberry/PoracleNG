package bot

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Target holds the resolved command target — who the command operates on.
type Target struct {
	ID          string
	Name        string
	Type        string // "discord:user", "discord:channel", "webhook", "telegram:user", "telegram:group"
	Language    string
	ProfileNo   int
	HasLocation bool
	HasArea     bool
	IsAdmin     bool
}

// BuildTarget resolves who a command operates on from the args.
// Admin users can override the target using name<webhookName> or user<userID>.
// Non-admin commands target the sender.
// Returns the target, remaining args (with consumed target args removed), and any error.
func BuildTarget(db *sqlx.DB, ctx *CommandContext, args []string) (*Target, []string, error) {
	remaining := make([]string, 0, len(args))
	var nameOverride, userOverride string

	// Extract name<X> / name:<X> and user<X> / user:<X> from args
	for _, arg := range args {
		if strings.HasPrefix(arg, "name:") && len(arg) > 5 {
			nameOverride = arg[5:]
			continue
		}
		if strings.HasPrefix(arg, "name") && len(arg) > 4 {
			nameOverride = arg[4:]
			continue
		}
		if strings.HasPrefix(arg, "user:") && len(arg) > 5 {
			userOverride = arg[5:]
			continue
		}
		if strings.HasPrefix(arg, "user") && len(arg) > 4 {
			userOverride = arg[4:]
			continue
		}
		remaining = append(remaining, arg)
	}

	// Default target: the sender themselves
	if nameOverride == "" && userOverride == "" {
		target, err := lookupHumanTarget(db, ctx.TargetID, ctx.TargetType)
		if err != nil {
			return nil, remaining, err
		}
		target.IsAdmin = ctx.IsAdmin
		return target, remaining, nil
	}

	// Admin override required
	if !ctx.IsAdmin && !ctx.Permissions.ChannelTracking {
		return nil, remaining, fmt.Errorf("only admins can target other users")
	}

	if userOverride != "" {
		target, err := lookupHumanByID(db, userOverride)
		if err != nil {
			return nil, remaining, fmt.Errorf("user %s not found", userOverride)
		}
		return target, remaining, nil
	}

	if nameOverride != "" {
		// Webhook override: look up by name
		target, err := lookupHumanByName(db, nameOverride)
		if err != nil {
			return nil, remaining, fmt.Errorf("webhook %s not found", nameOverride)
		}
		// Check webhook admin permission
		if !ctx.IsAdmin && !CanAdminWebhook(ctx.Config, ctx.UserID, nameOverride) {
			return nil, remaining, fmt.Errorf("no permission to manage webhook %s", nameOverride)
		}
		return target, remaining, nil
	}

	return nil, remaining, fmt.Errorf("no target resolved")
}

// lookupHumanTarget loads a human record by ID and type.
func lookupHumanTarget(db *sqlx.DB, id, typ string) (*Target, error) {
	var h struct {
		ID        string  `db:"id"`
		Name      string  `db:"name"`
		Type      string  `db:"type"`
		Language  *string `db:"language"`
		ProfileNo int     `db:"current_profile_no"`
		Latitude  float64 `db:"latitude"`
		Longitude float64 `db:"longitude"`
		Area      *string `db:"area"`
	}
	err := db.Get(&h, "SELECT id, name, type, language, current_profile_no, latitude, longitude, area FROM humans WHERE id = ? AND type = ?", id, typ)
	if err != nil {
		return nil, err
	}
	return humanToTarget(&h), nil
}

// lookupHumanByID loads a human record by ID (any type).
func lookupHumanByID(db *sqlx.DB, id string) (*Target, error) {
	var h struct {
		ID        string  `db:"id"`
		Name      string  `db:"name"`
		Type      string  `db:"type"`
		Language  *string `db:"language"`
		ProfileNo int     `db:"current_profile_no"`
		Latitude  float64 `db:"latitude"`
		Longitude float64 `db:"longitude"`
		Area      *string `db:"area"`
	}
	err := db.Get(&h, "SELECT id, name, type, language, current_profile_no, latitude, longitude, area FROM humans WHERE id = ? LIMIT 1", id)
	if err != nil {
		return nil, err
	}
	return humanToTarget(&h), nil
}

// lookupHumanByName loads a human record by name (for webhook lookup).
func lookupHumanByName(db *sqlx.DB, name string) (*Target, error) {
	var h struct {
		ID        string  `db:"id"`
		Name      string  `db:"name"`
		Type      string  `db:"type"`
		Language  *string `db:"language"`
		ProfileNo int     `db:"current_profile_no"`
		Latitude  float64 `db:"latitude"`
		Longitude float64 `db:"longitude"`
		Area      *string `db:"area"`
	}
	err := db.Get(&h, "SELECT id, name, type, language, current_profile_no, latitude, longitude, area FROM humans WHERE name = ? LIMIT 1", name)
	if err != nil {
		return nil, err
	}
	return humanToTarget(&h), nil
}

func humanToTarget(h *struct {
	ID        string  `db:"id"`
	Name      string  `db:"name"`
	Type      string  `db:"type"`
	Language  *string `db:"language"`
	ProfileNo int     `db:"current_profile_no"`
	Latitude  float64 `db:"latitude"`
	Longitude float64 `db:"longitude"`
	Area      *string `db:"area"`
}) *Target {
	lang := ""
	if h.Language != nil {
		lang = *h.Language
	}
	hasLocation := h.Latitude != 0 || h.Longitude != 0
	hasArea := false
	if h.Area != nil && *h.Area != "" && *h.Area != "[]" {
		hasArea = true
	}
	return &Target{
		ID:          h.ID,
		Name:        h.Name,
		Type:        h.Type,
		Language:    lang,
		ProfileNo:   h.ProfileNo,
		HasLocation: hasLocation,
		HasArea:     hasArea,
	}
}
