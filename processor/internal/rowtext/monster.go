package rowtext

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// MonsterRowText generates a human-readable description of a monster tracking rule.
func (g *Generator) MonsterRowText(tr *i18n.Translator, monster *db.MonsterTracking) string {
	name, formName := translateMonsterName(tr, g.GD, monster.PokemonID, monster.Form)

	minIV := fmt.Sprintf("%d", monster.MinIV)
	if monster.MinIV == -1 {
		minIV = "?"
	}

	minRarity := monster.Rarity
	if minRarity == -1 {
		minRarity = 1
	}

	minSize := monster.Size
	if minSize < 1 {
		minSize = 1
	}

	// PVP string
	var pvpString string
	if monster.PVPRankingLeague != 0 {
		leagueName := ""
		switch monster.PVPRankingLeague {
		case 500:
			leagueName = tr.T("tracking.pvp_little")
		case 1500:
			leagueName = tr.T("tracking.pvp_great")
		case 2500:
			leagueName = tr.T("tracking.pvp_ultra")
		}

		bestPrefix := ""
		if monster.PVPRankingBest > 1 {
			bestPrefix = fmt.Sprintf("%d-", monster.PVPRankingBest)
		}

		capStr := ""
		if monster.PVPRankingCap != 0 {
			capStr = " " + tr.Tf("tracking.level_cap_fmt", monster.PVPRankingCap)
		}

		pvpString = fmt.Sprintf("%s %s top%s%d (@%d+%s)",
			tr.T("tracking.pvp_ranking"), leagueName,
			bestPrefix, monster.PVPRankingWorst,
			monster.PVPRankingMinCP, capStr)
	}

	s := fmt.Sprintf("**%s** %s", name, formName)

	if monster.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", monster.Distance)
	}

	s += " | " + tr.Tf("tracking.iv_fmt", minIV, monster.MaxIV)
	s += " | " + tr.Tf("tracking.cp_fmt", monster.MinCP, monster.MaxCP)
	s += " | " + tr.Tf("tracking.level_fmt", monster.MinLevel, monster.MaxLevel)
	s += " | " + tr.Tf("tracking.stats_fmt",
		monster.ATK, monster.DEF, monster.STA,
		monster.MaxATK, monster.MaxDEF, monster.MaxSTA)

	if pvpString != "" {
		s += " | " + pvpString
	}

	if monster.Size > 0 || monster.MaxSize < 6 {
		minSizeName := tr.T(fmt.Sprintf("size_%d", minSize))
		maxSizeName := tr.T(fmt.Sprintf("size_%d", monster.MaxSize))
		s += " | " + tr.Tf("tracking.size_fmt", minSizeName, maxSizeName)
	}

	if monster.Rarity > 0 || monster.MaxRarity < 6 {
		minRarityName := tr.T(fmt.Sprintf("rarity_%d", minRarity))
		maxRarityName := tr.T(fmt.Sprintf("rarity_%d", monster.MaxRarity))
		s += " | " + tr.Tf("tracking.rarity_fmt", minRarityName, maxRarityName)
	}

	if monster.Gender != 0 {
		genderEmoji := ""
		if gi, ok := g.GD.Util.Genders[monster.Gender]; ok {
			genderEmoji = gi.Emoji
		}
		s += " | " + tr.Tf("tracking.gender_fmt", genderEmoji)
	}

	if monster.MinTime != 0 {
		s += " | " + tr.Tf("tracking.min_time_fmt", monster.MinTime)
	}

	s += " " + standardText(tr, monster.Template, g.DefaultTemplateName, monster.Clean)

	return s
}
