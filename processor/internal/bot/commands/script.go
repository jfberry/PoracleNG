package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	// db types still used for MonsterTrackingAPI etc. struct references
)

type ScriptCommand struct{}

func (c *ScriptCommand) Name() string      { return "cmd.script" }
func (c *ScriptCommand) Aliases() []string { return nil }

func (c *ScriptCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	prefix := bot.CommandPrefix(ctx)

	var sb strings.Builder

	// Monster tracking → !track commands
	monsters, err := ctx.Tracking.Monsters.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select monsters: %v", err)
	}
	for _, m := range monsters {
		sb.WriteString(c.monsterToScript(ctx, prefix, &m))
		sb.WriteByte('\n')
	}

	// Raid tracking
	raids, err := ctx.Tracking.Raids.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select raids: %v", err)
	}
	for _, r := range raids {
		sb.WriteString(c.raidToScript(ctx, prefix, &r))
		sb.WriteByte('\n')
	}

	// Egg tracking
	eggs, err := ctx.Tracking.Eggs.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select eggs: %v", err)
	}
	for _, e := range eggs {
		sb.WriteString(c.eggToScript(ctx, prefix, &e))
		sb.WriteByte('\n')
	}

	// Invasion tracking
	invasions, err := ctx.Tracking.Invasions.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select invasions: %v", err)
	}
	for _, inv := range invasions {
		sb.WriteString(c.invasionToScript(ctx, prefix, &inv))
		sb.WriteByte('\n')
	}

	// Lure tracking
	lures, err := ctx.Tracking.Lures.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select lures: %v", err)
	}
	for _, l := range lures {
		sb.WriteString(c.lureToScript(ctx, prefix, &l))
		sb.WriteByte('\n')
	}

	// Nest tracking
	nests, err := ctx.Tracking.Nests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select nests: %v", err)
	}
	for _, n := range nests {
		sb.WriteString(c.nestToScript(ctx, prefix, &n))
		sb.WriteByte('\n')
	}

	// Gym tracking
	gyms, err := ctx.Tracking.Gyms.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select gyms: %v", err)
	}
	for _, g := range gyms {
		sb.WriteString(c.gymToScript(ctx, prefix, &g))
		sb.WriteByte('\n')
	}

	// Quest tracking
	quests, err := ctx.Tracking.Quests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select quests: %v", err)
	}
	for _, q := range quests {
		sb.WriteString(c.questToScript(ctx, prefix, &q))
		sb.WriteByte('\n')
	}

	// Max battle tracking
	maxbattles, err := ctx.Tracking.Maxbattles.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select maxbattles: %v", err)
	}
	for _, mb := range maxbattles {
		sb.WriteString(c.maxbattleToScript(ctx, prefix, &mb))
		sb.WriteByte('\n')
	}

	// Fort update tracking
	forts, err := ctx.Tracking.Forts.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select forts: %v", err)
	}
	for _, f := range forts {
		sb.WriteString(c.fortToScript(ctx, prefix, &f))
		sb.WriteByte('\n')
	}

	text := strings.TrimSpace(sb.String())
	if text == "" {
		return []bot.Reply{{Text: tr.T("msg.script.none")}}
	}

	// Send as file if too long
	maxLen := 2000
	if ctx.Platform == "telegram" {
		maxLen = 4095
	}
	if len(text) > maxLen {
		return []bot.Reply{{
			Text: tr.T("msg.script.file"),
			Attachment: &bot.Attachment{
				Filename: "tracking_script.txt",
				Content:  []byte(text),
			},
		}}
	}

	return []bot.Reply{{Text: text}}
}

