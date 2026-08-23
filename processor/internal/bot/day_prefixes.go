package bot

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// canonicalDayPrefixes are the English day prefixes accepted by the settime
// grammar, in the order they should be offered as suggestions (single days
// first, then the groupings). English is always accepted regardless of the
// user's language.
var canonicalDayPrefixes = []struct {
	Name string
	Days []int
	Key  string // i18n key holding the translated alias
}{
	{"mon", []int{1}, "arg.prefix.mon"},
	{"tue", []int{2}, "arg.prefix.tue"},
	{"wed", []int{3}, "arg.prefix.wed"},
	{"thu", []int{4}, "arg.prefix.thu"},
	{"fri", []int{5}, "arg.prefix.fri"},
	{"sat", []int{6}, "arg.prefix.sat"},
	{"sun", []int{7}, "arg.prefix.sun"},
	{"weekday", []int{1, 2, 3, 4, 5}, "arg.prefix.weekday"},
	{"weekend", []int{6, 7}, "arg.prefix.weekend"},
	{"every", []int{1, 2, 3, 4, 5, 6, 7}, "arg.prefix.every"},
	{"everyday", []int{1, 2, 3, 4, 5, 6, 7}, "arg.prefix.everyday"},
}

// DayPrefixMap builds the day-prefix lookup used by the settime grammar:
// English names always, plus the caller language's translated aliases.
//
// Lives here rather than beside ProfileCommand so the slash autocomplete can
// offer exactly the prefixes the parser will accept — internal/bot/commands
// imports internal/discordbot/slash, so the autocomplete package cannot
// import commands without a cycle.
//
// tr may be nil, in which case only the English names are returned.
func DayPrefixMap(tr *i18n.Translator) map[string][]int {
	m := make(map[string][]int, len(canonicalDayPrefixes)*2)
	for _, p := range canonicalDayPrefixes {
		m[p.Name] = p.Days
	}
	if tr == nil {
		return m
	}
	for _, p := range canonicalDayPrefixes {
		translated := strings.ToLower(tr.T(p.Key))
		if translated != p.Key && translated != "" {
			m[translated] = p.Days
		}
	}
	return m
}

// DayPrefixNames returns the accepted prefixes in suggestion order: the
// canonical English names first, then any translated aliases that differ
// from them. Deterministic, unlike ranging over DayPrefixMap.
func DayPrefixNames(tr *i18n.Translator) []string {
	out := make([]string, 0, len(canonicalDayPrefixes)*2)
	seen := make(map[string]bool, len(canonicalDayPrefixes)*2)
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, p := range canonicalDayPrefixes {
		add(p.Name)
	}
	if tr != nil {
		for _, p := range canonicalDayPrefixes {
			add(strings.ToLower(tr.T(p.Key)))
		}
	}
	// A missing translation returns the key itself; never offer those.
	filtered := out[:0]
	for _, n := range out {
		if !strings.HasPrefix(n, "arg.prefix.") {
			filtered = append(filtered, n)
		}
	}
	return filtered
}
