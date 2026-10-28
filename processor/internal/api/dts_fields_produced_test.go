package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// fieldProducerDirs are the packages whose code can put a key into a DTS
// view: enrichment (base/per-language/per-user layers), dts (aliases,
// computed + emoji layers, block-helper contexts, quest summary view),
// cmd/processor (monsterChanged change fields, quest summary entries) and
// bot/commands (the greeting view).
var fieldProducerDirs = []string{
	"../enrichment",
	"../dts",
	"../../cmd/processor",
	"../bot/commands",
}

// rawWebhookDirs hold the structs the raw webhook layer is decoded from
// (Golbat payloads and the synthesized weather-change message); their json
// tags are the raw field names templates can reach.
var rawWebhookDirs = []string{
	"../webhook",
	"../tracker",
}

// collectProducedKeys returns every string literal in the producer
// packages plus every json tag name in the raw-webhook packages (non-test
// files only).
func collectProducedKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := make(map[string]bool, 4096)
	walk := func(dir string, visit func(n ast.Node)) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		fset := token.NewFileSet()
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				visit(n)
				return true
			})
		}
	}
	for _, dir := range fieldProducerDirs {
		walk(dir, func(n ast.Node) {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil {
					keys[s] = true
				}
			}
		})
	}
	for _, dir := range rawWebhookDirs {
		walk(dir, func(n ast.Node) {
			field, ok := n.(*ast.Field)
			if !ok || field.Tag == nil {
				return
			}
			tag, err := strconv.Unquote(field.Tag.Value)
			if err != nil {
				return
			}
			name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
			if name != "" && name != "-" {
				keys[name] = true
			}
		})
	}
	return keys
}

// TestFieldCatalogue_FieldsAreProduced guards the DTS editor field catalogue
// (fieldsByType) against entries for fields nothing produces (#274): every
// catalogued field — top level and inside block scopes — must be a key that
// some producer can emit. This is a name-level check, not a per-type one:
// it catches invented / misspelled / renamed fields (the editor offering
// {{isNight}} when the view has nightTime), while per-type placement is kept
// honest by the composition in fieldsByType. Common words ("name", "id")
// pass trivially; distinctive field names are what this protects.
func TestFieldCatalogue_FieldsAreProduced(t *testing.T) {
	produced := collectProducedKeys(t)

	check := func(dtsType, where string, fields []FieldDef) {
		for _, f := range fields {
			if produced[f.Name] {
				continue
			}
			t.Errorf("%s: %s field %q is not produced by enrichment, aliases, computed/emoji layers or the raw webhook — rename it to the real field or remove it", dtsType, where, f.Name)
		}
	}

	for dtsType, entry := range fieldsByType {
		check(dtsType, "top-level", entry.Fields)
		for _, scope := range entry.BlockScopes {
			check(dtsType, "{{#"+scope.Helper+"}} block", scope.Fields)
		}
	}
}

// TestFieldCatalogue_NoDuplicates catches the same field listed twice for a
// type (e.g. a type-specific entry repeating one from the common groups).
func TestFieldCatalogue_NoDuplicates(t *testing.T) {
	for dtsType, entry := range fieldsByType {
		seen := make(map[string]bool, len(entry.Fields))
		for _, f := range entry.Fields {
			if seen[f.Name] {
				t.Errorf("%s: field %q listed more than once", dtsType, f.Name)
			}
			seen[f.Name] = true
		}
	}
}

// TestFieldCatalogue_PerTypeGroups locks which common groups each type
// gets: map links, time-remaining and sun flags are only produced by some
// enrichers.
func TestFieldCatalogue_PerTypeGroups(t *testing.T) {
	for _, tc := range []struct {
		dtsType                string
		maps, tth, sun, common bool
	}{
		{"monster", true, true, true, true},
		{"raid", true, true, true, true},
		{"quest", true, true, false, true},
		{"nest", true, true, false, true},
		{"fort-update", true, false, true, true},
		{"tappable", true, true, true, true},
		{"weatherchange", false, false, true, true},
		{"questSummary", false, false, false, false},
		{"greeting", false, false, false, false},
	} {
		fields := fieldsByType[tc.dtsType].Fields
		for name, want := range map[string]bool{
			"googleMapUrl": tc.maps,
			"tthm":         tc.tth,
			"nightTime":    tc.sun,
			"addr":         tc.common,
		} {
			if got := hasFieldDef(fields, name); got != want {
				t.Errorf("%s: lists %s = %v, want %v", tc.dtsType, name, got, want)
			}
		}
	}
	if !hasFieldDef(fieldsByType["greeting"].Fields, "prefix") {
		t.Error("greeting should list prefix")
	}
}
