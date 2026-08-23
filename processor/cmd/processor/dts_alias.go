package main

import "github.com/pokemon/poracleng/processor/internal/dtsmap"

// dtsSource is a local alias for dtsmap.Source. The canonical table now
// lives in internal/dtsmap (see that package's doc comment) so
// internal/api's testdata endpoints can share it — package main can't be
// imported by internal packages. dtsSource/dtsAlias/dtsTypeMap are kept as
// thin wrappers here so existing call sites (enrich.go, test.go) and tests
// (dts_alias_test.go) are unaffected by the move.
type dtsSource = dtsmap.Source

// dtsAlias resolves a DTS template-type name (e.g. "monster", "egg",
// "monsterChanged") OR a raw webhook type (e.g. "pokemon", "max_battle") to
// its canonical dtsSource. The second return value is false when name is
// not recognized.
func dtsAlias(name string) (dtsSource, bool) {
	return dtsmap.Alias(name)
}

// dtsTypeMap returns a defensive copy of the full canonical table, for the
// API to expose (see superpowers/sdd task-8-brief.md).
func dtsTypeMap() map[string]dtsSource {
	return dtsmap.TypeMap()
}
