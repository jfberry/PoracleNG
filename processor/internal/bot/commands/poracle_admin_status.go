package commands

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
	processor "github.com/pokemon/poracleng/processor"
)

// Hard-coded indicator thresholds for the status snapshot. A
// [processor.status] config section is deferred to a later task; these
// defaults match thresholds already used elsewhere in the processor.
const (
	// renderQueueWarnPercent matches the existing tile-skip threshold
	// in render.go (80% full → start skipping tile generation).
	renderQueueWarnPercent = 80
	// deliveryQueueWarnPending paints the platform queue 🟡 once
	// pending jobs exceed this threshold.
	deliveryQueueWarnPending = 100
	// summaryBufferWarn paints the summary buffer 🟡 once total
	// buffered entries exceed this threshold.
	summaryBufferWarn = 100
)

// status indicator emoji (also used as the only place these strings appear).
const (
	indicatorGreen  = "🟢"
	indicatorYellow = "🟡"
	indicatorRed    = "🔴"
)

// statusReport returns the rendered health snapshot. The verbose flag
// adds per-route Discord detail, per-handler webhook breakdown, and
// full queue contents.
//
// The returned slice is always non-empty. Callers may pass it through
// directly or hand to SplitTextReply for long output.
func statusReport(ctx *bot.CommandContext, verbose bool) []bot.Reply {
	tr := ctx.Tr()

	var sb strings.Builder

	// Section 1: Header.
	sb.WriteString(tr.T("cmd.poracle_admin.status.header"))
	sb.WriteString("\n")
	sb.WriteString(formatHeaderTimestamp(time.Now()))

	// Section 2 (placed early for visibility): Maintenance banner.
	if banner := renderPausedBanner(ctx, tr); banner != "" {
		sb.WriteString("\n\n")
		sb.WriteString(banner)
	}

	// Section 3: Build / uptime.
	sb.WriteString("\n\n")
	sb.WriteString(renderBuildSection(ctx, tr))

	// Section 4: Webhooks.
	sb.WriteString("\n\n")
	sb.WriteString(renderWebhooksSection(ctx, tr, verbose))

	// Section 5: Render queue. The render channel is not exposed via
	// ctx today (would require a new introspection accessor that Phase
	// 2 explicitly closed). Render "n/a" with a TODO.
	sb.WriteString("\n\n")
	sb.WriteString(renderRenderQueueSection(ctx, tr))

	// Section 6: Delivery queue.
	sb.WriteString("\n\n")
	sb.WriteString(renderDeliverySection(ctx, tr, verbose))

	// Section 7: Discord rate.
	sb.WriteString("\n\n")
	sb.WriteString(renderDiscordRateSection(ctx, tr, verbose))

	// Section 8: Telegram rate.
	sb.WriteString("\n\n")
	sb.WriteString(renderTelegramRateSection(ctx, tr))

	// Section 9: Alert limits.
	sb.WriteString("\n\n")
	sb.WriteString(renderAlertLimitsSection(ctx, tr))

	// Section 10: Summary buffer.
	sb.WriteString("\n\n")
	sb.WriteString(renderSummaryBufferSection(ctx, tr))

	// Section 11: Tracking counts.
	sb.WriteString("\n\n")
	sb.WriteString(renderTrackingSection(ctx, tr))

	// Section 12: MySQL.
	sb.WriteString("\n\n")
	sb.WriteString(renderMySQLSection(ctx, tr))

	return bot.SplitTextReply(sb.String())
}

// formatHeaderTimestamp returns the timestamp line. Uses RFC3339-style
// UTC formatting — operators get a consistent value regardless of the
// processor host's timezone, which is what an operations snapshot
// wants. No i18n on the format string itself; the surrounding label
// is localised.
func formatHeaderTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}

// renderPausedBanner returns the empty string when delivery is not
// paused (or when no dispatcher is available). Otherwise the formatted
// 🔴 PAUSED banner.
func renderPausedBanner(ctx *bot.CommandContext, tr translator) string {
	if ctx.Admin == nil || ctx.Admin.Dispatcher == nil {
		return ""
	}
	paused, reason, since := ctx.Admin.Dispatcher.PauseState()
	if !paused {
		return ""
	}
	if reason == "" {
		reason = tr.T("cmd.poracle_admin.status.value.none")
	}
	d := time.Since(since).Round(time.Second)
	return tr.Tf("cmd.poracle_admin.status.paused_banner", reason, formatDuration(d))
}

