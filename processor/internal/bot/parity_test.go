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
				// /location is the only mapper that requires BotDeps for
				// geocoding; it is intentionally NOT registered in the
				// shared registry. Parity for /location is exercised by
				// mappers/location_test.go.
				if fix.Slash.Name == "location" {
					t.Skip("location mapper has BotDeps signature; covered by mappers/location_test.go")
				}
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
//   - A key whose string value is exactly "_bare" denotes a bare
//     SubCommand option (no children). Used for /area show, /profile list.
//
// Type inference for scalar values follows YAML unmarshal defaults:
//   - bool → ApplicationCommandOptionBoolean
//   - int / int64 / float64 → ApplicationCommandOptionInteger
//     (value stored as float64 because discordgo's IntValue() panics on
//     anything that isn't float64 inside Value).
//   - string → ApplicationCommandOptionString
//
// SubCommand options are sorted to the front of the returned slice so
// opts[0] is the sub-command for sub-command-aware mappers (area, profile,
// untrack).
func optionsFromMap(m map[string]interface{}) []*discordgo.ApplicationCommandInteractionDataOption {
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

	subs := map[string][]*discordgo.ApplicationCommandInteractionDataOption{}
	bareSubs := map[string]bool{}
	var flat []*discordgo.ApplicationCommandInteractionDataOption

	for _, k := range keys {
		v := m[k]
		if s, ok := v.(string); ok && s == "_bare" {
			bareSubs[k] = true
			continue
		}
		if dot := strings.IndexByte(k, '.'); dot > 0 {
			sub := k[:dot]
			opt := k[dot+1:]
			subs[sub] = append(subs[sub], buildOption(opt, v))
			continue
		}
		flat = append(flat, buildOption(k, v))
	}

	// Build a stable sub-command order: dotted-key subs alphabetically,
	// then bare subs alphabetically.
	subNames := make([]string, 0, len(subs))
	for s := range subs {
		subNames = append(subNames, s)
	}
	sort.Strings(subNames)
	var subOpts []*discordgo.ApplicationCommandInteractionDataOption
	for _, name := range subNames {
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

	// SubCommand options first, then flat options.
	return append(subOpts, flat...)
}

// buildOption infers an option type from a value's Go type and produces the
// matching discordgo option struct.
func buildOption(name string, v interface{}) *discordgo.ApplicationCommandInteractionDataOption {
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
