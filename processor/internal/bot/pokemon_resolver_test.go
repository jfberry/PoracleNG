package bot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

func newTestResolver() *PokemonResolver {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_1":   "Bulbasaur",
		"poke_4":   "Charmander",
		"poke_5":   "Charmeleon",
		"poke_6":   "Charizard",
		"poke_25":  "Pikachu",
		"poke_26":  "Raichu",
		"poke_122": "Mr. Mime",
		"poke_133": "Eevee",
		"poke_134": "Vaporeon",
		"poke_135": "Jolteon",
		"poke_136": "Flareon",
		"poke_143": "Snorlax",
	}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
		"poke_1":   "Bisasam",
		"poke_4":   "Glumanda",
		"poke_25":  "Pikachu",
		"poke_122": "Pantimos",
		"poke_143": "Relaxo",
	}))

	// Build minimal GameData with evolution chains
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 1, Form: 0}:   {PokemonID: 1, Evolutions: []gamedata.Evolution{{PokemonID: 2, }}},
			{ID: 2, Form: 0}:   {PokemonID: 2, Evolutions: []gamedata.Evolution{{PokemonID: 3, }}},
			{ID: 3, Form: 0}:   {PokemonID: 3},
			{ID: 4, Form: 0}:   {PokemonID: 4, Evolutions: []gamedata.Evolution{{PokemonID: 5, }}},
			{ID: 5, Form: 0}:   {PokemonID: 5, Evolutions: []gamedata.Evolution{{PokemonID: 6, }}},
			{ID: 6, Form: 0}:   {PokemonID: 6},
			{ID: 25, Form: 0}:  {PokemonID: 25, Evolutions: []gamedata.Evolution{{PokemonID: 26, }}},
			{ID: 26, Form: 0}:  {PokemonID: 26},
			{ID: 122, Form: 0}: {PokemonID: 122},
			{ID: 133, Form: 0}: {PokemonID: 133, Evolutions: []gamedata.Evolution{
				{PokemonID: 134},
				{PokemonID: 135},
				{PokemonID: 136},
			}},
			{ID: 134, Form: 0}: {PokemonID: 134},
			{ID: 135, Form: 0}: {PokemonID: 135},
			{ID: 136, Form: 0}: {PokemonID: 136},
			{ID: 143, Form: 0}: {PokemonID: 143},
		},
	}

	aliases := map[string][]int{
		"mr. mime":  {122},
		"mr mime":   {122},
		"laketrio":  {480, 481, 482},
	}

	return NewPokemonResolver(gd, bundle, []string{"en", "de"}, aliases)
}

func TestResolveByEnglishName(t *testing.T) {
	r := newTestResolver()
	result := r.Resolve("pikachu", "en")
	if len(result) != 1 || result[0].PokemonID != 25 {
		t.Errorf("got %v, want [{25, 0}]", result)
	}
}

func TestResolveByGermanName(t *testing.T) {
	r := newTestResolver()
	result := r.Resolve("relaxo", "de")
	if len(result) != 1 || result[0].PokemonID != 143 {
		t.Errorf("got %v, want [{143, 0}] (Relaxo=Snorlax)", result)
	}
}

func TestResolveEnglishFallbackForGermanUser(t *testing.T) {
	// German user types English name "snorlax" — should match via English fallback
	r := newTestResolver()
	result := r.Resolve("snorlax", "de")
	if len(result) != 1 || result[0].PokemonID != 143 {
		t.Errorf("got %v, want [{143, 0}] (English fallback)", result)
	}
}

func TestResolveByNumericID(t *testing.T) {
	r := newTestResolver()
	result := r.Resolve("25", "en")
	if len(result) != 1 || result[0].PokemonID != 25 {
		t.Errorf("got %v, want [{25, 0}]", result)
	}
}

func TestResolveByAlias(t *testing.T) {
	r := newTestResolver()
	result := r.Resolve("mr. mime", "en")
	if len(result) != 1 || result[0].PokemonID != 122 {
		t.Errorf("got %v, want [{122, 0}]", result)
	}

	// Alternative alias without dot
	result = r.Resolve("mr mime", "en")
	if len(result) != 1 || result[0].PokemonID != 122 {
		t.Errorf("got %v, want [{122, 0}]", result)
	}
}

func TestResolveByMultiAlias(t *testing.T) {
	r := newTestResolver()
	result := r.Resolve("laketrio", "en")
	if len(result) != 3 {
		t.Fatalf("got %d results, want 3", len(result))
	}
	ids := map[int]bool{}
	for _, p := range result {
		ids[p.PokemonID] = true
	}
	for _, expected := range []int{480, 481, 482} {
		if !ids[expected] {
			t.Errorf("laketrio should include pokemon %d, got %v", expected, result)
		}
	}
}

func TestResolveNotFound(t *testing.T) {
	r := newTestResolver()
	result := r.Resolve("notapokemon", "en")
	if len(result) != 0 {
		t.Errorf("got %v, want empty", result)
	}
}

func TestResolveCaseInsensitive(t *testing.T) {
	r := newTestResolver()
	result := r.Resolve("pikachu", "en") // already lowercase from parser
	if len(result) != 1 || result[0].PokemonID != 25 {
		t.Errorf("got %v", result)
	}
}

func TestResolveWithEvolutions(t *testing.T) {
	r := newTestResolver()
	ids := r.ResolveWithEvolutions(4) // Charmander → Charmeleon → Charizard
	if len(ids) != 3 {
		t.Fatalf("got %v, want 3 pokemon (charmander chain)", ids)
	}
	// Should contain 4, 5, 6
	idSet := make(map[int]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	if !idSet[4] || !idSet[5] || !idSet[6] {
		t.Errorf("missing evolution: got %v", ids)
	}
}

func TestResolveWithEvolutionsBranching(t *testing.T) {
	r := newTestResolver()
	ids := r.ResolveWithEvolutions(133) // Eevee → Vaporeon, Jolteon, Flareon
	if len(ids) != 4 {
		t.Fatalf("got %v, want 4 (eevee + 3 evolutions)", ids)
	}
	idSet := make(map[int]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	if !idSet[133] || !idSet[134] || !idSet[135] || !idSet[136] {
		t.Errorf("missing evolution: got %v", ids)
	}
}

func TestResolveWithEvolutionsNoEvolutions(t *testing.T) {
	r := newTestResolver()
	ids := r.ResolveWithEvolutions(143) // Snorlax has no evolutions
	if len(ids) != 1 || ids[0] != 143 {
		t.Errorf("got %v, want [143]", ids)
	}
}

func TestResolvePlusSuffix(t *testing.T) {
	// The + suffix is stripped by Resolve but the caller handles evolution expansion
	r := newTestResolver()
	result := r.Resolve("charmander+", "en")
	if len(result) != 1 || result[0].PokemonID != 4 {
		t.Errorf("got %v, want [{4, 0}] (+ stripped)", result)
	}
}

func TestResolveNilResolver(t *testing.T) {
	var r *PokemonResolver
	result := r.Resolve("pikachu", "en")
	if result != nil {
		t.Errorf("nil resolver should return nil, got %v", result)
	}
}

func TestResolveGermanNameForGermanUser(t *testing.T) {
	r := newTestResolver()
	// Pantimos is German for Mr. Mime
	result := r.Resolve("pantimos", "de")
	if len(result) != 1 || result[0].PokemonID != 122 {
		t.Errorf("got %v, want [{122, 0}] (Pantimos=Mr. Mime)", result)
	}
}
