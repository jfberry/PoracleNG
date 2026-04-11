package commands

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// commandPrefix returns the appropriate command prefix for the platform.
func commandPrefix(ctx *bot.CommandContext) string {
	if ctx.Platform == "telegram" {
		return "/"
	}
	prefix := ctx.Config.Discord.Prefix
	if prefix == "" {
		return "!"
	}
	return prefix
}

// usageReply returns a usage help reply if args are empty, or nil if args are present.
func usageReply(ctx *bot.CommandContext, args []string, usageKey string) *bot.Reply {
	if len(args) > 0 {
		return nil
	}
	tr := ctx.Tr()
	return &bot.Reply{Text: tr.Tf(usageKey, commandPrefix(ctx))}
}

// helpArgReply returns a usage reply if the first argument is "help", or nil otherwise.
func helpArgReply(ctx *bot.CommandContext, args []string, usageKey string) *bot.Reply {
	if len(args) > 0 && args[0] == "help" {
		tr := ctx.Tr()
		return &bot.Reply{Text: tr.Tf(usageKey, commandPrefix(ctx))}
	}
	return nil
}

// extractPings removes Discord @mention tokens (<@123456789> or <@!123456789>)
// from args and returns the combined ping string and remaining args.
func extractPings(args []string) (pings string, remaining []string) {
	var pingParts []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "<@") && strings.HasSuffix(arg, ">") {
			pingParts = append(pingParts, arg)
		} else {
			remaining = append(remaining, arg)
		}
	}
	return strings.Join(pingParts, " "), remaining
}

// enforceDistance applies default and max distance limits from config.
func enforceDistance(ctx *bot.CommandContext, distance int) int {
	if distance == 0 && ctx.Config.Tracking.DefaultDistance > 0 {
		distance = ctx.Config.Tracking.DefaultDistance
	}
	if ctx.Config.Tracking.MaxDistance > 0 && distance > ctx.Config.Tracking.MaxDistance {
		distance = ctx.Config.Tracking.MaxDistance
	}
	return distance
}

// trackingWarnings generates warning messages for common tracking issues.
// Returns a string to append to the reply, or empty if no warnings.
// Should be called by all tracking commands after successful tracking changes.
func trackingWarnings(ctx *bot.CommandContext, distance int) string {
	tr := ctx.Tr()
	var warnings []string

	// Check if user has stopped alerts
	if ctx.Humans != nil {
		if h, err := ctx.Humans.Get(ctx.TargetID); err == nil && h != nil && !h.Enabled {
			prefix := commandPrefix(ctx)
			warnings = append(warnings, tr.Tf("tracking.warn_stopped", prefix))
		}
	}

	// Check distance with no location
	if distance > 0 && !ctx.HasLocation {
		warnings = append(warnings, tr.T("tracking.warn_no_location"))
	}

	// Check no distance and no areas
	if distance == 0 && !ctx.HasArea {
		warnings = append(warnings, tr.T("tracking.warn_no_area"))
	}

	// Check max distance was applied
	if ctx.Config.Tracking.MaxDistance > 0 && distance > 0 && distance >= ctx.Config.Tracking.MaxDistance {
		warnings = append(warnings, tr.Tf("tracking.warn_max_distance", ctx.Config.Tracking.MaxDistance))
	}

	if len(warnings) == 0 {
		return ""
	}
	return "\n⚠️ " + strings.Join(warnings, "\n⚠️ ")
}

// targetDTSPlatform returns the DTS platform for template lookup based on TargetType.
// Webhooks always use discord templates.
func targetDTSPlatform(ctx *bot.CommandContext) string {
	if strings.HasPrefix(ctx.TargetType, "telegram") {
		return "telegram"
	}
	return "discord"
}

// validateTemplate checks if a DTS template exists for the given tracking type and
// template ID. For regular users, returns a blocking reply. For admins, returns nil
// but sets the warning text in the second return value. Returns (nil, "") if the
// template exists or DTS is unavailable.
func validateTemplate(ctx *bot.CommandContext, dtsType, templateID string) (*bot.Reply, string) {
	if ctx.DTS == nil {
		return nil, ""
	}
	platform := targetDTSPlatform(ctx)
	if ctx.DTS.Exists(dtsType, platform, templateID, ctx.Language) {
		return nil, ""
	}

	tr := ctx.Tr()
	if ctx.IsAdmin {
		return nil, tr.Tf("tracking.warn_template_not_found", templateID, dtsType, platform)
	}
	return &bot.Reply{React: "🙅", Text: tr.Tf("tracking.template_not_found", templateID)}, ""
}

