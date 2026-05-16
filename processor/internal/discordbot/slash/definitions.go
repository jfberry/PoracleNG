package slash

import (
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// buildDefinition is the shared constructor for ApplicationCommand defs.
// Name + Description + their localizations are all sourced from the i18n
// bundle. canonShortName is the canonical English short name used for
// programmatic lookup (enable list, command routing) — does NOT change with
// localization.
func buildDefinition(
	bundle *i18n.Bundle,
	key string, // e.g. "cmd.track"
	canonShortName string, // e.g. "track" — used for routing/enable
	options []*discordgo.ApplicationCommandOption,
) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:                     resolveSlashName(bundle, key, canonShortName),
		NameLocalizations:        slashNameLocalizations(bundle, key),
		Description:              slashDescription(bundle, key),
		DescriptionLocalizations: slashDescriptionLocalizations(bundle, key),
		Options:                  options,
	}
}

// resolveSlashName returns the English (primary) slash name from the i18n
// bundle's "slash.cmd.<short>" key, falling back to the canonical short name.
// Warning logged if the English key is missing.
func resolveSlashName(bundle *i18n.Bundle, key, canonShortName string) string {
	slashKey := "slash." + key // "cmd.track" → "slash.cmd.track"
	if bundle == nil {
		log.Warnf("slash: nil bundle; using canonical %q for %s", canonShortName, slashKey)
		return canonShortName
	}
	en := bundle.For("en")
	if en == nil {
		log.Warnf("slash: English bundle missing; using canonical %q for %s", canonShortName, slashKey)
		return canonShortName
	}
	val := en.T(slashKey)
	// Translator.T returns the key itself when missing; treat that as absent.
	if val == "" || val == slashKey {
		log.Debugf("slash: missing English %s; falling back to canonical %q", slashKey, canonShortName)
		return canonShortName
	}
	if !validSlashName(val) {
		log.Warnf("slash: English %s = %q fails Discord name regex; using canonical %q", slashKey, val, canonShortName)
		return canonShortName
	}
	return val
}

// slashDescription returns the English description from "slash.desc.<short>".
func slashDescription(bundle *i18n.Bundle, key string) string {
	short := strings.TrimPrefix(key, "cmd.")
	descKey := "slash.desc." + short
	if bundle == nil {
		return ""
	}
	en := bundle.For("en")
	if en == nil {
		return ""
	}
	val := en.T(descKey)
	if val == "" || val == descKey {
		log.Debugf("slash: missing English description %s", descKey)
		return ""
	}
	return val
}

// slashNameLocalizations returns a *map[discordgo.Locale]string of localized
// command names, or nil when no localizations apply. Filled in by Task 43/44
// (Phase 6 localization). Returning nil here is correct: discordgo emits
// `name_localizations` only when non-nil.
func slashNameLocalizations(bundle *i18n.Bundle, key string) *map[discordgo.Locale]string {
	// TODO(Task 43/44): emit per-locale renamings from "slash.cmd.<short>" entries.
	_ = bundle
	_ = key
	return nil
}

// slashDescriptionLocalizations returns a *map[discordgo.Locale]string of
// localized descriptions, or nil when no localizations apply. Filled in by
// Task 43/44 (Phase 6 localization).
func slashDescriptionLocalizations(bundle *i18n.Bundle, key string) *map[discordgo.Locale]string {
	// TODO(Task 43/44): emit per-locale descriptions from "slash.desc.<short>" entries.
	_ = bundle
	_ = key
	return nil
}

// validSlashName checks that a slash command name matches Discord's regex.
// Real implementation lands in Task 43; the stub accepts anything 1..32 chars.
func validSlashName(s string) bool {
	// TODO(Task 43): replace with Discord's official regex
	// (^[-_\p{L}\p{N}\p{Devanagari}\p{Thai}]{1,32}$, lowercase-required).
	return len(s) >= 1 && len(s) <= 32
}

// AllDefinitions returns the slash command set this build supports, filtered
// by the operator's [discord.slash_commands] enable subset. Empty enable
// means "all commands this build supports". Exported for use by the
// coverage meta-test (Task 48).
//
// The `enable` list always uses canonical English short names ("track",
// "raid", ...) regardless of i18n renaming — so an operator's enable
// config stays valid across language changes.
func AllDefinitions(bundle *i18n.Bundle, enable []string) []*discordgo.ApplicationCommand {
	allEnabled := len(enable) == 0
	enableSet := make(map[string]bool, len(enable))
	for _, n := range enable {
		enableSet[n] = true
	}

	keys := allCommandKeys()
	defs := make([]*discordgo.ApplicationCommand, 0, len(keys))
	for _, key := range keys {
		canon := canonShortName(key)
		if !allEnabled && !enableSet[canon] {
			continue
		}
		def := buildCommandDef(bundle, key, canon)
		if def == nil {
			continue
		}
		defs = append(defs, def)
	}
	return defs
}

// buildCommandDef dispatches to the per-command builder by key. Returns nil
// for keys whose implementation has not landed yet — AllDefinitions skips nil.
func buildCommandDef(bundle *i18n.Bundle, key, canon string) *discordgo.ApplicationCommand {
	switch key {
	case "cmd.version":
		return buildDefinition(bundle, key, canon, nil)
	case "cmd.tracked":
		return buildDefinition(bundle, key, canon, nil)
	case "cmd.info":
		return buildDefinition(bundle, key, canon, nil)
	case "cmd.help":
		opts := []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "topic",
				Description:  "Help topic",
				Required:     false,
				Autocomplete: true,
			},
		}
		return buildDefinition(bundle, key, canon, opts)
	case "cmd.language":
		opts := []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "code",
				Description: "Language code",
				Required:    false,
				Choices:     languageChoices(bundle),
			},
		}
		return buildDefinition(bundle, key, canon, opts)
	// Other commands added in later phase tasks.
	}
	return nil
}

// languageChoices builds the sorted Discord choice list for the /language
// command's "code" option from the i18n bundle's loaded locales. The list
// reflects whatever languages are actually present in the running build —
// no hardcoded list to drift from reality.
func languageChoices(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOptionChoice {
	if bundle == nil {
		return nil
	}
	langs := bundle.LoadedLanguages()
	sort.Strings(langs)
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(langs))
	for _, l := range langs {
		out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: l, Value: l})
	}
	return out
}

// allCommandKeys lists every slash-command key this build supports.
// Used by AllDefinitions to walk and build the registered set, filtered by
// config.Enable.
func allCommandKeys() []string {
	return []string{
		// Phase 1
		"cmd.version",
		// Phase 2
		"cmd.tracked", "cmd.help", "cmd.info", "cmd.language",
		// Phase 4
		"cmd.track", "cmd.raid", "cmd.egg", "cmd.quest", "cmd.invasion",
		"cmd.lure", "cmd.nest", "cmd.maxbattle", "cmd.gym", "cmd.fort",
		"cmd.untrack",
		// Phase 5
		"cmd.area", "cmd.profile", "cmd.location",
	}
}

// canonShortName returns the canonical English short name for a command key.
// Always the canonical name — never the i18n-localized variant.
// Used for the enable allow-list and for slash dispatch routing.
func canonShortName(key string) string {
	return strings.TrimPrefix(key, "cmd.")
}