// renderBuildSection lists version, commit, build date, uptime.
func renderBuildSection(ctx *bot.CommandContext, tr translator) string {
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.status.section.build"))

	version := processor.Version
	commit, date := "unknown", "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) > 7 {
					commit = s.Value[:7]
				} else if s.Value != "" {
					commit = s.Value
				}
			case "vcs.time":
				if s.Value != "" {
					date = s.Value
				}
			}
		}
	}

	sb.WriteString("\n  ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.version", version, commit, date))

	uptime := tr.T("cmd.poracle_admin.status.value.na")
	if ctx.Admin != nil && !ctx.Admin.ProcessStart.IsZero() {
		uptime = formatDuration(time.Since(ctx.Admin.ProcessStart))
	}
	sb.WriteString("\n  ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.uptime", uptime))

	return sb.String()
}

// renderWebhooksSection renders the webhook arrival rate section.
func renderWebhooksSection(ctx *bot.CommandContext, tr translator, verbose bool) string {
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.status.section.webhooks"))

	if ctx.Admin == nil || ctx.Admin.WebhookRate == nil {
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.value.na"))
		return sb.String()
	}
	snap := ctx.Admin.WebhookRate()

	// Indicator selection.
	indicator := indicatorGreen
	if snap.Per5Min == 0 && snap.Per60Min > 0 {
		indicator = indicatorRed
	} else if snap.Per5Min == 0 && snap.Per60Min == 0 {
		// No traffic at all — neutral, not necessarily alarming.
		indicator = indicatorYellow
	}

	// Per-minute totals over the 5/15/60 windows. Divide by window
	// length to express as messages-per-minute.
	per5 := float64(snap.Per5Min) / 5.0
	per15 := float64(snap.Per15Min) / 15.0
	per60 := float64(snap.Per60Min) / 60.0
	sb.WriteString("\n  ")
	sb.WriteString(indicator)
	sb.WriteString(" ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.webhooks_rate",
		fmt.Sprintf("%.1f", per5),
		fmt.Sprintf("%.1f", per15),
		fmt.Sprintf("%.1f", per60),
	))

	// Per-type breakdown.
	if len(snap.PerType) > 0 {
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.label.webhooks_by_type"))
		entries := sortByCountDesc(snap.PerType)
		limit := len(entries)
		if !verbose && limit > 5 {
			limit = 5
		}
		for i := 0; i < limit; i++ {
			sb.WriteString("\n    ")
			sb.WriteString(fmt.Sprintf("%s: %d", entries[i].name, entries[i].count))
		}
	}

	return sb.String()
}

// renderRenderQueueSection renders the render-queue depth section.
// The render channel itself is owned by ProcessorService and not (yet)
// exposed through CommandContext/BotDeps; Phase 2 introspection is
// closed, so we render "n/a".
//
// TODO(slash-commands): When/if a render-queue accessor is added to
// BotDeps (e.g. RenderQueueDepth / RenderQueueCapacity closures), wire
// it here and apply renderQueueWarnPercent for the 🟡 indicator.
func renderRenderQueueSection(ctx *bot.CommandContext, tr translator) string {
	_ = ctx
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.status.section.render_queue"))
	sb.WriteString("\n  ")
	sb.WriteString(tr.T("cmd.poracle_admin.status.value.na"))
	return sb.String()
}

// renderDeliverySection lists per-platform delivery queue state. The
// underlying senders' worker capacity is not exposed via dispatcher
// accessors today, so we report active in-flight count + a "queue
// depth not exposed" note for the pending side.
func renderDeliverySection(ctx *bot.CommandContext, tr translator, verbose bool) string {
	_ = verbose // recent-permanent-failure log isn't exposed today; nothing extra to print
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.status.section.delivery"))

	if ctx.Admin == nil || ctx.Admin.Dispatcher == nil {
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.value.na"))
		return sb.String()
	}

	totalQueueDepth := ctx.Admin.Dispatcher.QueueDepth()

	type platRow struct {
		labelKey string
		inFlight int
	}
	rows := []platRow{
		{"cmd.poracle_admin.status.label.delivery_discord", ctx.Admin.Dispatcher.DiscordDepth()},
		{"cmd.poracle_admin.status.label.delivery_telegram", ctx.Admin.Dispatcher.TelegramDepth()},
		{"cmd.poracle_admin.status.label.delivery_webhook", ctx.Admin.Dispatcher.WebhookDepth()},
	}
	for _, r := range rows {
		indicator := indicatorGreen
		if totalQueueDepth > deliveryQueueWarnPending {
			indicator = indicatorYellow
		}
		sb.WriteString("\n  ")
		sb.WriteString(indicator)
		sb.WriteString(" ")
		sb.WriteString(tr.Tf(r.labelKey, r.inFlight))
	}

	// Cumulative queue depth visible across all platforms — useful to
	// see backpressure even when the per-platform in-flight counts
	// look fine. Capacity is internal to the dispatcher.
	sb.WriteString("\n  ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.delivery_queue_depth",
		totalQueueDepth))

	return sb.String()
}

