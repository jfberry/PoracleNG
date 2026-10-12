package enrichment

import "math"

// ShinyFeed is the optional shiny-rate feed ([shiny_rates], implemented by
// personalshiny.Rates). known=false when the feed has not loaded or does not
// list the variant.
type ShinyFeed interface {
	ShinyVariant(pokemonID, form, costume int, lat, lon float64) (supported bool, probability float64, known bool)
}

// shinyFacts is what the shiny template fields are built from.
type shinyFacts struct {
	answered bool    // some source answered: set shinyPossible
	possible bool    // shinyPossible
	observed float64 // observed 1:N from Poracle's stats (shinyStats), 0 = unknown
	oneIn    int     // expected 1:N from the feed (shinyRate), 0 = unknown
	boosted  bool    // feed probability above 1/512 (shinyRateBoosted)
}

// shinyFacts resolves shiny information for a variant at a location. The
// feed decides when it lists the variant; otherwise the observed stats
// answer, exactly as before the feed existed.
func (e *Enricher) shinyFacts(pokemonID, form, costume int, lat, lon float64) shinyFacts {
	var f shinyFacts
	if e.ShinyProvider != nil {
		f.observed = e.ShinyProvider.GetShinyRate(pokemonID)
		f.answered = true
		f.possible = f.observed > 0
	}
	if e.ShinyFeed != nil {
		if supported, p, known := e.ShinyFeed.ShinyVariant(pokemonID, form, costume, lat, lon); known {
			f.answered = true
			f.possible = supported
			if supported && p > 0 && p <= 1 {
				f.oneIn = int(math.Round(1 / p))
				f.boosted = p > 1.0/512
			}
		}
	}
	return f
}
