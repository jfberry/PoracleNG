package commands

import (
	"sort"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/ratelimit"
)

// paRatelimit implements !poracle-admin ratelimit — per-destination alert
// rate-limit introspection and manual reset.
//
// Subcommands:
//
//	list       — show all targets currently breached or banned
//	show <t>   — detailed view of one target (both buckets)
//	reset <t>  — clear counters + violations; admin_disable is NOT touched
//	userlist   — shortcut: re-dispatches to !userlist disabled
var paRatelimit = &paSubgroup{
	run:  paRatelimitRun,
	help: paRatelimitHelp,
}

func paRatelimitHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.ratelimit.help.intro"))
	sb.WriteString("\n\n")
	sb.WriteString(tr.T("cmd.poracle_admin.ratelimit.list.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.ratelimit.show.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.ratelimit.reset.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.ratelimit.userlist.desc"))
	return []bot.Reply{{Text: sb.String()}}
}

func paRatelimitRun(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 || args[0] == "help" {
		return paRatelimitHelp(ctx)
	}

	switch strings.ToLower(args[0]) {
	case "list":
		return paRatelimitList(ctx)
	case "show":
		return paRatelimitShow(ctx, args[1:])
	case "reset":
		return paRatelimitReset(ctx, args[1:])
	case "userlist":
		return paRatelimitUserlist(ctx)
	default:
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.unknown_sub", "ratelimit")}}
	}
}

// paRatelimitList renders every (target, bucket) pair currently breached or banned.
func paRatelimitList(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	if ctx.AlertLimiter == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.ratelimit.not_configured")}}
	}

	blocked := ctx.AlertLimiter.ListBlocked()
	if len(blocked) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.ratelimit.list.empty")}}
	}

	// Partition by bucket.
	var alertStates, summaryStates []ratelimit.TargetState
	for _, s := range blocked {
		if s.Bucket == "summary" {
			summaryStates = append(summaryStates, s)
		} else {
			alertStates = append(alertStates, s)
		}
	}
	sortTargetStates(alertStates)
	sortTargetStates(summaryStates)

	var sb strings.Builder
	sb.WriteString("🚦 ")
	sb.WriteString(tr.T("cmd.poracle_admin.ratelimit.list.header_title"))
	sb.WriteString("\n")

	now := time.Now()
	if len(alertStates) > 0 {
		breached, banned := countBucketStats(alertStates)
		sb.WriteString(tr.Tf("cmd.poracle_admin.ratelimit.list.bucket_header", "alert", breached, banned))
		sb.WriteString("\n")
		for _, s := range alertStates {
			sb.WriteString("    ")
			sb.WriteString(formatTargetID(s))
			sb.WriteString(" — ")
			sb.WriteString(tr.Tf("cmd.poracle_admin.ratelimit.list.row",
				s.Count, s.Limit, statusBlurb(s, now, tr)))
			sb.WriteString("\n")
		}
	}

	if len(summaryStates) > 0 {
		if len(alertStates) > 0 {
			sb.WriteString("\n")
		}
		breached, banned := countBucketStats(summaryStates)
		sb.WriteString(tr.Tf("cmd.poracle_admin.ratelimit.list.bucket_header", "summary", breached, banned))
		sb.WriteString("\n")
		for _, s := range summaryStates {
			sb.WriteString("    ")
			sb.WriteString(formatTargetID(s))
			sb.WriteString(" — ")
			sb.WriteString(tr.Tf("cmd.poracle_admin.ratelimit.list.row",
				s.Count, s.Limit, statusBlurb(s, now, tr)))
			sb.WriteString("\n")
		}
	}

	return []bot.Reply{{Text: strings.TrimRight(sb.String(), "\n")}}
}

// paRatelimitShow renders detailed state for one target across both buckets.
func paRatelimitShow(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.ratelimit.usage.show")}}
	}

	if ctx.AlertLimiter == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.ratelimit.not_configured")}}
	}

	rawTarget := args[0]
	id, dtype := parseTargetArg(rawTarget)

	states := ctx.AlertLimiter.StateFor(id, dtype)
	if len(states) == 0 {
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.ratelimit.show.empty", rawTarget)}}
	}

	// Index by bucket.
	byBucket := make(map[string]ratelimit.TargetState, 2)
	for _, s := range states {
		byBucket[s.Bucket] = s
	}

	now := time.Now()
	var sb strings.Builder
	sb.WriteString(tr.Tf("cmd.poracle_admin.ratelimit.show.header", rawTarget))
	sb.WriteString("\n")

	for _, bucket := range []string{"alert", "summary"} {
		sb.WriteString("  ")
		sb.WriteString(tr.Tf("cmd.poracle_admin.ratelimit.show.bucket_header", bucket))
		sb.WriteString("\n")

		s, ok := byBucket[bucket]
		if !ok {
			sb.WriteString("    ")
			sb.WriteString(tr.T("cmd.poracle_admin.ratelimit.show.no_data"))
			sb.WriteString("\n")
			continue
		}

		// Count / limit line.
		overUnder := tr.T("cmd.poracle_admin.ratelimit.show.status.under_limit")
		if s.Count >= s.Limit {
			overUnder = tr.T("cmd.poracle_admin.ratelimit.show.status.over_limit")
		}
		sb.WriteString(tr.Tf("cmd.poracle_admin.ratelimit.show.row.count", s.Count, s.Limit, overUnder))

		// Window timing.
		if !s.WindowStart.IsZero() {
			ago := now.Sub(s.WindowStart).Round(time.Second)
			resetsIn := s.WindowEnd.Sub(now).Round(time.Second)
			if resetsIn < 0 {
				resetsIn = 0
			}
			sb.WriteString(tr.Tf("cmd.poracle_admin.ratelimit.show.row.window",
				formatDuration(ago), formatDuration(resetsIn)))
		}

		// Violations.
		if s.Violations24h == 0 {
			sb.WriteString("    ")
			sb.WriteString(tr.T("cmd.poracle_admin.ratelimit.show.no_violations"))
			sb.WriteString("\n")
		} else {
			sb.WriteString("    ")
			sb.WriteString(tr.Tf("cmd.poracle_admin.ratelimit.show.violations_count", s.Violations24h))
			sb.WriteString("\n")
		}

		// Ban line.
		if !s.BannedUntil.IsZero() {
			timeStr := s.BannedUntil.UTC().Format("15:04 UTC")
			remaining := s.BannedUntil.Sub(now).Round(time.Second)
			if remaining < 0 {
				remaining = 0
			}
			sb.WriteString("    ")
			sb.WriteString(tr.Tf("cmd.poracle_admin.ratelimit.show.banned_until", timeStr, formatDuration(remaining)))
			sb.WriteString("\n")
		}
	}

	return []bot.Reply{{Text: strings.TrimRight(sb.String(), "\n")}}
}

