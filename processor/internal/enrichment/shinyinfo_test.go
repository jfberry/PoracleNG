package enrichment

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

type fakeObserved map[int]float64

func (f fakeObserved) GetShinyRate(id int) float64 { return f[id] }

type fakeFeed map[[3]int]struct {
	supported bool
	p         float64
}

func (f fakeFeed) ShinyVariantForm(id, form, defaultForm, costume int, _, _ float64) (bool, float64, bool) {
	v, ok := f[[3]int{id, form, costume}]
	if !ok && form == 0 && defaultForm != 0 {
		v, ok = f[[3]int{id, defaultForm, costume}]
	}
	return v.supported, v.p, ok
}

func TestShinyFactsNoFeedIsObservedOnly(t *testing.T) {
	e := &Enricher{ShinyProvider: fakeObserved{25: 480}}
	f := e.shinyFacts(25, 0, 0, 0, 0)
	if !f.answered || !f.possible || f.observed != 480 || f.oneIn != 0 {
		t.Fatalf("observed only = %+v", f)
	}
	if f := e.shinyFacts(1, 0, 0, 0, 0); !f.answered || f.possible {
		t.Fatalf("unseen species = %+v", f)
	}
	if f := (&Enricher{}).shinyFacts(25, 0, 0, 0, 0); f.answered {
		t.Fatalf("no providers = %+v", f)
	}
}

func TestShinyFactsFeedWinsWhenKnown(t *testing.T) {
	e := &Enricher{
		ShinyProvider: fakeObserved{25: 480, 26: 300},
		ShinyFeed: fakeFeed{
			{25, 0, 0}: {true, 0.04},
			{27, 0, 0}: {false, 0},
		},
	}
	if f := e.shinyFacts(25, 0, 0, 0, 0); !f.possible || f.oneIn != 25 || !f.boosted || f.observed != 480 {
		t.Fatalf("listed boosted = %+v", f)
	}
	if f := e.shinyFacts(27, 0, 0, 0, 0); !f.answered || f.possible || f.oneIn != 0 {
		t.Fatalf("listed unreleased = %+v", f)
	}
	// Unlisted variant: fall back to observed stats.
	if f := e.shinyFacts(26, 0, 0, 0, 0); !f.possible || f.oneIn != 0 {
		t.Fatalf("unlisted falls back = %+v", f)
	}
	// Form-specific: (25, 5) is not listed, so observed answers.
	if f := e.shinyFacts(25, 5, 0, 0, 0); f.oneIn != 0 || !f.possible {
		t.Fatalf("other form falls back = %+v", f)
	}
}

func TestShinyFactsBaseRateNotBoosted(t *testing.T) {
	e := &Enricher{ShinyFeed: fakeFeed{{25, 0, 0}: {true, 1.0 / 512}}}
	if f := e.shinyFacts(25, 0, 0, 0, 0); f.boosted || f.oneIn != 512 {
		t.Fatalf("1/512 = %+v", f)
	}
}

func TestShinyFactsFormZeroUsesDefaultForm(t *testing.T) {
	gd := &gamedata.GameData{Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
		{ID: 25, Form: 0}: {PokemonID: 25, DefaultFormID: 598},
	}}
	e := &Enricher{GameData: gd, ShinyFeed: fakeFeed{{25, 598, 0}: {true, 0.04}}}
	if f := e.shinyFacts(25, 0, 0, 0, 0); !f.answered || !f.possible || f.oneIn != 25 {
		t.Fatalf("form 0 via default form = %+v", f)
	}
	// Nil GameData: no fallback, nothing listed under form 0.
	e = &Enricher{ShinyFeed: fakeFeed{{25, 598, 0}: {true, 0.04}}}
	if f := e.shinyFacts(25, 0, 0, 0, 0); f.answered {
		t.Fatalf("nil GameData = %+v", f)
	}
}
