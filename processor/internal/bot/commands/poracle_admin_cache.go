package commands

import (
	"fmt"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// paCache implements !poracle-admin cache — geocoder cache introspection.
//
// Subcommands:
//
//	stats         — render geocoder cache stats (memory entries, disk entries, hit/miss totals)
//	clear geocoder — drop the in-memory geocoder cache layer; report count dropped
//	clear          — (without specifier) reply with usage hint
var paCache = &paSubgroup{
	run:  paCacheRun,
	help: paCacheHelp,
}

func paCacheHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.cache.help.intro"))
	sb.WriteString("\n\n")
	sb.WriteString(tr.T("cmd.poracle_admin.cache.stats.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.cache.clear.desc"))

	return []bot.Reply{{Text: sb.String()}}
}

func paCacheRun(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 || args[0] == "help" {
		return paCacheHelp(ctx)
	}

	switch strings.ToLower(args[0]) {
	case "stats":
		return paCacheStats(ctx)

	case "clear":
		// Must have "geocoder" as the next arg.
		if len(args) < 2 || strings.ToLower(args[1]) != "geocoder" {
			return []bot.Reply{{Text: tr.T("cmd.poracle_admin.cache.clear.usage_hint")}}
		}
		return paCacheClearGeocoder(ctx)

	default:
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.unknown_sub", "cache")}}
	}
}

func paCacheStats(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	if ctx.GeocoderStats == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.cache.not_configured")}}
	}

	s := ctx.GeocoderStats()

	total := s.HitsMemory + s.HitsDisk + s.Misses
	var hitRate int
	if total > 0 {
		hitRate = int((s.HitsMemory+s.HitsDisk)*100 / total)
	}

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.cache.stats.header"))
	sb.WriteString("\n")
	sb.WriteString(tr.Tf("cmd.poracle_admin.cache.stats.row.memory_entries", fmt.Sprintf("%d", s.MemoryEntries)))
	sb.WriteString("\n")
	sb.WriteString(tr.Tf("cmd.poracle_admin.cache.stats.row.disk_entries", fmt.Sprintf("%d", s.DiskEntries)))
	sb.WriteString("\n")
	sb.WriteString(tr.Tf("cmd.poracle_admin.cache.stats.row.hits_memory", fmt.Sprintf("%d", s.HitsMemory)))
	sb.WriteString("\n")
	sb.WriteString(tr.Tf("cmd.poracle_admin.cache.stats.row.hits_disk", fmt.Sprintf("%d", s.HitsDisk)))
	sb.WriteString("\n")
	sb.WriteString(tr.Tf("cmd.poracle_admin.cache.stats.row.misses", fmt.Sprintf("%d", s.Misses)))
	sb.WriteString("\n")
	sb.WriteString(tr.Tf("cmd.poracle_admin.cache.stats.row.hit_rate", fmt.Sprintf("%d", hitRate)))

	return []bot.Reply{{Text: sb.String()}}
}

func paCacheClearGeocoder(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	if ctx.GeocoderClear == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.cache.not_configured")}}
	}

	count := ctx.GeocoderClear()
	return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.cache.clear.geocoder.success", fmt.Sprintf("%d", count))}}
}