func (c *ScriptCommand) monsterToScript(ctx *bot.CommandContext, prefix string, m *db.MonsterTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"track")

	// Pokemon name
	if m.PokemonID == 0 {
		parts = append(parts, "everything")
	} else {
		parts = append(parts, scriptToken(c.pokemonName(ctx, m.PokemonID)))
	}

	if m.Form != 0 {
		formName := c.formName(ctx, m.Form)
		if formName != "" {
			parts = append(parts, scriptToken("form:"+formName))
		}
	}

	if m.MinIV != -1 {
		if m.MaxIV != 100 {
			parts = append(parts, fmt.Sprintf("iv:%d-%d", m.MinIV, m.MaxIV))
		} else {
			parts = append(parts, fmt.Sprintf("iv:%d", m.MinIV))
		}
	}
	if m.MinCP > 0 || m.MaxCP != bot.WildcardID {
		parts = append(parts, fmt.Sprintf("cp:%d-%d", m.MinCP, m.MaxCP))
	}
	if m.MinLevel > 0 || m.MaxLevel != 55 {
		parts = append(parts, fmt.Sprintf("level:%d-%d", m.MinLevel, m.MaxLevel))
	}
	if m.ATK > 0 || m.MaxATK != 15 {
		parts = append(parts, fmt.Sprintf("atk:%d-%d", m.ATK, m.MaxATK))
	}
	if m.DEF > 0 || m.MaxDEF != 15 {
		parts = append(parts, fmt.Sprintf("def:%d-%d", m.DEF, m.MaxDEF))
	}
	if m.STA > 0 || m.MaxSTA != 15 {
		parts = append(parts, fmt.Sprintf("sta:%d-%d", m.STA, m.MaxSTA))
	}
	// weight intentionally not exported — !track no longer accepts weight
	// arguments, so re-importing the script would fail to parse "weight:".
	if m.Gender != 0 {
		switch m.Gender {
		case 1:
			parts = append(parts, "male")
		case 2:
			parts = append(parts, "female")
		case 3:
			parts = append(parts, "genderless")
		}
	}
	if m.Rarity > 0 || m.MaxRarity != 6 {
		parts = append(parts, fmt.Sprintf("rarity:%d-%d", m.Rarity, m.MaxRarity))
	}
	if m.Size > 0 || m.MaxSize != 5 {
		parts = append(parts, fmt.Sprintf("size:%d-%d", m.Size, m.MaxSize))
	}
	if db.IsPersonalShiny(m.Personal) {
		parts = append(parts, "personalshiny")
	}
	if db.IsPersonalBackground(m.Personal) {
		parts = append(parts, "personalbackground")
	}
	if m.PVPRankingLeague != 0 {
		league := ""
		switch m.PVPRankingLeague {
		case 500:
			league = "little"
		case 1500:
			league = "great"
		case 2500:
			league = "ultra"
		}
		if m.PVPRankingBest > 1 {
			parts = append(parts, fmt.Sprintf("%s:%d-%d", league, m.PVPRankingBest, m.PVPRankingWorst))
		} else {
			parts = append(parts, fmt.Sprintf("%s:%d", league, m.PVPRankingWorst))
		}
		if m.PVPRankingMinCP > 0 {
			parts = append(parts, fmt.Sprintf("%scp:%d", league, m.PVPRankingMinCP))
		}
		if m.PVPRankingCap > 0 {
			parts = append(parts, fmt.Sprintf("cap:%d", m.PVPRankingCap))
		}
	}
	if m.MinTime > 0 {
		parts = append(parts, fmt.Sprintf("t:%d", m.MinTime))
	}
	if m.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", m.Distance))
	}
	parts = append(parts, c.commonSuffix(ctx, ruleSettings{
		Template: m.Template, Clean: m.Clean, Ping: m.Ping, Distance: m.Distance,
		Location: m.OverrideLocationLabel, Areas: m.OverrideAreas,
	}, cleanBit|editBit)...)

	return strings.Join(parts, " ")
}

