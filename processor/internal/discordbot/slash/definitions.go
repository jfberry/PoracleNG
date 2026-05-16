package slash

import (
	"fmt"
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
// command names from the "slash.cmd.<short>" key across loaded locales, or
// nil when no translations apply. Returning nil is significant: discordgo
// emits `name_localizations` only when the pointer is non-nil, so this is
// how we shrink the registered payload when an operator only ships English.
//
// Name values are filtered through Discord's slash-name regex via
// localizationsForKey(validateName=true) so a translator's stray uppercase
// or whitespace doesn't reject the entire bulk-overwrite call.
func slashNameLocalizations(bundle *i18n.Bundle, key string) *map[discordgo.Locale]string {
	return localizationsForKey(bundle, "slash."+key, true /* validateName */)
}

// slashDescriptionLocalizations returns a *map[discordgo.Locale]string of
// localized descriptions from the "slash.desc.<short>" key. Same nil-vs-empty
// semantics as slashNameLocalizations: nil means "field omitted from payload".
//
// Descriptions are NOT validated against the name regex — Discord allows
// arbitrary 1..100-char description text, so we accept whatever the
// translator provides as long as it is non-empty.
func slashDescriptionLocalizations(bundle *i18n.Bundle, key string) *map[discordgo.Locale]string {
	short := strings.TrimPrefix(key, "cmd.")
	return localizationsForKey(bundle, "slash.desc."+short, false /* validateName */)
}

// AllDefinitions returns the slash command set this build supports, filtered
// by the operator's [discord.slash_commands] enable subset. Empty enable
// means "all commands this build supports". Exported for use by the
// coverage meta-test.
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
	case "cmd.track":
		return buildDefinition(bundle, key, canon, trackOptions())
	case "cmd.raid":
		return buildDefinition(bundle, key, canon, raidOptions())
	case "cmd.egg":
		return buildDefinition(bundle, key, canon, eggOptions())
	case "cmd.quest":
		return buildDefinition(bundle, key, canon, questOptions())
	case "cmd.invasion":
		return buildDefinition(bundle, key, canon, invasionOptions())
	case "cmd.incident":
		return buildDefinition(bundle, key, canon, incidentOptions())
	case "cmd.lure":
		return buildDefinition(bundle, key, canon, lureOptions())
	case "cmd.nest":
		return buildDefinition(bundle, key, canon, nestOptions())
	case "cmd.maxbattle":
		return buildDefinition(bundle, key, canon, maxbattleOptions())
	case "cmd.gym":
		return buildDefinition(bundle, key, canon, gymOptions())
	case "cmd.fort":
		return buildDefinition(bundle, key, canon, fortOptions())
	case "cmd.untrack":
		return buildDefinition(bundle, key, canon, untrackOptions())
	case "cmd.area":
		return buildDefinition(bundle, key, canon, areaOptions())
	case "cmd.profile":
		return buildDefinition(bundle, key, canon, profileOptions())
	case "cmd.location":
		return buildDefinition(bundle, key, canon, locationOptions())
	case "cmd.summary":
		return buildDefinition(bundle, key, canon, summaryOptions())
	}
	return nil
}

// untrackOptions exposes one sub-command per tracking type. Each sub-command
// has a single autocomplete-backed "tracking" option whose value is the
// database UID of the rule the user wants to remove. The sub-command name
// IS the tracking subtype — both the slash dispatcher (findUntrackSubtype)
// and the autocomplete lister read it directly to scope the choice list.
func untrackOptions() []*discordgo.ApplicationCommandOption {
	subs := []string{"pokemon", "raid", "egg", "quest", "invasion", "lure", "nest", "gym", "fort", "maxbattle"}
	out := make([]*discordgo.ApplicationCommandOption, 0, len(subs))
	for _, sub := range subs {
		out = append(out, untrackSub(sub))
	}
	return out
}

