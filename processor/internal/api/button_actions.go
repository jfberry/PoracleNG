package api

import (
	"github.com/pokemon/poracleng/processor/internal/buttons"
)

// ActionInfo is the wire shape returned by /api/dts/actions for a
// single registered button action. The config editor uses these to
// build a dropdown of available actions and per-action parameter
// hints when an operator adds a new button to a template.
type ActionInfo struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`           // accepted scope values; empty when scope isn't required
	RequiredScope bool     `json:"required_scope"`   // true when scope must be set (mute, unsubscribe)
	Params        []string `json:"params,omitempty"` // documented param keys handlers look up in def.Params
}

// describeAction returns the editor-facing description for an action
// name. Kept hand-rolled and explicit so the supported parameters for
// each action are easy to find when adding a new one. New actions
// SHOULD add an entry here AND a real Handler in
// internal/buttonactions; missing entries get a minimal stub.
func describeAction(name string) ActionInfo {
	switch name {
	case buttons.ActionMute:
		return ActionInfo{
			Name:          name,
			Scopes:        []string{buttons.ScopeGym, buttons.ScopePokemon, buttons.ScopeArea, buttons.ScopePokestop, buttons.ScopeStation, buttons.ScopeEverything, buttons.ScopeTracking},
			RequiredScope: true,
			Params:        []string{"duration_min"},
		}
	case buttons.ActionUnsubscribe:
		return ActionInfo{
			Name:          name,
			Scopes:        []string{buttons.ScopeTracking},
			RequiredScope: true,
		}
	case buttons.ActionRedeliver:
		return ActionInfo{Name: name}
	case buttons.ActionRender:
		return ActionInfo{
			Name:   name,
			Params: []string{"template_id"},
		}
	}
	return ActionInfo{Name: name}
}
