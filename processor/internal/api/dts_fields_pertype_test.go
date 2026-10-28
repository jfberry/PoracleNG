package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// enrichFunc is one enrichment-package function: the view keys it assigns
// (m["key"] = … or a "key": … map-literal entry) and the package
// functions/methods it calls.
type enrichFunc struct {
	keys  map[string]bool
	calls []string
}

// parseEnrichmentFuncs indexes every non-test function in ../enrichment by
// name (methods and plain functions share one namespace — names are unique
// enough within the package for this guard).
func parseEnrichmentFuncs(t *testing.T) map[string]*enrichFunc {
	t.Helper()
	const dir = "../enrichment"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	funcs := make(map[string]*enrichFunc)
	fset := token.NewFileSet()
	strKey := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(lit.Value)
		return s, err == nil
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ef := &enrichFunc{keys: map[string]bool{}}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.AssignStmt:
					for _, lhs := range n.Lhs {
						if ix, ok := lhs.(*ast.IndexExpr); ok {
							if k, ok := strKey(ix.Index); ok {
								ef.keys[k] = true
							}
						}
					}
				case *ast.KeyValueExpr:
					if k, ok := strKey(n.Key); ok {
						ef.keys[k] = true
					}
				case *ast.CallExpr:
					switch fn := n.Fun.(type) {
					case *ast.Ident:
						ef.calls = append(ef.calls, fn.Name)
					case *ast.SelectorExpr:
						if x, ok := fn.X.(*ast.Ident); ok && x.Name == "e" {
							ef.calls = append(ef.calls, fn.Sel.Name)
						}
					}
				}
				return true
			})
			funcs[fd.Name.Name] = ef
		}
	}
	return funcs
}

// reachableKeys returns every key assigned by the entry functions or
// anything they (transitively) call within the enrichment package.
func reachableKeys(funcs map[string]*enrichFunc, entries []string) map[string]bool {
	keys := map[string]bool{}
	seen := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		ef, ok := funcs[name]
		if !ok || seen[name] {
			return
		}
		seen[name] = true
		for k := range ef.keys {
			keys[k] = true
		}
		for _, c := range ef.calls {
			visit(c)
		}
	}
	for _, e := range entries {
		visit(e)
	}
	return keys
}

// typeEnrichEntries maps DTS types to the enrichment entry points that
// build their base / per-language / per-user layers.
var typeEnrichEntries = map[string][]string{
	"monster":        {"Pokemon", "PokemonTranslate", "PokemonPerUser"},
	"monsterNoIv":    {"Pokemon", "PokemonTranslate", "PokemonPerUser"},
	"monsterChanged": {"Pokemon", "PokemonTranslate", "PokemonPerUser"},
	"raid":           {"Raid", "RaidTranslate"},
	"rsvpChanges":    {"Raid", "RaidTranslate"},
	"egg":            {"Raid", "RaidTranslate"},
	"maxbattle":      {"Maxbattle", "MaxbattleTranslate"},
	"tappable":       {"Tappable", "TappableTranslate"},
}

// derivedFieldSources maps catalogued view fields that are derived later
// (emoji resolution, the composed weatherChange, forecast names gated on
// weatherNext) to the enrichment key that must be produced for them to
// render. Listing a field for a type whose enrichment never sets the
// source key is the bug class of #314.
var derivedFieldSources = map[string]string{
	"boostingWeathersEmoji": "boostingWeatherEmojiKeys",
	"weatherChange":         "weatherNext",
	"weatherChangeTime":     "weatherChangeTime",
	"weatherCurrentName":    "weatherNext",
	"weatherCurrentEmoji":   "weatherNext",
	"weatherNextName":       "weatherNext",
	"weatherNextEmoji":      "weatherNext",
}

// TestFieldCatalogue_DerivedFieldsProducedPerType is the per-type
// counterpart of TestFieldCatalogue_FieldsAreProduced: for types whose
// enrichment entry points are known, every catalogued derived field must
// have its source key produced by THAT type's enrichment (#314).
func TestFieldCatalogue_DerivedFieldsProducedPerType(t *testing.T) {
	funcs := parseEnrichmentFuncs(t)
	for dtsType, entries := range typeEnrichEntries {
		for _, e := range entries {
			if _, ok := funcs[e]; !ok {
				t.Fatalf("%s: enrichment entry point %s not found", dtsType, e)
			}
		}
		produced := reachableKeys(funcs, entries)
		for _, f := range fieldsByType[dtsType].Fields {
			src, ok := derivedFieldSources[f.Name]
			if !ok {
				continue
			}
			if !produced[src] {
				t.Errorf("%s: catalogue lists %q but %v never set %q", dtsType, f.Name, entries, src)
			}
		}
	}
}

// TestFieldCatalogue_WeatherFieldsPerType pins where the #314 fields are
// listed: pokemon and hatched raids get the boost emoji and the forecast
// change fields; eggs have no boss types so no forecast.
func TestFieldCatalogue_WeatherFieldsPerType(t *testing.T) {
	forecast := []string{"weatherChange", "weatherCurrentName", "weatherNextName"}
	for _, dtsType := range []string{"monster", "raid", "rsvpChanges"} {
		fields := fieldsByType[dtsType].Fields
		for _, name := range append([]string{"boostingWeathersEmoji"}, forecast...) {
			if !hasFieldDef(fields, name) {
				t.Errorf("%s should list %s", dtsType, name)
			}
		}
	}
	for _, name := range forecast {
		if hasFieldDef(fieldsByType["egg"].Fields, name) {
			t.Errorf("egg should not list %s (no boss types, no forecast)", name)
		}
	}
}