func (c *ScriptCommand) raidToScript(ctx *bot.CommandContext, prefix string, r *db.RaidTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"raid")

	if r.PokemonID != bot.WildcardID {
		parts = append(parts, scriptToken(c.pokemonName(ctx, r.PokemonID)))
	} else {
		parts = append(parts, fmt.Sprintf("level:%d", r.Level))
	}

	if r.Move != bot.WildcardID {
		moveName := c.moveName(ctx, r.Move)
		if moveName != "" {
			parts = append(parts, "move:"+moveName)
		}
	}
	parts = append(parts, c.teamArg(r.Team)...)
	if bool(r.Exclusive) {
		parts = append(parts, "ex")
	}
	if r.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", r.Distance))
	}
	parts = append(parts, c.rsvpArg(r.RSVPChanges)...)
	parts = append(parts, c.commonSuffix(ctx, ruleSettings{
		Template: r.Template, Clean: r.Clean, Ping: r.Ping, Distance: r.Distance,
		Location: r.OverrideLocationLabel, Areas: r.OverrideAreas,
	}, cleanBit|editBit)...)

	return strings.Join(parts, " ")
}

func (c *ScriptCommand) eggToScript(ctx *bot.CommandContext, prefix string, e *db.EggTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"egg")
	parts = append(parts, fmt.Sprintf("level:%d", e.Level))
	parts = append(parts, c.teamArg(e.Team)...)
	if bool(e.Exclusive) {
		parts = append(parts, "ex")
	}
	if e.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", e.Distance))
	}
	parts = append(parts, c.rsvpArg(e.RSVPChanges)...)
	parts = append(parts, c.commonSuffix(ctx, ruleSettings{
		Template: e.Template, Clean: e.Clean, Ping: e.Ping, Distance: e.Distance,
		Location: e.OverrideLocationLabel, Areas: e.OverrideAreas,
	}, cleanBit|editBit)...)
	return strings.Join(parts, " ")
}

func (c *ScriptCommand) invasionToScript(ctx *bot.CommandContext, prefix string, inv *db.InvasionTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"invasion")
	if inv.GruntType != "" {
		parts = append(parts, inv.GruntType)
	} else {
		parts = append(parts, "everything")
	}
	switch inv.Gender {
	case 1:
		parts = append(parts, "male")
	case 2:
		parts = append(parts, "female")
	}
	if inv.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", inv.Distance))
	}
	parts = append(parts, c.commonSuffix(ctx, ruleSettings{
		Template: inv.Template, Clean: inv.Clean, Ping: inv.Ping, Distance: inv.Distance,
		Location: inv.OverrideLocationLabel, Areas: inv.OverrideAreas,
	}, cleanBit)...)
	return strings.Join(parts, " ")
}

func (c *ScriptCommand) lureToScript(ctx *bot.CommandContext, prefix string, l *db.LureTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"lure")
	lureNames := map[int]string{0: "everything", 502: "glacial", 503: "mossy", 504: "magnetic", 505: "rainy", 506: "sparkly"}
	if name, ok := lureNames[l.LureID]; ok {
		parts = append(parts, name)
	} else {
		parts = append(parts, fmt.Sprintf("%d", l.LureID))
	}
	if l.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", l.Distance))
	}
	parts = append(parts, c.commonSuffix(ctx, ruleSettings{
		Template: l.Template, Clean: l.Clean, Ping: l.Ping, Distance: l.Distance,
		Location: l.OverrideLocationLabel, Areas: l.OverrideAreas,
	}, cleanBit|editBit)...)
	return strings.Join(parts, " ")
}

func (c *ScriptCommand) nestToScript(ctx *bot.CommandContext, prefix string, n *db.NestTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"nest")
	if n.PokemonID == 0 {
		parts = append(parts, "everything")
	} else {
		parts = append(parts, scriptToken(c.pokemonName(ctx, n.PokemonID)))
	}
	if n.MinSpawnAvg > 0 {
		parts = append(parts, fmt.Sprintf("minspawn:%d", n.MinSpawnAvg))
	}
	if n.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", n.Distance))
	}
	parts = append(parts, c.commonSuffix(ctx, ruleSettings{
		Template: n.Template, Clean: n.Clean, Ping: n.Ping, Distance: n.Distance,
		Location: n.OverrideLocationLabel, Areas: n.OverrideAreas,
	}, cleanBit)...)
	return strings.Join(parts, " ")
}

