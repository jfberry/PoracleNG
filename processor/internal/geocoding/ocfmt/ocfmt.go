// Package ocfmt implements address formatting using OpenCage address-formatting
// templates (worldwide.yaml). It embeds the templates and provides a simple
// formatter that renders addresses using country-specific Mustache-like templates.
//
// Templates source: https://github.com/OpenCageData/address-formatting
// License: MIT
package ocfmt

import (
	"embed"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed worldwide.yaml
var templatesFS embed.FS

// countryEntry represents a parsed entry from worldwide.yaml.
type countryEntry struct {
	AddressTemplate   string     `yaml:"address_template"`
	FallbackTemplate  string     `yaml:"fallback_template"`
	UseCountry        string     `yaml:"use_country"`
	Replace           [][]string `yaml:"replace"`
	PostformatReplace [][]string `yaml:"postformat_replace"`
}

// Formatter formats addresses using OpenCage country-specific templates.
type Formatter struct {
	countries    map[string]*countryEntry
	defaultEntry *countryEntry
}

var (
	globalFormatter *Formatter
	globalOnce      sync.Once
)

// Global returns the shared Formatter instance, loading templates on first call.
func Global() *Formatter {
	globalOnce.Do(func() {
		f, err := newFormatter()
		if err != nil {
			// Should not happen with embedded data
			globalFormatter = &Formatter{
				countries:    map[string]*countryEntry{},
				defaultEntry: &countryEntry{AddressTemplate: "{{{road}}} {{{house_number}}}, {{{postcode}}} {{{city}}}, {{{country}}}"},
			}
			return
		}
		globalFormatter = f
	})
	return globalFormatter
}

func newFormatter() (*Formatter, error) {
	data, err := templatesFS.ReadFile("worldwide.yaml")
	if err != nil {
		return nil, err
	}

	// Parse YAML as generic map — the file mixes strings (generic templates
	// with anchors) and objects (country entries). We must handle both.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	f := &Formatter{
		countries: make(map[string]*countryEntry, len(raw)),
	}

	for key, val := range raw {
		// Skip string entries (generic template anchors like generic1, fallback1)
		m, ok := val.(map[string]interface{})
		if !ok {
			continue
		}

		entry := &countryEntry{
			AddressTemplate:  mapStr(m, "address_template"),
			FallbackTemplate: mapStr(m, "fallback_template"),
			UseCountry:       mapStr(m, "use_country"),
			Replace:          mapStrSlices(m, "replace"),
			PostformatReplace: mapStrSlices(m, "postformat_replace"),
		}

		upper := strings.ToUpper(key)
		if upper == "DEFAULT" {
			f.defaultEntry = entry
		} else {
			f.countries[upper] = entry
		}
	}

	if f.defaultEntry == nil {
		f.defaultEntry = &countryEntry{
			AddressTemplate: "{{{road}}} {{{house_number}}}, {{{postcode}}} {{{city}}}, {{{country}}}",
		}
	}

	return f, nil
}

// mapStr extracts a string value from a map.
func mapStr(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// mapStrSlices extracts a [][]string from a map (for replace/postformat_replace rules).
func mapStrSlices(m map[string]interface{}, key string) [][]string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var result [][]string
	for _, item := range arr {
		inner, ok := item.([]interface{})
		if !ok {
			continue
		}
		var pair []string
		for _, elem := range inner {
			s, ok := elem.(string)
			if !ok {
				continue
			}
			pair = append(pair, s)
		}
		if len(pair) == 2 {
			result = append(result, pair)
		}
	}
	return result
}

// Format renders an address using the country-specific template.
// The components map uses OpenCage field names: road, house_number, city,
// town, village, suburb, neighbourhood, postcode, state, state_code,
// county, country, country_code, house, attention, place, hamlet, etc.
func (f *Formatter) Format(components map[string]string) string {
	cc := strings.ToUpper(components["country_code"])

	entry := f.resolve(cc)
	if entry == nil {
		entry = f.defaultEntry
	}

	// Apply input replace rules
	if len(entry.Replace) > 0 {
		components = applyReplace(components, entry.Replace)
	}

	// Render template
	result := renderTemplate(entry.AddressTemplate, components)

	// If result is empty/whitespace, try fallback template
	if strings.TrimSpace(result) == "" && entry.FallbackTemplate != "" {
		result = renderTemplate(entry.FallbackTemplate, components)
	}

	// If still empty after main template, try the default fallback
	if strings.TrimSpace(result) == "" && entry != f.defaultEntry {
		if f.defaultEntry.FallbackTemplate != "" {
			result = renderTemplate(f.defaultEntry.FallbackTemplate, components)
		}
	}

	// Apply postformat replace rules
	if len(entry.PostformatReplace) > 0 {
		result = applyPostformatReplace(result, entry.PostformatReplace)
	}

	// Clean up output
	result = cleanOutput(result)

	return result
}

