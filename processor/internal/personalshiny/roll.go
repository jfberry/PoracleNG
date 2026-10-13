package personalshiny

import (
	"crypto/sha1" //nolint:gosec // the game seeds its generator with SHA-1; this is not a security use
	"encoding/binary"
	"math"

	"github.com/pokemon/poracleng/processor/internal/personalshiny/javarandom"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Cost is candidates x players x ID length, so both are bounded. 256 bytes
// covers every known player ID format with room to spare.
// Poracle: exported, and applied per human (store limits) rather than per
// request.
const MaxPlayers = 50
const MaxPlayerIDBytes = 256

// personalLookup packs roll eligibility and costume into one byte:
// 0 means never personally shiny or background-eligible (Ditto, disguised,
// or seen only as nearby), otherwise costume+1.
// Poracle: Golbat reads its Pokemon record and stores the byte in
// PokemonLookup; Poracle reads the webhook. The webhook has no is_ditto:
// Golbat sends a detected Ditto as pokemon_id 132 with the disguise in
// display_pokemon_id, which the pokemon_id (132) and display checks cover.
// Golbat's costume is a uint8, so out-of-range webhook values are rejected
// with 255.
func personalLookup(p *webhook.PokemonWebhook) uint8 {
	costume := p.Costume
	if p.PokemonID == 132 || costume < 0 || costume >= math.MaxUint8 ||
		p.DisplayPokemonID != 0 && p.DisplayPokemonID != p.PokemonID ||
		p.SeenType != "wild" && p.SeenType != "encounter" {
		return 0
	}
	return uint8(costume) + 1
}

// shinyPlayer is used by one scan goroutine. It remembers only the last
// encounter's rolls, so a player listed in several clauses is hashed at most
// once per candidate, and only if a clause gets that far.
type shinyPlayer struct {
	input                 []byte
	last                  uint64
	lastValid             bool
	lastFirst, lastSecond uint32
}

// newShinyPlayer builds the hash input for a player ID.
// Poracle: Golbat inlines this in ScanPokemonWithShiny's add closure.
func newShinyPlayer(id string) *shinyPlayer {
	input := make([]byte, 8+len(id))
	copy(input[8:], id)
	return &shinyPlayer{input: input}
}

// rolls returns the first two java.util.Random nextFloat draws of the
// player's generator for an encounter, kept as their 24-bit integers. The
// shiny roll is the first; the background is the next draw after it, so the
// first when the variant cannot be shiny (see backgroundRoll). Recomputing
// (one SHA-1 block) is cheaper than a cache.
func (p *shinyPlayer) rolls(encounter uint64) (first, second uint32) {
	if p.lastValid && p.last == encounter {
		return p.lastFirst, p.lastSecond
	}
	rng := p.generator(encounter)
	first, second = rng.Next(24), rng.Next(24)
	p.last, p.lastValid, p.lastFirst, p.lastSecond = encounter, true, first, second
	return first, second
}

// generator is the player's fresh generator for an encounter.
func (p *shinyPlayer) generator(encounter uint64) *javarandom.Random {
	binary.LittleEndian.PutUint64(p.input[:8], encounter)
	digest := sha1.Sum(p.input) //nolint:gosec // see import
	return javarandom.New(binary.LittleEndian.Uint64(digest[:8]))
}

// shinyScan holds what a scan resolves rates against.
// Poracle: Golbat's shinyScan also carries the request ctx and player set;
// Poracle evaluates one webhook at a time (see adapter.go).
type shinyScan struct {
	rates *shinyRates
	zone  func(lat, lon float64) string
}

func (s *shinyScan) rate(pokemon, form, costume int) (shinyRate, bool) {
	rate, ok := s.rates.variants[shinyVariant{pokemon, form, costume}]
	if !ok && s.rates.unsupported[int16(pokemon)] {
		// A species the feed lists as never shiny: no shiny draw is taken,
		// rather than falling back to 1/512.
		return shinyRate{Pokemon: pokemon, Form: form, Costume: costume}, true
	}
	return rate, ok
}

func (s *shinyScan) zoneAt(overrides map[string]shinyPrediction, lat, lon float64) string {
	if len(overrides) == 0 {
		return ""
	}
	// Resolved per spawn: matching bbox corners do not imply the whole box
	// shares a zone (e.g. enclaves), and only variants with overrides pay.
	return s.zone(lat, lon)
}

// shinyCutoff resolves the shiny threshold at a location. capable reports
// whether the variant can be shiny there at all; only then does the game take
// a draw for the shiny roll, which shifts every later draw.
func (s *shinyScan) shinyCutoff(rate shinyRate, ok bool, lat, lon float64) (cutoff uint32, capable bool) {
	if !ok {
		return defaultShinyCutoff, true
	}
	prediction := rate.Prediction
	if override, found := rate.Overrides[s.zoneAt(rate.Overrides, lat, lon)]; found {
		prediction = override
	}
	return prediction.cutoff(defaultShinyCutoff), prediction.Supported
}

func (s *shinyScan) backgroundCutoff(rate shinyRate, ok bool, lat, lon float64) uint32 {
	if !ok {
		return 0
	}
	if override, found := rate.BackgroundOverrides[s.zoneAt(rate.BackgroundOverrides, lat, lon)]; found {
		return override.cutoff(0)
	}
	if rate.Background == nil {
		return 0
	}
	return rate.Background.cutoff(0)
}

// backgroundRoll is the draw after the shiny roll, which the game takes only
// when the variant can be shiny: the second draw if so, otherwise the first.
func backgroundRoll(shinyCapable bool, player *shinyPlayer, encounter uint64) uint32 {
	first, second := player.rolls(encounter)
	if shinyCapable {
		return second
	}
	return first
}
