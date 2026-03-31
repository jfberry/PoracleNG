package commands

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

// TrackedCommand implements !tracked — list all active tracking rules.
type TrackedCommand struct{}

func (c *TrackedCommand) Name() string      { return "cmd.tracked" }
func (c *TrackedCommand) Aliases() []string { return nil }

func (c *TrackedCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	var sb strings.Builder

	// Pokemon
	monsters, err := db.SelectMonstersByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select monsters: %v", err)
	} else if len(monsters) > 0 {
		sb.WriteString("**Pokemon:**\n")
		for i := range monsters {
			mt := monsterAPIToTracking(&monsters[i])
			sb.WriteString(ctx.RowText.MonsterRowText(tr, mt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Raids
	raids, err := db.SelectRaidsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select raids: %v", err)
	} else if len(raids) > 0 {
		sb.WriteString("**Raids:**\n")
		for i := range raids {
			rt := raidAPIToTracking(&raids[i])
			sb.WriteString(ctx.RowText.RaidRowText(tr, rt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Eggs
	eggs, err := db.SelectEggsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select eggs: %v", err)
	} else if len(eggs) > 0 {
		sb.WriteString("**Eggs:**\n")
		for i := range eggs {
			et := eggAPIToTracking(&eggs[i])
			sb.WriteString(ctx.RowText.EggRowText(tr, et))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Quests
	quests, err := db.SelectQuestsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select quests: %v", err)
	} else if len(quests) > 0 {
		sb.WriteString("**Quests:**\n")
		for i := range quests {
			qt := questAPIToTracking(&quests[i])
			sb.WriteString(ctx.RowText.QuestRowText(tr, qt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Invasions
	invasions, err := db.SelectInvasionsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select invasions: %v", err)
	} else if len(invasions) > 0 {
		sb.WriteString("**Invasions:**\n")
		for i := range invasions {
			it := invasionAPIToTracking(&invasions[i])
			sb.WriteString(ctx.RowText.InvasionRowText(tr, it))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Lures
	lures, err := db.SelectLuresByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select lures: %v", err)
	} else if len(lures) > 0 {
		sb.WriteString("**Lures:**\n")
		for i := range lures {
			lt := lureAPIToTracking(&lures[i])
			sb.WriteString(ctx.RowText.LureRowText(tr, lt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Gyms
	gyms, err := db.SelectGymsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select gyms: %v", err)
	} else if len(gyms) > 0 {
		sb.WriteString("**Gyms:**\n")
		for i := range gyms {
			gt := gymAPIToTracking(&gyms[i])
			sb.WriteString(ctx.RowText.GymRowText(tr, gt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Nests
	nests, err := db.SelectNestsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select nests: %v", err)
	} else if len(nests) > 0 {
		sb.WriteString("**Nests:**\n")
		for i := range nests {
			nt := nestAPIToTracking(&nests[i])
			sb.WriteString(ctx.RowText.NestRowText(tr, nt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Forts
	forts, err := db.SelectFortsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select forts: %v", err)
	} else if len(forts) > 0 {
		sb.WriteString("**Fort Updates:**\n")
		for i := range forts {
			ft := fortAPIToTracking(&forts[i])
			sb.WriteString(ctx.RowText.FortUpdateRowText(tr, ft))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Maxbattles
	maxbattles, err := db.SelectMaxbattlesByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select maxbattles: %v", err)
	} else if len(maxbattles) > 0 {
		sb.WriteString("**Max Battles:**\n")
		for i := range maxbattles {
			mt := maxbattleAPIToTracking(&maxbattles[i])
			sb.WriteString(ctx.RowText.MaxbattleRowText(tr, mt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	text := strings.TrimSpace(sb.String())
	if text == "" {
		text = "No active tracking"
	}

	return []bot.Reply{{Text: text}}
}
