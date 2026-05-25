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
		return buildDefinition(bundle, key, canon, infoOptions(bundle))
	case "cmd.start":
		return buildDefinition(bundle, key, canon, nil)
	case "cmd.stop":
		return buildDefinition(bundle, key, canon, nil)
	case "cmd.help":
		name, nameLoc := optName(bundle, "help.topic", "topic")
		desc, descLoc := optDesc(bundle, "help.topic", "Help topic")
		opts := []*discordgo.ApplicationCommandOption{
			{
				Type:                     discordgo.ApplicationCommandOptionString,
				Name:                     name,
				NameLocalizations:        nameLoc,
				Description:              desc,
				DescriptionLocalizations: descLoc,
				Required:                 false,
				Autocomplete:             true,
			},
		}
		return buildDefinition(bundle, key, canon, opts)
	case "cmd.language":
		name, nameLoc := optName(bundle, "language.code", "code")
		desc, descLoc := optDesc(bundle, "language.code", "Language code")
		opts := []*discordgo.ApplicationCommandOption{
			{
				Type:                     discordgo.ApplicationCommandOptionString,
				Name:                     name,
				NameLocalizations:        nameLoc,
				Description:              desc,
				DescriptionLocalizations: descLoc,
				Required:                 false,
				Choices:                  languageChoices(bundle),
			},
		}
		return buildDefinition(bundle, key, canon, opts)
	case "cmd.track":
		return buildDefinition(bundle, key, canon, trackOptions(bundle))
	case "cmd.raid":
		return buildDefinition(bundle, key, canon, raidOptions(bundle))
	case "cmd.egg":
		return buildDefinition(bundle, key, canon, eggOptions(bundle))
	case "cmd.quest":
		return buildDefinition(bundle, key, canon, questOptions(bundle))
	case "cmd.invasion":
		return buildDefinition(bundle, key, canon, invasionOptions(bundle))
	case "cmd.incident":
		return buildDefinition(bundle, key, canon, incidentOptions(bundle))
	case "cmd.lure":
		return buildDefinition(bundle, key, canon, lureOptions(bundle))
	case "cmd.nest":
		return buildDefinition(bundle, key, canon, nestOptions(bundle))
	case "cmd.maxbattle":
		return buildDefinition(bundle, key, canon, maxbattleOptions(bundle))
	case "cmd.gym":
		return buildDefinition(bundle, key, canon, gymOptions(bundle))
	case "cmd.fort":
		return buildDefinition(bundle, key, canon, fortOptions(bundle))
	case "cmd.untrack":
		return buildDefinition(bundle, key, canon, untrackOptions(bundle))
	case "cmd.area":
		return buildDefinition(bundle, key, canon, areaOptions(bundle))
	case "cmd.profile":
		return buildDefinition(bundle, key, canon, profileOptions(bundle))
	case "cmd.location":
		return buildDefinition(bundle, key, canon, locationOptions(bundle))
	case "cmd.summary":
		return buildDefinition(bundle, key, canon, summaryOptions(bundle))
	}
	return nil
}

// untrackOptions exposes one sub-command per tracking type. Each sub-command
// has a single autocomplete-backed "tracking" option whose value is the
// database UID of the rule the user wants to remove. The sub-command name
// IS the tracking subtype — both the slash dispatcher (findUntrackSubtype)
// and the autocomplete lister read it directly to scope the choice list.
func untrackOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	subs := []string{"pokemon", "raid", "egg", "quest", "invasion", "lure", "nest", "gym", "fort", "maxbattle"}
	out := make([]*discordgo.ApplicationCommandOption, 0, len(subs))
	for _, sub := range subs {
		out = append(out, untrackSub(bundle, sub))
	}
	return out
}

