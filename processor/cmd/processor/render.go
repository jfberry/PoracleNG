package main

import (
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// tileGate decouples tile resolution from the render worker. Every webhook
// handler that has a *staticmap.TilePending wraps it in a gate via
// ProcessorService.newTileGate; that helper spawns one goroutine which
// mutates the Enrichment map via TilePending.Apply, stores the resulting
// bytes in `bytes`, and closes `ready`. The render worker waits on ready
// before rendering, then copies bytes into its own job.TileImageData.
//
// Two reasons this lives between dispatch and render:
//
//   - Multi-job dispatches (dispatchPokemonChangeRender) emit several
//     RenderJobs that share the same Enrichment map. If the tile work
//     ran in one render worker via TilePending.Apply, a sibling worker
//     rendering another job would race on the map. With a single
//     goroutine writing before close(ready), chan-close happens-before
//     makes the map writes visible to every reader after <-ready.
//   - Single-job dispatches use the same pattern for uniformity — the
//     extra goroutine is bounded by the tile Deadline and adds ~negligible
//     overhead, but keeps every render worker on a single code path.
type tileGate struct {
	ready chan struct{}
	bytes []byte
}

// newTileGate spawns a goroutine that resolves the supplied TilePending and
// returns the gate to attach to a RenderJob. Returns nil if pending is nil,
// so call sites can write `TileGate: ps.newTileGate(tilePending)` without a
// nil check around the assignment.
//
// Queue-pressure backoff samples len(renderCh)/cap(renderCh) at gate-start
// time rather than at render time; the heuristic is approximate either way.
func (ps *ProcessorService) newTileGate(pending *staticmap.TilePending) *tileGate {
	if pending == nil {
		return nil
	}
	gate := &tileGate{ready: make(chan struct{})}
	queueLen, queueCap := len(ps.renderCh), cap(ps.renderCh)
	go func() {
		defer close(gate.ready)
		gate.bytes = ps.resolveTilePending(pending, queueLen, queueCap)
	}()
	return gate
}

// RenderJob holds everything needed to render DTS templates and deliver the
// resulting messages. Webhook handlers enqueue these into ps.renderCh so the
// I/O-heavy work happens off the worker goroutine. Tile resolution is
// handled by a TileGate (see newTileGate) so a single goroutine writes the
// staticMap field of Enrichment, never the render worker itself.
type RenderJob struct {
	// AlertType is the source webhook type (raid, egg, pokemon, etc.) —
	// distinct from TemplateType, which is the DTS template chosen to render
	// the message. Used by processRenderJob to populate delivery.Job.MsgType,
	// which the raid partition's first-visible check compares against to
	// determine whether a prior tracked message is of the same alert type.
	AlertType         string
	TemplateType      string
	Enrichment        map[string]any
	PerLangEnrichment map[string]map[string]any
	PerUserEnrichment map[string]map[string]any
	WebhookFields     map[string]any
	MatchedUsers      []webhook.MatchedUser
	MatchedAreas      []webhook.MatchedArea
	TileGate          *tileGate
	IsEncountered    bool // pokemon only
	IsPokemon        bool // true = RenderPokemon, false = RenderAlert
	LogReference     string
	EditKey          string
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
	ChangeType string
	// OverrideCleanTTH, if non-zero, overrides the map-derived TTH for this
	// job's delivery jobs. Used by rsvpChanges render jobs to clean at the
	// latest RSVP timeslot rather than raid.End. The value is a Unix timestamp
	// in seconds; the render pool converts it to a delivery.TTH in Task B.
	// Zero means "use the default" (map-derived from enrichment).
	OverrideCleanTTH int64
	TileImageData    []byte // inline tile bytes, set during tile resolution
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

// processRenderJob waits on the tile gate (if any), renders DTS templates,
// and delivers the resulting messages via the dispatcher.
func (ps *ProcessorService) processRenderJob(job RenderJob) {
	// 1. Wait for tile resolution. The gate's goroutine has already
	//    mutated job.Enrichment via Apply and stored the inline bytes;
	//    chan-close happens-before makes both visible after <-ready.
	if job.TileGate != nil {
		<-job.TileGate.ready
		job.TileImageData = job.TileGate.bytes
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
			var tth delivery.TTH
			if job.OverrideCleanTTH != 0 {
				tth = tthFromUnix(job.OverrideCleanTTH)
			} else {
				tth = tthFromMap(j.TTH)
			}
			ps.dispatcher.Dispatch(&delivery.Job{
				Target:        j.Target,
				Type:          j.Type,
				Message:       j.Message,
				TTH:           tth,
				Clean:         j.Clean,
				Name:          j.Name,
				LogReference:  j.LogReference,
				Lat:           parseCoordFloat(j.Lat),
				Lon:           parseCoordFloat(j.Lon),
				EditKey:       j.EditKey,
				ReplyKey:      job.ReplyKey,
				MsgType:       job.AlertType,
				StaticMapData: job.TileImageData,
				Language:      j.Language,
				SnapshotData:  ps.buildSnapshot(job, j, tth),
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

// tthFromUnix converts a Unix-seconds timestamp into a delivery.TTH using
// geo.ComputeTTH so the arithmetic is consistent with the enrichment layer.
func tthFromUnix(targetUnix int64) delivery.TTH {
	g := geo.ComputeTTH(targetUnix)
	return delivery.TTH{
		Days:    g.Days,
		Hours:   g.Hours,
		Minutes: g.Minutes,
		Seconds: g.Seconds,
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

// buildSnapshot constructs the per-delivery Snapshot for a DeliveryJob, or
// returns nil when the snapshot store isn't enabled. The MessageID is left
// empty — the dispatcher's sender fills it in from the resolved SentMessage
// after the platform API call succeeds. CreatedAt is also deferred (set on
// write) so re-renders for an edit see a fresh timestamp.
//
// Phase 1 of the buttons-and-snapshots work intentionally leaves
// TrackingUIDs and View unpopulated — Phase 3 wires them when buttons need
// them (snapshot.View for response template rendering, TrackingUIDs for
// scope=tracking action handlers). The Snapshot.Version field flags the
// schema so future readers know what they're getting.
func (ps *ProcessorService) buildSnapshot(rj RenderJob, dj webhook.DeliveryJob, tth delivery.TTH) *snapshots.Snapshot {
	if ps.snapshotStore == nil {
		return nil
	}
	now := time.Now()
	expires := now.Add(tth.Duration()).Unix()

	areas := make([]string, 0, len(rj.MatchedAreas))
	for _, a := range rj.MatchedAreas {
		areas = append(areas, a.Name)
	}

	return &snapshots.Snapshot{
		Target:       dj.Target,
		TargetType:   snapshotTargetType(dj.Type),
		ExpiresAt:    expires,
		AlertType:    rj.AlertType,
		TemplateType: rj.TemplateType,
		// TemplateRequested / TemplateSelected: see the Phase 1 note above.
		// Until the renderer surfaces the resolved entry id, both are left
		// empty. Phase 3 will route them through.
		Language:     dj.Language,
		Platform:     delivery.PlatformFromType(dj.Type),
		MatchedAreas: areas,
		// TrackingUIDs and View: Phase 3 fills these in. See buildSnapshot
		// docstring for the rationale.
	}
}

// snapshotTargetType maps a delivery.Job.Type ("discord:user", etc.) to the
// short noun used in Snapshot.TargetType ("dm" / "channel" / "webhook").
func snapshotTargetType(jobType string) string {
	switch jobType {
	case "discord:user", "telegram:user":
		return "dm"
	case "discord:channel", "discord:thread", "telegram:group", "telegram:channel":
		return "channel"
	case "webhook":
		return "webhook"
	default:
		return ""
	}
}