// renderDiscordRateSection renders the Discord API rate-limit snapshot.
func renderDiscordRateSection(ctx *bot.CommandContext, tr translator, verbose bool) string {
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.status.section.discord_rate"))

	if ctx.Admin == nil || ctx.Admin.DiscordRate == nil {
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.value.na"))
		return sb.String()
	}
	snap := ctx.Admin.DiscordRate()

	indicator429 := indicatorGreen
	if snap.Recent429Count > 0 {
		indicator429 = indicatorRed
	}

	nearLimit := 0
	for _, r := range snap.Routes {
		if r.Limit > 0 && r.Remaining < r.Limit {
			nearLimit++
		}
	}

	sb.WriteString("\n  ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.discord_routes",
		nearLimit,
	))
	sb.WriteString("\n  ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.discord_global",
		snap.GlobalTokens,
		snap.GlobalCapacity,
	))
	sb.WriteString("\n  ")
	sb.WriteString(indicator429)
	sb.WriteString(" ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.discord_429s",
		snap.Recent429Count,
	))

	if verbose && nearLimit > 0 {
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.label.discord_routes_detail"))
		for _, r := range snap.Routes {
			if r.Limit > 0 && r.Remaining < r.Limit {
				sb.WriteString("\n    ")
				sb.WriteString(fmt.Sprintf("%s: %d/%d (reset %s)",
					r.Route, r.Remaining, r.Limit,
					r.ResetAt.UTC().Format("15:04:05"),
				))
			}
		}
	}

	return sb.String()
}

// renderTelegramRateSection renders the Telegram API rate-limit snapshot.
func renderTelegramRateSection(ctx *bot.CommandContext, tr translator) string {
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.status.section.telegram_rate"))

	if ctx.Admin == nil || ctx.Admin.TelegramRate == nil {
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.value.na"))
		return sb.String()
	}
	snap := ctx.Admin.TelegramRate()

	indicator := indicatorGreen
	if snap.Recent429Count > 0 || (!snap.CurrentBackoffUntil.IsZero() && snap.CurrentBackoffUntil.After(time.Now())) {
		indicator = indicatorRed
	}

	sb.WriteString("\n  ")
	sb.WriteString(indicator)
	sb.WriteString(" ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.telegram_429s",
		snap.Recent429Count,
	))

	backoff := tr.T("cmd.poracle_admin.status.value.none")
	if !snap.CurrentBackoffUntil.IsZero() && snap.CurrentBackoffUntil.After(time.Now()) {
		backoff = formatDuration(time.Until(snap.CurrentBackoffUntil).Round(time.Second))
	}
	sb.WriteString("\n  ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.telegram_backoff", backoff))

	return sb.String()
}

// renderAlertLimitsSection renders the alert/summary breach + ban totals.
func renderAlertLimitsSection(ctx *bot.CommandContext, tr translator) string {
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.status.section.alert_limits"))

	if ctx.Admin == nil || ctx.Admin.AlertLimiter == nil {
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.value.na"))
		return sb.String()
	}
	blocked := ctx.Admin.AlertLimiter.ListBlocked()
	now := time.Now()

	var alertCount, summaryCount int
	bannedTargets := make(map[string]bool)
	for _, t := range blocked {
		if !t.BannedUntil.IsZero() && t.BannedUntil.After(now) {
			bannedTargets[t.ID] = true
		}
		switch t.Bucket {
		case "summary":
			summaryCount++
		default:
			alertCount++
		}
	}
	bannedCount := len(bannedTargets)

	indicator := indicatorGreen
	if bannedCount > 0 {
		indicator = indicatorRed
	} else if alertCount > 0 || summaryCount > 0 {
		indicator = indicatorYellow
	}

	sb.WriteString("\n  ")
	sb.WriteString(indicator)
	sb.WriteString(" ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.alert_limits_counts",
		alertCount,
		summaryCount,
		bannedCount,
	))

	return sb.String()
}

// renderSummaryBufferSection renders the summary buffer count using
// ctx.SummaryBuffer.EnumerateUsers(). Reports total buffered entries,
// bucket count, and top-3 users by entry count. Paints 🟡 when the
// total exceeds the summaryBufferWarn threshold.
func renderSummaryBufferSection(ctx *bot.CommandContext, tr translator) string {
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.status.section.summary_buffer"))

	if ctx.SummaryBuffer == nil {
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.value.na"))
		return sb.String()
	}

	enums := ctx.SummaryBuffer.EnumerateUsers()

	totalEntries := 0
	bucketCount := len(enums)
	for _, e := range enums {
		totalEntries += e.Count
	}

	indicator := indicatorGreen
	if totalEntries > summaryBufferWarn {
		indicator = indicatorYellow
	}

	sb.WriteString("\n  ")
	sb.WriteString(indicator)
	sb.WriteString(" ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.summary_buffer_totals",
		totalEntries,
		bucketCount,
	))

	if len(enums) > 0 {
		// Sort by count descending to show top-3.
		sort.Slice(enums, func(i, j int) bool {
			return enums[i].Count > enums[j].Count
		})
		top := enums
		if len(top) > 3 {
			top = top[:3]
		}
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.label.summary_buffer_top"))
		for _, e := range top {
			sb.WriteString("\n")
			sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.summary_buffer_user_row",
				e.HumanID,
				e.AlertType,
				e.Count,
			))
		}
	}

	return sb.String()
}