// untrackSub builds a single /untrack <subtype> sub-command. Keeping the
// shape identical across subtypes (one required "tracking" option, picker
// only) means there is exactly one Discord UI affordance per tracking type
// and the operator's mental model stays "pick one of your rules, click
// remove".
func untrackSub(bundle *i18n.Bundle, typ string) *discordgo.ApplicationCommandOption {
	subName, subNameLoc := optName(bundle, "untrack."+typ, typ)
	subDesc, subDescLoc := optDesc(bundle, "untrack."+typ, "Remove a "+typ+" tracking rule")
	trackingName, trackingNameLoc := optName(bundle, "untrack."+typ+".tracking", "tracking")
	trackingDesc, trackingDescLoc := optDesc(bundle, "untrack."+typ+".tracking", "Pick from your existing "+typ+" tracking rules")
	return &discordgo.ApplicationCommandOption{
		Type:                     discordgo.ApplicationCommandOptionSubCommand,
		Name:                     subName,
		NameLocalizations:        subNameLoc,
		Description:              subDesc,
		DescriptionLocalizations: subDescLoc,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:                     discordgo.ApplicationCommandOptionString,
				Name:                     trackingName,
				NameLocalizations:        trackingNameLoc,
				Description:              trackingDesc,
				DescriptionLocalizations: trackingDescLoc,
				Required:                 true,
				Autocomplete:             true,
			},
		},
	}
}

// infoOptions exposes /info pokemon, /info rarity, /info shiny, and
// /info weather as sub-commands. The corresponding text-command sub-
// vocabulary lives in msg.info.sub.* — the mapper translates each
// slash invocation back into the canonical English keyword (or pokemon
// name) that InfoCommand.Run reads. Admin-only sub-commands from the
// text surface (translate, dts, templates) are intentionally omitted
// here; admins still have them via the text command.
func infoOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		subCommand(bundle, "info.pokemon", "pokemon", "Show pokemon info",
			stringOpt(bundle, "info.pokemon.name", "name", "Pokemon name or dex id", true, false)),
		subCommand(bundle, "info.rarity", "rarity", "Show rarity tiers"),
		subCommand(bundle, "info.shiny", "shiny", "Show shiny rates"),
		subCommand(bundle, "info.weather", "weather", "Show current weather",
			stringOpt(bundle, "info.weather.coords", "coords", "lat,lon (optional)", false, false)),
	}
}

// areaOptions exposes /area add, /area remove, /area list (every area the
// user could subscribe to, with ✓ marks for the ones they already have),
// /area overview (overview map of selected areas), and /area show (list of
// selected area names).
func areaOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		subCommand(bundle, "area.add", "add", "Add an area to your tracking",
			stringOpt(bundle, "area.add.area", "area", "Area to add", true, true)),
		subCommand(bundle, "area.remove", "remove", "Remove an area from your tracking",
			stringOpt(bundle, "area.remove.area", "area", "Area to remove", true, true)),
		subCommand(bundle, "area.show", "show", "Show your selected areas"),
		subCommand(bundle, "area.list", "list", "List every area available to you (✓ = already added)"),
		subCommand(bundle, "area.overview", "overview", "Map overview of every area available to you"),
	}
}

// subCommand builds an ApplicationCommandOptionSubCommand with i18n-sourced
// name + description (slash.opt.<key> + slash.opt.<key>.desc). Variadic
// children let callers list any inner options inline.
func subCommand(bundle *i18n.Bundle, key, canonName, canonDesc string, children ...*discordgo.ApplicationCommandOption) *discordgo.ApplicationCommandOption {
	name, nameLoc := optName(bundle, key, canonName)
	desc, descLoc := optDesc(bundle, key, canonDesc)
	var opts []*discordgo.ApplicationCommandOption
	if len(children) > 0 {
		opts = children
	}
	return &discordgo.ApplicationCommandOption{
		Type:                     discordgo.ApplicationCommandOptionSubCommand,
		Name:                     name,
		NameLocalizations:        nameLoc,
		Description:              desc,
		DescriptionLocalizations: descLoc,
		Options:                  opts,
	}
}

// stringOpt builds an ApplicationCommandOptionString with i18n-sourced
// name + description and the supplied required/autocomplete flags.
func stringOpt(bundle *i18n.Bundle, key, canonName, canonDesc string, required, autocomplete bool) *discordgo.ApplicationCommandOption {
	name, nameLoc := optName(bundle, key, canonName)
	desc, descLoc := optDesc(bundle, key, canonDesc)
	return &discordgo.ApplicationCommandOption{
		Type:                     discordgo.ApplicationCommandOptionString,
		Name:                     name,
		NameLocalizations:        nameLoc,
		Description:              desc,
		DescriptionLocalizations: descLoc,
		Required:                 required,
		Autocomplete:             autocomplete,
	}
}