// untrackSub builds a single /untrack <subtype> sub-command. Keeping the
// shape identical across subtypes (one required "tracking" option, picker
// only) means there is exactly one Discord UI affordance per tracking type
// and the operator's mental model stays "pick one of your rules, click
// remove".
func untrackSub(typ string) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionSubCommand,
		Name:        typ,
		Description: "Remove a " + typ + " tracking rule",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "tracking",
				Description:  "Pick from your existing " + typ + " tracking rules",
				Required:     true,
				Autocomplete: true,
			},
		},
	}
}

// areaOptions exposes /area add, /area remove, /area list (every area the
// user could subscribe to, with ✓ marks for the ones they already have),
// /area overview (overview map of selected areas), and /area show (list of
// selected area names).
func areaOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "add",
			Description: "Add an area to your tracking",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "area",
					Description:  "Area to add",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "remove",
			Description: "Remove an area from your tracking",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "area",
					Description:  "Area to remove",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "show",
			Description: "Show your selected areas",
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "list",
			Description: "List every area available to you (✓ = already added)",
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "overview",
			Description: "Map overview of every area available to you",
		},
	}
}

// profileOptions exposes /profile list, change, create, delete, settime,
// cleartime, and copyto. settime/cleartime/copyto map to the text-bot's
// active-hours grammar — settime accepts a single string with the entire
// time-range expression (the bot's ParseSettimeArg tokenises commas itself).
func profileOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "list",
			Description: "List your profiles",
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "change",
			Description: "Switch active profile",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "name",
					Description:  "Profile name",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "create",
			Description: "Create a new profile",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "name",
					Description: "Profile name",
					Required:    true,
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "delete",
			Description: "Delete a profile",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "name",
					Description:  "Profile name",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "settime",
			Description: "Set active-hours for the current profile",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "times",
					Description: "Time-range string (e.g. mon07:30, weekday07:30-18:00, weekday:9-17/2)",
					Required:    true,
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "cleartime",
			Description: "Clear active-hours from the current profile",
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "copyto",
			Description: "Copy this profile's tracking to another profile",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "profile",
					Description:  "Target profile name or number",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
	}
}

// summaryOptions exposes /summary with one sub-command group per supported
// alertType (today: just `quest`). Each group has four sub-commands matching
// the text bot's grammar:
//
//	/summary quest show       → !summary quest               (status)
//	/summary quest settime    → !summary quest settime <times>
//	/summary quest cleartime  → !summary quest cleartime
//	/summary quest now        → !summary quest now           (force-dispatch)
//
// Discord's command tree limits depth to (Command → Group → Subcommand) =
// three levels including the command itself, so the alertType lives at the
// group level and the action at the subcommand level. Adding a second
// alertType later is "register another group alongside `quest`" — no
// schema change required.
func summaryOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionSubCommandGroup,
			Name:        "quest",
			Description: "Manage quest-summary buffering",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "show",
					Description: "Show schedule and current buffer count",
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "settime",
					Description: "Set the active-hours schedule for the quest summary",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        "times",
							Description: "Time-range string (e.g. mon07:30, weekday07:30-18:00, weekday:9-17/2)",
							Required:    true,
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "cleartime",
					Description: "Clear the active-hours schedule",
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "now",
					Description: "Force-dispatch the currently-buffered quests",
				},
			},
		},
	}
}

// locationOptions exposes /location with a single required "place" option
// that accepts either "lat,lon" coordinates or a free-form place name. The
// mapper forward-geocodes place names via deps.Geocoder when present.
func locationOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "place",
			Description: "Coordinates (\"51.28, 1.08\") or a place name",
			Required:    true,
		},
	}
}

// trackOptions builds the slash option list for /track. Pokemon is the
// only required option; everything else is filtered or omitted.
func trackOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "pokemon",
			Description:  "Pokemon to track (or 'everything')",
			Required:     true,
			Autocomplete: true,
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "iv",
			Description:  "IV filter (e.g. 100, 95, 0-0)",
			Autocomplete: true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "distance",
			Description: "Alert radius in metres",
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "great_rank",
			Description: "Top PVP rank in the Great League",
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "ultra_rank",
			Description: "Top PVP rank in the Ultra League",
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "little_rank",
			Description: "Top PVP rank in the Little League",
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "clean",
			Description: "Auto-delete the alert when the pokemon despawns",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "template",
			Description:  "DTS template name",
			Autocomplete: true,
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "form",
			Description:  "Pokemon form",
			Autocomplete: true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "size",
			Description: "Pokemon size class",
			Choices:     sizeChoices(),
		},
	}
}

