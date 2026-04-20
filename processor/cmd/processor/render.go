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
	IsEncountered     bool   // pokemon only
	IsPokemon         bool   // true = RenderPokemon, false = RenderAlert
	LogReference      string
	EditKey           string
	TileImageData     []byte // inline tile bytes, set during tile resolution
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
	// 1. Resolve tile with queue-pressure awareness.
	if job.TilePending != nil {
		queueLen := len(ps.renderCh)
		queueCap := cap(ps.renderCh)
		if queueCap > 0 && float64(queueLen)/float64(queueCap) > 0.8 {
			// Queue is under pressure — skip waiting for tile.
			switch {
			case job.TilePending.Both:
				// Drain both channels so the tile worker doesn't block; apply fallback URL.
				go func() { <-job.TilePending.ResultImg }()
				job.TilePending.Apply(job.TilePending.Fallback)
			case job.TilePending.Inline:
				// Don't set "inline" marker without image bytes — the template
				// would render "inline" as the image URL. Drain the channel in
				// the background so the tile worker doesn't block.
				go func() { <-job.TilePending.ResultImg }()
			default:
				job.TilePending.Apply(job.TilePending.Fallback)
			}
			metrics.RenderTileSkipped.Inc()
		} else {
			switch {
			case job.TilePending.Both:
				// Wait for the public URL first (embedded in the message).
				select {
				case url := <-job.TilePending.Result:
					if url != "" {
						job.TilePending.Apply(url)
					} else {
						job.TilePending.Apply(job.TilePending.Fallback)
					}
				case <-time.After(time.Until(job.TilePending.Deadline)):
					job.TilePending.Apply(job.TilePending.Fallback)
					// Drain bytes channel in the background.
					go func() { <-job.TilePending.ResultImg }()
				}
				// Then wait for the bytes (independent deadline). Nil bytes
				// are fine — Discord-upload destinations fall back to URL fetch.
				select {
				case imgData := <-job.TilePending.ResultImg:
					if imgData != nil {
						job.TileImageData = imgData
					}
				case <-time.After(time.Until(job.TilePending.Deadline)):
					// bytes timed out — Discord-upload destinations fall back to URL fetch
				}
			case job.TilePending.Inline:
				select {
				case imgData := <-job.TilePending.ResultImg:
					if imgData != nil {
						job.TilePending.ApplyInline()
						job.TileImageData = imgData
					}
				case <-time.After(time.Until(job.TilePending.Deadline)):
					// no image, proceed without
				}
			default:
				// Wait for tile to resolve or deadline to expire.
				select {
				case url := <-job.TilePending.Result:
					if url != "" {
						job.TilePending.Apply(url)
					} else {
						job.TilePending.Apply(job.TilePending.Fallback)
					}
				case <-time.After(time.Until(job.TilePending.Deadline)):
					job.TilePending.Apply(job.TilePending.Fallback)
				}
			}
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
				StaticMapData: job.TileImageData,
				Language:      j.Language,
			})
		}
	}

	metrics.RenderTotal.WithLabelValues("ok").Inc()
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