// intOpt builds an ApplicationCommandOptionInteger with i18n-sourced name +
// description. Numeric options don't autocomplete.
func intOpt(bundle *i18n.Bundle, key, canonName, canonDesc string, required bool) *discordgo.ApplicationCommandOption {
	name, nameLoc := optName(bundle, key, canonName)
	desc, descLoc := optDesc(bundle, key, canonDesc)
	return &discordgo.ApplicationCommandOption{
		Type:                     discordgo.ApplicationCommandOptionInteger,
		Name:                     name,
		NameLocalizations:        nameLoc,
		Description:              desc,
		DescriptionLocalizations: descLoc,
		Required:                 required,
	}
}

// boolOpt builds an ApplicationCommandOptionBoolean with i18n-sourced name +
// description.
func boolOpt(bundle *i18n.Bundle, key, canonName, canonDesc string) *discordgo.ApplicationCommandOption {
	name, nameLoc := optName(bundle, key, canonName)
	desc, descLoc := optDesc(bundle, key, canonDesc)
	return &discordgo.ApplicationCommandOption{
		Type:                     discordgo.ApplicationCommandOptionBoolean,
		Name:                     name,
		NameLocalizations:        nameLoc,
		Description:              desc,
		DescriptionLocalizations: descLoc,
	}
}

// cleanOpt builds the standard "clean" choice option (single Yes choice).
// canonDesc is the per-command English description ("Auto-delete the alert
// when the pokemon despawns" etc.); each call site supplies its own wording.
func cleanOpt(bundle *i18n.Bundle, cmdKey, canonDesc string) *discordgo.ApplicationCommandOption {
	name, nameLoc := optName(bundle, cmdKey+".clean", "clean")
	desc, descLoc := optDesc(bundle, cmdKey+".clean", canonDesc)
	yesName, yesLoc := choiceName(bundle, cmdKey+".clean.yes", "Yes")
	return &discordgo.ApplicationCommandOption{
		Type:                     discordgo.ApplicationCommandOptionString,
		Name:                     name,
		NameLocalizations:        nameLoc,
		Description:              desc,
		DescriptionLocalizations: descLoc,
		Choices: []*discordgo.ApplicationCommandOptionChoice{
			{Name: yesName, NameLocalizations: yesLoc, Value: "yes"},
		},
	}
}

// distanceOpt builds the standard "distance" integer option used by every
// tracking command. The English description is consistent; only the i18n key
// prefix changes per command.
func distanceOpt(bundle *i18n.Bundle, cmdKey string) *discordgo.ApplicationCommandOption {
	return intOpt(bundle, cmdKey+".distance", "distance", "Alert radius in metres", false)
}

// templateOpt builds the standard "template" string option used by every
// tracking command. Autocomplete is always on (DTS template name lookup).
func templateOpt(bundle *i18n.Bundle, cmdKey string) *discordgo.ApplicationCommandOption {
	return stringOpt(bundle, cmdKey+".template", "template", "DTS template name", false, true)
}

