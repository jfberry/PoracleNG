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

// compiledFilter holds a pre-parsed filter expression ready for repeated
// per-fence evaluation. A nil tmpl means "match all" (empty expression).
type compiledFilter struct {
	tmpl *raymond.Template
}

// compiledParams holds pre-parsed templates for each element of a params
// slice, ready for repeated per-fence rendering.
type compiledParams struct {
	tmpls []*raymond.Template
	raw   []string // original expressions, for error messages
}

// compileFilter parses a filter expression once.
// An empty/whitespace-only expr returns (compiledFilter{tmpl: nil}, nil),
// which matches all fences.
func compileFilter(expr string) (compiledFilter, error) {
	if strings.TrimSpace(expr) == "" {
		return compiledFilter{}, nil
	}
	dts.RegisterHelpers()
	tmpl, err := raymond.Parse(expr)
	if err != nil {
		return compiledFilter{}, fmt.Errorf("parse filter %q: %w", expr, err)
	}
	return compiledFilter{tmpl: tmpl}, nil
}

// compileParams parses each element of params once.
func compileParams(params []string) (compiledParams, error) {
	dts.RegisterHelpers()
	cp := compiledParams{
		tmpls: make([]*raymond.Template, len(params)),
		raw:   params,
	}
	for i, p := range params {
		tmpl, err := raymond.Parse(p)
		if err != nil {
			return compiledParams{}, fmt.Errorf("parse params[%d] %q: %w", i, p, err)
		}
		cp.tmpls[i] = tmpl
	}
	return cp, nil
}

// matches reports whether the compiled filter matches the given fence.
// A nil tmpl (empty filter) matches all fences.
func (cf compiledFilter) matches(f geofence.Fence) (bool, error) {
	if cf.tmpl == nil {
		return true, nil
	}
	out, err := cf.tmpl.Exec(fenceContext(f))
	if err != nil {
		return false, fmt.Errorf("render filter: %w", err)
	}
	return isTruthy(out), nil
}

// render renders each pre-parsed param against the fence context and
// returns the resulting strings, one per input.
func (cp compiledParams) render(f geofence.Fence) ([]string, error) {
	out := make([]string, len(cp.tmpls))
	ctx := fenceContext(f)
	for i, tmpl := range cp.tmpls {
		s, err := tmpl.Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("render params[%d] %q: %w", i, cp.raw[i], err)
		}
		out[i] = s
	}
	return out, nil
}

// renderFilter evaluates a Handlebars expression against a fence and
// reports whether the fence matches. An empty expression matches all.
// This is a one-shot wrapper around compileFilter + matches, retained for
// backward compatibility with tests and any future one-off callers.
func renderFilter(expr string, f geofence.Fence) (bool, error) {
	cf, err := compileFilter(expr)
	if err != nil {
		return false, err
	}
	return cf.matches(f)
}

// renderParams renders each element of params[] against the fence context
// and returns the resulting strings, one per input.
// This is a one-shot wrapper around compileParams + render, retained for
// backward compatibility with tests and any future one-off callers.
func renderParams(params []string, f geofence.Fence) ([]string, error) {
	cp, err := compileParams(params)
	if err != nil {
		return nil, err
	}
	return cp.render(f)
}