// resolveMoveByName looks up a move ID by its translated name.
// Returns bot.WildcardID if no match is found.
func resolveMoveByName(ctx *bot.CommandContext, moveName string) int {
	if ctx.GameData == nil {
		return bot.WildcardID
	}
	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")
	for id := range ctx.GameData.Moves {
		key := gamedata.MoveTranslationKey(id)
		if strings.EqualFold(tr.T(key), moveName) || strings.EqualFold(enTr.T(key), moveName) {
			return id
		}
	}
	return bot.WildcardID
}

// buildTrackingMessage generates the confirmation message for tracking mutations.
// The rowFunc callbacks produce row text for each entry by index.
func buildTrackingMessage(
	tr *i18n.Translator,
	ctx *bot.CommandContext,
	unchangedCount, updateCount, insertCount int,
	unchangedRow func(i int) string,
	updateRow func(i int) string,
	insertRow func(i int) string,
) string {
	total := unchangedCount + updateCount + insertCount
	if total > 20 {
		return tr.Tf("tracking.bulk_changes",
			commandPrefix(ctx), tr.T("tracking.tracked"))
	}

	var sb strings.Builder
	for i := 0; i < unchangedCount; i++ {
		sb.WriteString(tr.T("tracking.unchanged"))
		sb.WriteString(unchangedRow(i))
		sb.WriteByte('\n')
	}
	for i := 0; i < updateCount; i++ {
		sb.WriteString(tr.T("tracking.updated"))
		sb.WriteString(updateRow(i))
		sb.WriteByte('\n')
	}
	for i := 0; i < insertCount; i++ {
		sb.WriteString(tr.T("tracking.new"))
		sb.WriteString(insertRow(i))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// formatRemovedRows builds the response text for removed tracking rows.
// Shows individual row descriptions up to a threshold, then falls back to a count.
func formatRemovedRows(tr *i18n.Translator, descriptions []string) string {
	var sb strings.Builder
	if len(descriptions) > 20 {
		sb.WriteString(tr.Tf("msg.removed_n", len(descriptions)))
	} else {
		for _, desc := range descriptions {
			sb.WriteString(tr.T("tracking.removed_prefix"))
			sb.WriteString(desc)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// removeByUIDs validates and deletes tracking rows by their UIDs.
// getUID extracts the UID from a row. describeRow generates display text for removed rows.
// Reports which UIDs were not found.
func removeByUIDs[T any](
	ctx *bot.CommandContext,
	trackingStore store.TrackingStore[T],
	requestedUIDs []int64,
	getUID func(*T) int64,
	describeRow func(*T) string,
) []bot.Reply {
	tr := ctx.Tr()

	// Load existing rows to validate UIDs and generate descriptions
	existing, err := trackingStore.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("remove by uid: select: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Build lookup of existing UIDs
	byUID := make(map[int64]*T, len(existing))
	for i := range existing {
		byUID[getUID(&existing[i])] = &existing[i]
	}

	// Classify requested UIDs as found or not found
	var toDelete []int64
	var descriptions []string
	var notFound []int64
	for _, uid := range requestedUIDs {
		if row, ok := byUID[uid]; ok {
			toDelete = append(toDelete, uid)
			descriptions = append(descriptions, describeRow(row))
		} else {
			notFound = append(notFound, uid)
		}
	}

	if len(toDelete) == 0 {
		return []bot.Reply{{React: "👌", Text: tr.T("msg.nothing_to_remove")}}
	}

	if err := trackingStore.DeleteByUIDs(ctx.TargetID, toDelete); err != nil {
		log.Errorf("remove by uid: delete: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()

	msg := formatRemovedRows(tr, descriptions)
	if len(notFound) > 0 {
		for _, uid := range notFound {
			msg += tr.Tf("tracking.uid_not_found", uid) + "\n"
		}
	}
	return []bot.Reply{{React: "✅", Text: strings.TrimSpace(msg)}}
}

// commonTrackFields holds the standard fields parsed by all tracking commands.
type commonTrackFields struct {
	Template     string
	Distance     int
	Clean        int
	TemplateWarn string // non-empty if admin and template not found
}

// parseCommonTrackFields extracts template, distance, and clean from parsed args.
// Validates the template if explicitly specified. Returns a blocking reply if a
// non-admin user specifies a template that doesn't exist.
func parseCommonTrackFields(ctx *bot.CommandContext, parsed *bot.ParsedArgs, dtsType string) (*commonTrackFields, *bot.Reply) {
	cleanVal := 0
	if parsed.HasKeyword("arg.clean") {
		cleanVal |= 1
	}
	if parsed.HasKeyword("arg.edit") {
		cleanVal |= 2
	}
	f := &commonTrackFields{
		Template: ctx.DefaultTemplate(),
		Clean:    cleanVal,
	}

	if t, ok := parsed.Strings["template"]; ok {
		f.Template = t
	}

	if _, explicit := parsed.Strings["template"]; explicit {
		if block, warn := validateTemplate(ctx, dtsType, f.Template); block != nil {
			return nil, block
		} else {
			f.TemplateWarn = warn
		}
	}

	if d, ok := parsed.Singles["d"]; ok {
		f.Distance = d
	}
	f.Distance = enforceDistance(ctx, f.Distance)

	return f, nil
}

// filterByForm narrows a pokemon list to only those matching the given form name.
// Checks the user's language and English fallback via form_{id} translation keys.
// Returns empty list if no matches found (form name not recognized).
func filterByForm(ctx *bot.CommandContext, monsters []bot.ResolvedPokemon, formName string) []bot.ResolvedPokemon {
	if ctx.GameData == nil || formName == "" {
		return monsters
	}
	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")
	var filtered []bot.ResolvedPokemon
	for _, mon := range monsters {
		for key := range ctx.GameData.Monsters {
			if key.ID != mon.PokemonID || key.Form == 0 {
				continue
			}
			formKey := gamedata.FormTranslationKey(key.Form)
			translatedForm := strings.ToLower(tr.T(formKey))
			enForm := strings.ToLower(enTr.T(formKey))
			if translatedForm == formName || enForm == formName {
				filtered = append(filtered, bot.ResolvedPokemon{PokemonID: key.ID, Form: key.Form})
			}
		}
	}
	return filtered
}

// filterByGen narrows a pokemon list to those in the specified generation.
func filterByGen(ctx *bot.CommandContext, monsters []bot.ResolvedPokemon, gen int) []bot.ResolvedPokemon {
	if ctx.GameData == nil {
		return monsters
	}
	genInfo := ctx.GameData.Util.GenData[gen]
	if genInfo.Min <= 0 || genInfo.Max <= 0 {
		return monsters
	}
	var filtered []bot.ResolvedPokemon
	for _, mon := range monsters {
		if mon.PokemonID >= genInfo.Min && mon.PokemonID <= genInfo.Max {
			filtered = append(filtered, mon)
		}
	}
	return filtered
}

// filterByTypes narrows a pokemon list to those with any of the specified types.
func filterByTypes(ctx *bot.CommandContext, monsters []bot.ResolvedPokemon, typeIDs []int) []bot.ResolvedPokemon {
	if ctx.GameData == nil || len(typeIDs) == 0 {
		return monsters
	}
	typeSet := make(map[int]bool, len(typeIDs))
	for _, t := range typeIDs {
		typeSet[t] = true
	}
	var filtered []bot.ResolvedPokemon
	for _, mon := range monsters {
		m := ctx.GameData.Monsters[gamedata.MonsterKey{ID: mon.PokemonID, Form: mon.Form}]
		if m == nil {
			m = ctx.GameData.Monsters[gamedata.MonsterKey{ID: mon.PokemonID, Form: 0}]
		}
		if m == nil {
			continue
		}
		for _, t := range m.Types {
			if typeSet[t] {
				filtered = append(filtered, mon)
				break
			}
		}
	}
	return filtered
}

// filterByGenAndType applies generation and type filters from parsed args.
func filterByGenAndType(ctx *bot.CommandContext, monsters []bot.ResolvedPokemon, parsed *bot.ParsedArgs) []bot.ResolvedPokemon {
	if gen, ok := parsed.Singles["gen"]; ok {
		monsters = filterByGen(ctx, monsters, gen)
	}
	if len(parsed.Types) > 0 {
		monsters = filterByTypes(ctx, monsters, parsed.Types)
	}
	return monsters
}