// profileOptions exposes /profile list, change, create, delete, settime,
// cleartime, and copyto. settime/cleartime/copyto map to the text-bot's
// active-hours grammar — settime accepts a single string with the entire
// time-range expression (the bot's ParseSettimeArg tokenises commas itself).
func profileOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		subCommand(bundle, "profile.list", "list", "List your profiles"),
		subCommand(bundle, "profile.change", "change", "Switch active profile",
			stringOpt(bundle, "profile.change.name", "name", "Profile name", true, true)),
		subCommand(bundle, "profile.create", "create", "Create a new profile",
			stringOpt(bundle, "profile.create.name", "name", "Profile name", true, false)),
		subCommand(bundle, "profile.delete", "delete", "Delete a profile",
			stringOpt(bundle, "profile.delete.name", "name", "Profile name", true, true)),
		subCommand(bundle, "profile.settime", "settime", "Set active-hours for the current profile",
			stringOpt(bundle, "profile.settime.times", "times", "Time-range string (e.g. mon07:30, weekday07:30-18:00, weekday:9-17/2)", true, false)),
		subCommand(bundle, "profile.cleartime", "cleartime", "Clear active-hours from the current profile"),
		subCommand(bundle, "profile.copyto", "copyto", "Copy this profile's tracking to another profile",
			stringOpt(bundle, "profile.copyto.profile", "profile", "Target profile name or number", true, true)),
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
func summaryOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	questGroupName, questGroupNameLoc := optName(bundle, "summary.quest", "quest")
	questGroupDesc, questGroupDescLoc := optDesc(bundle, "summary.quest", "Manage quest-summary buffering")
	return []*discordgo.ApplicationCommandOption{
		{
			Type:                     discordgo.ApplicationCommandOptionSubCommandGroup,
			Name:                     questGroupName,
			NameLocalizations:        questGroupNameLoc,
			Description:              questGroupDesc,
			DescriptionLocalizations: questGroupDescLoc,
			Options: []*discordgo.ApplicationCommandOption{
				subCommand(bundle, "summary.quest.show", "show", "Show schedule and current buffer count"),
				subCommand(bundle, "summary.quest.settime", "settime", "Set the active-hours schedule for the quest summary",
					stringOpt(bundle, "summary.quest.settime.times", "times", "Time-range string (e.g. mon07:30, weekday07:30-18:00, weekday:9-17/2)", true, false)),
				subCommand(bundle, "summary.quest.cleartime", "cleartime", "Clear the active-hours schedule"),
				subCommand(bundle, "summary.quest.now", "now", "Force-dispatch the currently-buffered quests"),
			},
		},
	}
}

// locationOptions exposes /location with six sub-commands that mirror the
// text command's grammar:
//
//	add <name> <place>  — save a named location (geocoding done by text cmd)
//	list                — list all saved locations
//	show <name>         — show one saved location (autocomplete)
//	remove <name>       — remove a saved location or "default" (autocomplete)
//	set-default         — placeholder (no place arg yet; exists for discoverability)
//	remove-default      — clear the default lat/lon location
func locationOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		subCommand(bundle, "location.add", "add", "Save a named location",
			stringOpt(bundle, "location.add.name", "name", "Short name for this location (e.g. Home)", true, false),
			stringOpt(bundle, "location.add.place", "place", "Coordinates (lat,lon) or a place name", true, false)),
		subCommand(bundle, "location.list", "list", "List your saved locations"),
		subCommand(bundle, "location.show", "show", "Show one saved location",
			stringOpt(bundle, "location.show.name", "name", "Saved-location name", true, true)),
		subCommand(bundle, "location.remove", "remove", "Remove a saved location",
			stringOpt(bundle, "location.remove.name", "name", "Saved-location name (or \"default\" to clear your default location)", true, true)),
		subCommand(bundle, "location.set-default", "set-default", "Set your default location (use /location add first)"),
		subCommand(bundle, "location.remove-default", "remove-default", "Clear your default location"),
	}
}

// trackOptions builds the slash option list for /track. Pokemon is the
// only required option; everything else is filtered or omitted.
func trackOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	sizeName, sizeNameLoc := optName(bundle, "track.size", "size")
	sizeDesc, sizeDescLoc := optDesc(bundle, "track.size", "Pokemon size class")
	return []*discordgo.ApplicationCommandOption{
		stringOpt(bundle, "track.pokemon", "pokemon", "Pokemon to track (or 'everything')", true, true),
		stringOpt(bundle, "track.iv", "iv", "IV filter (e.g. 100, 95, 0-0)", false, true),
		distanceOpt(bundle, "track"),
		intOpt(bundle, "track.great_rank", "great_rank", "Top PVP rank in the Great League", false),
		intOpt(bundle, "track.ultra_rank", "ultra_rank", "Top PVP rank in the Ultra League", false),
		intOpt(bundle, "track.little_rank", "little_rank", "Top PVP rank in the Little League", false),
		cleanOpt(bundle, "track", "Auto-delete the alert when the pokemon despawns"),
		templateOpt(bundle, "track"),
		stringOpt(bundle, "track.form", "form", "Pokemon form", false, true),
		{
			Type:                     discordgo.ApplicationCommandOptionString,
			Name:                     sizeName,
			NameLocalizations:        sizeNameLoc,
			Description:              sizeDesc,
			DescriptionLocalizations: sizeDescLoc,
			Choices:                  sizeChoices(bundle),
		},
	}
}