// raidOptions builds the slash option list for /raid. Boss XOR level is
// enforced in the mapper rather than declaratively (Discord does not
// support mutual-exclusion natively on option groups).
func raidOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "boss",
			Description:  "Raid boss pokemon",
			Autocomplete: true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "level",
			Description: "Raid level",
			Choices:     raidLevelOptionChoices(),
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "team",
			Description: "Required gym-control team",
			Choices:     teamChoices(),
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "distance",
			Description: "Alert radius in metres",
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "clean",
			Description: "Auto-delete the alert when the raid expires",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "template",
			Description:  "DTS template name",
			Autocomplete: true,
		},
	}
}

// eggOptions builds the slash option list for /egg. Level is required.
func eggOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "level",
			Description: "Egg / raid level",
			Required:    true,
			Choices:     raidLevelOptionChoices(),
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "team",
			Description: "Required gym-control team",
			Choices:     teamChoices(),
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "distance",
			Description: "Alert radius in metres",
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "clean",
			Description: "Auto-delete the alert when the egg hatches",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "template",
			Description:  "DTS template name",
			Autocomplete: true,
		},
	}
}

// questOptions builds the slash option list for /quest. Reward types are
// mutually exclusive — enforced in the mapper rather than declaratively
// because Discord lacks option-group XOR semantics.
func questOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "pokemon",
			Description:  "Pokemon reward",
			Autocomplete: true,
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "item",
			Description:  "Item reward (e.g. golden razz berry)",
			Autocomplete: true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "stardust",
			Description: "Stardust reward (minimum amount)",
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "candy",
			Description:  "Candy reward pokemon",
			Autocomplete: true,
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "mega_energy",
			Description:  "Mega energy reward pokemon",
			Autocomplete: true,
		},
		// xl_candy intentionally omitted — matching/quest.go and
		// enrichment/quest.go handle reward types 2/3/4/7/12 only; XL
		// candy isn't wired anywhere so the option would never fire.
		{
			// min_amount applies only to reward types whose matcher
			// honours q.Amount (item=2, candy=4, mega_energy=12).
			// Pokemon (7) has no amount semantics and stardust (3)
			// stores its threshold in Reward via the dedicated
			// `stardust` integer option. The mapper rejects the
			// combination up-front so the user sees the validation
			// in the slash response rather than via an opaque
			// matcher failure.
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "min_amount",
			Description: "Minimum reward amount (item / candy / mega_energy only)",
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "distance",
			Description: "Alert radius in metres",
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "clean",
			Description: "Auto-delete the alert when the quest is completed",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "summary",
			Description: "Buffer matches and dispatch in a scheduled batch instead of alerting immediately",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "template",
			Description:  "DTS template name",
			Autocomplete: true,
		},
	}
}

// invasionOptions builds the slash option list for /invasion. grunt_type is
// required and autocomplete-served by autocomplete.Grunt (typed grunts +
// bosses + pokestop events). gender is an optional fixed-choice filter
// matching ParamGender on the text-bot side.
func invasionOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "grunt_type",
			Description:  "Grunt type (Fire, Water…), boss (Giovanni…), or incident (Kecleon…)",
			Required:     true,
			Autocomplete: true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "gender",
			Description: "Filter by grunt gender",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Male", Value: "male"},
				{Name: "Female", Value: "female"},
				{Name: "Genderless", Value: "genderless"},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "distance",
			Description: "Alert radius in metres",
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "clean",
			Description: "Auto-delete the alert when the invasion expires",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "template",
			Description:  "DTS template name",
			Autocomplete: true,
		},
	}
}

