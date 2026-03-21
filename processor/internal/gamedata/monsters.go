package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Monster represents a pokemon species + form combination.
type Monster struct {
	PokemonID      int
	FormID         int
	Types          []int // numeric type IDs (e.g. [4, 12])
	Attack         int
	Defense        int
	Stamina        int
	GenID          int
	Family         int
	Legendary      bool
	Mythic         bool
	UltraBeast     bool
	Evolutions     []Evolution
	TempEvolutions []TempEvolution
}

// Evolution represents a regular pokemon evolution.
type Evolution struct {
	PokemonID int
	FormID    int
	CandyCost int
}

// TempEvolution represents a mega/primal evolution.
type TempEvolution struct {
	TempEvoID int // 1=Mega, 2=Mega X, 3=Mega Y, 4=Primal, 5=Mega Z
	Attack    int
	Defense   int
	Stamina   int
	Types     []int // override types (nil = same as base)
}

// rawPokemon is the raw masterfile format for pokemon.
type rawPokemon struct {
	PokedexID     int    `json:"pokedexId"`
	PokemonName   string `json:"pokemonName"`
	DefaultFormID int    `json:"defaultFormId"`
	Types         []int  `json:"types"`
	GenID         int    `json:"genId"`
	Attack        int    `json:"attack"`
	Defense       int    `json:"defense"`
	Stamina       int    `json:"stamina"`
	Family        int    `json:"family"`
	Legendary     bool   `json:"legendary"`
	Mythic        bool   `json:"mythic"`
	UltraBeast    bool   `json:"ultraBeast"`
	Forms         []int  `json:"forms"`
	Evolutions    []struct {
		EvoID     int `json:"evoId"`
		FormID    int `json:"formId"`
		CandyCost int `json:"candyCost"`
	} `json:"evolutions"`
	TempEvolutions []struct {
		TempEvoID int   `json:"tempEvoId"`
		Attack    int   `json:"attack"`
		Defense   int   `json:"defense"`
		Stamina   int   `json:"stamina"`
		Types     []int `json:"types"`
	} `json:"tempEvolutions"`
}

// rawForm is the raw masterfile format for forms.
type rawForm struct {
	FormID   int    `json:"formId"`
	FormName string `json:"formName"`
	Proto    string `json:"proto"`
	Attack   int    `json:"attack"`
	Defense  int    `json:"defense"`
	Stamina  int    `json:"stamina"`
	Types    []int  `json:"types"`
	Evolutions []struct {
		EvoID     int `json:"evoId"`
		FormID    int `json:"formId"`
		CandyCost int `json:"candyCost"`
	} `json:"evolutions"`
	TempEvolutions []struct {
		TempEvoID int   `json:"tempEvoId"`
		Attack    int   `json:"attack"`
		Defense   int   `json:"defense"`
		Stamina   int   `json:"stamina"`
		Types     []int `json:"types"`
	} `json:"tempEvolutions"`
}