func (c *ScriptCommand) gymToScript(ctx *bot.CommandContext, prefix string, g *db.GymTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"gym")
	teamNames := map[int]string{0: "harmony", 1: "mystic", 2: "valor", 3: "instinct"}
	if name, ok := teamNames[g.Team]; ok {
		parts = append(parts, name)
	} else {
		parts = append(parts, "everything")
	}
	if bool(g.SlotChanges) {
		parts = append(parts, "slot_changes")
	}
	if bool(g.BattleChanges) {
		parts = append(parts, "battle_changes")
	}
	if g.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", g.Distance))
	}
	parts = append(parts, c.commonSuffix(ctx, ruleSettings{
		Template: g.Template, Clean: g.Clean, Ping: g.Ping, Distance: g.Distance,
		Location: g.OverrideLocationLabel, Areas: g.OverrideAreas,
	}, cleanBit)...)
	return strings.Join(parts, " ")
}

func (c *ScriptCommand) questToScript(ctx *bot.CommandContext, prefix string, q *db.QuestTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"quest")

	switch q.RewardType {
	case 3: // stardust — minimum amount stored in Reward field
		if q.Reward > 0 {
			parts = append(parts, fmt.Sprintf("stardust:%d", q.Reward))
		} else {
			parts = append(parts, "stardust")
		}
	case 8: // pokecoins — minimum amount stored in Reward field
		if q.Reward > 0 {
			parts = append(parts, fmt.Sprintf("pokecoins:%d", q.Reward))
		} else {
			parts = append(parts, "pokecoins")
		}
	case 4: // candy
		if q.Reward > 0 {
			parts = append(parts, scriptToken("candy:"+c.pokemonName(ctx, q.Reward)))
		} else {
			parts = append(parts, "candy")
		}
	case 7: // pokemon — reward 0 is the "all pokemon" wildcard
		if q.Reward > 0 {
			parts = append(parts, scriptToken(c.pokemonName(ctx, q.Reward)))
			if q.Form != 0 {
				if formName := c.formName(ctx, q.Form); formName != "" {
					parts = append(parts, scriptToken("form:"+formName))
				}
			}
		} else {
			parts = append(parts, "all_pokemon")
		}
	case 12: // energy
		if q.Reward > 0 {
			parts = append(parts, scriptToken("energy:"+c.pokemonName(ctx, q.Reward)))
		} else {
			parts = append(parts, "energy")
		}
	case 2: // item — reward 0 is the "all items" wildcard
		if q.Reward == 0 {
			parts = append(parts, "all_items")
		} else if name := c.itemName(ctx, q.Reward); name != "" {
			parts = append(parts, scriptToken(name))
		} else {
			parts = append(parts, fmt.Sprintf("reward_type:%d", q.RewardType))
		}
	default:
		parts = append(parts, fmt.Sprintf("reward_type:%d", q.RewardType))
	}

	// Only item, candy and energy rewards keep a minimum in Amount
	// (stardust and pokecoins keep theirs in Reward, emitted above).
	if q.Amount > 0 && (q.RewardType == 2 || q.RewardType == 4 || q.RewardType == 12) {
		parts = append(parts, fmt.Sprintf("amount:%d", q.Amount))
	}
	if bool(q.Shiny) {
		parts = append(parts, "shiny")
	}
	if q.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", q.Distance))
	}
	parts = append(parts, c.commonSuffix(ctx, ruleSettings{
		Template: q.Template, Clean: q.Clean, Ping: q.Ping, Distance: q.Distance,
		Location: q.OverrideLocationLabel, Areas: q.OverrideAreas,
	}, cleanBit|summaryBit)...)
	return strings.Join(parts, " ")
}

