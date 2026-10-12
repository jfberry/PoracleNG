// Package personalshiny predicts per-player shinies and event backgrounds
// for pokemon webhooks. It is a copy of Golbat's personal shiny scan
// (UnownHash/Golbat#423); see SYNC.md for the file mapping, the pinned
// upstream commit and the list of intentional differences. Keep Golbat's
// names, comments and control flow so the two can be diffed.
package personalshiny

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/geo"
)

const shinyScale = 1 << 24
const defaultShinyCutoff = shinyScale / 512

type shinyPrediction struct {
	Probability float64 `json:"probability"`
	Supported   bool    `json:"supported"`
}

// cutoff converts a probability to a threshold on a 24-bit roll. Invalid
// probabilities use fallback: 1/512 for shiny, never for backgrounds.
func (p shinyPrediction) cutoff(fallback uint32) uint32 {
	if !p.Supported {
		return 0
	}
	if math.IsNaN(p.Probability) || p.Probability < 0 || p.Probability > 1 {
		return fallback
	}
	return uint32(math.Ceil(p.Probability * shinyScale))
}

type shinyVariant struct{ Pokemon, Form, Costume int }
type shinyRate struct {
	Pokemon             int                        `json:"pokemon"`
	Form                int                        `json:"form"`
	Costume             int                        `json:"costume"`
	Prediction          shinyPrediction            `json:"prediction"`
	Overrides           map[string]shinyPrediction `json:"overrides"`
	Background          *shinyPrediction           `json:"background"`
	BackgroundOverrides map[string]shinyPrediction `json:"background_overrides"`
}
type shinyRates struct {
	variants          map[shinyVariant]shinyRate
	maximum           map[int16]uint32
	backgroundMaximum map[int16]uint32
}

// Status is a point-in-time view of the feed for admin status output.
// Poracle: Golbat only logs refresh outcomes.
type Status struct {
	Configured  bool
	Loaded      bool
	Variants    int
	LastSuccess time.Time
	LastAttempt time.Time
	LastError   string
}

// Rates owns the current rate snapshot and its refresher.
// Poracle: Golbat keeps the snapshot in the package global
// shinyRatesSnapshot and reads config.Config.ShinyRates; Poracle keeps both
// on this struct so the processor and tests own an instance.
type Rates struct {
	url, token string
	// Scans read the current snapshot without locking; only the refresher swaps it.
	snapshot atomic.Pointer[shinyRates]
	// zone resolves the IANA zone of a spawn. Poracle: Golbat calls
	// tz.SearchTimezone (tzf full finder); Poracle uses geo.TimezoneName
	// (tzf default finder). They can disagree within ~111 m of a zone
	// border; accepted, see SYNC.md.
	zone   func(lat, lon float64) string
	status atomic.Pointer[Status]
	cancel context.CancelFunc
	done   chan struct{}
}

// NewRates returns an unstarted feed. An empty url means the feature is off.
func NewRates(url, token string) *Rates {
	r := &Rates{url: url, token: token, zone: geo.TimezoneName}
	r.status.Store(&Status{Configured: url != ""})
	return r
}

// Loaded reports whether a snapshot has ever been loaded.
func (r *Rates) Loaded() bool {
	return r != nil && r.snapshot.Load() != nil
}

// Status returns the feed's current state. Nil-safe.
func (r *Rates) Status() Status {
	if r == nil {
		return Status{}
	}
	st := *r.status.Load()
	if snap := r.snapshot.Load(); snap != nil {
		st.Loaded = true
		st.Variants = len(snap.variants)
	}
	return st
}

// Start refreshes rates in the background every five minutes, or 30 seconds
// after a failure. A failed refresh keeps the last snapshot.
// Poracle: Golbat's StartShinyRates; a method, with Stop to wait for exit.
func (r *Rates) Start(ctx context.Context) {
	if r == nil || r.url == "" {
		return
	}
	ctx, r.cancel = context.WithCancel(ctx)
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		for {
			wait := 5 * time.Minute
			if err := r.refreshShinyRates(ctx); err != nil {
				wait = 30 * time.Second
				if r.snapshot.Load() == nil {
					// Poracle: "personal shiny alerts" for Golbat's "personal scans".
					log.Warnf("Shiny rates have not loaded yet; personal shiny alerts are unavailable: %v", err)
				} else {
					log.Warnf("Shiny rate refresh failed; retaining cached rates: %v", err)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
}

// Stop ends the refresher and waits for it. Poracle only.
func (r *Rates) Stop() {
	if r == nil || r.cancel == nil {
		return
	}
	r.cancel()
	<-r.done
}

func (r *Rates) refreshShinyRates(ctx context.Context) (err error) {
	// Poracle: record the outcome for Status.
	attempt := time.Now()
	defer func() {
		st := *r.status.Load()
		st.LastAttempt = attempt
		if err != nil {
			st.LastError = err.Error()
		} else {
			st.LastError = ""
			st.LastSuccess = attempt
		}
		r.status.Store(&st)
	}()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.url, "/")+"/v1/rates?compact=1", nil)
	if err != nil {
		return err
	}
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", res.StatusCode)
	}
	var feed struct {
		Rates *[]shinyRate `json:"rates"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&feed); err != nil {
		return err
	}
	// {} , null or an error object would otherwise replace good rates with none.
	if feed.Rates == nil {
		return fmt.Errorf("rates response has no rates list")
	}
	next := &shinyRates{variants: make(map[shinyVariant]shinyRate), maximum: make(map[int16]uint32), backgroundMaximum: make(map[int16]uint32)}
	for _, rt := range *feed.Rates {
		next.variants[shinyVariant{rt.Pokemon, rt.Form, rt.Costume}] = rt
		// Unknown costumes/forms still default to 1/512 shiny and no background.
		// The per-species maxima are a cheap rejection before the exact variant.
		bound := max(defaultShinyCutoff, rt.Prediction.cutoff(defaultShinyCutoff))
		for _, p := range rt.Overrides {
			bound = max(bound, p.cutoff(defaultShinyCutoff))
		}
		next.maximum[int16(rt.Pokemon)] = max(next.maximum[int16(rt.Pokemon)], bound)
		background := uint32(0)
		if rt.Background != nil {
			background = rt.Background.cutoff(0)
		}
		for _, p := range rt.BackgroundOverrides {
			background = max(background, p.cutoff(0))
		}
		next.backgroundMaximum[int16(rt.Pokemon)] = max(next.backgroundMaximum[int16(rt.Pokemon)], background)
	}
	r.snapshot.Store(next)
	return nil
}
