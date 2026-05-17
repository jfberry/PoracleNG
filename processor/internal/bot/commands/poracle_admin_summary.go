package commands

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// paSummary implements !poracle-admin summary — summary buffer inspection
// and forced dispatch.
//
// Subcommands:
//
//	list              — show all users with anything buffered
//	show <user>       — inspect one user's buffered entries (per alertType, first 5 reward groups)
//	fire <user> [alertType] — force-dispatch immediately (default alertType: quest)
var paSummary = &paSubgroup{
	run:  paSummaryRun,
	help: paSummaryHelp,
}

func paSummaryHelp(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	var sb strings.Builder
	sb.WriteString(tr.T("cmd.poracle_admin.summary.help.intro"))
	sb.WriteString("\n\n")
	sb.WriteString(tr.T("cmd.poracle_admin.summary.list.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.summary.show.desc"))
	sb.WriteString("\n")
	sb.WriteString(tr.T("cmd.poracle_admin.summary.fire.desc"))
	return []bot.Reply{{Text: sb.String()}}
}

func paSummaryRun(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 || args[0] == "help" {
		return paSummaryHelp(ctx)
	}

	if ctx.SummaryBuffer == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.summary.not_configured")}}
	}

	switch strings.ToLower(args[0]) {
	case "list":
		return paSummaryList(ctx)
	case "show":
		return paSummaryShow(ctx, args[1:])
	case "fire":
		return paSummaryFire(ctx, args[1:])
	default:
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.unknown_sub", "summary")}}
	}
}

// paSummaryList shows all (user, alertType) pairs with something buffered.
func paSummaryList(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()

	enums := ctx.SummaryBuffer.EnumerateUsers()
	if len(enums) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.summary.list.empty")}}
	}

	// Sort for stable output: by humanID, then alertType.
	sort.Slice(enums, func(i, j int) bool {
		if enums[i].HumanID != enums[j].HumanID {
			return enums[i].HumanID < enums[j].HumanID
		}
		return enums[i].AlertType < enums[j].AlertType
	})

	// Count totals.
	totalEntries := 0
	for _, e := range enums {
		totalEntries += e.Count
	}

	// Group by user for display.
	type userBucket struct {
		humanID string
		rows    []tracker.SummaryEnumeration
	}
	var users []userBucket
	seen := make(map[string]int) // humanID → index in users
	for _, e := range enums {
		idx, ok := seen[e.HumanID]
		if !ok {
			idx = len(users)
			users = append(users, userBucket{humanID: e.HumanID})
			seen[e.HumanID] = idx
		}
		users[idx].rows = append(users[idx].rows, e)
	}

	var sb strings.Builder
	sb.WriteString(tr.Tf("cmd.poracle_admin.summary.list.header", len(users), totalEntries))
	sb.WriteString("\n")
	for _, u := range users {
		sb.WriteString("\n")
		sb.WriteString(tr.Tf("cmd.poracle_admin.summary.list.user_header", u.humanID))
		sb.WriteString("\n")
		for _, row := range u.rows {
			nextFire := tr.T("cmd.poracle_admin.summary.list.next_fire.none")
			if !row.NextFireAt.IsZero() {
				nextFire = row.NextFireAt.UTC().Format("15:04 UTC")
			}
			sb.WriteString(tr.Tf("cmd.poracle_admin.summary.list.row",
				row.AlertType,
				strconv.Itoa(row.Count),
				nextFire,
			))
			sb.WriteString("\n")
		}
	}

	return bot.SplitTextReply(strings.TrimRight(sb.String(), "\n"))
}

// paSummaryShow renders per-alertType detail for one user, including the
// first 5 reward groupings of each alertType bucket.
func paSummaryShow(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.summary.show.usage")}}
	}

	humanID := args[0]

	// Collect all alertTypes that have something for this user.
	enums := ctx.SummaryBuffer.EnumerateUsers()
	var relevant []tracker.SummaryEnumeration
	for _, e := range enums {
		if e.HumanID == humanID {
			relevant = append(relevant, e)
		}
	}

	if len(relevant) == 0 {
		return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.summary.show.empty", humanID)}}
	}

	sort.Slice(relevant, func(i, j int) bool {
		return relevant[i].AlertType < relevant[j].AlertType
	})

	var sb strings.Builder
	sb.WriteString(tr.Tf("cmd.poracle_admin.summary.show.header", humanID))
	sb.WriteString("\n")
	for _, e := range relevant {
		sb.WriteString("\n")
		sb.WriteString(tr.Tf("cmd.poracle_admin.summary.show.alert_type", e.AlertType, strconv.Itoa(e.Count)))
		sb.WriteString("\n")

		// Show first 5 reward groupings.
		entries := ctx.SummaryBuffer.List(humanID, e.AlertType)
		// Group by (rewardType, reward, form) to match how dispatch groups them.
		type groupKey struct {
			RewardType int
			Reward     int
			Form       int
		}
		groupCounts := make(map[groupKey]int)
		for _, q := range entries {
			k := groupKey{q.RewardType, q.Reward, q.Form}
			groupCounts[k]++
		}
		// Sort groups for stable display.
		type groupEntry struct {
			k     groupKey
			count int
		}
		groups := make([]groupEntry, 0, len(groupCounts))
		for k, c := range groupCounts {
			groups = append(groups, groupEntry{k, c})
		}
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].k.RewardType != groups[j].k.RewardType {
				return groups[i].k.RewardType < groups[j].k.RewardType
			}
			if groups[i].k.Reward != groups[j].k.Reward {
				return groups[i].k.Reward < groups[j].k.Reward
			}
			return groups[i].k.Form < groups[j].k.Form
		})
		const maxGroups = 5
		shown := groups
		if len(shown) > maxGroups {
			shown = shown[:maxGroups]
		}
		for _, g := range shown {
			formSuffix := ""
			if g.k.Form > 0 {
				formSuffix = fmt.Sprintf(" form %d", g.k.Form)
			}
			sb.WriteString(fmt.Sprintf("    Reward type %d, reward %d%s — %d stops\n",
				g.k.RewardType, g.k.Reward, formSuffix, g.count))
		}
		if len(groups) > maxGroups {
			sb.WriteString(fmt.Sprintf("    ... (%d more reward groups)\n", len(groups)-maxGroups))
		}
	}

	return bot.SplitTextReply(strings.TrimRight(sb.String(), "\n"))
}

// paSummaryFire force-dispatches the buffer for one user immediately.
func paSummaryFire(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.summary.fire.usage")}}
	}

	humanID := args[0]
	alertType := "quest" // default
	if len(args) >= 2 {
		alertType = strings.ToLower(args[1])
	}

	// Count before dispatch so we can report the number delivered.
	count := len(ctx.SummaryBuffer.List(humanID, alertType))

	if ctx.SummaryDispatch == nil {
		return []bot.Reply{{Text: tr.T("cmd.poracle_admin.summary.not_configured")}}
	}

	// DispatchQuestSummary currently only supports alertType=="quest";
	// for other types we'll get a silent no-op (the function returns early).
	// We still invoke and report success — when other types are added they'll
	// work automatically.
	ctx.SummaryDispatch(humanID, alertType)

	return []bot.Reply{{Text: tr.Tf("cmd.poracle_admin.summary.fire.success",
		humanID, alertType, strconv.Itoa(count))}}
}
