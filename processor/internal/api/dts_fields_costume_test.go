package api

import "testing"

// hasFieldDef is defined in dts_fields_showcase_test.go (same test package).

func TestMonsterFields_Costume(t *testing.T) {
	m := fieldsByType["monster"]
	if !hasFieldDef(m.Fields, "costumeName") {
		t.Error("monster type should list costumeName")
	}
}