func (c *ScriptCommand) maxbattleToScript(ctx *bot.CommandContext, prefix string, mb *db.MaxbattleTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"maxbattle")

	switch {
	case mb.PokemonID != bot.WildcardID:
		parts = append(parts, scriptToken(c.pokemonName(ctx, mb.PokemonID)))
		if mb.Form != 0 {
			if formName := c.formName(ctx, mb.Form); formName != "" {
				parts = append(parts, scriptToken("form:"+formName))
			}
		}
	case mb.Level == 90: // 90 = all levels
		parts = append(parts, "everything")
	default:
		parts = append(parts, fmt.Sprintf("level:%d", mb.Level))
	}

	if mb.Move != bot.WildcardID {
		if moveName := c.moveName(ctx, mb.Move); moveName != "" {
			parts = append(parts, "move:"+moveName)
		}
	}
	if mb.Gmax != 0 {
		parts = append(parts, "gmax")
	}
	if mb.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", mb.Distance))
	}
	// !maxbattle accepts clean but not edit (maxbattle alerts carry no edit key).
	parts = append(parts, c.commonSuffix(ctx, ruleSettings{
		Template: mb.Template, Clean: mb.Clean, Ping: mb.Ping, Distance: mb.Distance,
		Location: mb.OverrideLocationLabel, Areas: mb.OverrideAreas,
	}, cleanBit)...)
	return strings.Join(parts, " ")
}

// fortChangeTypeKeywords maps stored change_types values back to the
// !fort keyword that produces them (photo is stored as Golbat's image_url).
var fortChangeTypeKeywords = map[string]string{
	"location":    "location",
	"new":         "new",
	"removal":     "removal",
	"image_url":   "photo",
	"name":        "name",
	"description": "description",
}

func (c *ScriptCommand) fortToScript(ctx *bot.CommandContext, prefix string, f *db.FortTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"fort")

	switch f.FortType {
	case "pokestop", "gym", "station":
		parts = append(parts, f.FortType)
	default:
		parts = append(parts, "everything")
	}

	var changeTypes []string
	if f.ChangeTypes != "" {
		if err := json.Unmarshal([]byte(f.ChangeTypes), &changeTypes); err != nil {
			log.Warnf("script: fort rule %d has unparseable change_types %q: %v", f.UID, f.ChangeTypes, err)
		}
	}
	for _, ct := range changeTypes {
		if kw, ok := fortChangeTypeKeywords[ct]; ok {
			parts = append(parts, kw)
		}
	}
	if bool(f.IncludeEmpty) {
		parts = append(parts, "include_empty")
	}
	if f.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", f.Distance))
	}
	// !fort takes no clean/edit keyword (fort rules have no clean column).
	parts = append(parts, c.commonSuffix(ctx, ruleSettings{
		Template: f.Template, Ping: f.Ping, Distance: f.Distance,
		Location: f.OverrideLocationLabel, Areas: f.OverrideAreas,
	}, 0)...)
	return strings.Join(parts, " ")
}

// Helper methods

func (c *ScriptCommand) pokemonName(ctx *bot.CommandContext, pokemonID int) string {
	if ctx.GameData == nil {
		return fmt.Sprintf("%d", pokemonID)
	}
	tr := ctx.Translations.For("en")
	name := tr.T(gamedata.PokemonTranslationKey(pokemonID))
	if name == gamedata.PokemonTranslationKey(pokemonID) {
		return fmt.Sprintf("%d", pokemonID)
	}
	return strings.ToLower(name)
}

func (c *ScriptCommand) formName(ctx *bot.CommandContext, formID int) string {
	if ctx.GameData == nil {
		return ""
	}
	tr := ctx.Translations.For("en")
	name := tr.T(gamedata.FormTranslationKey(formID))
	if name == gamedata.FormTranslationKey(formID) {
		return ""
	}
	return strings.ToLower(name)
}

