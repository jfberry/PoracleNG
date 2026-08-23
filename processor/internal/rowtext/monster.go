package rowtext

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
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

	minSize := max(monster.Size, 1)

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

		switch monster.PVPRankingEvolution {
		case 1:
			pvpString += " " + tr.T("tracking.pvp_mega_any")
		case 2:
			pvpString += " " + tr.T("tracking.pvp_mega_x")
		case 3:
			pvpString += " " + tr.T("tracking.pvp_mega_y")
		}
	}

	s := fmt.Sprintf("**%s** %s", name, formName)

	// Costume: 9000 (the default/wildcard) matches any costume and is
	// omitted; 0 is the explicit "no costume" filter; N>0 shows the
	// translated costume name, falling back to the raw masterfile name if the
	// costume_<id> gamelocale key is missing (mirrors info.go's costumeName).
	if monster.Costume != 9000 {
		costumeName := tr.T("msg.no_costume")
		if monster.Costume != 0 {
			key := gamedata.CostumeTranslationKey(monster.Costume)
			costumeName = tr.T(key)
			if costumeName == key && g.GD != nil {
				if info, ok := g.GD.Costumes[monster.Costume]; ok && info.Name != "" {
					costumeName = info.Name
				}
			}
		}
		s += " | " + costumeName
	}

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

	// Weight is a legacy filter: !track no longer accepts it because
	// Golbat's weight field is unreliable, but existing rows with
	// non-default values still suppress matches at match time. Surface
	// them here so users can see why their old rule isn't firing and
	// remove the rule via !untrack id:N. The DB stores grams
	// (webhook kg × 1000) — display as the same integer the user
	// originally typed at !track weight:N-M.
	if monster.MinWeight > 0 || (monster.MaxWeight > 0 && monster.MaxWeight < 9000000) {
		s += " | " + tr.Tf("tracking.weight_fmt", monster.MinWeight, monster.MaxWeight)
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
	s = appendOverride(tr, s, monster.OverrideLocationLabel, monster.OverrideAreas)

	return s
}
