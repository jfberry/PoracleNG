package tracker

import (
	"testing"
	"time"
)

func TestAllCostumesGroupsByPokemon(t *testing.T) {
	r := NewRecentActivity()
	r.RecordCostume(25, 1)
	r.RecordCostume(25, 8)
	r.RecordCostume(133, 5)

	got := r.AllCostumes()
	if len(got) != 2 {
		t.Fatalf("AllCostumes() covered %d pokemon, want 2 (got %v)", len(got), got)
	}
	if len(got[25]) != 2 {
		t.Errorf("AllCostumes()[25] = %v, want 2 costumes", got[25])
	}
	if len(got[133]) != 1 {
		t.Errorf("AllCostumes()[133] = %v, want 1 costume", got[133])
	}
}

func TestAllCostumesOmitsPokemonWithOnlyExpiredEntries(t *testing.T) {
	r := NewRecentActivity()
	r.now = func() time.Time { return time.Unix(1000, 0) }
	r.RecordCostume(25, 1)

	r.now = func() time.Time { return time.Unix(1000+int64(7*time.Hour/time.Second), 0) }
	r.RecordCostume(133, 5)

	got := r.AllCostumes()
	if _, ok := got[25]; ok {
		t.Errorf("pokemon 25 had only expired costumes, want it omitted entirely; got %v", got)
	}
	if len(got[133]) != 1 {
		t.Errorf("AllCostumes()[133] = %v, want the live costume", got[133])
	}
}

func TestAllFormsRaidCostumesRaidFormsGroupByPokemon(t *testing.T) {
	r := NewRecentActivity()
	r.RecordForm(25, 46)
	r.RecordRaidCostume(150, 12)
	r.RecordRaidForm(150, 952)

	if got := r.AllForms(); len(got[25]) != 1 {
		t.Errorf("AllForms()[25] = %v, want 1 form", got[25])
	}
	if got := r.AllRaidCostumes(); len(got[150]) != 1 {
		t.Errorf("AllRaidCostumes()[150] = %v, want 1 costume", got[150])
	}
	if got := r.AllRaidForms(); len(got[150]) != 1 {
		t.Errorf("AllRaidForms()[150] = %v, want 1 form", got[150])
	}
}