// itemName returns the lowercased English item name, the phrase !quest
// matches item names against.
func (c *ScriptCommand) itemName(ctx *bot.CommandContext, itemID int) string {
	if ctx.GameData == nil {
		return ""
	}
	tr := ctx.Translations.For("en")
	name := tr.T(gamedata.ItemTranslationKey(itemID))
	if name == gamedata.ItemTranslationKey(itemID) {
		return ""
	}
	return strings.ToLower(name)
}

func (c *ScriptCommand) moveName(ctx *bot.CommandContext, moveID int) string {
	if ctx.GameData == nil {
		return ""
	}
	tr := ctx.Translations.For("en")
	name := tr.T(gamedata.MoveTranslationKey(moveID))
	if name == gamedata.MoveTranslationKey(moveID) {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(name, " ", "_"))
}

func (c *ScriptCommand) teamArg(team int) []string {
	teamNames := map[int]string{0: "harmony", 1: "mystic", 2: "valor", 3: "instinct"}
	if name, ok := teamNames[team]; ok {
		return []string{name}
	}
	return nil // team 4 = any, no arg needed
}

func (c *ScriptCommand) rsvpArg(rsvpChanges int) []string {
	switch rsvpChanges {
	case 1:
		return []string{"rsvp"}
	case 2:
		return []string{"rsvp_only"}
	default:
		return nil
	}
}

// Clean bits a tracking command accepts as keywords (see db/clean.go).
const (
	cleanBit   = 1 // clean
	editBit    = 2 // edit
	summaryBit = 4 // summary (quest only)
)

// ruleSettings are the per-rule settings every tracking command accepts
// alongside its own filters.
type ruleSettings struct {
	Template string
	Clean    int
	Ping     string
	Distance int      // only to decide which override applies; d: is emitted by the caller
	Location string   // override_location_label
	Areas    []string // override_areas
}

// commonSuffix emits the shared settings of a rule. cleanBits is the set of
// clean bits the command accepts as keywords; a bit outside it is left out
// rather than emitted as a keyword the command would reject.
func (c *ScriptCommand) commonSuffix(ctx *bot.CommandContext, s ruleSettings, cleanBits int) []string {
	var parts []string
	if s.Template != ctx.DefaultTemplate() {
		parts = append(parts, scriptToken("template:"+s.Template))
	}
	clean := s.Clean & cleanBits
	if clean&cleanBit != 0 {
		parts = append(parts, "clean")
	}
	if clean&editBit != 0 {
		parts = append(parts, "edit")
	}
	if clean&summaryBit != 0 {
		parts = append(parts, "summary")
	}
	// The commands only accept location: with a distance and area: without
	// one. A row written through the API can break that; the matcher then
	// ignores the override (a distance rule never reads override_areas, an
	// area rule matches on areas whatever its anchor), so leave it out
	// rather than emit a line the command rejects.
	if s.Location != "" && s.Distance > 0 {
		parts = append(parts, scriptToken("location:"+s.Location))
	}
	if s.Distance == 0 {
		for _, a := range s.Areas {
			parts = append(parts, scriptToken("area:"+a))
		}
	}
	// The stored ping is the space-joined mention tokens extractPings took
	// from the original command, so it re-parses as the same pings.
	if s.Ping != "" {
		parts = append(parts, s.Ping)
	}
	return parts
}

// scriptToken quotes a token that contains whitespace or an underscore. The
// parser keeps a quoted token whole and leaves its underscores alone, where
// it would otherwise split on the space or turn the underscore into one
// (which a saved-location label lookup would not match). The tokenizer has
// no escapes, so a double quote inside a quoted token is dropped, as
// discordbot's quoteForCommand does.
func scriptToken(tok string) string {
	if strings.ContainsAny(tok, " \t_") {
		return `"` + strings.ReplaceAll(tok, `"`, "") + `"`
	}
	return tok
}