// raidOptions builds the slash option list for /raid. Boss XOR level is
// enforced in the mapper rather than declaratively (Discord does not
// support mutual-exclusion natively on option groups).
func raidOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	levelName, levelNameLoc := optName(bundle, "raid.level", "level")
	levelDesc, levelDescLoc := optDesc(bundle, "raid.level", "Raid level")
	teamName, teamNameLoc := optName(bundle, "raid.team", "team")
	teamDesc, teamDescLoc := optDesc(bundle, "raid.team", "Required gym-control team")
	return []*discordgo.ApplicationCommandOption{
		stringOpt(bundle, "raid.boss", "boss", "Raid boss pokemon", false, true),
		{
			Type:                     discordgo.ApplicationCommandOptionString,
			Name:                     levelName,
			NameLocalizations:        levelNameLoc,
			Description:              levelDesc,
			DescriptionLocalizations: levelDescLoc,
			Choices:                  raidLevelOptionChoices(bundle, "raid"),
		},
		{
			Type:                     discordgo.ApplicationCommandOptionInteger,
			Name:                     teamName,
			NameLocalizations:        teamNameLoc,
			Description:              teamDesc,
			DescriptionLocalizations: teamDescLoc,
			Choices:                  teamChoices(bundle, "raid"),
		},
		distanceOpt(bundle, "raid"),
		cleanOpt(bundle, "raid", "Auto-delete the alert when the raid expires"),
		templateOpt(bundle, "raid"),
	}
}

// eggOptions builds the slash option list for /egg. Level is required.
func eggOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	levelName, levelNameLoc := optName(bundle, "egg.level", "level")
	levelDesc, levelDescLoc := optDesc(bundle, "egg.level", "Egg / raid level")
	teamName, teamNameLoc := optName(bundle, "egg.team", "team")
	teamDesc, teamDescLoc := optDesc(bundle, "egg.team", "Required gym-control team")
	return []*discordgo.ApplicationCommandOption{
		{
			Type:                     discordgo.ApplicationCommandOptionString,
			Name:                     levelName,
			NameLocalizations:        levelNameLoc,
			Description:              levelDesc,
			DescriptionLocalizations: levelDescLoc,
			Required:                 true,
			Choices:                  raidLevelOptionChoices(bundle, "egg"),
		},
		{
			Type:                     discordgo.ApplicationCommandOptionInteger,
			Name:                     teamName,
			NameLocalizations:        teamNameLoc,
			Description:              teamDesc,
			DescriptionLocalizations: teamDescLoc,
			Choices:                  teamChoices(bundle, "egg"),
		},
		distanceOpt(bundle, "egg"),
		cleanOpt(bundle, "egg", "Auto-delete the alert when the egg hatches"),
		templateOpt(bundle, "egg"),
	}
}

