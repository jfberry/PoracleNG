package personalshiny

// adapter.go is Poracle-only: it runs Golbat's annotate logic (see roll.go)
// against one pokemon webhook and the humans matched to it.

import (
	"sort"
	"strconv"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Evaluation is one webhook's resolved variant: built once by Evaluate,
// then asked per account. Not safe for concurrent use; a webhook is matched
// and enriched on one goroutine.
type Evaluation struct {
	encounter        uint64
	ditto            bool // known Ditto: only the Ditto roll applies
	shinyCutoff      uint32
	shinyCapable     bool
	backgroundCutoff uint32
	memo             map[string]playerResult // keyed by player ID
}

type playerResult struct{ shiny, background bool }

// Result lists which of a human's accounts the spawn is shiny / has a
// background for, in the human's account order.
type Result struct {
	Shiny      []db.PlayerAccount
	Background []db.PlayerAccount
}

// Evaluate resolves the webhook's variant, cutoffs and eligibility once. It
// returns nil when the feed has not loaded (fail closed) or the pokemon can
// never be personally shiny or background-eligible.
// The body is the first half of Golbat's shinyScan.annotate: a known Ditto
// takes the Ditto rule, anything else its variant's cutoffs.
func (r *Rates) Evaluate(p *webhook.PokemonWebhook) *Evaluation {
	if r == nil {
		return nil
	}
	snap := r.snapshot.Load()
	if snap == nil {
		return nil
	}
	id, err := strconv.ParseUint(p.EncounterID, 10, 64)
	if err != nil {
		return nil
	}
	personal := personalLookup(p)
	if personal == 0 {
		return nil
	}
	ev := &Evaluation{encounter: id}
	if isKnownDitto(p) {
		// A known Ditto ignores the feed (rates, fallback, unsupportedPokemon).
		ev.ditto = true
		return ev
	}
	s := &shinyScan{rates: snap, zone: r.zone}
	rate := s.rate(p.PokemonID, p.Form, int(personal)-1)
	ev.shinyCapable = rate.capable()
	ev.shinyCutoff = s.shinyCutoff(rate, p.Latitude, p.Longitude)
	ev.backgroundCutoff = s.backgroundCutoff(rate, p.Latitude, p.Longitude)
	return ev
}

// player is the per-player loop body of Golbat's shinyScan.annotate.
func (e *Evaluation) player(id string) playerResult {
	if v, ok := e.memo[id]; ok {
		return v
	}
	var res playerResult
	if e.ditto {
		// A known Ditto is shiny on the third draw at 1/64 and never has a
		// background: no Ditto background has been observed.
		res.shiny = newShinyPlayer(id).dittoRoll(e.encounter) < dittoShinyCutoff
		e.remember(id, res)
		return res
	}
	rng := newShinyPlayer(id).generator(e.encounter)
	if e.shinyCapable && rng.Next(24) < e.shinyCutoff {
		res.shiny = true
	}
	// A background is on offer exactly when its cutoff is nonzero.
	if e.backgroundCutoff > 0 && rng.Next(24) < e.backgroundCutoff {
		res.background = true
	}
	e.remember(id, res)
	return res
}

func (e *Evaluation) remember(id string, res playerResult) {
	if e.memo == nil {
		e.memo = make(map[string]playerResult)
	}
	e.memo[id] = res
}

// For evaluates a human's accounts. Nil-safe: a nil Evaluation hits nothing.
func (e *Evaluation) For(accounts []db.PlayerAccount) Result {
	var out Result
	if e == nil {
		return out
	}
	for _, a := range accounts {
		res := e.player(a.PlayerID)
		if res.shiny {
			out.Shiny = append(out.Shiny, a)
		}
		if res.background {
			out.Background = append(out.Background, a)
		}
	}
	return out
}

// Checker answers personal rule bits for the humans of one state snapshot,
// memoised per human, for one webhook. Not safe for concurrent use.
type Checker struct {
	ev     *Evaluation
	humans map[string]*db.Human
	memo   map[string]Result
}

// NewChecker binds an evaluation (nil when the feed is not loaded or the
// pokemon is ineligible) to the state's humans.
func NewChecker(ev *Evaluation, humans map[string]*db.Human) *Checker {
	return &Checker{ev: ev, humans: humans}
}

// Evaluated reports whether the spawn was evaluated: the feed is loaded and
// the pokemon is eligible. Nil-safe.
func (c *Checker) Evaluated() bool { return c != nil && c.ev != nil }

// Result returns the human's hits; ok is false when the human has no linked
// accounts (or is unknown). A nil evaluation yields an empty Result with ok
// still true for linked humans.
func (c *Checker) Result(humanID string) (Result, bool) {
	if c == nil {
		return Result{}, false
	}
	h := c.humans[humanID]
	if h == nil || len(h.Accounts) == 0 {
		return Result{}, false
	}
	if r, ok := c.memo[humanID]; ok {
		return r, true
	}
	r := c.ev.For(h.Accounts)
	if c.memo == nil {
		c.memo = make(map[string]Result)
	}
	c.memo[humanID] = r
	return r, true
}

// Allows reports whether a rule with these personal bits matches for the
// human. Unknown (reserved) bits never match.
func (c *Checker) Allows(humanID string, bits int) bool {
	if c == nil || c.ev == nil || bits&^db.PersonalKnownMask != 0 {
		return false
	}
	r, ok := c.Result(humanID)
	if !ok {
		return false
	}
	if db.IsPersonalShiny(bits) && len(r.Shiny) == 0 {
		return false
	}
	if db.IsPersonalBackground(bits) && len(r.Background) == 0 {
		return false
	}
	return true
}

// listed returns a variant's cutoffs when the feed decides it: the variant
// is listed, or its species is listed as never shiny.
// Poracle: only for shinyPossible / shinyRate / !info, not for rolls.
// Golbat's fallback rate for other unlisted variants is a roll assumption,
// and as a "shiny possible" badge it would flag unreleased shinies, so they
// report unknown.
//
// Poracle: species 132 reports the fixed Ditto rate (1/64, supported) for
// every form and costume, whatever the feed says, so a Ditto alert shows
// shinyPossible and shinyRate 64 instead of the feed's unsupported entry.
func (s *shinyScan) listed(pokemon, form, costume int) (shinyCutoffs, bool) {
	if pokemon == dittoPokemonId {
		return shinyCutoffs{shiny: dittoShinyCutoff}, true
	}
	if _, ok := s.rates.variants[shinyVariant{pokemon, form, costume}]; !ok && !s.rates.unsupported[int16(pokemon)] {
		return shinyCutoffs{}, false
	}
	return s.rate(pokemon, form, costume), true
}

// ShinyVariant reports the feed's view of a variant at a location. supported
// is the variant's base capability (an override never changes it), and
// probability the cutoff at the location over shinyScale. known is false
// when the feed has not loaded or does not decide the variant (see listed);
// callers then fall back to observed stats.
func (r *Rates) ShinyVariant(pokemonID, form, costume int, lat, lon float64) (supported bool, probability float64, known bool) {
	if r == nil {
		return false, 0, false
	}
	snap := r.snapshot.Load()
	if snap == nil {
		return false, 0, false
	}
	s := &shinyScan{rates: snap, zone: r.zone}
	rate, ok := s.listed(pokemonID, form, costume)
	if !ok {
		return false, 0, false
	}
	return rate.capable(), float64(s.shinyCutoff(rate, lat, lon)) / shinyScale, true
}

// BaseShinyVariant is ShinyVariant without any zone override applied: the
// feed's base rate. (Resolving a fixed point such as (0, 0) is not enough —
// tzf assigns it "Etc/GMT", which an override could in principle name.)
func (r *Rates) BaseShinyVariant(pokemonID, form, costume int) (supported bool, probability float64, known bool) {
	if r == nil {
		return false, 0, false
	}
	snap := r.snapshot.Load()
	if snap == nil {
		return false, 0, false
	}
	s := &shinyScan{rates: snap}
	rate, ok := s.listed(pokemonID, form, costume)
	if !ok {
		return false, 0, false
	}
	return rate.capable(), float64(rate.shiny) / shinyScale, true
}

// ShinyVariantForm is ShinyVariant for callers that may hold form 0 for a
// species whose normal variant has its own form id: when (pokemon, form 0,
// costume) is not listed and defaultForm != 0, it retries with defaultForm.
// Poracle only; the personal roll (Evaluate) never uses this.
func (r *Rates) ShinyVariantForm(pokemonID, form, defaultForm, costume int, lat, lon float64) (supported bool, probability float64, known bool) {
	if supported, probability, known = r.ShinyVariant(pokemonID, form, costume, lat, lon); known || form != 0 || defaultForm == 0 {
		return supported, probability, known
	}
	return r.ShinyVariant(pokemonID, defaultForm, costume, lat, lon)
}

// BaseShinyVariantForm is BaseShinyVariant with the ShinyVariantForm
// default-form fallback.
func (r *Rates) BaseShinyVariantForm(pokemonID, form, defaultForm, costume int) (supported bool, probability float64, known bool) {
	if supported, probability, known = r.BaseShinyVariant(pokemonID, form, costume); known || form != 0 || defaultForm == 0 {
		return supported, probability, known
	}
	return r.BaseShinyVariant(pokemonID, defaultForm, costume)
}

// VariantRate is a variant whose shiny rate is above the 1/512 base.
type VariantRate struct {
	PokemonID, Form, Costume int
	OneIn                    int
}

// Boosted lists the shiny-capable variants whose cutoff at the location is
// above the 1/512 cutoff (an event), sorted by pokemon, form, costume.
func (r *Rates) Boosted(lat, lon float64) []VariantRate {
	if r == nil {
		return nil
	}
	snap := r.snapshot.Load()
	if snap == nil {
		return nil
	}
	s := &shinyScan{rates: snap, zone: r.zone}
	var out []VariantRate
	for v, rate := range snap.variants {
		// 1/512 here is the event threshold, not the feed's fallback.
		if cutoff := s.shinyCutoff(rate, lat, lon); rate.capable() && cutoff > defaultShinyCutoff {
			out = append(out, VariantRate{PokemonID: v.Pokemon, Form: v.Form, Costume: v.Costume, OneIn: oneIn(float64(cutoff) / shinyScale)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.PokemonID != b.PokemonID {
			return a.PokemonID < b.PokemonID
		}
		if a.Form != b.Form {
			return a.Form < b.Form
		}
		return a.Costume < b.Costume
	})
	return out
}

// OneIn converts a probability to the nearest "1 in N" (0 for p <= 0).
func OneIn(p float64) int { return oneIn(p) }

func oneIn(p float64) int {
	if p <= 0 {
		return 0
	}
	return int(1/p + 0.5)
}

// DisplayName is how an account is shown to users: its name (the Label field), else the
// player ID shortened to "…" plus its last four characters.
func DisplayName(a db.PlayerAccount) string {
	if a.Label != "" {
		return a.Label
	}
	runes := []rune(a.PlayerID)
	if len(runes) <= 4 {
		return a.PlayerID
	}
	return "…" + string(runes[len(runes)-4:])
}
