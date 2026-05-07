package discordbot

import (
	"fmt"
	"strings"

	raymond "github.com/mailgun/raymond/v2"

	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

// fenceContext builds the data context that filter and params expressions
// see during rendering. Named Fence fields are populated first, then
// properties are merged in — but only for keys that don't collide with a
// named field, so the struct fields always win.
func fenceContext(f geofence.Fence) map[string]any {
	ctx := map[string]any{
		"name":             f.Name,
		"group":            f.Group,
		"description":      f.Description,
		"color":            f.Color,
		"userSelectable":   f.UserSelectable,
		"displayInMatches": f.DisplayInMatches,
	}
	for k, v := range f.Properties {
		if _, exists := ctx[k]; exists {
			continue
		}
		ctx[k] = v
	}
	return ctx
}

// isTruthy applies the bulk-autocreate truthiness rule: trimmed string is
// truthy unless empty, "false", or "0".
func isTruthy(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" || t == "false" || t == "0" {
		return false
	}
	return true
}

// renderFilter evaluates a Handlebars expression against a fence and
// reports whether the fence matches. An empty expression matches all.
func renderFilter(expr string, f geofence.Fence) (bool, error) {
	if strings.TrimSpace(expr) == "" {
		return true, nil
	}
	dts.RegisterHelpers()
	tmpl, err := raymond.Parse(expr)
	if err != nil {
		return false, fmt.Errorf("parse filter %q: %w", expr, err)
	}
	out, err := tmpl.Exec(fenceContext(f))
	if err != nil {
		return false, fmt.Errorf("render filter %q: %w", expr, err)
	}
	return isTruthy(out), nil
}

// renderParams renders each element of params[] against the fence context
// and returns the resulting strings, one per input. Used by the bulk
// runner to derive the args passed to applyAutocreate.
func renderParams(params []string, f geofence.Fence) ([]string, error) {
	dts.RegisterHelpers()
	out := make([]string, len(params))
	ctx := fenceContext(f)
	for i, p := range params {
		tmpl, err := raymond.Parse(p)
		if err != nil {
			return nil, fmt.Errorf("parse params[%d] %q: %w", i, p, err)
		}
		s, err := tmpl.Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("render params[%d] %q: %w", i, p, err)
		}
		out[i] = s
	}
	return out, nil
}