// LoadMonsters parses raw masterfile pokemon.json + forms.json files
// and joins form overrides onto base pokemon data.
func LoadMonsters(pokemonPath, formsPath string) (map[MonsterKey]*Monster, error) {
	pokemonData, err := os.ReadFile(pokemonPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", pokemonPath, err)
	}

	var rawPokemon map[string]rawPokemon
	if err := json.Unmarshal(pokemonData, &rawPokemon); err != nil {
		return nil, fmt.Errorf("parse %s: %w", pokemonPath, err)
	}

	// Load forms
	formsData, err := os.ReadFile(formsPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", formsPath, err)
	}

	var rawForms map[string]rawForm
	if err := json.Unmarshal(formsData, &rawForms); err != nil {
		return nil, fmt.Errorf("parse %s: %w", formsPath, err)
	}

	monsters := make(map[MonsterKey]*Monster, len(rawPokemon)*2)

	for _, poke := range rawPokemon {
		// Create base entry (form 0)
		baseEvolutions := make([]Evolution, len(poke.Evolutions))
		for i, e := range poke.Evolutions {
			baseEvolutions[i] = Evolution{
				PokemonID: e.EvoID,
				FormID:    e.FormID,
				CandyCost: e.CandyCost,
			}
		}

		baseTempEvos := make([]TempEvolution, len(poke.TempEvolutions))
		for i, te := range poke.TempEvolutions {
			baseTempEvos[i] = TempEvolution{
				TempEvoID: te.TempEvoID,
				Attack:    te.Attack,
				Defense:   te.Defense,
				Stamina:   te.Stamina,
				Types:     te.Types,
			}
		}

		baseMon := &Monster{
			PokemonID:      poke.PokedexID,
			FormID:         0,
			Types:          poke.Types,
			Attack:         poke.Attack,
			Defense:        poke.Defense,
			Stamina:        poke.Stamina,
			GenID:          poke.GenID,
			Family:         poke.Family,
			Legendary:      poke.Legendary,
			Mythic:         poke.Mythic,
			UltraBeast:     poke.UltraBeast,
			Evolutions:     baseEvolutions,
			TempEvolutions: baseTempEvos,
		}
		monsters[MonsterKey{poke.PokedexID, 0}] = baseMon

		// Create entries for each form
		for _, formID := range poke.Forms {
			formKey := strconv.Itoa(formID)
			form, ok := rawForms[formKey]
			if !ok {
				// Form not in forms dict — create entry with base data
				monsters[MonsterKey{poke.PokedexID, formID}] = &Monster{
					PokemonID:      poke.PokedexID,
					FormID:         formID,
					Types:          poke.Types,
					Attack:         poke.Attack,
					Defense:        poke.Defense,
					Stamina:        poke.Stamina,
					GenID:          poke.GenID,
					Family:         poke.Family,
					Legendary:      poke.Legendary,
					Mythic:         poke.Mythic,
					UltraBeast:     poke.UltraBeast,
					Evolutions:     baseEvolutions,
					TempEvolutions: baseTempEvos,
				}
				continue
			}

			// Apply form overrides
			types := poke.Types
			if len(form.Types) > 0 {
				types = form.Types
			}
			attack, defense, stamina := poke.Attack, poke.Defense, poke.Stamina
			if form.Attack > 0 {
				attack = form.Attack
			}
			if form.Defense > 0 {
				defense = form.Defense
			}
			if form.Stamina > 0 {
				stamina = form.Stamina
			}

			evos := baseEvolutions
			if len(form.Evolutions) > 0 {
				evos = make([]Evolution, len(form.Evolutions))
				for i, e := range form.Evolutions {
					evos[i] = Evolution{
						PokemonID: e.EvoID,
						FormID:    e.FormID,
						CandyCost: e.CandyCost,
					}
				}
			}

			tempEvos := baseTempEvos
			if len(form.TempEvolutions) > 0 {
				tempEvos = make([]TempEvolution, len(form.TempEvolutions))
				for i, te := range form.TempEvolutions {
					tempEvos[i] = TempEvolution{
						TempEvoID: te.TempEvoID,
						Attack:    te.Attack,
						Defense:   te.Defense,
						Stamina:   te.Stamina,
						Types:     te.Types,
					}
				}
			}

			monsters[MonsterKey{poke.PokedexID, formID}] = &Monster{
				PokemonID:      poke.PokedexID,
				FormID:         formID,
				Types:          types,
				Attack:         attack,
				Defense:        defense,
				Stamina:        stamina,
				GenID:          poke.GenID,
				Family:         poke.Family,
				Legendary:      poke.Legendary,
				Mythic:         poke.Mythic,
				UltraBeast:     poke.UltraBeast,
				Evolutions:     evos,
				TempEvolutions: tempEvos,
			}
		}
	}

	return monsters, nil
}