// questOptions builds the slash option list for /quest. Reward types are
// mutually exclusive — enforced in the mapper rather than declaratively
// because Discord lacks option-group XOR semantics.
func questOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	// /quest summary mirrors the clean-Yes shape but carries different
	// semantics (buffer + scheduled dispatch). Built inline rather than
	// via cleanOpt so the key prefix stays consistent.
	summaryName, summaryNameLoc := optName(bundle, "quest.summary", "summary")
	summaryDesc, summaryDescLoc := optDesc(bundle, "quest.summary", "Buffer matches and dispatch in a scheduled batch instead of alerting immediately")
	summaryYesName, summaryYesLoc := choiceName(bundle, "quest.summary.yes", "Yes")
	return []*discordgo.ApplicationCommandOption{
		stringOpt(bundle, "quest.pokemon", "pokemon", "Pokemon reward", false, true),
		stringOpt(bundle, "quest.item", "item", "Item reward (e.g. golden razz berry)", false, true),
		intOpt(bundle, "quest.stardust", "stardust", "Stardust reward (minimum amount)", false),
		stringOpt(bundle, "quest.candy", "candy", "Candy reward pokemon", false, true),
		stringOpt(bundle, "quest.mega_energy", "mega_energy", "Mega energy reward pokemon", false, true),
		// xl_candy intentionally omitted — matching/quest.go and
		// enrichment/quest.go handle reward types 2/3/4/7/12 only; XL
		// candy isn't wired anywhere so the option would never fire.
		//
		// min_amount applies only to reward types whose matcher
		// honours q.Amount (item=2, candy=4, mega_energy=12).
		// Pokemon (7) has no amount semantics and stardust (3)
		// stores its threshold in Reward via the dedicated
		// `stardust` integer option. The mapper rejects the
		// combination up-front so the user sees the validation
		// in the slash response rather than via an opaque
		// matcher failure.
		intOpt(bundle, "quest.min_amount", "min_amount", "Minimum reward amount (item / candy / mega_energy only)", false),
		distanceOpt(bundle, "quest"),
		cleanOpt(bundle, "quest", "Auto-delete the alert when the quest is completed"),
		{
			Type:                     discordgo.ApplicationCommandOptionString,
			Name:                     summaryName,
			NameLocalizations:        summaryNameLoc,
			Description:              summaryDesc,
			DescriptionLocalizations: summaryDescLoc,
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: summaryYesName, NameLocalizations: summaryYesLoc, Value: "yes"},
			},
		},
		templateOpt(bundle, "quest"),
	}
}

// invasionOptions builds the slash option list for /invasion. grunt_type is
// required and autocomplete-served by autocomplete.Grunt (typed grunts +
// bosses + pokestop events). gender is an optional fixed-choice filter
// matching ParamGender on the text-bot side.
func invasionOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	genderName, genderNameLoc := optName(bundle, "invasion.gender", "gender")
	genderDesc, genderDescLoc := optDesc(bundle, "invasion.gender", "Filter by grunt gender")
	maleName, maleLoc := choiceName(bundle, "invasion.gender.male", "Male")
	femaleName, femaleLoc := choiceName(bundle, "invasion.gender.female", "Female")
	genderlessName, genderlessLoc := choiceName(bundle, "invasion.gender.genderless", "Genderless")
	return []*discordgo.ApplicationCommandOption{
		stringOpt(bundle, "invasion.grunt_type", "grunt_type", "Grunt type (Fire, Water…), boss (Giovanni…), or incident (Kecleon…)", true, true),
		{
			Type:                     discordgo.ApplicationCommandOptionString,
			Name:                     genderName,
			NameLocalizations:        genderNameLoc,
			Description:              genderDesc,
			DescriptionLocalizations: genderDescLoc,
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: maleName, NameLocalizations: maleLoc, Value: "male"},
				{Name: femaleName, NameLocalizations: femaleLoc, Value: "female"},
				{Name: genderlessName, NameLocalizations: genderlessLoc, Value: "genderless"},
			},
		},
		distanceOpt(bundle, "invasion"),
		cleanOpt(bundle, "invasion", "Auto-delete the alert when the invasion expires"),
		templateOpt(bundle, "invasion"),
	}
}

// incidentOptions builds the slash option list for /incident — the
// pokestop-event surface (Kecleon, Gold Pokestop, Showcase, Pokestop
// Spawn …) split out of /invasion grunt_type. Routes through the same
// cmd.invasion handler because cmd.incident is just an alias on the
// text-bot side; both write the same gruntType DB column.
func incidentOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		stringOpt(bundle, "incident.type", "type", "Pokestop incident type (Kecleon, Gold Pokestop, Showcase…)", true, true),
		distanceOpt(bundle, "incident"),
		cleanOpt(bundle, "incident", "Auto-delete the alert when the incident expires"),
		templateOpt(bundle, "incident"),
	}
}

