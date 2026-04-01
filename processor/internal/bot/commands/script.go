package commands

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

type ScriptCommand struct{}

func (c *ScriptCommand) Name() string      { return "cmd.script" }
func (c *ScriptCommand) Aliases() []string { return nil }

func (c *ScriptCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	prefix := commandPrefix(ctx)

	var sb strings.Builder

	// Monster tracking → !track commands
	monsters, err := db.SelectMonstersByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select monsters: %v", err)
	}
	for _, m := range monsters {
		sb.WriteString(c.monsterToScript(ctx, prefix, &m))
		sb.WriteByte('\n')
	}

	// Raid tracking
	raids, err := db.SelectRaidsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select raids: %v", err)
	}
	for _, r := range raids {
		sb.WriteString(c.raidToScript(ctx, prefix, &r))
		sb.WriteByte('\n')
	}

	// Egg tracking
	eggs, err := db.SelectEggsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select eggs: %v", err)
	}
	for _, e := range eggs {
		sb.WriteString(c.eggToScript(ctx, prefix, &e))
		sb.WriteByte('\n')
	}

	// Invasion tracking
	invasions, err := db.SelectInvasionsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select invasions: %v", err)
	}
	for _, inv := range invasions {
		sb.WriteString(c.invasionToScript(prefix, &inv))
		sb.WriteByte('\n')
	}

	// Lure tracking
	lures, err := db.SelectLuresByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select lures: %v", err)
	}
	for _, l := range lures {
		sb.WriteString(c.lureToScript(prefix, &l))
		sb.WriteByte('\n')
	}

	// Nest tracking
	nests, err := db.SelectNestsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select nests: %v", err)
	}
	for _, n := range nests {
		sb.WriteString(c.nestToScript(ctx, prefix, &n))
		sb.WriteByte('\n')
	}

	// Gym tracking
	gyms, err := db.SelectGymsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select gyms: %v", err)
	}
	for _, g := range gyms {
		sb.WriteString(c.gymToScript(prefix, &g))
		sb.WriteByte('\n')
	}

	// Quest tracking
	quests, err := db.SelectQuestsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("script: select quests: %v", err)
	}
	for _, q := range quests {
		sb.WriteString(c.questToScript(ctx, prefix, &q))
		sb.WriteByte('\n')
	}

	text := strings.TrimSpace(sb.String())
	if text == "" {
		return []bot.Reply{{Text: tr.T("cmd.script.none")}}
	}

	// Send as file if too long
	maxLen := 2000
	if ctx.Platform == "telegram" {
		maxLen = 4095
	}
	if len(text) > maxLen {
		return []bot.Reply{{
			Text: tr.T("cmd.script.file"),
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
		name := c.pokemonName(ctx, m.PokemonID)
		parts = append(parts, name)
	}

	if m.Form != 0 {
		formName := c.formName(ctx, m.Form)
		if formName != "" {
			parts = append(parts, "form:"+formName)
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
	if m.MinWeight > 0 || m.MaxWeight != 9000000 {
		parts = append(parts, fmt.Sprintf("weight:%d-%d", m.MinWeight, m.MaxWeight))
	}
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
	parts = append(parts, c.commonSuffix(ctx, m.Template, bool(m.Clean))...)

	return strings.Join(parts, " ")
}

func (c *ScriptCommand) raidToScript(ctx *bot.CommandContext, prefix string, r *db.RaidTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"raid")

	if r.PokemonID != bot.WildcardID {
		parts = append(parts, c.pokemonName(ctx, r.PokemonID))
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
	parts = append(parts, c.commonSuffix(ctx, r.Template, bool(r.Clean))...)

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
	parts = append(parts, c.commonSuffix(ctx, e.Template, bool(e.Clean))...)
	return strings.Join(parts, " ")
}

func (c *ScriptCommand) invasionToScript(prefix string, inv *db.InvasionTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"invasion")
	if inv.GruntType != "" {
		parts = append(parts, inv.GruntType)
	} else {
		parts = append(parts, "everything")
	}
	if inv.Gender == 1 {
		parts = append(parts, "male")
	} else if inv.Gender == 2 {
		parts = append(parts, "female")
	}
	if inv.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", inv.Distance))
	}
	return strings.Join(parts, " ")
}

func (c *ScriptCommand) lureToScript(prefix string, l *db.LureTrackingAPI) string {
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
	return strings.Join(parts, " ")
}

func (c *ScriptCommand) nestToScript(ctx *bot.CommandContext, prefix string, n *db.NestTrackingAPI) string {
	var parts []string
	parts = append(parts, prefix+"nest")
	if n.PokemonID == 0 {
		parts = append(parts, "everything")
	} else {
		parts = append(parts, c.pokemonName(ctx, n.PokemonID))
	}
	if n.MinSpawnAvg > 0 {
		parts = append(parts, fmt.Sprintf("minspawn:%d", n.MinSpawnAvg))
	}
	if n.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", n.Distance))
	}
	return strings.Join(parts, " ")
}

func (c *ScriptCommand) gymToScript(prefix string, g *db.GymTrackingAPI) string {
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
	case 4: // candy
		if q.Reward > 0 {
			parts = append(parts, "candy:"+c.pokemonName(ctx, q.Reward))
		} else {
			parts = append(parts, "candy")
		}
	case 7: // pokemon
		if q.Reward > 0 {
			parts = append(parts, c.pokemonName(ctx, q.Reward))
		} else {
			parts = append(parts, "everything")
		}
	case 12: // energy
		if q.Reward > 0 {
			parts = append(parts, "energy:"+c.pokemonName(ctx, q.Reward))
		} else {
			parts = append(parts, "energy")
		}
	default:
		parts = append(parts, fmt.Sprintf("reward_type:%d", q.RewardType))
	}

	if q.Distance > 0 {
		parts = append(parts, fmt.Sprintf("d:%d", q.Distance))
	}
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

func (c *ScriptCommand) commonSuffix(ctx *bot.CommandContext, template string, clean bool) []string {
	var parts []string
	defaultTemplate := ctx.DefaultTemplate()
	if template != defaultTemplate {
		parts = append(parts, "template:"+template)
	}
	if clean {
		parts = append(parts, "clean")
	}
	return parts
}