// incidentOptions builds the slash option list for /incident — the
// pokestop-event surface (Kecleon, Gold Pokestop, Showcase, Pokestop
// Spawn …) split out of /invasion grunt_type. Routes through the same
// cmd.invasion handler because cmd.incident is just an alias on the
// text-bot side; both write the same gruntType DB column.
func incidentOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "type",
			Description:  "Pokestop incident type (Kecleon, Gold Pokestop, Showcase…)",
			Required:     true,
			Autocomplete: true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "distance",
			Description: "Alert radius in metres",
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "clean",
			Description: "Auto-delete the alert when the incident expires",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "template",
			Description:  "DTS template name",
			Autocomplete: true,
		},
	}
}

// lureOptions builds the slash option list for /lure. lure_type is required.
func lureOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "lure_type",
			Description: "Lure type",
			Required:    true,
			Choices:     lureTypeChoices(),
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "distance",
			Description: "Alert radius in metres",
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "clean",
			Description: "Auto-delete the alert when the lure expires",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "template",
			Description:  "DTS template name",
			Autocomplete: true,
		},
	}
}

// nestOptions builds the slash option list for /nest. pokemon is required.
func nestOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "pokemon",
			Description:  "Nesting pokemon",
			Required:     true,
			Autocomplete: true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "min_spawn_avg",
			Description: "Minimum spawns per hour",
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "distance",
			Description: "Alert radius in metres",
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "clean",
			Description: "Auto-delete the alert when the nest expires",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "template",
			Description:  "DTS template name",
			Autocomplete: true,
		},
	}
}

// maxbattleOptions builds the slash option list for /maxbattle.
func maxbattleOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "pokemon",
			Description:  "Max battle pokemon",
			Required:     true,
			Autocomplete: true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "level",
			Description: "Battle level (1..6)",
			Choices:     maxbattleLevelChoices(),
		},
		{
			Type:        discordgo.ApplicationCommandOptionBoolean,
			Name:        "gmax",
			Description: "Gigantamax only",
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "distance",
			Description: "Alert radius in metres",
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "clean",
			Description: "Auto-delete the alert when the battle ends",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "template",
			Description:  "DTS template name",
			Autocomplete: true,
		},
	}
}

// gymOptions builds the slash option list for /gym. Team is optional —
// omitting it tracks all teams.
func gymOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "team",
			Description: "Gym-control team to alert on",
			Choices:     teamChoices(),
		},
		{
			Type:        discordgo.ApplicationCommandOptionBoolean,
			Name:        "slot_changes",
			Description: "Alert on slot composition changes",
		},
		{
			Type:        discordgo.ApplicationCommandOptionBoolean,
			Name:        "battle_changes",
			Description: "Alert on battle state changes",
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "distance",
			Description: "Alert radius in metres",
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "clean",
			Description: "Auto-delete the alert when the gym state changes",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "template",
			Description:  "DTS template name",
			Autocomplete: true,
		},
	}
}

// fortOptions builds the slash option list for /fort. fort_type is required.
func fortOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "fort_type",
			Description: "Fort type to track",
			Required:    true,
			Choices:     fortTypeChoices(),
		},
		{
			Type:        discordgo.ApplicationCommandOptionBoolean,
			Name:        "include_empty",
			Description: "Alert on empty/unowned forts",
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "distance",
			Description: "Alert radius in metres",
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "clean",
			Description: "Auto-delete the alert when the fort changes again",
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Yes", Value: "yes"},
			},
		},
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "template",
			Description:  "DTS template name",
			Autocomplete: true,
		},
	}
}

// lureTypeChoices maps the slash /lure lure_type option to the lure-name
// keywords accepted by the bot's argmatch.go (arg.normal..arg.sparkly).
func lureTypeChoices() []*discordgo.ApplicationCommandOptionChoice {
	return []*discordgo.ApplicationCommandOptionChoice{
		{Name: "Normal", Value: "normal"},
		{Name: "Glacial", Value: "glacial"},
		{Name: "Mossy", Value: "mossy"},
		{Name: "Magnetic", Value: "magnetic"},
		{Name: "Rainy", Value: "rainy"},
		{Name: "Sparkly", Value: "sparkly"},
	}
}