// lureOptions builds the slash option list for /lure. lure_type is required.
func lureOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	lureTypeName, lureTypeNameLoc := optName(bundle, "lure.lure_type", "lure_type")
	lureTypeDesc, lureTypeDescLoc := optDesc(bundle, "lure.lure_type", "Lure type")
	return []*discordgo.ApplicationCommandOption{
		{
			Type:                     discordgo.ApplicationCommandOptionString,
			Name:                     lureTypeName,
			NameLocalizations:        lureTypeNameLoc,
			Description:              lureTypeDesc,
			DescriptionLocalizations: lureTypeDescLoc,
			Required:                 true,
			Choices:                  lureTypeChoices(bundle),
		},
		distanceOpt(bundle, "lure"),
		cleanOpt(bundle, "lure", "Auto-delete the alert when the lure expires"),
		templateOpt(bundle, "lure"),
	}
}

// nestOptions builds the slash option list for /nest. pokemon is required.
func nestOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		stringOpt(bundle, "nest.pokemon", "pokemon", "Nesting pokemon", true, true),
		intOpt(bundle, "nest.min_spawn_avg", "min_spawn_avg", "Minimum spawns per hour", false),
		distanceOpt(bundle, "nest"),
		cleanOpt(bundle, "nest", "Auto-delete the alert when the nest expires"),
		templateOpt(bundle, "nest"),
	}
}

// maxbattleOptions builds the slash option list for /maxbattle.
func maxbattleOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	levelName, levelNameLoc := optName(bundle, "maxbattle.level", "level")
	levelDesc, levelDescLoc := optDesc(bundle, "maxbattle.level", "Battle level (1..6)")
	return []*discordgo.ApplicationCommandOption{
		stringOpt(bundle, "maxbattle.pokemon", "pokemon", "Max battle pokemon", true, true),
		{
			Type:                     discordgo.ApplicationCommandOptionInteger,
			Name:                     levelName,
			NameLocalizations:        levelNameLoc,
			Description:              levelDesc,
			DescriptionLocalizations: levelDescLoc,
			Choices:                  maxbattleLevelChoices(bundle),
		},
		boolOpt(bundle, "maxbattle.gmax", "gmax", "Gigantamax only"),
		distanceOpt(bundle, "maxbattle"),
		cleanOpt(bundle, "maxbattle", "Auto-delete the alert when the battle ends"),
		templateOpt(bundle, "maxbattle"),
	}
}

// gymOptions builds the slash option list for /gym. Team is optional —
// omitting it tracks all teams.
func gymOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	teamName, teamNameLoc := optName(bundle, "gym.team", "team")
	teamDesc, teamDescLoc := optDesc(bundle, "gym.team", "Gym-control team to alert on")
	return []*discordgo.ApplicationCommandOption{
		{
			Type:                     discordgo.ApplicationCommandOptionInteger,
			Name:                     teamName,
			NameLocalizations:        teamNameLoc,
			Description:              teamDesc,
			DescriptionLocalizations: teamDescLoc,
			Choices:                  teamChoices(bundle, "gym"),
		},
		boolOpt(bundle, "gym.slot_changes", "slot_changes", "Alert on slot composition changes"),
		boolOpt(bundle, "gym.battle_changes", "battle_changes", "Alert on battle state changes"),
		distanceOpt(bundle, "gym"),
		cleanOpt(bundle, "gym", "Auto-delete the alert when the gym state changes"),
		templateOpt(bundle, "gym"),
	}
}

