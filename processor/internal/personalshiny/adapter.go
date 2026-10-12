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
// The body is the first half of Golbat's shinyScan.annotate.
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
	s := &shinyScan{rates: snap, zone: r.zone}
	rate, ok := s.rate(p.PokemonID, p.Form, int(personal)-1)
	ev := &Evaluation{encounter: id}
	ev.shinyCutoff, ev.shinyCapable = s.shinyCutoff(rate, ok, p.Latitude, p.Longitude)
	ev.backgroundCutoff = s.backgroundCutoff(rate, ok, p.Latitude, p.Longitude)
	return ev
}

// player is the per-player loop body of Golbat's shinyScan.annotate.
func (e *Evaluation) player(id string) playerResult {
	if v, ok := e.memo[id]; ok {
		return v
	}
	var res playerResult
	rng := newShinyPlayer(id).generator(e.encounter)
	if e.shinyCapable && rng.Next(24) < e.shinyCutoff {
		res.shiny = true
	}
	// A background is on offer exactly when its cutoff is nonzero.
	if e.backgroundCutoff > 0 && rng.Next(24) < e.backgroundCutoff {
		res.background = true
	}
	if e.memo == nil {
		e.memo = make(map[string]playerResult)
	}
	e.memo[id] = res
	return res
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

// prediction resolves a variant's shiny prediction at a location: the
// timezone override when one applies, else the base prediction. Poracle only
// (used for shinyPossible / shinyRate / !info, not for rolls).
func (s *shinyScan) prediction(rate shinyRate, lat, lon float64) shinyPrediction {
	if override, found := rate.Overrides[s.zoneAt(rate.Overrides, lat, lon)]; found {
		return override
	}
	return rate.Prediction
}

// ShinyVariant reports the feed's view of a variant at a location. known is
// false when the feed has not loaded or does not list the variant; callers
// then fall back to observed stats (Golbat's "unknown ⇒ 1/512" is for rolls
// only — as a "shiny possible" badge it would flag unreleased shinies).
func (r *Rates) ShinyVariant(pokemonID, form, costume int, lat, lon float64) (supported bool, probability float64, known bool) {
	if r == nil {
		return false, 0, false
	}
	snap := r.snapshot.Load()
	if snap == nil {
		return false, 0, false
	}
	s := &shinyScan{rates: snap, zone: r.zone}
	rate, ok := s.rate(pokemonID, form, costume)
	if !ok {
		return false, 0, false
	}
	p := s.prediction(rate, lat, lon)
	return p.Supported, p.Probability, true
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
	s := &shinyScan{rates: snap, zone: func(float64, float64) string { return "" }}
	rate, ok := s.rate(pokemonID, form, costume)
	if !ok {
		return false, 0, false
	}
	return rate.Prediction.Supported, rate.Prediction.Probability, true
}

// VariantRate is a variant whose shiny rate is above the 1/512 base.
type VariantRate struct {
	PokemonID, Form, Costume int
	OneIn                    int
}

// Boosted lists the variants whose shiny probability at the location is
// above 1/512 (an event), sorted by pokemon, form, costume.
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
		p := s.prediction(rate, lat, lon)
		if p.Supported && p.Probability > 1.0/512 && p.Probability <= 1 {
			out = append(out, VariantRate{PokemonID: v.Pokemon, Form: v.Form, Costume: v.Costume, OneIn: oneIn(p.Probability)})
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

// DisplayName is how an account is shown to users: its label, else the
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
