package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/buttonactions"
	"github.com/pokemon/poracleng/processor/internal/buttons"
)

// ActionInfo is the wire shape returned by /api/dts/actions for a
// single registered button action. The config editor uses these to
// build a dropdown of available actions and per-action parameter
// hints when an operator adds a new button to a template.
type ActionInfo struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`               // accepted scope values; empty when scope isn't required
	RequiredScope bool `json:"required_scope"`       // true when scope must be set (mute, unsubscribe)
	Params    []string `json:"params,omitempty"`     // documented param keys handlers look up in def.Params
}

// HandleButtonActionsList returns the list of currently-registered
// button actions plus the metadata the editor needs to render their
// configuration UI. Static for now — derived from the registry's
// known action names and hard-coded per-action knowledge. A future
// extension could let handlers self-describe via an optional
// Describe() method on the Handler interface.
func HandleButtonActionsList(reg *buttonactions.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		if reg == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "button actions not configured"})
			return
		}
		names := reg.Names()
		out := make([]ActionInfo, 0, len(names))
		for _, n := range names {
			out = append(out, describeAction(n))
		}
		c.JSON(http.StatusOK, gin.H{"actions": out})
	}
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
