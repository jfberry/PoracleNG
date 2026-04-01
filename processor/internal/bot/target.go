package bot

import (
	"fmt"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// Target holds the resolved command target — who the command operates on.
type Target struct {
	ID               string
	Name             string
	Type             string // "discord:user", "discord:channel", "webhook", "telegram:user", "telegram:group"
	Language         string
	ProfileNo        int
	HasLocation      bool
	HasArea          bool
	IsAdmin          bool
	ExecutionMessage string // shown to user when target is overridden (admin feature)
}

// BuildTarget resolves who a command operates on from the args.
// Admin users can override the target using name<webhookName> or user<userID>.
// Non-admin commands target the sender.
// Returns the target, remaining args (with consumed target args removed), and any error.
func BuildTarget(ctx *CommandContext, args []string) (*Target, []string, error) {
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

	hs := ctx.Humans

	// Default target: the sender themselves
	if nameOverride == "" && userOverride == "" {
		// In a channel (not DM) with admin/delegated permissions: target the channel
		if !ctx.IsDM && (ctx.IsAdmin || ctx.Permissions.ChannelTracking) {
			target, err := lookupTarget(hs, ctx.ChannelID)
			if err != nil || target == nil {
				// Channel not registered — fall back to self
				target, err = lookupTarget(hs, ctx.TargetID)
				if err != nil {
					return nil, remaining, err
				}
			}
			if target != nil {
				target.IsAdmin = ctx.IsAdmin
				return target, remaining, nil
			}
		}
		target, err := lookupTarget(hs, ctx.TargetID)
		if err != nil {
			return nil, remaining, err
		}
		if target == nil {
			return nil, remaining, fmt.Errorf("user %s not found", ctx.TargetID)
		}
		target.IsAdmin = ctx.IsAdmin
		return target, remaining, nil
	}

	// Admin override required
	if !ctx.IsAdmin && !ctx.Permissions.ChannelTracking {
		return nil, remaining, fmt.Errorf("only admins can target other users")
	}

	if userOverride != "" {
		target, err := lookupTarget(hs, userOverride)
		if err != nil || target == nil {
			return nil, remaining, fmt.Errorf("user %s not found or not registered", userOverride)
		}
		target.ExecutionMessage = fmt.Sprintf("This command is being executed as %s %s", target.ID, target.Name)
		return target, remaining, nil
	}

	if nameOverride != "" {
		// Webhook override: look up by name
		id, err := hs.LookupWebhookByName(nameOverride)
		if err != nil || id == "" {
			return nil, remaining, fmt.Errorf("webhook %s not found", nameOverride)
		}
		target, err := lookupTarget(hs, id)
		if err != nil || target == nil {
			return nil, remaining, fmt.Errorf("webhook %s not found", nameOverride)
		}
		target.ExecutionMessage = fmt.Sprintf("This command is being executed as %s %s", target.Type, target.Name)
		// Check webhook admin permission
		if !ctx.IsAdmin && !CanAdminWebhook(ctx.Config, ctx.UserID, nameOverride) {
			return nil, remaining, fmt.Errorf("no permission to manage webhook %s", nameOverride)
		}
		return target, remaining, nil
	}

	return nil, remaining, fmt.Errorf("no target resolved")
}

// lookupTarget loads a human record by ID and converts to Target.
func lookupTarget(hs store.HumanStore, id string) (*Target, error) {
	h, err := hs.Get(id)
	if err != nil {
		return nil, err
	}
	if h == nil {
		return nil, nil
	}
	return humanToTarget(h), nil
}

func humanToTarget(h *store.Human) *Target {
	hasLocation := h.Latitude != 0 || h.Longitude != 0
	hasArea := len(h.Area) > 0
	return &Target{
		ID:          h.ID,
		Name:        h.Name,
		Type:        h.Type,
		Language:    h.Language,
		ProfileNo:   h.CurrentProfileNo,
		HasLocation: hasLocation,
		HasArea:     hasArea,
	}
}
