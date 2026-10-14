package enrichment

import (
	"math"

	"github.com/pokemon/poracleng/processor/internal/personalshiny"
)

// ShinyFeed is the optional shiny-rate feed ([shiny_rates], implemented by
// personalshiny.Rates). known=false when the feed has not loaded or does not
// list the variant.
type ShinyFeed interface {
	ShinyVariantForm(pokemonID, form, defaultForm, costume int, lat, lon float64) (supported bool, probability float64, known bool)
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
		if supported, p, known := e.ShinyFeed.ShinyVariantForm(pokemonID, form, e.GameData.DefaultForm(pokemonID), costume, lat, lon); known {
			f.answered = true
			// supported is the base capability; the badge follows the
			// location, so a zero override here means not possible here.
			f.possible = supported && p > 0
			if supported && p > 0 && p <= 1 {
				f.oneIn = int(math.Round(1 / p))
				// Ditto's 1/64 is its normal rate, not an event.
				f.boosted = p > 1.0/512 && pokemonID != personalshiny.DittoPokemonID
			}
		}
	}
	return f
}
