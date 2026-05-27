package bot_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/mappers"
)

// TestSlashTextParity exercises each parity fixture through the slash mapper
// and asserts the produced token set matches the fixture's expected tokens.
//
// Text-side parity is not asserted by this runner — full text parsing requires
// a wired-up Parser + bundle + admin lists, which the text bot's own tests
// already cover. This runner pins the slash mapper outputs so they cannot
// silently drift from the text bot's accepted token grammar.
func TestSlashTextParity(t *testing.T) {
	fixtures, err := bot.LoadParityFixtures("testdata/parity.yaml")
	if err != nil {
		t.Fatal(err)
	}

	for _, fix := range fixtures {
		t.Run(fix.Name, func(t *testing.T) {
			mapper := mappers.Lookup(fix.Slash.Name)
			if mapper == nil {
				t.Fatalf("no mapper for %q", fix.Slash.Name)
			}
			tokens, err := mapper(optionsFromMap(fix.Slash.Options))
			if err != nil {
				t.Fatalf("mapper error: %v", err)
			}
			assertTokensEqual(t, fix.ExpectedTokens, tokens, "slash mapper output")
		})
	}
}

// optionsFromMap converts a YAML option map into the discordgo option slice
// shape the mappers expect.
//
// Encoding conventions:
//
//   - A top-level option key "foo" with a scalar value becomes a flat
//     option of inferred type.
//   - A dotted key "sub.opt" denotes a SubCommand option named "sub" with
//     a child option named "opt". Multiple dotted keys sharing a prefix
//     collect under the same SubCommand.
//   - A double-dotted key "group.sub.opt" denotes a SubCommandGroup option
//     named "group" with a SubCommand "sub" with a child option "opt"
//     (used by /summary).
//   - A key whose string value is exactly "_bare" denotes a bare
//     SubCommand option (no children). Used for /area show, /profile list.
//   - A double-dotted key "group.sub" with value "_bare" denotes a bare
//     sub-command nested under a SubCommandGroup.
//
// Type inference for scalar values follows YAML unmarshal defaults:
//   - bool → ApplicationCommandOptionBoolean
//   - int / int64 / float64 → ApplicationCommandOptionInteger
//     (value stored as float64 because discordgo's IntValue() panics on
//     anything that isn't float64 inside Value).
//   - string → ApplicationCommandOptionString
//
// SubCommand and SubCommandGroup options are sorted to the front of the
// returned slice so opts[0] is the sub-command (or group) for
// sub-command-aware mappers (area, profile, untrack, summary).
func optionsFromMap(m map[string]any) []*discordgo.ApplicationCommandInteractionDataOption {
	if len(m) == 0 {
		return nil
	}

	// Deterministic iteration so any future order-sensitive mapper has a
	// stable input.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// First pass: detect which top-level names are SubCommandGroups by
	// looking for either:
	//   - a key with 2 dots ("group.sub.opt"), or
	//   - a 1-dot key whose value is "_bare" ("group.sub" → bare sub
	//     inside a group; the only valid interpretation of a 2-segment
	//     key with a _bare value, because flat SubCommands don't have
	//     bare leaf options).
	groupNames := map[string]bool{}
	for _, k := range keys {
		dots := strings.Count(k, ".")
		if dots >= 2 {
			groupNames[k[:strings.IndexByte(k, '.')]] = true
			continue
		}
		if dots == 1 {
			if s, ok := m[k].(string); ok && s == "_bare" {
				groupNames[k[:strings.IndexByte(k, '.')]] = true
			}
		}
	}

	subs := map[string][]*discordgo.ApplicationCommandInteractionDataOption{}
	bareSubs := map[string]bool{}
	// Two-level: group → sub-command → its children (or empty for bare).
	groups := map[string]map[string][]*discordgo.ApplicationCommandInteractionDataOption{}
	groupBare := map[string]map[string]bool{}
	var flat []*discordgo.ApplicationCommandInteractionDataOption

	for _, k := range keys {
		v := m[k]
		isBare := false
		if s, ok := v.(string); ok && s == "_bare" {
			isBare = true
		}
		dots := strings.Count(k, ".")
		switch dots {
		case 0:
			if isBare {
				bareSubs[k] = true
			} else {
				flat = append(flat, buildOption(k, v))
			}
		case 1:
			parts := strings.SplitN(k, ".", 2)
			topName, child := parts[0], parts[1]
			if groupNames[topName] {
				// Bare sub-command nested in a SubCommandGroup.
				if _, ok := groupBare[topName]; !ok {
					groupBare[topName] = map[string]bool{}
				}
				groupBare[topName][child] = true
			} else {
				subs[topName] = append(subs[topName], buildOption(child, v))
			}
		default:
			parts := strings.SplitN(k, ".", 3)
			group, sub, opt := parts[0], parts[1], parts[2]
			if _, ok := groups[group]; !ok {
				groups[group] = map[string][]*discordgo.ApplicationCommandInteractionDataOption{}
			}
			groups[group][sub] = append(groups[group][sub], buildOption(opt, v))
		}
	}

	// Build a stable sub-command order: dotted-key subs alphabetically,
	// then bare subs alphabetically, then groups alphabetically.
	subOrder := make([]string, 0, len(subs))
	for s := range subs {
		subOrder = append(subOrder, s)
	}
	sort.Strings(subOrder)
	var subOpts []*discordgo.ApplicationCommandInteractionDataOption
	for _, name := range subOrder {
		children := subs[name]
		sort.SliceStable(children, func(i, j int) bool { return children[i].Name < children[j].Name })
		subOpts = append(subOpts, &discordgo.ApplicationCommandInteractionDataOption{
			Name:    name,
			Type:    discordgo.ApplicationCommandOptionSubCommand,
			Options: children,
		})
	}
	bareNames := make([]string, 0, len(bareSubs))
	for s := range bareSubs {
		bareNames = append(bareNames, s)
	}
	sort.Strings(bareNames)
	for _, name := range bareNames {
		subOpts = append(subOpts, &discordgo.ApplicationCommandInteractionDataOption{
			Name: name,
			Type: discordgo.ApplicationCommandOptionSubCommand,
		})
	}

	// SubCommandGroups: combine option-bearing + bare children per group.
	groupOrder := make([]string, 0, len(groupNames))
	for g := range groupNames {
		groupOrder = append(groupOrder, g)
	}
	sort.Strings(groupOrder)
	for _, gName := range groupOrder {
		var children []*discordgo.ApplicationCommandInteractionDataOption
		seen := map[string]bool{}

		subWithOpts := make([]string, 0, len(groups[gName]))
		for s := range groups[gName] {
			subWithOpts = append(subWithOpts, s)
		}
		sort.Strings(subWithOpts)
		for _, sName := range subWithOpts {
			subChildren := groups[gName][sName]
			sort.SliceStable(subChildren, func(i, j int) bool { return subChildren[i].Name < subChildren[j].Name })
			children = append(children, &discordgo.ApplicationCommandInteractionDataOption{
				Name:    sName,
				Type:    discordgo.ApplicationCommandOptionSubCommand,
				Options: subChildren,
			})
			seen[sName] = true
		}
		bareInGroup := make([]string, 0, len(groupBare[gName]))
		for s := range groupBare[gName] {
			if !seen[s] {
				bareInGroup = append(bareInGroup, s)
			}
		}
		sort.Strings(bareInGroup)
		for _, sName := range bareInGroup {
			children = append(children, &discordgo.ApplicationCommandInteractionDataOption{
				Name: sName,
				Type: discordgo.ApplicationCommandOptionSubCommand,
			})
		}
		subOpts = append(subOpts, &discordgo.ApplicationCommandInteractionDataOption{
			Name:    gName,
			Type:    discordgo.ApplicationCommandOptionSubCommandGroup,
			Options: children,
		})
	}

	// SubCommand options first, then flat options.
	return append(subOpts, flat...)
}

