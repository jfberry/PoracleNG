package gamedata

import (
	"fmt"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// PreviousEvolution records what pokemon evolves into a target pokemon.
type PreviousEvolution struct {
	PokemonID int
	FormID    int
	Evolution Evolution // the evolution entry (candy, item, conditions)
}

// BuildPrevEvolutions builds a reverse index: for each evolution target,
// record what pokemon evolves into it. Called once after loading all monsters.
func BuildPrevEvolutions(monsters map[MonsterKey]*Monster) map[int][]PreviousEvolution {
	prev := make(map[int][]PreviousEvolution)
	for _, mon := range monsters {
		for _, evo := range mon.Evolutions {
			prev[evo.PokemonID] = append(prev[evo.PokemonID], PreviousEvolution{
				PokemonID: mon.PokemonID,
				FormID:    mon.FormID,
				Evolution: evo,
			})
		}
	}
	return prev
}

// EvolutionRequirementText builds a translated description of evolution requirements.
// Example outputs: "25 candies", "100 candies + Sun Stone", "25 candies + buddy + daytime"
func EvolutionRequirementText(tr *i18n.Translator, evo Evolution) string {
	var parts []string

	if evo.CandyCost > 0 {
		parts = append(parts, tr.Tf("evo.candy_fmt", evo.CandyCost))
	}

	if evo.ItemRequirement > 0 {
		itemName := tr.T(fmt.Sprintf("item_%d", evo.ItemRequirement))
		parts = append(parts, itemName)
	}

	if evo.MustBeBuddy {
		parts = append(parts, tr.T("evo.buddy"))
	}

	if evo.OnlyDaytime {
		parts = append(parts, tr.T("evo.daytime"))
	}

	if evo.OnlyNighttime {
		parts = append(parts, tr.T("evo.nighttime"))
	}

	if evo.TradeBonus {
		parts = append(parts, tr.T("evo.or_trade"))
	}

	if evo.GenderRequirement != 0 {
		genderKey := fmt.Sprintf("gender_%d", evo.GenderRequirement)
		parts = append(parts, tr.T(genderKey))
	}

	return strings.Join(parts, " + ")
}