// resolve looks up the country entry, following use_country redirects.
func (f *Formatter) resolve(cc string) *countryEntry {
	seen := make(map[string]bool)
	for {
		entry, ok := f.countries[cc]
		if !ok {
			return f.defaultEntry
		}
		if entry.UseCountry == "" {
			return entry
		}
		if seen[cc] {
			return entry // break infinite loop
		}
		seen[cc] = true
		cc = strings.ToUpper(entry.UseCountry)
	}
}

// renderTemplate processes a Mustache-like template with {{{field}}} substitutions
// and {{#first}} ... {{/first}} blocks.
func renderTemplate(tmpl string, components map[string]string) string {
	// Process {{#first}} ... {{/first}} blocks
	result := firstBlockRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		inner := firstBlockRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return ""
		}
		return resolveFirst(inner[1], components)
	})

	// Replace {{{field}}} and {{field}} with values
	result = tripleBraceRe.ReplaceAllStringFunc(result, func(match string) string {
		field := match[3 : len(match)-3]
		return components[strings.TrimSpace(field)]
	})
	result = doubleBraceRe.ReplaceAllStringFunc(result, func(match string) string {
		field := match[2 : len(match)-2]
		return components[strings.TrimSpace(field)]
	})

	return result
}

var (
	firstBlockRe  = regexp.MustCompile(`\{\{#first\}\}\s*(.*?)\s*\{\{/first\}\}`)
	tripleBraceRe = regexp.MustCompile(`\{\{\{[^}]+\}\}\}`)
	doubleBraceRe = regexp.MustCompile(`\{\{[^#/][^}]*\}\}`)
)

// resolveFirst handles {{#first}} A || B || C {{/first}} — returns the first
// non-empty alternative.
func resolveFirst(inner string, components map[string]string) string {
	alternatives := strings.Split(inner, "||")
	for _, alt := range alternatives {
		rendered := strings.TrimSpace(alt)
		// Substitute fields within this alternative
		rendered = tripleBraceRe.ReplaceAllStringFunc(rendered, func(match string) string {
			field := match[3 : len(match)-3]
			return components[strings.TrimSpace(field)]
		})
		rendered = doubleBraceRe.ReplaceAllStringFunc(rendered, func(match string) string {
			field := match[2 : len(match)-2]
			return components[strings.TrimSpace(field)]
		})
		if strings.TrimSpace(rendered) != "" {
			return strings.TrimSpace(rendered)
		}
	}
	return ""
}

// applyReplace applies input replacement rules to component values.
func applyReplace(components map[string]string, rules [][]string) map[string]string {
	out := make(map[string]string, len(components))
	for k, v := range components {
		out[k] = v
	}
	for _, rule := range rules {
		if len(rule) != 2 {
			continue
		}
		re, err := regexp.Compile(rule[0])
		if err != nil {
			continue
		}
		for k, v := range out {
			if re.MatchString(v) {
				out[k] = re.ReplaceAllString(v, rule[1])
			}
		}
	}
	return out
}

// applyPostformatReplace applies regex replacements to the formatted output.
func applyPostformatReplace(result string, rules [][]string) string {
	for _, rule := range rules {
		if len(rule) != 2 {
			continue
		}
		re, err := regexp.Compile(rule[0])
		if err != nil {
			continue
		}
		result = re.ReplaceAllString(result, rule[1])
	}
	return result
}

// cleanOutput normalizes whitespace and removes blank lines from formatted output.
func cleanOutput(s string) string {
	// Split into lines, trim each, remove empty
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Remove leading/trailing commas
		line = strings.TrimLeft(line, ", ")
		line = strings.TrimRight(line, ", ")
		if line != "" {
			out = append(out, line)
		}
	}
	// Join with ", " for single-line output (used as FormattedAddress)
	return strings.Join(out, ", ")
}