// maxbattleLevelChoices enumerates battle tiers 1..6. The slash form is
// stricter than the bot (which accepts any int), but the canonical set
// of supported max battle tiers in the game today is 1..6.
func maxbattleLevelChoices() []*discordgo.ApplicationCommandOptionChoice {
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 6)
	for i := 1; i <= 6; i++ {
		out = append(out, &discordgo.ApplicationCommandOptionChoice{
			Name:  fmt.Sprintf("Level %d", i),
			Value: i,
		})
	}
	return out
}

// fortTypeChoices maps the slash /fort fort_type integer to the canonical
// English fort-type keyword via fortTypeName in mappers/common.go.
func fortTypeChoices() []*discordgo.ApplicationCommandOptionChoice {
	return []*discordgo.ApplicationCommandOptionChoice{
		{Name: "Pokestop", Value: 0},
		{Name: "Gym", Value: 1},
	}
}

// raidLevelChoice is the static (label, value) pair backing the raid
// level dropdown. Value strings are what the text bot expects as tokens.
type raidLevelChoice struct{ Label, Value string }

var raidLevelChoices = []raidLevelChoice{
	{"Tier 1", "1"},
	{"Tier 3", "3"},
	{"Tier 5", "5"},
	{"Tier 6", "6"},
	{"Mega", "mega"},
	{"Legendary", "legendary"},
	{"Shadow Tier 1", "shadow1"},
	{"Shadow Tier 3", "shadow3"},
	{"Shadow Tier 5", "shadow5"},
	{"Ultra Beast", "ultra beast"},
	{"Everything", "everything"},
}

// raidLevelOptionChoices builds the Discord choice slice for the raid level
// option. Shared by /raid and /egg.
func raidLevelOptionChoices() []*discordgo.ApplicationCommandOptionChoice {
	out := make([]*discordgo.ApplicationCommandOptionChoice, len(raidLevelChoices))
	for i, c := range raidLevelChoices {
		out[i] = &discordgo.ApplicationCommandOptionChoice{Name: c.Label, Value: c.Value}
	}
	return out
}

// teamChoices builds the Discord choice slice for the gym-control team
// option. Values match the canonical team IDs in argmatch.go.
func teamChoices() []*discordgo.ApplicationCommandOptionChoice {
	return []*discordgo.ApplicationCommandOptionChoice{
		{Name: "Harmony", Value: 0},
		{Name: "Mystic", Value: 1},
		{Name: "Valor", Value: 2},
		{Name: "Instinct", Value: 3},
	}
}

// sizeChoices builds the Discord choice slice for the pokemon size option.
// "all" is the explicit catch-all that omits a size token from the rendered
// text command, matching the text bot's "no size filter" default.
func sizeChoices() []*discordgo.ApplicationCommandOptionChoice {
	return []*discordgo.ApplicationCommandOptionChoice{
		{Name: "All sizes", Value: "all"},
		{Name: "XXS", Value: "xxs"},
		{Name: "XS", Value: "xs"},
		{Name: "M", Value: "m"},
		{Name: "XL", Value: "xl"},
		{Name: "XXL", Value: "xxl"},
	}
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
		"cmd.version",
		"cmd.tracked", "cmd.help", "cmd.info", "cmd.language",
		"cmd.track", "cmd.raid", "cmd.egg", "cmd.quest", "cmd.invasion",
		"cmd.incident",
		"cmd.lure", "cmd.nest", "cmd.maxbattle", "cmd.gym", "cmd.fort",
		"cmd.untrack",
		"cmd.area", "cmd.profile", "cmd.location",
		"cmd.summary",
	}
}

// canonShortName returns the canonical English short name for a command key.
// Always the canonical name — never the i18n-localized variant.
// Used for the enable allow-list and for slash dispatch routing.
func canonShortName(key string) string {
	return strings.TrimPrefix(key, "cmd.")
}