// buildOption infers an option type from a value's Go type and produces the
// matching discordgo option struct.
func buildOption(name string, v any) *discordgo.ApplicationCommandInteractionDataOption {
	opt := &discordgo.ApplicationCommandInteractionDataOption{Name: name}
	switch val := v.(type) {
	case bool:
		opt.Type = discordgo.ApplicationCommandOptionBoolean
		opt.Value = val
	case int:
		opt.Type = discordgo.ApplicationCommandOptionInteger
		opt.Value = float64(val)
	case int64:
		opt.Type = discordgo.ApplicationCommandOptionInteger
		opt.Value = float64(val)
	case float64:
		opt.Type = discordgo.ApplicationCommandOptionInteger
		opt.Value = val
	case string:
		opt.Type = discordgo.ApplicationCommandOptionString
		opt.Value = val
	default:
		// Fall back to string with empty value rather than dropping the
		// option silently — surfaces fixture authoring mistakes.
		opt.Type = discordgo.ApplicationCommandOptionString
		opt.Value = ""
	}
	return opt
}

// assertTokensEqual compares two token slices as multisets (order-insensitive)
// so fixture authors don't need to track mapper emit order.
func assertTokensEqual(t *testing.T, want, got []string, label string) {
	t.Helper()
	wantSorted := append([]string{}, want...)
	gotSorted := append([]string{}, got...)
	sort.Strings(wantSorted)
	sort.Strings(gotSorted)
	if len(wantSorted) != len(gotSorted) {
		t.Errorf("%s: token-count want=%d got=%d (want=%v got=%v)", label, len(wantSorted), len(gotSorted), want, got)
		return
	}
	for i := range wantSorted {
		if wantSorted[i] != gotSorted[i] {
			t.Errorf("%s: token mismatch at sorted-index %d: want=%q got=%q (want=%v got=%v)", label, i, wantSorted[i], gotSorted[i], want, got)
			return
		}
	}
}
