package bot_test

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// TestEveryCommandAndOptionHasFixture asserts that every slash command and
// every one of its options is exercised by at least one parity fixture.
//
// The check walks slash.AllDefinitions and looks for evidence of each option
// in the loaded fixture set:
//   - flat option "foo" → fixture has `options: { foo: <value> }`
//   - sub-command "sub" with child option "opt" → fixture has `sub.opt: ...`
//   - bare sub-command "sub" (no children) → fixture has `sub: _bare`
//
// When a new option is added to a definition without a corresponding fixture,
// this test fails — making missing parity coverage immediately visible.
func TestEveryCommandAndOptionHasFixture(t *testing.T) {
	fixtures, err := bot.LoadParityFixtures("testdata/parity.yaml")
	if err != nil {
		t.Fatal(err)
	}

	covered := map[string]map[string]bool{}
	for _, fix := range fixtures {
		if _, ok := covered[fix.Slash.Name]; !ok {
			covered[fix.Slash.Name] = map[string]bool{}
		}
		for k := range fix.Slash.Options {
			covered[fix.Slash.Name][k] = true
		}
	}

	bundle := coverageBundle()
	for _, cmd := range slash.AllDefinitions(bundle, nil) {
		opts, ok := covered[cmd.Name]
		if !ok {
			t.Errorf("slash command %q has no parity fixture", cmd.Name)
			continue
		}
		for _, opt := range cmd.Options {
			if opt.Type != discordgo.ApplicationCommandOptionSubCommand {
				if !opts[opt.Name] {
					t.Errorf("slash command %q option %q never exercised in any parity fixture", cmd.Name, opt.Name)
				}
				continue
			}
			// Sub-command: each child option needs a "<sub>.<opt>" key.
			// A child-less sub-command needs a bare-key entry (e.g. show=_bare).
			if len(opt.Options) == 0 {
				if !opts[opt.Name] {
					t.Errorf("slash command %q subcommand %q never exercised (expected bare-subcommand fixture)", cmd.Name, opt.Name)
				}
				continue
			}
			for _, subOpt := range opt.Options {
				key := opt.Name + "." + subOpt.Name
				if !opts[key] {
					t.Errorf("slash command %q subcommand %q option %q never exercised in any parity fixture (key=%q)", cmd.Name, opt.Name, subOpt.Name, key)
				}
			}
		}
	}
}

// coverageBundle returns a minimal i18n bundle with the slash.* keys needed
// by slash.AllDefinitions. We hand-roll the message map (rather than loading
// the embedded bundle) to keep the test independent of locale-file shape
// and operator overrides — coverage only needs canonical English names.
func coverageBundle() *i18n.Bundle {
	msgs := map[string]string{
		"slash.cmd.version":    "version",
		"slash.desc.version":   "Show Poracle version",
		"slash.cmd.tracked":    "tracked",
		"slash.desc.tracked":   "List your tracking rules",
		"slash.cmd.help":       "help",
		"slash.desc.help":      "Show help",
		"slash.cmd.info":       "info",
		"slash.desc.info":      "Show your bot registration info",
		"slash.cmd.language":   "language",
		"slash.desc.language":  "Show or set your language",
		"slash.cmd.track":      "track",
		"slash.desc.track":     "Track a Pokemon",
		"slash.cmd.raid":       "raid",
		"slash.desc.raid":      "Track a raid boss or raid level",
		"slash.cmd.egg":        "egg",
		"slash.desc.egg":       "Track an egg / raid level",
		"slash.cmd.quest":      "quest",
		"slash.desc.quest":     "Track a quest reward",
		"slash.cmd.invasion":   "invasion",
		"slash.desc.invasion":  "Track a Team Rocket invasion",
		"slash.cmd.lure":       "lure",
		"slash.desc.lure":      "Track a pokestop lure",
		"slash.cmd.nest":       "nest",
		"slash.desc.nest":      "Track a nesting pokemon",
		"slash.cmd.maxbattle":  "maxbattle",
		"slash.desc.maxbattle": "Track a max battle",
		"slash.cmd.gym":        "gym",
		"slash.desc.gym":       "Track gym team / slot / battle changes",
		"slash.cmd.fort":       "fort",
		"slash.desc.fort":      "Track pokestop or gym updates",
		"slash.cmd.untrack":    "untrack",
		"slash.desc.untrack":   "Remove a tracking rule",
		"slash.cmd.area":       "area",
		"slash.desc.area":      "Manage your areas",
		"slash.cmd.profile":    "profile",
		"slash.desc.profile":   "Manage your profiles",
		"slash.cmd.location":   "location",
		"slash.desc.location":  "Set your location",
	}
	b := i18n.NewBundle()
	b.AddTranslator(i18n.NewTranslator("en", msgs))
	b.LinkFallbacks()
	return b
}
