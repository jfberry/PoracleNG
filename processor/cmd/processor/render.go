package main

import (
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// tileGate coordinates tile resolution across sibling RenderJobs that share
// the same Enrichment map. The dispatcher spawns one goroutine to resolve
// the tile; that goroutine mutates the shared Enrichment map via Apply,
// stores the resulting bytes in `bytes`, and closes `ready`. Each render
// worker for a job in the batch waits on ready before rendering, then
// copies bytes into its own job.TileImageData.
//
// Without this barrier, multi-job dispatches (e.g. dispatchPokemonChangeRender)
// would race: one render worker calls TilePending.Apply (writes to the shared
// map) while a sibling worker concurrently reads it via LayeredView. Channel
// close establishes happens-before for the map writes, so readers after
// <-ready see a consistent map.
type tileGate struct {
	ready chan struct{}
	bytes []byte
}

// RenderJob holds everything needed to resolve a tile, render DTS templates,
// and deliver the resulting messages. Webhook handlers enqueue these into
// ps.renderCh so the I/O-heavy work happens off the worker goroutine.
type RenderJob struct {
	TemplateType      string
	Enrichment        map[string]any
	PerLangEnrichment map[string]map[string]any
	PerUserEnrichment map[string]map[string]any
	WebhookFields     map[string]any
	MatchedUsers      []webhook.MatchedUser
	MatchedAreas      []webhook.MatchedArea
	TilePending       *staticmap.TilePending
	// TileGate, when set, replaces TilePending for batched multi-job dispatch
	// (see dispatchPokemonChangeRender). The render worker waits on the gate
	// instead of resolving the tile itself. TileGate and TilePending are
	// mutually exclusive — set one or the other, never both.
	TileGate      *tileGate
	IsEncountered bool // pokemon only
	IsPokemon     bool // true = RenderPokemon, false = RenderAlert
	LogReference  string
	EditKey       string
	// ReplyKey indexes the sent message for reply chaining. Copied verbatim
	// onto every constructed delivery.Job. For pokemon, this is the encounter
	// ID so subsequent change events can find prior messages via the
	// MessageTracker reply index.
	ReplyKey string
	// IsChange routes the job through RenderPokemonChanged instead of
	// RenderPokemon. Only meaningful when IsPokemon is true.
	IsChange bool
	// OriginalView is the {{original.X}} field bag (built via
	// dts.BuildOriginalView). Threaded into the LayeredView for
	// monsterChanged templates. Nil for non-change renders.
	OriginalView map[string]any
	// ChangeType is a human-readable label for the change dimension
	// (species/form/gender/encountered/weather_boost). Currently used for
	// logging only. Empty for non-change renders.
	ChangeType    string
	TileImageData []byte // inline tile bytes, set during tile resolution
}

// renderWorker processes render jobs from the shared channel until it is closed.
func (ps *ProcessorService) renderWorker() {
	defer ps.renderWg.Done()
	for job := range ps.renderCh {
		start := time.Now()
		ps.processRenderJob(job)
		metrics.RenderDuration.Observe(time.Since(start).Seconds())
	}
}

// processRenderJob resolves the pending tile (if any), renders DTS templates,
// and delivers the resulting messages via the dispatcher.
func (ps *ProcessorService) processRenderJob(job RenderJob) {
	// 1. Resolve tile.
	switch {
	case job.TileGate != nil:
		// Batched dispatch: one goroutine is resolving the tile and will close
		// ready when the shared Enrichment map has been updated. Wait, then
		// pick up the bytes for our local TileImageData.
		<-job.TileGate.ready
		job.TileImageData = job.TileGate.bytes
	case job.TilePending != nil:
		// Single-job path: resolve inline.
		bytes := ps.resolveTilePending(job.TilePending, len(ps.renderCh), cap(ps.renderCh))
		if bytes != nil {
			job.TileImageData = bytes
		}
	}

	// 2. Render templates.
	var jobs []webhook.DeliveryJob
	if ps.dtsRenderer == nil {
		log.Warnf("[%s] DTS renderer not available, skipping render", job.LogReference)
		metrics.RenderTotal.WithLabelValues("error").Inc()
		return
	}

	if job.IsPokemon {
		if job.IsChange {
			jobs = ps.dtsRenderer.RenderPokemonChanged(
				job.Enrichment,
				job.PerLangEnrichment,
				job.PerUserEnrichment,
				job.WebhookFields,
				job.OriginalView,
				job.MatchedUsers,
				job.MatchedAreas,
				job.LogReference,
				job.EditKey,
			)
		} else {
			jobs = ps.dtsRenderer.RenderPokemon(
				job.Enrichment,
				job.PerLangEnrichment,
				job.PerUserEnrichment,
				job.WebhookFields,
				job.MatchedUsers,
				job.MatchedAreas,
				job.IsEncountered,
				job.LogReference,
				job.EditKey,
			)
		}
	} else {
		jobs = ps.dtsRenderer.RenderAlert(
			job.TemplateType,
			job.Enrichment,
			job.PerLangEnrichment,
			job.WebhookFields,
			job.MatchedUsers,
			job.MatchedAreas,
			job.LogReference,
			job.EditKey,
		)
	}

	// 3. Deliver rendered messages.
	if len(jobs) > 0 {
		if ps.dispatcher == nil {
			log.Warnf("[%s] Delivery dispatcher not configured, dropping %d messages", job.LogReference, len(jobs))
			metrics.RenderTotal.WithLabelValues("error").Inc()
			return
		}
		for _, j := range jobs {
			ps.dispatcher.Dispatch(&delivery.Job{
				Target:        j.Target,
				Type:          j.Type,
				Message:       j.Message,
				TTH:           tthFromMap(j.TTH),
				Clean:         j.Clean,
				Name:          j.Name,
				LogReference:  j.LogReference,
				Lat:           parseCoordFloat(j.Lat),
				Lon:           parseCoordFloat(j.Lon),
				EditKey:       j.EditKey,
				ReplyKey:      job.ReplyKey,
				StaticMapData: job.TileImageData,
				Language:      j.Language,
			})
		}
	}

	metrics.RenderTotal.WithLabelValues("ok").Inc()
}

// resolveTilePending performs the synchronous tile wait that processRenderJob
// previously did inline. It mutates pending.target (the shared Enrichment map)
// via Apply / ApplyInline and returns the resulting tile bytes (nil if the
// mode doesn't produce bytes, the bytes channel timed out, or the result was
// nil). queueLen/queueCap are passed in so the function can apply the same
// queue-pressure back-off as before — the caller samples them at the point
// of dispatch (which is close enough to render time for the heuristic).
//
// Safe to call from a goroutine spawned by the dispatcher (see tileGate).
func (ps *ProcessorService) resolveTilePending(pending *staticmap.TilePending, queueLen, queueCap int) []byte {
	if queueCap > 0 && float64(queueLen)/float64(queueCap) > 0.8 {
		// Queue is under pressure — skip waiting for tile.
		switch {
		case pending.Both:
			// Drain both channels so the tile worker doesn't block; apply fallback URL.
			go func() { <-pending.ResultImg }()
			pending.Apply(pending.Fallback)
		case pending.Inline:
			// Don't set "inline" marker without image bytes — the template
			// would render "inline" as the image URL. Drain the channel in
			// the background so the tile worker doesn't block.
			go func() { <-pending.ResultImg }()
		default:
			pending.Apply(pending.Fallback)
		}
		metrics.RenderTileSkipped.Inc()
		return nil
	}

	switch {
	case pending.Both:
		// Wait for the public URL first (embedded in the message).
		select {
		case url := <-pending.Result:
			if url != "" {
				pending.Apply(url)
			} else {
				pending.Apply(pending.Fallback)
			}
		case <-time.After(time.Until(pending.Deadline)):
			pending.Apply(pending.Fallback)
			// Drain bytes channel in the background.
			go func() { <-pending.ResultImg }()
			return nil
		}
		// Then wait for the bytes (independent deadline). Nil bytes
		// are fine — Discord-upload destinations fall back to URL fetch.
		select {
		case imgData := <-pending.ResultImg:
			return imgData
		case <-time.After(time.Until(pending.Deadline)):
			// bytes timed out — Discord-upload destinations fall back to URL fetch
			return nil
		}
	case pending.Inline:
		select {
		case imgData := <-pending.ResultImg:
			if imgData != nil {
				pending.ApplyInline()
				return imgData
			}
			return nil
		case <-time.After(time.Until(pending.Deadline)):
			// no image, proceed without
			return nil
		}
	default:
		// Wait for tile to resolve or deadline to expire.
		select {
		case url := <-pending.Result:
			if url != "" {
				pending.Apply(url)
			} else {
				pending.Apply(pending.Fallback)
			}
		case <-time.After(time.Until(pending.Deadline)):
			pending.Apply(pending.Fallback)
		}
		return nil
	}
}

func tthFromMap(m map[string]any) delivery.TTH {
	return delivery.TTH{
		Days:    intFromAny(m["days"]),
		Hours:   intFromAny(m["hours"]),
		Minutes: intFromAny(m["minutes"]),
		Seconds: intFromAny(m["seconds"]),
	}
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

func parseCoordFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