// paRatelimitReset clears all counters and violations for one target.
func paRatelimitReset(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.ratelimit.usage.reset")}}
	}

	if ctx.AlertLimiter == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.ratelimit.not_configured")}}
	}

	rawTarget := args[0]
	id, dtype := parseTargetArg(rawTarget)

	changed := ctx.AlertLimiter.Reset(id, dtype)
	if !changed {
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.ratelimit.reset.no_state", rawTarget)}}
	}

	var sb strings.Builder
	sb.WriteString(tr.Tf("cmd.poracle_admin.ratelimit.reset.success", rawTarget))
	sb.WriteString("\n\n")
	sb.WriteString(tr.T("cmd.poracle_admin.ratelimit.reset.note"))
	return []bot.Reply{{Text: sb.String()}}
}

// paRatelimitUserlist re-dispatches to !userlist disabled.
func paRatelimitUserlist(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	if ctx.Registry == nil {
		log.Errorf("poracle-admin ratelimit userlist: registry is nil")
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.ratelimit.userlist.lookup_failed")}}
	}

	target := ctx.Registry.Lookup("cmd.userlist")
	if target == nil {
		log.Errorf("poracle-admin ratelimit userlist: cmd.userlist not found in registry")
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.ratelimit.userlist.lookup_failed")}}
	}

	return target.Run(ctx, []string{"disabled"})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseTargetArg splits a target argument into (id, dtype).
// Accepts either "dtype/id" (e.g. "discord:user/123456") or bare "id" (dtype="").
// The split is on the LAST "/" so that "discord:user/123456" → id="123456", dtype="discord:user".
func parseTargetArg(raw string) (id, dtype string) {
	// SplitN with n=2 splits on the FIRST "/". For "discord:user/123456":
	//   parts[0] = "discord:user", parts[1] = "123456"
	// For a bare id like "123456":
	//   parts[0] = "123456", len=1
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[1], parts[0]
}

// formatTargetID builds a display string for a TargetState.
// Uses "type/id" when type is known, else bare id.
func formatTargetID(s ratelimit.TargetState) string {
	if s.Type != "" {
		return s.Type + "/" + s.ID
	}
	return s.ID
}

// countBucketStats returns the (breached, banned) counts for a slice of states.
func countBucketStats(states []ratelimit.TargetState) (breached, banned int) {
	for _, s := range states {
		if !s.BannedUntil.IsZero() {
			banned++
		} else if s.Count >= s.Limit {
			breached++
		}
	}
	return
}

// statusBlurb returns a short human-readable status fragment for a list row.
func statusBlurb(s ratelimit.TargetState, now time.Time, tr interface{ Tf(string, ...any) string }) string {
	if !s.BannedUntil.IsZero() {
		remaining := s.BannedUntil.Sub(now).Round(time.Second)
		if remaining < 0 {
			remaining = 0
		}
		return tr.Tf("cmd.poracle_admin.ratelimit.list.status.banned", formatDuration(remaining))
	}
	if !s.WindowEnd.IsZero() {
		resetsIn := s.WindowEnd.Sub(now).Round(time.Second)
		if resetsIn < 0 {
			resetsIn = 0
		}
		return tr.Tf("cmd.poracle_admin.ratelimit.list.status.breached", formatDuration(resetsIn))
	}
	return ""
}

// sortTargetStates sorts a slice of TargetState for stable output.
// Banned entries first, then by ID.
func sortTargetStates(states []ratelimit.TargetState) {
	sort.Slice(states, func(i, j int) bool {
		iBanned := !states[i].BannedUntil.IsZero()
		jBanned := !states[j].BannedUntil.IsZero()
		if iBanned != jBanned {
			return iBanned // banned first
		}
		return states[i].ID < states[j].ID
	})
}