// fortOptions builds the slash option list for /fort. fort_type is required.
func fortOptions(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOption {
	fortTypeName, fortTypeNameLoc := optName(bundle, "fort.fort_type", "fort_type")
	fortTypeDesc, fortTypeDescLoc := optDesc(bundle, "fort.fort_type", "Fort type to track")
	return []*discordgo.ApplicationCommandOption{
		{
			Type:                     discordgo.ApplicationCommandOptionInteger,
			Name:                     fortTypeName,
			NameLocalizations:        fortTypeNameLoc,
			Description:              fortTypeDesc,
			DescriptionLocalizations: fortTypeDescLoc,
			Required:                 true,
			Choices:                  fortTypeChoices(bundle),
		},
		boolOpt(bundle, "fort.include_empty", "include_empty", "Alert on empty/unowned forts"),
		distanceOpt(bundle, "fort"),
		cleanOpt(bundle, "fort", "Auto-delete the alert when the fort changes again"),
		templateOpt(bundle, "fort"),
	}
}

// choiceDef is the static (value, English label) pair feeding choiceList.
// Value stays canonical English regardless of i18n because the slash mapper
// and text bot resolve by Value, not Name.
type choiceDef struct {
	Value    any
	Fallback string
}

// choiceList builds a localized choice slice. optionKey is the dotted
// "<cmd>.<option>" path; each choice reads slash.choice.<optionKey>.<value>.
// Value is stringified via fmt.Sprint for the key suffix — integer values
// like the team IDs become "0", "1", ... in the key.
func choiceList(bundle *i18n.Bundle, optionKey string, choices []choiceDef) []*discordgo.ApplicationCommandOptionChoice {
	out := make([]*discordgo.ApplicationCommandOptionChoice, len(choices))
	for i, c := range choices {
		valueStr := fmt.Sprint(c.Value)
		name, loc := choiceName(bundle, optionKey+"."+valueStr, c.Fallback)
		out[i] = &discordgo.ApplicationCommandOptionChoice{
			Name:              name,
			NameLocalizations: loc,
			Value:             c.Value,
		}
	}
	return out
}

// lureTypeChoices maps the slash /lure lure_type option to the lure-name
// keywords accepted by the bot's argmatch.go (arg.normal..arg.sparkly).
func lureTypeChoices(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOptionChoice {
	return choiceList(bundle, "lure.lure_type", []choiceDef{
		{"normal", "Normal"},
		{"glacial", "Glacial"},
		{"mossy", "Mossy"},
		{"magnetic", "Magnetic"},
		{"rainy", "Rainy"},
		{"sparkly", "Sparkly"},
	})
}

// maxbattleLevelChoices enumerates battle tiers 1..6. The slash form is
// stricter than the bot (which accepts any int), but the canonical set
// of supported max battle tiers in the game today is 1..6.
func maxbattleLevelChoices(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOptionChoice {
	defs := make([]choiceDef, 0, 6)
	for i := 1; i <= 6; i++ {
		defs = append(defs, choiceDef{Value: i, Fallback: fmt.Sprintf("Level %d", i)})
	}
	return choiceList(bundle, "maxbattle.level", defs)
}

// fortTypeChoices maps the slash /fort fort_type integer to the canonical
// English fort-type keyword via fortTypeName in mappers/common.go.
func fortTypeChoices(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOptionChoice {
	return choiceList(bundle, "fort.fort_type", []choiceDef{
		{0, "Pokestop"},
		{1, "Gym"},
	})
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
// option. Shared by /raid and /egg — cmdKey is "raid" or "egg" so each
// command's i18n keys stay independent (slash.choice.raid.level.5 vs
// slash.choice.egg.level.5) even though the value list is identical.
func raidLevelOptionChoices(bundle *i18n.Bundle, cmdKey string) []*discordgo.ApplicationCommandOptionChoice {
	defs := make([]choiceDef, len(raidLevelChoices))
	for i, c := range raidLevelChoices {
		defs[i] = choiceDef{Value: c.Value, Fallback: c.Label}
	}
	return choiceList(bundle, cmdKey+".level", defs)
}

// teamChoices builds the Discord choice slice for the gym-control team
// option. Values match the canonical team IDs in argmatch.go. cmdKey is
// the calling command ("raid", "egg", "gym") so each command can localize
// the team labels independently if desired.
func teamChoices(bundle *i18n.Bundle, cmdKey string) []*discordgo.ApplicationCommandOptionChoice {
	return choiceList(bundle, cmdKey+".team", []choiceDef{
		{0, "Harmony"},
		{1, "Mystic"},
		{2, "Valor"},
		{3, "Instinct"},
	})
}

// sizeChoices builds the Discord choice slice for the pokemon size option.
// "all" is the explicit catch-all that omits a size token from the rendered
// text command, matching the text bot's "no size filter" default.
func sizeChoices(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOptionChoice {
	return choiceList(bundle, "track.size", []choiceDef{
		{"all", "All sizes"},
		{"xxs", "XXS"},
		{"xs", "XS"},
		{"m", "M"},
		{"xl", "XL"},
		{"xxl", "XXL"},
	})
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
		"cmd.start", "cmd.stop",
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