// renderTrackingSection lists tracking-rule totals broken down by type
// plus active-human and registered-human counts.
func renderTrackingSection(ctx *bot.CommandContext, tr translator) string {
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.status.section.tracking"))

	if ctx.StateMgr == nil {
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.value.na"))
		return sb.String()
	}
	s := ctx.StateMgr.Get()
	if s == nil {
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.value.na"))
		return sb.String()
	}

	// countTrackingRules is defined in poracle_admin_reload.go and
	// pre-sums the monster index Total field; reused here.
	total := countTrackingRules(s)
	sb.WriteString("\n  ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.tracking_total",
		total))

	monsters := 0
	if s.Monsters != nil {
		monsters = s.Monsters.Total
	}

	// Per-type counts, one line each so the breakdown stays readable.
	type pair struct {
		key   string
		count int
	}
	for _, p := range []pair{
		{"pokemon", monsters},
		{"raid", len(s.Raids)},
		{"egg", len(s.Eggs)},
		{"quest", len(s.Quests)},
		{"invasion", len(s.Invasions)},
		{"lure", len(s.Lures)},
		{"nest", len(s.Nests)},
		{"gym", len(s.Gyms)},
		{"fort", len(s.Forts)},
		{"maxbattle", len(s.Maxbattles)},
	} {
		sb.WriteString("\n    ")
		sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.tracking_type",
			tr.T("cmd.poracle_admin.status.tracking_type."+p.key),
			p.count,
		))
	}

	// Active humans: state snapshot loads enabled=1 AND admin_disable=0.
	activeHumans := len(s.Humans)
	sb.WriteString("\n  ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.tracking_active_humans",
		activeHumans))

	// Registered humans (total): query the store. Surface error
	// explicitly rather than silently omitting the line.
	if ctx.Humans != nil {
		all, err := ctx.Humans.ListAll()
		sb.WriteString("\n  ")
		if err != nil {
			sb.WriteString(tr.T("cmd.poracle_admin.status.label.tracking_registered_humans_error"))
		} else {
			sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.tracking_registered_humans",
				len(all)))
		}
	}

	return sb.String()
}

// renderMySQLSection probes the DB and reports pool stats.
func renderMySQLSection(ctx *bot.CommandContext, tr translator) string {
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.status.section.mysql"))

	if ctx.DB == nil {
		sb.WriteString("\n  ")
		sb.WriteString(tr.T("cmd.poracle_admin.status.value.na"))
		return sb.String()
	}

	// Quick ping with a 2s timeout so a hung DB doesn't freeze the
	// whole status command. The dispatcher-side health is what we
	// actually care about; "ping ok" + pool stats are an operational
	// hint, not a deep diagnostic.
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	pingErr := ctx.DB.PingContext(pingCtx)
	cancel()

	indicator := indicatorGreen
	pingStatus := tr.T("cmd.poracle_admin.status.label.mysql_ping_ok")
	if pingErr != nil {
		indicator = indicatorRed
		pingStatus = tr.Tf("cmd.poracle_admin.status.label.mysql_ping_fail", pingErr.Error())
	}
	sb.WriteString("\n  ")
	sb.WriteString(indicator)
	sb.WriteString(" ")
	sb.WriteString(pingStatus)

	st := ctx.DB.Stats()
	sb.WriteString("\n  ")
	sb.WriteString(tr.Tf("cmd.poracle_admin.status.label.mysql_pool",
		st.OpenConnections,
		st.MaxOpenConnections,
		st.InUse,
		st.Idle,
		st.WaitCount,
	))

	return sb.String()
}


// typeCount is a (name, count) pair used to sort webhook breakdown
// entries by count descending.
type typeCount struct {
	name  string
	count int
}

// sortByCountDesc returns the map entries sorted by count descending,
// then by name ascending for stable output.
func sortByCountDesc(m map[string]int) []typeCount {
	out := make([]typeCount, 0, len(m))
	for k, v := range m {
		out = append(out, typeCount{name: k, count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].name < out[j].name
	})
	return out
}

// translator is the minimal interface statusReport's render helpers
// need from an *i18n.Translator. Declared here so tests don't have
// to spin up a full bundle when sharper assertions on output suffice.
type translator interface {
	T(key string) string
	Tf(key string, args ...any) string
}
